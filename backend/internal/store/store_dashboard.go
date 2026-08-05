package store

import (
	"context"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"red-token/internal/domain"
)

type DashboardSummaryData struct {
	Backends        int
	ClientKeys      int
	SocksProxies    int
	HealthyBackends int
	RecentErrors    int
	ActiveClients   int
	RequestGrowth   float64
	ErrorGrowth     float64
	Sparkline       []DashboardSparkPoint
}

type DashboardSparkPoint struct {
	Label    string
	Requests int
}

type DashboardUsageSeriesPoint struct {
	Label        string
	Requests     int
	TrafficBytes int64
	ErrorRate    float64
}

type DashboardActivityData struct {
	Events  []domain.AuditEvent
	Usage   []domain.UsageLog
	Summary []DashboardActivitySummary
}

type DashboardActivitySummary struct {
	Category string
	Count    int
}

func (s *Store) DashboardSummary(ctx context.Context, now time.Time) (DashboardSummaryData, error) {
	backendsCount, err := s.CountBackends(ctx)
	if err != nil {
		return DashboardSummaryData{}, err
	}
	clientCount, err := s.CountClientKeys(ctx)
	if err != nil {
		return DashboardSummaryData{}, err
	}
	proxyCount, err := s.CountSocksProxies(ctx)
	if err != nil {
		return DashboardSummaryData{}, err
	}
	backends, err := s.ListBackends(ctx)
	if err != nil {
		return DashboardSummaryData{}, err
	}

	now = now.UTC()
	currentStart := startOfUTCDay(now).AddDate(0, 0, -6)
	previousStart := currentStart.AddDate(0, 0, -7)
	recentErrorSince := now.Add(-24 * time.Hour)

	logs, err := s.listUsageLogsSince(ctx, previousStart)
	if err != nil {
		return DashboardSummaryData{}, err
	}
	_, usageSeries, err := s.DashboardUsageSeries(ctx, now, "7d")
	if err != nil {
		return DashboardSummaryData{}, err
	}

	var (
		healthyBackends  int
		recentErrors     int
		currentRequests  int
		previousRequests int
		currentErrors    int
		previousErrors   int
	)
	activeClients := make(map[string]struct{})

	for _, backend := range backends {
		if backend.Status == domain.BackendStatusNormal {
			healthyBackends++
		}
	}

	for _, log := range logs {
		if !log.CreatedAt.Before(recentErrorSince) && domain.IsBackendFailureStatus(log.StatusCode) {
			recentErrors++
		}
		if !log.CreatedAt.Before(currentStart) {
			currentRequests++
			if domain.IsBackendFailureStatus(log.StatusCode) {
				currentErrors++
			}
			if key := activityClientKey(log); key != "" {
				activeClients[key] = struct{}{}
			}
			continue
		}
		if !log.CreatedAt.Before(previousStart) {
			previousRequests++
			if domain.IsBackendFailureStatus(log.StatusCode) {
				previousErrors++
			}
		}
	}

	sparkline := make([]DashboardSparkPoint, 0, len(usageSeries))
	for _, point := range usageSeries {
		sparkline = append(sparkline, DashboardSparkPoint{
			Label:    point.Label,
			Requests: point.Requests,
		})
	}

	return DashboardSummaryData{
		Backends:        backendsCount,
		ClientKeys:      clientCount,
		SocksProxies:    proxyCount,
		HealthyBackends: healthyBackends,
		RecentErrors:    recentErrors,
		ActiveClients:   len(activeClients),
		RequestGrowth:   growthPercent(previousRequests, currentRequests),
		ErrorGrowth:     growthPercent(previousErrors, currentErrors),
		Sparkline:       sparkline,
	}, nil
}

func (s *Store) DashboardUsageSeries(ctx context.Context, now time.Time, rangeKey string) (string, []DashboardUsageSeriesPoint, error) {
	now = now.UTC()
	rangeKey = normalizeDashboardRange(rangeKey)

	if rangeKey == "24h" {
		start := now.Truncate(time.Hour).Add(-23 * time.Hour)
		logs, err := s.listUsageLogsSince(ctx, start)
		if err != nil {
			return rangeKey, nil, err
		}

		series := make([]DashboardUsageSeriesPoint, 24)
		failures := make([]int, 24)
		for i := 0; i < 24; i++ {
			bucketStart := start.Add(time.Duration(i) * time.Hour)
			series[i] = DashboardUsageSeriesPoint{
				Label: bucketStart.Format("15:04"),
			}
		}
		for _, log := range logs {
			index := int(log.CreatedAt.Sub(start) / time.Hour)
			if index < 0 || index >= len(series) {
				continue
			}
			series[index].Requests++
			series[index].TrafficBytes += log.RequestBytes + log.ResponseBytes
			if domain.IsBackendFailureStatus(log.StatusCode) {
				failures[index]++
			}
		}
		for i := range series {
			if series[i].Requests > 0 {
				series[i].ErrorRate = float64(failures[i]) / float64(series[i].Requests) * 100
			}
		}
		return rangeKey, series, nil
	}

	days := 7
	if rangeKey == "30d" {
		days = 30
	}
	start := startOfUTCDay(now).AddDate(0, 0, -(days - 1))
	logs, err := s.listUsageLogsSince(ctx, start)
	if err != nil {
		return rangeKey, nil, err
	}

	series := make([]DashboardUsageSeriesPoint, days)
	failures := make([]int, days)
	for i := 0; i < days; i++ {
		bucketStart := start.AddDate(0, 0, i)
		series[i] = DashboardUsageSeriesPoint{
			Label: bucketStart.Format("Jan 2"),
		}
	}
	for _, log := range logs {
		index := int(startOfUTCDay(log.CreatedAt).Sub(start) / (24 * time.Hour))
		if index < 0 || index >= len(series) {
			continue
		}
		series[index].Requests++
		series[index].TrafficBytes += log.RequestBytes + log.ResponseBytes
		if domain.IsBackendFailureStatus(log.StatusCode) {
			failures[index]++
		}
	}
	for i := range series {
		if series[i].Requests > 0 {
			series[i].ErrorRate = float64(failures[i]) / float64(series[i].Requests) * 100
		}
	}
	return rangeKey, series, nil
}

func (s *Store) DashboardRecentActivity(ctx context.Context, limit int) (DashboardActivityData, error) {
	limit = normalizeLimit(limit, 10, 50)
	events, err := s.ListAuditEvents(ctx, limit)
	if err != nil {
		return DashboardActivityData{}, err
	}
	usage, err := s.ListUsageLogsPage(ctx, limit, 0)
	if err != nil {
		return DashboardActivityData{}, err
	}

	errorCount := 0
	for _, entry := range usage {
		if domain.IsBackendFailureStatus(entry.StatusCode) {
			errorCount++
		}
	}

	summary := eventCategorySummary(events)
	if len(summary) == 0 {
		summary = []DashboardActivitySummary{
			{Category: "events", Count: len(events)},
			{Category: "requests", Count: len(usage)},
			{Category: "errors", Count: errorCount},
		}
	}

	return DashboardActivityData{
		Events:  ensureSlice(events),
		Usage:   ensureSlice(usage),
		Summary: summary,
	}, nil
}

func startOfUTCDay(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func growthPercent(previous, current int) float64 {
	if previous <= 0 {
		if current <= 0 {
			return 0
		}
		return 100
	}
	return (float64(current-previous) / float64(previous)) * 100
}

func normalizeDashboardRange(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "24h", "1d":
		return "24h"
	case "30d":
		return "30d"
	default:
		return "7d"
	}
}
