package cardlink

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes = 1 << 20 // 1 MB

type Client struct {
	httpClient *http.Client
	baseURL    string
	token      string
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
	}
}

func (c *Client) CreateBill(ctx context.Context, req *CreateBillRequest) (*CreateBillResponse, error) {
	endpoint := fmt.Sprintf("%s/bill/create", c.baseURL)

	form := url.Values{}
	form.Set("amount", req.Amount)
	form.Set("shop_id", req.ShopID)
	if req.OrderID != "" {
		form.Set("order_id", req.OrderID)
	}
	if req.Description != "" {
		form.Set("description", req.Description)
	}
	if req.CurrencyIn != "" {
		form.Set("currency_in", req.CurrencyIn)
	}
	if req.TTL > 0 {
		form.Set("ttl", fmt.Sprintf("%d", req.TTL))
	}
	if req.Custom != "" {
		form.Set("custom", req.Custom)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("API error, status: %d, body: %s", resp.StatusCode, string(body))
	}

	var result CreateBillResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if result.Success != "true" {
		return nil, fmt.Errorf("API returned success=%s, body: %s", result.Success, string(body))
	}

	return &result, nil
}

func (c *Client) GetBillStatus(ctx context.Context, billID string) (*BillStatusResponse, error) {
	endpoint := fmt.Sprintf("%s/bill/status", c.baseURL)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := httpReq.URL.Query()
	q.Set("id", billID)
	httpReq.URL.RawQuery = q.Encode()
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error, status: %d, body: %s", resp.StatusCode, string(body))
	}

	var result BillStatusResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}
