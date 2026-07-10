// Package adminops holds the Telegram-agnostic business logic behind every admin mutating
// action (enable/disable, traffic reset, device reset, top-up, extend, trial toggle, broadcast,
// sync, fix-traffic-strategy). Both the Telegram bot admin panel (internal/handler) and the admin
// web app (internal/webapp) call into the same Service methods, so behavior can't drift between
// the two front ends and every mutation is unconditionally audit-logged (success and failure).
package adminops

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/remnawave"
	syncsvc "remnawave-tg-shop-bot/internal/sync"
	"remnawave-tg-shop-bot/internal/translation"
)

var (
	// ErrRemnawaveUserNotFound is returned when the target telegram ID has no matching user in
	// Remnawave. Wrapped with the target ID via %w so callers can still errors.Is() against it.
	ErrRemnawaveUserNotFound = errors.New("remnawave user not found")
	// ErrCustomerNotFound is returned when the target telegram ID has no row in the bot's own
	// customer table.
	ErrCustomerNotFound = errors.New("customer not found")
	// ErrNegativeLimit is returned when a traffic top-up/deduction would push the Remnawave
	// traffic limit below zero.
	ErrNegativeLimit = errors.New("resulting traffic limit would be negative")
	// ErrEmptyMessage is returned by SendMessage when the admin-authored text is blank.
	ErrEmptyMessage = errors.New("message text is empty")
)

// Service implements every admin mutating action once, callable from both the Telegram bot
// (internal/handler) and the admin web API (internal/webapp). telegramBot is used only to notify
// the affected customer and to deliver broadcasts — it never parses inbound Telegram
// updates/callbacks, which is what keeps this package UI-agnostic.
type Service struct {
	customerRepository     *database.CustomerRepository
	purchaseRepository     *database.PurchaseRepository
	topupRepository        *database.TrafficTopupRepository
	referralRepository     *database.ReferralRepository
	auditLogRepository     *database.AdminAuditLogRepository
	webhookInboxRepository *database.WebhookInboxRepository
	remnawaveClient        *remnawave.Client
	syncService            *syncsvc.SyncService
	telegramBot            *bot.Bot
	translation            *translation.Manager
}

func NewService(
	customerRepository *database.CustomerRepository,
	purchaseRepository *database.PurchaseRepository,
	topupRepository *database.TrafficTopupRepository,
	referralRepository *database.ReferralRepository,
	auditLogRepository *database.AdminAuditLogRepository,
	webhookInboxRepository *database.WebhookInboxRepository,
	remnawaveClient *remnawave.Client,
	syncService *syncsvc.SyncService,
	telegramBot *bot.Bot,
	translationManager *translation.Manager,
) *Service {
	return &Service{
		customerRepository:     customerRepository,
		purchaseRepository:     purchaseRepository,
		topupRepository:        topupRepository,
		referralRepository:     referralRepository,
		auditLogRepository:     auditLogRepository,
		webhookInboxRepository: webhookInboxRepository,
		remnawaveClient:        remnawaveClient,
		syncService:            syncService,
		telegramBot:            telegramBot,
		translation:            translationManager,
	}
}

// audit writes one admin_audit_log row for every mutating action, success or failure. Logging is
// best-effort: a failure to write the audit row must not roll back or mask the action's own
// result, so errors here are only logged, not returned.
func (s *Service) audit(ctx context.Context, action string, targetID int64, param *int, actionErr error, source string) {
	outcome := "success"
	var errMsg *string
	if actionErr != nil {
		outcome = "failure"
		m := actionErr.Error()
		errMsg = &m
	}
	if _, err := s.auditLogRepository.Create(ctx, &database.AdminAuditLog{
		AdminTelegramID:  config.GetAdminTelegramId(),
		Action:           action,
		TargetTelegramID: targetID,
		ParamInt:         param,
		Outcome:          outcome,
		ErrorMessage:     errMsg,
		Source:           source,
	}); err != nil {
		slog.Error("adminops: write audit log", "action", action, "target", targetID, "error", err)
	}
}

// auditText mirrors audit but records a free-text parameter (e.g. the body of an admin-authored
// message) instead of a numeric one — kept as a separate helper so audit's ParamInt-only callers
// don't need to change shape.
func (s *Service) auditText(ctx context.Context, action string, targetID int64, param *string, actionErr error, source string) {
	outcome := "success"
	var errMsg *string
	if actionErr != nil {
		outcome = "failure"
		m := actionErr.Error()
		errMsg = &m
	}
	if _, err := s.auditLogRepository.Create(ctx, &database.AdminAuditLog{
		AdminTelegramID:  config.GetAdminTelegramId(),
		Action:           action,
		TargetTelegramID: targetID,
		ParamText:        param,
		Outcome:          outcome,
		ErrorMessage:     errMsg,
		Source:           source,
	}); err != nil {
		slog.Error("adminops: write audit log", "action", action, "target", targetID, "error", err)
	}
}

func intPtr(v int) *int { return &v }

// notify sends an HTML message to a Telegram chat, best-effort. Every call site logs and swallows
// the error, matching existing bot behavior where a notification failure never blocks the action.
func (s *Service) notify(ctx context.Context, chatID int64, text string) {
	if s.telegramBot == nil {
		return
	}
	if _, err := s.telegramBot.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, ParseMode: models.ParseModeHTML, Text: text}); err != nil {
		slog.Error("adminops: notify failed", "chat_id", chatID, "error", err)
	}
}

// customerLanguage resolves the target customer's preferred language for translation lookups.
// Returns "" (not an error) when the customer can't be found — translation.Manager.GetText falls
// back to the default language for an unknown/empty langCode, so callers never need to branch on
// this failing.
func (s *Service) customerLanguage(ctx context.Context, targetID int64) string {
	customer, err := s.customerRepository.FindByTelegramId(ctx, targetID)
	if err != nil || customer == nil {
		return ""
	}
	return customer.Language
}

// notifyKey resolves the target customer's language, looks up text by translation key, formats it
// with args when provided, and sends it via notify. This is the one path every adminops
// notification should go through instead of hardcoding Russian strings, so wording lives in
// translations/*.json (and is available in every supported language) rather than in Go source.
func (s *Service) notifyKey(ctx context.Context, targetID int64, key string, args ...any) {
	if s.translation == nil {
		return
	}
	lang := s.customerLanguage(ctx, targetID)
	text := s.translation.GetText(lang, key)
	if len(args) > 0 {
		text = fmt.Sprintf(text, args...)
	}
	s.notify(ctx, targetID, text)
}

// SetStatusResult carries the outcome of SetStatus for API/UI display.
type SetStatusResult struct {
	Status string
}

// SetStatus applies an ACTIVE/DISABLED status change in Remnawave and notifies the customer.
// Mirrors the bot's execSetStatus exactly, plus unconditional audit logging.
func (s *Service) SetStatus(ctx context.Context, targetID int64, status, source string) (SetStatusResult, error) {
	rwUsers, err := s.remnawaveClient.GetUsersByTelegramID(ctx, targetID)
	if err != nil || len(rwUsers) == 0 {
		wrapped := fmt.Errorf("%w for %d", ErrRemnawaveUserNotFound, targetID)
		s.audit(ctx, "set_status_"+status, targetID, nil, wrapped, source)
		return SetStatusResult{}, wrapped
	}
	if err := s.remnawaveClient.SetUserStatus(ctx, rwUsers[0].UUID, status); err != nil {
		s.audit(ctx, "set_status_"+status, targetID, nil, err, source)
		return SetStatusResult{}, err
	}

	if status == "ACTIVE" {
		s.notifyKey(ctx, targetID, "admin_status_active")
	} else {
		s.notifyKey(ctx, targetID, "admin_status_disabled")
	}
	slog.Info("adminops: set user status", "telegram_id", targetID, "status", status, "source", source)
	s.audit(ctx, "set_status_"+status, targetID, nil, nil, source)
	return SetStatusResult{Status: status}, nil
}

// ResetDevices clears all HWID devices for a user in Remnawave. Mirrors the bot's
// execResetDevices exactly, plus unconditional audit logging and (new) a customer notification —
// this action used to happen silently from the customer's point of view.
func (s *Service) ResetDevices(ctx context.Context, targetID int64, source string) error {
	rwUsers, err := s.remnawaveClient.GetUsersByTelegramID(ctx, targetID)
	if err != nil || len(rwUsers) == 0 {
		wrapped := fmt.Errorf("%w for %d", ErrRemnawaveUserNotFound, targetID)
		s.audit(ctx, "reset_devices", targetID, nil, wrapped, source)
		return wrapped
	}
	if err := s.remnawaveClient.DeleteAllUserHwidDevices(ctx, rwUsers[0].UUID); err != nil {
		s.audit(ctx, "reset_devices", targetID, nil, err, source)
		return err
	}
	s.notifyKey(ctx, targetID, "admin_devices_reset")
	slog.Info("adminops: reset devices", "telegram_id", targetID, "source", source)
	s.audit(ctx, "reset_devices", targetID, nil, nil, source)
	return nil
}

// ResetTrafficResult carries the outcome of ResetTraffic for API/UI display.
type ResetTrafficResult struct {
	NewLimitGB int64
}

// ResetTraffic resets the traffic counter for a user in Remnawave and notifies the customer.
// Mirrors the bot's execResetTraffic exactly, plus unconditional audit logging.
func (s *Service) ResetTraffic(ctx context.Context, targetID int64, source string) (ResetTrafficResult, error) {
	rwUsers, err := s.remnawaveClient.GetUsersByTelegramID(ctx, targetID)
	if err != nil || len(rwUsers) == 0 {
		wrapped := fmt.Errorf("%w for %d", ErrRemnawaveUserNotFound, targetID)
		s.audit(ctx, "reset_traffic", targetID, nil, wrapped, source)
		return ResetTrafficResult{}, wrapped
	}
	if err := s.remnawaveClient.ResetUserTraffic(ctx, rwUsers[0].UUID); err != nil {
		s.audit(ctx, "reset_traffic", targetID, nil, err, source)
		return ResetTrafficResult{}, err
	}
	limitGB := int64(rwUsers[0].TrafficLimitBytes) / int64(config.BytesInGigabyte())
	s.notifyKey(ctx, targetID, "admin_traffic_reset")
	slog.Info("adminops: reset traffic", "telegram_id", targetID, "source", source)
	s.audit(ctx, "reset_traffic", targetID, nil, nil, source)
	return ResetTrafficResult{NewLimitGB: limitGB}, nil
}

// PreviewTopup fetches the user's current Remnawave traffic limit and computes what it would
// become after applying gb, without writing anything. Used to show a confirmation prompt before
// Topup actually runs, and to re-validate live state right before a confirmed Topup executes.
func (s *Service) PreviewTopup(ctx context.Context, targetID int64, gb int) (int64, error) {
	rwUsers, err := s.remnawaveClient.GetUsersByTelegramID(ctx, targetID)
	if err != nil || len(rwUsers) == 0 {
		return 0, fmt.Errorf("%w for %d", ErrRemnawaveUserNotFound, targetID)
	}
	delta := int64(gb) * int64(config.BytesInGigabyte())
	newLimit := int64(rwUsers[0].TrafficLimitBytes) + delta
	if newLimit < 0 {
		return 0, fmt.Errorf("%w: current %d GB, subtracting %d GB", ErrNegativeLimit, rwUsers[0].TrafficLimitBytes/config.BytesInGigabyte(), -gb)
	}
	return newLimit, nil
}

// TopupResult carries the outcome of Topup for API/UI display.
type TopupResult struct {
	NewLimitGB      int64
	DBRecordCreated bool
}

// Topup applies a traffic top-up/deduction in Remnawave and records the DB topup row. Mirrors the
// bot's execTopup exactly (including re-validating against fresh Remnawave state), plus
// unconditional audit logging.
func (s *Service) Topup(ctx context.Context, targetID int64, gb int, source string) (TopupResult, error) {
	rwUsers, err := s.remnawaveClient.GetUsersByTelegramID(ctx, targetID)
	if err != nil || len(rwUsers) == 0 {
		wrapped := fmt.Errorf("%w for %d", ErrRemnawaveUserNotFound, targetID)
		s.audit(ctx, "topup", targetID, intPtr(gb), wrapped, source)
		return TopupResult{}, wrapped
	}
	u := rwUsers[0]
	delta := int64(gb) * int64(config.BytesInGigabyte())
	newLimit := int64(u.TrafficLimitBytes) + delta
	if newLimit < 0 {
		wrapped := fmt.Errorf("%w: current %d GB, subtracting %d GB", ErrNegativeLimit, u.TrafficLimitBytes/config.BytesInGigabyte(), -gb)
		s.audit(ctx, "topup", targetID, intPtr(gb), wrapped, source)
		return TopupResult{}, wrapped
	}
	if err := s.remnawaveClient.UpdateUserTrafficLimit(ctx, u.UUID, int(newLimit), config.TrafficLimitResetStrategy()); err != nil {
		s.audit(ctx, "topup", targetID, intPtr(gb), err, source)
		return TopupResult{}, err
	}
	now := time.Now()
	target := newLimit
	dbCreated := true
	if _, dbErr := s.topupRepository.Create(ctx, &database.TrafficTopup{
		TelegramID:              targetID,
		RemnawaveUUID:           u.UUID.String(),
		GBAmount:                gb,
		Status:                  database.TopupStatusCompleted,
		TargetTrafficLimitBytes: &target,
		CompletedAt:             &now,
	}); dbErr != nil {
		slog.Error("adminops topup: create DB record", "telegram_id", targetID, "error", dbErr)
		dbCreated = false
	}
	newLimitGB := newLimit / int64(config.BytesInGigabyte())
	if gb > 0 {
		s.notifyKey(ctx, targetID, "admin_topup_increase", gb, newLimitGB)
	} else {
		s.notifyKey(ctx, targetID, "admin_topup_decrease", -gb, newLimitGB)
	}
	slog.Info("adminops: topup applied", "telegram_id", targetID, "gb", gb, "new_limit_gb", newLimitGB, "source", source)
	s.audit(ctx, "topup", targetID, intPtr(gb), nil, source)
	return TopupResult{NewLimitGB: newLimitGB, DBRecordCreated: dbCreated}, nil
}

// TopupEnrollResult carries the outcome of TopupEnroll for API/UI display: exactly one of
// AlreadyBase, AlreadyEnrolled, Enrolled is true.
type TopupEnrollResult struct {
	AlreadyBase      bool
	AlreadyEnrolled  bool
	Enrolled         bool
	ExistingTargetGB int64
	CurrentLimitGB   int64
	BaseLimitGB      int64
	DeltaGB          int
}

// TopupEnroll registers an existing (out-of-band) Remnawave traffic limit into the topup system
// so future Topup/rollover logic accounts for it correctly. Mirrors
// AdminTopupEnrollCommandHandler exactly, plus unconditional audit logging.
//
// Intentionally does not notify the customer: this is an internal bookkeeping reconciliation
// (registering a limit that already exists in Remnawave into our topup ledger) — the customer's
// actual traffic limit doesn't change, so there's nothing user-visible to report.
func (s *Service) TopupEnroll(ctx context.Context, targetID int64, source string) (TopupEnrollResult, error) {
	rwUsers, err := s.remnawaveClient.GetUsersByTelegramID(ctx, targetID)
	if err != nil || len(rwUsers) == 0 {
		wrapped := fmt.Errorf("%w for %d", ErrRemnawaveUserNotFound, targetID)
		s.audit(ctx, "topup_enroll", targetID, nil, wrapped, source)
		return TopupEnrollResult{}, wrapped
	}
	u := rwUsers[0]
	currentLimitBytes := u.TrafficLimitBytes
	baseLimitBytes := config.TrafficLimit()

	if currentLimitBytes <= baseLimitBytes {
		result := TopupEnrollResult{
			AlreadyBase:    true,
			CurrentLimitGB: int64(currentLimitBytes) / int64(config.BytesInGigabyte()),
			BaseLimitGB:    int64(baseLimitBytes) / int64(config.BytesInGigabyte()),
		}
		s.audit(ctx, "topup_enroll", targetID, nil, nil, source)
		return result, nil
	}

	existing, err := s.topupRepository.FindLatestCompletedByTelegramID(ctx, targetID)
	if err != nil {
		s.audit(ctx, "topup_enroll", targetID, nil, err, source)
		return TopupEnrollResult{}, err
	}
	if existing != nil {
		var targetGB int64
		if existing.TargetTrafficLimitBytes != nil {
			targetGB = *existing.TargetTrafficLimitBytes / int64(config.BytesInGigabyte())
		}
		result := TopupEnrollResult{AlreadyEnrolled: true, ExistingTargetGB: targetGB}
		s.audit(ctx, "topup_enroll", targetID, nil, nil, source)
		return result, nil
	}

	deltaGB := (currentLimitBytes - baseLimitBytes) / config.BytesInGigabyte()
	now := time.Now()
	target := int64(currentLimitBytes)
	if _, dbErr := s.topupRepository.Create(ctx, &database.TrafficTopup{
		TelegramID:              targetID,
		RemnawaveUUID:           u.UUID.String(),
		GBAmount:                deltaGB,
		Status:                  database.TopupStatusCompleted,
		TargetTrafficLimitBytes: &target,
		CompletedAt:             &now,
	}); dbErr != nil {
		s.audit(ctx, "topup_enroll", targetID, nil, dbErr, source)
		return TopupEnrollResult{}, dbErr
	}

	slog.Info("adminops: topup enroll", "telegram_id", targetID, "delta_gb", deltaGB, "source", source)
	result := TopupEnrollResult{
		Enrolled:       true,
		CurrentLimitGB: int64(currentLimitBytes) / int64(config.BytesInGigabyte()),
		BaseLimitGB:    int64(baseLimitBytes) / int64(config.BytesInGigabyte()),
		DeltaGB:        deltaGB,
	}
	s.audit(ctx, "topup_enroll", targetID, nil, nil, source)
	return result, nil
}

// ExtendResult carries the outcome of Extend for API/UI display.
type ExtendResult struct {
	ExpireAt  time.Time
	DBUpdated bool
}

// Extend adds days to a customer's subscription in Remnawave and notifies them. Mirrors the bot's
// execExtend exactly. Unlike the bot today, this always audit-logs (extend was previously a gap
// in the audit trail).
func (s *Service) Extend(ctx context.Context, targetID int64, days int, source string) (ExtendResult, error) {
	customer, err := s.customerRepository.FindByTelegramId(ctx, targetID)
	if err != nil {
		s.audit(ctx, "extend", targetID, intPtr(days), err, source)
		return ExtendResult{}, err
	}
	if customer == nil {
		wrapped := fmt.Errorf("%w: %d", ErrCustomerNotFound, targetID)
		s.audit(ctx, "extend", targetID, intPtr(days), wrapped, source)
		return ExtendResult{}, wrapped
	}
	rwUsers, err := s.remnawaveClient.GetUsersByTelegramID(ctx, targetID)
	if err != nil || len(rwUsers) == 0 {
		wrapped := fmt.Errorf("%w for %d", ErrRemnawaveUserNotFound, targetID)
		s.audit(ctx, "extend", targetID, intPtr(days), wrapped, source)
		return ExtendResult{}, wrapped
	}
	newUser, err := s.remnawaveClient.CreateOrUpdateUser(ctx, customer.ID, customer.TelegramID, rwUsers[0].TrafficLimitBytes, days, customer.IsTrial)
	if err != nil {
		s.audit(ctx, "extend", targetID, intPtr(days), err, source)
		return ExtendResult{}, err
	}
	dbUpdated := true
	if err := s.customerRepository.UpdateFields(ctx, customer.ID, map[string]interface{}{
		"expire_at":         newUser.ExpireAt,
		"subscription_link": newUser.SubscriptionUrl,
	}); err != nil {
		slog.Error("adminops extend: update customer DB", "error", err)
		dbUpdated = false
	}
	s.notifyKey(ctx, targetID, "admin_subscription_extended", days, newUser.ExpireAt.Format("02.01.2006"))
	slog.Info("adminops: extend", "telegram_id", targetID, "days", days, "source", source)
	s.audit(ctx, "extend", targetID, intPtr(days), nil, source)
	return ExtendResult{ExpireAt: newUser.ExpireAt, DBUpdated: dbUpdated}, nil
}

// SetTrial toggles the is_trial flag for a customer in the bot database. Mirrors
// AdminSetTrialCommandHandler exactly. Unlike the bot today, this always audit-logs (set_trial
// was previously a gap in the audit trail).
func (s *Service) SetTrial(ctx context.Context, targetID int64, isTrial bool, source string) error {
	param := 0
	if isTrial {
		param = 1
	}
	customer, err := s.customerRepository.FindByTelegramId(ctx, targetID)
	if err != nil {
		s.audit(ctx, "set_trial", targetID, &param, err, source)
		return err
	}
	if customer == nil {
		wrapped := fmt.Errorf("%w: %d", ErrCustomerNotFound, targetID)
		s.audit(ctx, "set_trial", targetID, &param, wrapped, source)
		return wrapped
	}
	if err := s.customerRepository.UpdateFields(ctx, customer.ID, map[string]interface{}{"is_trial": isTrial}); err != nil {
		s.audit(ctx, "set_trial", targetID, &param, err, source)
		return err
	}
	if isTrial {
		s.notifyKey(ctx, targetID, "admin_trial_enabled")
	} else {
		s.notifyKey(ctx, targetID, "admin_trial_disabled")
	}
	slog.Info("adminops: set_trial", "telegram_id", targetID, "is_trial", isTrial, "source", source)
	s.audit(ctx, "set_trial", targetID, &param, nil, source)
	return nil
}

// RunSync triggers a full Remnawave->bot-DB customer sync. Mirrors AdminPanelSyncCallback
// exactly (fire-and-forget, no confirmation needed — it's non-destructive). Audit-logged like
// every other adminops mutation even though the bot didn't log this before.
func (s *Service) RunSync(ctx context.Context, source string) {
	s.syncService.Sync()
	s.audit(ctx, "sync", 0, nil, nil, source)
}

// SendMessage delivers a one-way, admin-authored message to a customer's Telegram DM, wrapped in
// the admin_message_prefix template (localized to the customer's language). Unlike every other
// mutation here, it doesn't touch Remnawave at all — same reasoning as broadcast delivery — so
// there's no live-state re-fetch step. Always audit-logs (success and failure), preserving the
// sent text in ParamText so admins can review what was actually said.
func (s *Service) SendMessage(ctx context.Context, targetID int64, text, source string) error {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		s.auditText(ctx, "send_message", targetID, nil, ErrEmptyMessage, source)
		return ErrEmptyMessage
	}

	if s.telegramBot == nil {
		err := errors.New("telegram bot not configured")
		s.auditText(ctx, "send_message", targetID, &trimmed, err, source)
		return err
	}

	wrapped := trimmed
	if s.translation != nil {
		wrapped = fmt.Sprintf(s.translation.GetText(s.customerLanguage(ctx, targetID), "admin_message_prefix"), trimmed)
	}

	_, err := s.telegramBot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: targetID, ParseMode: models.ParseModeHTML, Text: wrapped,
	})
	s.auditText(ctx, "send_message", targetID, &trimmed, err, source)
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	return nil
}
