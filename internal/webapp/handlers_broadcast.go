package webapp

import (
	"encoding/json"
	"net/http"
)

type broadcastPreviewRequest struct {
	Segment string `json:"segment"`
}

type broadcastPreviewResponse struct {
	RecipientCount int `json:"recipientCount"`
}

func (h *Handler) handleBroadcastPreview(w http.ResponseWriter, r *http.Request) {
	var req broadcastPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	count, err := h.ops.PreviewBroadcast(r.Context(), req.Segment)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, broadcastPreviewResponse{RecipientCount: count})
}

type broadcastSendRequest struct {
	Text    string `json:"text"`
	Segment string `json:"segment"`
}

type broadcastSendResponse struct {
	JobID string `json:"jobId"`
}

func (h *Handler) handleBroadcastSend(w http.ResponseWriter, r *http.Request) {
	var req broadcastSendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	jobID, err := h.ops.RunBroadcast(r.Context(), req.Text, req.Segment, "webapi")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, broadcastSendResponse{JobID: jobID})
}

func (h *Handler) handleBroadcastStatus(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("jobId")
	progress, ok := h.ops.BroadcastStatus(jobID)
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, progress)
}
