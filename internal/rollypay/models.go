package rollypay

import "time"

// CreatePaymentRequest is the body for POST /api/v1/payments. Fields match RollyPay's documented
// API exactly (docs.rollypay.io/api/payments); PaymentMethod is intentionally left empty by every
// caller in this codebase so RollyPay's hosted pay_url page lets the customer pick among the
// terminal's connected methods (SBP/crypto/cryptobot/xrocket) instead of the bot building its own
// method picker.
type CreatePaymentRequest struct {
	TerminalID         string         `json:"terminal_id"`
	Amount             string         `json:"amount"`
	PaymentCurrency    string         `json:"payment_currency,omitempty"`
	PaymentMethod      string         `json:"payment_method,omitempty"`
	OrderID            string         `json:"order_id"`
	Description        string         `json:"description,omitempty"`
	CustomerID         string         `json:"customer_id,omitempty"`
	SuccessRedirectURL string         `json:"success_redirect_url,omitempty"`
	FailRedirectURL    string         `json:"fail_redirect_url,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	Test               bool           `json:"test,omitempty"`
}

// Payment is the response shape for both POST /api/v1/payments and GET /api/v1/payments/{id}.
type Payment struct {
	PaymentID       string     `json:"payment_id"`
	OrderID         string     `json:"order_id"`
	Status          string     `json:"status"`
	Token           string     `json:"token"`
	PayURL          string     `json:"pay_url"`
	Amount          string     `json:"amount"`
	PaymentCurrency string     `json:"payment_currency"`
	CreatedAt       time.Time  `json:"created_at"`
	ExpiresAt       time.Time  `json:"expires_at"`
	PaidAt          *time.Time `json:"paid_at"`
}

// WebhookPayload is the body RollyPay POSTs to the configured Callback URL.
type WebhookPayload struct {
	EventType string `json:"event_type"`
	PaymentID string `json:"payment_id"`
	OrderID   string `json:"order_id"`
	Status    string `json:"status"`
	Amount    string `json:"amount"`
	Currency  string `json:"currency"`
	Test      bool   `json:"test"`
}

const (
	EventPaymentCreated    = "payment.created"
	EventPaymentPaid       = "payment.paid"
	EventPaymentCanceled   = "payment.canceled"
	EventPaymentChargeback = "payment.chargeback"
	EventPaymentRefunded   = "payment.refunded"
)
