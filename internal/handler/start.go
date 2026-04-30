package handler

import (
	"context"
	"fmt"
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
	existingCustomer, err := h.customerRepository.FindByTelegramId(ctx, update.Message.Chat.ID)
	if err != nil {
		slog.Error("error finding customer by telegram id", "error", err)
		return
	}

	if existingCustomer == nil {
		existingCustomer, err = h.customerRepository.Create(ctxWithTime, &database.Customer{
			TelegramID: update.Message.Chat.ID,
			Language:   langCode,
		})
		if err != nil {
			slog.Error("error creating customer", "error", err)
			return
		}

		if strings.Contains(update.Message.Text, "ref_") {
			arg := strings.Split(update.Message.Text, " ")[1]
			if strings.HasPrefix(arg, "ref_") {
				code := strings.TrimPrefix(arg, "ref_")
				referrerId, err := strconv.ParseInt(code, 10, 64)
				if err != nil {
					slog.Error("error parsing referrer id", "error", err)
					return
				}
				_, err = h.customerRepository.FindByTelegramId(ctx, referrerId)
				if err == nil {
					_, err := h.referralRepository.Create(ctx, referrerId, existingCustomer.TelegramID)
					if err != nil {
						slog.Error("error creating referral", "error", err)
						return
					}
					slog.Info("referral created", "referrerId", utils.MaskHalfInt64(referrerId), "refereeId", utils.MaskHalfInt64(existingCustomer.TelegramID))
				}
			}
		}
	} else {
		err = h.customerRepository.UpdateFields(ctx, existingCustomer.ID, map[string]interface{}{"language": langCode})
		if err != nil {
			slog.Error("Error updating customer", "error", err)
			return
		}
	}

	resetStrategy, lastResetAt := h.fetchResetInfo(ctx, existingCustomer)
	inlineKeyboard := h.buildStartKeyboard(existingCustomer, langCode)
	greetingText := buildGreetingText(existingCustomer, langCode, resetStrategy, lastResetAt)

	m, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "🧹",
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

	resetStrategy, lastResetAt := h.fetchResetInfo(ctxWithTime, existingCustomer)
	inlineKeyboard := h.buildStartKeyboard(existingCustomer, langCode)
	greetingText := buildGreetingText(existingCustomer, langCode, resetStrategy, lastResetAt)

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

// fetchResetInfo fetches traffic strategy and last reset time from Remnawave.
// Returns empty values on failure — callers handle gracefully (no reset shown).
func (h Handler) fetchResetInfo(ctx context.Context, customer *database.Customer) (resetStrategy string, lastResetAt *time.Time) {
	if customer.SubscriptionLink == nil || customer.ExpireAt == nil || !customer.ExpireAt.After(time.Now()) {
		return
	}
	rwCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	rwUsers, err := h.remnawaveClient.GetUsersByTelegramID(rwCtx, customer.TelegramID)
	if err == nil && len(rwUsers) > 0 {
		resetStrategy = rwUsers[0].TrafficLimitStrategy
		lastResetAt = rwUsers[0].LastTrafficResetAt
	}
	return
}

// buildGreetingText returns a personalized greeting showing subscription status and next reset.
func buildGreetingText(customer *database.Customer, langCode string, resetStrategy string, lastResetAt *time.Time) string {
	tm := translation.GetInstance()

	if customer.SubscriptionLink != nil && customer.ExpireAt != nil && customer.ExpireAt.After(time.Now()) {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf(tm.GetText(langCode, "subscription_active"), customer.ExpireAt.Format("02.01.2006")))
		if nextReset := calcNextReset(resetStrategy, lastResetAt); nextReset != nil {
			sb.WriteString("\n" + fmt.Sprintf(tm.GetText(langCode, "next_traffic_reset"), nextReset.Format("02.01.2006")))
		}
		sb.WriteString("\n\n" + tm.GetText(langCode, "greeting"))
		return sb.String()
	} else if customer.ExpireAt != nil {
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

func (h Handler) buildStartKeyboard(existingCustomer *database.Customer, langCode string) [][]models.InlineKeyboardButton {
	var inlineKeyboard [][]models.InlineKeyboardButton

	if existingCustomer.SubscriptionLink == nil && config.TrialDays() > 0 {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{h.translation.GetButton(langCode, "trial_button").InlineCallback(CallbackTrial)})
	}

	inlineKeyboard = append(inlineKeyboard, [][]models.InlineKeyboardButton{{h.translation.GetButton(langCode, "buy_button").InlineCallback(CallbackBuy)}}...)

	if existingCustomer.SubscriptionLink != nil && existingCustomer.ExpireAt != nil && existingCustomer.ExpireAt.After(time.Now()) {
		inlineKeyboard = append(inlineKeyboard, h.resolveConnectButton(langCode))
	}

	if config.TopupEnabled() && existingCustomer.SubscriptionLink != nil && existingCustomer.ExpireAt != nil && existingCustomer.ExpireAt.After(time.Now()) && !existingCustomer.IsTrial {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{
			h.translation.GetButton(langCode, "topup_button").InlineCallback(CallbackTopup),
		})
	}

	if existingCustomer.SubscriptionLink != nil && existingCustomer.ExpireAt != nil && existingCustomer.ExpireAt.After(time.Now()) {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{
			h.translation.GetButton(langCode, "devices_button").InlineCallback(CallbackDevices),
		})
	}

	// Referrals only for active paid subscribers
	if config.GetReferralDays() > 0 && existingCustomer.SubscriptionLink != nil && existingCustomer.ExpireAt != nil && existingCustomer.ExpireAt.After(time.Now()) && !existingCustomer.IsTrial {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{h.translation.GetButton(langCode, "referral_button").InlineCallback(CallbackReferral)})
	}

	if config.ServerStatusURL() != "" {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{h.translation.GetButton(langCode, "server_status_button").InlineURL(config.ServerStatusURL())})
	}
	if config.SupportURL() != "" {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{h.translation.GetButton(langCode, "support_button").InlineURL(config.SupportURL())})
	}
	if config.FeedbackURL() != "" {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{h.translation.GetButton(langCode, "feedback_button").InlineURL(config.FeedbackURL())})
	}
	if config.ChannelURL() != "" {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{h.translation.GetButton(langCode, "channel_button").InlineURL(config.ChannelURL())})
	}
	if config.TosURL() != "" {
		inlineKeyboard = append(inlineKeyboard, []models.InlineKeyboardButton{h.translation.GetButton(langCode, "tos_button").InlineURL(config.TosURL())})
	}
	return inlineKeyboard
}
