package webapp

import (
	"net/http"
	"time"

	"remnawave-tg-shop-bot/internal/database"
)

type webhookInboxDTO struct {
	ID          int64      `json:"id"`
	EventType   string     `json:"eventType"`
	Status      string     `json:"status"`
	Attempts    int        `json:"attempts"`
	ErrorMsg    *string    `json:"errorMsg"`
	CreatedAt   time.Time  `json:"createdAt"`
	ProcessedAt *time.Time `json:"processedAt"`
}

func toWebhookInboxDTO(wh database.WebhookInbox) webhookInboxDTO {
	return webhookInboxDTO{
		ID: wh.ID, EventType: wh.EventType, Status: wh.Status, Attempts: wh.Attempts,
		ErrorMsg: wh.ErrorMsg, CreatedAt: wh.CreatedAt, ProcessedAt: wh.ProcessedAt,
	}
}

func (h *Handler) handleWebhooksList(w http.ResponseWriter, r *http.Request) {
	limit, offset, page := pagination(r)
	status := r.URL.Query().Get("status")

	items, total, err := h.webhookInboxRepository.FindAllPaginated(r.Context(), status, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dtos := make([]webhookInboxDTO, 0, len(items))
	for _, wh := range items {
		dtos = append(dtos, toWebhookInboxDTO(wh))
	}
	writeJSON(w, http.StatusOK, Page[webhookInboxDTO]{Items: dtos, Total: total, Page: page, Limit: limit})
}

// handleWebhookRetry re-dispatches a single failed webhook via tribute.Client.RetryByID. Returns
// 503 when Tribute webhooks aren't configured on this deployment (there's nothing to retry).
func (h *Handler) handleWebhookRetry(w http.ResponseWriter, r *http.Request) {
	if h.tributeClient == nil {
		writeError(w, http.StatusServiceUnavailable, "tribute webhooks are not enabled on this deployment")
		return
	}
	id, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	if err := h.tributeClient.RetryByID(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
