package notification

import (
	"context"
	"fmt"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"log/slog"
	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/handler"
	"remnawave-tg-shop-bot/internal/translation"
	"time"
)

// tributeAutorenewStreakCap is decision 9's safety cap: after this many consecutive cron-driven
// "optimistic" renewals with no fresh genuine Tribute webhook in between (reset in
// tribute.Client.newSubscriptionHandler), ProcessSubscriptionExpiration stops auto-extending a
// customer's access and alerts the admin instead — there's no way to ask Tribute's API whether the
// subscription is still genuinely paid, so this bounds how long a lost cancellation webhook could
// grant free access for.
const tributeAutorenewStreakCap = 3

type customerRepository interface {
	FindByExpirationRange(ctx context.Context, startDate, endDate time.Time) (*[]database.Customer, error)
	UpdateFields(ctx context.Context, id int64, updates map[string]interface{}) error
}

type tributeRepository interface {
	FindLatestActiveTributesByCustomerIDs(ctx context.Context, customerIDs []int64) (*[]database.Purchase, error)
}

type paymentProcessor interface {
	CreatePurchase(ctx context.Context, amount float64, months int, customer *database.Customer, invoiceType database.InvoiceType) (string, int64, float64, error)
	ProcessPurchaseById(ctx context.Context, purchaseId int64) error
}

type SubscriptionService struct {
	customerRepository        customerRepository
	purchaseRepository        tributeRepository
	paymentService            paymentProcessor
	telegramBot               *bot.Bot
	tm                        *translation.Manager
	notificationLogRepository *database.NotificationLogRepository
	notify                    func(context.Context, database.Customer) error
}

func NewSubscriptionService(customerRepository customerRepository,
	purchaseRepository tributeRepository,
	paymentService paymentProcessor,
	telegramBot *bot.Bot,
	tm *translation.Manager,
	notificationLogRepository *database.NotificationLogRepository) *SubscriptionService {
	svc := &SubscriptionService{customerRepository: customerRepository, purchaseRepository: purchaseRepository, paymentService: paymentService, telegramBot: telegramBot, tm: tm, notificationLogRepository: notificationLogRepository}
	svc.notify = svc.sendNotification
	return svc
}

// logNotification writes a best-effort notification_log row. A failure to write must never block
// or fail the actual notification send — only logged via slog.Error, never returned.
func (s *SubscriptionService) logNotification(ctx context.Context, customer database.Customer, notificationType, status string, sendErr error) {
	if s.notificationLogRepository == nil {
		return
	}
	var errMsg *string
	if sendErr != nil {
		m := sendErr.Error()
		errMsg = &m
	}
	if err := s.notificationLogRepository.Create(ctx, database.NotificationLog{
		CustomerTelegramID: customer.TelegramID,
		NotificationType:   notificationType,
		Status:             status,
		ErrorMessage:       errMsg,
		Source:             "system",
	}); err != nil {
		slog.Error("notification: write notification_log", "notification_type", notificationType, "customer_id", customer.TelegramID, "error", err)
	}
}

// alreadySentToday mirrors logNotification's nil-guard: notificationLogRepository is nil in unit
// tests that don't exercise logging, and dedup must fail open (never sent) rather than panic.
func (s *SubscriptionService) alreadySentToday(ctx context.Context, telegramID int64, notificationType string, since time.Time) bool {
	if s.notificationLogRepository == nil {
		return false
	}
	sent, err := s.notificationLogRepository.HasSentSince(ctx, telegramID, notificationType, since)
	if err != nil {
		slog.Error("notification: check already-sent-today failed, sending anyway", "notification_type", notificationType, "customer_id", telegramID, "error", err)
		return false
	}
	return sent
}

func (s *SubscriptionService) ProcessSubscriptionExpiration() error {
	ctx := context.Background()
	customers, err := s.getCustomersWithExpiringSubscriptions()
	if err != nil {
		slog.Error("Failed to get customers with expiring subscriptions", "error", err)
		return err
	}
	slog.Info(fmt.Sprintf("Found %d customers with expiring subscriptions", len(*customers)))
	if len(*customers) == 0 {
		return nil
	}
	now := time.Now()
	customersIds := make([]int64, len(*customers))
	for i, customer := range *customers {
		customersIds[i] = customer.ID
	}
	latestActiveTributes, err := s.purchaseRepository.FindLatestActiveTributesByCustomerIDs(ctx, customersIds)
	if err != nil {
		slog.Error("Failed to query tribute purchases", "error", err)
		return err
	}
	customerIdTributes := make(map[int64]*database.Purchase, len(*latestActiveTributes))
	for i := range *latestActiveTributes {
		p := &(*latestActiveTributes)[i]
		customerIdTributes[p.CustomerID] = p
	}
	tributesProcessed := make(map[int64]bool, len(*latestActiveTributes))

	// dayStart bounds the dedup check below to "already sent today" — this cron runs every 4
	// hours, and without this guard a customer sitting in the [-1,+3]-day expiration window would
	// get a fresh reminder on every tick (up to ~6/day) instead of once per calendar day.
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	for _, customer := range *customers {
		daysUntilExpiration := s.getDaysUntilExpiration(now, *customer.ExpireAt)

		if p, ok := customerIdTributes[customer.ID]; ok {
			if daysUntilExpiration > 1 {
				continue
			}
			if customer.TributeAutorenewPaused {
				slog.Info("Tribute optimistic renewal skipped: paused by admin", "customer_id", customer.ID)
				continue
			}
			if customer.TributeAutorenewStreak >= tributeAutorenewStreakCap {
				slog.Warn("Tribute optimistic renewal streak cap reached, alerting admin", "customer_id", customer.ID, "streak", customer.TributeAutorenewStreak)
				s.notifyAdminStreakCap(ctx, customer)
				continue
			}
			_, purchaseId, _, err := s.paymentService.CreatePurchase(ctx, p.Amount, p.Month, &customer, database.InvoiceTypeTribute)
			if err != nil {
				slog.Error("Failed to create tribute purchase", "error", err)
				continue
			}
			err = s.paymentService.ProcessPurchaseById(ctx, purchaseId)
			if err != nil {
				slog.Error("Failed to process tribute purchase", "error", err)
				continue
			}
			if streakErr := s.customerRepository.UpdateFields(ctx, customer.ID, map[string]interface{}{
				"tribute_autorenew_streak": customer.TributeAutorenewStreak + 1,
			}); streakErr != nil {
				slog.Error("Failed to increment tribute autorenew streak", "customer_id", customer.ID, "error", streakErr)
			}
			slog.Info("Tribute purchase processed successfully", "purchase_id", purchaseId)
			tributesProcessed[customer.ID] = true
		}
		if _, ok := tributesProcessed[customer.ID]; ok {
			continue
		}

		if customer.IsTrial {
			if daysUntilExpiration <= 1 && !s.alreadySentToday(ctx, customer.TelegramID, "trial_expiring", dayStart) {
				_ = s.sendTrialExpiringNotification(ctx, customer)
			}
			continue
		}

		if s.alreadySentToday(ctx, customer.TelegramID, "subscription_expiring", dayStart) {
			continue
		}

		send := s.notify
		if send == nil {
			send = s.sendNotification
		}
		err := send(ctx, customer)
		if err != nil {
			slog.Error("Failed to send notification", "customer_id", customer.ID, "days_until_expiration", daysUntilExpiration, "error", err)
			continue
		}
		slog.Info("Notification sent successfully", "customer_id", customer.ID, "days_until_expiration", daysUntilExpiration)
	}

	slog.Info(fmt.Sprintf("Processed tributes customers %d with expiring subscriptions", len(tributesProcessed)))
	slog.Info(fmt.Sprintf("Sent notifications to %d customers with expiring subscriptions", len(*customers)-len(tributesProcessed)))
	return nil
}

// notifyAdminStreakCap alerts the admin once a customer's optimistic Tribute renewal streak trips
// the cap — mirrors tribute.Client.notifyAdmin (not reused directly: this package would otherwise
// need to import internal/tribute, which imports internal/payment, which is already imported
// widely enough here that pulling in tribute risks a cycle for four lines of code).
func (s *SubscriptionService) notifyAdminStreakCap(ctx context.Context, customer database.Customer) {
	if s.telegramBot == nil {
		return
	}
	text := fmt.Sprintf(
		"⚠️ Tribute customer %d has been auto-renewed %d times with no fresh webhook — auto-renewal stopped, please verify manually.",
		customer.TelegramID, customer.TributeAutorenewStreak,
	)
	if _, err := s.telegramBot.SendMessage(ctx, &bot.SendMessageParams{ChatID: config.GetAdminTelegramId(), Text: text}); err != nil {
		slog.Error("Failed to notify admin about tribute autorenew streak cap", "customer_id", customer.ID, "error", err)
	}
}

func (s *SubscriptionService) getCustomersWithExpiringSubscriptions() (*[]database.Customer, error) {
	now := time.Now()
	startDate := now.AddDate(0, 0, -1)
	endDate := now.AddDate(0, 0, 3)
	dbCustomers, err := s.customerRepository.FindByExpirationRange(context.Background(), startDate, endDate)
	if err != nil {
		return nil, err
	}
	return dbCustomers, nil
}

func (s *SubscriptionService) getDaysUntilExpiration(now time.Time, expireAt time.Time) int {
	nowDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	expireDate := time.Date(expireAt.Year(), expireAt.Month(), expireAt.Day(), 0, 0, 0, 0, expireAt.Location())
	duration := expireDate.Sub(nowDate)
	return int(duration.Hours() / 24)
}

func (s *SubscriptionService) sendTrialExpiringNotification(ctx context.Context, customer database.Customer) error {
	expireDate := customer.ExpireAt.Format("02.01.2006")
	text := fmt.Sprintf(s.tm.GetText(customer.Language, "trial_expiring"), expireDate)
	_, err := s.telegramBot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    customer.TelegramID,
		Text:      text,
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{s.tm.GetButton(customer.Language, "buy_button").InlineCallback(handler.CallbackBuy)},
			},
		},
	})
	if err != nil {
		s.logNotification(ctx, customer, "trial_expiring", "failed", err)
	} else {
		s.logNotification(ctx, customer, "trial_expiring", "sent", nil)
	}
	return err
}

// ResendNotification dispatches a manual admin-triggered resend for one customer, reusing the
// same send paths ProcessSubscriptionExpiration uses (which already write their own
// notification_log row on completion — callers must not log a second row). Only
// "trial_expiring" and "subscription_expiring" are supported here; other notification types
// (traffic_warning lives on TrafficWarningService, broadcast/admin_message have no stored
// message text to resend) are the caller's responsibility to reject before calling this.
func (s *SubscriptionService) ResendNotification(ctx context.Context, customer database.Customer, notificationType string) error {
	switch notificationType {
	case "trial_expiring":
		return s.sendTrialExpiringNotification(ctx, customer)
	case "subscription_expiring":
		return s.sendNotification(ctx, customer)
	default:
		return fmt.Errorf("resend not supported for notification type %q", notificationType)
	}
}

func (s *SubscriptionService) sendNotification(ctx context.Context, customer database.Customer) error {
	expireDate := customer.ExpireAt.Format("02.01.2006")
	messageText := fmt.Sprintf(s.tm.GetText(customer.Language, "subscription_expiring"), expireDate)
	_, err := s.telegramBot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    customer.TelegramID,
		Text:      messageText,
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{s.tm.GetButton(customer.Language, "renew_subscription_button").InlineCallback(handler.CallbackBuy)},
			},
		},
	})
	if err != nil {
		s.logNotification(ctx, customer, "subscription_expiring", "failed", err)
	} else {
		s.logNotification(ctx, customer, "subscription_expiring", "sent", nil)
	}
	return err
}
