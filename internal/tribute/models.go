package tribute

import (
	"time"
)

type SubscriptionWebhook struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	SentAt    time.Time `json:"sent_at"`
	Payload   Payload   `json:"payload"`
}

type Payload struct {
	// Subscription fields
	SubscriptionName string    `json:"subscription_name"`
	SubscriptionID   int       `json:"subscription_id"`
	PeriodID         int       `json:"period_id"`
	Period           string    `json:"period"`
	Price            int       `json:"price"`
	ChannelID        int       `json:"channel_id"`
	ChannelName      string    `json:"channel_name"`
	ExpiresAt        time.Time `json:"expires_at"`
	// Common fields
	Amount         int    `json:"amount"` // minor units (kopecks) — divide by 100 for whole rubles
	Currency       string `json:"currency"`
	UserID         int    `json:"user_id"`
	TelegramUserID int64  `json:"telegram_user_id"`
}
