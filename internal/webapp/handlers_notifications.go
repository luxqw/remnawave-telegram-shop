package webapp

import (
	"net/http"
	"time"

	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
)

// resendableNotificationTypes are the notification_type values that carry enough information
// (a deterministic template driven purely by customer state) to be safely re-sent from the admin
// panel. "broadcast" and "admin_message" only ever had a status/detail string logged, not the
// actual message text, so there's nothing meaningful to resend for those — a deliberate scope
// limit, not a bug.
var resendableNotificationTypes = map[string]bool{
	"trial_expiring":        true,
	"subscription_expiring": true,
	"traffic_warning":       true,
}

type notificationDTO struct {
	ID                 int64     `json:"id"`
	CreatedAt          time.Time `json:"createdAt"`
	CustomerTelegramID int64     `json:"customerTelegramId"`
	CustomerUsername   *string   `json:"customerUsername,omitempty"`
	NotificationType   string    `json:"notificationType"`
	Status             string    `json:"status"`
	Detail             *string   `json:"detail"`
	ErrorMessage       *string   `json:"errorMessage"`
	Source             string    `json:"source"`
}

func toNotificationDTO(l database.NotificationLog) notificationDTO {
	return notificationDTO{
		ID: l.ID, CreatedAt: l.CreatedAt, CustomerTelegramID: l.CustomerTelegramID,
		NotificationType: l.NotificationType, Status: l.Status, Detail: l.Detail,
		ErrorMessage: l.ErrorMessage, Source: l.Source,
	}
}

// parseNotificationFilter builds a NotificationLogFilter from query params, mirroring
// parseActivityFilter's shape (type/status/customerId/from/to).
func parseNotificationFilter(r *http.Request) database.NotificationLogFilter {
	q := r.URL.Query()
	adminID := config.GetAdminTelegramId()
	filter := database.NotificationLogFilter{
		NotificationType:          q.Get("type"),
		Status:                    q.Get("status"),
		ExcludeCustomerTelegramID: &adminID,
	}
	if v := q.Get("customerId"); v != "" {
		if id, ok := parseInt64Query(v); ok {
			filter.CustomerTelegramID = &id
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
	return filter
}

// handleNotificationList is the full paginated/filterable notification_log page backing the
// admin "Notifications" screen — distinct from the merged Activity feed, which only shows a
// flattened string. This surfaces the structured Status field so a resend button can be
// conditionally shown.
func (h *Handler) handleNotificationList(w http.ResponseWriter, r *http.Request) {
	limit, offset, page := pagination(r)
	filter := parseNotificationFilter(r)

	logs, total, err := h.notificationLogRepository.FindAllPaginated(r.Context(), filter, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ids := make([]int64, 0, len(logs))
	for _, l := range logs {
		ids = append(ids, l.CustomerTelegramID)
	}
	byID := h.usernamesByTelegramID(r.Context(), ids)

	items := make([]notificationDTO, 0, len(logs))
	for _, l := range logs {
		item := toNotificationDTO(l)
		item.CustomerUsername = byID[l.CustomerTelegramID]
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, Page[notificationDTO]{Items: items, Total: total, Page: page, Limit: limit})
}

type notificationStatsResponse struct {
	Sent    int64 `json:"sent"`
	Failed  int64 `json:"failed"`
	Skipped int64 `json:"skipped"`
	Total   int64 `json:"total"`
}

// handleNotificationStats returns delivery-rate counts (sent/failed/skipped) over the trailing
// `days` window (default 7), powering the stat-card row above the notification log table.
func (h *Handler) handleNotificationStats(w http.ResponseWriter, r *http.Request) {
	days := daysParam(r, 7)
	since := time.Now().AddDate(0, 0, -days)

	counts, err := h.notificationLogRepository.CountByStatus(r.Context(), since, config.GetAdminTelegramId())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := notificationStatsResponse{
		Sent:    counts["sent"],
		Failed:  counts["failed"],
		Skipped: counts["skipped"],
	}
	for _, c := range counts {
		resp.Total += c
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleNotificationResend re-triggers a failed notification for the three types whose send
// path is deterministic from customer state (see resendableNotificationTypes). The underlying
// service methods already write their own new notification_log row on completion, so this
// handler must not write a second one.
func (h *Handler) handleNotificationResend(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}

	entry, err := h.notificationLogRepository.FindByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if entry == nil {
		writeError(w, http.StatusNotFound, "notification not found")
		return
	}
	if !resendableNotificationTypes[entry.NotificationType] {
		writeError(w, http.StatusBadRequest, "resend is not supported for notification type \""+entry.NotificationType+"\" (no stored message text to resend)")
		return
	}

	customer, err := h.customerRepository.FindByTelegramId(r.Context(), entry.CustomerTelegramID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if customer == nil {
		writeError(w, http.StatusNotFound, "customer not found")
		return
	}

	switch entry.NotificationType {
	case "traffic_warning":
		err = h.trafficWarningService.ResendForCustomer(r.Context(), *customer)
	default:
		err = h.subscriptionService.ResendNotification(r.Context(), *customer, entry.NotificationType)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}
