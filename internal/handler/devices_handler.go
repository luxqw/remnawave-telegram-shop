package handler

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/remnawave"
	"remnawave-tg-shop-bot/internal/translation"
)

func (h Handler) DevicesCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	telegramID := update.CallbackQuery.From.ID
	langCode := update.CallbackQuery.From.LanguageCode
	msg := update.CallbackQuery.Message.Message

	customer, err := h.customerRepository.FindByTelegramId(ctx, telegramID)
	if err != nil || customer == nil || customer.SubscriptionLink == nil || customer.ExpireAt == nil {
		return
	}

	// Clears a stale "awaiting slot count" prompt from DeviceManageCallbackHandler — this is the
	// screen "Назад" returns to, and without this a customer who backed out and then sent an
	// unrelated message within the TTL would have it silently misread as a device count. Mirrors
	// TopupCallbackHandler's same clear for the custom-GB-amount flow.
	h.deviceManageAwaitingInput.Delete(telegramID)

	h.showDevicesList(ctx, b, msg.Chat.ID, msg.ID, langCode, customer.TelegramID, customer.IsTrial)
}

// deviceBuyRow returns the "buy +1 device slot" button row, or nil when the purchase isn't
// offered (RollyPay disabled, or the customer is on a trial — mirrors TopupCallbackHandler's
// same non-trial gate).
func (h Handler) deviceBuyRow(langCode string, isTrial bool) []models.InlineKeyboardButton {
	if !config.IsRollyPayEnabled() || isTrial {
		return nil
	}
	label := fmt.Sprintf(h.translation.GetText(langCode, "device_buy_button"), config.DeviceSlotPriceRUB())
	return []models.InlineKeyboardButton{{Text: label, CallbackData: CallbackDeviceBuy}}
}

// maxPaidDeviceSlots caps DeviceManageAmountTextHandler's typed target — a safety rail against a
// fat-fingered huge number generating a huge invoice, not a business-tunable plan limit, so it's
// a plain constant rather than a config knob.
const maxPaidDeviceSlots = 20

// baseDeviceSlots is the free device allowance included in every plan (see the greeting's "До 5
// устройств на одной подписке"). Displayed as a plain constant rather than derived from
// Remnawave's live HwidDeviceLimit minus device_addons.DeviceCount: that subtraction is only as
// good as device_addons staying in perfect sync with Remnawave, and drifts on any account that's
// been hand-adjusted outside the normal buy/decrease flow (e.g. an admin panel reset) — showing a
// wrong "base" then looks like this bot doesn't know its own plan. The actual limit enforcement
// is untouched by this: buy/decrease still work in relative deltas off Remnawave's live number
// (see DeviceLimitAfterDecrease), this constant is display-only.
const baseDeviceSlots = 5

// deviceManageRow returns the "change slot count" entry-point button — same eligibility gate as
// deviceBuyRow (RollyPay enabled, not a trial), since decreasing is just as much a paid-plan-only
// concept as buying: a trial customer has no addon to decrease in the first place.
func (h Handler) deviceManageRow(langCode string, isTrial bool) []models.InlineKeyboardButton {
	if !config.IsRollyPayEnabled() || isTrial {
		return nil
	}
	return []models.InlineKeyboardButton{
		{Text: h.translation.GetText(langCode, "device_manage_button"), CallbackData: CallbackDeviceManage},
	}
}

// deviceSlotBreakdown is the single source of the paid/total/pending-note numbers shown on both
// the devices list and (in a slimmer form) the manage-slots prompt — computed once here so the
// two screens can't drift into showing different numbers for the same account.
func (h Handler) deviceSlotBreakdown(langCode string, addon *database.DeviceAddon) (paid, total int, pendingNote string) {
	if addon != nil && addon.Status != database.AddonStatusExpired {
		paid = addon.DeviceCount
	}
	total = baseDeviceSlots + paid
	if addon != nil && addon.PendingDeviceCount != nil {
		pendingNote = fmt.Sprintf(h.translation.GetText(langCode, "device_decrease_pending_note"), *addon.PendingDeviceCount)
	}
	return paid, total, pendingNote
}

func (h Handler) showDevicesList(ctx context.Context, b *bot.Bot, chatID int64, messageID int, langCode string, telegramID int64, isTrial bool) {
	rwUsers, err := h.remnawaveClient.GetUsersByTelegramID(ctx, telegramID)
	if err != nil || len(rwUsers) == 0 {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID: chatID, MessageID: messageID, ParseMode: models.ParseModeHTML,
			Text: h.translation.GetText(langCode, "devices_panel_error"),
			ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
				{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)},
			}},
		})
		return
	}
	rwUser := rwUsers[0]

	devices, err := h.remnawaveClient.GetUserHwidDevices(ctx, rwUser.UUID)
	if err != nil {
		slog.Warn("devices: get hwid failed", "error", err)
		devices = nil
	}

	addon, err := h.deviceAddonRepository.FindActiveByTelegramID(ctx, telegramID)
	if err != nil {
		slog.Warn("devices: find device addon failed", "error", err)
		addon = nil
	}

	paid, total, pendingNote := h.deviceSlotBreakdown(langCode, addon)

	if len(devices) == 0 {
		text := fmt.Sprintf(h.translation.GetText(langCode, "devices_empty"), total, paid) + pendingNote
		var rows [][]models.InlineKeyboardButton
		if config.GetMiniAppURL() != "" {
			rows = append(rows, []models.InlineKeyboardButton{
				h.translation.GetButton(langCode, "connect_button").InlineWebApp(config.GetMiniAppURL()),
			})
		}
		if row := h.deviceBuyRow(langCode, isTrial); row != nil {
			rows = append(rows, row)
		}
		if row := h.deviceManageRow(langCode, isTrial); row != nil {
			rows = append(rows, row)
		}
		rows = append(rows, []models.InlineKeyboardButton{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)})
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID: chatID, MessageID: messageID, ParseMode: models.ParseModeHTML,
			Text: text, ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: rows},
		})
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(h.translation.GetText(langCode, "devices_list_header"), total, paid, len(devices)))
	for i, d := range devices {
		sb.WriteString(fmt.Sprintf("<b>%d.</b> %s\n", i+1, buildDeviceDescription(langCode, h.translation, i, d)))
	}
	sb.WriteString(h.translation.GetText(langCode, "devices_list_footer"))
	sb.WriteString(pendingNote)

	// Buy row goes first, right under the header — otherwise it gets buried below one delete
	// button per connected device and is easy to miss once a customer has more than 1-2 devices.
	var rows [][]models.InlineKeyboardButton
	if row := h.deviceBuyRow(langCode, isTrial); row != nil {
		rows = append(rows, row)
	}
	if row := h.deviceManageRow(langCode, isTrial); row != nil {
		rows = append(rows, row)
	}
	for i, d := range devices {
		label := fmt.Sprintf(h.translation.GetText(langCode, "devices_delete_button_label"), i+1, buildDeviceShortName(langCode, h.translation, i, d))
		delCallback := fmt.Sprintf("%s?i=%d", CallbackDevicesDeleteDevice, i)
		rows = append(rows, []models.InlineKeyboardButton{{Text: label, CallbackData: delCallback}})
	}
	rows = append(rows, []models.InlineKeyboardButton{
		h.translation.GetButton(langCode, "devices_reset_button").InlineCallback(CallbackDevicesReset),
	})
	rows = append(rows, []models.InlineKeyboardButton{
		h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart),
	})

	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID: chatID, MessageID: messageID, ParseMode: models.ParseModeHTML,
		Text: sb.String(), ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: rows},
	})
}

func (h Handler) DevicesDeleteDeviceCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	telegramID := update.CallbackQuery.From.ID
	langCode := update.CallbackQuery.From.LanguageCode
	msg := update.CallbackQuery.Message.Message

	cbData := parseCallbackData(update.CallbackQuery.Data)
	idx, err := strconv.Atoi(cbData["i"])
	if err != nil {
		return
	}

	rwUsers, err := h.remnawaveClient.GetUsersByTelegramID(ctx, telegramID)
	if err != nil || len(rwUsers) == 0 {
		return
	}
	rwUser := rwUsers[0]

	devices, err := h.remnawaveClient.GetUserHwidDevices(ctx, rwUser.UUID)
	if err != nil || idx >= len(devices) {
		_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
			Text:            h.translation.GetText(langCode, "devices_list_changed"),
		})
		return
	}

	hwid := devices[idx].Hwid
	if err := h.remnawaveClient.DeleteUserHwidDevice(ctx, rwUser.UUID, hwid); err != nil {
		slog.Error("devices: delete hwid device", "error", err)
		_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
			Text:            h.translation.GetText(langCode, "devices_delete_error"),
		})
		return
	}

	_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: update.CallbackQuery.ID,
		Text:            h.translation.GetText(langCode, "devices_delete_success"),
	})

	customer, _ := h.customerRepository.FindByTelegramId(ctx, telegramID)
	if customer != nil && customer.ExpireAt != nil {
		h.showDevicesList(ctx, b, msg.Chat.ID, msg.ID, langCode, telegramID, customer.IsTrial)
	}
	slog.Info("devices: deleted device", "telegram_id", telegramID, "index", idx)
}

func (h Handler) DevicesResetCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	langCode := update.CallbackQuery.From.LanguageCode
	msg := update.CallbackQuery.Message.Message

	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID: msg.Chat.ID, MessageID: msg.ID, ParseMode: models.ParseModeHTML,
		Text: h.translation.GetText(langCode, "devices_reset_confirm"),
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

	rwUsers, err := h.remnawaveClient.GetUsersByTelegramID(ctx, telegramID)
	if err != nil || len(rwUsers) == 0 {
		return
	}

	if err := h.remnawaveClient.DeleteAllUserHwidDevices(ctx, rwUsers[0].UUID); err != nil {
		slog.Error("devices: delete all hwid", "error", err)
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID: msg.Chat.ID, MessageID: msg.ID, ParseMode: models.ParseModeHTML,
			Text: h.translation.GetText(langCode, "devices_reset_error"),
			ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
				{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)},
			}},
		})
		return
	}

	var text string
	var rows [][]models.InlineKeyboardButton
	if config.GetMiniAppURL() != "" {
		text = h.translation.GetText(langCode, "devices_reset_success_webapp")
		rows = [][]models.InlineKeyboardButton{
			{h.translation.GetButton(langCode, "connect_button").InlineWebApp(config.GetMiniAppURL())},
			{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)},
		}
	} else {
		text = h.translation.GetText(langCode, "devices_reset_success_no_url")
		rows = [][]models.InlineKeyboardButton{
			{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)},
		}
	}
	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID: msg.Chat.ID, MessageID: msg.ID, ParseMode: models.ParseModeHTML,
		Text: text, ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: rows},
	})
	slog.Info("devices: all hwid devices deleted", "telegram_id", telegramID)
}

// DeviceManageCallbackHandler is the single entry point for exact slot-count control: prompts for
// a typed target count of PAID slots. The base/total/connected breakdown already lives on the
// devices list this is opened from (deviceSlotBreakdown) — repeating it here was the exact
// "connected count shown in two places" duplication that made the old two-screen split confusing,
// so this prompt only asks the one new question: how many purchased slots to have.
func (h Handler) DeviceManageCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	telegramID := update.CallbackQuery.From.ID
	langCode := update.CallbackQuery.From.LanguageCode
	msg := update.CallbackQuery.Message.Message

	addon, err := h.deviceAddonRepository.FindActiveByTelegramID(ctx, telegramID)
	if err != nil {
		slog.Warn("device manage: find device addon failed", "error", err)
		addon = nil
	}
	paid, _, pendingNote := h.deviceSlotBreakdown(langCode, addon)

	text := fmt.Sprintf(h.translation.GetText(langCode, "device_manage_prompt"), paid, maxPaidDeviceSlots) + pendingNote

	var rows [][]models.InlineKeyboardButton
	if addon != nil && addon.PendingDeviceCount != nil {
		undoLabel := fmt.Sprintf(h.translation.GetText(langCode, "device_decrease_undo_button"), *addon.PendingDeviceCount)
		rows = append(rows, []models.InlineKeyboardButton{{Text: undoLabel, CallbackData: CallbackDeviceDecreaseUndo}})
	}
	rows = append(rows, []models.InlineKeyboardButton{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackDevices)})

	message, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID: msg.Chat.ID, MessageID: msg.ID, ParseMode: models.ParseModeHTML,
		Text: text, ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: rows},
	})
	if err != nil {
		slog.Error("device manage: edit prompt message", "error", err)
		return
	}
	h.deviceManageAwaitingInput.Set(telegramID, message.ID)
}

// DeviceManageAwaitingInput reports whether telegramID currently has an open device-slot-count
// prompt — used by main.go's RegisterHandlerMatchFunc, mirroring TopupAwaitingInput.
func (h Handler) DeviceManageAwaitingInput(telegramID int64) (int, bool) {
	return h.deviceManageAwaitingInput.Get(telegramID)
}

// DeviceManageAmountTextHandler parses the typed target paid-slot count and either queues a
// decrease (target < current — no refund, next renewal, see applyPendingDeviceDecrease in
// payment.go/webhook.go) or creates an invoice for the difference (target > current, via
// createDeviceSlotInvoice in device_purchase_handler.go). Equal to current is a no-op.
func (h Handler) DeviceManageAmountTextHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	telegramID := update.Message.From.ID
	langCode := update.Message.From.LanguageCode
	promptMessageID, ok := h.deviceManageAwaitingInput.Get(telegramID)
	if !ok {
		return
	}
	h.deviceManageAwaitingInput.Delete(telegramID)

	target, err := strconv.Atoi(strings.TrimSpace(update.Message.Text))
	if err != nil || target < 0 || target > maxPaidDeviceSlots {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID: update.Message.Chat.ID, MessageID: promptMessageID, ParseMode: models.ParseModeHTML,
			Text: fmt.Sprintf(h.translation.GetText(langCode, "device_manage_invalid"), maxPaidDeviceSlots),
			ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
				{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackDevices)},
			}},
		})
		return
	}

	customer, err := h.customerRepository.FindByTelegramId(ctx, telegramID)
	if err != nil || customer == nil {
		slog.Error("device manage: find customer", "telegram_id", telegramID, "error", err)
		return
	}

	addon, err := h.deviceAddonRepository.FindActiveByTelegramID(ctx, telegramID)
	if err != nil {
		slog.Error("device manage: find device addon", "error", err)
		return
	}
	current := 0
	if addon != nil && addon.Status != database.AddonStatusExpired {
		current = addon.DeviceCount
	}

	switch {
	case target == current:
		h.showDevicesList(ctx, b, update.Message.Chat.ID, promptMessageID, langCode, telegramID, customer.IsTrial)
	case target < current:
		if err := h.deviceAddonRepository.SetPendingDeviceCount(ctx, addon.ID, &target); err != nil {
			slog.Error("device manage: set pending count", "error", err)
			return
		}
		slog.Info("devices: queued decrease", "telegram_id", telegramID, "addon_id", addon.ID, "target_device_count", target)
		h.showDevicesList(ctx, b, update.Message.Chat.ID, promptMessageID, langCode, telegramID, customer.IsTrial)
	default: // target > current
		h.createDeviceSlotInvoice(ctx, b, update.Message.Chat.ID, promptMessageID, telegramID, langCode, customer, target-current)
	}
}

// DeviceDecreaseUndoCallbackHandler cancels a queued decrease before it takes effect.
func (h Handler) DeviceDecreaseUndoCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	telegramID := update.CallbackQuery.From.ID
	langCode := update.CallbackQuery.From.LanguageCode
	msg := update.CallbackQuery.Message.Message

	addon, err := h.deviceAddonRepository.FindActiveByTelegramID(ctx, telegramID)
	if err != nil || addon == nil {
		slog.Error("device decrease undo: no addon", "telegram_id", telegramID, "error", err)
		return
	}
	if err := h.deviceAddonRepository.SetPendingDeviceCount(ctx, addon.ID, nil); err != nil {
		slog.Error("device decrease undo: clear pending count", "error", err)
		return
	}

	customer, _ := h.customerRepository.FindByTelegramId(ctx, telegramID)
	isTrial := customer != nil && customer.IsTrial
	h.showDevicesList(ctx, b, msg.Chat.ID, msg.ID, langCode, telegramID, isTrial)
}

// buildDeviceDescription builds a full device description line for the message body.
func buildDeviceDescription(langCode string, tr *translation.Manager, idx int, d remnawave.HwidDevice) string {
	var parts []string
	if d.DeviceModel != nil && *d.DeviceModel != "" {
		parts = append(parts, *d.DeviceModel)
	}
	if d.OsVersion != nil && *d.OsVersion != "" {
		parts = append(parts, *d.OsVersion)
	} else if d.Platform != nil && *d.Platform != "" {
		parts = append(parts, *d.Platform)
	}
	if len(parts) == 0 {
		return fmt.Sprintf(tr.GetText(langCode, "devices_label_numbered"), idx+1)
	}
	return "📱 " + strings.Join(parts, " · ")
}

// buildDeviceShortName returns a short name for use in a button label.
func buildDeviceShortName(langCode string, tr *translation.Manager, idx int, d remnawave.HwidDevice) string {
	if d.DeviceModel != nil && *d.DeviceModel != "" {
		name := *d.DeviceModel
		if len(name) > 20 {
			name = name[:20] + "…"
		}
		return name
	}
	if d.Platform != nil && *d.Platform != "" {
		return *d.Platform
	}
	return fmt.Sprintf(tr.GetText(langCode, "devices_label_short"), idx+1)
}
