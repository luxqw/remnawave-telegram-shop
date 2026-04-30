package handler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remnawave-tg-shop-bot/internal/config"
)

func (h Handler) DevicesCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	telegramID := update.CallbackQuery.From.ID
	langCode := update.CallbackQuery.From.LanguageCode
	msg := update.CallbackQuery.Message.Message

	customer, err := h.customerRepository.FindByTelegramId(ctx, telegramID)
	if err != nil || customer == nil || customer.SubscriptionLink == nil || customer.ExpireAt == nil {
		return
	}

	expireDate := customer.ExpireAt.Format("02.01.2006")
	var text string
	var keyboard [][]models.InlineKeyboardButton

	if config.GetMiniAppURL() != "" {
		text = fmt.Sprintf(h.translation.GetText(langCode, "devices_info_webapp"), expireDate)
		keyboard = [][]models.InlineKeyboardButton{
			{h.translation.GetButton(langCode, "connect_button").InlineWebApp(config.GetMiniAppURL())},
			{h.translation.GetButton(langCode, "devices_reset_button").InlineCallback(CallbackDevicesReset)},
			{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)},
		}
	} else {
		text = fmt.Sprintf(h.translation.GetText(langCode, "devices_info"), expireDate, *customer.SubscriptionLink)
		keyboard = [][]models.InlineKeyboardButton{
			{h.translation.GetButton(langCode, "devices_reset_button").InlineCallback(CallbackDevicesReset)},
			{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)},
		}
	}

	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    msg.Chat.ID,
		MessageID: msg.ID,
		ParseMode: models.ParseModeHTML,
		Text:      text,
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: keyboard},
	})
}

func (h Handler) DevicesResetCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	langCode := update.CallbackQuery.From.LanguageCode
	msg := update.CallbackQuery.Message.Message

	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    msg.Chat.ID,
		MessageID: msg.ID,
		ParseMode: models.ParseModeHTML,
		Text:      h.translation.GetText(langCode, "devices_reset_confirm"),
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
			{h.translation.GetButton(langCode, "devices_reset_confirm_button").InlineCallback(CallbackDevicesResetConfirm)},
			{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackDevices)},
		}},
	})
}

func (h Handler) DevicesResetConfirmCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	telegramID := update.CallbackQuery.From.ID
	langCode := update.CallbackQuery.From.LanguageCode
	msg := update.CallbackQuery.Message.Message

	customer, err := h.customerRepository.FindByTelegramId(ctx, telegramID)
	if err != nil || customer == nil || customer.SubscriptionLink == nil {
		return
	}

	newUser, err := h.remnawaveClient.ResetSubscription(ctx, customer.ID, customer.TelegramID, customer.IsTrial)
	if err != nil {
		slog.Error("devices reset: failed", "telegram_id", telegramID, "error", err)
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    msg.Chat.ID,
			MessageID: msg.ID,
			ParseMode: models.ParseModeHTML,
			Text:      h.translation.GetText(langCode, "devices_reset_error"),
			ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
				{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)},
			}},
		})
		return
	}

	if err := h.customerRepository.UpdateFields(ctx, customer.ID, map[string]interface{}{
		"subscription_link": newUser.SubscriptionUrl,
	}); err != nil {
		slog.Error("devices reset: update subscription link", "error", err)
	}

	var text string
	var keyboard [][]models.InlineKeyboardButton
	if config.GetMiniAppURL() != "" {
		text = h.translation.GetText(langCode, "devices_reset_success_webapp")
		keyboard = [][]models.InlineKeyboardButton{
			{h.translation.GetButton(langCode, "connect_button").InlineWebApp(config.GetMiniAppURL())},
			{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)},
		}
	} else {
		text = fmt.Sprintf(h.translation.GetText(langCode, "devices_reset_success"), newUser.SubscriptionUrl)
		keyboard = [][]models.InlineKeyboardButton{
			{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)},
		}
	}

	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    msg.Chat.ID,
		MessageID: msg.ID,
		ParseMode: models.ParseModeHTML,
		Text:      text,
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: keyboard},
	})
	slog.Info("devices reset: completed", "telegram_id", telegramID)
}

func formatTimeUntil(t time.Time) string {
	d := time.Until(t)
	if d < 0 {
		return "истекла"
	}
	days := int(d.Hours() / 24)
	if days == 0 {
		return "сегодня"
	}
	return fmt.Sprintf("через %d дн.", days)
}
