package webapp

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"remnawave-tg-shop-bot/internal/config"
)

type ctxKey int

const ctxKeyAdminID ctxKey = iota

// adminIDFromContext returns the authenticated admin's Telegram ID, set by requireAdminSession.
func adminIDFromContext(ctx context.Context) int64 {
	id, _ := ctx.Value(ctxKeyAdminID).(int64)
	return id
}

// requireAdminSession verifies the bearer session token on every request: signature, expiry, AND
// that the token's subject still matches the configured admin ID. Re-checking the admin ID (not
// just trusting a previously-valid signature) means a config change immediately invalidates old
// sessions instead of waiting for their natural TTL.
func (h *Handler) requireAdminSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authz := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(authz, "Bearer ")
		if !ok || token == "" {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := verifySessionToken(config.AdminWebAppJWTSecret(), token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired session")
			return
		}
		if claims.Sub != config.GetAdminTelegramId() {
			writeError(w, http.StatusForbidden, "not admin")
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyAdminID, claims.Sub)
		next(w, r.WithContext(ctx))
	}
}

// withCSP sets the frame-ancestors CSP directive needed to let Telegram Desktop embed the Mini
// App in an iframe. Deliberately does NOT set X-Frame-Options: DENY — that would blank the page
// on Telegram Desktop, since it renders Mini Apps inside a frame (mobile clients use a native
// WebView, so they're unaffected by this either way).
func withCSP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy",
			"frame-ancestors https://web.telegram.org https://webk.telegram.org https://webz.telegram.org 'self'")
		next.ServeHTTP(w, r)
	})
}

// withLogging logs method, path, status, and duration for every request under /admin/.
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		slog.Info("webapp request", "method", r.Method, "path", r.URL.Path, "status", sw.status, "duration", time.Since(start))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(status int) {
	sw.status = status
	sw.ResponseWriter.WriteHeader(status)
}
