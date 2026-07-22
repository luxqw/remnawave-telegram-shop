package handler

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/payment"
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

	// An existing addon's billing mode is reused rather than re-derived so a customer's mode can
	// never flip mid-cycle if their Tribute status happens to change between purchases.
	existingAddon, err := h.deviceAddonRepository.FindActiveByTelegramID(ctx, telegramID)
	if err != nil {
		slog.Error("device buy: find existing device addon", "error", err)
		h.showDeviceBuyError(ctx, b, msg.Chat.ID, msg.ID, langCode)
		return
	}
	hasActiveAddon := existingAddon != nil && existingAddon.Status != database.AddonStatusExpired

	billingMode := database.AddonBillingModeBundled
	if hasActiveAddon {
		billingMode = existingAddon.BillingMode
	} else {
		billingMode, err = h.paymentService.DetermineDeviceAddonBillingMode(ctx, customer)
		if err != nil {
			slog.Error("device buy: determine billing mode", "error", err)
			h.showDeviceBuyError(ctx, b, msg.Chat.ID, msg.ID, langCode)
			return
		}
	}

	// Bundled addons always ride the subscription's own cycle (decision 2), so every purchase —
	// first slot or another one mid-cycle — prorates against customer.ExpireAt. Standalone addons
	// have their own independent cycle (decision 3): a first purchase has no cycle yet to prorate
	// against, so it's a full fresh-cycle charge; adding another slot mid that cycle prorates
	// against the addon's own CycleExpiresAt, never the (often much longer, unrelated) subscription.
	var amount float64
	switch {
	case billingMode == database.AddonBillingModeBundled:
		amount, _ = payment.ProrateDeviceCost(customer, 1)
	case hasActiveAddon:
		amount, _ = payment.ProrateDeviceCostForCycle(existingAddon.CycleExpiresAt, 1)
	default:
		amount = float64(config.DeviceSlotPriceRUB())
	}
	if amount <= 0 {
		slog.Error("device buy: prorated amount is zero", "telegram_id", telegramID)
		h.showDeviceBuyError(ctx, b, msg.Chat.ID, msg.ID, langCode)
		return
	}

	topupID, err := h.deviceTopupRepository.Create(ctx, &database.DeviceTopup{
		TelegramID:  telegramID,
		DeviceCount: 1,
		PriceAmount: amount,
		Currency:    "RUB",
		Status:      database.TopupStatusPending,
	})
	if err != nil {
		slog.Error("device buy: create pending record", "error", err)
		h.showDeviceBuyError(ctx, b, msg.Chat.ID, msg.ID, langCode)
		return
	}

	paymentResp, err := h.rollypayClient.CreatePayment(ctx, rollypay.CreatePaymentRequest{
		Amount:      fmt.Sprintf("%.2f", amount),
		OrderID:     fmt.Sprintf("device-%d", topupID),
		Description: "+1 device slot",
		Test:        config.RollyPayTestMode() && telegramID == config.GetAdminTelegramId(),
	})
	if err != nil {
		slog.Error("device buy: create rollypay payment", "error", err, "device_topup_id", topupID)
		if expireErr := h.deviceTopupRepository.ExpireByID(ctx, topupID); expireErr != nil {
			slog.Error("device buy: expire orphaned pending record", "error", expireErr, "device_topup_id", topupID)
		}
		h.showDeviceBuyError(ctx, b, msg.Chat.ID, msg.ID, langCode)
		return
	}

	disclaimerKey := "device_disclaimer_bundled"
	if billingMode == database.AddonBillingModeStandalone {
		disclaimerKey = "device_disclaimer_standalone"
	}
	disclaimer := fmt.Sprintf(h.translation.GetText(langCode, disclaimerKey), int(math.Round(amount)))
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
