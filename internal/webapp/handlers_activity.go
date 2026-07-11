package webapp

import (
	"net/http"
	"strconv"
	"time"
)

type activityEventResponse struct {
	Type           string    `json:"type"`
	Timestamp      time.Time `json:"timestamp"`
	ActorID        *int64    `json:"actorId"`
	ActorUsername  *string   `json:"actorUsername,omitempty"`
	TargetID       int64     `json:"targetId"`
	TargetUsername *string   `json:"targetUsername,omitempty"`
	Detail         string    `json:"detail"`
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

	ids := make([]int64, 0, len(events)*2)
	for _, e := range events {
		if e.ActorID != nil {
			ids = append(ids, *e.ActorID)
		}
		ids = append(ids, e.TargetID)
	}
	byID := h.usernamesByTelegramID(r.Context(), ids)

	resp := make([]activityEventResponse, 0, len(events))
	for _, e := range events {
		item := activityEventResponse{
			Type:      e.Type,
			Timestamp: e.Timestamp,
			ActorID:   e.ActorID,
			TargetID:  e.TargetID,
			Detail:    e.Detail,
		}
		if e.ActorID != nil {
			item.ActorUsername = byID[*e.ActorID]
		}
		item.TargetUsername = byID[e.TargetID]
		resp = append(resp, item)
	}
	writeJSON(w, http.StatusOK, resp)
}
