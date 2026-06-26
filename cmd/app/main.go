package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"remnawave-tg-shop-bot/internal/cache"
	"remnawave-tg-shop-bot/internal/cardlink"
	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/cryptopay"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/handler"
	"remnawave-tg-shop-bot/internal/moynalog"
	"remnawave-tg-shop-bot/internal/notification"
	"remnawave-tg-shop-bot/internal/payment"
	"remnawave-tg-shop-bot/internal/remnawave"
	"remnawave-tg-shop-bot/internal/sync"
	"remnawave-tg-shop-bot/internal/translation"
	"remnawave-tg-shop-bot/internal/tribute"
	"remnawave-tg-shop-bot/internal/yookasa"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/robfig/cron/v3"
)

var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	config.InitConfig()
	slog.Info("Application starting", "version", Version, "commit", Commit, "buildDate", BuildDate)

	// Check if Moynalog is enabled
	var moynalogClient *moynalog.Client
	if config.IsMoynalogEnabled() {
		var err error
		moynalogClient, err = moynalog.NewClient(config.MoynalogUrl(), config.MoynalogUsername(), config.MoynalogPassword(), config.MoynalogProxyURL())
		if err != nil {
			log.Fatalf("Moynalog initialization error: %v", err)
		}

		slog.Info("Moynalog authentication successful")
	}

	tm := translation.GetInstance()
	err := tm.InitTranslations("./translations", config.DefaultLanguage())
	if err != nil {
		panic(err)
	}

	pool, err := initDatabase(ctx, config.DadaBaseUrl())
	if err != nil {
		panic(err)
	}

	err = database.RunMigrations(ctx, &database.MigrationConfig{Direction: "up", MigrationsPath: "./db/migrations", Steps: 0}, pool)
	if err != nil {
		panic(err)
	}
	cache := cache.NewCache(30 * time.Minute)
	customerRepository := database.NewCustomerRepository(pool)
	purchaseRepository := database.NewPurchaseRepository(pool)
	referralRepository := database.NewReferralRepository(pool)
	topupRepository := database.NewTrafficTopupRepository(pool)
	webhookInboxRepository := database.NewWebhookInboxRepository(pool)

	cryptoPayClient := cryptopay.NewCryptoPayClient(config.CryptoPayUrl(), config.CryptoPayToken())
	remnawaveClient := remnawave.NewClient(config.RemnawaveUrl(), config.RemnawaveToken(), config.RemnawaveMode())
	yookasaClient := yookasa.NewClient(config.YookasaUrl(), config.YookasaShopId(), config.YookasaSecretKey())
	botOpts := []bot.Option{bot.WithWorkers(3)}
	if proxyStr := config.TelegramProxyURL(); proxyStr != "" {
		proxyURL, parseErr := url.Parse(proxyStr)
		if parseErr != nil {
			panic(fmt.Sprintf("invalid TELEGRAM_PROXY_URL: %v", parseErr))
		}
		proxyClient := &http.Client{
			Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
			Timeout:   30 * time.Second,
		}
		botOpts = append(botOpts, bot.WithHTTPClient(30*time.Second, proxyClient))
		slog.Info("Telegram bot using proxy", "proxy", proxyURL.Host)
	}
	b, err := bot.New(config.TelegramToken(), botOpts...)
	if err != nil {
		panic(err)
	}

	paymentService := payment.NewPaymentService(tm, purchaseRepository, remnawaveClient, customerRepository, b, cryptoPayClient, yookasaClient, referralRepository, cache, moynalogClient, topupRepository)

	var cardlinkClient *cardlink.Client
	if config.CardlinkTopupEnabled() {
		cardlinkClient = cardlink.NewClient(config.CardlinkBaseURL(), config.CardlinkAPIToken())
		slog.Info("Cardlink topup client initialized")
	}

	cronScheduler := setupInvoiceChecker(purchaseRepository, cryptoPayClient, paymentService, yookasaClient)
	if cronScheduler != nil {
		cronScheduler.Start()
		defer cronScheduler.Stop()
	}

	if cardlinkClient != nil {
		cardlinkCron := setupCardlinkTopupChecker(topupRepository, cardlinkClient, remnawaveClient, b)
		cardlinkCron.Start()
		defer cardlinkCron.Stop()
	}

	subService := notification.NewSubscriptionService(customerRepository, purchaseRepository, paymentService, b, tm)

	subscriptionNotificationCronScheduler := subscriptionChecker(subService)
	subscriptionNotificationCronScheduler.Start()
	defer subscriptionNotificationCronScheduler.Stop()

	trafficSvc := notification.NewTrafficWarningService(customerRepository, remnawaveClient, b, tm)
	trafficCron := setupTrafficWarningCron(trafficSvc)
	trafficCron.Start()
	defer trafficCron.Stop()

	topupCleanupCron := setupTopupCleanup(topupRepository)
	topupCleanupCron.Start()
	defer topupCleanupCron.Stop()

	topupIntegritySvc := notification.NewTopupIntegrityService(topupRepository, remnawaveClient)
	topupIntegrityCron := setupTopupIntegrityCron(topupIntegritySvc)
	topupIntegrityCron.Start()
	defer topupIntegrityCron.Stop()

	syncService := sync.NewSyncService(remnawaveClient, customerRepository)

	h := handler.NewHandler(syncService, paymentService, tm, customerRepository, purchaseRepository, cryptoPayClient, yookasaClient, referralRepository, cache, remnawaveClient, topupRepository, cardlinkClient)

	me, err := b.GetMe(ctx)
	if err != nil {
		panic(err)
	}

	if _, err = b.SetChatMenuButton(ctx, &bot.SetChatMenuButtonParams{
		MenuButton: &models.MenuButtonCommands{
			Type: models.MenuButtonTypeCommands,
		},
	}); err != nil {
		slog.Warn("bot setup: SetChatMenuButton failed", "error", err)
	}

	if _, err = b.SetMyCommands(ctx, &bot.SetMyCommandsParams{
		Commands: []models.BotCommand{
			{Command: "start", Description: "Начать работу с ботом"},
		},
		LanguageCode: "ru",
	}); err != nil {
		slog.Warn("bot setup: SetMyCommands (ru) failed", "error", err)
	}

	if _, err = b.SetMyCommands(ctx, &bot.SetMyCommandsParams{
		Commands: []models.BotCommand{
			{Command: "start", Description: "Start using the bot"},
		},
		LanguageCode: "en",
	}); err != nil {
		slog.Warn("bot setup: SetMyCommands (en) failed", "error", err)
	}

	config.SetBotURL(fmt.Sprintf("https://t.me/%s", me.Username))

	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypePrefix, h.StartCommandHandler, h.SuspiciousUserFilterMiddleware)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/sync", bot.MatchTypeExact, h.SyncUsersCommandHandler, isAdminMiddleware)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/fix_traffic_strategy", bot.MatchTypePrefix, h.FixTrafficStrategyCommandHandler, isAdminMiddleware)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/admin_user", bot.MatchTypePrefix, h.AdminUserCommandHandler, isAdminMiddleware)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/admin_topup_enroll", bot.MatchTypePrefix, h.AdminTopupEnrollCommandHandler, isAdminMiddleware)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/admin_topup", bot.MatchTypePrefix, h.AdminTopupCommandHandler, isAdminMiddleware)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/admin_reset_devices", bot.MatchTypePrefix, h.AdminResetDevicesCommandHandler, isAdminMiddleware)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/admin_broadcast", bot.MatchTypeExact, h.AdminBroadcastCommandHandler, isAdminMiddleware)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/cancel", bot.MatchTypeExact, h.AdminCancelCommandHandler, isAdminMiddleware)
	b.RegisterHandlerMatchFunc(func(update *models.Update) bool {
		if update.Message == nil || update.Message.From == nil {
			return false
		}
		if update.Message.From.ID != config.GetAdminTelegramId() {
			return false
		}
		if strings.HasPrefix(update.Message.Text, "/") {
			return false
		}
		return h.IsBroadcastTextPending(update.Message.Chat.ID)
	}, h.AdminBroadcastTextHandler)
	b.RegisterHandlerMatchFunc(func(update *models.Update) bool {
		if update.Message == nil || update.Message.From == nil {
			return false
		}
		if update.Message.From.ID != config.GetAdminTelegramId() {
			return false
		}
		if strings.HasPrefix(update.Message.Text, "/") {
			return false
		}
		return h.IsAdminSessionActive(update.Message.Chat.ID)
	}, h.AdminPanelTextHandler)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackBroadcastConfirm, bot.MatchTypeExact, h.AdminBroadcastConfirmCallback, h.AnswerCallbackQueryMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackBroadcastConfirmExpired, bot.MatchTypeExact, h.AdminBroadcastConfirmExpiredCallback, h.AnswerCallbackQueryMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackBroadcastConfirmInactive, bot.MatchTypeExact, h.AdminBroadcastConfirmInactiveCallback, h.AnswerCallbackQueryMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackBroadcastConfirmNew, bot.MatchTypeExact, h.AdminBroadcastConfirmNewCallback, h.AnswerCallbackQueryMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackBroadcastConfirmAll, bot.MatchTypeExact, h.AdminBroadcastConfirmAllCallback, h.AnswerCallbackQueryMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackBroadcastTest, bot.MatchTypeExact, h.AdminBroadcastTestCallback, h.AnswerCallbackQueryMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackBroadcastCancel, bot.MatchTypeExact, h.AdminBroadcastCancelCallback, h.AnswerCallbackQueryMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackAdminPanelMenu, bot.MatchTypeExact, h.AdminPanelMenuCallback, h.AnswerCallbackQueryMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackAdminPanelStats, bot.MatchTypeExact, h.AdminPanelStatsCallback, h.AnswerCallbackQueryMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackAdminPanelBcast, bot.MatchTypeExact, h.AdminPanelBcastCallback, h.AnswerCallbackQueryMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackAdminPanelSystem, bot.MatchTypeExact, h.AdminPanelSystemCallback, h.AnswerCallbackQueryMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackAdminPanelSync, bot.MatchTypeExact, h.AdminPanelSyncCallback, h.AnswerCallbackQueryMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackAdminPanelUsers, bot.MatchTypeExact, h.AdminPanelUsersCallback, h.AnswerCallbackQueryMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackAdminUserTopup, bot.MatchTypePrefix, h.AdminUserTopupCallback, h.AnswerCallbackQueryMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackAdminUserExtend, bot.MatchTypePrefix, h.AdminUserExtendCallback, h.AnswerCallbackQueryMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackAdminUserEnable, bot.MatchTypePrefix, h.AdminUserEnableCallback, h.AnswerCallbackQueryMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackAdminUserDisable, bot.MatchTypePrefix, h.AdminUserDisableCallback, h.AnswerCallbackQueryMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackAdminUserResetDevices, bot.MatchTypePrefix, h.AdminUserResetDevicesCallback, h.AnswerCallbackQueryMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackAdminUserResetTraffic, bot.MatchTypePrefix, h.AdminUserResetTrafficCallback, h.AnswerCallbackQueryMiddleware)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/admin", bot.MatchTypeExact, h.AdminMenuCommandHandler, isAdminMiddleware)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/admin_set_trial", bot.MatchTypePrefix, h.AdminSetTrialCommandHandler, isAdminMiddleware)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/admin_extend", bot.MatchTypePrefix, h.AdminExtendCommandHandler, isAdminMiddleware)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/admin_disable", bot.MatchTypePrefix, h.AdminDisableCommandHandler, isAdminMiddleware)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/admin_enable", bot.MatchTypePrefix, h.AdminEnableCommandHandler, isAdminMiddleware)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/admin_reset_traffic", bot.MatchTypePrefix, h.AdminResetTrafficCommandHandler, isAdminMiddleware)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/admin_stats", bot.MatchTypeExact, h.AdminStatsCommandHandler, isAdminMiddleware)

	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackReferral, bot.MatchTypeExact, h.ReferralCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackBuy, bot.MatchTypeExact, h.BuyCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackTrial, bot.MatchTypeExact, h.TrialCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackActivateTrial, bot.MatchTypeExact, h.ActivateTrialCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackStart, bot.MatchTypeExact, h.StartCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackSell, bot.MatchTypePrefix, h.SellCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackConnect, bot.MatchTypeExact, h.ConnectCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackTopup, bot.MatchTypeExact, h.TopupCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackTopupSelect, bot.MatchTypePrefix, h.TopupSelectCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackTopupCancel, bot.MatchTypePrefix, h.TopupCancelCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackStatus, bot.MatchTypeExact, h.StatusCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackDevices, bot.MatchTypeExact, h.DevicesCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackDevicesDeleteDevice, bot.MatchTypePrefix, h.DevicesDeleteDeviceCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackDevicesReset, bot.MatchTypeExact, h.DevicesResetCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackDevicesResetConfirm, bot.MatchTypeExact, h.DevicesResetConfirmCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackPayment, bot.MatchTypePrefix, h.PaymentCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandlerMatchFunc(func(update *models.Update) bool {
		return update.PreCheckoutQuery != nil
	}, h.PreCheckoutCallbackHandler, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)

	b.RegisterHandlerMatchFunc(func(update *models.Update) bool {
		return update.Message != nil && update.Message.SuccessfulPayment != nil
	}, h.SuccessPaymentHandler, h.SuspiciousUserFilterMiddleware)

	mux := http.NewServeMux()
	mux.Handle("/healthcheck", fullHealthHandler(pool, remnawaveClient))
	if config.GetTributeWebHookUrl() != "" {
		tributeClient := tribute.NewClient(paymentService, customerRepository, topupRepository, webhookInboxRepository, remnawaveClient, b)
		mux.Handle(config.GetTributeWebHookUrl(), tributeClient.WebHookHandler())
		webhookRetryCron := setupWebhookRetryCron(tributeClient)
		webhookRetryCron.Start()
		defer webhookRetryCron.Stop()
	}

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", config.GetHealthCheckPort()),
		Handler: mux,
	}
	go func() {
		log.Printf("Server listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	slog.Info("Bot is starting...")
	b.Start(ctx)

	log.Println("Shutting down health server…")
	shutdownCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Health server shutdown error: %v", err)
	}
}

func fullHealthHandler(pool *pgxpool.Pool, rw *remnawave.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := map[string]string{
			"status":    "ok",
			"db":        "ok",
			"rw":        "ok",
			"time":      time.Now().Format(time.RFC3339),
			"version":   Version,
			"commit":    Commit,
			"buildDate": BuildDate,
		}

		dbCtx, dbCancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer dbCancel()
		if err := pool.Ping(dbCtx); err != nil {
			status["status"] = "fail"
			status["db"] = "error: " + err.Error()
		}

		rwCtx, rwCancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer rwCancel()
		if err := rw.Ping(rwCtx); err != nil {
			status["status"] = "fail"
			status["rw"] = "error: " + err.Error()
		}

		w.Header().Set("Content-Type", "application/json")
		if status["status"] == "ok" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		fmt.Fprintf(w, `{"status":"%s","db":"%s","remnawave":"%s","time":"%s","version":"%s","commit":"%s","buildDate":"%s"}`,
			status["status"], status["db"], status["rw"], status["time"], Version, Commit, BuildDate)
	})
}

func isAdminMiddleware(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message != nil && update.Message.From.ID == config.GetAdminTelegramId() {
			next(ctx, b, update)
		} else {
			return
		}
	}
}

func subscriptionChecker(subService *notification.SubscriptionService) *cron.Cron {
	c := cron.New()

	_, err := c.AddFunc("0 */4 * * *", func() {
		err := subService.ProcessSubscriptionExpiration()
		if err != nil {
			slog.Error("Error sending subscription notifications", "error", err)
		}
	})

	if err != nil {
		panic(err)
	}
	return c
}

func initDatabase(ctx context.Context, connString string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, err
	}

	config.MaxConns = 20
	config.MinConns = 5

	return pgxpool.ConnectConfig(ctx, config)
}

func setupTopupIntegrityCron(svc *notification.TopupIntegrityService) *cron.Cron {
	c := cron.New()
	_, err := c.AddFunc("0 */6 * * *", func() { // every 6 hours
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := svc.CheckAndReapply(ctx); err != nil {
			slog.Error("topup integrity cron error", "error", err)
		}
	})
	if err != nil {
		panic(err)
	}
	return c
}

func setupTopupCleanup(topupRepository *database.TrafficTopupRepository) *cron.Cron {
	c := cron.New()
	_, err := c.AddFunc("0 * * * *", func() { // every hour
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		n, err := topupRepository.ExpireOldPending(ctx, 2*time.Hour)
		if err != nil {
			slog.Error("topup cleanup: expire old pending", "error", err)
			return
		}
		if n > 0 {
			slog.Info("topup cleanup: expired stale pending records", "count", n)
		}
	})
	if err != nil {
		panic(err)
	}
	return c
}

func setupWebhookRetryCron(tributeClient *tribute.Client) *cron.Cron {
	c := cron.New()
	_, err := c.AddFunc("*/10 * * * *", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tributeClient.RetryFailed(ctx)
	})
	if err != nil {
		panic(err)
	}
	return c
}

func setupTrafficWarningCron(svc *notification.TrafficWarningService) *cron.Cron {
	c := cron.New()
	_, err := c.AddFunc("0 12 * * *", func() {
		if err := svc.CheckAndNotify(); err != nil {
			slog.Error("traffic warning cron error", "error", err)
		}
	})
	if err != nil {
		panic(err)
	}
	return c
}

func setupInvoiceChecker(
	purchaseRepository *database.PurchaseRepository,
	cryptoPayClient *cryptopay.Client,
	paymentService *payment.PaymentService,
	yookasaClient *yookasa.Client) *cron.Cron {
	if !config.IsYookasaEnabled() && !config.IsCryptoPayEnabled() {
		return nil
	}
	c := cron.New(cron.WithSeconds())

	if config.IsCryptoPayEnabled() {
		_, err := c.AddFunc("*/5 * * * * *", func() {
			ctx := context.Background()
			checkCryptoPayInvoice(ctx, purchaseRepository, cryptoPayClient, paymentService)
		})

		if err != nil {
			panic(err)
		}
	}

	if config.IsYookasaEnabled() {
		_, err := c.AddFunc("*/5 * * * * *", func() {
			ctx := context.Background()
			checkYookasaInvoice(ctx, purchaseRepository, yookasaClient, paymentService)
		})

		if err != nil {
			panic(err)
		}
	}

	return c
}

func checkYookasaInvoice(
	ctx context.Context,
	purchaseRepository *database.PurchaseRepository,
	yookasaClient *yookasa.Client,
	paymentService *payment.PaymentService,
) {
	pendingPurchases, err := purchaseRepository.FindByInvoiceTypeAndStatus(
		ctx,
		database.InvoiceTypeYookasa,
		database.PurchaseStatusPending,
	)
	if err != nil {
		log.Printf("Error finding pending purchases: %v", err)
		return
	}
	if len(*pendingPurchases) == 0 {
		return
	}

	for _, purchase := range *pendingPurchases {

		invoice, err := yookasaClient.GetPayment(ctx, *purchase.YookasaID)

		if err != nil {
			slog.Error("Error getting invoice", "invoiceId", purchase.YookasaID, "error", err)
			continue
		}

		if invoice.IsCancelled() {
			err := paymentService.CancelYookassaPayment(purchase.ID)
			if err != nil {
				slog.Error("Error canceling invoice", "invoiceId", invoice.ID, "purchaseId", purchase.ID, "error", err)
			}
			continue
		}

		if !invoice.Paid {
			continue
		}

		purchaseId, err := strconv.Atoi(invoice.Metadata["purchaseId"])
		if err != nil {
			slog.Error("Error parsing purchaseId", "invoiceId", invoice.ID, "error", err)
			continue
		}
		ctxWithValue := context.WithValue(ctx, remnawave.CtxKeyUsername, invoice.Metadata["username"])
		err = paymentService.ProcessPurchaseById(ctxWithValue, int64(purchaseId))
		if err != nil {
			slog.Error("Error processing invoice", "invoiceId", invoice.ID, "purchaseId", purchaseId, "error", err)
		} else {
			slog.Info("Invoice processed", "invoiceId", invoice.ID, "purchaseId", purchaseId)
		}

	}
}

func checkCryptoPayInvoice(
	ctx context.Context,
	purchaseRepository *database.PurchaseRepository,
	cryptoPayClient *cryptopay.Client,
	paymentService *payment.PaymentService,
) {
	pendingPurchases, err := purchaseRepository.FindByInvoiceTypeAndStatus(
		ctx,
		database.InvoiceTypeCrypto,
		database.PurchaseStatusPending,
	)
	if err != nil {
		log.Printf("Error finding pending purchases: %v", err)
		return
	}
	if len(*pendingPurchases) == 0 {
		return
	}

	var invoiceIDs []string

	for _, purchase := range *pendingPurchases {
		if purchase.CryptoInvoiceID != nil {
			invoiceIDs = append(invoiceIDs, fmt.Sprintf("%d", *purchase.CryptoInvoiceID))
		}
	}

	if len(invoiceIDs) == 0 {
		return
	}

	stringInvoiceIDs := strings.Join(invoiceIDs, ",")
	invoices, err := cryptoPayClient.GetInvoices("", "", "", stringInvoiceIDs, 0, 0)
	if err != nil {
		log.Printf("Error getting invoices: %v", err)
		return
	}

	for _, invoice := range *invoices {
		if invoice.InvoiceID != nil && invoice.IsPaid() {
			payload := strings.Split(invoice.Payload, "&")
			purchaseID, err := strconv.Atoi(strings.Split(payload[0], "=")[1])
			if err != nil {
				slog.Error("crypto: invalid purchaseID in payload", "payload", payload[0], "error", err)
				continue
			}
			username := strings.Split(payload[1], "=")[1]
			ctxWithUsername := context.WithValue(ctx, remnawave.CtxKeyUsername, username)
			err = paymentService.ProcessPurchaseById(ctxWithUsername, int64(purchaseID))
			if err != nil {
				slog.Error("Error processing invoice", "invoiceId", invoice.InvoiceID, "error", err)
			} else {
				slog.Info("Invoice processed", "invoiceId", invoice.InvoiceID, "purchaseId", purchaseID)
			}

		}
	}

}

func setupCardlinkTopupChecker(
	topupRepository *database.TrafficTopupRepository,
	cardlinkClient *cardlink.Client,
	remnawaveClient *remnawave.Client,
	telegramBot *bot.Bot,
) *cron.Cron {
	c := cron.New(cron.WithSeconds())
	_, err := c.AddFunc("*/5 * * * * *", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		checkCardlinkTopups(ctx, topupRepository, cardlinkClient, remnawaveClient, telegramBot)
	})
	if err != nil {
		panic(err)
	}
	return c
}

func checkCardlinkTopups(
	ctx context.Context,
	topupRepository *database.TrafficTopupRepository,
	cardlinkClient *cardlink.Client,
	remnawaveClient *remnawave.Client,
	telegramBot *bot.Bot,
) {
	pendingTopups, err := topupRepository.ClaimPendingCardlink(ctx)
	if err != nil {
		slog.Error("cardlink topup: find pending", "error", err)
		return
	}
	if len(pendingTopups) == 0 {
		return
	}

	for _, topup := range pendingTopups {
		if topup.CardlinkBillID == nil {
			continue
		}

		bill, err := cardlinkClient.GetBillStatus(ctx, *topup.CardlinkBillID)
		if err != nil {
			slog.Error("cardlink topup: get bill status", "bill_id", *topup.CardlinkBillID, "error", err)
			continue
		}

		if bill.IsFailed() {
			if err := topupRepository.MarkFailed(ctx, topup.ID); err != nil {
				slog.Error("cardlink topup: mark failed", "topup_id", topup.ID, "error", err)
			}
			slog.Info("cardlink topup: bill failed", "bill_id", *topup.CardlinkBillID, "topup_id", topup.ID)
			continue
		}

		if !bill.IsPaid() {
			continue
		}

		rwUsers, err := remnawaveClient.GetUsersByTelegramID(ctx, topup.TelegramID)
		if err != nil || len(rwUsers) == 0 {
			slog.Error("cardlink topup: get remnawave user", "telegram_id", topup.TelegramID, "error", err)
			if err := topupRepository.MarkFailed(ctx, topup.ID); err != nil {
				slog.Error("cardlink topup: mark failed after rw lookup", "topup_id", topup.ID, "error", err)
			}
			sendMessage(ctx, telegramBot, topup.TelegramID, "❌ Ошибка зачисления трафика: аккаунт не найден. Обратитесь в поддержку.")
			continue
		}
		rwUser := rwUsers[0]

		if rwUser.TrafficLimitBytes == 0 {
			slog.Warn("cardlink topup: user has unlimited traffic", "telegram_id", topup.TelegramID)
			if err := topupRepository.MarkCompleted(ctx, topup.ID); err != nil {
				slog.Error("cardlink topup: mark completed (unlimited)", "topup_id", topup.ID, "error", err)
			}
			sendMessage(ctx, telegramBot, topup.TelegramID, "ℹ️ У тебя безлимитный тариф — дополнительный трафик не нужен. Обратись в поддержку для возврата.")
			continue
		}

		targetBytes := int64(rwUser.TrafficLimitBytes) + int64(topup.GBAmount)*int64(config.BytesInGigabyte())
		rwUUID := rwUser.UUID

		if err := remnawaveClient.UpdateUserTrafficLimit(ctx, rwUUID, int(targetBytes), rwUser.TrafficLimitStrategy); err != nil {
			if err := topupRepository.MarkFailed(ctx, topup.ID); err != nil {
				slog.Error("cardlink topup: mark failed after rw update", "topup_id", topup.ID, "error", err)
			}
			slog.Error("cardlink topup: update traffic limit", "telegram_id", topup.TelegramID, "error", err)
			sendMessage(ctx, telegramBot, config.GetAdminTelegramId(), fmt.Sprintf("Cardlink top-up: Remnawave update failed for telegram_id=%d, pkg=%dGB: %v", topup.TelegramID, topup.GBAmount, err))
			continue
		}

		if err := topupRepository.MarkCompleted(ctx, topup.ID); err != nil {
			slog.Error("cardlink topup: mark completed failed (traffic already credited)", "topup_id", topup.ID, "error", err)
		}
		newLimitGB := int(targetBytes) / config.BytesInGigabyte()
		msg := fmt.Sprintf("✅ Зачислено <b>+%d ГБ</b>.\nТекущий лимит трафика: <b>%d ГБ</b>.", topup.GBAmount, newLimitGB)
		sendMessage(ctx, telegramBot, topup.TelegramID, msg)
		slog.Info("cardlink topup: completed", "telegram_id", topup.TelegramID, "gb_amount", topup.GBAmount, "new_limit_gb", newLimitGB)
	}
}

func sendMessage(ctx context.Context, b *bot.Bot, chatID int64, text string) {
	_, err := b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text, ParseMode: models.ParseModeHTML})
	if err != nil {
		slog.Error("sendMessage failed", "chat_id", chatID, "error", err)
	}
}
