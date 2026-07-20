package rollypay

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/remnawave"
	"remnawave-tg-shop-bot/internal/translation"
)

const signatureFreshnessWindow = 5 * time.Minute

// purchaseProcessor is the one payment.PaymentService method this package needs. Defined locally
// (rather than importing the payment package) because payment.go itself calls into rollypay.Client
// to create RollyPay invoices — importing payment here would create an import cycle.
type purchaseProcessor interface {
	ProcessPurchaseById(ctx context.Context, purchaseId int64) error
}

// WebhookClient receives and dispatches RollyPay's webhook callbacks. Kept separate from Client
// (the outbound payments API caller) because it carries a much larger, handler-shaped dependency
// set (payment service, repositories, webhook inbox) that CreatePayment/GetPaymentStatus callers
// don't need.
type WebhookClient struct {
	payments              *Client
	paymentService        purchaseProcessor
	purchaseRepository    *database.PurchaseRepository
	topupRepository       *database.TrafficTopupRepository
	deviceTopupRepository *database.DeviceTopupRepository
	webhookInbox          *database.WebhookInboxRepository
	remnawaveClient       *remnawave.Client
	telegramBot           *bot.Bot
	translation           *translation.Manager
}

func NewWebhookClient(
	payments *Client,
	paymentService purchaseProcessor,
	purchaseRepository *database.PurchaseRepository,
	topupRepository *database.TrafficTopupRepository,
	deviceTopupRepository *database.DeviceTopupRepository,
	webhookInbox *database.WebhookInboxRepository,
	remnawaveClient *remnawave.Client,
	telegramBot *bot.Bot,
	translationManager *translation.Manager,
) *WebhookClient {
	return &WebhookClient{
		payments:              payments,
		paymentService:        paymentService,
		purchaseRepository:    purchaseRepository,
		topupRepository:       topupRepository,
		deviceTopupRepository: deviceTopupRepository,
		webhookInbox:          webhookInbox,
		remnawaveClient:       remnawaveClient,
		telegramBot:           telegramBot,
		translation:           translationManager,
	}
}

func (c *WebhookClient) WebHookHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second*60)
		defer cancel()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			slog.Error("rollypay webhook: read body error", "error", err)
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		if err := c.verifySignature(r, body); err != nil {
			slog.Warn("rollypay webhook: signature check failed", "error", err)
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		var wh WebhookPayload
		if err := json.Unmarshal(body, &wh); err != nil {
			slog.Error("rollypay webhook: unmarshal error", "error", err, "payload", string(body))
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		inboxID, storeErr := c.webhookInbox.Create(ctx, body, wh.EventType, "rollypay")
		if storeErr != nil {
			slog.Error("rollypay webhook: store in inbox failed", "error", storeErr)
		}

		processErr := c.dispatch(ctx, wh)
		if processErr != nil {
			if inboxID > 0 {
				_ = c.webhookInbox.MarkFailed(ctx, inboxID, processErr.Error())
			}
			slog.Error("rollypay webhook: processing error", "error", processErr, "event", wh.EventType, "order_id", wh.OrderID)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if inboxID > 0 {
			_ = c.webhookInbox.MarkProcessed(ctx, inboxID)
		}
		w.WriteHeader(http.StatusOK)
	})
}

// verifySignature checks X-Signature = hex(HMAC-SHA256(signingSecret, X-Timestamp + "." + body))
// per docs.rollypay.io/api/callbacks, plus a freshness check RollyPay's spec doesn't itself
// require but which is cheap replay-attack protection worth having on a money path.
func (c *WebhookClient) verifySignature(r *http.Request, body []byte) error {
	signature := r.Header.Get("X-Signature")
	if signature == "" {
		return fmt.Errorf("missing X-Signature header")
	}
	timestamp := r.Header.Get("X-Timestamp")
	if timestamp == "" {
		return fmt.Errorf("missing X-Timestamp header")
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid X-Timestamp: %w", err)
	}
	if age := time.Since(time.Unix(ts, 0)); age > signatureFreshnessWindow || age < -signatureFreshnessWindow {
		return fmt.Errorf("timestamp outside freshness window: age=%s", age)
	}

	mac := hmac.New(sha256.New, []byte(c.payments.SigningSecret()))
	mac.Write([]byte(timestamp + "." + string(body)))
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

func (c *WebhookClient) dispatch(ctx context.Context, wh WebhookPayload) error {
	switch wh.EventType {
	case EventPaymentPaid:
		return c.handlePaid(ctx, wh)
	case EventPaymentCanceled, EventPaymentChargeback, EventPaymentRefunded:
		return c.handleNotPaid(ctx, wh.OrderID)
	default:
		slog.Info("rollypay webhook: ignoring event", "event", wh.EventType, "order_id", wh.OrderID)
		return nil
	}
}

func (c *WebhookClient) handlePaid(ctx context.Context, wh WebhookPayload) error {
	orderID := wh.OrderID
	switch {
	case strings.HasPrefix(orderID, "sub-"):
		return c.handleSubscriptionPaid(ctx, orderID)
	case strings.HasPrefix(orderID, "topup-"):
		return c.handleTopupPaid(ctx, orderID, wh.PaymentID)
	case strings.HasPrefix(orderID, "device-"):
		return c.handleDevicePaid(ctx, orderID, wh.PaymentID)
	default:
		slog.Warn("rollypay webhook: unrecognized order_id prefix", "order_id", orderID)
		return nil
	}
}

func (c *WebhookClient) handleSubscriptionPaid(ctx context.Context, orderID string) error {
	id, err := parsePurchaseID(orderID, "sub-")
	if err != nil {
		return err
	}
	purchase, err := c.purchaseRepository.FindById(ctx, id)
	if err != nil {
		return fmt.Errorf("find purchase %d: %w", id, err)
	}
	if purchase == nil {
		return fmt.Errorf("purchase %d not found", id)
	}
	if purchase.Status == database.PurchaseStatusPaid {
		slog.Info("rollypay webhook: duplicate payment.paid, already applied", "purchase_id", id)
		return nil
	}
	return c.paymentService.ProcessPurchaseById(ctx, id)
}

// handleTopupPaid applies a GB top-up, mirroring tribute.go's handleTopupPayment/applyTopup:
// fetch the customer's current Remnawave traffic limit, add the purchased GB on top, PATCH it,
// then mark the row completed with the target bytes recorded (payment.go's rollover logic reads
// that column, so this is required for the top-up to survive the next subscription renewal).
func (c *WebhookClient) handleTopupPaid(ctx context.Context, orderID, paymentID string) error {
	id, err := parsePurchaseID(orderID, "topup-")
	if err != nil {
		return err
	}
	t, err := c.topupRepository.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("find topup %d: %w", id, err)
	}
	if t == nil {
		return fmt.Errorf("topup %d not found", id)
	}
	if t.Status == database.TopupStatusCompleted {
		slog.Info("rollypay webhook: duplicate topup payment.paid, already applied", "topup_id", id)
		return nil
	}

	rwUsers, err := c.remnawaveClient.GetUsersByTelegramID(ctx, t.TelegramID)
	if err != nil {
		return fmt.Errorf("topup: get remnawave user: %w", err)
	}
	if len(rwUsers) == 0 {
		return fmt.Errorf("topup: remnawave user not found for telegram_id %d", t.TelegramID)
	}
	rwUser := rwUsers[0]

	targetBytes := int64(rwUser.TrafficLimitBytes) + int64(t.GBAmount)*int64(config.BytesInGigabyte())
	remnaUUID := rwUser.UUID.String()

	if err := c.topupRepository.MarkProcessingRollyPay(ctx, id, paymentID, remnaUUID, targetBytes); err != nil {
		return fmt.Errorf("topup: mark processing: %w", err)
	}
	if err := c.remnawaveClient.UpdateUserTrafficLimit(ctx, rwUser.UUID, int(targetBytes), rwUser.TrafficLimitStrategy); err != nil {
		_ = c.topupRepository.MarkFailed(ctx, id)
		return fmt.Errorf("topup: remnawave update: %w", err)
	}
	if err := c.topupRepository.MarkCompleted(ctx, id); err != nil {
		slog.Error("topup: mark completed failed (remnawave already updated)", "error", err, "topup_id", id)
	}

	newLimitGB := int(targetBytes) / config.BytesInGigabyte()
	c.notifyUserKey(ctx, t.TelegramID, "tribute_topup_success", t.GBAmount, newLimitGB)
	slog.Info("rollypay topup: completed", "telegram_id", t.TelegramID, "gb_amount", t.GBAmount, "new_limit_gb", newLimitGB)
	return nil
}

// handleDevicePaid applies a device-slot purchase: bump HwidDeviceLimit by the purchased count
// (always 1 today). No rollover bookkeeping is needed here — unlike traffic, a device limit isn't
// reset by subscription renewal (see remnawave/client.go UpdateUserDeviceLimit's comment), so a
// purchased slot is already permanent without any extra logic.
func (c *WebhookClient) handleDevicePaid(ctx context.Context, orderID, paymentID string) error {
	id, err := parsePurchaseID(orderID, "device-")
	if err != nil {
		return err
	}
	t, err := c.deviceTopupRepository.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("find device topup %d: %w", id, err)
	}
	if t == nil {
		return fmt.Errorf("device topup %d not found", id)
	}
	if t.Status == database.TopupStatusCompleted {
		slog.Info("rollypay webhook: duplicate device payment.paid, already applied", "device_topup_id", id)
		return nil
	}

	rwUsers, err := c.remnawaveClient.GetUsersByTelegramID(ctx, t.TelegramID)
	if err != nil {
		return fmt.Errorf("device topup: get remnawave user: %w", err)
	}
	if len(rwUsers) == 0 {
		return fmt.Errorf("device topup: remnawave user not found for telegram_id %d", t.TelegramID)
	}
	rwUser := rwUsers[0]

	currentLimit := 0
	if rwUser.HwidDeviceLimit != nil {
		currentLimit = *rwUser.HwidDeviceLimit
	}
	targetLimit := currentLimit + t.DeviceCount
	remnaUUID := rwUser.UUID.String()

	if err := c.deviceTopupRepository.MarkProcessing(ctx, id, paymentID, remnaUUID, targetLimit); err != nil {
		return fmt.Errorf("device topup: mark processing: %w", err)
	}
	if err := c.remnawaveClient.UpdateUserDeviceLimit(ctx, rwUser.UUID, targetLimit); err != nil {
		_ = c.deviceTopupRepository.MarkFailed(ctx, id)
		return fmt.Errorf("device topup: remnawave update: %w", err)
	}
	if err := c.deviceTopupRepository.MarkCompleted(ctx, id); err != nil {
		slog.Error("device topup: mark completed failed (remnawave already updated)", "error", err, "device_topup_id", id)
	}

	c.notifyUserKey(ctx, t.TelegramID, "device_topup_success", targetLimit)
	slog.Info("rollypay device topup: completed", "telegram_id", t.TelegramID, "new_limit", targetLimit)
	return nil
}

// handleNotPaid marks the underlying row cancelled/failed for admin visibility. It never claws
// back access already granted by an earlier payment.paid — matches how Tribute's cancellation
// flow only ever prevents future renewal, never revokes a period already provisioned.
func (c *WebhookClient) handleNotPaid(ctx context.Context, orderID string) error {
	switch {
	case strings.HasPrefix(orderID, "sub-"):
		id, err := parsePurchaseID(orderID, "sub-")
		if err != nil {
			return err
		}
		return c.purchaseRepository.UpdateFields(ctx, id, map[string]interface{}{
			"status": database.PurchaseStatusCancel,
		})
	case strings.HasPrefix(orderID, "topup-"):
		id, err := parsePurchaseID(orderID, "topup-")
		if err != nil {
			return err
		}
		return c.topupRepository.MarkFailed(ctx, id)
	case strings.HasPrefix(orderID, "device-"):
		id, err := parsePurchaseID(orderID, "device-")
		if err != nil {
			return err
		}
		return c.deviceTopupRepository.MarkFailed(ctx, id)
	default:
		slog.Warn("rollypay webhook: unrecognized order_id prefix on non-paid event", "order_id", orderID)
		return nil
	}
}

func (c *WebhookClient) notifyUserKey(ctx context.Context, telegramID int64, key string, args ...any) {
	if c.translation == nil || c.telegramBot == nil {
		return
	}
	text := c.translation.GetText("", key)
	if len(args) > 0 {
		text = fmt.Sprintf(text, args...)
	}
	_, err := c.telegramBot.SendMessage(ctx, &bot.SendMessageParams{ChatID: telegramID, Text: text, ParseMode: models.ParseModeHTML})
	if err != nil {
		slog.Error("rollypay webhook: failed to send user message", "telegram_id", telegramID, "error", err)
	}
}

func parsePurchaseID(orderID, prefix string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(orderID, prefix), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse id from order_id %q: %w", orderID, err)
	}
	return id, nil
}

// RetryFailed re-dispatches this provider's failed webhook_inbox rows — mirrors
// tribute.Client.RetryFailed, called from its own cron in cmd/app/main.go.
func (c *WebhookClient) RetryFailed(ctx context.Context) {
	items, err := c.webhookInbox.FindRetryable(ctx, "rollypay", 3, 2*time.Minute)
	if err != nil {
		slog.Error("rollypay webhook retry: find retryable", "error", err)
		return
	}
	for _, item := range items {
		var wh WebhookPayload
		if err := json.Unmarshal(item.Payload, &wh); err != nil {
			_ = c.webhookInbox.MarkFailed(ctx, item.ID, "unmarshal: "+err.Error())
			continue
		}
		if err := c.dispatch(ctx, wh); err != nil {
			slog.Warn("rollypay webhook retry: still failing", "id", item.ID, "event", item.EventType, "error", err)
			_ = c.webhookInbox.MarkFailed(ctx, item.ID, err.Error())
		} else {
			_ = c.webhookInbox.MarkProcessed(ctx, item.ID)
			slog.Info("rollypay webhook retry: recovered", "id", item.ID, "event", item.EventType)
		}
	}
}

// RetryByID re-dispatches a single webhook_inbox item on demand (admin webapp "Retry" button).
func (c *WebhookClient) RetryByID(ctx context.Context, id int64) error {
	item, err := c.webhookInbox.FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("find webhook inbox item: %w", err)
	}
	if item == nil {
		return fmt.Errorf("webhook inbox item %d not found", id)
	}
	var wh WebhookPayload
	if err := json.Unmarshal(item.Payload, &wh); err != nil {
		_ = c.webhookInbox.MarkFailed(ctx, item.ID, "unmarshal: "+err.Error())
		return fmt.Errorf("unmarshal payload: %w", err)
	}
	if err := c.dispatch(ctx, wh); err != nil {
		_ = c.webhookInbox.MarkFailed(ctx, item.ID, err.Error())
		return fmt.Errorf("dispatch: %w", err)
	}
	_ = c.webhookInbox.MarkProcessed(ctx, item.ID)
	return nil
}
