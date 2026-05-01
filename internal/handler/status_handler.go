package handler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/translation"
)

func (h Handler) StatusCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	telegramID := update.CallbackQuery.From.ID
	langCode := update.CallbackQuery.From.LanguageCode
	msg := update.CallbackQuery.Message.Message

	customer, err := h.customerRepository.FindByTelegramId(ctx, telegramID)
	if err != nil || customer == nil {
		return
	}

	var resetStrategy string
	var lastResetAt *time.Time
	var usedBytes, limitBytes int

	rwCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if rwUsers, rwErr := h.remnawaveClient.GetUsersByTelegramID(rwCtx, telegramID); rwErr == nil && len(rwUsers) > 0 {
		rw := rwUsers[0]
		resetStrategy = rw.TrafficLimitStrategy
		lastResetAt = rw.LastTrafficResetAt
		limitBytes = rw.TrafficLimitBytes
		if rw.UserTraffic != nil {
			usedBytes = rw.UserTraffic.UsedTrafficBytes
		}
	}

	if _, err = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID: msg.Chat.ID, MessageID: msg.ID, ParseMode: models.ParseModeHTML,
		Text: buildStatusText(customer, langCode, resetStrategy, lastResetAt, usedBytes, limitBytes),
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
			{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)},
		}},
	}); err != nil {
		slog.Warn("status: failed to edit message", "error", err, "telegram_id", telegramID)
	}
}

func buildStatusText(customer *database.Customer, langCode string, resetStrategy string, lastResetAt *time.Time, usedBytes, limitBytes int) string {
	tm := translation.GetInstance()
	var sb strings.Builder

	sb.WriteString(tm.GetText(langCode, "status_header"))

	if customer.SubscriptionLink != nil && customer.ExpireAt != nil && customer.ExpireAt.After(time.Now()) {
		sb.WriteString(fmt.Sprintf(tm.GetText(langCode, "subscription_active"), customer.ExpireAt.Format("02.01.2006")))
		if nextReset := calcNextReset(resetStrategy, lastResetAt); nextReset != nil {
			sb.WriteString("\n" + fmt.Sprintf(tm.GetText(langCode, "next_traffic_reset"), nextReset.Format("02.01.2006")))
		} else if strings.ToUpper(resetStrategy) == "NO_RESET" {
			sb.WriteString("\n" + tm.GetText(langCode, "traffic_no_reset"))
		}
		if limitBytes > 0 {
			usedGB := float64(usedBytes) / (1024 * 1024 * 1024)
			limitGB := float64(limitBytes) / (1024 * 1024 * 1024)
			sb.WriteString("\n" + fmt.Sprintf(tm.GetText(langCode, "traffic_used"), usedGB, limitGB))
		}
	} else if customer.ExpireAt != nil {
		sb.WriteString(tm.GetText(langCode, "subscription_expired"))
	} else {
		sb.WriteString(tm.GetText(langCode, "no_subscription"))
	}

	return sb.String()
}
