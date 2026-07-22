package notification

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/rollypay"
	"remnawave-tg-shop-bot/internal/translation"
)

type deviceAddonRepository interface {
	FindActiveExpiringBefore(ctx context.Context, cutoff time.Time) ([]*database.DeviceAddon, error)
}

type rollyPayCreator interface {
	CreatePayment(ctx context.Context, req rollypay.CreatePaymentRequest) (*rollypay.Payment, error)
}

// DeviceAddonRenewalService reminds standalone-billed device addon holders (Tribute-linked
// customers, whose subscription charge can't include a variable device cost — see decision 3 of
// the device-addon plan) to renew their addon's own independent cycle. RollyPay has no autopay, so
// a manual pay link is the only way to collect it — bundled addons need no reminder here, since
// their cost already rides the subscription-expiring reminder SubscriptionService sends.
type DeviceAddonRenewalService struct {
	deviceAddonRepository     deviceAddonRepository
	rollypayClient            rollyPayCreator
	telegramBot               *bot.Bot
	tm                        *translation.Manager
	notificationLogRepository *database.NotificationLogRepository
}

func NewDeviceAddonRenewalService(
	deviceAddonRepository *database.DeviceAddonRepository,
	rollypayClient *rollypay.Client,
	telegramBot *bot.Bot,
	tm *translation.Manager,
	notificationLogRepository *database.NotificationLogRepository,
) *DeviceAddonRenewalService {
	return &DeviceAddonRenewalService{
		deviceAddonRepository:     deviceAddonRepository,
		rollypayClient:            rollypayClient,
		telegramBot:               telegramBot,
		tm:                        tm,
		notificationLogRepository: notificationLogRepository,
	}
}

const deviceAddonRenewalNotificationType = "device_addon_renewal"

// ProcessRenewalReminders sends a reminder + fresh RollyPay pay link to every standalone device
// addon whose cycle ends within a day, at most once per calendar day per customer.
func (s *DeviceAddonRenewalService) ProcessRenewalReminders() error {
	ctx := context.Background()
	addons, err := s.deviceAddonRepository.FindActiveExpiringBefore(ctx, time.Now().Add(24*time.Hour))
	if err != nil {
		return fmt.Errorf("find expiring device addons: %w", err)
	}

	sent := 0
	for _, addon := range addons {
		if addon.BillingMode != database.AddonBillingModeStandalone {
			continue
		}
		if s.alreadySentToday(ctx, addon.TelegramID) {
			continue
		}
		if err := s.sendReminder(ctx, addon); err != nil {
			slog.Error("device addon renewal: send reminder failed", "telegram_id", addon.TelegramID, "addon_id", addon.ID, "error", err)
			continue
		}
		sent++
	}
	slog.Info("device addon renewal: reminders sent", "count", sent, "candidates", len(addons))
	return nil
}

func (s *DeviceAddonRenewalService) sendReminder(ctx context.Context, addon *database.DeviceAddon) error {
	amount := float64(addon.DeviceCount) * float64(config.DeviceSlotPriceRUB())
	paymentResp, err := s.rollypayClient.CreatePayment(ctx, rollypay.CreatePaymentRequest{
		Amount:      fmt.Sprintf("%.2f", amount),
		OrderID:     fmt.Sprintf("addon-%d", addon.ID),
		Description: fmt.Sprintf("Device addon renewal x%d", addon.DeviceCount),
		Test:        config.RollyPayTestMode() && addon.TelegramID == config.GetAdminTelegramId(),
	})
	if err != nil {
		return fmt.Errorf("create rollypay payment: %w", err)
	}

	// langCode "" falls back to the default language, matching rollypay/webhook.go's
	// notifyUserKey — this cron only has the addon row, not the customer's stored preference.
	text := fmt.Sprintf(s.tm.GetText("", "device_addon_renewal_reminder"), addon.DeviceCount, int(amount))
	_, sendErr := s.telegramBot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    addon.TelegramID,
		Text:      text,
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: s.tm.GetButton("", "pay_button").Text, URL: paymentResp.PayURL}},
		}},
	})
	s.logNotification(ctx, addon.TelegramID, sendErr)
	return sendErr
}

func (s *DeviceAddonRenewalService) logNotification(ctx context.Context, telegramID int64, sendErr error) {
	if s.notificationLogRepository == nil {
		return
	}
	status := "sent"
	var errMsg *string
	if sendErr != nil {
		status = "failed"
		m := sendErr.Error()
		errMsg = &m
	}
	if err := s.notificationLogRepository.Create(ctx, database.NotificationLog{
		CustomerTelegramID: telegramID,
		NotificationType:   deviceAddonRenewalNotificationType,
		Status:             status,
		ErrorMessage:       errMsg,
		Source:             "system",
	}); err != nil {
		slog.Error("device addon renewal: write notification_log", "telegram_id", telegramID, "error", err)
	}
}

func (s *DeviceAddonRenewalService) alreadySentToday(ctx context.Context, telegramID int64) bool {
	if s.notificationLogRepository == nil {
		return false
	}
	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	sent, err := s.notificationLogRepository.HasSentSince(ctx, telegramID, deviceAddonRenewalNotificationType, dayStart)
	if err != nil {
		slog.Error("device addon renewal: check already-sent-today failed, sending anyway", "telegram_id", telegramID, "error", err)
		return false
	}
	return sent
}
