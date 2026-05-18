package payment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"remnawave-tg-shop-bot/internal/cache"
	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/cryptopay"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/moynalog"
	"remnawave-tg-shop-bot/internal/remnawave"
	"remnawave-tg-shop-bot/internal/translation"
	"remnawave-tg-shop-bot/internal/yookasa"
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
	cryptoPayClient    *cryptopay.Client
	yookasaClient      *yookasa.Client
	referralRepository *database.ReferralRepository
	cache              *cache.Cache
	moynalogClient     *moynalog.Client
	topupRepository    *database.TrafficTopupRepository
}

func NewPaymentService(
	translation *translation.Manager,
	purchaseRepository *database.PurchaseRepository,
	remnawaveClient *remnawave.Client,
	customerRepository *database.CustomerRepository,
	telegramBot *bot.Bot,
	cryptoPayClient *cryptopay.Client,
	yookasaClient *yookasa.Client,
	referralRepository *database.ReferralRepository,
	cache *cache.Cache,
	moynalogClient *moynalog.Client,
	topupRepository *database.TrafficTopupRepository,
) *PaymentService {
	return &PaymentService{
		purchaseRepository: purchaseRepository,
		remnawaveClient:    remnawaveClient,
		customerRepository: customerRepository,
		telegramBot:        telegramBot,
		translation:        translation,
		cryptoPayClient:    cryptoPayClient,
		yookasaClient:      yookasaClient,
		referralRepository: referralRepository,
		cache:              cache,
		moynalogClient:     moynalogClient,
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

	slog.Info("checking conditions for Moynalog receipt", "invoice_type", purchase.InvoiceType, "moynalog_client", s.moynalogClient != nil)
	if purchase.InvoiceType == database.InvoiceTypeYookasa && s.moynalogClient != nil {
		slog.Info("attempting to send receipt to Moynalog", "purchase_id", utils.MaskHalfInt64(purchase.ID), "amount", purchase.Amount, "month", purchase.Month)
		go func() {
			moynalogCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			err := s.sendReceiptToMoynalog(moynalogCtx, purchase)
			if err != nil {
				slog.Error("error sending receipt to Moynalog", "error", err, "purchase_id", utils.MaskHalfInt64(purchase.ID))
				_, err = s.telegramBot.SendMessage(ctx, &bot.SendMessageParams{
					ChatID: config.GetAdminTelegramId(),
					Text:   "Ошибка при отправке чека в Мой налог. Проверьте логи.",
				})
				if err != nil {
					slog.Error("error while sending moy nalog error message", "error", err, "purchase_id", utils.MaskHalfInt64(purchase.ID))
				}
			} else {
				slog.Info("successfully sent receipt to Moynalog", "purchase_id", utils.MaskHalfInt64(purchase.ID))
			}
		}()
	} else {
		if purchase.InvoiceType != database.InvoiceTypeYookasa {
			slog.Info("not sending receipt to Moynalog - not a Yookasa invoice", "invoice_type", purchase.InvoiceType, "purchase_id", utils.MaskHalfInt64(purchase.ID))
		} else if s.moynalogClient == nil {
			slog.Error("not sending receipt to Moynalog - client is nil", "purchase_id", utils.MaskHalfInt64(purchase.ID))
		}
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
	case database.InvoiceTypeCrypto:
		return s.createCryptoInvoice(ctx, amount, months, customer)
	case database.InvoiceTypeYookasa:
		return s.createYookasaInvoice(ctx, amount, months, customer)
	case database.InvoiceTypeTelegram:
		return s.createTelegramInvoice(ctx, amount, months, customer)
	case database.InvoiceTypeTribute:
		return s.createTributeInvoice(ctx, amount, months, customer)
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

func (s PaymentService) createCryptoInvoice(ctx context.Context, amount float64, months int, customer *database.Customer) (url string, purchaseId int64, err error) {
	purchaseId, err = s.purchaseRepository.Create(ctx, &database.Purchase{
		InvoiceType: database.InvoiceTypeCrypto,
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

	invoice, err := s.cryptoPayClient.CreateInvoice(&cryptopay.InvoiceRequest{
		CurrencyType:   "fiat",
		Fiat:           "RUB",
		Amount:         fmt.Sprintf("%d", int(amount)),
		AcceptedAssets: "USDT",
		Payload:        fmt.Sprintf("purchaseId=%d&username=%s", purchaseId, ctx.Value("username")),
		Description:    fmt.Sprintf("Subscription on %d month", months),
		PaidBtnName:    "callback",
		PaidBtnUrl:     config.BotURL(),
	})
	if err != nil {
		slog.Error("Error creating invoice", "error", err)
		return "", 0, err
	}

	updates := map[string]interface{}{
		"crypto_invoice_url": invoice.BotInvoiceUrl,
		"crypto_invoice_id":  invoice.InvoiceID,
		"status":             database.PurchaseStatusPending,
	}

	err = s.purchaseRepository.UpdateFields(ctx, purchaseId, updates)
	if err != nil {
		slog.Error("Error updating purchase", "error", err)
		return "", 0, err
	}

	return invoice.BotInvoiceUrl, purchaseId, nil
}

func (s PaymentService) createYookasaInvoice(ctx context.Context, amount float64, months int, customer *database.Customer) (url string, purchaseId int64, err error) {
	purchaseId, err = s.purchaseRepository.Create(ctx, &database.Purchase{
		InvoiceType: database.InvoiceTypeYookasa,
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

	invoice, err := s.yookasaClient.CreateInvoice(ctx, int(amount), months, customer.ID, purchaseId)
	if err != nil {
		slog.Error("Error creating invoice", "error", err)
		return "", 0, err
	}

	updates := map[string]interface{}{
		"yookasa_url": invoice.Confirmation.ConfirmationURL,
		"yookasa_id":  invoice.ID,
		"status":      database.PurchaseStatusPending,
	}

	err = s.purchaseRepository.UpdateFields(ctx, purchaseId, updates)
	if err != nil {
		slog.Error("Error updating purchase", "error", err)
		return "", 0, err
	}

	return invoice.Confirmation.ConfirmationURL, purchaseId, nil
}

func (s PaymentService) createTelegramInvoice(ctx context.Context, amount float64, months int, customer *database.Customer) (url string, purchaseId int64, err error) {
	purchaseId, err = s.purchaseRepository.Create(ctx, &database.Purchase{
		InvoiceType: database.InvoiceTypeTelegram,
		Status:      database.PurchaseStatusNew,
		Amount:      amount,
		Currency:    "STARS",
		CustomerID:  customer.ID,
		Month:       months,
	})
	if err != nil {
		slog.Error("Error creating purchase", "error", err)
		return "", 0, nil
	}

	invoiceUrl, err := s.telegramBot.CreateInvoiceLink(ctx, &bot.CreateInvoiceLinkParams{
		Title:    s.translation.GetText(customer.Language, "invoice_title"),
		Currency: "XTR",
		Prices: []models.LabeledPrice{
			{
				Label:  s.translation.GetText(customer.Language, "invoice_label"),
				Amount: int(amount),
			},
		},
		Description: s.translation.GetText(customer.Language, "invoice_description"),
		Payload:     fmt.Sprintf("%d&%s", purchaseId, ctx.Value("username")),
	})

	updates := map[string]interface{}{
		"status": database.PurchaseStatusPending,
	}

	err = s.purchaseRepository.UpdateFields(ctx, purchaseId, updates)
	if err != nil {
		slog.Error("Error updating purchase", "error", err)
		return "", 0, err
	}

	return invoiceUrl, purchaseId, nil
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

func (s PaymentService) CancelYookassaPayment(purchaseId int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	purchase, err := s.purchaseRepository.FindById(ctx, purchaseId)
	if err != nil {
		return err
	}
	if purchase == nil {
		return fmt.Errorf("purchase with crypto invoice id %s not found", utils.MaskHalfInt64(purchaseId))
	}

	purchaseFieldsToUpdate := map[string]interface{}{
		"status": database.PurchaseStatusCancel,
	}

	err = s.purchaseRepository.UpdateFields(ctx, purchaseId, purchaseFieldsToUpdate)
	if err != nil {
		return err
	}

	return nil
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
// Formula: rollover = topup_bytes - max(0, used_bytes - base_bytes)
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

	usedBeyondBase := int64(u.UserTraffic.UsedTrafficBytes) - baseBytes
	if usedBeyondBase <= 0 {
		return topupBytes
	}

	rollover := topupBytes - usedBeyondBase
	if rollover <= 0 {
		return 0
	}
	return rollover
}

func (s PaymentService) sendReceiptToMoynalog(ctx context.Context, purchase *database.Purchase) error {
	if s.moynalogClient == nil {
		return fmt.Errorf("moynalog client not initialized")
	}

	var monthString string
	switch purchase.Month {
	case 1:
		monthString = "месяц"
	case 3, 4:
		monthString = "месяца"
	default:
		monthString = "месяцев"
	}
	comment := fmt.Sprintf("Подписка на %d %s", purchase.Month, monthString)
	amount := purchase.Amount

	_, err := s.moynalogClient.CreateIncome(ctx, amount, comment)
	if err != nil {
		return fmt.Errorf("failed to create income in Moynalog: %w", err)
	}

	slog.Info("Receipt sent to Moynalog", "purchase_id", utils.MaskHalfInt64(purchase.ID), "amount", amount, "comment", comment)
	return nil
}


