package handler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"strconv"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/remnawave"
)

func (h Handler) DevicesCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	telegramID := update.CallbackQuery.From.ID
	langCode := update.CallbackQuery.From.LanguageCode
	msg := update.CallbackQuery.Message.Message

	customer, err := h.customerRepository.FindByTelegramId(ctx, telegramID)
	if err != nil || customer == nil || customer.SubscriptionLink == nil || customer.ExpireAt == nil {
		return
	}

	h.showDevicesList(ctx, b, msg.Chat.ID, msg.ID, langCode, customer.TelegramID, *customer.ExpireAt)
}

func (h Handler) showDevicesList(ctx context.Context, b *bot.Bot, chatID int64, messageID int, langCode string, telegramID int64, expireAt time.Time) {
	rwUsers, err := h.remnawaveClient.GetUsersByTelegramID(ctx, telegramID)
	if err != nil || len(rwUsers) == 0 {
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID: chatID, MessageID: messageID, ParseMode: models.ParseModeHTML,
			Text: h.translation.GetText(langCode, "devices_error"),
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

	expireDate := expireAt.Format("02.01.2006")
	var rows [][]models.InlineKeyboardButton

	if len(devices) == 0 {
		text := fmt.Sprintf(h.translation.GetText(langCode, "devices_empty"), expireDate)
		if config.GetMiniAppURL() != "" {
			rows = append(rows, []models.InlineKeyboardButton{
				h.translation.GetButton(langCode, "connect_button").InlineWebApp(config.GetMiniAppURL()),
			})
		}
		rows = append(rows, []models.InlineKeyboardButton{h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart)})
		_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID: chatID, MessageID: messageID, ParseMode: models.ParseModeHTML,
			Text: text, ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: rows},
		})
		return
	}

	text := fmt.Sprintf(h.translation.GetText(langCode, "devices_list_header"), expireDate, len(devices))
	for i, d := range devices {
		label := buildDeviceLabel(d)
		// Use index in callback data to avoid 64-byte Telegram limit on HWID strings
		delCallback := fmt.Sprintf("%s?i=%d", CallbackDevicesDeleteDevice, i)
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: label, CallbackData: "noop"},
			{Text: "🗑️", CallbackData: delCallback},
		})
	}
	rows = append(rows, []models.InlineKeyboardButton{
		h.translation.GetButton(langCode, "devices_reset_button").InlineCallback(CallbackDevicesReset),
	})
	rows = append(rows, []models.InlineKeyboardButton{
		h.translation.GetButton(langCode, "back_button").InlineCallback(CallbackStart),
	})

	_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID: chatID, MessageID: messageID, ParseMode: models.ParseModeHTML,
		Text: text, ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: rows},
	})
}

func (h Handler) DevicesDeleteDeviceCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	telegramID := update.CallbackQuery.From.ID
	langCode := update.CallbackQuery.From.LanguageCode
	msg := update.CallbackQuery.Message.Message

	cbData := parseCallbackData(update.CallbackQuery.Data)
	idxStr := cbData["i"]
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		return
	}

	rwUsers, err := h.remnawaveClient.GetUsersByTelegramID(ctx, telegramID)
	if err != nil || len(rwUsers) == 0 {
		return
	}
	rwUser := rwUsers[0]

	// Fetch fresh device list - use index to identify device
	devices, err := h.remnawaveClient.GetUserHwidDevices(ctx, rwUser.UUID)
	if err != nil || idx >= len(devices) {
		_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
			Text:            "❌ Устройство не найдено (список обновился)",
		})
		return
	}

	hwid := devices[idx].Hwid
	if err := h.remnawaveClient.DeleteUserHwidDevice(ctx, rwUser.UUID, hwid); err != nil {
		slog.Error("devices: delete hwid device", "error", err)
		_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
			Text:            "❌ Ошибка удаления",
		})
		return
	}

	_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: update.CallbackQuery.ID,
		Text:            "✅ Устройство удалено",
	})

	customer, _ := h.customerRepository.FindByTelegramId(ctx, telegramID)
	if customer != nil && customer.ExpireAt != nil {
		h.showDevicesList(ctx, b, msg.Chat.ID, msg.ID, langCode, telegramID, *customer.ExpireAt)
	}
	slog.Info("devices: deleted hwid device", "telegram_id", telegramID, "hwid_prefix", hwid[:min(8, len(hwid))])
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
	rwUser := rwUsers[0]

	if err := h.remnawaveClient.DeleteAllUserHwidDevices(ctx, rwUser.UUID); err != nil {
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

func buildDeviceLabel(d remnawave.HwidDevice) string {
	var parts []string
	if d.DeviceModel != nil && *d.DeviceModel != "" {
		parts = append(parts, *d.DeviceModel)
	}
	if d.Platform != nil && *d.Platform != "" {
		parts = append(parts, *d.Platform)
	}
	if len(parts) == 0 {
		short := d.Hwid
		if len(short) > 12 {
			short = short[:12] + "..."
		}
		return "📱 " + short
	}
	return "📱 " + strings.Join(parts, " ")
}

func formatTimeUntil(t time.Time) string {
	d := time.Until(t)
	if d < 0 {
		return "истекла"
	}
	days := int(d.Hours() / 24)
	if days == 0 {
		return "сегодня"
	}
	return fmt.Sprintf("через %d дн.", days)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}