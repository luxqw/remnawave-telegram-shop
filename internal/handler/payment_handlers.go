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
			_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
				ChatID:    callback.Chat.ID,
				MessageID: callback.ID,
				ParseMode: models.ParseModeHTML,
				Text:      h.translation.GetText(langCode, "topup_pending_warning"),
				ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
					{h.translation.GetButton(langCode, "topup_cancel_button").InlineCallback(fmt.Sprintf("%s?id=%d", CallbackPaymentCancel, pending.ID))},
					{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)},
				}},
			})
			return
		}
	}

	ctxWithUsername := context.WithValue(ctx, remnawave.CtxKeyUsername, update.CallbackQuery.From.Username)
	paymentURL, purchaseId, err := h.paymentService.CreatePurchase(ctxWithUsername, float64(price), month, customer, invoiceType)
	if err != nil {
		slog.Error("Error creating payment", "error", err)
		h.showPaymentError(ctx, b, callback.Chat.ID, callback.ID, langCode)
		return
	}

	monthLabel := h.translation.GetText(langCode, fmt.Sprintf("month_%d", month))
	summaryText := fmt.Sprintf(h.translation.GetText(langCode, "payment_summary"), monthLabel, price)

	message, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    callback.Chat.ID,
		MessageID: callback.ID,
		ParseMode: models.ParseModeHTML,
		Text:      summaryText,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					h.translation.GetButton(langCode, "pay_button").InlineURL(paymentURL),
					h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackBuy),
				},
			},
		},
	})
	if err != nil {
		slog.Error("Error updating payment message", "error", err)
		return
	}
	h.cache.Set(purchaseId, message.ID)
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
