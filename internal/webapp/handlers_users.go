package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"remnawave-tg-shop-bot/internal/adminops"
	"remnawave-tg-shop-bot/internal/database"
)

// pathInt64 extracts an int64 path parameter, writing a 400 response and returning ok=false when
// missing or malformed.
func pathInt64(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	raw := r.PathValue(name)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return id, true
}

type customerDTO struct {
	ID               int64      `json:"id"`
	TelegramID       int64      `json:"telegramId"`
	ExpireAt         *time.Time `json:"expireAt"`
	CreatedAt        time.Time  `json:"createdAt"`
	SubscriptionLink *string    `json:"subscriptionLink"`
	Language         string     `json:"language"`
	IsTrial          bool       `json:"isTrial"`
	Username         *string    `json:"username,omitempty"`
}

func toCustomerDTO(c database.Customer) customerDTO {
	return customerDTO{
		ID: c.ID, TelegramID: c.TelegramID, ExpireAt: c.ExpireAt, CreatedAt: c.CreatedAt,
		SubscriptionLink: c.SubscriptionLink, Language: c.Language, IsTrial: c.IsTrial,
		Username: c.Username,
	}
}

func (h *Handler) handleUsersList(w http.ResponseWriter, r *http.Request) {
	limit, offset, page := pagination(r)
	filter := r.URL.Query().Get("filter")
	search := r.URL.Query().Get("search")

	customers, total, err := h.customerRepository.FindAllPaginated(r.Context(), filter, search, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]customerDTO, 0, len(customers))
	for _, c := range customers {
		items = append(items, toCustomerDTO(c))
	}
	writeJSON(w, http.StatusOK, Page[customerDTO]{Items: items, Total: total, Page: page, Limit: limit})
}

type remnawaveUserDTO struct {
	UUID                 string    `json:"uuid"`
	Status               string    `json:"status"`
	TrafficLimitGB       int       `json:"trafficLimitGb"`
	TrafficLimitStrategy string    `json:"trafficLimitStrategy"`
	ExpireAt             time.Time `json:"expireAt"`
	SubscriptionURL      string    `json:"subscriptionUrl"`
}

type userDetailResponse struct {
	Customer  customerDTO       `json:"customer"`
	Remnawave *remnawaveUserDTO `json:"remnawave,omitempty"`
	RWError   string            `json:"remnawaveError,omitempty"`
}

// handleUserDetail mirrors sendUserCard/AdminUserCommandHandler: the bot DB record plus a
// best-effort live Remnawave lookup (a Remnawave error is surfaced in the response, not a hard
// failure — the bot record alone is still useful).
func (h *Handler) handleUserDetail(w http.ResponseWriter, r *http.Request) {
	targetID, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	customer, err := h.customerRepository.FindByTelegramId(r.Context(), targetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if customer == nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	resp := userDetailResponse{Customer: toCustomerDTO(*customer)}
	rwUsers, err := h.remnawaveClient.GetUsersByTelegramID(r.Context(), targetID)
	switch {
	case err != nil:
		resp.RWError = err.Error()
	case len(rwUsers) == 0:
		resp.RWError = "not found in remnawave"
	default:
		u := rwUsers[0]
		resp.Remnawave = &remnawaveUserDTO{
			UUID: u.UUID.String(), Status: u.Status,
			TrafficLimitGB: u.TrafficLimitBytes / bytesInGB(), TrafficLimitStrategy: u.TrafficLimitStrategy,
			ExpireAt: u.ExpireAt, SubscriptionURL: u.SubscriptionUrl,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func bytesInGB() int { return 1073741824 }

type purchaseDTO struct {
	ID          int64      `json:"id"`
	Amount      float64    `json:"amount"`
	Currency    string     `json:"currency"`
	Month       int        `json:"month"`
	Status      string     `json:"status"`
	InvoiceType string     `json:"invoiceType"`
	CreatedAt   time.Time  `json:"createdAt"`
	PaidAt      *time.Time `json:"paidAt"`
	ExpireAt    *time.Time `json:"expireAt"`
	TelegramID  *int64     `json:"telegramId,omitempty"`
	Username    *string    `json:"username,omitempty"`
}

func toPurchaseDTO(p database.Purchase) purchaseDTO {
	return purchaseDTO{
		ID: p.ID, Amount: p.Amount, Currency: p.Currency, Month: p.Month,
		Status: string(p.Status), InvoiceType: string(p.InvoiceType),
		CreatedAt: p.CreatedAt, PaidAt: p.PaidAt, ExpireAt: p.ExpireAt,
	}
}

func (h *Handler) handleUserOrders(w http.ResponseWriter, r *http.Request) {
	targetID, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	customer, err := h.customerRepository.FindByTelegramId(r.Context(), targetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if customer == nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	limit, offset, page := pagination(r)
	purchases, total, err := h.purchaseRepository.FindByCustomerID(r.Context(), customer.ID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]purchaseDTO, 0, len(purchases))
	for _, p := range purchases {
		items = append(items, toPurchaseDTO(p))
	}
	writeJSON(w, http.StatusOK, Page[purchaseDTO]{Items: items, Total: total, Page: page, Limit: limit})
}

type auditLogDTO struct {
	ID               int64     `json:"id"`
	CreatedAt        time.Time `json:"createdAt"`
	AdminTelegramID  int64     `json:"adminTelegramId"`
	AdminUsername    *string   `json:"adminUsername,omitempty"`
	Action           string    `json:"action"`
	TargetTelegramID int64     `json:"targetTelegramId"`
	TargetUsername   *string   `json:"targetUsername,omitempty"`
	ParamInt         *int      `json:"paramInt"`
	ParamText        *string   `json:"paramText"`
	Outcome          string    `json:"outcome"`
	ErrorMessage     *string   `json:"errorMessage"`
	Source           string    `json:"source"`
}

func toAuditLogDTO(l database.AdminAuditLog) auditLogDTO {
	return auditLogDTO{
		ID: l.ID, CreatedAt: l.CreatedAt, AdminTelegramID: l.AdminTelegramID, Action: l.Action,
		TargetTelegramID: l.TargetTelegramID, ParamInt: l.ParamInt, ParamText: l.ParamText, Outcome: l.Outcome,
		ErrorMessage: l.ErrorMessage, Source: l.Source,
	}
}

// hydrateAuditUsernames attaches admin/target usernames to a page of audit log DTOs, mirroring
// hydrateTelegramIDs in handlers_orders.go — one batch lookup instead of a per-row query.
func (h *Handler) hydrateAuditUsernames(ctx context.Context, items []auditLogDTO) {
	if len(items) == 0 {
		return
	}
	ids := make([]int64, 0, len(items)*2)
	for _, e := range items {
		ids = append(ids, e.AdminTelegramID, e.TargetTelegramID)
	}
	byID := h.usernamesByTelegramID(ctx, ids)
	for i := range items {
		items[i].AdminUsername = byID[items[i].AdminTelegramID]
		items[i].TargetUsername = byID[items[i].TargetTelegramID]
	}
}

func (h *Handler) handleUserAudit(w http.ResponseWriter, r *http.Request) {
	targetID, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	entries, err := h.auditLogRepository.FindRecentByTarget(r.Context(), targetID, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]auditLogDTO, 0, len(entries))
	for _, e := range entries {
		items = append(items, toAuditLogDTO(e))
	}
	h.hydrateAuditUsernames(r.Context(), items)
	writeJSON(w, http.StatusOK, items)
}

type referralDTO struct {
	ID               int64     `json:"id"`
	ReferrerID       int64     `json:"referrerId"`
	ReferrerUsername *string   `json:"referrerUsername,omitempty"`
	RefereeID        int64     `json:"refereeId"`
	RefereeUsername  *string   `json:"refereeUsername,omitempty"`
	UsedAt           time.Time `json:"usedAt"`
	BonusGranted     bool      `json:"bonusGranted"`
}

func toReferralDTO(r database.Referral) referralDTO {
	return referralDTO{ID: r.ID, ReferrerID: r.ReferrerID, RefereeID: r.RefereeID, UsedAt: r.UsedAt, BonusGranted: r.BonusGranted}
}

// hydrateReferralUsernames attaches referrer/referee usernames to a page of referral DTOs.
func (h *Handler) hydrateReferralUsernames(ctx context.Context, items []referralDTO) {
	if len(items) == 0 {
		return
	}
	ids := make([]int64, 0, len(items)*2)
	for _, ref := range items {
		ids = append(ids, ref.ReferrerID, ref.RefereeID)
	}
	byID := h.usernamesByTelegramID(ctx, ids)
	for i := range items {
		items[i].ReferrerUsername = byID[items[i].ReferrerID]
		items[i].RefereeUsername = byID[items[i].RefereeID]
	}
}

func (h *Handler) handleUserReferrals(w http.ResponseWriter, r *http.Request) {
	targetID, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	refs, err := h.referralRepository.FindByReferrer(r.Context(), targetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]referralDTO, 0, len(refs))
	for _, ref := range refs {
		items = append(items, toReferralDTO(ref))
	}
	h.hydrateReferralUsernames(r.Context(), items)
	writeJSON(w, http.StatusOK, items)
}

// --- Mutating actions: every one of these calls into adminops.Service, never the Remnawave
// client directly, so the "re-fetch live state before writing" rule holds structurally. ---

type topupPreviewRequest struct {
	GB int `json:"gb"`
}

type topupPreviewResponse struct {
	NewLimitGB int64 `json:"newLimitGb"`
}

func (h *Handler) handleUserTopupPreview(w http.ResponseWriter, r *http.Request) {
	targetID, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	var req topupPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.GB == 0 {
		writeError(w, http.StatusBadRequest, "gb must be a non-zero integer")
		return
	}
	newLimit, err := h.ops.PreviewTopup(r.Context(), targetID, req.GB)
	if err != nil {
		writeOpsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, topupPreviewResponse{NewLimitGB: newLimit / int64(bytesInGB())})
}

type topupRequest struct {
	GB int `json:"gb"`
}

func (h *Handler) handleUserTopup(w http.ResponseWriter, r *http.Request) {
	targetID, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	var req topupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.GB == 0 {
		writeError(w, http.StatusBadRequest, "gb must be a non-zero integer")
		return
	}
	result, err := h.ops.Topup(r.Context(), targetID, req.GB, "webapi")
	if err != nil {
		writeOpsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) handleUserTopupEnroll(w http.ResponseWriter, r *http.Request) {
	targetID, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	result, err := h.ops.TopupEnroll(r.Context(), targetID, "webapi")
	if err != nil {
		writeOpsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) handleUserResetDevices(w http.ResponseWriter, r *http.Request) {
	targetID, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	if err := h.ops.ResetDevices(r.Context(), targetID, "webapi"); err != nil {
		writeOpsError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleUserResetTraffic(w http.ResponseWriter, r *http.Request) {
	targetID, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	result, err := h.ops.ResetTraffic(r.Context(), targetID, "webapi")
	if err != nil {
		writeOpsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type statusRequest struct {
	Status string `json:"status"`
}

func (h *Handler) handleUserStatus(w http.ResponseWriter, r *http.Request) {
	targetID, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	var req statusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Status != "ACTIVE" && req.Status != "DISABLED") {
		writeError(w, http.StatusBadRequest, `status must be "ACTIVE" or "DISABLED"`)
		return
	}
	result, err := h.ops.SetStatus(r.Context(), targetID, req.Status, "webapi")
	if err != nil {
		writeOpsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type extendRequest struct {
	Days int `json:"days"`
}

func (h *Handler) handleUserExtend(w http.ResponseWriter, r *http.Request) {
	targetID, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	var req extendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Days <= 0 {
		writeError(w, http.StatusBadRequest, "days must be a positive integer")
		return
	}
	result, err := h.ops.Extend(r.Context(), targetID, req.Days, "webapi")
	if err != nil {
		writeOpsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type trialRequest struct {
	IsTrial bool `json:"isTrial"`
}

func (h *Handler) handleUserTrial(w http.ResponseWriter, r *http.Request) {
	targetID, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	var req trialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := h.ops.SetTrial(r.Context(), targetID, req.IsTrial, "webapi"); err != nil {
		writeOpsError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type sendMessageRequest struct {
	Text string `json:"text"`
}

func (h *Handler) handleUserSendMessage(w http.ResponseWriter, r *http.Request) {
	targetID, ok := pathInt64(w, r, "id")
	if !ok {
		return
	}
	var req sendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := h.ops.SendMessage(r.Context(), targetID, req.Text, "webapi"); err != nil {
		writeOpsError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeOpsError maps adminops sentinel errors to the appropriate HTTP status; anything
// unrecognized is a 500 (already audit-logged by adminops itself regardless of outcome).
func writeOpsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, adminops.ErrRemnawaveUserNotFound), errors.Is(err, adminops.ErrCustomerNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, adminops.ErrNegativeLimit):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, adminops.ErrFixStrategyInProgress):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, adminops.ErrEmptyMessage):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
