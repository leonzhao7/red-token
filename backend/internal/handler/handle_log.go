package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"red-token/internal/domain"
	"red-token/internal/store"
)

type UsageLogHandler struct {
	store *store.Store
}

func NewUsageLogHandler(st *store.Store) *UsageLogHandler {
	return &UsageLogHandler{store: st}
}

func EnsureUsageLogs(values []domain.UsageLog) []domain.UsageLog {
	if values == nil {
		return []domain.UsageLog{}
	}
	return values
}

type dashboardUsageResponse struct {
	Range  string                `json:"range"`
	Series []dashboardUsagePoint `json:"series"`
}

type dashboardUsagePoint struct {
	Label        string  `json:"label"`
	Requests     int     `json:"requests"`
	Successes    int     `json:"successes,omitempty"`
	Failures     int     `json:"failures,omitempty"`
	LatencyMS    int64   `json:"latency_ms,omitempty"`
	TrafficBytes int64   `json:"traffic_bytes,omitempty"`
	ErrorRate    float64 `json:"error_rate"`
}

type dashboardUsageMetrics struct {
	Successes int
	Failures  int
	LatencyMS int64
}

func (h *UsageLogHandler) AppendAttemptUsageLog(ctx context.Context, base domain.UsageLog, attemptStartedAt time.Time) {
	log := base
	log.CreatedAt = attemptStartedAt.UTC()
	log.DurationMS = time.Since(attemptStartedAt).Milliseconds()
	logCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = h.store.AppendUsageLog(logCtx, log)
}

func (h *UsageLogHandler) HandleDashboardUsage(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	rangeKey, series, err := h.store.DashboardUsageSeries(r.Context(), now, r.URL.Query().Get("range"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	logs, err := h.store.ListUsageLogsPage(r.Context(), 5000, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	usageMetrics := buildDashboardUsageMetrics(logs, rangeKey, now)

	responseSeries := make([]dashboardUsagePoint, 0, len(series))
	for _, point := range series {
		metrics := usageMetrics[point.Label]
		responseSeries = append(responseSeries, dashboardUsagePoint{
			Label:        point.Label,
			Requests:     point.Requests,
			Successes:    metrics.Successes,
			Failures:     metrics.Failures,
			LatencyMS:    metrics.LatencyMS,
			TrafficBytes: point.TrafficBytes,
			ErrorRate:    point.ErrorRate,
		})
	}

	writeJSON(w, http.StatusOK, dashboardUsageResponse{
		Range:  rangeKey,
		Series: responseSeries,
	})
}

func (h *UsageLogHandler) HandleListUsageLogs(w http.ResponseWriter, r *http.Request) {
	page, limit := parsePageQuery(r)
	filter, err := usageLogFilterFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	total, err := h.store.CountUsageLogsFiltered(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	logs, err := h.store.ListUsageLogsPageFiltered(r.Context(), filter, limit, pageOffset(page, limit))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pagedResponse(EnsureUsageLogs(logs), total, page, limit))
}

func (h *UsageLogHandler) HandleUsageLogStats(w http.ResponseWriter, r *http.Request) {
	filter, err := usageLogFilterFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	stats, err := h.store.UsageLogStats(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	families := make([]map[string]any, 0, len(stats.StatusFamilies))
	for _, item := range stats.StatusFamilies {
		families = append(families, map[string]any{"family": item.Family, "count": item.Count})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"totals": map[string]any{
			"requests":  stats.Requests,
			"successes": stats.Successes,
			"failures":  stats.Failures,
		},
		"latency": map[string]any{
			"avg_ms": stats.AvgDurationMS,
			"p95_ms": stats.P95DurationMS,
		},
		"status_families": families,
	})
}

func (h *UsageLogHandler) HandleGetUsageLog(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	entry, err := h.store.GetUsageLog(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "usage log not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"overview": map[string]any{
			"request_id":         entry.RequestID,
			"status_code":        entry.StatusCode,
			"backend":            entry.BackendName,
			"model":              entry.Model,
			"input_tokens":       entry.InputTokens,
			"output_tokens":      entry.OutputTokens,
			"input_cache_tokens": entry.InputCacheTokens,
		},
		"request": map[string]any{
			"bytes":        entry.RequestBytes,
			"body_preview": entry.RequestBodyPreview,
			"headers_json": entry.RequestHeadersJSON,
			"method":       entry.Method,
			"path":         entry.Path,
			"query":        entry.Query,
		},
		"response": map[string]any{
			"bytes":         entry.ResponseBytes,
			"body_preview":  entry.ResponseBodyPreview,
			"headers_json":  entry.ResponseHeadersJSON,
			"status_family": NonEmpty(entry.StatusFamily, StatusFamily(entry.StatusCode)),
			"is_stream":     entry.IsStream,
		},
		"metadata": map[string]any{
			"id":                entry.ID,
			"trace_id":          entry.TraceID,
			"proxy_name":        entry.ProxyName,
			"preview_truncated": entry.PreviewTruncated,
			"created_at":        entry.CreatedAt,
		},
		"raw": entry,
	})
}

func (h *UsageLogHandler) HandleUsageLogOptions(w http.ResponseWriter, r *http.Request) {
	options, err := h.store.UsageLogOptions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"backends":    options.Backends,
		"models":      options.Models,
		"client_keys": options.ClientKeys,
		"proxies":     options.Proxies,
	})
}

func (h *UsageLogHandler) HandleClearUsageLogs(w http.ResponseWriter, r *http.Request) {
	filter, err := usageLogFilterFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var (
		deleted  int64
		storeErr error
	)
	if filter == (store.UsageLogFilter{}) {
		deleted, storeErr = h.store.ClearUsageLogs(r.Context())
	} else {
		deleted, storeErr = h.store.DeleteUsageLogsFiltered(r.Context(), filter)
	}
	if storeErr != nil {
		writeError(w, http.StatusInternalServerError, storeErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cleared": true,
		"filter":  filter,
		"deleted": deleted,
	})
}

func usageLogFilterFromRequest(r *http.Request) (store.UsageLogFilter, error) {
	filter := store.UsageLogFilter{
		BackendName: strings.TrimSpace(r.URL.Query().Get("backend")),
		Model:       strings.TrimSpace(r.URL.Query().Get("model")),
		ClientName:  strings.TrimSpace(r.URL.Query().Get("client_key")),
		ProxyName:   strings.TrimSpace(r.URL.Query().Get("proxy")),
	}
	status, err := normalizeUsageLogStatusFilter(r.URL.Query().Get("status"))
	if err != nil {
		return store.UsageLogFilter{}, err
	}
	filter.Status = status
	filter.Query = strings.TrimSpace(r.URL.Query().Get("q"))
	filter.DateFrom = parseTimeQuery(r.URL.Query().Get("date_from"))
	filter.DateTo = parseTimeQuery(r.URL.Query().Get("date_to"))
	return filter, nil
}

func normalizeUsageLogStatusFilter(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return "", nil
	}
	switch normalized {
	case "2xx", "3xx", "4xx", "5xx":
		return normalized, nil
	default:
		return "", fmt.Errorf("invalid usage log status filter %q", value)
	}
}

func buildDashboardUsageMetrics(logs []domain.UsageLog, rangeKey string, now time.Time) map[string]dashboardUsageMetrics {
	start := dashboardUsageRangeStart(now, rangeKey)
	metrics := make(map[string]dashboardUsageMetrics)
	for _, log := range logs {
		createdAt := log.CreatedAt.UTC()
		if createdAt.Before(start) || createdAt.After(now) {
			continue
		}
		key := dashboardUsageLabel(createdAt, rangeKey)
		entry := metrics[key]
		if domain.IsBackendFailureStatus(log.StatusCode) {
			entry.Failures++
		} else {
			entry.Successes++
		}
		entry.LatencyMS += log.DurationMS
		metrics[key] = entry
	}
	return metrics
}

func dashboardUsageRangeStart(now time.Time, rangeKey string) time.Time {
	now = now.UTC()
	if rangeKey == "24h" {
		return now.Truncate(time.Hour).Add(-23 * time.Hour)
	}
	days := 7
	if rangeKey == "30d" {
		days = 30
	}
	return startOfUTCDay(now).AddDate(0, 0, -(days - 1))
}

func dashboardUsageLabel(createdAt time.Time, rangeKey string) string {
	if rangeKey == "24h" {
		return createdAt.UTC().Truncate(time.Hour).Format("15:04")
	}
	return startOfUTCDay(createdAt.UTC()).Format("Jan 2")
}

func CloneResponseForLogging(resp *http.Response) (*http.Response, []byte, int64, string, bool, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		_ = resp.Body.Close()
		return nil, nil, 0, "", false, err
	}
	_ = resp.Body.Close()
	preview, truncated := PreviewText(body, 16*1024)
	cloned := *resp
	cloned.Body = io.NopCloser(bytes.NewReader(body))
	return &cloned, body, int64(len(body)), preview, truncated, nil
}

func ApplyResponseLogFields(log *domain.UsageLog, resp *http.Response, responseBody []byte, responseBytes int64, responsePreview string, truncated bool) {
	log.ResponseHeadersJSON = MarshalHeaders(RedactedHeaders(resp.Header))
	log.IsStream = strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
	log.ResponseBytes = responseBytes
	log.ResponseBodyPreview = responsePreview
	log.PreviewTruncated = log.PreviewTruncated || truncated
	usage := extractNormalizedTokenUsage(resp, responseBody)
	log.InputTokens = usage.InputTokens
	log.OutputTokens = usage.OutputTokens
	log.InputCacheTokens = usage.InputCacheTokens
}

func extractNormalizedTokenUsage(resp *http.Response, responseBody []byte) normalizedTokenUsage {
	if len(responseBody) == 0 {
		return normalizedTokenUsage{}
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(contentType, "text/event-stream") {
		events, err := parseLoggingSSEStream(responseBody)
		if err != nil {
			return normalizedTokenUsage{}
		}
		usage := normalizedTokenUsage{}
		for _, event := range events {
			payload, ok := decodeLoggingSSEJSON(event.Data)
			if !ok {
				continue
			}
			usage = mergeNormalizedTokenUsage(usage, normalizedTokenUsageFromPayload(payload))
		}
		return usage
	}

	var payload map[string]any
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return normalizedTokenUsage{}
	}
	return normalizedTokenUsageFromPayload(payload)
}

func normalizedTokenUsageFromPayload(payload map[string]any) normalizedTokenUsage {
	usage := normalizedTokenUsage{}
	for _, candidate := range []map[string]any{
		usageObject(payload["usage"]),
		usageObject(usageObject(payload["response"])["usage"]),
		usageObject(usageObject(payload["message"])["usage"]),
	} {
		if len(candidate) == 0 {
			continue
		}
		usage = mergeNormalizedTokenUsage(usage, normalizeProviderUsage(candidate))
	}
	return usage
}

func normalizeProviderUsage(usage map[string]any) normalizedTokenUsage {
	inputTokens := int64Value(usage["input_tokens"])
	if inputTokens == 0 {
		inputTokens = int64Value(usage["prompt_tokens"])
	}
	outputTokens := int64Value(usage["output_tokens"])
	if outputTokens == 0 {
		outputTokens = int64Value(usage["completion_tokens"])
	}
	if looksLikeAnthropicUsage(usage) {
		cacheTokens := anthropicCacheTokens(usage)
		return normalizedTokenUsage{
			InputTokens:      inputTokens + cacheTokens,
			OutputTokens:     outputTokens,
			InputCacheTokens: cacheTokens,
		}
	}
	cacheTokens := int64Value(usageObject(usage["input_tokens_details"])["cached_tokens"])
	if cacheTokens == 0 {
		cacheTokens = int64Value(usageObject(usage["prompt_tokens_details"])["cached_tokens"])
	}
	return normalizedTokenUsage{
		InputTokens:      inputTokens,
		OutputTokens:     outputTokens,
		InputCacheTokens: cacheTokens,
	}
}

func looksLikeAnthropicUsage(usage map[string]any) bool {
	return int64Value(usage["cache_creation_input_tokens"]) > 0 ||
		int64Value(usage["cache_read_input_tokens"]) > 0 ||
		len(usageObject(usage["cache_creation"])) > 0 ||
		len(usageObject(usage["cache_read"])) > 0
}

func anthropicCacheTokens(usage map[string]any) int64 {
	cacheCreation := int64Value(usage["cache_creation_input_tokens"])
	if cacheCreation == 0 {
		cacheCreation = sumUsageDetailTokens(usageObject(usage["cache_creation"]))
	}
	cacheRead := int64Value(usage["cache_read_input_tokens"])
	if cacheRead == 0 {
		cacheRead = sumUsageDetailTokens(usageObject(usage["cache_read"]))
	}
	return cacheCreation + cacheRead
}

func sumUsageDetailTokens(values map[string]any) int64 {
	var total int64
	for _, value := range values {
		total += int64Value(value)
	}
	return total
}

func mergeNormalizedTokenUsage(base, next normalizedTokenUsage) normalizedTokenUsage {
	if next.InputTokens > base.InputTokens {
		base.InputTokens = next.InputTokens
	}
	if next.OutputTokens > base.OutputTokens {
		base.OutputTokens = next.OutputTokens
	}
	if next.InputCacheTokens > base.InputCacheTokens {
		base.InputCacheTokens = next.InputCacheTokens
	}
	return base
}

func parseLoggingSSEStream(body []byte) ([]loggingSSEEvent, error) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var (
		events   []loggingSSEEvent
		current  loggingSSEEvent
		dataRows []string
	)
	flush := func() {
		if current.Event == "" && len(dataRows) == 0 {
			return
		}
		current.Data = strings.Join(dataRows, "\n")
		events = append(events, current)
		current = loggingSSEEvent{}
		dataRows = nil
	}

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			current.Event = value
		case "data":
			dataRows = append(dataRows, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	flush()
	return events, nil
}

func decodeLoggingSSEJSON(data string) (map[string]any, bool) {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" || trimmed == "[DONE]" {
		return nil, false
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, false
	}
	if payload == nil {
		return nil, false
	}
	return payload, true
}

func usageObject(value any) map[string]any {
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return map[string]any{}
	}
	return object
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return parsed
		}
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func StatusFamily(statusCode int) string {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return "2xx"
	case statusCode >= 300 && statusCode < 400:
		return "3xx"
	case statusCode >= 400 && statusCode < 500:
		return "4xx"
	case statusCode >= 500 && statusCode < 600:
		return "5xx"
	default:
		return "other"
	}
}

func NonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func PreviewText(data []byte, limit int) (string, bool) {
	if len(data) == 0 {
		return "", false
	}
	if limit <= 0 || len(data) <= limit {
		return string(data), false
	}
	return string(data[:limit]), true
}

func RedactedHeaders(header http.Header) http.Header {
	out := make(http.Header, len(header))
	for key, values := range header {
		lower := strings.ToLower(strings.TrimSpace(key))
		if lower == "authorization" || lower == "api-key" || lower == "x-api-key" || lower == "cookie" {
			out[key] = []string{"[redacted]"}
			continue
		}
		copied := make([]string, len(values))
		copy(copied, values)
		out[key] = copied
	}
	return out
}

func MarshalHeaders(header http.Header) string {
	if len(header) == 0 {
		return "{}"
	}
	data, err := json.Marshal(header)
	if err != nil {
		return "{}"
	}
	return string(data)
}

type normalizedTokenUsage struct {
	InputTokens      int64
	OutputTokens     int64
	InputCacheTokens int64
}

type loggingSSEEvent struct {
	Event string
	Data  string
}

func parseTimeQuery(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func startOfUTCDay(value time.Time) time.Time {
	utc := value.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}
