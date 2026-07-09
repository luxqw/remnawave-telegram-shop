package handler

import (
	"sync"

	"remnawave-tg-shop-bot/internal/adminops"
	"remnawave-tg-shop-bot/internal/cache"
	"remnawave-tg-shop-bot/internal/cryptopay"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/payment"
	"remnawave-tg-shop-bot/internal/remnawave"
	isync "remnawave-tg-shop-bot/internal/sync"
	"remnawave-tg-shop-bot/internal/translation"
	"remnawave-tg-shop-bot/internal/yookasa"
)

type Handler struct {
	customerRepository *database.CustomerRepository
	purchaseRepository *database.PurchaseRepository
	cryptoPayClient    *cryptopay.Client
	yookasaClient      *yookasa.Client
	translation        *translation.Manager
	paymentService     *payment.PaymentService
	syncService        *isync.SyncService
	referralRepository *database.ReferralRepository
	cache              *cache.Cache
	remnawaveClient    *remnawave.Client
	topupRepository    *database.TrafficTopupRepository
	auditLogRepository *database.AdminAuditLogRepository
	// adminOps holds the Telegram-agnostic business logic behind every admin mutating action.
	// The bot handlers in this package are thin adapters over it (see admin_panel.go,
	// admin_handler.go, admin_fix_strategy.go); the admin web app calls the same instance.
	adminOps *adminops.Service
	// broadcastSessions and adminSessions are pointers so value-receiver methods share the same map.
	broadcastSessions *sync.Map
	adminSessions     *sync.Map
}

func NewHandler(
	syncService *isync.SyncService,
	paymentService *payment.PaymentService,
	translation *translation.Manager,
	customerRepository *database.CustomerRepository,
	purchaseRepository *database.PurchaseRepository,
	cryptoPayClient *cryptopay.Client,
	yookasaClient *yookasa.Client,
	referralRepository *database.ReferralRepository,
	cache *cache.Cache,
	remnawaveClient *remnawave.Client,
	topupRepository *database.TrafficTopupRepository,
	auditLogRepository *database.AdminAuditLogRepository,
	adminOps *adminops.Service,
) *Handler {
	return &Handler{
		syncService:        syncService,
		paymentService:     paymentService,
		customerRepository: customerRepository,
		purchaseRepository: purchaseRepository,
		cryptoPayClient:    cryptoPayClient,
		yookasaClient:      yookasaClient,
		translation:        translation,
		referralRepository: referralRepository,
		cache:              cache,
		remnawaveClient:    remnawaveClient,
		topupRepository:    topupRepository,
		auditLogRepository: auditLogRepository,
		adminOps:           adminOps,
		broadcastSessions:  &sync.Map{},
		adminSessions:      &sync.Map{},
	}
}
