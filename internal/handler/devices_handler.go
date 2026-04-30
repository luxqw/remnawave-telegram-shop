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
			Text: "❌ Не удалось получить данные из панели. Попробуйте позже.",
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

	if len(devices) == 0 {
		text := fmt.Sprintf("📱 <b>Мои устройства</b>\n\n📅 Подписка до: <b>%s</b>\n\nНет подключённых устройств.\nПодключите устройство через кнопку «Подключиться».", expireDate)
		var rows [][]models.InlineKeyboardButton
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

	// Build message with device list as text
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📱 <b>Мои устройства</b>\n\n📅 Подписка до: <b>%s</b>\nПодключено: <b>%d</b>\n\n", expireDate, len(devices)))
	for i, d := range devices {
		sb.WriteString(fmt.Sprintf("<b>%d.</b> %s\n", i+1, buildDeviceDescription(d)))
	}
	sb.WriteString("\nНажмите кнопку устройства ниже, чтобы удалить его:")

	// One delete button per device
	var rows [][]models.InlineKeyboardButton
	for i, d := range devices {
		label := fmt.Sprintf("🗑️ Удалить #%d — %s", i+1, buildDeviceShortName(d))
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
			Text:            "❌ Список устройств изменился, обновите страницу",
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

// buildDeviceDescription builds a full device description line for the message body.
func buildDeviceDescription(d remnawave.HwidDevice) string {
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
		short := d.Hwid
		if len(short) > 16 {
			short = short[:16] + "..."
		}
		return "📱 " + short
	}
	return "📱 " + strings.Join(parts, " · ")
}

// buildDeviceShortName returns a short name for use in a button label.
func buildDeviceShortName(d remnawave.HwidDevice) string {
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
	if len(d.Hwid) > 8 {
		return d.Hwid[:8] + "…"
	}
	return d.Hwid
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
