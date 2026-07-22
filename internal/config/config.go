package config

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

// GBTopupTier is one bot-owned, bot-priced GB top-up preset sold through RollyPay — the bot itself
// knows the price (RollyPay has no product catalog like Tribute's Digital Products), needed
// because the bot builds the payment amount itself.
type GBTopupTier struct {
	GBAmount int
	PriceRUB int
}

type config struct {
	telegramToken                                             string
	price1, price3, price6, price12                           int
	remnawaveUrl, remnawaveToken, remnawaveMode, remnawaveTag string
	defaultLanguage                                           string
	englishEnabled                                            bool
	databaseURL                                               string
	botURL                                                    string
	trafficLimit, trialTrafficLimit                           int
	feedbackURL                                               string
	channelURL                                                string
	serverStatusURL                                           string
	supportURL                                                string
	tosURL                                                    string
	proxyURL                                                  string
	adminTelegramId                                           int64
	trialDays                                                 int
	trialRemnawaveTag                                         string
	squadUUIDs                                                map[uuid.UUID]uuid.UUID
	referralDays                                              int
	miniApp                                                   string
	enableAutoPayment                                         bool
	healthCheckPort                                           int
	tributeWebhookUrl, tributeAPIKey, tributePaymentUrl       string
	rollypayEnabled                                           bool
	rollypayAPIKey, rollypaySigningSecret, rollypayTerminalID string
	rollypayWebhookUrl                                        string
	rollypaySuccessRedirectUrl, rollypayFailRedirectUrl       string
	rollypayTestMode                                          bool
	isWebAppLinkEnabled                                       bool
	daysInMonth                                               int
	externalSquadUUID                                         uuid.UUID
	blockedTelegramIds                                        map[int64]bool
	whitelistedTelegramIds                                    map[int64]bool
	trialInternalSquads                                       map[uuid.UUID]uuid.UUID
	trialExternalSquadUUID                                    uuid.UUID
	remnawaveHeaders                                          map[string]string
	trialTrafficLimitResetStrategy                            string
	trafficLimitResetStrategy                                 string
	topupEnabled                                              bool
	statusEnabled                                             bool
	gbTopupTiers                                              []GBTopupTier
	gbTopupCustomPricePerGB                                   int
	gbTopupCustomMinGB, gbTopupCustomMaxGB                    int
	deviceSlotPriceRUB                                        int
	adminWebAppEnabled                                        bool
	adminWebAppJWTSecret                                      string
	adminWebAppURL                                            string
	adminSessionTTLMinutes                                    int
	adminWebAppInitDataMaxAgeHours                            int
}

var conf config

func RemnawaveTag() string {
	return conf.remnawaveTag
}

func TrialRemnawaveTag() string {
	if conf.trialRemnawaveTag != "" {
		return conf.trialRemnawaveTag
	}
	return conf.remnawaveTag
}

func DefaultLanguage() string {
	return conf.defaultLanguage
}

// EnglishEnabled gates whether the "en" translation file gets loaded at all — while false,
// customers with an English Telegram client fall back to DefaultLanguage instead of seeing
// (possibly stale) English text. Off by default until a proper in-bot language switcher exists.
func EnglishEnabled() bool {
	return conf.englishEnabled
}
func GetTributeWebHookUrl() string {
	return conf.tributeWebhookUrl
}
func GetTributeAPIKey() string {
	return conf.tributeAPIKey
}

func GetTributePaymentUrl() string {
	return conf.tributePaymentUrl
}

func IsRollyPayEnabled() bool {
	return conf.rollypayEnabled
}

func RollyPayAPIKey() string {
	return conf.rollypayAPIKey
}

func RollyPaySigningSecret() string {
	return conf.rollypaySigningSecret
}

func RollyPayTerminalID() string {
	return conf.rollypayTerminalID
}

func GetRollyPayWebHookUrl() string {
	return conf.rollypayWebhookUrl
}

func RollyPaySuccessRedirectUrl() string {
	return conf.rollypaySuccessRedirectUrl
}

func RollyPayFailRedirectUrl() string {
	return conf.rollypayFailRedirectUrl
}

func RollyPayTestMode() bool {
	return conf.rollypayTestMode
}

func GetReferralDays() int {
	return conf.referralDays
}

func GetMiniAppURL() string {
	return conf.miniApp
}

func SquadUUIDs() map[uuid.UUID]uuid.UUID {
	return conf.squadUUIDs
}

func GetBlockedTelegramIds() map[int64]bool {
	return conf.blockedTelegramIds
}

func GetWhitelistedTelegramIds() map[int64]bool {
	return conf.whitelistedTelegramIds
}

func TrialInternalSquads() map[uuid.UUID]uuid.UUID {
	if conf.trialInternalSquads != nil && len(conf.trialInternalSquads) > 0 {
		return conf.trialInternalSquads
	}
	return conf.squadUUIDs
}

func TrialExternalSquadUUID() uuid.UUID {
	if conf.trialExternalSquadUUID != uuid.Nil {
		return conf.trialExternalSquadUUID
	}
	return conf.externalSquadUUID
}

func TrialTrafficLimit() int {
	return conf.trialTrafficLimit * bytesInGigabyte
}

func TrialDays() int {
	return conf.trialDays
}
func FeedbackURL() string {
	return conf.feedbackURL
}

func ChannelURL() string {
	return conf.channelURL
}

func ServerStatusURL() string {
	return conf.serverStatusURL
}

func SupportURL() string {
	return conf.supportURL
}

func TosURL() string {
	return conf.tosURL
}

func ProxyURL() string {
	return conf.proxyURL
}

func Price1() int {
	return conf.price1
}

func Price3() int {
	return conf.price3
}

func Price6() int {
	return conf.price6
}

func Price12() int {
	return conf.price12
}

func DaysInMonth() int {
	return conf.daysInMonth
}

func ExternalSquadUUID() uuid.UUID {
	return conf.externalSquadUUID
}

func Price(month int) int {
	switch month {
	case 1:
		return conf.price1
	case 3:
		return conf.price3
	case 6:
		return conf.price6
	case 12:
		return conf.price12
	default:
		return conf.price1
	}
}

func TelegramToken() string {
	return conf.telegramToken
}

func TelegramProxyURL() string {
	return envStringDefault("TELEGRAM_PROXY_URL", "")
}

func RemnawaveUrl() string {
	return conf.remnawaveUrl
}
func DadaBaseUrl() string {
	return conf.databaseURL
}
func RemnawaveToken() string {
	return conf.remnawaveToken
}
func RemnawaveMode() string {
	return conf.remnawaveMode
}
func BotURL() string {
	return conf.botURL
}
func SetBotURL(botURL string) {
	conf.botURL = botURL
}
func TrafficLimit() int {
	return conf.trafficLimit * bytesInGigabyte
}

func GetAdminTelegramId() int64 {
	return conf.adminTelegramId
}

func GetHealthCheckPort() int {
	return conf.healthCheckPort
}

func IsWepAppLinkEnabled() bool {
	return conf.isWebAppLinkEnabled
}

func RemnawaveHeaders() map[string]string {
	return conf.remnawaveHeaders
}

func TrialTrafficLimitResetStrategy() string {
	return conf.trialTrafficLimitResetStrategy
}

func TrafficLimitResetStrategy() string {
	return conf.trafficLimitResetStrategy
}

func TopupEnabled() bool {
	return conf.topupEnabled
}

func StatusEnabled() bool {
	return conf.statusEnabled
}

// GBTopupTiers returns the configured bot-owned, RollyPay-priced GB top-up presets.
func GBTopupTiers() []GBTopupTier {
	return conf.gbTopupTiers
}

func GBTopupTierByGB(gb int) *GBTopupTier {
	for _, tier := range conf.gbTopupTiers {
		if tier.GBAmount == gb {
			return &tier
		}
	}
	return nil
}

// GBTopupCustomPricePerGB is the RUB price per 1 GB for the free-text "custom amount" flow.
func GBTopupCustomPricePerGB() int {
	return conf.gbTopupCustomPricePerGB
}

func GBTopupCustomMinGB() int {
	return conf.gbTopupCustomMinGB
}

func GBTopupCustomMaxGB() int {
	return conf.gbTopupCustomMaxGB
}

func DeviceSlotPriceRUB() int {
	return conf.deviceSlotPriceRUB
}

// DeviceSlotDailyPriceRUB is the per-day rate used to prorate a mid-cycle device slot purchase
// against the days remaining in the customer's current subscription cycle.
func DeviceSlotDailyPriceRUB() float64 {
	return float64(conf.deviceSlotPriceRUB) / float64(conf.daysInMonth)
}

const bytesInGigabyte = 1073741824

func BytesInGigabyte() int { return bytesInGigabyte }

func IsAdminWebAppEnabled() bool {
	return conf.adminWebAppEnabled
}

func AdminWebAppJWTSecret() string {
	return conf.adminWebAppJWTSecret
}

func AdminWebAppURL() string {
	return conf.adminWebAppURL
}

func AdminSessionTTLMinutes() int {
	return conf.adminSessionTTLMinutes
}

func AdminWebAppInitDataMaxAgeHours() int {
	return conf.adminWebAppInitDataMaxAgeHours
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Panicf("env %q not set", key)
	}
	return v
}

func mustEnvInt(key string) int {
	v := mustEnv(key)
	i, err := strconv.Atoi(v)
	if err != nil {
		log.Panicf("invalid int in %q: %v", key, err)
	}
	return i
}

func envIntDefault(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		log.Panicf("invalid int in %q: %v", key, err)
	}
	return i
}

func envStringDefault(key string, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

func envBool(key string) bool {
	return os.Getenv(key) == "true"
}

func InitConfig() {
	if os.Getenv("DISABLE_ENV_FILE") != "true" {
		if err := godotenv.Load(".env"); err != nil {
			log.Println("No .env loaded:", err)
		}
	}
	var err error
	conf.adminTelegramId, err = strconv.ParseInt(os.Getenv("ADMIN_TELEGRAM_ID"), 10, 64)
	if err != nil {
		panic("ADMIN_TELEGRAM_ID .env variable not set")
	}

	conf.telegramToken = mustEnv("TELEGRAM_TOKEN")

	conf.isWebAppLinkEnabled = func() bool {
		isWebAppLinkEnabled := os.Getenv("IS_WEB_APP_LINK") == "true"
		return isWebAppLinkEnabled
	}()

	conf.miniApp = envStringDefault("MINI_APP_URL", "")

	conf.remnawaveTag = envStringDefault("REMNAWAVE_TAG", "")

	conf.trialRemnawaveTag = envStringDefault("TRIAL_REMNAWAVE_TAG", "")

	conf.trialTrafficLimitResetStrategy = strings.ToUpper(envStringDefault("TRIAL_TRAFFIC_LIMIT_RESET_STRATEGY", "MONTH_ROLLING"))
	conf.trafficLimitResetStrategy = strings.ToUpper(envStringDefault("TRAFFIC_LIMIT_RESET_STRATEGY", "MONTH_ROLLING"))

	validStrategies := map[string]bool{
		"DAY": true, "WEEK": true, "MONTH": true, "MONTH_ROLLING": true, "NO_RESET": true,
	}
	if !validStrategies[conf.trafficLimitResetStrategy] {
		panic(fmt.Sprintf("invalid TRAFFIC_LIMIT_RESET_STRATEGY: %q. Allowed: DAY, WEEK, MONTH, MONTH_ROLLING, NO_RESET", conf.trafficLimitResetStrategy))
	}
	if !validStrategies[conf.trialTrafficLimitResetStrategy] {
		panic(fmt.Sprintf("invalid TRIAL_TRAFFIC_LIMIT_RESET_STRATEGY: %q. Allowed: DAY, WEEK, MONTH, MONTH_ROLLING, NO_RESET", conf.trialTrafficLimitResetStrategy))
	}

	conf.defaultLanguage = envStringDefault("DEFAULT_LANGUAGE", "ru")
	conf.englishEnabled = envBool("ENGLISH_ENABLED")

	conf.daysInMonth = envIntDefault("DAYS_IN_MONTH", 30)

	externalSquadUUIDStr := os.Getenv("EXTERNAL_SQUAD_UUID")
	if externalSquadUUIDStr != "" {
		parsedUUID, err := uuid.Parse(externalSquadUUIDStr)
		if err != nil {
			panic(fmt.Sprintf("invalid EXTERNAL_SQUAD_UUID format: %v", err))
		}
		conf.externalSquadUUID = parsedUUID
	} else {
		conf.externalSquadUUID = uuid.Nil
	}

	conf.trialTrafficLimit = mustEnvInt("TRIAL_TRAFFIC_LIMIT")

	conf.healthCheckPort = envIntDefault("HEALTH_CHECK_PORT", 8080)

	conf.trialDays = mustEnvInt("TRIAL_DAYS")

	conf.enableAutoPayment = envBool("ENABLE_AUTO_PAYMENT")

	conf.price1 = mustEnvInt("PRICE_1")
	conf.price3 = mustEnvInt("PRICE_3")
	conf.price6 = mustEnvInt("PRICE_6")
	conf.price12 = mustEnvInt("PRICE_12")

	conf.remnawaveUrl = mustEnv("REMNAWAVE_URL")

	conf.remnawaveMode = func() string {
		v := os.Getenv("REMNAWAVE_MODE")
		if v != "" {
			if v != "remote" && v != "local" {
				panic("REMNAWAVE_MODE .env variable must be either 'remote' or 'local'")
			} else {
				return v
			}
		} else {
			return "remote"
		}
	}()

	conf.remnawaveToken = mustEnv("REMNAWAVE_TOKEN")

	conf.databaseURL = mustEnv("DATABASE_URL")

	conf.trafficLimit = mustEnvInt("TRAFFIC_LIMIT")
	conf.referralDays = mustEnvInt("REFERRAL_DAYS")

	conf.serverStatusURL = os.Getenv("SERVER_STATUS_URL")
	conf.supportURL = os.Getenv("SUPPORT_URL")
	conf.feedbackURL = os.Getenv("FEEDBACK_URL")
	conf.channelURL = os.Getenv("CHANNEL_URL")
	conf.tosURL = os.Getenv("TOS_URL")
	conf.proxyURL = os.Getenv("PROXY_URL")

	conf.squadUUIDs = func() map[uuid.UUID]uuid.UUID {
		v := os.Getenv("SQUAD_UUIDS")
		if v != "" {
			uuids := strings.Split(v, ",")
			var inboundsMap = make(map[uuid.UUID]uuid.UUID)
			for _, value := range uuids {
				uuid, err := uuid.Parse(value)
				if err != nil {
					panic(err)
				}
				inboundsMap[uuid] = uuid
			}
			slog.Info("Loaded squad UUIDs", "uuids", uuids)
			return inboundsMap
		} else {
			slog.Info("No squad UUIDs specified, all will be used")
			return map[uuid.UUID]uuid.UUID{}
		}
	}()

	conf.tributeWebhookUrl = os.Getenv("TRIBUTE_WEBHOOK_URL")
	if conf.tributeWebhookUrl != "" {
		conf.tributeAPIKey = mustEnv("TRIBUTE_API_KEY")
		conf.tributePaymentUrl = mustEnv("TRIBUTE_PAYMENT_URL")
	}

	conf.rollypayEnabled = envBool("ROLLYPAY_ENABLED")
	if conf.rollypayEnabled {
		conf.rollypayAPIKey = mustEnv("ROLLYPAY_API_KEY")
		conf.rollypaySigningSecret = mustEnv("ROLLYPAY_SIGNING_SECRET")
		conf.rollypayTerminalID = mustEnv("ROLLYPAY_TERMINAL_ID")
		conf.rollypayWebhookUrl = mustEnv("ROLLYPAY_WEBHOOK_URL")
		conf.rollypaySuccessRedirectUrl = envStringDefault("ROLLYPAY_SUCCESS_REDIRECT_URL", "")
		conf.rollypayFailRedirectUrl = envStringDefault("ROLLYPAY_FAIL_REDIRECT_URL", "")
		conf.rollypayTestMode = envBool("ROLLYPAY_TEST_MODE")

		conf.gbTopupTiers = parseGBTopupTiers(mustEnv("GB_TOPUP_TIERS"))
		conf.gbTopupCustomPricePerGB = mustEnvInt("GB_TOPUP_CUSTOM_PRICE_PER_GB")
		conf.gbTopupCustomMinGB = envIntDefault("GB_TOPUP_CUSTOM_MIN_GB", 1)
		conf.gbTopupCustomMaxGB = envIntDefault("GB_TOPUP_CUSTOM_MAX_GB", 500)
		if conf.gbTopupCustomMinGB <= 0 || conf.gbTopupCustomMaxGB < conf.gbTopupCustomMinGB {
			panic(fmt.Sprintf("invalid GB_TOPUP_CUSTOM_MIN_GB/MAX_GB: %d/%d", conf.gbTopupCustomMinGB, conf.gbTopupCustomMaxGB))
		}

		conf.deviceSlotPriceRUB = mustEnvInt("DEVICE_SLOT_PRICE_RUB")
	}

	conf.blockedTelegramIds = func() map[int64]bool {
		v := os.Getenv("BLOCKED_TELEGRAM_IDS")
		if v != "" {
			ids := strings.Split(v, ",")
			var blockedMap = make(map[int64]bool)
			for _, idStr := range ids {
				id, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)
				if err != nil {
					panic(fmt.Sprintf("invalid telegram ID in BLOCKED_TELEGRAM_IDS: %v", err))
				}
				blockedMap[id] = true
			}
			slog.Info("Loaded blocked telegram IDs", "count", len(blockedMap))
			return blockedMap
		} else {
			slog.Info("No blocked telegram IDs specified")
			return map[int64]bool{}
		}
	}()

	conf.whitelistedTelegramIds = func() map[int64]bool {
		v := os.Getenv("WHITELISTED_TELEGRAM_IDS")
		if v != "" {
			ids := strings.Split(v, ",")
			var whitelistedMap = make(map[int64]bool)
			for _, idStr := range ids {
				id, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)
				if err != nil {
					panic(fmt.Sprintf("invalid telegram ID in WHITELISTED_TELEGRAM_IDS: %v", err))
				}
				whitelistedMap[id] = true
			}
			slog.Info("Loaded whitelisted telegram IDs", "count", len(whitelistedMap))
			return whitelistedMap
		} else {
			slog.Info("No whitelisted telegram IDs specified")
			return map[int64]bool{}
		}
	}()

	conf.trialInternalSquads = func() map[uuid.UUID]uuid.UUID {
		v := os.Getenv("TRIAL_INTERNAL_SQUADS")
		if v != "" {
			uuids := strings.Split(v, ",")
			var trialSquadsMap = make(map[uuid.UUID]uuid.UUID)
			for _, value := range uuids {
				parsedUUID, err := uuid.Parse(strings.TrimSpace(value))
				if err != nil {
					panic(fmt.Sprintf("invalid UUID in TRIAL_INTERNAL_SQUADS: %v", err))
				}
				trialSquadsMap[parsedUUID] = parsedUUID
			}
			slog.Info("Loaded trial internal squad UUIDs", "uuids", uuids)
			return trialSquadsMap
		} else {
			slog.Info("No trial internal squads specified, will use regular SQUAD_UUIDS for trial users")
			return map[uuid.UUID]uuid.UUID{}
		}
	}()

	trialExternalSquadUUIDStr := os.Getenv("TRIAL_EXTERNAL_SQUAD_UUID")
	if trialExternalSquadUUIDStr != "" {
		parsedUUID, err := uuid.Parse(trialExternalSquadUUIDStr)
		if err != nil {
			panic(fmt.Sprintf("invalid TRIAL_EXTERNAL_SQUAD_UUID format: %v", err))
		}
		conf.trialExternalSquadUUID = parsedUUID
		slog.Info("Loaded trial external squad UUID", "uuid", trialExternalSquadUUIDStr)
	} else {
		conf.trialExternalSquadUUID = uuid.Nil
		slog.Info("No trial external squad specified, will use regular EXTERNAL_SQUAD_UUID for trial users")
	}

	conf.remnawaveHeaders = func() map[string]string {
		v := os.Getenv("REMNAWAVE_HEADERS")
		if v != "" {
			headers := make(map[string]string)
			pairs := strings.Split(v, ";")
			for _, pair := range pairs {
				parts := strings.SplitN(strings.TrimSpace(pair), ":", 2)
				if len(parts) == 2 {
					key := strings.TrimSpace(parts[0])
					value := strings.TrimSpace(parts[1])
					if key != "" && value != "" {
						headers[key] = value
					}
				}
			}
			if len(headers) > 0 {
				slog.Info("Loaded remnawave headers", "count", len(headers))
				return headers
			}
		}
		return map[string]string{}
	}()

	conf.topupEnabled = envBool("TOPUP_ENABLED")
	conf.statusEnabled = envBool("STATUS_ENABLED")

	conf.adminWebAppEnabled = envBool("ADMIN_WEBAPP_ENABLED")
	if conf.adminWebAppEnabled {
		conf.adminWebAppJWTSecret = mustEnv("ADMIN_WEBAPP_JWT_SECRET")
		conf.adminWebAppURL = envStringDefault("ADMIN_WEBAPP_URL", "")
		conf.adminSessionTTLMinutes = envIntDefault("ADMIN_SESSION_TTL_MINUTES", 1440)
		conf.adminWebAppInitDataMaxAgeHours = envIntDefault("ADMIN_WEBAPP_INITDATA_MAX_AGE_HOURS", 24)
	}
}

// parseGBTopupTiers parses "gb:price_rub" pairs separated by commas, e.g. "10:150,25:300,50:500" —
// the same comma-delimited convention SQUAD_UUIDS/BLOCKED_TELEGRAM_IDS already use in this file.
func parseGBTopupTiers(v string) []GBTopupTier {
	parts := strings.Split(v, ",")
	tiers := make([]GBTopupTier, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, ":", 2)
		if len(kv) != 2 {
			panic(fmt.Sprintf("invalid GB_TOPUP_TIERS entry %q, expected gb:price_rub", part))
		}
		gb, err := strconv.Atoi(strings.TrimSpace(kv[0]))
		if err != nil {
			panic(fmt.Sprintf("invalid GB amount in GB_TOPUP_TIERS entry %q: %v", part, err))
		}
		price, err := strconv.Atoi(strings.TrimSpace(kv[1]))
		if err != nil {
			panic(fmt.Sprintf("invalid price in GB_TOPUP_TIERS entry %q: %v", part, err))
		}
		tiers = append(tiers, GBTopupTier{GBAmount: gb, PriceRUB: price})
	}
	if len(tiers) == 0 {
		panic("GB_TOPUP_TIERS is set but contains no valid entries")
	}
	return tiers
}
