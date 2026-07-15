package handler

import (
	"context"
	"log/slog"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/translation"
)

// sendAdminPanel sends a single message with a single InlineWebApp button opening the admin web
// panel. Everything the old bot-native admin panel used to do (stats, user lookup, broadcast,
// per-user actions) now lives exclusively in the web app (internal/webapp) backed by
// internal/adminops — this handler is just the door into it.
//
// When ADMIN_WEBAPP_URL isn't configured, staying silent would leave the admin staring at a "/admin"
// command that appears to do nothing; instead we explain what's missing and point at /sync as the
// one remaining bot-native fallback.
func sendAdminPanel(ctx context.Context, b *bot.Bot, chatID int64) {
	webAppURL := config.AdminWebAppURL()
	if webAppURL == "" {
		_, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text: "⚠️ <b>Веб-панель администратора не настроена</b>\n\n" +
				"Задайте переменную окружения ADMIN_WEBAPP_URL, чтобы открыть панель отсюда.\n\n" +
				"Пока панель недоступна, единственная резервная команда — /sync (синхронизация пользователей с Vexel VPN).",
			ParseMode: models.ParseModeHTML,
		})
		if err != nil {
			slog.Error("admin panel: send unconfigured notice", "error", err)
		}
		return
	}

	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      "🛠 <b>Панель администратора</b>",
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{translation.ButtonData{Text: "🌐 Открыть панель"}.InlineWebApp(webAppURL)},
			},
		},
	})
	if err != nil {
		slog.Error("admin panel: send menu", "error", err)
	}
}

// AdminMenuCommandHandler handles /admin — opens the admin web panel.
func (h Handler) AdminMenuCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	sendAdminPanel(ctx, b, update.Message.Chat.ID)
}
