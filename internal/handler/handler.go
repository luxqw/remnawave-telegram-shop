package handler

import (
	"remnawave-tg-shop-bot/internal/cache"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/payment"
	"remnawave-tg-shop-bot/internal/remnawave"
	"remnawave-tg-shop-bot/internal/rollypay"
	isync "remnawave-tg-shop-bot/internal/sync"
	"remnawave-tg-shop-bot/internal/translation"
)

type Handler struct {
	customerRepository     *database.CustomerRepository
	purchaseRepository     *database.PurchaseRepository
	rollypayClient         *rollypay.Client
	translation            *translation.Manager
	paymentService         *payment.PaymentService
	syncService            *isync.SyncService
	referralRepository     *database.ReferralRepository
	cache                  *cache.Cache
	remnawaveClient        *remnawave.Client
	topupRepository        *database.TrafficTopupRepository
	deviceTopupRepository  *database.DeviceTopupRepository
	deviceAddonRepository  *database.DeviceAddonRepository
	adminMessageRepository *database.AdminMessageRepository
	// topupAwaitingInput tracks telegram IDs currently expected to send a free-text GB amount for
	// the custom top-up flow, mapping to the prompt message ID (for editing it in place). Reuses
	// cache.Cache verbatim rather than a new type — same TTL-expiring-map shape already used for
	// pending purchase message IDs elsewhere in this handler.
	topupAwaitingInput *cache.Cache
	// topupInvoiceCache/deviceTopupInvoiceCache map a pending TrafficTopup/DeviceTopup ID to its
	// invoice message ID, mirroring the subscription flow's `cache` field (purchaseId -> messageId)
	// so rollypay.WebhookClient can delete the stale Pay/Cancel message on completion instead of
	// leaving it stuck on screen. Separate instances rather than reusing `cache`: TrafficTopup,
	// DeviceTopup, and Purchase IDs come from different DB sequences and can collide on the same
	// int64 key within one shared map.
	topupInvoiceCache       *cache.Cache
	deviceTopupInvoiceCache *cache.Cache
	// deviceManageAwaitingInput mirrors topupAwaitingInput: telegram ID -> prompt message ID, for
	// the "type an exact target slot count" flow in DeviceManageCallbackHandler.
	deviceManageAwaitingInput *cache.Cache
}

func NewHandler(
	syncService *isync.SyncService,
	paymentService *payment.PaymentService,
	translation *translation.Manager,
	customerRepository *database.CustomerRepository,
	purchaseRepository *database.PurchaseRepository,
	rollypayClient *rollypay.Client,
	referralRepository *database.ReferralRepository,
	cache *cache.Cache,
	remnawaveClient *remnawave.Client,
	topupRepository *database.TrafficTopupRepository,
	deviceTopupRepository *database.DeviceTopupRepository,
	deviceAddonRepository *database.DeviceAddonRepository,
	adminMessageRepository *database.AdminMessageRepository,
	topupAwaitingInput *cache.Cache,
	topupInvoiceCache *cache.Cache,
	deviceTopupInvoiceCache *cache.Cache,
	deviceManageAwaitingInput *cache.Cache,
) *Handler {
	return &Handler{
		syncService:               syncService,
		paymentService:            paymentService,
		customerRepository:        customerRepository,
		purchaseRepository:        purchaseRepository,
		rollypayClient:            rollypayClient,
		translation:               translation,
		referralRepository:        referralRepository,
		cache:                     cache,
		remnawaveClient:           remnawaveClient,
		topupRepository:           topupRepository,
		deviceTopupRepository:     deviceTopupRepository,
		deviceAddonRepository:     deviceAddonRepository,
		adminMessageRepository:    adminMessageRepository,
		topupAwaitingInput:        topupAwaitingInput,
		topupInvoiceCache:         topupInvoiceCache,
		deviceTopupInvoiceCache:   deviceTopupInvoiceCache,
		deviceManageAwaitingInput: deviceManageAwaitingInput,
	}
}
