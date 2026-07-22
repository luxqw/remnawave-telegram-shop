package webapp

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"remnawave-tg-shop-bot/internal/adminops"
	"remnawave-tg-shop-bot/internal/config"
)

// handleSystemSettingsGet returns the current effective value of every admin-editable runtime
// setting (decision 13b) — an override if one has been PATCHed in, else the .env default.
func (h *Handler) handleSystemSettingsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, config.RuntimeSettingsSnapshot())
}

// handleSystemSettingsPatch applies a partial update to the runtime settings whitelist. Body is a
// flat {"KEY": "value"} map so the frontend can send only the fields the admin actually changed.
func (h *Handler) handleSystemSettingsPatch(w http.ResponseWriter, r *http.Request) {
	var updates map[string]string
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	snapshot, err := h.ops.SetRuntimeSettings(r.Context(), updates, "webapi")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (h *Handler) handleSystemSync(w http.ResponseWriter, r *http.Request) {
	// Detach from the request context: sync can take a while and shouldn't be cancelled just
	// because the HTTP client's connection closes once the "started" response is sent.
	go h.ops.RunSync(context.WithoutCancel(r.Context()), "webapi")
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) handleFixStrategyPreview(w http.ResponseWriter, r *http.Request) {
	result, err := h.ops.FixStrategyPreview(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// fixStrategyJobStatus is a point-in-time snapshot of one bulk apply run, mirroring
// adminops.BroadcastProgress's snapshot-per-update pattern (safe for concurrent polling: each
// update Stores a fresh value rather than mutating shared state in place).
type fixStrategyJobStatus struct {
	JobID     string                      `json:"jobId"`
	Processed int                         `json:"processed"`
	Total     int                         `json:"total"`
	Updated   int                         `json:"updated"`
	Errored   int                         `json:"errored"`
	Done      bool                        `json:"done"`
	Result    *adminops.FixStrategyResult `json:"result,omitempty"`
	Error     string                      `json:"error,omitempty"`
}

// handleFixStrategyApply starts the bulk apply in the background and returns a job ID
// immediately — a full run can process thousands of customers at ~100ms each, far too slow for a
// single synchronous HTTP request.
func (h *Handler) handleFixStrategyApply(w http.ResponseWriter, r *http.Request) {
	jobID := uuid.NewString()
	h.fixStrategyJobs.Store(jobID, fixStrategyJobStatus{JobID: jobID})

	bgCtx := context.WithoutCancel(r.Context())
	go func() {
		result, err := h.ops.FixStrategyApply(bgCtx, "webapi", func(processed, total, updated, errored int) {
			h.fixStrategyJobs.Store(jobID, fixStrategyJobStatus{
				JobID: jobID, Processed: processed, Total: total, Updated: updated, Errored: errored,
			})
		})
		final := fixStrategyJobStatus{JobID: jobID, Done: true}
		if err != nil {
			final.Error = err.Error()
		} else {
			final.Result = &result
			final.Updated = result.Updated
			final.Total = result.Total
			final.Processed = result.Total
			final.Errored = len(result.Errors)
		}
		h.fixStrategyJobs.Store(jobID, final)
	}()

	writeJSON(w, http.StatusAccepted, fixStrategyJobStatus{JobID: jobID})
}

func (h *Handler) handleFixStrategyStatus(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("jobId")
	v, ok := h.fixStrategyJobs.Load(jobID)
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, v)
}
