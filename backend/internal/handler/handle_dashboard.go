package handler

import (
	"net/http"
	"time"

	"red-token/internal/domain"
	"red-token/internal/store"
)

type DashboardHandler struct {
	store          *store.Store
	backendHandler *BackendHandler
}

func NewDashboardHandler(st *store.Store, backendHandler *BackendHandler) *DashboardHandler {
	return &DashboardHandler{store: st, backendHandler: backendHandler}
}

type overviewResponse struct {
	Backends     []BackendView       `json:"backends"`
	SocksProxies int                 `json:"socks_proxies"`
	ClientKeys   int                 `json:"client_keys"`
	Events       []domain.AuditEvent `json:"events"`
}

type dashboardSummaryResponse struct {
	Cards     map[string]dashboardCard `json:"cards,omitempty"`
	Counts    dashboardSummaryCounts   `json:"counts"`
	Growth    dashboardSummaryGrowth   `json:"growth"`
	Status    dashboardSummaryStatus   `json:"status"`
	Sparkline []dashboardSparkPoint    `json:"sparkline"`
}

type dashboardCard struct {
	Count     int `json:"count"`
	Enabled   int `json:"enabled,omitempty"`
	Successes int `json:"successes,omitempty"`
	Failures  int `json:"failures,omitempty"`
}

type dashboardSummaryCounts struct {
	Backends     int `json:"backends"`
	ClientKeys   int `json:"client_keys"`
	SocksProxies int `json:"socks_proxies"`
}

type dashboardSummaryGrowth struct {
	Requests float64 `json:"requests"`
	Errors   float64 `json:"errors"`
}

type dashboardSummaryStatus struct {
	HealthyBackends int `json:"healthy_backends"`
	RecentErrors    int `json:"recent_errors"`
	ActiveClients   int `json:"active_clients"`
}

type dashboardSparkPoint struct {
	Label    string `json:"label"`
	Requests int    `json:"requests"`
}

type dashboardActivityResponse struct {
	Events    []domain.AuditEvent        `json:"events"`
	Usage     []domain.UsageLog          `json:"usage"`
	UsageLogs []domain.UsageLog          `json:"usage_logs"`
	Summary   []dashboardActivitySummary `json:"summary"`
}

type dashboardActivitySummary struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

func (a *DashboardHandler) HandleOverview(w http.ResponseWriter, r *http.Request) {
	backends, err := a.store.ListBackends(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	clients, err := a.store.ListClientKeys(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	proxies, err := a.store.ListSocksProxies(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	events, err := a.store.ListAuditEvents(r.Context(), 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	stats, err := a.store.BackendRequestStatsSince(r.Context(), time.Now().UTC().Add(-30*time.Minute))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	summaries, err := a.backendHandler.BackendUsageSummaryMap(r.Context(), backends)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, overviewResponse{
		Backends:     EnsureBackendViews(BuildBackendViews(backends, summaries, stats, map[int64]store.BackendHourlyStats{})),
		SocksProxies: len(proxies),
		ClientKeys:   len(clients),
		Events:       EnsureAuditEvents(events),
	})
}

func (a *DashboardHandler) HandleDashboardSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := a.store.DashboardSummary(r.Context(), time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	sparkline := make([]dashboardSparkPoint, 0, len(summary.Sparkline))
	for _, point := range summary.Sparkline {
		sparkline = append(sparkline, dashboardSparkPoint{
			Label:    point.Label,
			Requests: point.Requests,
		})
	}

	writeJSON(w, http.StatusOK, dashboardSummaryResponse{
		Cards: buildDashboardCards(summary),
		Counts: dashboardSummaryCounts{
			Backends:     summary.Backends,
			ClientKeys:   summary.ClientKeys,
			SocksProxies: summary.SocksProxies,
		},
		Growth: dashboardSummaryGrowth{
			Requests: summary.RequestGrowth,
			Errors:   summary.ErrorGrowth,
		},
		Status: dashboardSummaryStatus{
			HealthyBackends: summary.HealthyBackends,
			RecentErrors:    summary.RecentErrors,
			ActiveClients:   summary.ActiveClients,
		},
		Sparkline: sparkline,
	})
}

func (a *DashboardHandler) HandleDashboardActivity(w http.ResponseWriter, r *http.Request) {
	activity, err := a.store.DashboardRecentActivity(r.Context(), 10)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	summary := make([]dashboardActivitySummary, 0, len(activity.Summary))
	for _, item := range activity.Summary {
		summary = append(summary, dashboardActivitySummary{
			Category: item.Category,
			Count:    item.Count,
		})
	}

	writeJSON(w, http.StatusOK, dashboardActivityResponse{
		Events:    EnsureAuditEvents(activity.Events),
		Usage:     EnsureUsageLogs(activity.Usage),
		UsageLogs: EnsureUsageLogs(activity.Usage),
		Summary:   summary,
	})
}

func buildDashboardCards(summary store.DashboardSummaryData) map[string]dashboardCard {
	return map[string]dashboardCard{
		"backends": {
			Count:    summary.Backends,
			Enabled:  summary.HealthyBackends,
			Failures: summary.RecentErrors,
		},
		"client_keys": {
			Count:   summary.ClientKeys,
			Enabled: summary.ActiveClients,
		},
		"proxies": {
			Count: summary.SocksProxies,
		},
	}
}
