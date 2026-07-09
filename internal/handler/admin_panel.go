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

const (
	adminStepWaitUserID = "uid"
	adminStepWaitGB     = "gb"
	adminStepWaitDays   = "days"
	adminStepConfirm    = "confirm"
)

const adminConfirmExpiry = 5 * time.Minute

type adminSession struct {
	Step        string
	TargetID    int64
	Action      string
	Param       int
	RequestedAt time.Time
	Source      string
}

func (h Handler) isAdminSessionActive(chatID int64) bool {
	_, ok := h.adminSessions.Load(chatID)
	return ok
}

func (h Handler) getAdminSession(chatID int64) (adminSession, bool) {
	val, ok := h.adminSessions.Load(chatID)
	if !ok {
		return adminSession{}, false
	}
	s, ok := val.(adminSession)
	return s, ok
}

func (h Handler) setAdminSession(chatID int64, s adminSession) {
	h.adminSessions.Store(chatID, s)
}

// requestAdminConfirmation stores a pending destructive/money-adjusting action in the admin
// session and shows a Да/Отменить prompt. The action only actually runs once the admin taps
// "Да" (AdminActionConfirmCallback), which dispatches to the matching exec* helper.
func (h Handler) requestAdminConfirmation(ctx context.Context, b *bot.Bot, chatID, targetID int64, action string, param int, source, confirmText string) {
	h.setAdminSession(chatID, adminSession{
		Step:        adminStepConfirm,
		TargetID:    targetID,
		Action:      action,
		Param:       param,
		RequestedAt: time.Now(),
		Source:      source,
	})
	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      confirmText,
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					{Text: "✅ Да, подтвердить", CallbackData: CallbackAdminActionConfirm},
					{Text: "❌ Отменить", CallbackData: CallbackAdminActionCancel},
				},
			},
		},
	})
	if err != nil {
		slog.Error("admin: send confirmation prompt", "error", err)
	}
}

// AdminActionConfirmCallback executes the pending confirmed action stored in the admin session,
// then records the outcome in the audit log. Clears the session before executing so a double tap
// on "Да" can't double-execute.
func (h Handler) AdminActionConfirmCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil || update.CallbackQuery.From.ID != config.GetAdminTelegramId() {
		return
	}
	chatID := update.CallbackQuery.From.ID
	sess, ok := h.getAdminSession(chatID)
	if !ok || sess.Step != adminStepConfirm {
		return
	}
	h.adminSessions.Delete(chatID)
	if time.Since(sess.RequestedAt) > adminConfirmExpiry {
		sendAdminReply(ctx, b, chatID, "❌ Время подтверждения истекло, повторите действие.")
		return
	}

	switch sess.Action {
	case "enable":
		h.execSetStatus(ctx, b, chatID, sess.TargetID, "ACTIVE", sess.Source)
	case "disable":
		h.execSetStatus(ctx, b, chatID, sess.TargetID, "DISABLED", sess.Source)
	case "reset_traffic":
		h.execResetTraffic(ctx, b, chatID, sess.TargetID, sess.Source)
	case "reset_devices":
		h.execResetDevices(ctx, b, chatID, sess.TargetID, sess.Source)
	case "topup":
		h.execTopup(ctx, b, chatID, sess.TargetID, sess.Param, sess.Source)
	default:
		slog.Error("admin: unknown confirm action", "action", sess.Action)
	}
}

// AdminActionCancelCallback discards a pending confirmation without executing it.
func (h Handler) AdminActionCancelCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil || update.CallbackQuery.From.ID != config.GetAdminTelegramId() {
		return
	}
	chatID := update.CallbackQuery.From.ID
	h.adminSessions.Delete(chatID)
	sendAdminReply(ctx, b, chatID, "❌ Отменено.")
}

// logAdminAction writes one audit row for a confirmed admin action. Logged best-effort — a
// failure to write the audit row must not block or roll back the action itself.
func (h Handler) logAdminAction(ctx context.Context, action string, targetID int64, param *int, outcome string, errMsg *string, source string) {
	if _, err := h.auditLogRepository.Create(ctx, &database.AdminAuditLog{
		AdminTelegramID:  config.GetAdminTelegramId(),
		Action:           action,
		TargetTelegramID: targetID,
		ParamInt:         param,
		Outcome:          outcome,
		ErrorMessage:     errMsg,
		Source:           source,
	}); err != nil {
		slog.Error("admin: write audit log", "action", action, "target", targetID, "error", err)
	}
}

func sendAdminPanel(ctx context.Context, b *bot.Bot, chatID int64) {
	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      "🛠 <b>Панель администратора</b>\n\nВыберите раздел:",
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					{Text: "📊 Статистика", CallbackData: CallbackAdminPanelStats},
					{Text: "👤 Пользователь", CallbackData: CallbackAdminPanelUsers},
				},
				{
					{Text: "📢 Рассылка", CallbackData: CallbackAdminPanelBcast},
					{Text: "🔧 Система", CallbackData: CallbackAdminPanelSystem},
				},
			},
		},
	})
	if err != nil {
		slog.Error("admin panel: send menu", "error", err)
	}
}

func (h Handler) AdminPanelMenuCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil || update.CallbackQuery.From.ID != config.GetAdminTelegramId() {
		return
	}
	chatID := update.CallbackQuery.From.ID
	h.adminSessions.Delete(chatID)
	sendAdminPanel(ctx, b, chatID)
}

func (h Handler) AdminPanelStatsCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil || update.CallbackQuery.From.ID != config.GetAdminTelegramId() {
		return
	}
	chatID := update.CallbackQuery.From.ID
	customers, err := h.customerRepository.FindAll(ctx)
	if err != nil {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("DB error: %v", err))
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
	msg := fmt.Sprintf("📊 <b>Статистика</b>\n\nВсего: <b>%d</b>\nАктивных подписок: <b>%d</b>\nТриалов: <b>%d</b>\nИстёкших: <b>%d</b>\nБез подписки: <b>%d</b>",
		len(customers), activePaid, activeTrial, expired, noSub)
	_, err = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      msg,
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{{Text: "◀ Назад", CallbackData: CallbackAdminPanelMenu}},
			},
		},
	})
	if err != nil {
		slog.Error("admin panel stats: send", "error", err)
	}
}

func (h Handler) AdminPanelBcastCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil || update.CallbackQuery.From.ID != config.GetAdminTelegramId() {
		return
	}
	chatID := update.CallbackQuery.From.ID
	h.adminSessions.Delete(chatID)
	h.broadcastSessions.Store(chatID, broadcastWaitingForText)
	sendAdminReply(ctx, b, chatID, "📢 <b>Рассылка</b>\n\nОтправь текст рассылки (поддерживается HTML-форматирование).\n\nДля отмены: /cancel")
}

func (h Handler) AdminPanelSystemCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil || update.CallbackQuery.From.ID != config.GetAdminTelegramId() {
		return
	}
	chatID := update.CallbackQuery.From.ID
	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      "🔧 <b>Система</b>",
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{{Text: "🔄 Синхронизировать", CallbackData: CallbackAdminPanelSync}},
				{{Text: "◀ Назад", CallbackData: CallbackAdminPanelMenu}},
			},
		},
	})
	if err != nil {
		slog.Error("admin panel system: send", "error", err)
	}
}

func (h Handler) AdminPanelSyncCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil || update.CallbackQuery.From.ID != config.GetAdminTelegramId() {
		return
	}
	chatID := update.CallbackQuery.From.ID
	sendAdminReply(ctx, b, chatID, "🔄 Синхронизация запущена...")
	go h.syncService.Sync()
}

func (h Handler) AdminPanelUsersCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil || update.CallbackQuery.From.ID != config.GetAdminTelegramId() {
		return
	}
	chatID := update.CallbackQuery.From.ID
	h.broadcastSessions.Delete(chatID)
	h.setAdminSession(chatID, adminSession{Step: adminStepWaitUserID})
	sendAdminReply(ctx, b, chatID, "👤 Введите Telegram ID пользователя:")
}

func sendUserCard(ctx context.Context, b *bot.Bot, chatID, targetID int64, h Handler) {
	customer, err := h.customerRepository.FindByTelegramId(ctx, targetID)
	if err != nil {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("DB error: %v", err))
		return
	}
	if customer == nil {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("❌ Пользователь %d не найден в БД", targetID))
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
		msg += fmt.Sprintf("📅 <b>Подписка:</b> %s до <b>%s</b> (%s)\n",
			status, customer.ExpireAt.Format("02.01.2006 15:04"), formatTimeUntil(*customer.ExpireAt))
	} else {
		msg += "📅 <b>Подписка:</b> отсутствует\n"
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

	idStr := strconv.FormatInt(targetID, 10)
	_, err = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      msg,
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					{Text: "✅ Включить", CallbackData: CallbackAdminUserEnable + ":" + idStr},
					{Text: "❌ Отключить", CallbackData: CallbackAdminUserDisable + ":" + idStr},
				},
				{
					{Text: "➕ Топап ГБ", CallbackData: CallbackAdminUserTopup + ":" + idStr},
					{Text: "📅 Продлить", CallbackData: CallbackAdminUserExtend + ":" + idStr},
				},
				{
					{Text: "🔄 Сброс трафика", CallbackData: CallbackAdminUserResetTraffic + ":" + idStr},
					{Text: "🖥 Сброс устройств", CallbackData: CallbackAdminUserResetDevices + ":" + idStr},
				},
				{
					{Text: "🔍 Другой пользователь", CallbackData: CallbackAdminPanelUsers},
					{Text: "◀ Назад", CallbackData: CallbackAdminPanelMenu},
				},
			},
		},
	})
	if err != nil {
		slog.Error("admin panel: send user card", "error", err)
	}
}

// IsAdminSessionActive returns true when there's an active admin panel session for the given chat.
// Used by the match func registered in main.go.
func (h Handler) IsAdminSessionActive(chatID int64) bool {
	return h.isAdminSessionActive(chatID)
}

// AdminPanelTextHandler routes admin text input to the correct step handler.
func (h Handler) AdminPanelTextHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	chatID := update.Message.Chat.ID
	text := strings.TrimSpace(update.Message.Text)

	sess, ok := h.getAdminSession(chatID)
	if !ok {
		return
	}

	switch sess.Step {
	case adminStepWaitUserID:
		targetID, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			sendAdminReply(ctx, b, chatID, "❌ Некорректный Telegram ID. Введите число:")
			return
		}
		h.adminSessions.Delete(chatID)
		sendUserCard(ctx, b, chatID, targetID, h)

	case adminStepWaitGB:
		gb, err := strconv.Atoi(text)
		if err != nil || gb == 0 {
			sendAdminReply(ctx, b, chatID, "❌ Некорректное значение. Введите число ГБ (может быть отрицательным, но не 0):")
			return
		}
		h.adminSessions.Delete(chatID)
		newLimitGB, ok := h.previewTopupNewLimitGB(ctx, b, chatID, sess.TargetID, gb)
		if !ok {
			return
		}
		sign := "+"
		if gb < 0 {
			sign = ""
		}
		h.requestAdminConfirmation(ctx, b, chatID, sess.TargetID, "topup", gb, "panel",
			fmt.Sprintf("⚠️ Подтвердите: %s%d GB для пользователя <code>%d</code>?\nНовый лимит: %d GB",
				sign, gb, sess.TargetID, newLimitGB/int64(config.BytesInGigabyte())))

	case adminStepWaitDays:
		days, err := strconv.Atoi(text)
		if err != nil || days <= 0 {
			sendAdminReply(ctx, b, chatID, "❌ Некорректное значение. Введите число дней (больше 0):")
			return
		}
		h.adminSessions.Delete(chatID)
		h.execExtend(ctx, b, chatID, sess.TargetID, days)
	}
}

func parseUserActionCallback(data, prefix string) (int64, bool) {
	after, ok := strings.CutPrefix(data, prefix+":")
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(after, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

func (h Handler) AdminUserTopupCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil || update.CallbackQuery.From.ID != config.GetAdminTelegramId() {
		return
	}
	chatID := update.CallbackQuery.From.ID
	targetID, ok := parseUserActionCallback(update.CallbackQuery.Data, CallbackAdminUserTopup)
	if !ok {
		return
	}
	h.setAdminSession(chatID, adminSession{Step: adminStepWaitGB, TargetID: targetID})
	sendAdminReply(ctx, b, chatID, fmt.Sprintf("➕ Введите кол-во ГБ для пользователя <code>%d</code> (может быть отрицательным):", targetID))
}

func (h Handler) AdminUserExtendCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil || update.CallbackQuery.From.ID != config.GetAdminTelegramId() {
		return
	}
	chatID := update.CallbackQuery.From.ID
	targetID, ok := parseUserActionCallback(update.CallbackQuery.Data, CallbackAdminUserExtend)
	if !ok {
		return
	}
	h.setAdminSession(chatID, adminSession{Step: adminStepWaitDays, TargetID: targetID})
	sendAdminReply(ctx, b, chatID, fmt.Sprintf("📅 Введите кол-во дней продления для пользователя <code>%d</code>:", targetID))
}

func (h Handler) AdminUserEnableCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil || update.CallbackQuery.From.ID != config.GetAdminTelegramId() {
		return
	}
	chatID := update.CallbackQuery.From.ID
	targetID, ok := parseUserActionCallback(update.CallbackQuery.Data, CallbackAdminUserEnable)
	if !ok {
		return
	}
	h.requestAdminConfirmation(ctx, b, chatID, targetID, "enable", 0, "panel",
		fmt.Sprintf("⚠️ Подтвердите: ВКЛЮЧИТЬ пользователя <code>%d</code>?", targetID))
}

func (h Handler) AdminUserDisableCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil || update.CallbackQuery.From.ID != config.GetAdminTelegramId() {
		return
	}
	chatID := update.CallbackQuery.From.ID
	targetID, ok := parseUserActionCallback(update.CallbackQuery.Data, CallbackAdminUserDisable)
	if !ok {
		return
	}
	h.requestAdminConfirmation(ctx, b, chatID, targetID, "disable", 0, "panel",
		fmt.Sprintf("⚠️ Подтвердите: ОТКЛЮЧИТЬ пользователя <code>%d</code>?", targetID))
}

func (h Handler) AdminUserResetDevicesCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil || update.CallbackQuery.From.ID != config.GetAdminTelegramId() {
		return
	}
	chatID := update.CallbackQuery.From.ID
	targetID, ok := parseUserActionCallback(update.CallbackQuery.Data, CallbackAdminUserResetDevices)
	if !ok {
		return
	}
	h.requestAdminConfirmation(ctx, b, chatID, targetID, "reset_devices", 0, "panel",
		fmt.Sprintf("⚠️ Подтвердите: СБРОСИТЬ устройства пользователя <code>%d</code>?", targetID))
}

func (h Handler) AdminUserResetTrafficCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil || update.CallbackQuery.From.ID != config.GetAdminTelegramId() {
		return
	}
	chatID := update.CallbackQuery.From.ID
	targetID, ok := parseUserActionCallback(update.CallbackQuery.Data, CallbackAdminUserResetTraffic)
	if !ok {
		return
	}
	h.requestAdminConfirmation(ctx, b, chatID, targetID, "reset_traffic", 0, "panel",
		fmt.Sprintf("⚠️ Подтвердите: СБРОСИТЬ трафик пользователя <code>%d</code>?", targetID))
}

// execSetStatus applies an ACTIVE/DISABLED status change in Remnawave, notifies the customer,
// and records the outcome in the audit log. Called only after admin confirmation.
func (h Handler) execSetStatus(ctx context.Context, b *bot.Bot, chatID, targetID int64, status, source string) {
	rwUsers, err := h.remnawaveClient.GetUsersByTelegramID(ctx, targetID)
	if err != nil || len(rwUsers) == 0 {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("❌ Remnawave user not found for %d", targetID))
		h.logAdminAction(ctx, "set_status_"+status, targetID, nil, "failure", strPtr("remnawave user not found"), source)
		return
	}
	if err := h.remnawaveClient.SetUserStatus(ctx, rwUsers[0].UUID, status); err != nil {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("❌ Error: %v", err))
		h.logAdminAction(ctx, "set_status_"+status, targetID, nil, "failure", strPtr(err.Error()), source)
		return
	}

	if status == "ACTIVE" {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("✅ Пользователь %d включён (ACTIVE)", targetID))
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: targetID, ParseMode: models.ParseModeHTML,
			Text: "<b>Доступ восстановлен!</b>\n\nВаш VPN снова активен. Приятного пользования!",
		})
	} else {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("✅ Пользователь %d отключён (DISABLED)", targetID))
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: targetID, ParseMode: models.ParseModeHTML,
			Text: "<b>Доступ приостановлен.</b>\n\nВаш VPN временно отключён. Если это ошибка — обратитесь в поддержку.",
		})
	}
	slog.Info("admin: set user status", "telegram_id", targetID, "status", status, "source", source)
	h.logAdminAction(ctx, "set_status_"+status, targetID, nil, "success", nil, source)
}

// execResetDevices clears all HWID devices for a user and records the outcome in the audit log.
// Called only after admin confirmation.
func (h Handler) execResetDevices(ctx context.Context, b *bot.Bot, chatID, targetID int64, source string) {
	rwUsers, err := h.remnawaveClient.GetUsersByTelegramID(ctx, targetID)
	if err != nil || len(rwUsers) == 0 {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("❌ Remnawave user not found for %d", targetID))
		h.logAdminAction(ctx, "reset_devices", targetID, nil, "failure", strPtr("remnawave user not found"), source)
		return
	}
	if err := h.remnawaveClient.DeleteAllUserHwidDevices(ctx, rwUsers[0].UUID); err != nil {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("❌ Error: %v", err))
		h.logAdminAction(ctx, "reset_devices", targetID, nil, "failure", strPtr(err.Error()), source)
		return
	}
	sendAdminReply(ctx, b, chatID, fmt.Sprintf("✅ Устройства пользователя %d сброшены (HWID очищен)", targetID))
	slog.Info("admin: reset devices", "telegram_id", targetID, "source", source)
	h.logAdminAction(ctx, "reset_devices", targetID, nil, "success", nil, source)
}

// execResetTraffic resets the traffic counter for a user and records the outcome in the audit
// log. Called only after admin confirmation.
func (h Handler) execResetTraffic(ctx context.Context, b *bot.Bot, chatID, targetID int64, source string) {
	rwUsers, err := h.remnawaveClient.GetUsersByTelegramID(ctx, targetID)
	if err != nil || len(rwUsers) == 0 {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("❌ Remnawave user not found for %d", targetID))
		h.logAdminAction(ctx, "reset_traffic", targetID, nil, "failure", strPtr("remnawave user not found"), source)
		return
	}
	if err := h.remnawaveClient.ResetUserTraffic(ctx, rwUsers[0].UUID); err != nil {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("❌ Error: %v", err))
		h.logAdminAction(ctx, "reset_traffic", targetID, nil, "failure", strPtr(err.Error()), source)
		return
	}
	limitGB := rwUsers[0].TrafficLimitBytes / config.BytesInGigabyte()
	sendAdminReply(ctx, b, chatID, fmt.Sprintf("✅ Трафик пользователя %d сброшен. Лимит: %d GB", targetID, limitGB))
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: targetID, ParseMode: models.ParseModeHTML,
		Text: "<b>Трафик сброшен!</b>\n\nВаш счётчик трафика сброшен администратором. Снова доступен полный объём.",
	})
	slog.Info("admin: reset traffic", "telegram_id", targetID, "source", source)
	h.logAdminAction(ctx, "reset_traffic", targetID, nil, "success", nil, source)
}

// previewTopupNewLimitGB fetches the user's current Remnawave traffic limit and computes what it
// would become after applying gb. Returns ok=false (and replies with the reason) when the user
// can't be found or the result would go negative, so callers can reject before ever showing a
// confirmation prompt for an operation that's guaranteed to fail.
func (h Handler) previewTopupNewLimitGB(ctx context.Context, b *bot.Bot, chatID, targetID int64, gb int) (int64, bool) {
	rwUsers, err := h.remnawaveClient.GetUsersByTelegramID(ctx, targetID)
	if err != nil || len(rwUsers) == 0 {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("❌ Remnawave user not found for %d", targetID))
		return 0, false
	}
	delta := int64(gb) * int64(config.BytesInGigabyte())
	newLimit := int64(rwUsers[0].TrafficLimitBytes) + delta
	if newLimit < 0 {
		sendAdminReply(ctx, b, chatID,
			fmt.Sprintf("❌ Нельзя: текущий лимит %d GB, вычитаете %d GB — результат отрицательный.",
				rwUsers[0].TrafficLimitBytes/config.BytesInGigabyte(), -gb))
		return 0, false
	}
	return newLimit, true
}

// execTopup applies a traffic top-up/deduction in Remnawave, records the DB topup row, and writes
// an audit log entry. Called only after admin confirmation; re-validates against fresh Remnawave
// state in case it changed since the confirmation prompt was shown.
func (h Handler) execTopup(ctx context.Context, b *bot.Bot, chatID, targetID int64, gb int, source string) {
	rwUsers, err := h.remnawaveClient.GetUsersByTelegramID(ctx, targetID)
	if err != nil || len(rwUsers) == 0 {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("❌ Remnawave user not found for %d", targetID))
		h.logAdminAction(ctx, "topup", targetID, &gb, "failure", strPtr("remnawave user not found"), source)
		return
	}
	u := rwUsers[0]
	delta := int64(gb) * int64(config.BytesInGigabyte())
	newLimit := int64(u.TrafficLimitBytes) + delta
	if newLimit < 0 {
		msg := fmt.Sprintf("Нельзя: текущий лимит %d GB, вычитаете %d GB — результат отрицательный.",
			u.TrafficLimitBytes/config.BytesInGigabyte(), -gb)
		sendAdminReply(ctx, b, chatID, "❌ "+msg)
		h.logAdminAction(ctx, "topup", targetID, &gb, "failure", strPtr(msg), source)
		return
	}
	if err := h.remnawaveClient.UpdateUserTrafficLimit(ctx, u.UUID, int(newLimit), config.TrafficLimitResetStrategy()); err != nil {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("❌ Failed: %v", err))
		h.logAdminAction(ctx, "topup", targetID, &gb, "failure", strPtr(err.Error()), source)
		return
	}
	now := time.Now()
	target := newLimit
	reply := ""
	if _, dbErr := h.topupRepository.Create(ctx, &database.TrafficTopup{
		TelegramID:              targetID,
		RemnawaveUUID:           u.UUID.String(),
		GBAmount:                gb,
		Status:                  database.TopupStatusCompleted,
		TargetTrafficLimitBytes: &target,
		CompletedAt:             &now,
	}); dbErr != nil {
		slog.Error("admin topup: create DB record", "telegram_id", targetID, "error", dbErr)
		reply = "\n⚠️ DB-запись не создана."
	}
	sign := "+"
	if gb < 0 {
		sign = ""
	}
	sendAdminReply(ctx, b, chatID,
		fmt.Sprintf("✅ %s%d GB для %d\nНовый лимит: %d GB%s", sign, gb, targetID, newLimit/int64(config.BytesInGigabyte()), reply))
	slog.Info("admin topup: applied", "telegram_id", targetID, "gb", gb, "new_limit_gb", newLimit/int64(config.BytesInGigabyte()), "source", source)
	h.logAdminAction(ctx, "topup", targetID, &gb, "success", nil, source)
}

func strPtr(s string) *string { return &s }

func (h Handler) execExtend(ctx context.Context, b *bot.Bot, chatID, targetID int64, days int) {
	customer, err := h.customerRepository.FindByTelegramId(ctx, targetID)
	if err != nil || customer == nil {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("❌ User %d not found", targetID))
		return
	}
	rwUsers, err := h.remnawaveClient.GetUsersByTelegramID(ctx, targetID)
	if err != nil || len(rwUsers) == 0 {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("❌ Remnawave user not found for %d", targetID))
		return
	}
	newUser, err := h.remnawaveClient.CreateOrUpdateUser(ctx, customer.ID, customer.TelegramID, rwUsers[0].TrafficLimitBytes, days, customer.IsTrial)
	if err != nil {
		sendAdminReply(ctx, b, chatID, fmt.Sprintf("❌ Remnawave error: %v", err))
		return
	}
	dbNote := ""
	if err := h.customerRepository.UpdateFields(ctx, customer.ID, map[string]interface{}{
		"expire_at":         newUser.ExpireAt,
		"subscription_link": newUser.SubscriptionUrl,
	}); err != nil {
		slog.Error("admin panel extend: update customer DB", "error", err)
		dbNote = "\n⚠️ DB-запись не обновлена — бот не видит новую дату."
	}
	expireDate := newUser.ExpireAt.Format("02.01.2006")
	sendAdminReply(ctx, b, chatID,
		fmt.Sprintf("✅ Продлено на %d дн. для %d. Подписка до: %s%s", days, targetID, expireDate, dbNote))
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: targetID, ParseMode: models.ParseModeHTML,
		Text: fmt.Sprintf("<b>Хорошие новости! Ваша подписка продлена на %d %s.</b>\n\nАктивна до: %s",
			days, pluralDays(days), expireDate),
	})
	slog.Info("admin panel extend", "telegram_id", targetID, "days", days)
}
