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
	"remnawave-tg-shop-bot/internal/adminops"
	"remnawave-tg-shop-bot/internal/cache"
	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/handler"
	"remnawave-tg-shop-bot/internal/notification"
	"remnawave-tg-shop-bot/internal/payment"
	"remnawave-tg-shop-bot/internal/remnawave"
	"remnawave-tg-shop-bot/internal/rollypay"
	"remnawave-tg-shop-bot/internal/sync"
	"remnawave-tg-shop-bot/internal/translation"
	"remnawave-tg-shop-bot/internal/tribute"
	"remnawave-tg-shop-bot/internal/webapp"
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

	tm := translation.GetInstance()
	var disabledLanguages []string
	if !config.EnglishEnabled() {
		disabledLanguages = append(disabledLanguages, "en")
	}
	err := tm.InitTranslations("./translations", config.DefaultLanguage(), disabledLanguages...)
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
	topupInputCache := cache.NewCache(3 * time.Minute)
	cache := cache.NewCache(30 * time.Minute)
	customerRepository := database.NewCustomerRepository(pool)
	purchaseRepository := database.NewPurchaseRepository(pool)
	referralRepository := database.NewReferralRepository(pool)
	topupRepository := database.NewTrafficTopupRepository(pool)
	deviceTopupRepository := database.NewDeviceTopupRepository(pool)
	deviceAddonRepository := database.NewDeviceAddonRepository(pool)
	auditLogRepository := database.NewAdminAuditLogRepository(pool)
	webhookInboxRepository := database.NewWebhookInboxRepository(pool)
	activityRepository := database.NewActivityRepository(pool)
	notificationLogRepository := database.NewNotificationLogRepository(pool)
	adminMessageRepository := database.NewAdminMessageRepository(pool)
	botRuntimeSettingsRepository := database.NewBotRuntimeSettingsRepository(pool)

	if storedSettings, err := botRuntimeSettingsRepository.FindAll(ctx); err != nil {
		slog.Error("Failed to load runtime settings, using .env defaults only", "error", err)
	} else {
		config.ApplyRuntimeSettings(storedSettings)
	}

	remnawaveClient := remnawave.NewClient(config.RemnawaveUrl(), config.RemnawaveToken(), config.RemnawaveMode())

	var rollypayClient *rollypay.Client
	if config.IsRollyPayEnabled() {
		rollypayClient = rollypay.NewClient(config.RollyPayAPIKey(), config.RollyPaySigningSecret(), config.RollyPayTerminalID())
	}
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

	paymentService := payment.NewPaymentService(tm, purchaseRepository, remnawaveClient, customerRepository, b, rollypayClient, referralRepository, cache, topupRepository, deviceAddonRepository)

	subService := notification.NewSubscriptionService(customerRepository, purchaseRepository, paymentService, b, tm, notificationLogRepository)

	subscriptionNotificationCronScheduler := subscriptionChecker(subService)
	subscriptionNotificationCronScheduler.Start()
	defer subscriptionNotificationCronScheduler.Stop()

	trafficSvc := notification.NewTrafficWarningService(customerRepository, remnawaveClient, b, tm, notificationLogRepository)
	trafficCron := setupTrafficWarningCron(trafficSvc)
	trafficCron.Start()
	defer trafficCron.Stop()

	topupCleanupCron := setupTopupCleanup(topupRepository, deviceTopupRepository)
	topupCleanupCron.Start()
	defer topupCleanupCron.Stop()

	topupIntegritySvc := notification.NewTopupIntegrityService(topupRepository, remnawaveClient)
	topupIntegrityCron := setupTopupIntegrityCron(topupIntegritySvc)
	topupIntegrityCron.Start()
	defer topupIntegrityCron.Stop()

	syncService := sync.NewSyncService(remnawaveClient, customerRepository)

	// tributeClient is constructed here (rather than only inside the webhook-route block below)
	// because both the admin webapp and adminops-adjacent wiring need a reference to it; it stays
	// nil when Tribute webhooks aren't configured, and every consumer treats that as optional.
	var tributeClient *tribute.Client
	if config.GetTributeWebHookUrl() != "" {
		tributeClient = tribute.NewClient(paymentService, customerRepository, webhookInboxRepository, remnawaveClient, b, tm)
	}

	// rollypayWebhookClient is likewise nil when RollyPay isn't configured; every consumer
	// (webhook route, retry cron, admin webapp) treats that as optional, same as tributeClient.
	var rollypayWebhookClient *rollypay.WebhookClient
	if config.IsRollyPayEnabled() {
		rollypayWebhookClient = rollypay.NewWebhookClient(rollypayClient, paymentService, purchaseRepository, customerRepository, topupRepository, deviceTopupRepository, deviceAddonRepository, webhookInboxRepository, remnawaveClient, b, tm)

		deviceAddonRenewalSvc := notification.NewDeviceAddonRenewalService(deviceAddonRepository, rollypayClient, remnawaveClient, b, tm, notificationLogRepository)
		deviceAddonRenewalCron := deviceAddonRenewalChecker(deviceAddonRenewalSvc)
		deviceAddonRenewalCron.Start()
		defer deviceAddonRenewalCron.Stop()

		deviceAddonGraceCron := deviceAddonGraceChecker(deviceAddonRenewalSvc)
		deviceAddonGraceCron.Start()
		defer deviceAddonGraceCron.Stop()
	}

	opsService := adminops.NewService(customerRepository, purchaseRepository, topupRepository, referralRepository, auditLogRepository, webhookInboxRepository, notificationLogRepository, adminMessageRepository, botRuntimeSettingsRepository, remnawaveClient, syncService, b, tm)

	h := handler.NewHandler(syncService, paymentService, tm, customerRepository, purchaseRepository, rollypayClient, referralRepository, cache, remnawaveClient, topupRepository, deviceTopupRepository, deviceAddonRepository, adminMessageRepository, topupInputCache)

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
	b.RegisterHandler(bot.HandlerTypeMessageText, "/admin", bot.MatchTypeExact, h.AdminMenuCommandHandler, isAdminMiddleware)

	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackReferral, bot.MatchTypeExact, h.ReferralCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackReferralList, bot.MatchTypePrefix, h.ReferralListCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackBuy, bot.MatchTypeExact, h.BuyCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackTrial, bot.MatchTypeExact, h.TrialCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackActivateTrial, bot.MatchTypeExact, h.ActivateTrialCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackStart, bot.MatchTypeExact, h.StartCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackConnect, bot.MatchTypeExact, h.ConnectCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackTopup, bot.MatchTypeExact, h.TopupCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackTopupSelect, bot.MatchTypePrefix, h.TopupSelectCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackTopupCancel, bot.MatchTypePrefix, h.TopupCancelCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackTopupCustom, bot.MatchTypeExact, h.TopupCustomCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackStatus, bot.MatchTypeExact, h.StatusCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackDevices, bot.MatchTypeExact, h.DevicesCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackDevicesDeleteDevice, bot.MatchTypePrefix, h.DevicesDeleteDeviceCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackDevicesReset, bot.MatchTypeExact, h.DevicesResetCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackDevicesResetConfirm, bot.MatchTypeExact, h.DevicesResetConfirmCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackDeviceBuy, bot.MatchTypeExact, h.DeviceBuyCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackPayment, bot.MatchTypePrefix, h.PaymentCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackPaymentCancel, bot.MatchTypePrefix, h.PaymentCancelCallbackHandler, h.AnswerCallbackQueryMiddleware, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)

	b.RegisterHandlerMatchFunc(func(update *models.Update) bool {
		if update.Message == nil || update.Message.Text == "" {
			return false
		}
		_, ok := h.TopupAwaitingInput(update.Message.From.ID)
		return ok
	}, h.TopupCustomAmountTextHandler, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)

	// Registered last among text-message handlers: first-match-wins dispatch means this only ever
	// sees a customer's free-text message once every more specific awaiting-input flow above (e.g.
	// the custom top-up catcher just above) has already had first refusal.
	b.RegisterHandlerMatchFunc(handler.IsCandidateAdminReply, h.AdminReplyMessageHandler, h.SuspiciousUserFilterMiddleware, h.CreateCustomerIfNotExistMiddleware)

	mux := http.NewServeMux()
	mux.Handle("/healthcheck", fullHealthHandler(pool, remnawaveClient))
	if tributeClient != nil {
		mux.Handle(config.GetTributeWebHookUrl(), tributeClient.WebHookHandler())
		webhookRetryCron := setupWebhookRetryCron(tributeClient)
		webhookRetryCron.Start()
		defer webhookRetryCron.Stop()
	}

	if rollypayWebhookClient != nil {
		mux.Handle(config.GetRollyPayWebHookUrl(), rollypayWebhookClient.WebHookHandler())
		rollypayRetryCron := setupRollyPayRetryCron(rollypayWebhookClient)
		rollypayRetryCron.Start()
		defer rollypayRetryCron.Stop()
	}

	if config.IsAdminWebAppEnabled() {
		webappHandler := webapp.NewHandler(
			customerRepository, purchaseRepository, referralRepository, auditLogRepository,
			webhookInboxRepository, activityRepository, notificationLogRepository, adminMessageRepository, botRuntimeSettingsRepository, remnawaveClient, tributeClient, rollypayWebhookClient, opsService,
			subService, trafficSvc, pool,
			webapp.BuildInfo{Version: Version, Commit: Commit, BuildDate: BuildDate},
		)
		mux.Handle("/admin/", webappHandler)
		slog.Info("Admin webapp enabled", "url", config.AdminWebAppURL())
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

	registerAdminCommands(ctx, b)

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

// registerAdminCommands publishes the admin's native "/" command menu, scoped to the admin's own
// chat only (regular customers never see these). Almost everything that used to be a /admin_*
// command now lives in the web app opened via /admin; /sync remains as the one bot-native backup
// action that still makes sense to trigger without opening the panel.
//
// DeleteMyCommands is called first and is required, not decorative: Telegram caches the
// previously-registered command list client-side, and SetMyCommands alone does not clear entries
// that are no longer present in the new list — this is a documented limitation of the Bot API
// scoping model, not a bug in this implementation. Without the delete, admins who used the bot
// before this change would keep seeing the old ~16-command menu indefinitely.
func registerAdminCommands(ctx context.Context, b *bot.Bot) {
	scope := &models.BotCommandScopeChat{ChatID: config.GetAdminTelegramId()}

	if ok, err := b.DeleteMyCommands(ctx, &bot.DeleteMyCommandsParams{Scope: scope}); err != nil || !ok {
		slog.Warn("failed to clear stale admin command list", "error", err)
	}

	commands := []models.BotCommand{
		{Command: "start", Description: "Начать работу с ботом"},
		{Command: "admin", Description: "Открыть панель администратора"},
		{Command: "sync", Description: "Синхронизация пользователей с Remnawave"},
	}

	ok, err := b.SetMyCommands(ctx, &bot.SetMyCommandsParams{
		Commands: commands,
		Scope:    scope,
	})
	if err != nil || !ok {
		slog.Error("failed to register admin command list", "error", err)
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

func deviceAddonRenewalChecker(svc *notification.DeviceAddonRenewalService) *cron.Cron {
	c := cron.New()

	_, err := c.AddFunc("0 */4 * * *", func() {
		if err := svc.ProcessRenewalReminders(); err != nil {
			slog.Error("Error sending device addon renewal reminders", "error", err)
		}
	})

	if err != nil {
		panic(err)
	}
	return c
}

func deviceAddonGraceChecker(svc *notification.DeviceAddonRenewalService) *cron.Cron {
	c := cron.New()

	_, err := c.AddFunc("0 */4 * * *", func() {
		if err := svc.ProcessGraceAndExpiry(); err != nil {
			slog.Error("Error processing device addon grace/expiry", "error", err)
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

func setupTopupCleanup(topupRepository *database.TrafficTopupRepository, deviceTopupRepository *database.DeviceTopupRepository) *cron.Cron {
	c := cron.New()
	_, err := c.AddFunc("0 * * * *", func() { // every hour
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		n, err := topupRepository.ExpireOldPending(ctx, 2*time.Hour)
		if err != nil {
			slog.Error("topup cleanup: expire old pending", "error", err)
		} else if n > 0 {
			slog.Info("topup cleanup: expired stale pending records", "count", n)
		}
		dn, err := deviceTopupRepository.ExpireOldPending(ctx, 2*time.Hour)
		if err != nil {
			slog.Error("device topup cleanup: expire old pending", "error", err)
		} else if dn > 0 {
			slog.Info("device topup cleanup: expired stale pending records", "count", dn)
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

func setupRollyPayRetryCron(rollypayClient *rollypay.WebhookClient) *cron.Cron {
	c := cron.New()
	_, err := c.AddFunc("*/10 * * * *", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rollypayClient.RetryFailed(ctx)
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
