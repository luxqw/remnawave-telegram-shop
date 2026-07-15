package webapp

import (
	"net/http"
	"strconv"
	"time"

	"remnawave-tg-shop-bot/internal/database"
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

// toActivityEventResponse maps a database.ActivityEvent to the shared response DTO, without
// username hydration (callers hydrate in batch afterward).
func toActivityEventResponse(e database.ActivityEvent) activityEventResponse {
	return activityEventResponse{
		Type:      e.Type,
		Timestamp: e.Timestamp,
		ActorID:   e.ActorID,
		TargetID:  e.TargetID,
		Detail:    e.Detail,
	}
}

// handleActivityList is the full paginated/filterable activity feed backing the admin "Activity"
// page, mirroring handleAuditList's shape exactly (type/actorId/targetId/from/to filters,
// pagination() helper, Page[...] envelope). handleDashboardActivity above stays untouched — it
// serves the dashboard widget's flat-array, non-paginated contract.
func (h *Handler) handleActivityList(w http.ResponseWriter, r *http.Request) {
	limit, offset, page := pagination(r)
	q := r.URL.Query()

	filter := database.ActivityFilter{
		Type: q.Get("type"),
	}
	if v := q.Get("actorId"); v != "" {
		if id, ok := parseInt64Query(v); ok {
			filter.ActorID = &id
		}
	}
	if v := q.Get("targetId"); v != "" {
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

	events, total, err := h.activityRepository.FindAllPaginated(r.Context(), filter, limit, offset)
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

	items := make([]activityEventResponse, 0, len(events))
	for _, e := range events {
		item := toActivityEventResponse(e)
		if e.ActorID != nil {
			item.ActorUsername = byID[*e.ActorID]
		}
		item.TargetUsername = byID[e.TargetID]
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, Page[activityEventResponse]{Items: items, Total: total, Page: page, Limit: limit})
}
