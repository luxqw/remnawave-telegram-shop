package tribute

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/payment"
	"remnawave-tg-shop-bot/internal/remnawave"
	"remnawave-tg-shop-bot/internal/translation"
	"strings"
	"time"

	"github.com/go-telegram/bot"
)

type Client struct {
	paymentService     *payment.PaymentService
	customerRepository *database.CustomerRepository
	webhookInbox       *database.WebhookInboxRepository
	remnawaveClient    *remnawave.Client
	telegramBot        *bot.Bot
	translation        *translation.Manager
}

const (
	CancelledSubscription = "cancelled_subscription"
	NewSubscription       = "new_subscription"
	TestHook              = ""
)

func NewClient(
	paymentService *payment.PaymentService,
	customerRepository *database.CustomerRepository,
	webhookInbox *database.WebhookInboxRepository,
	remnawaveClient *remnawave.Client,
	telegramBot *bot.Bot,
	translationManager *translation.Manager,
) *Client {
	return &Client{
		paymentService:     paymentService,
		customerRepository: customerRepository,
		webhookInbox:       webhookInbox,
		remnawaveClient:    remnawaveClient,
		telegramBot:        telegramBot,
		translation:        translationManager,
	}
}

func (c *Client) WebHookHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second*60)
		defer cancel()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			slog.Error("webhook: read body error", "error", err)
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		signature := r.Header.Get("trbt-signature")
		if signature == "" {
			http.Error(w, "missing signature", http.StatusUnauthorized)
			return
		}

		secret := config.GetTributeAPIKey()
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expected := hex.EncodeToString(mac.Sum(nil))

		if !hmac.Equal([]byte(expected), []byte(signature)) {
			log.Printf("webhook: bad signature (expected %s)", expected)
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		var wh SubscriptionWebhook
		if err := json.Unmarshal(body, &wh); err != nil {
			slog.Error("webhook: unmarshal error", "error", err, "payload", string(body))
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		inboxID, storeErr := c.webhookInbox.Create(ctx, body, wh.Name, "tribute")
		if storeErr != nil {
			slog.Error("webhook: store in inbox failed", "error", storeErr)
		}

		processErr := c.dispatch(ctx, wh, body)
		if processErr != nil {
			if inboxID > 0 {
				_ = c.webhookInbox.MarkFailed(ctx, inboxID, processErr.Error())
			}
			if errors.Is(processErr, payment.ErrCustomerNotFound) {
				slog.Warn("webhook: customer not found", "telegram_id", wh.Payload.TelegramUserID)
				w.WriteHeader(http.StatusOK)
				return
			}
			slog.Error("webhook: processing error", "error", processErr, "event", wh.Name)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if inboxID > 0 {
			_ = c.webhookInbox.MarkProcessed(ctx, inboxID)
		}
		w.WriteHeader(http.StatusOK)
	})
}

func (c *Client) dispatch(ctx context.Context, wh SubscriptionWebhook, body []byte) error {
	switch wh.Name {
	case NewSubscription:
		return c.newSubscriptionHandler(ctx, wh)
	case CancelledSubscription:
		return c.cancelSubscriptionHandler(ctx, wh)
	case TestHook:
		slog.Info("Tribute webhook working")
	}
	return nil
}

func (c *Client) RetryFailed(ctx context.Context) {
	items, err := c.webhookInbox.FindRetryable(ctx, "tribute", 3, 2*time.Minute)
	if err != nil {
		slog.Error("webhook retry: find retryable", "error", err)
		return
	}
	for _, item := range items {
		var wh SubscriptionWebhook
		if err := json.Unmarshal(item.Payload, &wh); err != nil {
			_ = c.webhookInbox.MarkFailed(ctx, item.ID, "unmarshal: "+err.Error())
			continue
		}
		if err := c.dispatch(ctx, wh, item.Payload); err != nil {
			slog.Warn("webhook retry: still failing", "id", item.ID, "event", item.EventType, "error", err)
			_ = c.webhookInbox.MarkFailed(ctx, item.ID, err.Error())
		} else {
			_ = c.webhookInbox.MarkProcessed(ctx, item.ID)
			slog.Info("webhook retry: recovered", "id", item.ID, "event", item.EventType)
		}
	}
}

// RetryByID re-dispatches a single webhook_inbox item on demand (admin webapp "Retry" button),
// mirroring the per-item body of RetryFailed's loop but for one explicitly chosen ID rather than
// the cron's batch of retryable failures.
func (c *Client) RetryByID(ctx context.Context, id int64) error {
	item, err := c.webhookInbox.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("find webhook inbox item: %w", err)
	}
	if item == nil {
		return fmt.Errorf("webhook inbox item %d not found", id)
	}
	var wh SubscriptionWebhook
	if err := json.Unmarshal(item.Payload, &wh); err != nil {
		_ = c.webhookInbox.MarkFailed(ctx, item.ID, "unmarshal: "+err.Error())
		return fmt.Errorf("unmarshal payload: %w", err)
	}
	if err := c.dispatch(ctx, wh, item.Payload); err != nil {
		_ = c.webhookInbox.MarkFailed(ctx, item.ID, err.Error())
		return fmt.Errorf("dispatch: %w", err)
	}
	_ = c.webhookInbox.MarkProcessed(ctx, item.ID)
	return nil
}

func (c *Client) cancelSubscriptionHandler(ctx context.Context, wh SubscriptionWebhook) error {
	return c.paymentService.CancelTributePurchase(ctx, wh.Payload.TelegramUserID)
}

func (c *Client) newSubscriptionHandler(ctx context.Context, wh SubscriptionWebhook) error {
	months := convertPeriodToMonths(wh.Payload.Period)
	customer, err := c.customerRepository.FindByTelegramId(ctx, wh.Payload.TelegramUserID)
	if err != nil {
		return fmt.Errorf("failed to find customer: %w", err)
	}
	if customer == nil {
		return fmt.Errorf("newSubscription: %w", payment.ErrCustomerNotFound)
	}
	_, purchaseId, _, err := c.paymentService.CreatePurchase(ctx, float64(wh.Payload.Amount)/100, months, customer, database.InvoiceTypeTribute)
	if err != nil {
		return err
	}
	if err := c.paymentService.ProcessPurchaseById(ctx, purchaseId); err != nil {
		return err
	}

	// A genuine incoming Tribute webhook resets the optimistic-renewal streak cap (decision 9) —
	// this customer's subscription is confirmed still actually being paid, not just assumed so by
	// the cron. Best-effort: never fail the purchase over a bookkeeping column.
	if err := c.customerRepository.UpdateFields(ctx, customer.ID, map[string]interface{}{"tribute_autorenew_streak": 0}); err != nil {
		slog.Error("newSubscription: reset tribute autorenew streak failed", "customer_id", customer.ID, "error", err)
	}
	return nil
}

func convertPeriodToMonths(period string) int {
	switch strings.ToLower(period) {
	case "monthly":
		return 1
	case "quarterly", "3-month", "3months", "3-months", "q":
		return 3
	case "halfyearly":
		return 6
	case "yearly", "annual", "y":
		return 12
	default:
		return 1
	}
}
