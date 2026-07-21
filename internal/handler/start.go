package handler

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"log/slog"

	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/translation"
	"remnawave-tg-shop-bot/utils"
)

func (h Handler) StartCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	ctxWithTime, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	langCode := update.Message.From.LanguageCode
	var username *string
	if update.Message.From.Username != "" {
		username = &update.Message.From.Username
	}
	existingCustomer, err := h.customerRepository.FindByTelegramId(ctxWithTime, update.Message.Chat.ID)
	if err != nil {
		slog.Error("error finding customer by telegram id", "error", err)
		return
	}

	if existingCustomer == nil {
		existingCustomer, err = h.customerRepository.Create(ctxWithTime, &database.Customer{
			TelegramID: update.Message.Chat.ID,
			Language:   langCode,
			Username:   username,
		})
		if err != nil {
			slog.Error("error creating customer", "error", err)
			return
		}

		if parts := strings.SplitN(update.Message.Text, " ", 2); len(parts) == 2 && strings.HasPrefix(parts[1], "ref_") {
			code := strings.TrimPrefix(parts[1], "ref_")
			referrerId, err := strconv.ParseInt(code, 10, 64)
			if err != nil {
				slog.Error("error parsing referrer id", "error", err)
				return
			}
			referrer, err := h.customerRepository.FindByTelegramId(ctx, referrerId)
			if err == nil && referrer != nil {
				_, err := h.referralRepository.Create(ctx, referrerId, existingCustomer.TelegramID)
				if err != nil {
					slog.Error("error creating referral", "error", err)
					return
				}
				slog.Info("referral created", "referrerId", utils.MaskHalfInt64(referrerId), "refereeId", utils.MaskHalfInt64(existingCustomer.TelegramID))
			}
		}
	} else {
		updates := map[string]interface{}{"language": langCode}
		if username != nil {
			updates["username"] = *username
		}
		err = h.customerRepository.UpdateFields(ctx, existingCustomer.ID, updates)
		if err != nil {
			slog.Error("Error updating customer", "error", err)
			return
		}
	}

	inlineKeyboard := h.buildStartKeyboard(existingCustomer, langCode)
	greetingText := buildGreetingText(existingCustomer, langCode)

	m, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      update.Message.Chat.ID,
		Text:        "🧹",
		ReplyMarkup: models.ReplyKeyboardRemove{RemoveKeyboard: true},
	})
	if err != nil {
		slog.Error("Error sending removing reply keyboard", "error", err)
		return
	}
	_, err = b.DeleteMessage(ctx, &bot.DeleteMessageParams{
		ChatID:    update.Message.Chat.ID,
		MessageID: m.ID,
	})
	if err != nil {
		slog.Error("Error deleting message", "error", err)
		return
	}

	_, err = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    update.Message.Chat.ID,
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: inlineKeyboard,
		},
		Text: greetingText,
	})
	if err != nil {
		slog.Error("Error sending /start message", "error", err)
	}
}

func (h Handler) StartCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	ctxWithTime, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	callback := update.CallbackQuery
	langCode := callback.From.LanguageCode

	existingCustomer, err := h.customerRepository.FindByTelegramId(ctxWithTime, callback.From.ID)
	if err != nil {
		slog.Error("error finding customer by telegram id", "error", err)
		return
	}
	if existingCustomer == nil {
		return
	}

	inlineKeyboard := h.buildStartKeyboard(existingCustomer, langCode)
	greetingText := buildGreetingText(existingCustomer, langCode)

	_, err = b.EditMessageText(ctxWithTime, &bot.EditMessageTextParams{
		ChatID:    callback.Message.Message.Chat.ID,
		MessageID: callback.Message.Message.ID,
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: inlineKeyboard,
		},
		Text: greetingText,
	})
	if err != nil {
		slog.Error("Error sending /start message", "error", err)
	}
}

// buildGreetingText returns the greeting text. Subscription details are in the status screen.
func buildGreetingText(customer *database.Customer, langCode string) string {
	tm := translation.GetInstance()
	if customer.ExpireAt != nil && !customer.ExpireAt.After(time.Now()) {
		return tm.GetText(langCode, "subscription_expired") + "\n\n" + tm.GetText(langCode, "greeting")
	}
	return tm.GetText(langCode, "greeting")
}

func (h Handler) resolveConnectButton(lang string) []models.InlineKeyboardButton {
	bd := h.translation.GetButton(lang, "connect_button")
	if config.GetMiniAppURL() != "" {
		return []models.InlineKeyboardButton{bd.InlineWebApp(config.GetMiniAppURL())}
	}
	return []models.InlineKeyboardButton{bd.InlineCallback(CallbackConnect)}
}

// buildStartKeyboard groups related buttons two-per-row instead of one long vertical stack:
// subscription-management actions (connect/status/devices/topup) in one cluster, informational
// links (server status/support/feedback/channel/tos) in another. Buy and trial stay full-width as
// the primary calls to action.
func (h Handler) buildStartKeyboard(existingCustomer *database.Customer, langCode string) [][]models.InlineKeyboardButton {
	var inlineKeyboard [][]models.InlineKeyboardButton

	if existingCustomer.SubscriptionLink == nil && config.TrialDays() > 0 {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{h.translation.GetButton(langCode, "trial_button").InlineCallback(CallbackTrial)})
	}

	buyButtonKey := "buy_button"
	if !existingCustomer.IsTrial && existingCustomer.SubscriptionLink != nil {
		buyButtonKey = "renew_subscription_button"
	}
	inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{h.translation.GetButton(langCode, buyButtonKey).InlineCallback(CallbackBuy)})

	hasActiveSubscription := existingCustomer.SubscriptionLink != nil && existingCustomer.ExpireAt != nil && existingCustomer.ExpireAt.After(time.Now())

	var managementButtons []models.InlineKeyboardButton
	if hasActiveSubscription {
		managementButtons = append(managementButtons, h.resolveConnectButton(langCode)...)
		if config.StatusEnabled() {
			managementButtons = append(managementButtons, h.translation.GetButton(langCode, "status_button").InlineCallback(CallbackStatus))
		}
		managementButtons = append(managementButtons, h.translation.GetButton(langCode, "devices_button").InlineCallback(CallbackDevices))
		if config.TopupEnabled() && !existingCustomer.IsTrial {
			managementButtons = append(managementButtons, h.translation.GetButton(langCode, "topup_button").InlineCallback(CallbackTopup))
		}
	}
	inlineKeyboard = append(inlineKeyboard, chunkButtons(managementButtons, 2)...)

	// Referrals only for active paid subscribers
	if config.GetReferralDays() > 0 && hasActiveSubscription && !existingCustomer.IsTrial {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{h.translation.GetButton(langCode, "referral_button").InlineCallback(CallbackReferral)})
	}

	var infoButtons []models.InlineKeyboardButton
	if config.ServerStatusURL() != "" {
		infoButtons = append(infoButtons, h.translation.GetButton(langCode, "server_status_button").InlineURL(config.ServerStatusURL()))
	}
	if config.SupportURL() != "" {
		infoButtons = append(infoButtons, h.translation.GetButton(langCode, "support_button").InlineURL(config.SupportURL()))
	}
	if config.FeedbackURL() != "" {
		infoButtons = append(infoButtons, h.translation.GetButton(langCode, "feedback_button").InlineURL(config.FeedbackURL()))
	}
	if config.ChannelURL() != "" {
		infoButtons = append(infoButtons, h.translation.GetButton(langCode, "channel_button").InlineURL(config.ChannelURL()))
	}
	if config.TosURL() != "" {
		infoButtons = append(infoButtons, h.translation.GetButton(langCode, "tos_button").InlineURL(config.TosURL()))
	}
	if config.ProxyURL() != "" {
		infoButtons = append(infoButtons, h.translation.GetButton(langCode, "proxy_button").InlineURL(config.ProxyURL()))
	}
	inlineKeyboard = append(inlineKeyboard, chunkButtons(infoButtons, 2)...)

	return inlineKeyboard
}

// chunkButtons packs a flat button list into rows of at most size buttons each.
func chunkButtons(buttons []models.InlineKeyboardButton, size int) [][]models.InlineKeyboardButton {
	var rows [][]models.InlineKeyboardButton
	for i := 0; i < len(buttons); i += size {
		end := i + size
		if end > len(buttons) {
			end = len(buttons)
		}
		rows = append(rows, buttons[i:end])
	}
	return rows
}
