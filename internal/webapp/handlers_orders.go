package webapp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"remnawave-tg-shop-bot/internal/database"
)

func (h *Handler) handleOrdersList(w http.ResponseWriter, r *http.Request) {
	limit, offset, page := pagination(r)
	status := r.URL.Query().Get("status")
	invoiceType := r.URL.Query().Get("invoiceType")

	var customerID *int64
	if v := r.URL.Query().Get("customerId"); v != "" {
		id, ok := parseInt64Query(v)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid customerId")
			return
		}
		customerID = &id
	}

	purchases, total, err := h.purchaseRepository.FindAllPaginated(r.Context(), status, invoiceType, customerID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]purchaseDTO, 0, len(purchases))
	for _, p := range purchases {
		items = append(items, toPurchaseDTO(p))
	}
	h.hydrateTelegramIDs(r.Context(), purchases, items)
	writeJSON(w, http.StatusOK, Page[purchaseDTO]{Items: items, Total: total, Page: page, Limit: limit})
}

// hydrateTelegramIDs batch-fetches each purchase's customer and attaches TelegramID to the
// matching DTO, so the admin UI can show/link who made each order without a per-row lookup.
// Purchase only carries the internal customer_id; this is the one list endpoint where a raw
// telegram_id isn't already available on the row.
func (h *Handler) hydrateTelegramIDs(ctx context.Context, purchases []database.Purchase, items []purchaseDTO) {
	if len(purchases) == 0 {
		return
	}
	seen := make(map[int64]bool, len(purchases))
	ids := make([]int64, 0, len(purchases))
	for _, p := range purchases {
		if !seen[p.CustomerID] {
			seen[p.CustomerID] = true
			ids = append(ids, p.CustomerID)
		}
	}
	customers, err := h.customerRepository.FindByIds(ctx, ids)
	if err != nil {
		slog.Warn("failed to hydrate order telegram ids", "error", err)
		return
	}
	byID := make(map[int64]database.Customer, len(customers))
	for _, c := range customers {
		byID[c.ID] = c
	}
	for i, p := range purchases {
		if c, ok := byID[p.CustomerID]; ok {
			tgID := c.TelegramID
			items[i].TelegramID = &tgID
			items[i].Username = c.Username
		}
	}
}

func (h *Handler) handleOrderDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	purchase, err := h.purchaseRepository.FindById(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if purchase == nil {
		writeError(w, http.StatusNotFound, "order not found")
		return
	}
	items := []purchaseDTO{toPurchaseDTO(*purchase)}
	h.hydrateTelegramIDs(r.Context(), []database.Purchase{*purchase}, items)
	writeJSON(w, http.StatusOK, items[0])
}

// handleOrdersExportCSV streams the same filtered query as handleOrdersList as CSV, capped at a
// generous single page size (10k rows) rather than truly streaming the whole table — this admin
// tool is used interactively, not as a data pipeline.
func (h *Handler) handleOrdersExportCSV(w http.ResponseWriter, r *http.Request) {
	const exportLimit = 10000
	status := r.URL.Query().Get("status")
	invoiceType := r.URL.Query().Get("invoiceType")

	var customerID *int64
	if v := r.URL.Query().Get("customerId"); v != "" {
		id, ok := parseInt64Query(v)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid customerId")
			return
		}
		customerID = &id
	}

	purchases, _, err := h.purchaseRepository.FindAllPaginated(r.Context(), status, invoiceType, customerID, exportLimit, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]purchaseDTO, len(purchases))
	for i, p := range purchases {
		items[i] = toPurchaseDTO(p)
	}
	h.hydrateTelegramIDs(r.Context(), purchases, items)

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="orders.csv"`)
	fmt.Fprintln(w, "id,customer_id,telegram_id,amount,currency,month,status,invoice_type,created_at,paid_at,expire_at")
	for i, p := range purchases {
		paidAt := ""
		if p.PaidAt != nil {
			paidAt = p.PaidAt.Format("2006-01-02T15:04:05Z07:00")
		}
		expireAt := ""
		if p.ExpireAt != nil {
			expireAt = p.ExpireAt.Format("2006-01-02T15:04:05Z07:00")
		}
		telegramID := ""
		if tg := items[i].TelegramID; tg != nil {
			telegramID = strconv.FormatInt(*tg, 10)
		}
		fmt.Fprintf(w, "%d,%d,%s,%.2f,%s,%d,%s,%s,%s,%s,%s\n",
			p.ID, p.CustomerID, telegramID, p.Amount, p.Currency, p.Month, p.Status, p.InvoiceType,
			p.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), paidAt, expireAt)
	}
}

func parseInt64Query(v string) (int64, bool) {
	id, err := strconv.ParseInt(v, 10, 64)
	return id, err == nil
}
