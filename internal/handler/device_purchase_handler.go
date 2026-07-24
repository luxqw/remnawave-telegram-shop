package handler

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/payment"
	"remnawave-tg-shop-bot/internal/rollypay"
)

// DeviceBuyCallbackHandler is the quick "+1 slot" path — the common case, one tap. Delegates to
// createDeviceSlotInvoice, which also backs DeviceManageAmountTextHandler's typed-target-count
// flow (devices_handler.go) when the target is above the current count.
func (h Handler) DeviceBuyCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	telegramID := update.CallbackQuery.From.ID
	langCode := update.CallbackQuery.From.LanguageCode
	msg := update.CallbackQuery.Message.Message

	customer, err := h.customerRepository.FindByTelegramId(ctx, telegramID)
	if err != nil {
		slog.Error("device buy: find customer", "error", err)
		return
	}
	if customer == nil {
		return
	}
	h.createDeviceSlotInvoice(ctx, b, msg.Chat.ID, msg.ID, telegramID, langCode, customer, 1)
}

// createDeviceSlotInvoice creates a pending device_topups row for count additional slots, asks
// RollyPay for a payment, and shows the pay_url. count is always 1 from the quick-buy button, but
// can be more from DeviceManageAmountTextHandler's typed target — the proration math already
// takes a count param, so generalizing this from a hardcoded "+1" cost nothing but the Create/
// invoice/disclaimer calls actually using it.
func (h Handler) createDeviceSlotInvoice(ctx context.Context, b *bot.Bot, chatID int64, messageID int, telegramID int64, langCode string, customer *database.Customer, count int) {
	if customer.ExpireAt == nil || !customer.ExpireAt.After(time.Now()) || customer.IsTrial || !config.IsRollyPayEnabled() {
		blockMsg := h.translation.GetText(langCode, "device_no_subscription")
		if customer.IsTrial {
			blockMsg = h.translation.GetText(langCode, "device_trial_only")
		}
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: messageID,
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
		h.showPendingPurchaseWarning(ctx, b, chatID, messageID, langCode, fmt.Sprintf("%s?id=%d", CallbackDeviceCancel, pending.ID))
		return
	}

	// An existing addon's billing mode is reused rather than re-derived so a customer's mode can
	// never flip mid-cycle if their Tribute status happens to change between purchases.
	existingAddon, err := h.deviceAddonRepository.FindActiveByTelegramID(ctx, telegramID)
	if err != nil {
		slog.Error("device buy: find existing device addon", "error", err)
		h.showDeviceBuyError(ctx, b, chatID, messageID, langCode)
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
			h.showDeviceBuyError(ctx, b, chatID, messageID, langCode)
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
		amount, _ = payment.ProrateDeviceCost(customer, count)
	case hasActiveAddon:
		amount, _ = payment.ProrateDeviceCostForCycle(existingAddon.CycleExpiresAt, count)
	default:
		amount = float64(count) * float64(config.DeviceSlotPriceRUB())
	}
	if amount <= 0 {
		slog.Error("device buy: prorated amount is zero", "telegram_id", telegramID)
		h.showDeviceBuyError(ctx, b, chatID, messageID, langCode)
		return
	}

	topupID, err := h.deviceTopupRepository.Create(ctx, &database.DeviceTopup{
		TelegramID:  telegramID,
		DeviceCount: count,
		PriceAmount: amount,
		Currency:    "RUB",
		Status:      database.TopupStatusPending,
	})
	if err != nil {
		slog.Error("device buy: create pending record", "error", err)
		h.showDeviceBuyError(ctx, b, chatID, messageID, langCode)
		return
	}

	paymentResp, err := h.rollypayClient.CreatePayment(ctx, rollypay.CreatePaymentRequest{
		Amount:      fmt.Sprintf("%.2f", amount),
		OrderID:     fmt.Sprintf("device-%d", topupID),
		Description: fmt.Sprintf("+%d device slot(s)", count),
		Test:        config.RollyPayTestMode() && telegramID == config.GetAdminTelegramId(),
	})
	if err != nil {
		slog.Error("device buy: create rollypay payment", "error", err, "device_topup_id", topupID)
		if expireErr := h.deviceTopupRepository.ExpireByID(ctx, topupID); expireErr != nil {
			slog.Error("device buy: expire orphaned pending record", "error", expireErr, "device_topup_id", topupID)
		}
		h.showDeviceBuyError(ctx, b, chatID, messageID, langCode)
		return
	}

	disclaimerKey := "device_disclaimer_bundled"
	if billingMode == database.AddonBillingModeStandalone {
		disclaimerKey = "device_disclaimer_standalone"
	}
	disclaimer := fmt.Sprintf(h.translation.GetText(langCode, disclaimerKey), count, int(math.Round(amount)))
	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   messageID,
		ParseMode:   models.ParseModeHTML,
		Text:        disclaimer,
		ReplyMarkup: h.payOrCancelKeyboard(langCode, paymentResp.PayURL, fmt.Sprintf("%s?id=%d", CallbackDeviceCancel, topupID)),
	})
	// Lets rollypay.WebhookClient delete this Pay/Cancel message on completion instead of leaving
	// it stuck on screen next to the success notice (mirrors the subscription flow's cache.Set in
	// payment_handlers.go). The invoice is an edit of the message already at messageID, so that's
	// also its final message ID.
	h.deviceTopupInvoiceCache.Set(topupID, messageID)
}

// DeviceCancelCallbackHandler cancels a stuck pending device-slot purchase, mirroring
// TopupCancelCallbackHandler/PaymentCancelCallbackHandler — lets the customer immediately retry
// instead of waiting out the 30-minute pending window in FindRecentPendingByTelegramID.
func (h Handler) DeviceCancelCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	cbData := parseCallbackData(update.CallbackQuery.Data)
	if idStr, ok := cbData["id"]; ok {
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			if err := h.deviceTopupRepository.ExpireByID(ctx, id); err != nil {
				slog.Error("device cancel: expire record", "id", id, "error", err)
			}
		}
	}
	h.DevicesCallbackHandler(ctx, b, update)
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
