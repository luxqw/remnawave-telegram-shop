package handler

import (
	"context"
	"log/slog"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func (h Handler) SyncUsersCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := update.Message.Chat.ID
	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      "⏳ Синхронизация запущена...",
		ParseMode: models.ParseModeHTML,
	})
	if err != nil {
		slog.Error("Error sending sync start message", "error", err)
	}

	go func() {
		h.syncService.Sync()
		bgCtx := context.Background()
		_, err := b.SendMessage(bgCtx, &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      "✅ Синхронизация завершена",
			ParseMode: models.ParseModeHTML,
		})
		if err != nil {
			slog.Error("Error sending sync complete message", "error", err)
		}
	}()
}