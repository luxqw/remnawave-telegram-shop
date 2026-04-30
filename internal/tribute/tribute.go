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
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type Client struct {
	paymentService     *payment.PaymentService
	customerRepository *database.CustomerRepository
	topupRepository    *database.TrafficTopupRepository
	webhookInbox       *database.WebhookInboxRepository
	remnawaveClient    *remnawave.Client
	telegramBot        *bot.Bot
}

const (
	CancelledSubscription = "cancelled_subscription"
	NewSubscription       = "new_subscription"
	NewDigitalProduct     = "new_digital_product"
	TestHook              = ""
)

func NewClient(
	paymentService *payment.PaymentService,
	customerRepository *database.CustomerRepository,
	topupRepository *database.TrafficTopupRepository,
	webhookInbox *database.WebhookInboxRepository,
	remnawaveClient *remnawave.Client,
	telegramBot *bot.Bot,
) *Client {
	return &Client{
		paymentService:     paymentService,
		customerRepository: customerRepository,
		topupRepository:    topupRepository,
		webhookInbox:       webhookInbox,
		remnawaveClient:    remnawaveClient,
		telegramBot:        telegramBot,
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

		inboxID, storeErr := c.webhookInbox.Create(ctx, body, wh.Name)
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
	case NewDigitalProduct:
		if config.TopupEnabled() {
			if pkg, ok := config.TopupPackageByProductID(wh.Payload.ProductID); ok {
				return c.handleTopupPayment(ctx, wh, pkg)
			}
		}
		slog.Warn("webhook: unknown digital product", "product_id", wh.Payload.ProductID)
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
	items, err := c.webhookInbox.FindRetryable(ctx, 3, 2*time.Minute)
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
		return fmt.Errorf("customer not found for telegram_id: %d", wh.Payload.TelegramUserID)
	}
	_, purchaseId, err := c.paymentService.CreatePurchase(ctx, float64(wh.Payload.Amount), months, customer, database.InvoiceTypeTribute)
	if err != nil {
		return err
	}
	return c.paymentService.ProcessPurchaseById(ctx, purchaseId)
}

func (c *Client) handleTopupPayment(ctx context.Context, wh SubscriptionWebhook, pkg config.TopupPackageConfig) error {
	telegramID := wh.Payload.TelegramUserID
	tributePaymentID := strconv.Itoa(wh.Payload.PurchaseID)
	gbAmount := pkg.GBAmount

	existing, err := c.topupRepository.FindByTributePaymentID(ctx, tributePaymentID)
	if err != nil {
		return fmt.Errorf("topup: find by payment id: %w", err)
	}
	if existing != nil && existing.Status == database.TopupStatusCompleted {
		slog.Info("topup: duplicate webhook, already completed", "tribute_payment_id", tributePaymentID)
		return nil
	}
	if existing != nil && existing.Status == database.TopupStatusProcessing && existing.TargetTrafficLimitBytes != nil {
		return c.applyTopup(ctx, existing.ID, existing.RemnawaveUUID, telegramID, *existing.TargetTrafficLimitBytes, gbAmount)
	}

	rwUsers, err := c.remnawaveClient.GetUsersByTelegramID(ctx, telegramID)
	if err != nil {
		return fmt.Errorf("topup: get remnawave user: %w", err)
	}
	if len(rwUsers) == 0 {
		c.notifyUser(ctx, telegramID, "❌ Ошибка зачисления трафика: аккаунт не найден в панели. Обратитесь в поддержку.")
		c.notifyAdmin(ctx, fmt.Sprintf("Top-up: Remnawave user not found for telegram_id=%d, pkg=%dGB", telegramID, gbAmount))
		return fmt.Errorf("topup: remnawave user not found for telegram_id %d", telegramID)
	}
	rwUser := rwUsers[0]

	if rwUser.TrafficLimitBytes == 0 {
		slog.Warn("topup: user has unlimited traffic, skipping", "telegram_id", telegramID)
		c.notifyUser(ctx, telegramID, "ℹ️ У тебя безлимитный тариф — дополнительный трафик не нужен. Обратись в поддержку для возврата.")
		payID := tributePaymentID
		_, _ = c.topupRepository.Create(ctx, &database.TrafficTopup{
			TelegramID: telegramID, RemnawaveUUID: rwUser.UUID.String(), GBAmount: gbAmount,
			PriceAmount: float64(wh.Payload.Amount), Currency: wh.Payload.Currency,
			TributePaymentID: &payID, Status: database.TopupStatusCompleted,
		})
		return nil
	}

	if strings.ToUpper(rwUser.Status) != "ACTIVE" {
		slog.Warn("topup: user not active in Remnawave", "telegram_id", telegramID, "status", rwUser.Status)
		c.notifyUser(ctx, telegramID, "❌ Ошибка зачисления трафика: аккаунт не активен. Обратись в поддержку.")
		c.notifyAdmin(ctx, fmt.Sprintf("Top-up: user %d not active (status=%s), pkg=%dGB", telegramID, rwUser.Status, gbAmount))
		return fmt.Errorf("topup: user %d not active: %s", telegramID, rwUser.Status)
	}

	targetBytes := int64(rwUser.TrafficLimitBytes) + int64(gbAmount)*int64(config.BytesInGigabyte())
	remnaUUID := rwUser.UUID.String()

	var topupID int64
	if existing != nil {
		topupID = existing.ID
	} else {
		pending, err := c.topupRepository.FindPendingByTelegramIDAndGB(ctx, telegramID, gbAmount)
		if err != nil {
			return fmt.Errorf("topup: find pending: %w", err)
		}
		if pending != nil {
			if err := c.topupRepository.MarkProcessing(ctx, pending.ID, tributePaymentID, remnaUUID, targetBytes); err != nil {
				return fmt.Errorf("topup: mark processing: %w", err)
			}
			topupID = pending.ID
		} else {
			payID := tributePaymentID
			tb := targetBytes
			id, err := c.topupRepository.Create(ctx, &database.TrafficTopup{
				TelegramID: telegramID, RemnawaveUUID: remnaUUID, GBAmount: gbAmount,
				PriceAmount: float64(wh.Payload.Amount), Currency: wh.Payload.Currency,
				TributePaymentID: &payID, TargetTrafficLimitBytes: &tb, Status: database.TopupStatusProcessing,
			})
			if err != nil {
				return fmt.Errorf("topup: create record: %w", err)
			}
			topupID = id
		}
	}
	return c.applyTopup(ctx, topupID, remnaUUID, telegramID, targetBytes, gbAmount)
}

func (c *Client) applyTopup(ctx context.Context, topupID int64, remnaUUID string, telegramID int64, targetBytes int64, gbAmount int) error {
	rwUUID, err := parseUUID(remnaUUID)
	if err != nil {
		return fmt.Errorf("topup: parse uuid %q: %w", remnaUUID, err)
	}
	strategy := config.TrafficLimitResetStrategy()
	if err := c.remnawaveClient.UpdateUserTrafficLimit(ctx, rwUUID, int(targetBytes), strategy); err != nil {
		_ = c.topupRepository.MarkFailed(ctx, topupID)
		c.notifyAdmin(ctx, fmt.Sprintf("Top-up: Remnawave UpdateUser failed for telegram_id=%d, pkg=%dGB: %v", telegramID, gbAmount, err))
		return fmt.Errorf("topup: remnawave update: %w", err)
	}
	if err := c.topupRepository.MarkCompleted(ctx, topupID); err != nil {
		slog.Error("topup: mark completed failed (Remnawave already updated)", "error", err, "topup_id", topupID)
	}
	newLimitGB := int(targetBytes) / int(config.BytesInGigabyte())
	msg := fmt.Sprintf("✅ Зачислено <b>+%d ГБ</b>.\nТекущий лимит трафика: <b>%d ГБ</b>.", gbAmount, newLimitGB)
	c.notifyUser(ctx, telegramID, msg)
	slog.Info("topup: completed", "telegram_id", telegramID, "gb_amount", gbAmount, "new_limit_gb", newLimitGB)
	return nil
}

func (c *Client) notifyUser(ctx context.Context, telegramID int64, text string) {
	_, err := c.telegramBot.SendMessage(ctx, &bot.SendMessageParams{ChatID: telegramID, Text: text, ParseMode: models.ParseModeHTML})
	if err != nil {
		slog.Error("topup: failed to send user message", "telegram_id", telegramID, "error", err)
	}
}

func (c *Client) notifyAdmin(ctx context.Context, text string) {
	_, err := c.telegramBot.SendMessage(ctx, &bot.SendMessageParams{ChatID: config.GetAdminTelegramId(), Text: text})
	if err != nil {
		slog.Error("topup: failed to send admin message", "error", err)
	}
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
