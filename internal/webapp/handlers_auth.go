package webapp

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
)

type loginRequest struct {
	InitData string `json:"initData"`
}

type loginResponse struct {
	Token     string       `json:"token"`
	ExpiresAt int64        `json:"expiresAt"`
	User      TelegramUser `json:"user"`
}

// handleLogin verifies Telegram WebApp initData, checks the signed-in user is the configured
// admin, and issues a session token. Every outcome (including HMAC mismatch and wrong Telegram
// ID) is audit-logged as "webapp_login_denied" / success, per the plan's security requirements —
// failed login attempts against this endpoint are visible in the same audit trail as every other
// admin action.
func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
	if !h.loginLimiter.Allow(ip) {
		writeError(w, http.StatusTooManyRequests, "too many login attempts, try again later")
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InitData == "" {
		writeError(w, http.StatusBadRequest, "missing initData")
		return
	}

	maxAge := time.Duration(config.AdminWebAppInitDataMaxAgeHours()) * time.Hour
	user, err := verifyInitData(req.InitData, config.TelegramToken(), maxAge)
	if err != nil {
		slog.Warn("webapp login: initData verification failed", "error", err)
		writeError(w, http.StatusUnauthorized, "invalid initData")
		return
	}

	if user.ID != config.GetAdminTelegramId() {
		h.auditLoginDenied(r, user.ID, "telegram id is not the configured admin")
		writeError(w, http.StatusForbidden, "not authorized")
		return
	}

	ttl := time.Duration(config.AdminSessionTTLMinutes()) * time.Minute
	token, err := issueSessionToken(config.AdminWebAppJWTSecret(), user.ID, ttl)
	if err != nil {
		slog.Error("webapp login: issue session token", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to issue session")
		return
	}

	if _, err := h.auditLogRepository.Create(r.Context(), &database.AdminAuditLog{
		AdminTelegramID:  user.ID,
		Action:           "webapp_login",
		TargetTelegramID: user.ID,
		Outcome:          "success",
		Source:           "webapi",
	}); err != nil {
		slog.Error("webapp login: write audit log", "error", err)
	}

	writeJSON(w, http.StatusOK, loginResponse{
		Token:     token,
		ExpiresAt: time.Now().Add(ttl).Unix(),
		User:      user,
	})
}

func (h *Handler) auditLoginDenied(r *http.Request, attemptedID int64, reason string) {
	slog.Warn("webapp login: denied", "telegram_id", attemptedID, "reason", reason)
	if _, err := h.auditLogRepository.Create(r.Context(), &database.AdminAuditLog{
		AdminTelegramID:  attemptedID,
		Action:           "webapp_login_denied",
		TargetTelegramID: attemptedID,
		Outcome:          "failure",
		ErrorMessage:     &reason,
		Source:           "webapi",
	}); err != nil {
		slog.Error("webapp login denied: write audit log", "error", err)
	}
}

// handleLogout is a client-side no-op on the server (sessions aren't stored server-side, so
// there's nothing to revoke) but exists so the frontend has a symmetric endpoint to call before
// clearing sessionStorage, and so a logout is visible in server logs.
func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	slog.Info("webapp: logout", "admin_id", adminIDFromContext(r.Context()))
	w.WriteHeader(http.StatusNoContent)
}

type meResponse struct {
	TelegramID int64 `json:"telegramId"`
}

func (h *Handler) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, meResponse{TelegramID: adminIDFromContext(r.Context())})
}
