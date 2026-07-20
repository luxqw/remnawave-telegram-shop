package payment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"remnawave-tg-shop-bot/internal/cache"
	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/remnawave"
	"remnawave-tg-shop-bot/internal/rollypay"
	"remnawave-tg-shop-bot/internal/translation"
	"remnawave-tg-shop-bot/utils"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type PaymentService struct {
	purchaseRepository *database.PurchaseRepository
	remnawaveClient    *remnawave.Client
	customerRepository *database.CustomerRepository
	telegramBot        *bot.Bot
	translation        *translation.Manager
	rollypayClient     *rollypay.Client
	referralRepository *database.ReferralRepository
	cache              *cache.Cache
	topupRepository    *database.TrafficTopupRepository
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
) *PaymentService {
	return &PaymentService{
		purchaseRepository: purchaseRepository,
		remnawaveClient:    remnawaveClient,
		customerRepository: customerRepository,
		telegramBot:        telegramBot,
		translation:        translation,
		rollypayClient:     rollypayClient,
		referralRepository: referralRepository,
		cache:              cache,
		topupRepository:    topupRepository,
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

	// Reset the traffic counter so the new period starts from 0.
	// Rollover was snapshotted above before this call, so the ordering is correct.
	if resetErr := s.remnawaveClient.ResetUserTraffic(ctx, user.UUID); resetErr != nil {
		slog.Error("renewal: failed to reset user traffic", "uuid", user.UUID, "telegram_id", customer.TelegramID, "error", resetErr)
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

	_, err = s.telegramBot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: customer.TelegramID,
		Text:   s.translation.GetText(customer.Language, "subscription_activated"),
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

func (s PaymentService) CreatePurchase(ctx context.Context, amount float64, months int, customer *database.Customer, invoiceType database.InvoiceType) (url string, purchaseId int64, err error) {
	switch invoiceType {
	case database.InvoiceTypeTribute:
		return s.createTributeInvoice(ctx, amount, months, customer)
	case database.InvoiceTypeRollyPay:
		return s.createRollyPayInvoice(ctx, amount, months, customer)
	default:
		return "", 0, fmt.Errorf("unknown invoice type: %s", invoiceType)
	}
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

func (s PaymentService) createRollyPayInvoice(ctx context.Context, amount float64, months int, customer *database.Customer) (url string, purchaseId int64, err error) {
	purchaseId, err = s.purchaseRepository.Create(ctx, &database.Purchase{
		InvoiceType: database.InvoiceTypeRollyPay,
		Status:      database.PurchaseStatusNew,
		Amount:      amount,
		Currency:    "RUB",
		CustomerID:  customer.ID,
		Month:       months,
	})
	if err != nil {
		slog.Error("Error creating purchase", "error", err)
		return "", 0, err
	}

	paymentResp, err := s.rollypayClient.CreatePayment(ctx, rollypay.CreatePaymentRequest{
		Amount:      fmt.Sprintf("%.2f", amount),
		OrderID:     fmt.Sprintf("sub-%d", purchaseId),
		Description: fmt.Sprintf("Subscription %d month(s)", months),
	})
	if err != nil {
		slog.Error("Error creating rollypay payment", "error", err)
		return "", 0, err
	}

	updates := map[string]interface{}{
		"provider_payment_id": paymentResp.PaymentID,
		"provider_pay_url":    paymentResp.PayURL,
		"status":              database.PurchaseStatusPending,
	}

	err = s.purchaseRepository.UpdateFields(ctx, purchaseId, updates)
	if err != nil {
		slog.Error("Error updating purchase", "error", err)
		return "", 0, err
	}

	return paymentResp.PayURL, purchaseId, nil
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

func (s PaymentService) createTributeInvoice(ctx context.Context, amount float64, months int, customer *database.Customer) (url string, purchaseId int64, err error) {
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
		return "", 0, err
	}

	return "", purchaseId, nil
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
