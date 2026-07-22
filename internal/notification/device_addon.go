package notification

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/google/uuid"

	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/remnawave"
	"remnawave-tg-shop-bot/internal/rollypay"
	"remnawave-tg-shop-bot/internal/translation"
)

// deviceAddonGracePeriod is decision 5's "~1 day soft grace" between a device addon's cycle
// lapsing and its device limit actually shrinking.
const deviceAddonGracePeriod = 24 * time.Hour

const (
	deviceAddonRenewalNotificationType = "device_addon_renewal"
	deviceAddonGraceNotificationType   = "device_addon_grace"
)

type deviceAddonRepository interface {
	FindActiveExpiringBefore(ctx context.Context, cutoff time.Time) ([]*database.DeviceAddon, error)
	FindGraceExpiredBefore(ctx context.Context, cutoff time.Time) ([]*database.DeviceAddon, error)
	MarkGrace(ctx context.Context, id int64, graceUntil time.Time) error
	MarkExpired(ctx context.Context, id int64) error
}

type rollyPayCreator interface {
	CreatePayment(ctx context.Context, req rollypay.CreatePaymentRequest) (*rollypay.Payment, error)
}

// remnawaveDeviceClient is the narrow slice of remnawave.Client this service needs to shrink a
// customer's device limit and trim their oldest connected device(s) once a grace period lapses.
type remnawaveDeviceClient interface {
	GetUsersByTelegramID(ctx context.Context, telegramID int64) ([]remnawave.User, error)
	UpdateUserDeviceLimit(ctx context.Context, userUUID uuid.UUID, newLimit int) error
	GetUserHwidDevices(ctx context.Context, userUUID uuid.UUID) ([]remnawave.HwidDevice, error)
	DeleteUserHwidDevice(ctx context.Context, userUUID uuid.UUID, hwid string) error
}

// DeviceAddonRenewalService runs the full lifecycle of a standalone-billed device addon (Tribute-
// linked customers, whose subscription charge can't include a variable device cost — see decision
// 3 of the device-addon plan): reminding before its own cycle lapses, a grace window with one more
// pay-link nudge, and finally shrinking the Remnawave device limit (trimming the oldest connected
// device first) if grace lapses unpaid too. Bundled addons never reach the grace/expiry path here
// in practice — their cycle stays synced to the subscription's own expiry, which is enforced by
// SubscriptionService/Remnawave directly — but the transition logic itself is billing-mode-agnostic
// since it only reads cycle_expires_at/grace_until/status.
type DeviceAddonRenewalService struct {
	deviceAddonRepository     deviceAddonRepository
	rollypayClient            rollyPayCreator
	remnawaveClient           remnawaveDeviceClient
	telegramBot               *bot.Bot
	tm                        *translation.Manager
	notificationLogRepository *database.NotificationLogRepository
}

func NewDeviceAddonRenewalService(
	deviceAddonRepository *database.DeviceAddonRepository,
	rollypayClient *rollypay.Client,
	remnawaveClient *remnawave.Client,
	telegramBot *bot.Bot,
	tm *translation.Manager,
	notificationLogRepository *database.NotificationLogRepository,
) *DeviceAddonRenewalService {
	return &DeviceAddonRenewalService{
		deviceAddonRepository:     deviceAddonRepository,
		rollypayClient:            rollypayClient,
		remnawaveClient:           remnawaveClient,
		telegramBot:               telegramBot,
		tm:                        tm,
		notificationLogRepository: notificationLogRepository,
	}
}

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
		if s.alreadySentToday(ctx, addon.TelegramID, deviceAddonRenewalNotificationType) {
			continue
		}
		if err := s.sendRenewalPayLink(ctx, addon, "device_addon_renewal_reminder", deviceAddonRenewalNotificationType); err != nil {
			slog.Error("device addon renewal: send reminder failed", "telegram_id", addon.TelegramID, "addon_id", addon.ID, "error", err)
			continue
		}
		sent++
	}
	slog.Info("device addon renewal: reminders sent", "count", sent, "candidates", len(addons))
	return nil
}

// ProcessGraceAndExpiry runs decisions 5 and 6: active addons whose cycle has already lapsed move
// into a ~1 day grace window with one more pay-link nudge; addons still unpaid once grace itself
// lapses are marked expired and have their Remnawave device limit shrunk back down, trimming the
// oldest currently-connected device(s) first if the customer is still over the new limit.
func (s *DeviceAddonRenewalService) ProcessGraceAndExpiry() error {
	ctx := context.Background()
	if err := s.enterGrace(ctx); err != nil {
		return err
	}
	return s.expireGraced(ctx)
}

// enterGrace only acts on standalone addons. Bundled addons' cycle_expires_at is kept synced to
// customer.ExpireAt and pushed forward by payment.PaymentService.ProcessPurchaseById on every
// subscription renewal (decision 2) — they never carry an independent charge, so there is nothing
// for this cron to remind or grace-gate separately. If a bundled customer's subscription lapses,
// that is already handled by the existing subscription-expiry reminder/enforcement path; sending a
// standalone device-addon pay link on top of it would charge them the exact per-customer variable
// fee decision 3 says bundled (RollyPay) customers must never see.
func (s *DeviceAddonRenewalService) enterGrace(ctx context.Context) error {
	lapsed, err := s.deviceAddonRepository.FindActiveExpiringBefore(ctx, time.Now())
	if err != nil {
		return fmt.Errorf("find lapsed device addons: %w", err)
	}
	for _, addon := range lapsed {
		if addon.BillingMode != database.AddonBillingModeStandalone {
			continue
		}
		graceUntil := addon.CycleExpiresAt.Add(deviceAddonGracePeriod)
		if err := s.deviceAddonRepository.MarkGrace(ctx, addon.ID, graceUntil); err != nil {
			slog.Error("device addon grace: mark grace failed", "addon_id", addon.ID, "error", err)
			continue
		}
		if s.alreadySentToday(ctx, addon.TelegramID, deviceAddonGraceNotificationType) {
			continue
		}
		if err := s.sendRenewalPayLink(ctx, addon, "device_addon_grace_started", deviceAddonGraceNotificationType); err != nil {
			slog.Error("device addon grace: notify failed", "telegram_id", addon.TelegramID, "addon_id", addon.ID, "error", err)
		}
	}
	return nil
}

func (s *DeviceAddonRenewalService) expireGraced(ctx context.Context) error {
	graced, err := s.deviceAddonRepository.FindGraceExpiredBefore(ctx, time.Now())
	if err != nil {
		return fmt.Errorf("find grace-expired device addons: %w", err)
	}
	for _, addon := range graced {
		s.expireAndShrink(ctx, addon)
	}
	return nil
}

func (s *DeviceAddonRenewalService) expireAndShrink(ctx context.Context, addon *database.DeviceAddon) {
	// Defense in depth: enterGrace no longer marks bundled addons as grace, so this should be
	// unreachable for BillingMode == bundled — guarded anyway in case a row was marked grace by an
	// older build before this fix shipped. Trimming a bundled customer's devices here would be
	// wrong regardless: their device limit is governed by the subscription's own expiry, not by
	// this addon-specific cron.
	if addon.BillingMode != database.AddonBillingModeStandalone {
		slog.Warn("device addon expiry: skipping non-standalone addon reached via grace expiry", "addon_id", addon.ID, "billing_mode", addon.BillingMode)
		return
	}
	if err := s.deviceAddonRepository.MarkExpired(ctx, addon.ID); err != nil {
		slog.Error("device addon expiry: mark expired failed", "addon_id", addon.ID, "error", err)
		return
	}

	rwUsers, err := s.remnawaveClient.GetUsersByTelegramID(ctx, addon.TelegramID)
	if err != nil || len(rwUsers) == 0 {
		slog.Error("device addon expiry: find remnawave user failed", "telegram_id", addon.TelegramID, "error", err)
		return
	}
	rwUser := rwUsers[0]

	currentLimit := 0
	if rwUser.HwidDeviceLimit != nil {
		currentLimit = *rwUser.HwidDeviceLimit
	}
	targetLimit := currentLimit - addon.DeviceCount
	if targetLimit < 0 {
		targetLimit = 0
	}

	if err := s.remnawaveClient.UpdateUserDeviceLimit(ctx, rwUser.UUID, targetLimit); err != nil {
		slog.Error("device addon expiry: shrink device limit failed", "telegram_id", addon.TelegramID, "error", err)
		return
	}

	trimmed := s.trimExcessDevices(ctx, rwUser.UUID, targetLimit)

	// telegramBot/tm nil-guarded like rollypay/webhook.go's notifyUserKey — both are optional here
	// (unit tests construct this service without them).
	if s.telegramBot != nil && s.tm != nil {
		text := fmt.Sprintf(s.tm.GetText("", "device_addon_expired"), addon.DeviceCount)
		if _, sendErr := s.telegramBot.SendMessage(ctx, &bot.SendMessageParams{ChatID: addon.TelegramID, Text: text, ParseMode: models.ParseModeHTML}); sendErr != nil {
			slog.Error("device addon expiry: notify user failed", "telegram_id", addon.TelegramID, "error", sendErr)
		}
	}
	slog.Info("device addon expiry: shrunk device limit", "telegram_id", addon.TelegramID, "addon_id", addon.ID, "new_limit", targetLimit, "trimmed_devices", trimmed)
}

// trimExcessDevices deletes the oldest-by-last-used devices (decision 6) until the connected
// count is within targetLimit, returning how many were removed.
func (s *DeviceAddonRenewalService) trimExcessDevices(ctx context.Context, userUUID uuid.UUID, targetLimit int) int {
	devices, err := s.remnawaveClient.GetUserHwidDevices(ctx, userUUID)
	if err != nil {
		slog.Error("device addon expiry: list hwid devices failed", "user_uuid", userUUID, "error", err)
		return 0
	}
	if len(devices) <= targetLimit {
		return 0
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].UpdatedAt.Before(devices[j].UpdatedAt) })

	excess := len(devices) - targetLimit
	removed := 0
	for i := 0; i < excess; i++ {
		if err := s.remnawaveClient.DeleteUserHwidDevice(ctx, userUUID, devices[i].Hwid); err != nil {
			slog.Error("device addon expiry: delete hwid device failed", "user_uuid", userUUID, "hwid", devices[i].Hwid, "error", err)
			continue
		}
		removed++
	}
	return removed
}

// sendRenewalPayLink creates a fresh RollyPay renewal invoice for addon and sends it with the
// given translation key — shared by the pre-expiry reminder and the grace-entry nudge, which
// differ only in urgency wording and which notification_type they dedup against.
func (s *DeviceAddonRenewalService) sendRenewalPayLink(ctx context.Context, addon *database.DeviceAddon, translationKey, notificationType string) error {
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
	text := fmt.Sprintf(s.tm.GetText("", translationKey), addon.DeviceCount, int(amount))
	_, sendErr := s.telegramBot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    addon.TelegramID,
		Text:      text,
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: s.tm.GetButton("", "pay_button").Text, URL: paymentResp.PayURL}},
		}},
	})
	s.logNotification(ctx, addon.TelegramID, notificationType, sendErr)
	return sendErr
}

func (s *DeviceAddonRenewalService) logNotification(ctx context.Context, telegramID int64, notificationType string, sendErr error) {
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
		NotificationType:   notificationType,
		Status:             status,
		ErrorMessage:       errMsg,
		Source:             "system",
	}); err != nil {
		slog.Error("device addon: write notification_log", "notification_type", notificationType, "telegram_id", telegramID, "error", err)
	}
}

func (s *DeviceAddonRenewalService) alreadySentToday(ctx context.Context, telegramID int64, notificationType string) bool {
	if s.notificationLogRepository == nil {
		return false
	}
	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	sent, err := s.notificationLogRepository.HasSentSince(ctx, telegramID, notificationType, dayStart)
	if err != nil {
		slog.Error("device addon: check already-sent-today failed, sending anyway", "notification_type", notificationType, "telegram_id", telegramID, "error", err)
		return false
	}
	return sent
}
