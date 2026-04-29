package handler

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
)

func (h Handler) TopupCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	telegramID := update.CallbackQuery.From.ID
	langCode := update.CallbackQuery.From.LanguageCode
	msg := update.CallbackQuery.Message.Message

	// Check for a recent pending payment (created < 30 min ago, not yet attached to webhook).
	pending, err := h.topupRepository.FindRecentPendingByTelegramID(ctx, telegramID, 30*time.Minute)
	if err != nil {
		slog.Error("topup: find recent pending", "error", err)
	}

	if pending != nil {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    msg.Chat.ID,
			MessageID: msg.ID,
			ParseMode: models.ParseModeHTML,
			Text:      h.translation.GetText(langCode, "topup_pending_warning"),
			ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
				{h.translation.GetButton(langCode, "topup_cancel_button").InlineCallback(
					fmt.Sprintf("%s?id=%d", CallbackTopupCancel, pending.ID),
				)},
				{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)},
			}},
		})
		return
	}

	h.showTopupPackages(ctx, b, msg.Chat.ID, msg.ID, langCode, false)
}

func (h Handler) TopupSelectCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	telegramID := update.CallbackQuery.From.ID
	langCode := update.CallbackQuery.From.LanguageCode
	msg := update.CallbackQuery.Message.Message

	cbData := parseCallbackData(update.CallbackQuery.Data)
	gb, err := strconv.Atoi(cbData["gb"])
	if err != nil {
		slog.Error("topup select: invalid gb param", "data", update.CallbackQuery.Data, "error", err)
		return
	}

	pkg := config.TopupPackageByGB(gb)
	if pkg == nil {
		slog.Error("topup select: unknown gb amount", "gb", gb)
		return
	}

	// Create pending record before showing the payment URL.
	_, err = h.topupRepository.Create(ctx, &database.TrafficTopup{
		TelegramID: telegramID,
		GBAmount:   gb,
		Status:     database.TopupStatusPending,
	})
	if err != nil {
		slog.Error("topup select: create pending record", "error", err)
	}

	disclaimer := fmt.Sprintf(h.translation.GetText(langCode, "topup_disclaimer"), gb)
	btnLabel := fmt.Sprintf("+%d ГБ", pkg.GBAmount)

	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    msg.Chat.ID,
		MessageID: msg.ID,
		ParseMode: models.ParseModeHTML,
		Text:      disclaimer,
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: btnLabel, URL: pkg.URL}},
			{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackTopup)},
		}},
	})
}

func (h Handler) TopupCancelCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	telegramID := update.CallbackQuery.From.ID
	langCode := update.CallbackQuery.From.LanguageCode
	msg := update.CallbackQuery.Message.Message

	cbData := parseCallbackData(update.CallbackQuery.Data)
	if idStr, ok := cbData["id"]; ok {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err == nil {
			if err := h.topupRepository.ExpireByID(ctx, id); err != nil {
				slog.Error("topup cancel: expire record", "id", id, "error", err)
			}
		}
	}

	h.showTopupPackages(ctx, b, msg.Chat.ID, msg.ID, langCode, false)
}

func (h Handler) showTopupPackages(ctx context.Context, b *bot.Bot, chatID int64, messageID int, langCode string, isEdit bool) {
	packages := config.AllTopupPackages()
	var rows [][]models.InlineKeyboardButton
	for _, pkg := range packages {
		label := fmt.Sprintf("+%d ГБ", pkg.GBAmount)
		rows = append(rows, []models.InlineKeyboardButton{
			{
				Text:         label,
				CallbackData: fmt.Sprintf("%s?gb=%d", CallbackTopupSelect, pkg.GBAmount),
			},
		})
	}
	rows = append(rows, []models.InlineKeyboardButton{
		h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart),
	})

	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: messageID,
		ParseMode: models.ParseModeHTML,
		Text:      h.translation.GetText(langCode, "topup_select_package"),
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: rows},
	})
}
