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
	"remnawave-tg-shop-bot/internal/rollypay"
)

func (h Handler) TopupCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	telegramID := update.CallbackQuery.From.ID
	langCode := update.CallbackQuery.From.LanguageCode
	msg := update.CallbackQuery.Message.Message

	customer, err := h.customerRepository.FindByTelegramId(ctx, telegramID)
	if err != nil {
		slog.Error("topup: find customer", "error", err)
		return
	}
	if customer == nil || customer.ExpireAt == nil || !customer.ExpireAt.After(time.Now()) || customer.IsTrial {
		blockMsg := h.translation.GetText(langCode, "topup_no_subscription")
		if customer != nil && customer.IsTrial {
			blockMsg = h.translation.GetText(langCode, "topup_trial_only")
		}
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    msg.Chat.ID,
			MessageID: msg.ID,
			ParseMode: models.ParseModeHTML,
			Text:      blockMsg,
			ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
				{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)},
			}},
		})
		return
	}

	h.topupAwaitingInput.Delete(telegramID)

	pending, err := h.topupRepository.FindRecentPendingByTelegramID(ctx, telegramID, 30*time.Minute)
	if err != nil {
		slog.Error("topup: find recent pending", "error", err)
		return
	}
	if pending != nil {
		h.showPendingPurchaseWarning(ctx, b, msg.Chat.ID, msg.ID, langCode, fmt.Sprintf("%s?id=%d", CallbackTopupCancel, pending.ID))
		return
	}
	h.showTopupPackages(ctx, b, msg.Chat.ID, msg.ID, langCode)
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
	tier := config.GBTopupTierByGB(gb)
	if tier == nil {
		slog.Error("topup select: unknown gb amount", "gb", gb)
		return
	}
	h.createTopupInvoice(ctx, b, msg.Chat.ID, msg.ID, telegramID, langCode, gb, tier.PriceRUB)
}

// TopupCustomCallbackHandler prompts the user to type how much GB they want, then marks them as
// awaiting a free-text reply (picked up by TopupCustomAmountTextHandler).
func (h Handler) TopupCustomCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	telegramID := update.CallbackQuery.From.ID
	langCode := update.CallbackQuery.From.LanguageCode
	msg := update.CallbackQuery.Message.Message

	minGB, maxGB := config.GBTopupCustomMinGB(), config.GBTopupCustomMaxGB()
	prompt := fmt.Sprintf(h.translation.GetText(langCode, "topup_custom_prompt"), minGB, maxGB)

	message, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    msg.Chat.ID,
		MessageID: msg.ID,
		ParseMode: models.ParseModeHTML,
		Text:      prompt,
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
			{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackTopup)},
		}},
	})
	if err != nil {
		slog.Error("topup custom: edit prompt message", "error", err)
		return
	}
	h.topupAwaitingInput.Set(telegramID, message.ID)
}

// TopupAwaitingInput reports whether telegramID currently has an open custom-GB-amount prompt —
// used by main.go's RegisterHandlerMatchFunc to decide whether an incoming text message should be
// routed to TopupCustomAmountTextHandler.
func (h Handler) TopupAwaitingInput(telegramID int64) (int, bool) {
	return h.topupAwaitingInput.Get(telegramID)
}

// TopupCustomAmountTextHandler parses the free-text GB amount, validates bounds, and starts the
// same create-row -> CreatePayment -> show-pay-button flow the preset tiers use.
func (h Handler) TopupCustomAmountTextHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	telegramID := update.Message.From.ID
	langCode := update.Message.From.LanguageCode
	promptMessageID, ok := h.topupAwaitingInput.Get(telegramID)
	if !ok {
		return
	}
	h.topupAwaitingInput.Delete(telegramID)

	minGB, maxGB := config.GBTopupCustomMinGB(), config.GBTopupCustomMaxGB()

	gb, err := strconv.Atoi(update.Message.Text)
	if err != nil {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    update.Message.Chat.ID,
			MessageID: promptMessageID,
			ParseMode: models.ParseModeHTML,
			Text:      h.translation.GetText(langCode, "topup_custom_invalid"),
			ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
				{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackTopup)},
			}},
		})
		return
	}
	if gb < minGB || gb > maxGB {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    update.Message.Chat.ID,
			MessageID: promptMessageID,
			ParseMode: models.ParseModeHTML,
			Text:      fmt.Sprintf(h.translation.GetText(langCode, "topup_custom_out_of_range"), minGB, maxGB),
			ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
				{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackTopup)},
			}},
		})
		return
	}

	price := gb * config.GBTopupCustomPricePerGB()
	h.createTopupInvoice(ctx, b, update.Message.Chat.ID, promptMessageID, telegramID, langCode, gb, price)
}

// createTopupInvoice creates the pending traffic_topups row, asks RollyPay for a payment, and
// shows the resulting pay_url — the shared tail end of both the preset-tier and custom-amount
// flows.
func (h Handler) createTopupInvoice(ctx context.Context, b *bot.Bot, chatID int64, messageID int, telegramID int64, langCode string, gb, priceRUB int) {
	topupID, err := h.topupRepository.Create(ctx, &database.TrafficTopup{
		TelegramID:  telegramID,
		GBAmount:    gb,
		PriceAmount: float64(priceRUB),
		Currency:    "RUB",
		Status:      database.TopupStatusPending,
	})
	if err != nil {
		slog.Error("topup: create pending record", "error", err)
		h.showTopupError(ctx, b, chatID, messageID, langCode)
		return
	}

	paymentResp, err := h.rollypayClient.CreatePayment(ctx, rollypay.CreatePaymentRequest{
		Amount:      fmt.Sprintf("%d.00", priceRUB),
		OrderID:     fmt.Sprintf("topup-%d", topupID),
		Description: fmt.Sprintf("+%d GB traffic top-up", gb),
		Test:        config.RollyPayTestMode() && telegramID == config.GetAdminTelegramId(),
	})
	if err != nil {
		slog.Error("topup: create rollypay payment", "error", err, "topup_id", topupID)
		if expireErr := h.topupRepository.ExpireByID(ctx, topupID); expireErr != nil {
			slog.Error("topup: expire orphaned pending record", "error", expireErr, "topup_id", topupID)
		}
		h.showTopupError(ctx, b, chatID, messageID, langCode)
		return
	}

	baseGB := config.TrafficLimit() / config.BytesInGigabyte()
	disclaimer := fmt.Sprintf(h.translation.GetText(langCode, "topup_disclaimer"), gb, priceRUB, baseGB)
	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   messageID,
		ParseMode:   models.ParseModeHTML,
		Text:        disclaimer,
		ReplyMarkup: h.payOrCancelKeyboard(langCode, paymentResp.PayURL, fmt.Sprintf("%s?id=%d", CallbackTopupCancel, topupID)),
	})
	// Lets rollypay.WebhookClient delete this Pay/Cancel message on completion instead of leaving
	// it stuck on screen next to the success notice (mirrors the subscription flow's cache.Set in
	// payment_handlers.go). The invoice is an edit of the message already at messageID, so that's
	// also its final message ID — no need to read EditMessageText's return value.
	h.topupInvoiceCache.Set(topupID, messageID)
}

func (h Handler) showTopupError(ctx context.Context, b *bot.Bot, chatID int64, messageID int, langCode string) {
	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID: chatID, MessageID: messageID, ParseMode: models.ParseModeHTML,
		Text: h.translation.GetText(langCode, "topup_error"),
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
			{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)},
		}},
	})
}

func (h Handler) TopupCancelCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
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
	h.showTopupPackages(ctx, b, msg.Chat.ID, msg.ID, langCode)
}

func (h Handler) showTopupPackages(ctx context.Context, b *bot.Bot, chatID int64, messageID int, langCode string) {
	tiers := config.GBTopupTiers()
	var rows [][]models.InlineKeyboardButton
	for _, tier := range tiers {
		label := fmt.Sprintf("+%d GB — %d ₽", tier.GBAmount, tier.PriceRUB)
		rows = append(rows, []models.InlineKeyboardButton{{Text: label, CallbackData: fmt.Sprintf("%s?gb=%d", CallbackTopupSelect, tier.GBAmount)}})
	}
	rows = append(rows, []models.InlineKeyboardButton{
		{Text: h.translation.GetButton(langCode, "topup_custom_button").Text, CallbackData: CallbackTopupCustom},
	})
	rows = append(rows, []models.InlineKeyboardButton{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)})
	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   messageID,
		ParseMode:   models.ParseModeHTML,
		Text:        h.translation.GetText(langCode, "topup_select_package"),
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: rows},
	})
}
