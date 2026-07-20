package rollypay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
)

// baseURL is fixed by RollyPay (docs.rollypay.io/api/overview) — not configurable per-deployment.
const baseURL = "https://rollypay.io"

// Client talks to RollyPay's payments API (create/query one-off payments). Webhook receiving is
// handled separately by WebhookClient in webhook.go.
type Client struct {
	httpClient    *http.Client
	apiKey        string
	signingSecret string
	terminalID    string
}

func NewClient(apiKey, signingSecret, terminalID string) *Client {
	return &Client{
		httpClient:    &http.Client{},
		apiKey:        apiKey,
		signingSecret: signingSecret,
		terminalID:    terminalID,
	}
}

// SigningSecret exposes the webhook-signature secret to the webhook handler, which lives in this
// same package but is constructed separately (it has its own, larger set of dependencies).
func (c *Client) SigningSecret() string {
	return c.signingSecret
}

func (c *Client) do(ctx context.Context, method, path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("X-Nonce", uuid.NewString())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("rollypay API error: status %d, body %s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// CreatePayment — POST /api/v1/payments. Returns the payment including pay_url for the customer.
func (c *Client) CreatePayment(ctx context.Context, req CreatePaymentRequest) (*Payment, error) {
	req.TerminalID = c.terminalID
	if req.PaymentCurrency == "" {
		req.PaymentCurrency = "RUB"
	}
	var payment Payment
	if err := c.do(ctx, http.MethodPost, "/api/v1/payments", req, &payment); err != nil {
		return nil, err
	}
	return &payment, nil
}

// GetPaymentStatus — GET /api/v1/payments/{id}.
func (c *Client) GetPaymentStatus(ctx context.Context, paymentID string) (*Payment, error) {
	var payment Payment
	if err := c.do(ctx, http.MethodGet, "/api/v1/payments/"+paymentID, nil, &payment); err != nil {
		return nil, err
	}
	return &payment, nil
}
