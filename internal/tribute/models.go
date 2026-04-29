package tribute

import (
	"fmt"
	"time"

	"github.com/google/uuid"
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
	// Digital product fields
	ProductID         int       `json:"product_id"`
	PurchaseID        int       `json:"purchase_id"`
	TransactionID     int       `json:"transaction_id"`
	ProductName       string    `json:"product_name"`
	PurchaseCreatedAt time.Time `json:"purchase_created_at"`
	// Common fields
	Amount         int    `json:"amount"`
	Currency       string `json:"currency"`
	UserID         int    `json:"user_id"`
	TelegramUserID int64  `json:"telegram_user_id"`
}

func parseUUID(s string) (uuid.UUID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse uuid %q: %w", s, err)
	}
	return u, nil
}
