package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remnawave-tg-shop-bot/internal/adminops"
	"remnawave-tg-shop-bot/internal/config"
)

func (h Handler) AdminUserCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 2 {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Usage: /admin_user &lt;telegram_id&gt;")
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
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Usage: /admin_topup &lt;telegram_id&gt; &lt;gb&gt;\nGB can be negative to subtract traffic.")
		return
	}
	targetID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Invalid telegram_id")
		return
	}
	gb, err := strconv.Atoi(parts[2])
	if err != nil || gb == 0 {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Invalid gb amount (must be non-zero)")
		return
	}
	chatID := update.Message.Chat.ID
	newLimitGB, ok := h.previewTopupNewLimitGB(ctx, b, chatID, targetID, gb)
	if !ok {
		return
	}
	sign := "+"
	if gb < 0 {
		sign = ""
	}
	h.requestAdminConfirmation(ctx, b, chatID, targetID, "topup", gb, "command",
		fmt.Sprintf("⚠️ Подтвердите: %s%d GB для пользователя <code>%d</code>?\nНовый лимит: %d GB",
			sign, gb, targetID, newLimitGB/int64(config.BytesInGigabyte())))
}

func (h Handler) AdminTopupEnrollCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 2 {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Usage: /admin_topup_enroll &lt;telegram_id&gt;")
		return
	}
	targetID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Invalid telegram_id")
		return
	}

	result, err := h.adminOps.TopupEnroll(ctx, targetID, "command")
	if err != nil {
		if errors.Is(err, adminops.ErrRemnawaveUserNotFound) {
			sendAdminReply(ctx, b, update.Message.Chat.ID, fmt.Sprintf("Remnawave user not found for %d", targetID))
		} else {
			sendAdminReply(ctx, b, update.Message.Chat.ID, fmt.Sprintf("DB error: %v", err))
		}
		return
	}

	switch {
	case result.AlreadyBase:
		sendAdminReply(ctx, b, update.Message.Chat.ID,
			fmt.Sprintf("ℹ️ Пользователь %d имеет базовый или меньший лимит (%d GB). Topup не нужен.",
				targetID, result.CurrentLimitGB))
	case result.AlreadyEnrolled:
		sendAdminReply(ctx, b, update.Message.Chat.ID,
			fmt.Sprintf("ℹ️ Пользователь %d уже в системе (последний target: %d GB).\nЧтобы изменить — используй /admin_topup.", targetID, result.ExistingTargetGB))
	default:
		sendAdminReply(ctx, b, update.Message.Chat.ID,
			fmt.Sprintf("✅ Пользователь %d зачислён в систему topup\nЛимит: %d GB (базовый %d GB + %d GB extra)",
				targetID, result.CurrentLimitGB, result.BaseLimitGB, result.DeltaGB))
		slog.Info("admin topup enroll", "telegram_id", targetID, "delta_gb", result.DeltaGB)
	}
}

func (h Handler) AdminResetDevicesCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 2 {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Usage: /admin_reset_devices &lt;telegram_id&gt;")
		return
	}
	targetID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Invalid telegram_id")
		return
	}
	chatID := update.Message.Chat.ID
	h.requestAdminConfirmation(ctx, b, chatID, targetID, "reset_devices", 0, "command",
		fmt.Sprintf("⚠️ Подтвердите: СБРОСИТЬ устройства пользователя <code>%d</code>?", targetID))
}

// broadcastWaitingForText is the sentinel stored in broadcastSessions when the admin has
// initiated a broadcast but hasn't sent the message text yet.
const broadcastWaitingForText = "\x00waiting"

// AdminBroadcastCommandHandler starts the two-step broadcast dialog.
func (h Handler) AdminBroadcastCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := update.Message.Chat.ID
	h.adminSessions.Delete(chatID)
	h.broadcastSessions.Store(chatID, broadcastWaitingForText)
	sendAdminReply(ctx, b, chatID, "📢 <b>Рассылка</b>\n\nОтправь текст рассылки (поддерживается HTML-форматирование).\n\nДля отмены: /cancel")
}

// IsBroadcastTextPending returns true when the admin started a broadcast session and the bot
// is waiting for the message text. Used by the match func registered in main.go.
func (h Handler) IsBroadcastTextPending(chatID int64) bool {
	val, ok := h.broadcastSessions.Load(chatID)
	if !ok {
		return false
	}
	s, _ := val.(string)
	return s == broadcastWaitingForText
}

// AdminBroadcastTextHandler captures the admin's message text, stores it, and shows a preview
// with action buttons. Triggered by the match func registered in main.go.
func (h Handler) AdminBroadcastTextHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := update.Message.Chat.ID
	text := update.Message.Text
	h.broadcastSessions.Store(chatID, text)

	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      "📋 <b>Предпросмотр рассылки:</b>\n\n" + text + "\n\n━━━━━━━━━━━━━━\nВыбери действие:",
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					{Text: "✅ Активным", CallbackData: CallbackBroadcastConfirm},
					{Text: "🕓 Истёкшим", CallbackData: CallbackBroadcastConfirmExpired},
				},
				{
					{Text: "🆕 Не покупавшим", CallbackData: CallbackBroadcastConfirmNew},
					{Text: "💤 Неактивным", CallbackData: CallbackBroadcastConfirmInactive},
				},
				{
					{Text: "👥 Всем", CallbackData: CallbackBroadcastConfirmAll},
				},
				{
					{Text: "🧪 Только мне", CallbackData: CallbackBroadcastTest},
				},
				{
					{Text: "❌ Отменить", CallbackData: CallbackBroadcastCancel},
				},
			},
		},
	})
	if err != nil {
		slog.Error("broadcast: send preview", "error", err)
	}
}

// AdminBroadcastConfirmCallback sends the stored message to all active subscribers
// (ExpireAt set and not yet passed).
func (h Handler) AdminBroadcastConfirmCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	h.runBroadcast(ctx, b, update, "active")
}

// AdminBroadcastConfirmExpiredCallback sends the stored message to subscribers whose
// subscription has expired (ExpireAt set and already in the past). Users who never had a
// subscription (ExpireAt == nil) are excluded.
func (h Handler) AdminBroadcastConfirmExpiredCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	h.runBroadcast(ctx, b, update, "expired")
}

// AdminBroadcastConfirmInactiveCallback sends the stored message to every customer who is not a
// current active subscriber: expired subscriptions (ExpireAt in the past) plus customers who
// never subscribed (ExpireAt == nil).
func (h Handler) AdminBroadcastConfirmInactiveCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	h.runBroadcast(ctx, b, update, "inactive")
}

// AdminBroadcastConfirmNewCallback sends the stored message only to customers who never had a
// subscription (ExpireAt == nil).
func (h Handler) AdminBroadcastConfirmNewCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	h.runBroadcast(ctx, b, update, "new")
}

// AdminBroadcastConfirmAllCallback sends the stored message to every customer in the database,
// regardless of subscription state (active, expired, or never subscribed).
func (h Handler) AdminBroadcastConfirmAllCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	h.runBroadcast(ctx, b, update, "all")
}

// runBroadcast validates the admin session and kicks off delivery via adminops.Service.RunBroadcast
// (which resolves the audience and delivers in the background). Answering the callback
// immediately and polling job status in a goroutine avoids Telegram's "query is too old" error
// from a long synchronous send loop, and keeps the admin UI responsive.
func (h Handler) runBroadcast(ctx context.Context, b *bot.Bot, update *models.Update, segment string) {
	if update.CallbackQuery == nil || update.CallbackQuery.From.ID != config.GetAdminTelegramId() {
		return
	}
	chatID := update.CallbackQuery.From.ID
	val, ok := h.broadcastSessions.LoadAndDelete(chatID)
	if !ok {
		return
	}
	text, _ := val.(string)
	if text == "" || text == broadcastWaitingForText {
		return
	}

	count, err := h.adminOps.PreviewBroadcast(ctx, segment)
	if err != nil {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("DB error: %v", err))
		return
	}
	jobID, err := h.adminOps.RunBroadcast(ctx, text, segment, "panel")
	if err != nil {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("Ошибка запуска рассылки: %v", err))
		return
	}

	sendAdminReply(ctx, b, chatID, fmt.Sprintf("🚀 Рассылка запущена\nПолучателей: <b>%d</b>\nРезультат пришлю по завершении.", count))

	// Detach from the request context so polling isn't cancelled when the handler returns.
	bgCtx := context.WithoutCancel(ctx)
	go h.reportBroadcastCompletion(bgCtx, b, chatID, jobID)
}

// reportBroadcastCompletion polls adminops for job progress and sends the admin a completion
// summary once delivery finishes, preserving the bot's original synchronous-looking UX even
// though delivery now happens inside adminops.
func (h Handler) reportBroadcastCompletion(ctx context.Context, b *bot.Bot, chatID int64, jobID string) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			progress, ok := h.adminOps.BroadcastStatus(jobID)
			if !ok {
				return
			}
			if !progress.Done {
				continue
			}
			sendAdminReply(ctx, b, chatID, fmt.Sprintf(
				"✅ Рассылка завершена\nОтправлено: <b>%d</b>\nОшибок: <b>%d</b>\n• недоступны/заблокировали: %d\n• прочее: %d",
				progress.Sent, progress.Failed, progress.Unreachable, progress.OtherFailed))
			return
		}
	}
}

// AdminBroadcastTestCallback sends the stored message only to the admin.
// The session is kept alive so the admin can confirm a real send afterward.
func (h Handler) AdminBroadcastTestCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil || update.CallbackQuery.From.ID != config.GetAdminTelegramId() {
		return
	}
	chatID := update.CallbackQuery.From.ID
	val, ok := h.broadcastSessions.Load(chatID)
	if !ok {
		return
	}
	text, _ := val.(string)
	if text == "" || text == broadcastWaitingForText {
		return
	}
	sendAdminReply(ctx, b, chatID, "🧪 <b>Тест (только ты видишь):</b>\n\n"+text)
}

// AdminBroadcastCancelCallback cancels the active broadcast session.
func (h Handler) AdminBroadcastCancelCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil || update.CallbackQuery.From.ID != config.GetAdminTelegramId() {
		return
	}
	chatID := update.CallbackQuery.From.ID
	h.broadcastSessions.Delete(chatID)
	sendAdminReply(ctx, b, chatID, "❌ Рассылка отменена.")
}

// AdminCancelCommandHandler handles /cancel — cancels any active admin dialog state.
func (h Handler) AdminCancelCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := update.Message.Chat.ID
	_, bcast := h.broadcastSessions.LoadAndDelete(chatID)
	_, panel := h.adminSessions.LoadAndDelete(chatID)
	if bcast || panel {
		sendAdminReply(ctx, b, chatID, "❌ Отменено.")
	} else {
		sendAdminReply(ctx, b, chatID, "Нечего отменять.")
	}
}

func (h Handler) AdminExtendCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 3 {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Usage: /admin_extend &lt;telegram_id&gt; &lt;days&gt;")
		return
	}
	targetID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Invalid telegram_id")
		return
	}
	days, err := strconv.Atoi(parts[2])
	if err != nil || days <= 0 {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Invalid days (must be positive)")
		return
	}
	h.execExtend(ctx, b, update.Message.Chat.ID, targetID, days, "command")
}

func (h Handler) AdminDisableCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	h.requestSetRemnawaveStatus(ctx, b, update, "DISABLED")
}

func (h Handler) AdminEnableCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	h.requestSetRemnawaveStatus(ctx, b, update, "ACTIVE")
}

// requestSetRemnawaveStatus parses the target and shows a confirmation prompt; the actual status
// change happens in execSetStatus once the admin confirms.
func (h Handler) requestSetRemnawaveStatus(ctx context.Context, b *bot.Bot, update *models.Update, status string) {
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 2 {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Usage: /admin_disable or /admin_enable &lt;telegram_id&gt;")
		return
	}
	targetID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Invalid telegram_id")
		return
	}
	chatID := update.Message.Chat.ID
	action, verb := "disable", "ОТКЛЮЧИТЬ"
	if status == "ACTIVE" {
		action, verb = "enable", "ВКЛЮЧИТЬ"
	}
	h.requestAdminConfirmation(ctx, b, chatID, targetID, action, 0, "command",
		fmt.Sprintf("⚠️ Подтвердите: %s пользователя <code>%d</code>?", verb, targetID))
}

func (h Handler) AdminResetTrafficCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 2 {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Usage: /admin_reset_traffic &lt;telegram_id&gt;")
		return
	}
	targetID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Invalid telegram_id")
		return
	}
	chatID := update.Message.Chat.ID
	h.requestAdminConfirmation(ctx, b, chatID, targetID, "reset_traffic", 0, "command",
		fmt.Sprintf("⚠️ Подтвердите: СБРОСИТЬ трафик пользователя <code>%d</code>?", targetID))
}

func (h Handler) AdminStatsCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	customers, err := h.customerRepository.FindAll(ctx)
	if err != nil {
		sendAdminReply(ctx, b, update.Message.Chat.ID, fmt.Sprintf("DB error: %v", err))
		return
	}
	now := time.Now()
	var activePaid, activeTrial, expired, noSub int
	for _, c := range customers {
		if c.ExpireAt == nil {
			noSub++
			continue
		}
		if c.ExpireAt.Before(now) {
			expired++
		} else if c.IsTrial {
			activeTrial++
		} else {
			activePaid++
		}
	}
	msg := fmt.Sprintf("Статистика бота\n\nВсего: %d\nАктивных подписок: %d\nТриалов: %d\nИстёкших: %d\nБез подписки: %d",
		len(customers), activePaid, activeTrial, expired, noSub)
	sendAdminReply(ctx, b, update.Message.Chat.ID, msg)
}

// AdminReferralsCommandHandler handles /admin_referrals — a global overview of the referral
// program across all customers: total invites, how many converted to a granted bonus, and the
// most recent referral events (referrer -> referee, date, status).
func (h Handler) AdminReferralsCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := update.Message.Chat.ID
	total, err := h.referralRepository.CountAll(ctx)
	if err != nil {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("DB error: %v", err))
		return
	}
	if total == 0 {
		sendAdminReply(ctx, b, chatID, "👥 <b>Рефералы</b>\n\nПока никто никого не пригласил.")
		return
	}
	granted, err := h.referralRepository.CountAllGranted(ctx)
	if err != nil {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("DB error: %v", err))
		return
	}

	const recentLimit = 20
	recent, err := h.referralRepository.FindRecent(ctx, recentLimit)
	if err != nil {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("DB error: %v", err))
		return
	}

	msg := fmt.Sprintf("👥 <b>Рефералы</b>\n\nВсего приглашений: <b>%d</b>\nБонус начислен: <b>%d</b>\nКонверсия: <b>%.0f%%</b>\n\n<b>Последние %d:</b>\n",
		total, granted, float64(granted)/float64(total)*100, len(recent))
	for _, ref := range recent {
		icon := "⏳"
		if ref.BonusGranted {
			icon = "✅"
		}
		msg += fmt.Sprintf("%s <code>%d</code> → <code>%d</code>  %s\n", icon, ref.ReferrerID, ref.RefereeID, ref.UsedAt.Format("02.01.2006"))
	}
	msg += "\nПодробности по конкретному рефереру: /admin_user &lt;telegram_id&gt;"
	sendAdminReply(ctx, b, chatID, msg)
}

func pluralDays(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return "день"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20):
		return "дня"
	default:
		return "дней"
	}
}

// AdminMenuCommandHandler handles /admin — opens the interactive admin panel.
func (h Handler) AdminMenuCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	sendAdminPanel(ctx, b, update.Message.Chat.ID)
}

// AdminSetTrialCommandHandler handles /admin_set_trial <id> <on|off>
// Toggles the is_trial flag for a user in the bot database.
func (h Handler) AdminSetTrialCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 3 {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Usage: /admin_set_trial &lt;telegram_id&gt; &lt;on|off&gt;")
		return
	}
	targetID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "❌ Invalid telegram_id")
		return
	}
	var isTrial bool
	switch strings.ToLower(parts[2]) {
	case "on", "true", "1":
		isTrial = true
	case "off", "false", "0":
		isTrial = false
	default:
		sendAdminReply(ctx, b, update.Message.Chat.ID, "❌ Use: on or off")
		return
	}

	if err := h.adminOps.SetTrial(ctx, targetID, isTrial, "command"); err != nil {
		if errors.Is(err, adminops.ErrCustomerNotFound) {
			sendAdminReply(ctx, b, update.Message.Chat.ID, fmt.Sprintf("❌ User %d not found", targetID))
		} else {
			sendAdminReply(ctx, b, update.Message.Chat.ID, fmt.Sprintf("❌ DB error: %v", err))
		}
		return
	}

	status := "оплачен"
	if isTrial {
		status = "триал"
	}
	sendAdminReply(ctx, b, update.Message.Chat.ID,
		fmt.Sprintf("✅ Пользователь %d → <b>%s</b>\n\nТеперь:\n• Докупить трафик: %v\n• Мои устройства: видит", targetID, status, !isTrial))
}

// AdminAuditCommandHandler handles /admin_audit <telegram_id> — shows the last 20 audit-logged
// actions taken against that user (enable/disable, traffic/device resets, topups).
func (h Handler) AdminAuditCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	parts := strings.Fields(update.Message.Text)
	if len(parts) < 2 {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Usage: /admin_audit &lt;telegram_id&gt;")
		return
	}
	targetID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		sendAdminReply(ctx, b, update.Message.Chat.ID, "Invalid telegram_id")
		return
	}
	entries, err := h.auditLogRepository.FindRecentByTarget(ctx, targetID, 20)
	if err != nil {
		sendAdminReply(ctx, b, update.Message.Chat.ID, fmt.Sprintf("DB error: %v", err))
		return
	}
	if len(entries) == 0 {
		sendAdminReply(ctx, b, update.Message.Chat.ID, fmt.Sprintf("📜 Нет записей аудита для %d", targetID))
		return
	}
	msg := fmt.Sprintf("📜 <b>Аудит для %d</b> (последние %d):\n\n", targetID, len(entries))
	for _, e := range entries {
		outcomeIcon := "✅"
		if e.Outcome != "success" {
			outcomeIcon = "❌"
		}
		line := fmt.Sprintf("%s %s — <b>%s</b> (%s)", e.CreatedAt.Format("02.01.2006 15:04"), outcomeIcon, e.Action, e.Source)
		if e.ParamInt != nil {
			line += fmt.Sprintf(" [%d]", *e.ParamInt)
		}
		if e.ErrorMessage != nil {
			line += fmt.Sprintf(" — %s", *e.ErrorMessage)
		}
		msg += line + "\n"
	}
	sendAdminReply(ctx, b, update.Message.Chat.ID, msg)
}

func sendAdminReply(ctx context.Context, b *bot.Bot, chatID int64, text string) {
	_, err := b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text, ParseMode: models.ParseModeHTML})
	if err != nil {
		slog.Error("admin reply: send message", "error", err)
	}
}
