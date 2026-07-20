package handler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/rollypay"
)

// DeviceBuyCallbackHandler starts a device-slot purchase: creates a pending device_topups row,
// asks RollyPay for a payment, and shows the pay_url. Kept in its own file rather than folded
// into devices_handler.go, which is purchase-flow-free self-management code (list/delete/reset) —
// same split as payment_handlers.go living next to devices_handler.go today.
func (h Handler) DeviceBuyCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	telegramID := update.CallbackQuery.From.ID
	langCode := update.CallbackQuery.From.LanguageCode
	msg := update.CallbackQuery.Message.Message

	customer, err := h.customerRepository.FindByTelegramId(ctx, telegramID)
	if err != nil {
		slog.Error("device buy: find customer", "error", err)
		return
	}
	if customer == nil || customer.ExpireAt == nil || !customer.ExpireAt.After(time.Now()) || customer.IsTrial || !config.IsRollyPayEnabled() {
		blockMsg := h.translation.GetText(langCode, "device_no_subscription")
		if customer != nil && customer.IsTrial {
			blockMsg = h.translation.GetText(langCode, "device_trial_only")
		}
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    msg.Chat.ID,
			MessageID: msg.ID,
			ParseMode: models.ParseModeHTML,
			Text:      blockMsg,
			ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
				{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackDevices)},
			}},
		})
		return
	}

	pending, err := h.deviceTopupRepository.FindRecentPendingByTelegramID(ctx, telegramID, 30*time.Minute)
	if err != nil {
		slog.Error("device buy: find recent pending", "error", err)
		return
	}
	if pending != nil {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    msg.Chat.ID,
			MessageID: msg.ID,
			ParseMode: models.ParseModeHTML,
			Text:      h.translation.GetText(langCode, "topup_pending_warning"),
			ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
				{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackDevices)},
			}},
		})
		return
	}

	priceRUB := config.DeviceSlotPriceRUB()
	topupID, err := h.deviceTopupRepository.Create(ctx, &database.DeviceTopup{
		TelegramID:  telegramID,
		DeviceCount: 1,
		PriceAmount: float64(priceRUB),
		Currency:    "RUB",
		Status:      database.TopupStatusPending,
	})
	if err != nil {
		slog.Error("device buy: create pending record", "error", err)
		h.showDeviceBuyError(ctx, b, msg.Chat.ID, msg.ID, langCode)
		return
	}

	paymentResp, err := h.rollypayClient.CreatePayment(ctx, rollypay.CreatePaymentRequest{
		Amount:      fmt.Sprintf("%d.00", priceRUB),
		OrderID:     fmt.Sprintf("device-%d", topupID),
		Description: "+1 device slot",
	})
	if err != nil {
		slog.Error("device buy: create rollypay payment", "error", err, "device_topup_id", topupID)
		if expireErr := h.deviceTopupRepository.ExpireByID(ctx, topupID); expireErr != nil {
			slog.Error("device buy: expire orphaned pending record", "error", expireErr, "device_topup_id", topupID)
		}
		h.showDeviceBuyError(ctx, b, msg.Chat.ID, msg.ID, langCode)
		return
	}

	disclaimer := fmt.Sprintf(h.translation.GetText(langCode, "device_disclaimer"), priceRUB)
	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    msg.Chat.ID,
		MessageID: msg.ID,
		ParseMode: models.ParseModeHTML,
		Text:      disclaimer,
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: h.translation.GetButton(langCode, "pay_button").Text, URL: paymentResp.PayURL}},
			{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackDevices)},
		}},
	})
}

func (h Handler) showDeviceBuyError(ctx context.Context, b *bot.Bot, chatID int64, messageID int, langCode string) {
	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID: chatID, MessageID: messageID, ParseMode: models.ParseModeHTML,
		Text: h.translation.GetText(langCode, "topup_error"),
		ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
			{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackDevices)},
		}},
	})
}
