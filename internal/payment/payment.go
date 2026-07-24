package payment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"remnawave-tg-shop-bot/internal/cache"
	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/remnawave"
	"remnawave-tg-shop-bot/internal/rollypay"
	"remnawave-tg-shop-bot/internal/translation"
	"remnawave-tg-shop-bot/utils"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/google/uuid"
)

type PaymentService struct {
	purchaseRepository    *database.PurchaseRepository
	remnawaveClient       *remnawave.Client
	customerRepository    *database.CustomerRepository
	telegramBot           *bot.Bot
	translation           *translation.Manager
	rollypayClient        *rollypay.Client
	referralRepository    *database.ReferralRepository
	cache                 *cache.Cache
	topupRepository       *database.TrafficTopupRepository
	deviceAddonRepository *database.DeviceAddonRepository
}

func NewPaymentService(
	translation *translation.Manager,
	purchaseRepository *database.PurchaseRepository,
	remnawaveClient *remnawave.Client,
	customerRepository *database.CustomerRepository,
	telegramBot *bot.Bot,
	rollypayClient *rollypay.Client,
	referralRepository *database.ReferralRepository,
	cache *cache.Cache,
	topupRepository *database.TrafficTopupRepository,
	deviceAddonRepository *database.DeviceAddonRepository,
) *PaymentService {
	return &PaymentService{
		purchaseRepository:    purchaseRepository,
		remnawaveClient:       remnawaveClient,
		customerRepository:    customerRepository,
		telegramBot:           telegramBot,
		translation:           translation,
		rollypayClient:        rollypayClient,
		referralRepository:    referralRepository,
		cache:                 cache,
		topupRepository:       topupRepository,
		deviceAddonRepository: deviceAddonRepository,
	}
}

func (s PaymentService) ProcessPurchaseById(ctx context.Context, purchaseId int64) error {
	purchase, err := s.purchaseRepository.FindById(ctx, purchaseId)
	if err != nil {
		return err
	}
	if purchase == nil {
		return fmt.Errorf("purchase with crypto invoice id %s not found", utils.MaskHalfInt64(purchaseId))
	}

	customer, err := s.customerRepository.FindById(ctx, purchase.CustomerID)
	if err != nil {
		return err
	}
	if customer == nil {
		return fmt.Errorf("customer %s not found", utils.MaskHalfInt64(purchase.CustomerID))
	}

	if messageId, b := s.cache.Get(purchase.ID); b {
		_, err = s.telegramBot.DeleteMessage(ctx, &bot.DeleteMessageParams{
			ChatID:    customer.TelegramID,
			MessageID: messageId,
		})
		if err != nil {
			slog.Error("Error deleting message", "error", err)
		}
	}

	// needsTrafficReset decides whether this purchase should zero the usage counter. Resetting on
	// every purchase (the old behavior) double-dips with Remnawave's own periodic reset
	// (TRAFFIC_LIMIT_RESET_STRATEGY): a customer renewing a few days early would get a bonus reset
	// on top of the panel's own scheduled one. Reset only when it's actually needed: the
	// subscription had lapsed (a genuine new-period start), or the account is currently LIMITED
	// (blocked for exceeding its cap) despite having days left — otherwise the purchase would grant
	// nothing and the customer stays stuck until the panel's own next scheduled reset.
	needsTrafficReset := customer.ExpireAt == nil || !customer.ExpireAt.After(time.Now())
	if !needsTrafficReset {
		needsTrafficReset = s.isTrafficLimited(ctx, customer.TelegramID)
	}

	// calculateRollover reads the user's current usage from Remnawave before CreateOrUpdateUser
	// resets the counter, which is the correct moment to snapshot "bytes consumed this period".
	// On payment retry, the new rollover record created below will have a fresher completed_at
	// than any prior record, so FindLatestCompletedByTelegramID will prefer it — preventing
	// double-counting across retries.
	rolloverBytes := s.calculateRollover(ctx, customer.TelegramID)
	// Keep totalTrafficLimit as int64 until the API call that requires int.
	totalTrafficLimitBytes := int64(config.TrafficLimit()) + rolloverBytes

	user, err := s.remnawaveClient.CreateOrUpdateUser(ctx, customer.ID, customer.TelegramID, int(totalTrafficLimitBytes), purchase.Month*config.DaysInMonth(), false)
	if err != nil {
		return err
	}

	// Reset the traffic counter so the new period starts from 0. Rollover was snapshotted above
	// before this call, so the ordering is correct. Skipped for an early renewal that isn't
	// LIMITED — see needsTrafficReset above.
	if needsTrafficReset {
		if resetErr := s.remnawaveClient.ResetUserTraffic(ctx, user.UUID); resetErr != nil {
			slog.Error("renewal: failed to reset user traffic", "uuid", user.UUID, "telegram_id", customer.TelegramID, "error", resetErr)
		}
	}

	if rolloverBytes > 0 {
		now := time.Now()
		target := totalTrafficLimitBytes
		rolloverGB := int(rolloverBytes / int64(config.BytesInGigabyte()))
		if _, dbErr := s.topupRepository.Create(ctx, &database.TrafficTopup{
			TelegramID:              customer.TelegramID,
			RemnawaveUUID:           user.UUID.String(),
			GBAmount:                rolloverGB,
			Status:                  database.TopupStatusCompleted,
			TargetTrafficLimitBytes: &target,
			CompletedAt:             &now,
		}); dbErr != nil {
			slog.Error("rollover: create topup record", "telegram_id", customer.TelegramID, "error", dbErr)
		}
		slog.Info("rollover: carried unused topup to new period",
			"telegram_id", customer.TelegramID,
			"rollover_bytes", rolloverBytes,
			"new_limit_bytes", totalTrafficLimitBytes,
		)
	}

	err = s.purchaseRepository.MarkAsPaid(ctx, purchase.ID)
	if err != nil {
		return err
	}

	customerFilesToUpdate := map[string]interface{}{
		"subscription_link": user.SubscriptionUrl,
		"expire_at":         user.ExpireAt,
		"is_trial":          false,
	}

	err = s.customerRepository.UpdateFields(ctx, customer.ID, customerFilesToUpdate)
	if err != nil {
		return err
	}

	// Bundled device addons ride the subscription's own cycle (decision 2) — once this renewal
	// actually lands, push the addon's cycle_expires_at out to match so it doesn't fall out of
	// sync with what was just paid for. Standalone (Tribute) addons are never bundled here, so
	// they're untouched — their own renewal is handled by the addon-renewal webhook path instead.
	if purchase.InvoiceType == database.InvoiceTypeRollyPay && s.deviceAddonRepository != nil {
		addon, addonErr := s.deviceAddonRepository.FindActiveByTelegramID(ctx, customer.TelegramID)
		if addonErr != nil {
			slog.Error("purchase processed: find device addon for cycle sync failed", "telegram_id", customer.TelegramID, "error", addonErr)
		} else if addon != nil && addon.Status != database.AddonStatusExpired && addon.BillingMode == database.AddonBillingModeBundled {
			if addon.PendingDeviceCount != nil {
				applyPendingDeviceDecrease(ctx, s.remnawaveClient, s.deviceAddonRepository, addon, user.UUID, user.HwidDeviceLimit)
			}
			if syncErr := s.deviceAddonRepository.ExtendCycle(ctx, addon.ID, user.ExpireAt); syncErr != nil {
				slog.Error("purchase processed: sync device addon cycle failed", "addon_id", addon.ID, "error", syncErr)
			}
		}
	}

	activationTextKey := "subscription_activated"
	if !customer.IsTrial && customer.SubscriptionLink != nil {
		activationTextKey = "subscription_renewed"
	}
	_, err = s.telegramBot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: customer.TelegramID,
		Text:   s.translation.GetText(customer.Language, activationTextKey),
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: s.createConnectKeyboard(customer),
		},
	})
	if err != nil {
		return err
	}

	ctxReferee := context.Background()
	referee, err := s.referralRepository.FindByReferee(ctxReferee, customer.TelegramID)
	if referee == nil {
		return nil
	}
	if referee.BonusGranted {
		return nil
	}
	if err != nil {
		return err
	}
	refereeCustomer, err := s.customerRepository.FindByTelegramId(ctxReferee, referee.ReferrerID)
	if err != nil {
		return err
	}
	refereeUser, err := s.remnawaveClient.CreateOrUpdateUser(ctxReferee, refereeCustomer.ID, refereeCustomer.TelegramID, config.TrafficLimit(), config.GetReferralDays(), false)
	if err != nil {
		return err
	}
	refereeUserFilesToUpdate := map[string]interface{}{
		"subscription_link": refereeUser.SubscriptionUrl,
		"expire_at":         refereeUser.ExpireAt,
	}
	err = s.customerRepository.UpdateFields(ctxReferee, refereeCustomer.ID, refereeUserFilesToUpdate)
	if err != nil {
		return err
	}
	err = s.referralRepository.MarkBonusGranted(ctxReferee, referee.ID)
	if err != nil {
		return err
	}
	slog.Info("Granted referral bonus", "customer_id", utils.MaskHalfInt64(refereeCustomer.ID))
	_, err = s.telegramBot.SendMessage(ctxReferee, &bot.SendMessageParams{
		ChatID:    refereeCustomer.TelegramID,
		ParseMode: models.ParseModeHTML,
		Text:      s.translation.GetText(refereeCustomer.Language, "referral_bonus_granted"),
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: s.createConnectKeyboard(refereeCustomer),
		},
	})

	slog.Info("purchase processed", "purchase_id", utils.MaskHalfInt64(purchase.ID), "type", purchase.InvoiceType, "customer_id", utils.MaskHalfInt64(customer.ID))

	return nil
}

func (s PaymentService) createConnectKeyboard(customer *database.Customer) [][]models.InlineKeyboardButton {
	var inlineCustomerKeyboard [][]models.InlineKeyboardButton

	bd := s.translation.GetButton(customer.Language, "connect_button")
	if config.GetMiniAppURL() != "" {
		inlineCustomerKeyboard = append(inlineCustomerKeyboard, []models.InlineKeyboardButton{bd.InlineWebApp(config.GetMiniAppURL())})
	} else {
		inlineCustomerKeyboard = append(inlineCustomerKeyboard, []models.InlineKeyboardButton{bd.InlineCallback("connect")})
	}

	inlineCustomerKeyboard = append(inlineCustomerKeyboard, []models.InlineKeyboardButton{
		s.translation.GetButton(customer.Language, "back_button").InlineCallback("start"),
	})
	return inlineCustomerKeyboard
}

// CreatePurchase's deviceSlotCount return is the number of device slots folded into this specific
// invoice's charge (0 if none) — lets callers tell the customer exactly how many slots the
// "renewal also covers a device addon" surcharge is for, instead of just a bare RUB amount.
func (s PaymentService) CreatePurchase(ctx context.Context, amount float64, months int, customer *database.Customer, invoiceType database.InvoiceType) (url string, purchaseId int64, chargedAmount float64, deviceSlotCount int, err error) {
	switch invoiceType {
	case database.InvoiceTypeTribute:
		return s.createTributeInvoice(ctx, amount, months, customer)
	case database.InvoiceTypeRollyPay:
		return s.createRollyPayInvoice(ctx, amount, months, customer)
	default:
		return "", 0, 0, 0, fmt.Errorf("unknown invoice type: %s", invoiceType)
	}
}

// ProrateDeviceCost returns the RUB cost of adding deviceCount device slots for the days remaining
// in the customer's current subscription cycle, using customer.ExpireAt as the cycle end. Only
// correct for bundled-billing customers, whose device addon rides the subscription's own cycle —
// for standalone (Tribute-linked) customers the addon has its own independent cycle instead, so
// mid-cycle additions must prorate against that via ProrateDeviceCostForCycle, not this.
func ProrateDeviceCost(customer *database.Customer, deviceCount int) (amount float64, days int) {
	return prorateDeviceCost(customer.ExpireAt, deviceCount, config.DeviceSlotDailyPriceRUB())
}

// ProrateDeviceCostForCycle prorates deviceCount slots against an arbitrary cycle end — used for a
// standalone device addon's own CycleExpiresAt when a customer buys another slot mid-cycle,
// instead of against their (unrelated, often much longer) subscription cycle.
func ProrateDeviceCostForCycle(cycleExpiresAt time.Time, deviceCount int) (amount float64, days int) {
	return prorateDeviceCost(&cycleExpiresAt, deviceCount, config.DeviceSlotDailyPriceRUB())
}

func prorateDeviceCost(expireAt *time.Time, deviceCount int, dailyPriceRUB float64) (amount float64, days int) {
	if expireAt == nil {
		return 0, 0
	}
	remainingHours := time.Until(*expireAt).Hours()
	if remainingHours <= 0 {
		return 0, 0
	}
	days = int(math.Ceil(remainingHours / 24))
	amount = dailyPriceRUB * float64(deviceCount) * float64(days)
	return amount, days
}

// DetermineDeviceAddonBillingMode decides whether a customer's device addon should be billed
// standalone (Tribute-linked customers, whose charge amount cannot vary) or bundled into their
// next RollyPay renewal invoice (everyone else).
func (s PaymentService) DetermineDeviceAddonBillingMode(ctx context.Context, customer *database.Customer) (database.AddonBillingMode, error) {
	tributes, err := s.purchaseRepository.FindLatestActiveTributesByCustomerIDs(ctx, []int64{customer.ID})
	if err != nil {
		return "", fmt.Errorf("determine device addon billing mode: %w", err)
	}
	return database.DetermineAddonBillingMode(*tributes), nil
}

var ErrCustomerNotFound = errors.New("customer not found")

func (s PaymentService) CancelTributePurchase(ctx context.Context, telegramId int64) error {
	slog.Info("Canceling tribute purchase", "telegram_id", utils.MaskHalfInt64(telegramId))
	customer, err := s.customerRepository.FindByTelegramId(ctx, telegramId)
	if err != nil {
		return err
	}
	if customer == nil {
		return ErrCustomerNotFound
	}
	tributePurchase, err := s.purchaseRepository.FindByCustomerIDAndInvoiceTypeLast(ctx, customer.ID, database.InvoiceTypeTribute)
	if err != nil {
		return err
	}
	if tributePurchase == nil {
		return errors.New("tribute purchase not found")
	}
	expireAt, err := s.remnawaveClient.DecreaseSubscription(ctx, telegramId, config.TrafficLimit(), -tributePurchase.Month*config.DaysInMonth())
	if err != nil {
		return err
	}

	if err := s.customerRepository.UpdateFields(ctx, customer.ID, map[string]interface{}{
		"expire_at": expireAt,
	}); err != nil {
		return err
	}

	if err := s.purchaseRepository.UpdateFields(ctx, tributePurchase.ID, map[string]interface{}{
		"status": database.PurchaseStatusCancel,
	}); err != nil {
		return err
	}
	_, err = s.telegramBot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    telegramId,
		ParseMode: models.ParseModeHTML,
		Text:      s.translation.GetText(customer.Language, "tribute_cancelled"),
	})
	if err != nil {
		slog.Error("Error sending message about tribute cancelled", "error", err, "telegram_id", utils.MaskHalfInt64(telegramId))
	}
	slog.Info("Canceled tribute purchase", "purchase_id", utils.MaskHalfInt64(tributePurchase.ID), "telegram_id", utils.MaskHalfInt64(telegramId))
	return nil
}

func (s PaymentService) createRollyPayInvoice(ctx context.Context, amount float64, months int, customer *database.Customer) (url string, purchaseId int64, chargedAmount float64, deviceSlotCount int, err error) {
	// Bundled device addons ride the subscription renewal invoice as a single payment (decision 2
	// of the device-addon plan) rather than a separate charge — Tribute-linked customers never
	// reach here with a bundled addon, since DetermineDeviceAddonBillingMode forces them standalone.
	if s.deviceAddonRepository != nil {
		addon, addonErr := s.deviceAddonRepository.FindActiveByTelegramID(ctx, customer.TelegramID)
		if addonErr != nil {
			slog.Error("createRollyPayInvoice: find device addon failed", "telegram_id", customer.TelegramID, "error", addonErr)
		} else if addon != nil && addon.Status != database.AddonStatusExpired && addon.BillingMode == database.AddonBillingModeBundled {
			// Charge for the count that will actually be in effect once this renewal lands (see
			// applyPendingDeviceDecrease), not the stale pre-decrease count — otherwise the customer
			// pays for a slot this invoice is about to remove.
			effectiveCount := addon.DeviceCount
			if addon.PendingDeviceCount != nil {
				effectiveCount = *addon.PendingDeviceCount
			}
			amount += float64(effectiveCount) * float64(config.DeviceSlotPriceRUB())
			deviceSlotCount = effectiveCount
		}
	}

	purchaseId, err = s.purchaseRepository.Create(ctx, &database.Purchase{
		InvoiceType: database.InvoiceTypeRollyPay,
		Status:      database.PurchaseStatusNew,
		Amount:      amount,
		Currency:    "RUB",
		CustomerID:  customer.ID,
		Month:       months,
		IsTest:      config.RollyPayTestMode() && customer.TelegramID == config.GetAdminTelegramId(),
	})
	if err != nil {
		slog.Error("Error creating purchase", "error", err)
		return "", 0, 0, 0, err
	}

	paymentResp, err := s.rollypayClient.CreatePayment(ctx, rollypay.CreatePaymentRequest{
		Amount:      fmt.Sprintf("%.2f", amount),
		OrderID:     fmt.Sprintf("sub-%d", purchaseId),
		Description: fmt.Sprintf("Subscription %d month(s)", months),
		Test:        config.RollyPayTestMode() && customer.TelegramID == config.GetAdminTelegramId(),
	})
	if err != nil {
		slog.Error("Error creating rollypay payment", "error", err)
		if cancelErr := s.purchaseRepository.UpdateFields(ctx, purchaseId, map[string]interface{}{"status": database.PurchaseStatusCancel}); cancelErr != nil {
			slog.Error("Error cancelling orphaned purchase", "error", cancelErr, "purchase_id", purchaseId)
		}
		return "", 0, 0, 0, err
	}

	updates := map[string]interface{}{
		"provider_payment_id": paymentResp.PaymentID,
		"provider_pay_url":    paymentResp.PayURL,
		"status":              database.PurchaseStatusPending,
	}

	err = s.purchaseRepository.UpdateFields(ctx, purchaseId, updates)
	if err != nil {
		slog.Error("Error updating purchase", "error", err)
		return "", 0, 0, 0, err
	}

	return paymentResp.PayURL, purchaseId, amount, deviceSlotCount, nil
}

func (s PaymentService) ActivateTrial(ctx context.Context, telegramId int64) (string, error) {
	if config.TrialDays() == 0 {
		return "", nil
	}
	customer, err := s.customerRepository.FindByTelegramId(ctx, telegramId)
	if err != nil {
		slog.Error("Error finding customer", "error", err)
		return "", err
	}
	if customer == nil {
		return "", fmt.Errorf("customer %d not found", telegramId)
	}
	user, err := s.remnawaveClient.CreateOrUpdateUser(ctx, customer.ID, telegramId, config.TrialTrafficLimit(), config.TrialDays(), true)
	if err != nil {
		slog.Error("Error creating user", "error", err)
		return "", err
	}

	customerFilesToUpdate := map[string]interface{}{
		"subscription_link": user.SubscriptionUrl,
		"expire_at":         user.ExpireAt,
		"is_trial":          true,
	}

	err = s.customerRepository.UpdateFields(ctx, customer.ID, customerFilesToUpdate)
	if err != nil {
		return "", err
	}

	return user.SubscriptionUrl, nil

}

func (s PaymentService) createTributeInvoice(ctx context.Context, amount float64, months int, customer *database.Customer) (url string, purchaseId int64, chargedAmount float64, deviceSlotCount int, err error) {
	// Tribute never bundles a device addon into this charge — Tribute-linked customers are always
	// standalone-billed (DetermineDeviceAddonBillingMode), so deviceSlotCount is always 0 here.
	purchaseId, err = s.purchaseRepository.Create(ctx, &database.Purchase{
		InvoiceType: database.InvoiceTypeTribute,
		Status:      database.PurchaseStatusPending,
		Amount:      amount,
		Currency:    "RUB",
		CustomerID:  customer.ID,
		Month:       months,
	})
	if err != nil {
		slog.Error("Error creating purchase", "error", err)
		return "", 0, 0, 0, err
	}

	return "", purchaseId, amount, 0, nil
}

// calculateRollover returns how many bytes of an existing admin topup remain unused
// and should be carried into the next subscription period.
//
// Formula: rollover = max(0, topup_bytes - used_bytes)
// where topup_bytes = target_limit - base_limit.
func (s PaymentService) calculateRollover(ctx context.Context, telegramID int64) int64 {
	if s.topupRepository == nil {
		return 0
	}
	topup, err := s.topupRepository.FindLatestCompletedByTelegramID(ctx, telegramID)
	if err != nil {
		slog.Warn("rollover: find latest topup", "telegram_id", telegramID, "error", err)
		return 0
	}
	if topup == nil || topup.TargetTrafficLimitBytes == nil {
		return 0
	}

	baseBytes := int64(config.TrafficLimit())
	topupBytes := *topup.TargetTrafficLimitBytes - baseBytes
	if topupBytes <= 0 {
		return 0
	}

	users, err := s.remnawaveClient.GetUsersByTelegramID(ctx, telegramID)
	if err != nil || len(users) == 0 {
		slog.Warn("rollover: cannot get remnawave user, skipping rollover", "telegram_id", telegramID, "error", err)
		return 0
	}

	u := users[0]
	if u.UserTraffic == nil || u.UserTraffic.UsedTrafficBytes == 0 {
		return topupBytes
	}

	rollover := topupBytes - int64(u.UserTraffic.UsedTrafficBytes)
	if rollover <= 0 {
		return 0
	}
	return rollover
}

// applyPendingDeviceDecrease shrinks the Remnawave device limit by exactly the queued delta and
// commits the new device_count, clearing the pending flag. Duplicated in rollypay/webhook.go's
// standalone renewal path rather than shared — same reasoning as upsertDeviceAddon's billing-mode
// duplication there, this package already imports rollypay, so the reverse would cycle.
//
// The delta (not an absolute base+count recompute) matters: this bot never assumes what the free
// base allowance is, only ever adjusts relative to whatever Remnawave currently reports — matching
// every other device-limit mutation in this codebase (buying a slot, expiry-trimming an unpaid
// one). Best-effort: a failure here shouldn't block the renewal it's piggybacking on.
func applyPendingDeviceDecrease(ctx context.Context, rw *remnawave.Client, repo *database.DeviceAddonRepository, addon *database.DeviceAddon, userUUID uuid.UUID, currentLimit *int) {
	target := *addon.PendingDeviceCount
	if target < addon.DeviceCount {
		limit := 0
		if currentLimit != nil {
			limit = *currentLimit
		}
		newLimit := database.DeviceLimitAfterDecrease(limit, addon.DeviceCount, target)
		if err := rw.UpdateUserDeviceLimit(ctx, userUUID, newLimit); err != nil {
			slog.Error("apply pending device decrease: shrink limit failed", "addon_id", addon.ID, "error", err)
			return
		}
	}
	if err := repo.UpdateDeviceCount(ctx, addon.ID, target); err != nil {
		slog.Error("apply pending device decrease: update count failed", "addon_id", addon.ID, "error", err)
		return
	}
	if err := repo.SetPendingDeviceCount(ctx, addon.ID, nil); err != nil {
		slog.Error("apply pending device decrease: clear pending failed", "addon_id", addon.ID, "error", err)
	}
}

// isTrafficLimited reports whether the customer's Remnawave account is currently blocked for
// exceeding its traffic cap (status LIMITED) despite still having subscription days left — used
// by ProcessPurchaseById to force a reset on early renewal even when the lapse check says no.
func (s PaymentService) isTrafficLimited(ctx context.Context, telegramID int64) bool {
	users, err := s.remnawaveClient.GetUsersByTelegramID(ctx, telegramID)
	if err != nil || len(users) == 0 {
		return false
	}
	return strings.ToUpper(users[0].Status) == "LIMITED"
}
