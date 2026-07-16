package handler

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remnawave-tg-shop-bot/internal/config"

	"log/slog"
)

const referralListPageSize = 10

func (h Handler) ReferralCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	customer, err := h.customerRepository.FindByTelegramId(ctx, update.CallbackQuery.From.ID)
	if err != nil {
		slog.Error("error finding customer", "error", err)
		return
	}
	if customer == nil {
		slog.Error("customer not found", "telegramId", update.CallbackQuery.From.ID)
		return
	}
	langCode := update.CallbackQuery.From.LanguageCode
	refCode := customer.TelegramID

	botUsername := ""
	if msg := update.CallbackQuery.Message.Message; msg != nil && msg.From != nil {
		botUsername = msg.From.Username
	}
	if botUsername == "" {
		slog.Warn("referral: bot has no username, cannot generate referral link")
		callbackMessage := update.CallbackQuery.Message.Message
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    callbackMessage.Chat.ID,
			MessageID: callbackMessage.ID,
			Text:      h.translation.GetText(langCode, "referral_link_unavailable"),
			ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
				{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)},
			}},
		})
		return
	}
	refLink := fmt.Sprintf("https://telegram.me/share/url?url=https://t.me/%s?start=ref_%d", botUsername, refCode)
	count, err := h.referralRepository.CountByReferrer(ctx, customer.TelegramID)
	if err != nil {
		slog.Error("error counting referrals", "error", err)
		return
	}
	text := fmt.Sprintf(h.translation.GetText(langCode, "referral_text"), count)
	callbackMessage := update.CallbackQuery.Message.Message
	var rows [][]models.InlineKeyboardButton
	rows = append(rows, []models.InlineKeyboardButton{h.translation.GetButton(langCode, "share_referral_button").InlineURL(refLink)})
	if count > 0 {
		rows = append(rows, []models.InlineKeyboardButton{
			h.translation.GetButton(langCode, "referral_list_button").InlineCallback(CallbackReferralList + "?p=1"),
		})
	}
	rows = append(rows, []models.InlineKeyboardButton{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)})
	_, err = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      callbackMessage.Chat.ID,
		MessageID:   callbackMessage.ID,
		Text:        text,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: rows},
	})
	if err != nil {
		slog.Error("Error sending referral message", "error", err)
	}
}

// ReferralListCallbackHandler shows a paginated list of the customer's referrals with each
// referral's bonus status, plus the total bonus days earned so far.
func (h Handler) ReferralListCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	customer, err := h.customerRepository.FindByTelegramId(ctx, update.CallbackQuery.From.ID)
	if err != nil {
		slog.Error("error finding customer", "error", err)
		return
	}
	if customer == nil {
		slog.Error("customer not found", "telegramId", update.CallbackQuery.From.ID)
		return
	}
	langCode := update.CallbackQuery.From.LanguageCode
	callbackMessage := update.CallbackQuery.Message.Message

	cbData := parseCallbackData(update.CallbackQuery.Data)
	page, err := strconv.Atoi(cbData["p"])
	if err != nil || page < 1 {
		page = 1
	}

	total, err := h.referralRepository.CountByReferrer(ctx, customer.TelegramID)
	if err != nil {
		slog.Error("error counting referrals", "error", err)
		return
	}

	if total == 0 {
		_, err = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    callbackMessage.Chat.ID,
			MessageID: callbackMessage.ID,
			Text:      h.translation.GetText(langCode, "referral_list_empty"),
			ParseMode: models.ParseModeHTML,
			ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
				{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackReferral)},
			}},
		})
		if err != nil {
			slog.Error("Error sending empty referral list message", "error", err)
		}
		return
	}

	granted, err := h.referralRepository.CountGrantedByReferrer(ctx, customer.TelegramID)
	if err != nil {
		slog.Error("error counting granted referrals", "error", err)
		return
	}

	lastPage := (total + referralListPageSize - 1) / referralListPageSize
	if page > lastPage {
		page = lastPage
	}
	offset := (page - 1) * referralListPageSize

	referrals, err := h.referralRepository.FindByReferrerPaginated(ctx, customer.TelegramID, referralListPageSize, offset)
	if err != nil {
		slog.Error("error fetching paginated referrals", "error", err)
		return
	}

	totalDays := granted * config.GetReferralDays()
	text := fmt.Sprintf(h.translation.GetText(langCode, "referral_list_header"), total, totalDays)
	for i, ref := range referrals {
		position := offset + i + 1
		joinedDate := ref.UsedAt.Format("02.01.2006")
		if ref.BonusGranted {
			text += fmt.Sprintf(h.translation.GetText(langCode, "referral_list_item_granted"), position, joinedDate, config.GetReferralDays()) + "\n"
		} else {
			text += fmt.Sprintf(h.translation.GetText(langCode, "referral_list_item_pending"), position, joinedDate) + "\n"
		}
	}

	var navRow []models.InlineKeyboardButton
	if page > 1 {
		navRow = append(navRow, h.translation.GetButton(langCode, "prev_page_button").InlineCallback(fmt.Sprintf("%s?p=%d", CallbackReferralList, page-1)))
	}
	if page < lastPage {
		navRow = append(navRow, h.translation.GetButton(langCode, "next_page_button").InlineCallback(fmt.Sprintf("%s?p=%d", CallbackReferralList, page+1)))
	}

	var rows [][]models.InlineKeyboardButton
	if len(navRow) > 0 {
		rows = append(rows, navRow)
	}
	rows = append(rows, []models.InlineKeyboardButton{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackReferral)})

	_, err = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      callbackMessage.Chat.ID,
		MessageID:   callbackMessage.ID,
		Text:        text,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: rows},
	})
	if err != nil {
		slog.Error("Error sending referral list message", "error", err)
	}
}
