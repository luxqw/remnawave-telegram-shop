package webapp

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"remnawave-tg-shop-bot/internal/database"
)

type dashboardStatsResponse struct {
	Total       int64 `json:"total"`
	ActivePaid  int64 `json:"activePaid"`
	ActiveTrial int64 `json:"activeTrial"`
	Expired     int64 `json:"expired"`
	NoSub       int64 `json:"noSub"`
}

func (h *Handler) handleDashboardStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.customerRepository.CountStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dashboardStatsResponse{
		Total: stats.Total, ActivePaid: stats.ActivePaid, ActiveTrial: stats.ActiveTrial,
		Expired: stats.Expired, NoSub: stats.NoSub,
	})
}

type dayPoint struct {
	Day   string  `json:"day"`
	Value float64 `json:"value"`
	Count int64   `json:"count"`
}

func daysParam(r *http.Request, def int) int {
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 366 {
			return n
		}
	}
	return def
}

func (h *Handler) handleDashboardRevenue(w http.ResponseWriter, r *http.Request) {
	days := daysParam(r, 30)
	rows, err := h.purchaseRepository.RevenueByDay(r.Context(), days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	byDay := make(map[string]database.RevenueDay, len(rows))
	for _, row := range rows {
		byDay[row.Day.Format("2006-01-02")] = row
	}
	writeJSON(w, http.StatusOK, fillRevenueGaps(byDay, days))
}

// fillRevenueGaps produces one point per day over the window (zero-filled), so the frontend chart
// never has to reason about missing days.
func fillRevenueGaps(byDay map[string]database.RevenueDay, days int) []dayPoint {
	points := make([]dayPoint, 0, days)
	now := time.Now()
	for i := days - 1; i >= 0; i-- {
		day := now.AddDate(0, 0, -i)
		key := day.Format("2006-01-02")
		p := dayPoint{Day: key}
		if row, ok := byDay[key]; ok {
			p.Value = row.Revenue
			p.Count = row.Count
		}
		points = append(points, p)
	}
	return points
}

func (h *Handler) handleDashboardGrowth(w http.ResponseWriter, r *http.Request) {
	days := daysParam(r, 30)
	rows, err := h.customerRepository.NewCustomersByDay(r.Context(), days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	byDay := make(map[string]int64, len(rows))
	for _, row := range rows {
		byDay[row.Day.Format("2006-01-02")] = row.Count
	}
	points := make([]dayPoint, 0, days)
	now := time.Now()
	for i := days - 1; i >= 0; i-- {
		day := now.AddDate(0, 0, -i)
		key := day.Format("2006-01-02")
		points = append(points, dayPoint{Day: key, Value: float64(byDay[key]), Count: byDay[key]})
	}
	writeJSON(w, http.StatusOK, points)
}

type dashboardReferralsResponse struct {
	Total       int     `json:"total"`
	Granted     int     `json:"granted"`
	ConversionP float64 `json:"conversionPercent"`
}

func (h *Handler) handleDashboardReferrals(w http.ResponseWriter, r *http.Request) {
	total, err := h.referralRepository.CountAll(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	granted, err := h.referralRepository.CountAllGranted(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := dashboardReferralsResponse{Total: total, Granted: granted}
	if total > 0 {
		resp.ConversionP = float64(granted) / float64(total) * 100
	}
	writeJSON(w, http.StatusOK, resp)
}

type headerStatsResponse struct {
	ActiveSubscriptions int64   `json:"activeSubscriptions"`
	MRR30d              float64 `json:"mrr30d"`
	MRRCurrency         string  `json:"mrrCurrency"`
	ExpiringToday       int64   `json:"expiringToday"`
}

// handleDashboardHeaderStats backs the Topbar's compact metrics strip: active subscriptions
// (reuses CustomerRepository.CountStats — no duplicate SQL), a trailing-30-day paid-revenue
// approximation labeled explicitly as such (not true recurring MRR — there's no per-customer
// recurring-amount tracking to compute that from), and today's expiration count (deliberately not
// labeled "churn" — see CountExpiringToday's doc comment).
func (h *Handler) handleDashboardHeaderStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.customerRepository.CountStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	mrr, err := h.purchaseRepository.RevenueSince(r.Context(), time.Now().AddDate(0, 0, -30))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	expiringToday, err := h.customerRepository.CountExpiringToday(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, headerStatsResponse{
		ActiveSubscriptions: stats.ActivePaid + stats.ActiveTrial,
		MRR30d:              mrr,
		MRRCurrency:         "RUB", // matches the single-currency assumption already baked into RevenueByDay/the dashboard revenue chart
		ExpiringToday:       expiringToday,
	})
}

type dashboardHealthResponse struct {
	Status    string `json:"status"`
	DB        string `json:"db"`
	Remnawave string `json:"remnawave"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
	Time      string `json:"time"`
}

// handleDashboardHealth mirrors the logic of fullHealthHandler in cmd/app/main.go (same pool +
// Remnawave ping checks) so the admin webapp doesn't need to scrape its own /healthcheck.
func (h *Handler) handleDashboardHealth(w http.ResponseWriter, r *http.Request) {
	resp := dashboardHealthResponse{
		Status: "ok", DB: "ok", Remnawave: "ok",
		Version: h.build.Version, Commit: h.build.Commit, BuildDate: h.build.BuildDate,
		Time: time.Now().Format(time.RFC3339),
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := h.pool.Ping(dbCtx); err != nil {
		resp.Status = "fail"
		resp.DB = "error: " + err.Error()
	}

	rwCtx, rwCancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer rwCancel()
	if err := h.remnawaveClient.Ping(rwCtx); err != nil {
		resp.Status = "fail"
		resp.Remnawave = "error: " + err.Error()
	}

	status := http.StatusOK
	if resp.Status != "ok" {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, resp)
}
