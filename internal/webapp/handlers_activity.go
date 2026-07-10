package webapp

import (
	"net/http"
	"strconv"
	"time"
)

type activityEventResponse struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	ActorID   *int64    `json:"actorId"`
	TargetID  int64     `json:"targetId"`
	Detail    string    `json:"detail"`
}

// handleDashboardActivity returns the most recent activity feed (signups, completed purchases,
// granted referral bonuses, admin actions) as a flat array — this is a feed, not the full audit
// trail (that's GET /admin/api/audit), so no pagination envelope.
func (h *Handler) handleDashboardActivity(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	events, err := h.activityRepository.FindRecent(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := make([]activityEventResponse, 0, len(events))
	for _, e := range events {
		resp = append(resp, activityEventResponse{
			Type:      e.Type,
			Timestamp: e.Timestamp,
			ActorID:   e.ActorID,
			TargetID:  e.TargetID,
			Detail:    e.Detail,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}
