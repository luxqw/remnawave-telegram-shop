package handler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
)

// IsCandidateAdminReply reports whether update looks like a customer's free-text reply that
// should be captured into the admin message thread (decision 13a) — not a command, not from the
// admin's own account. Exported so main.go's handler registration can use it directly as the
// RegisterHandlerMatchFunc predicate without duplicating the logic.
func IsCandidateAdminReply(update *models.Update) bool {
	if update.Message == nil || update.Message.Text == "" {
		return false
	}
	if strings.HasPrefix(update.Message.Text, "/") {
		return false
	}
	return update.Message.From.ID != config.GetAdminTelegramId()
}

// AdminReplyMessageHandler persists a customer's free-text message as the "in" side of their
// admin_messages thread and pings the admin, giving the one-directional SendMessage a way back
// (decision 13a). Registered last among the text-message handlers in main.go — TopupAwaitingInput
// and any other specific awaiting-input flow already claims the update first via first-match-wins
// dispatch, so this only ever sees messages nothing else wanted.
func (h Handler) AdminReplyMessageHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	telegramID := update.Message.From.ID
	text := strings.TrimSpace(update.Message.Text)
	if text == "" || h.adminMessageRepository == nil {
		return
	}

	if _, err := h.adminMessageRepository.Create(ctx, &database.AdminMessage{
		CustomerTelegramID: telegramID, Direction: database.MessageDirectionIn, Text: text,
	}); err != nil {
		slog.Error("admin reply: persist inbound message failed", "telegram_id", telegramID, "error", err)
		return
	}

	username := update.Message.From.Username
	label := fmt.Sprintf("%d", telegramID)
	if username != "" {
		label = "@" + username
	}
	preview := text
	if len(preview) > 200 {
		preview = preview[:200] + "…"
	}
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: config.GetAdminTelegramId(),
		Text:   fmt.Sprintf("💬 Новое сообщение от %s:\n\n%s", label, preview),
	}); err != nil {
		slog.Error("admin reply: notify admin failed", "telegram_id", telegramID, "error", err)
	}
}
