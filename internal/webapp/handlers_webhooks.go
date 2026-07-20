package webapp

import (
	"net/http"
	"time"

	"remnawave-tg-shop-bot/internal/database"
)

type webhookInboxDTO struct {
	ID          int64      `json:"id"`
	EventType   string     `json:"eventType"`
	Provider    string     `json:"provider"`
	Status      string     `json:"status"`
	Attempts    int        `json:"attempts"`
	ErrorMsg    *string    `json:"errorMsg"`
	CreatedAt   time.Time  `json:"createdAt"`
	ProcessedAt *time.Time `json:"processedAt"`
}

func toWebhookInboxDTO(wh database.WebhookInbox) webhookInboxDTO {
	return webhookInboxDTO{
		ID: wh.ID, EventType: wh.EventType, Provider: wh.Provider, Status: wh.Status, Attempts: wh.Attempts,
		ErrorMsg: wh.ErrorMsg, CreatedAt: wh.CreatedAt, ProcessedAt: wh.ProcessedAt,
	}
}

// webhookInboxDetailDTO extends the list DTO with the raw payload — omitted from the list
// response to keep pages light, but shown in the admin UI's detail modal.
type webhookInboxDetailDTO struct {
	webhookInboxDTO
	Payload string `json:"payload"`
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

// handleWebhookDetail returns a single webhook inbox row including its raw payload, backing the
// admin UI's row-click detail modal.
func (h *Handler) handleWebhookDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	wh, err := h.webhookInboxRepository.FindByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if wh == nil {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}
	writeJSON(w, http.StatusOK, webhookInboxDetailDTO{webhookInboxDTO: toWebhookInboxDTO(*wh), Payload: string(wh.Payload)})
}

// handleWebhookRetry re-dispatches a single failed webhook, routed by the row's own Provider
// field to the matching client's RetryByID — each provider's payload shape is only understood by
// its own client, so retrying via the wrong one would fail to unmarshal.
func (h *Handler) handleWebhookRetry(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	item, err := h.webhookInboxRepository.FindByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if item == nil {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}

	switch item.Provider {
	case "rollypay":
		if h.rollypayClient == nil {
			writeError(w, http.StatusServiceUnavailable, "rollypay webhooks are not enabled on this deployment")
			return
		}
		err = h.rollypayClient.RetryByID(r.Context(), id)
	default:
		if h.tributeClient == nil {
			writeError(w, http.StatusServiceUnavailable, "tribute webhooks are not enabled on this deployment")
			return
		}
		err = h.tributeClient.RetryByID(r.Context(), id)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
