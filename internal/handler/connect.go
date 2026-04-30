package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"log/slog"

	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/translation"
	"remnawave-tg-shop-bot/utils"
)

func (h Handler) ConnectCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	customer, err := h.customerRepository.FindByTelegramId(ctx, update.Message.Chat.ID)
	if err != nil {
		slog.Error("Error finding customer", "error", err)
		return
	}
	if customer == nil {
		slog.Error("customer not exist", "telegramId", utils.MaskHalfInt64(update.Message.Chat.ID), "error", err)
		return
	}

	langCode := update.Message.From.LanguageCode

	var resetStrategy string
	var lastResetAt *time.Time
	if rwUsers, rwErr := h.remnawaveClient.GetUsersByTelegramID(ctx, customer.TelegramID); rwErr == nil && len(rwUsers) > 0 {
		resetStrategy = rwUsers[0].TrafficLimitStrategy
		lastResetAt = rwUsers[0].LastTrafficResetAt
	}

	bd := h.translation.GetButton(langCode, "connect_button")
	var markup [][]models.InlineKeyboardButton
	if config.GetMiniAppURL() != "" {
		markup = append(markup, []models.InlineKeyboardButton{bd.InlineWebApp(config.GetMiniAppURL())})
	} else if config.IsWepAppLinkEnabled() {
		if customer.SubscriptionLink != nil && customer.ExpireAt != nil && customer.ExpireAt.After(time.Now()) {
			markup = append(markup, []models.InlineKeyboardButton{bd.InlineWebApp(*customer.SubscriptionLink)})
		}
	}
	markup = append(markup, []models.InlineKeyboardButton{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)})

	isDisabled := true
	_, err = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    update.Message.Chat.ID,
		Text:      buildConnectText(customer, langCode, resetStrategy, lastResetAt),
		ParseMode: models.ParseModeHTML,
		LinkPreviewOptions: &models.LinkPreviewOptions{
			IsDisabled: &isDisabled,
		},
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: markup,
		},
	})
	if err != nil {
		slog.Error("Error sending connect message", "error", err)
	}
}

func (h Handler) ConnectCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	callback := update.CallbackQuery.Message.Message

	customer, err := h.customerRepository.FindByTelegramId(ctx, callback.Chat.ID)
	if err != nil {
		slog.Error("Error finding customer", "error", err)
		return
	}
	if customer == nil {
		slog.Error("customer not exist", "telegramId", utils.MaskHalfInt64(callback.Chat.ID), "error", err)
		return
	}

	langCode := update.CallbackQuery.From.LanguageCode

	var resetStrategy string
	var lastResetAt *time.Time
	if rwUsers, rwErr := h.remnawaveClient.GetUsersByTelegramID(ctx, customer.TelegramID); rwErr == nil && len(rwUsers) > 0 {
		resetStrategy = rwUsers[0].TrafficLimitStrategy
		lastResetAt = rwUsers[0].LastTrafficResetAt
	}

	cbd := h.translation.GetButton(langCode, "connect_button")
	var markup [][]models.InlineKeyboardButton
	if config.GetMiniAppURL() != "" {
		markup = append(markup, []models.InlineKeyboardButton{cbd.InlineWebApp(config.GetMiniAppURL())})
	} else if config.IsWepAppLinkEnabled() {
		if customer.SubscriptionLink != nil && customer.ExpireAt != nil && customer.ExpireAt.After(time.Now()) {
			markup = append(markup, []models.InlineKeyboardButton{cbd.InlineWebApp(*customer.SubscriptionLink)})
		}
	}
	markup = append(markup, []models.InlineKeyboardButton{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)})

	isDisabled := true
	_, err = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    callback.Chat.ID,
		MessageID: callback.ID,
		ParseMode: models.ParseModeHTML,
		Text:      buildConnectText(customer, langCode, resetStrategy, lastResetAt),
		LinkPreviewOptions: &models.LinkPreviewOptions{
			IsDisabled: &isDisabled,
		},
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: markup,
		},
	})
	if err != nil {
		slog.Error("Error sending connect message", "error", err)
	}
}

func buildConnectText(customer *database.Customer, langCode string, resetStrategy string, lastResetAt *time.Time) string {
	var info strings.Builder
	tm := translation.GetInstance()

	if customer.ExpireAt != nil && time.Now().Before(*customer.ExpireAt) {
		formattedDate := customer.ExpireAt.Format("02.01.2006")
		info.WriteString(fmt.Sprintf(tm.GetText(langCode, "subscription_active"), formattedDate))

		if nextReset := calcNextReset(resetStrategy, lastResetAt); nextReset != nil {
			info.WriteString(fmt.Sprintf("\n🔄 Сброс трафика: <b>%s</b>", nextReset.Format("02.01.2006")))
		}

		if customer.SubscriptionLink != nil && *customer.SubscriptionLink != "" {
			if config.GetMiniAppURL() == "" && !config.IsWepAppLinkEnabled() {
				info.WriteString(fmt.Sprintf(tm.GetText(langCode, "subscription_link"), *customer.SubscriptionLink))
			}
		}
	} else if customer.ExpireAt != nil {
		info.WriteString(tm.GetText(langCode, "subscription_expired"))
	} else {
		info.WriteString(tm.GetText(langCode, "no_subscription"))
	}

	return info.String()
}

// calcNextReset returns the next traffic reset date based on strategy and last reset time.
func calcNextReset(strategy string, lastResetAt *time.Time) *time.Time {
	now := time.Now().UTC()
	switch strings.ToUpper(strategy) {
	case "MONTH":
		next := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		return &next
	case "MONTH_ROLLING":
		if lastResetAt == nil {
			return nil
		}
		next := lastResetAt.AddDate(0, 1, 0)
		return &next
	case "WEEK":
		if lastResetAt == nil {
			return nil
		}
		next := lastResetAt.AddDate(0, 0, 7)
		return &next
	case "DAY":
		next := now.AddDate(0, 0, 1)
		return &next
	case "NO_RESET":
		return nil
	}
	return nil
}
