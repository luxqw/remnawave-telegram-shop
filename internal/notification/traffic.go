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
		if float64(u.UserTraffic.UsedTrafficBytes)/float64(u.TrafficLimitBytes) < 0.9 {
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
		notified++
		time.Sleep(40 * time.Millisecond)
	}
	slog.Info("traffic warning: check complete", "notified", notified)
	return nil
}
