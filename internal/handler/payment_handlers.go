package handler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"log/slog"

	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/remnawave"
)

// BuyCallbackHandler shows the month picker. RollyPay is the only payment provider, so a month
// button routes straight into PaymentCallbackHandler (CallbackPayment) instead of through an
// intermediate provider-choice screen — there's nothing to choose anymore.
func (h Handler) BuyCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	callback := update.CallbackQuery.Message.Message
	langCode := update.CallbackQuery.From.LanguageCode

	if !config.IsRollyPayEnabled() {
		_, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    callback.Chat.ID,
			MessageID: callback.ID,
			ParseMode: models.ParseModeHTML,
			Text:      h.translation.GetText(langCode, "payment_unavailable"),
			ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
				{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)},
			}},
		})
		if err != nil {
			slog.Error("Error sending buy message", "error", err)
		}
		return
	}

	var priceButtons []models.InlineKeyboardButton

	if config.Price1() > 0 {
		priceButtons = append(priceButtons, h.translation.GetButton(langCode, "month_1").InlineCallback(fmt.Sprintf("%s?month=%d&invoiceType=%s&amount=%d", CallbackPayment, 1, database.InvoiceTypeRollyPay, config.Price1())))
	}

	if config.Price3() > 0 {
		priceButtons = append(priceButtons, h.translation.GetButton(langCode, "month_3").InlineCallback(fmt.Sprintf("%s?month=%d&invoiceType=%s&amount=%d", CallbackPayment, 3, database.InvoiceTypeRollyPay, config.Price3())))
	}

	if config.Price6() > 0 {
		priceButtons = append(priceButtons, h.translation.GetButton(langCode, "month_6").InlineCallback(fmt.Sprintf("%s?month=%d&invoiceType=%s&amount=%d", CallbackPayment, 6, database.InvoiceTypeRollyPay, config.Price6())))
	}

	if config.Price12() > 0 {
		priceButtons = append(priceButtons, h.translation.GetButton(langCode, "month_12").InlineCallback(fmt.Sprintf("%s?month=%d&invoiceType=%s&amount=%d", CallbackPayment, 12, database.InvoiceTypeRollyPay, config.Price12())))
	}

	keyboard := [][]models.InlineKeyboardButton{}

	if len(priceButtons) == 4 {
		keyboard = append(keyboard, priceButtons[:2])
		keyboard = append(keyboard, priceButtons[2:])
	} else if len(priceButtons) > 0 {
		keyboard = append(keyboard, priceButtons)
	}

	keyboard = append(keyboard, []models.InlineKeyboardButton{
		h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart),
	})

	_, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    callback.Chat.ID,
		MessageID: callback.ID,
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: keyboard,
		},
		Text: h.translation.GetText(langCode, "pricing_info"),
	})

	if err != nil {
		slog.Error("Error sending buy message", "error", err)
	}
}

func (h Handler) PaymentCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	callback := update.CallbackQuery.Message.Message
	callbackQuery := parseCallbackData(update.CallbackQuery.Data)
	month, err := strconv.Atoi(callbackQuery["month"])
	if err != nil {
		slog.Error("Error getting month from query", "error", err)
		return
	}

	invoiceType := database.InvoiceType(callbackQuery["invoiceType"])
	price := config.Price(month)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	langCode := update.CallbackQuery.From.LanguageCode

	customer, err := h.customerRepository.FindByTelegramId(ctx, callback.Chat.ID)
	if err != nil {
		slog.Error("Error finding customer", "error", err)
		h.showPaymentError(ctx, b, callback.Chat.ID, callback.ID, langCode)
		return
	}
	if customer == nil {
		slog.Error("customer not exist", "chatID", callback.Chat.ID, "error", err)
		h.showPaymentError(ctx, b, callback.Chat.ID, callback.ID, langCode)
		return
	}

	if invoiceType == database.InvoiceTypeRollyPay {
		pending, err := h.purchaseRepository.FindRecentPendingByCustomerID(ctx, customer.ID, 30*time.Minute)
		if err != nil {
			slog.Error("Error checking recent pending purchase", "error", err)
			h.showPaymentError(ctx, b, callback.Chat.ID, callback.ID, langCode)
			return
		}
		if pending != nil {
			h.showPendingPurchaseWarning(ctx, b, callback.Chat.ID, callback.ID, langCode, fmt.Sprintf("%s?id=%d", CallbackPaymentCancel, pending.ID))
			return
		}
	}

	ctxWithUsername := context.WithValue(ctx, remnawave.CtxKeyUsername, update.CallbackQuery.From.Username)
	paymentURL, purchaseId, chargedAmount, err := h.paymentService.CreatePurchase(ctxWithUsername, float64(price), month, customer, invoiceType)
	if err != nil {
		slog.Error("Error creating payment", "error", err)
		h.showPaymentError(ctx, b, callback.Chat.ID, callback.ID, langCode)
		return
	}

	// chargedAmount may exceed price: createRollyPayInvoice folds an active bundled device addon's
	// renewal cost into the subscription invoice (decision 2), and the summary must show what's
	// actually being charged, not the bare subscription price.
	monthLabel := h.translation.GetText(langCode, fmt.Sprintf("month_%d", month))
	summaryText := fmt.Sprintf(h.translation.GetText(langCode, "payment_summary"), monthLabel, int(chargedAmount))

	message, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      callback.Chat.ID,
		MessageID:   callback.ID,
		ParseMode:   models.ParseModeHTML,
		Text:        summaryText,
		ReplyMarkup: h.payOrCancelKeyboard(langCode, paymentURL, fmt.Sprintf("%s?id=%d", CallbackPaymentCancel, purchaseId)),
	})
	if err != nil {
		slog.Error("Error updating payment message", "error", err)
		return
	}
	h.cache.Set(purchaseId, message.ID)
}

// payOrCancelKeyboard is the button row shown right after a RollyPay invoice is created (Screen A
// across the subscription/topup/device flows): Pay, plus a single Cancel. There used to be a
// "Back (doesn't cancel)" button here instead of Cancel, but none of the rollypay webhook dispatch
// paths gate on cancelled/expired status — a completed payment is honored regardless of local
// state — so a non-cancelling alternative never protected anything, it only added a confusing
// second button.
func (h Handler) payOrCancelKeyboard(langCode, payURL, cancelCallbackData string) models.InlineKeyboardMarkup {
	return models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{h.translation.GetButton(langCode, "pay_button").InlineURL(payURL)},
		{h.translation.GetButton(langCode, "topup_cancel_button").InlineCallback(cancelCallbackData)},
	}}
}

// showPendingPurchaseWarning renders the "payment not finished" blocker screen (Screen B) shown
// when a customer returns to a purchase flow with one already in flight. Shared by the
// subscription/topup/device-purchase flows, which used to each copy-paste this screen with
// slightly different (and in two cases, stale) button sets — see payOrCancelKeyboard's doc comment
// for why Cancel is the only action offered.
func (h Handler) showPendingPurchaseWarning(ctx context.Context, b *bot.Bot, chatID int64, messageID int, langCode, cancelCallbackData string) {
	_, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: messageID,
		ParseMode: models.ParseModeHTML,
		Text:      h.translation.GetText(langCode, "topup_pending_warning"),
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
			{h.translation.GetButton(langCode, "topup_cancel_button").InlineCallback(cancelCallbackData)},
		}},
	})
	if err != nil {
		slog.Error("show pending purchase warning", "error", err)
	}
}

// showPaymentError gives visible feedback on a failed purchase attempt instead of leaving the
// previous screen frozen with stale buttons — matches showTopupError/showDeviceBuyError.
func (h Handler) showPaymentError(ctx context.Context, b *bot.Bot, chatID int64, messageID int, langCode string) {
	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID: chatID, MessageID: messageID, ParseMode: models.ParseModeHTML,
		Text: h.translation.GetText(langCode, "topup_error"),
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
			{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)},
		}},
	})
}

// PaymentCancelCallbackHandler cancels a stuck pending subscription purchase, mirroring
// TopupCancelCallbackHandler — lets the customer immediately retry instead of waiting out the
// 30-minute pending window in FindRecentPendingByCustomerID.
func (h Handler) PaymentCancelCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	langCode := update.CallbackQuery.From.LanguageCode
	callback := update.CallbackQuery.Message.Message
	cbData := parseCallbackData(update.CallbackQuery.Data)
	if idStr, ok := cbData["id"]; ok {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err == nil {
			if err := h.purchaseRepository.UpdateFields(ctx, id, map[string]interface{}{"status": database.PurchaseStatusCancel}); err != nil {
				slog.Error("payment cancel: cancel purchase", "id", id, "error", err)
			}
		}
	}
	_, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    callback.Chat.ID,
		MessageID: callback.ID,
		ParseMode: models.ParseModeHTML,
		Text:      h.translation.GetText(langCode, "pricing_info"),
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
			{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackBuy)},
		}},
	})
	if err != nil {
		slog.Error("payment cancel: edit message", "error", err)
	}
}

func parseCallbackData(data string) map[string]string {
	result := make(map[string]string)

	parts := strings.Split(data, "?")
	if len(parts) < 2 {
		return result
	}

	params := strings.Split(parts[1], "&")
	for _, param := range params {
		kv := strings.SplitN(param, "=", 2)
		if len(kv) == 2 {
			result[kv[0]] = kv[1]
		}
	}

	return result
}
