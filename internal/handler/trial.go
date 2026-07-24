package handler

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"log/slog"

	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/remnawave"
	"remnawave-tg-shop-bot/utils"
)

func (h Handler) TrialCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if config.TrialDays() == 0 {
		return
	}
	c, err := h.customerRepository.FindByTelegramId(ctx, update.CallbackQuery.From.ID)
	if err != nil {
		slog.Error("Error finding customer", "error", err)
		return
	}
	if c == nil {
		slog.Error("customer not exist", "telegramId", utils.MaskHalfInt64(update.CallbackQuery.From.ID), "error", err)
		return
	}
	callback := update.CallbackQuery.Message.Message
	langCode := update.CallbackQuery.From.LanguageCode
	if c.SubscriptionLink != nil {
		_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
			Text:            h.translation.GetText(langCode, "trial_already_active"),
		})
		return
	}
	_, err = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    callback.Chat.ID,
		MessageID: callback.ID,
		Text:      h.translation.GetText(langCode, "trial_text"),
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
			{h.translation.GetButton(langCode, "activate_trial_button").InlineCallback(CallbackActivateTrial)},
			{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)},
		}},
	})
	if err != nil {
		slog.Error("Error sending /trial message", "error", err)
	}
}

func (h Handler) ActivateTrialCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if config.TrialDays() == 0 {
		return
	}
	c, err := h.customerRepository.FindByTelegramId(ctx, update.CallbackQuery.From.ID)
	if err != nil {
		slog.Error("Error finding customer", "error", err)
		return
	}
	if c == nil {
		slog.Error("customer not exist", "telegramId", utils.MaskHalfInt64(update.CallbackQuery.From.ID), "error", err)
		return
	}
	callback := update.CallbackQuery.Message.Message
	langCode := update.CallbackQuery.From.LanguageCode
	if c.SubscriptionLink != nil {
		_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
			Text:            h.translation.GetText(langCode, "trial_already_active"),
		})
		return
	}
	ctxWithUsername := context.WithValue(ctx, remnawave.CtxKeyUsername, update.CallbackQuery.From.Username)
	_, err = h.paymentService.ActivateTrial(ctxWithUsername, update.CallbackQuery.From.ID)
	if err != nil {
		slog.Error("Error activating trial", "error", err)
		// The trial button is the primary new-user CTA — silently doing nothing on failure (the
		// old behavior) looks exactly like a broken bot to someone who has no other signal yet
		// that this service works. Every other purchase-adjacent flow already shows an explicit
		// error (topup_error, devices_delete_error); this was the one gap.
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    callback.Chat.ID,
			MessageID: callback.ID,
			Text:      h.translation.GetText(langCode, "trial_activation_error"),
			ParseMode: models.ParseModeHTML,
			ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
				{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)},
			}},
		})
		return
	}
	_, err = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      callback.Chat.ID,
		MessageID:   callback.ID,
		Text:        h.translation.GetText(langCode, "trial_activated"),
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: h.createConnectKeyboard(langCode)},
	})
	if err != nil {
		slog.Error("Error sending /trial message", "error", err)
	}
}

func (h Handler) createConnectKeyboard(lang string) [][]models.InlineKeyboardButton {
	var inlineCustomerKeyboard [][]models.InlineKeyboardButton
	inlineCustomerKeyboard = append(inlineCustomerKeyboard, h.resolveConnectButton(lang))

	inlineCustomerKeyboard = append(inlineCustomerKeyboard, []models.InlineKeyboardButton{
		h.translation.GetButton(lang, "back_button").InlineCallback(CallbackStart),
	})
	return inlineCustomerKeyboard
}
