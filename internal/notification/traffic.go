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
	"remnawave-tg-shop-bot/internal/remnawave"
	"remnawave-tg-shop-bot/internal/translation"
)

type TrafficWarningService struct {
	customerRepository *database.CustomerRepository
	remnawaveClient    *remnawave.Client
	telegramBot        *bot.Bot
	tm                 *translation.Manager
}

func NewTrafficWarningService(
	customerRepository *database.CustomerRepository,
	remnawaveClient *remnawave.Client,
	telegramBot *bot.Bot,
	tm *translation.Manager,
) *TrafficWarningService {
	return &TrafficWarningService{customerRepository: customerRepository, remnawaveClient: remnawaveClient, telegramBot: telegramBot, tm: tm}
}

// CheckAndNotify sends a low-traffic warning to users who have used >90% of their limit.
// It will not send more than once per traffic reset period.
func (s *TrafficWarningService) CheckAndNotify() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	customers, err := s.customerRepository.FindAll(ctx)
	if err != nil {
		return fmt.Errorf("traffic warning: get customers: %w", err)
	}

	now := time.Now()
	notified := 0

	for _, customer := range customers {
		if customer.ExpireAt == nil || customer.ExpireAt.Before(now) || customer.IsTrial {
			continue
		}

		rwUsers, err := s.remnawaveClient.GetUsersByTelegramID(ctx, customer.TelegramID)
		if err != nil || len(rwUsers) == 0 {
			continue
		}
		u := rwUsers[0]

		if u.TrafficLimitBytes == 0 || u.UserTraffic == nil || u.UserTraffic.UsedTrafficBytes == 0 {
			continue
		}

		usedPct := float64(u.UserTraffic.UsedTrafficBytes) / float64(u.TrafficLimitBytes)
		if usedPct < 0.9 {
			continue
		}

		// Deduplication: don't send if we already warned since the last traffic reset.
		// If Remnawave provides lastTrafficResetAt, use it as the boundary.
		// Otherwise fall back to checking if warning was sent in the last 30 days.
		if alreadyWarnedThisPeriod(customer, u) {
			continue
		}

		remainingGB := (u.TrafficLimitBytes - u.UserTraffic.UsedTrafficBytes) / config.BytesInGigabyte()
		totalGB := u.TrafficLimitBytes / config.BytesInGigabyte()
		text := fmt.Sprintf(s.tm.GetText(customer.Language, "traffic_warning"), remainingGB, totalGB)

		var rows [][]models.InlineKeyboardButton
		if config.TopupEnabled() {
			rows = append(rows, []models.InlineKeyboardButton{s.tm.GetButton(customer.Language, "topup_button").InlineCallback("topup")})
		}

		_, err = s.telegramBot.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: customer.TelegramID, Text: text, ParseMode: models.ParseModeHTML,
			ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: rows},
		})
		if err != nil {
			slog.Warn("traffic warning: send failed", "telegram_id", customer.TelegramID, "error", err)
			continue
		}

		// Record that we warned this user
		_ = s.customerRepository.UpdateFields(ctx, customer.ID, map[string]interface{}{
			"last_traffic_warning_at": now,
		})

		notified++
		time.Sleep(40 * time.Millisecond)
	}

	slog.Info("traffic warning: check complete", "notified", notified)
	return nil
}

// alreadyWarnedThisPeriod returns true if a warning was already sent since the last traffic reset.
func alreadyWarnedThisPeriod(customer database.Customer, u remnawave.User) bool {
	if customer.LastTrafficWarningAt == nil {
		return false
	}
	warned := *customer.LastTrafficWarningAt

	// If Remnawave provides the last reset time, use it as the period boundary.
	if u.LastTrafficResetAt != nil && !u.LastTrafficResetAt.IsZero() {
		return warned.After(*u.LastTrafficResetAt)
	}

	// Fallback: don't send more than once every 30 days if reset time is unknown.
	return time.Since(warned) < 30*24*time.Hour
}