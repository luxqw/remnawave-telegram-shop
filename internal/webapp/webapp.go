// Package webapp implements the admin web app: a JSON REST API under /admin/api/ plus the
// embedded SPA under /admin/. It is mounted directly on the existing net/http mux built in
// cmd/app/main.go — no separate port, no router library (Go 1.25's http.ServeMux already
// supports "METHOD /path/{param}" patterns). All mutating logic lives in internal/adminops;
// this package only translates HTTP <-> adminops calls and serves read-only queries.
package webapp

import (
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"

	"remnawave-tg-shop-bot/internal/adminops"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/notification"
	"remnawave-tg-shop-bot/internal/remnawave"
	"remnawave-tg-shop-bot/internal/rollypay"
	"remnawave-tg-shop-bot/internal/tribute"
)

// BuildInfo carries version metadata for GET /admin/api/dashboard/health, mirroring what
// fullHealthHandler in cmd/app/main.go already reports for /healthcheck.
type BuildInfo struct {
	Version   string
	Commit    string
	BuildDate string
}

// Handler owns every dependency the admin web API needs. Constructed once in main.go from the
// same repository/service instances the bot handlers use.
type Handler struct {
	customerRepository           *database.CustomerRepository
	purchaseRepository           *database.PurchaseRepository
	referralRepository           *database.ReferralRepository
	auditLogRepository           *database.AdminAuditLogRepository
	webhookInboxRepository       *database.WebhookInboxRepository
	activityRepository           *database.ActivityRepository
	notificationLogRepository    *database.NotificationLogRepository
	adminMessageRepository       *database.AdminMessageRepository
	botRuntimeSettingsRepository *database.BotRuntimeSettingsRepository
	remnawaveClient              *remnawave.Client
	tributeClient                *tribute.Client         // nil when Tribute webhooks are disabled
	rollypayClient               *rollypay.WebhookClient // nil when RollyPay webhooks are disabled
	ops                          *adminops.Service
	subscriptionService          *notification.SubscriptionService
	trafficWarningService        *notification.TrafficWarningService
	pool                         *pgxpool.Pool
	build                        BuildInfo

	loginLimiter *rateLimiter

	// fixStrategyJobs tracks in-flight/completed bulk fix-traffic-strategy apply runs, the same
	// way adminops tracks broadcast jobs — in-memory snapshots, safe for concurrent polling.
	fixStrategyJobs sync.Map

	mux http.Handler
}

// NewHandler builds the admin web app's route table and returns a ready-to-mount http.Handler
// producer. tributeClient may be nil (Tribute webhooks disabled) — webhook retry then reports a
// 503.
func NewHandler(
	customerRepository *database.CustomerRepository,
	purchaseRepository *database.PurchaseRepository,
	referralRepository *database.ReferralRepository,
	auditLogRepository *database.AdminAuditLogRepository,
	webhookInboxRepository *database.WebhookInboxRepository,
	activityRepository *database.ActivityRepository,
	notificationLogRepository *database.NotificationLogRepository,
	adminMessageRepository *database.AdminMessageRepository,
	botRuntimeSettingsRepository *database.BotRuntimeSettingsRepository,
	remnawaveClient *remnawave.Client,
	tributeClient *tribute.Client,
	rollypayClient *rollypay.WebhookClient,
	ops *adminops.Service,
	subscriptionService *notification.SubscriptionService,
	trafficWarningService *notification.TrafficWarningService,
	pool *pgxpool.Pool,
	build BuildInfo,
) *Handler {
	h := &Handler{
		customerRepository:           customerRepository,
		purchaseRepository:           purchaseRepository,
		referralRepository:           referralRepository,
		auditLogRepository:           auditLogRepository,
		webhookInboxRepository:       webhookInboxRepository,
		activityRepository:           activityRepository,
		notificationLogRepository:    notificationLogRepository,
		adminMessageRepository:       adminMessageRepository,
		botRuntimeSettingsRepository: botRuntimeSettingsRepository,
		remnawaveClient:              remnawaveClient,
		tributeClient:                tributeClient,
		rollypayClient:               rollypayClient,
		ops:                          ops,
		subscriptionService:          subscriptionService,
		trafficWarningService:        trafficWarningService,
		pool:                         pool,
		build:                        build,
		loginLimiter:                 newRateLimiter(10, time.Minute),
	}
	h.mux = withLogging(withCSP(h.routes()))
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) routes() http.Handler {
	mux := http.NewServeMux()

	// Auth
	mux.HandleFunc("POST /admin/api/auth/login", h.handleLogin)
	mux.HandleFunc("POST /admin/api/auth/logout", h.requireAdminSession(h.handleLogout))
	mux.HandleFunc("GET /admin/api/auth/me", h.requireAdminSession(h.handleMe))

	// Dashboard
	mux.HandleFunc("GET /admin/api/dashboard/stats", h.requireAdminSession(h.handleDashboardStats))
	mux.HandleFunc("GET /admin/api/dashboard/revenue", h.requireAdminSession(h.handleDashboardRevenue))
	mux.HandleFunc("GET /admin/api/dashboard/growth", h.requireAdminSession(h.handleDashboardGrowth))
	mux.HandleFunc("GET /admin/api/dashboard/referrals", h.requireAdminSession(h.handleDashboardReferrals))
	mux.HandleFunc("GET /admin/api/dashboard/health", h.requireAdminSession(h.handleDashboardHealth))
	mux.HandleFunc("GET /admin/api/dashboard/activity", h.requireAdminSession(h.handleDashboardActivity))
	mux.HandleFunc("GET /admin/api/dashboard/header-stats", h.requireAdminSession(h.handleDashboardHeaderStats))

	// Activity feed (full paginated/filterable page, distinct from the dashboard widget above)
	mux.HandleFunc("GET /admin/api/activity", h.requireAdminSession(h.handleActivityList))
	mux.HandleFunc("GET /admin/api/activity/export.csv", h.requireAdminSession(h.handleActivityExportCSV))

	// Notification log (raw notification_log rows, distinct from the merged activity feed)
	mux.HandleFunc("GET /admin/api/notifications", h.requireAdminSession(h.handleNotificationList))
	mux.HandleFunc("GET /admin/api/notifications/stats", h.requireAdminSession(h.handleNotificationStats))
	mux.HandleFunc("POST /admin/api/notifications/{id}/resend", h.requireAdminSession(h.handleNotificationResend))

	// Users
	mux.HandleFunc("GET /admin/api/users", h.requireAdminSession(h.handleUsersList))
	mux.HandleFunc("GET /admin/api/users/{id}", h.requireAdminSession(h.handleUserDetail))
	mux.HandleFunc("GET /admin/api/users/{id}/orders", h.requireAdminSession(h.handleUserOrders))
	mux.HandleFunc("GET /admin/api/users/{id}/audit", h.requireAdminSession(h.handleUserAudit))
	mux.HandleFunc("GET /admin/api/users/{id}/referrals", h.requireAdminSession(h.handleUserReferrals))
	mux.HandleFunc("POST /admin/api/users/{id}/topup/preview", h.requireAdminSession(h.handleUserTopupPreview))
	mux.HandleFunc("POST /admin/api/users/{id}/topup", h.requireAdminSession(h.handleUserTopup))
	mux.HandleFunc("POST /admin/api/users/{id}/topup-enroll", h.requireAdminSession(h.handleUserTopupEnroll))
	mux.HandleFunc("POST /admin/api/users/{id}/reset-devices", h.requireAdminSession(h.handleUserResetDevices))
	mux.HandleFunc("POST /admin/api/users/{id}/reset-traffic", h.requireAdminSession(h.handleUserResetTraffic))
	mux.HandleFunc("POST /admin/api/users/{id}/status", h.requireAdminSession(h.handleUserStatus))
	mux.HandleFunc("POST /admin/api/users/{id}/extend", h.requireAdminSession(h.handleUserExtend))
	mux.HandleFunc("POST /admin/api/users/{id}/trial", h.requireAdminSession(h.handleUserTrial))
	mux.HandleFunc("POST /admin/api/users/{id}/tribute-autorenew", h.requireAdminSession(h.handleUserTributeAutorenew))
	mux.HandleFunc("POST /admin/api/users/{id}/message", h.requireAdminSession(h.handleUserSendMessage))
	mux.HandleFunc("GET /admin/api/users/{id}/messages", h.requireAdminSession(h.handleUserMessages))

	// Broadcast
	mux.HandleFunc("POST /admin/api/broadcast/preview", h.requireAdminSession(h.handleBroadcastPreview))
	mux.HandleFunc("POST /admin/api/broadcast/send", h.requireAdminSession(h.handleBroadcastSend))
	mux.HandleFunc("POST /admin/api/broadcast/test", h.requireAdminSession(h.handleBroadcastTest))
	mux.HandleFunc("GET /admin/api/broadcast/status/{jobId}", h.requireAdminSession(h.handleBroadcastStatus))

	// Referrals
	mux.HandleFunc("GET /admin/api/referrals", h.requireAdminSession(h.handleReferralsList))

	// Audit log
	mux.HandleFunc("GET /admin/api/audit", h.requireAdminSession(h.handleAuditList))

	// System
	mux.HandleFunc("GET /admin/api/system/settings", h.requireAdminSession(h.handleSystemSettingsGet))
	mux.HandleFunc("PATCH /admin/api/system/settings", h.requireAdminSession(h.handleSystemSettingsPatch))
	mux.HandleFunc("POST /admin/api/system/sync", h.requireAdminSession(h.handleSystemSync))
	mux.HandleFunc("POST /admin/api/system/fix-traffic-strategy/preview", h.requireAdminSession(h.handleFixStrategyPreview))
	mux.HandleFunc("POST /admin/api/system/fix-traffic-strategy/apply", h.requireAdminSession(h.handleFixStrategyApply))
	mux.HandleFunc("GET /admin/api/system/fix-traffic-strategy/status/{jobId}", h.requireAdminSession(h.handleFixStrategyStatus))

	// Orders
	mux.HandleFunc("GET /admin/api/orders", h.requireAdminSession(h.handleOrdersList))
	mux.HandleFunc("GET /admin/api/orders/{id}", h.requireAdminSession(h.handleOrderDetail))
	mux.HandleFunc("GET /admin/api/orders/export.csv", h.requireAdminSession(h.handleOrdersExportCSV))

	// Webhook inbox
	mux.HandleFunc("GET /admin/api/webhooks", h.requireAdminSession(h.handleWebhooksList))
	mux.HandleFunc("GET /admin/api/webhooks/{id}", h.requireAdminSession(h.handleWebhookDetail))
	mux.HandleFunc("POST /admin/api/webhooks/{id}/retry", h.requireAdminSession(h.handleWebhookRetry))

	// SPA (must be registered last / least specific — Go's ServeMux picks the most specific
	// pattern automatically regardless of registration order, but keeping it last mirrors how
	// it's reasoned about: "everything else under /admin/ is a client-side route").
	mux.Handle("/admin/", h.staticHandler())

	return mux
}
