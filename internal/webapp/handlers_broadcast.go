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

type broadcastTestRequest struct {
	Text string `json:"text"`
}

// handleBroadcastTest sends the draft text only to the configured admin telegram ID — a preview
// send before committing to a real broadcast, preserving the bot's old "🧪 Только мне" button now
// that the bot-native broadcast dialog has been removed.
func (h *Handler) handleBroadcastTest(w http.ResponseWriter, r *http.Request) {
	var req broadcastTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	if err := h.ops.SendTestBroadcast(r.Context(), req.Text, "webapi"); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
