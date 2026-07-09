package webapp

import (
	"fmt"
	"net/http"
	"strconv"
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
	writeJSON(w, http.StatusOK, Page[purchaseDTO]{Items: items, Total: total, Page: page, Limit: limit})
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
	writeJSON(w, http.StatusOK, toPurchaseDTO(*purchase))
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

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="orders.csv"`)
	fmt.Fprintln(w, "id,customer_id,amount,currency,month,status,invoice_type,created_at,paid_at,expire_at")
	for _, p := range purchases {
		paidAt := ""
		if p.PaidAt != nil {
			paidAt = p.PaidAt.Format("2006-01-02T15:04:05Z07:00")
		}
		expireAt := ""
		if p.ExpireAt != nil {
			expireAt = p.ExpireAt.Format("2006-01-02T15:04:05Z07:00")
		}
		fmt.Fprintf(w, "%d,%d,%.2f,%s,%d,%s,%s,%s,%s,%s\n",
			p.ID, p.CustomerID, p.Amount, p.Currency, p.Month, p.Status, p.InvoiceType,
			p.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), paidAt, expireAt)
	}
}

func parseInt64Query(v string) (int64, bool) {
	id, err := strconv.ParseInt(v, 10, 64)
	return id, err == nil
}
