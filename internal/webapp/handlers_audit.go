package webapp

import (
	"net/http"
	"time"

	"remnawave-tg-shop-bot/internal/database"
)

func (h *Handler) handleAuditList(w http.ResponseWriter, r *http.Request) {
	limit, offset, page := pagination(r)
	q := r.URL.Query()

	filter := database.AdminAuditLogFilter{
		Action:  q.Get("action"),
		Outcome: q.Get("outcome"),
	}
	if v := q.Get("admin"); v != "" {
		if id, ok := parseInt64Query(v); ok {
			filter.AdminTelegramID = &id
		}
	}
	if v := q.Get("target"); v != "" {
		if id, ok := parseInt64Query(v); ok {
			filter.TargetID = &id
		}
	}
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			filter.From = &t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			end := t.Add(24 * time.Hour)
			filter.To = &end
		}
	}

	entries, total, err := h.auditLogRepository.FindAllPaginated(r.Context(), filter, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]auditLogDTO, 0, len(entries))
	for _, e := range entries {
		items = append(items, toAuditLogDTO(e))
	}
	writeJSON(w, http.StatusOK, Page[auditLogDTO]{Items: items, Total: total, Page: page, Limit: limit})
}
