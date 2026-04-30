package handler

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
)

func (h Handler) AdminUserCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 2 {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Usage: /admin_user <telegram_id>")
		return
	}
	targetID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Invalid telegram_id")
		return
	}
	customer, err := h.customerRepository.FindByTelegramId(ctx, targetID)
	if err != nil {
		sendAdminReply(ctx, b, update.Message.Chat.ID, fmt.Sprintf("DB error: %v", err))
		return
	}
	if customer == nil {
		sendAdminReply(ctx, b, update.Message.Chat.ID, fmt.Sprintf("User %d not found", targetID))
		return
	}
	msg := fmt.Sprintf("👤 <b>User:</b> <code>%d</code>\n🌐 <b>Lang:</b> %s\n", targetID, customer.Language)
	if customer.IsTrial {
		msg += "🎁 <b>Trial user</b>\n"
	}
	if customer.ExpireAt != nil {
		status := "✅ активна"
		if customer.ExpireAt.Before(time.Now()) {
			status = "❌ истекла"
		}
		msg += fmt.Sprintf("📅 <b>Подписка:</b> %s до <b>%s</b> (%s)\n", status, customer.ExpireAt.Format("02.01.2006 15:04"), formatTimeUntil(*customer.ExpireAt))
	} else {
		msg += "📅 <b>Подписка:</b> отсутствует\n"
	}
	if customer.SubscriptionLink != nil {
		msg += fmt.Sprintf("🔗 <b>Sub:</b> <code>%s</code>\n", *customer.SubscriptionLink)
	}
	rwUsers, err := h.remnawaveClient.GetUsersByTelegramID(ctx, targetID)
	if err != nil {
		msg += fmt.Sprintf("\n⚠️ Remnawave error: %v", err)
	} else if len(rwUsers) == 0 {
		msg += "\n⚠️ Remnawave: не найден"
	} else {
		u := rwUsers[0]
		limitGB := u.TrafficLimitBytes / config.BytesInGigabyte()
		msg += fmt.Sprintf("\n🌐 <b>RW status:</b> %s\n📊 <b>Limit:</b> %d GB\n📅 <b>RW expire:</b> %s\n🆔 <code>%s</code>",
			u.Status, limitGB, u.ExpireAt.Format("02.01.2006 15:04"), u.UUID)
	}
	sendAdminReply(ctx, b, update.Message.Chat.ID, msg)
}

func (h Handler) AdminTopupCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 3 {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Usage: /admin_topup <telegram_id> <gb>")
		return
	}
	targetID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Invalid telegram_id")
		return
	}
	gb, err := strconv.Atoi(parts[2])
	if err != nil || gb <= 0 {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Invalid gb amount")
		return
	}
	rwUsers, err := h.remnawaveClient.GetUsersByTelegramID(ctx, targetID)
	if err != nil || len(rwUsers) == 0 {
		sendAdminReply(ctx, b, update.Message.Chat.ID, fmt.Sprintf("Remnawave user not found for %d", targetID))
		return
	}
	u := rwUsers[0]
	newLimit := u.TrafficLimitBytes + gb*config.BytesInGigabyte()
	if err := h.remnawaveClient.UpdateUserTrafficLimit(ctx, u.UUID, newLimit, config.TrafficLimitResetStrategy()); err != nil {
		sendAdminReply(ctx, b, update.Message.Chat.ID, fmt.Sprintf("Failed: %v", err))
		return
	}
	sendAdminReply(ctx, b, update.Message.Chat.ID, fmt.Sprintf("✅ +%d GB для %d\nНовый лимит: %d GB", gb, targetID, newLimit/config.BytesInGigabyte()))
	slog.Info("admin topup: granted", "telegram_id", targetID, "gb", gb)
}

func (h Handler) AdminResetDevicesCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 2 {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Usage: /admin_reset_devices <telegram_id>")
		return
	}
	targetID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Invalid telegram_id")
		return
	}
	rwUsers, err := h.remnawaveClient.GetUsersByTelegramID(ctx, targetID)
	if err != nil || len(rwUsers) == 0 {
		sendAdminReply(ctx, b, update.Message.Chat.ID, fmt.Sprintf("Remnawave user not found for %d", targetID))
		return
	}
	if err := h.remnawaveClient.DeleteAllUserHwidDevices(ctx, rwUsers[0].UUID); err != nil {
		sendAdminReply(ctx, b, update.Message.Chat.ID, fmt.Sprintf("Failed: %v", err))
		return
	}
	sendAdminReply(ctx, b, update.Message.Chat.ID, fmt.Sprintf("✅ Все устройства пользователя %d отключены (HWID очищен)", targetID))
}

func (h Handler) AdminBroadcastCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	parts := strings.SplitN(update.Message.Text, " ", 2)
	if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Usage: /admin_broadcast <message HTML>")
		return
	}
	text := strings.TrimSpace(parts[1])
	customers, err := h.customerRepository.FindAll(ctx)
	if err != nil {
		sendAdminReply(ctx, b, update.Message.Chat.ID, fmt.Sprintf("DB error: %v", err))
		return
	}
	sent, failed := 0, 0
	now := time.Now()
	for _, customer := range customers {
		if customer.ExpireAt == nil || customer.ExpireAt.Before(now) {
			continue
		}
		_, err := b.SendMessage(ctx, &bot.SendMessageParams{ChatID: customer.TelegramID, Text: text, ParseMode: models.ParseModeHTML})
		if err != nil {
			failed++
		} else {
			sent++
		}
		time.Sleep(40 * time.Millisecond)
	}
	sendAdminReply(ctx, b, update.Message.Chat.ID, fmt.Sprintf("✅ Рассылка завершена\nОтправлено: <b>%d</b>\nОшибок: <b>%d</b>", sent, failed))
	slog.Info("admin broadcast: done", "sent", sent, "failed", failed)
}


// AdminBroadcastTestCommandHandler handles /admin_broadcast_test <message>
// Sends the broadcast message only to the admin for preview.
func (h Handler) AdminBroadcastTestCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	parts := strings.SplitN(update.Message.Text, " ", 2)
	if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Usage: /admin_broadcast_test <message HTML>")
		return
	}
	text := strings.TrimSpace(parts[1])
	preview := "🧪 <b>Preview (только ты видишь это):</b>\n\n" + text
	sendAdminReply(ctx, b, update.Message.Chat.ID, preview)
}
func countActive(customers []database.Customer) int {
	now := time.Now()
	n := 0
	for _, c := range customers {
		if c.ExpireAt != nil && c.ExpireAt.After(now) {
			n++
		}
	}
	return n
}

func sendAdminReply(ctx context.Context, b *bot.Bot, chatID int64, text string) {
	_, err := b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text, ParseMode: models.ParseModeHTML})
	if err != nil {
		slog.Error("admin reply: send message", "error", err)
	}
}
