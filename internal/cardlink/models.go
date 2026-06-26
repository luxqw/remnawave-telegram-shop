package cardlink

type CreateBillRequest struct {
	Amount      string `json:"amount"`
	ShopID      string `json:"shop_id"`
	OrderID     string `json:"order_id,omitempty"`
	Description string `json:"description,omitempty"`
	CurrencyIn  string `json:"currency_in,omitempty"`
	TTL         int    `json:"ttl,omitempty"`
	Custom      string `json:"custom,omitempty"`
}

type CreateBillResponse struct {
	Success     string `json:"success"`
	LinkURL     string `json:"link_url"`
	LinkPageURL string `json:"link_page_url"`
	BillID      string `json:"bill_id"`
}

type BillStatusResponse struct {
	ID         string  `json:"id"`
	OrderID    string  `json:"order_id"`
	Status     string  `json:"status"`
	Type       string  `json:"type"`
	Amount     float64 `json:"amount"`
	CurrencyIn string  `json:"currency_in"`
	CreatedAt  string  `json:"created_at"`
	TTL        int     `json:"ttl"`
	Active     bool    `json:"active"`
	Success    bool    `json:"success"`
}

func (r *BillStatusResponse) IsPaid() bool {
	return r.Status == "SUCCESS"
}

func (r *BillStatusResponse) IsFailed() bool {
	return r.Status == "FAIL"
}
