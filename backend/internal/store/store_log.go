package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"red-token/internal/domain"
)

type UsageLogFilter struct {
	BackendName string
	Model       string
	ClientName  string
	ProxyName   string
	Status      string
	Query       string
	DateFrom    time.Time
	DateTo      time.Time
}

type UsageLogOptions struct {
	Backends   []string
	Models     []string
	ClientKeys []string
	Proxies    []string
}

type UsageLogStats struct {
	Requests       int
	Successes      int
	Failures       int
	AvgDurationMS  float64
	P95DurationMS  int64
	StatusFamilies []StatusFamilyCount
}

type StatusFamilyCount struct {
	Family string
	Count  int
}

func (s *Store) AppendUsageLog(ctx context.Context, log domain.UsageLog) error {
	createdAt := log.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO usage_logs(
			request_id, client_id, client_name, client_token_prefix,
			method, path, query, endpoint, model, backend_id, backend_name, proxy_id, proxy_name,
			attempts, status_code, status_family, duration_ms, error_message, client_ip, user_agent, trace_id,
			request_bytes, response_bytes, input_tokens, output_tokens, input_cache_tokens,
			request_headers_json, request_body_preview, response_headers_json, response_body_preview,
			preview_truncated, is_stream, created_at
		)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		strings.TrimSpace(log.RequestID),
		log.ClientID,
		log.ClientName,
		log.ClientTokenPrefix,
		strings.TrimSpace(log.Method),
		strings.TrimSpace(log.Path),
		strings.TrimSpace(log.Query),
		strings.TrimSpace(log.Endpoint),
		strings.TrimSpace(log.Model),
		log.BackendID,
		log.BackendName,
		log.ProxyID,
		strings.TrimSpace(log.ProxyName),
		log.Attempts,
		log.StatusCode,
		nonEmpty(log.StatusFamily, statusFamily(log.StatusCode)),
		log.DurationMS,
		strings.TrimSpace(log.ErrorMessage),
		strings.TrimSpace(log.ClientIP),
		strings.TrimSpace(log.UserAgent),
		strings.TrimSpace(log.TraceID),
		log.RequestBytes,
		log.ResponseBytes,
		log.InputTokens,
		log.OutputTokens,
		log.InputCacheTokens,
		nonEmpty(log.RequestHeadersJSON, "{}"),
		log.RequestBodyPreview,
		nonEmpty(log.ResponseHeadersJSON, "{}"),
		log.ResponseBodyPreview,
		boolToInt(log.PreviewTruncated),
		boolToInt(log.IsStream),
		formatTime(createdAt.UTC()),
	)
	if err != nil {
		return err
	}

	if err = upsertBackendHourlyModelStats(ctx, tx, log, createdAt); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) CountUsageLogs(ctx context.Context) (int, error) {
	return s.CountUsageLogsFiltered(ctx, UsageLogFilter{})
}

func (s *Store) GetUsageLog(ctx context.Context, id int64) (domain.UsageLog, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, request_id, client_id, client_name, client_token_prefix,
			method, path, query, endpoint, model, backend_id, backend_name, proxy_id, proxy_name,
			attempts, status_code, status_family, duration_ms, error_message, client_ip, user_agent, trace_id,
			request_bytes, response_bytes, input_tokens, output_tokens, input_cache_tokens,
			request_headers_json, request_body_preview, response_headers_json, response_body_preview,
			preview_truncated, is_stream, created_at
		FROM usage_logs
		WHERE id = ?
	`, id)
	return scanUsageLog(row)
}

func (s *Store) UsageLogStats(ctx context.Context, filter UsageLogFilter) (UsageLogStats, error) {
	where, args := usageLogFilterClause(filter)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, request_id, client_id, client_name, client_token_prefix,
			method, path, query, endpoint, model, backend_id, backend_name, proxy_id, proxy_name,
			attempts, status_code, status_family, duration_ms, error_message, client_ip, user_agent, trace_id,
			request_bytes, response_bytes, input_tokens, output_tokens, input_cache_tokens,
			request_headers_json, request_body_preview, response_headers_json, response_body_preview,
			preview_truncated, is_stream, created_at
		FROM usage_logs
	`+where+`
		ORDER BY id DESC
	`, args...)
	if err != nil {
		return UsageLogStats{}, err
	}
	defer rows.Close()

	var (
		logs         []domain.UsageLog
		totalLatency int64
	)
	statusCounts := make(map[string]int)

	for rows.Next() {
		entry, err := scanUsageLog(rows)
		if err != nil {
			return UsageLogStats{}, err
		}
		logs = append(logs, entry)
		totalLatency += entry.DurationMS
		statusCounts[nonEmpty(entry.StatusFamily, statusFamily(entry.StatusCode))]++
	}
	if err := rows.Err(); err != nil {
		return UsageLogStats{}, err
	}

	if len(logs) == 0 {
		return UsageLogStats{StatusFamilies: []StatusFamilyCount{}}, nil
	}

	stats := UsageLogStats{
		Requests:      len(logs),
		AvgDurationMS: float64(totalLatency) / float64(len(logs)),
	}
	durations := make([]int64, 0, len(logs))
	for _, entry := range logs {
		if entry.StatusCode >= 200 && entry.StatusCode < 400 {
			stats.Successes++
		}
		if domain.IsBackendFailureStatus(entry.StatusCode) {
			stats.Failures++
		}
		durations = append(durations, entry.DurationMS)
	}
	slices.Sort(durations)
	p95Index := int(float64(len(durations)-1) * 0.95)
	if p95Index < 0 {
		p95Index = 0
	}
	stats.P95DurationMS = durations[p95Index]
	for _, family := range []string{"2xx", "3xx", "4xx", "5xx", "other"} {
		if count := statusCounts[family]; count > 0 {
			stats.StatusFamilies = append(stats.StatusFamilies, StatusFamilyCount{Family: family, Count: count})
		}
	}
	if stats.StatusFamilies == nil {
		stats.StatusFamilies = []StatusFamilyCount{}
	}
	return stats, nil
}

func (s *Store) CountUsageLogsFiltered(ctx context.Context, filter UsageLogFilter) (int, error) {
	where, args := usageLogFilterClause(filter)
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_logs`+where, args...)
	var total int
	if err := row.Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (s *Store) ListUsageLogsPage(ctx context.Context, limit, offset int) ([]domain.UsageLog, error) {
	return s.ListUsageLogsPageFiltered(ctx, UsageLogFilter{}, limit, offset)
}

func (s *Store) ListUsageLogsPageFiltered(ctx context.Context, filter UsageLogFilter, limit, offset int) ([]domain.UsageLog, error) {
	where, args := usageLogFilterClause(filter)
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, request_id, client_id, client_name, client_token_prefix,
			method, path, query, endpoint, model, backend_id, backend_name, proxy_id, proxy_name,
			attempts, status_code, status_family, duration_ms, error_message, client_ip, user_agent, trace_id,
			request_bytes, response_bytes, input_tokens, output_tokens, input_cache_tokens,
			request_headers_json, request_body_preview, response_headers_json, response_body_preview,
			preview_truncated, is_stream, created_at
		FROM usage_logs
	`+where+`
		ORDER BY id DESC
		LIMIT ? OFFSET ?
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []domain.UsageLog
	for rows.Next() {
		entry, err := scanUsageLog(rows)
		if err != nil {
			return nil, err
		}
		logs = append(logs, entry)
	}
	return logs, rows.Err()
}

func (s *Store) DeleteUsageLog(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM usage_logs WHERE id = ?`, id)
	return err
}

func (s *Store) ClearUsageLogs(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM usage_logs`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) DeleteUsageLogsFiltered(ctx context.Context, filter UsageLogFilter) (int64, error) {
	where, args := usageLogFilterClause(filter)
	result, err := s.db.ExecContext(ctx, `DELETE FROM usage_logs`+where, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) UsageLogOptions(ctx context.Context) (UsageLogOptions, error) {
	backends, err := s.ListBackends(ctx)
	if err != nil {
		return UsageLogOptions{}, err
	}
	clients, err := s.ListClientKeys(ctx)
	if err != nil {
		return UsageLogOptions{}, err
	}
	proxies, err := s.ListSocksProxies(ctx)
	if err != nil {
		return UsageLogOptions{}, err
	}

	backendSet := make(map[string]struct{})
	modelSet := make(map[string]struct{})
	clientSet := make(map[string]struct{})
	proxySet := make(map[string]struct{})

	for _, backend := range backends {
		if name := strings.TrimSpace(backend.Name); name != "" {
			backendSet[name] = struct{}{}
		}
		for _, model := range backend.Models {
			model = strings.TrimSpace(model)
			if model != "" {
				modelSet[model] = struct{}{}
			}
		}
		for clientModel := range backend.ModelMapping {
			clientModel = strings.TrimSpace(clientModel)
			if clientModel != "" {
				modelSet[clientModel] = struct{}{}
			}
		}
	}

	for _, client := range clients {
		if name := strings.TrimSpace(client.Name); name != "" {
			clientSet[name] = struct{}{}
		}
	}
	for _, proxy := range proxies {
		if name := strings.TrimSpace(proxy.Name); name != "" {
			proxySet[name] = struct{}{}
		}
	}

	return UsageLogOptions{
		Backends:   sortedKeys(backendSet),
		Models:     sortedKeys(modelSet),
		ClientKeys: sortedKeys(clientSet),
		Proxies:    sortedKeys(proxySet),
	}, nil
}

func usageLogFilterClause(filter UsageLogFilter) (string, []any) {
	var (
		clauses []string
		args    []any
	)
	if value := strings.TrimSpace(filter.BackendName); value != "" {
		clauses = append(clauses, `backend_name = ?`)
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Model); value != "" {
		clauses = append(clauses, `model = ?`)
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.ClientName); value != "" {
		clauses = append(clauses, `client_name = ?`)
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.ProxyName); value != "" {
		clauses = append(clauses, `proxy_name = ?`)
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Status); value != "" {
		switch strings.ToLower(value) {
		case "2xx", "3xx", "4xx", "5xx":
			clauses = append(clauses, `status_family = ?`)
			args = append(args, value)
		}
	}
	if value := strings.TrimSpace(filter.Query); value != "" {
		like := "%" + strings.ToLower(value) + "%"
		clauses = append(clauses, `(lower(request_id) LIKE ? OR lower(trace_id) LIKE ? OR lower(path) LIKE ? OR lower(client_name) LIKE ? OR lower(model) LIKE ? OR lower(backend_name) LIKE ? OR lower(error_message) LIKE ?)`)
		args = append(args, like, like, like, like, like, like, like)
	}
	if !filter.DateFrom.IsZero() {
		clauses = append(clauses, `created_at >= ?`)
		args = append(args, formatTime(filter.DateFrom.UTC()))
	}
	if !filter.DateTo.IsZero() {
		clauses = append(clauses, `created_at <= ?`)
		args = append(args, formatTime(filter.DateTo.UTC()))
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func sortedKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return []string{}
	}
	items := make([]string, 0, len(set))
	for item := range set {
		items = append(items, item)
	}
	slices.Sort(items)
	return items
}

func scanUsageLog(s scanner) (domain.UsageLog, error) {
	var (
		entry            domain.UsageLog
		createdAt        string
		previewTruncated int
		isStream         int
	)
	err := s.Scan(
		&entry.ID,
		&entry.RequestID,
		&entry.ClientID,
		&entry.ClientName,
		&entry.ClientTokenPrefix,
		&entry.Method,
		&entry.Path,
		&entry.Query,
		&entry.Endpoint,
		&entry.Model,
		&entry.BackendID,
		&entry.BackendName,
		&entry.ProxyID,
		&entry.ProxyName,
		&entry.Attempts,
		&entry.StatusCode,
		&entry.StatusFamily,
		&entry.DurationMS,
		&entry.ErrorMessage,
		&entry.ClientIP,
		&entry.UserAgent,
		&entry.TraceID,
		&entry.RequestBytes,
		&entry.ResponseBytes,
		&entry.InputTokens,
		&entry.OutputTokens,
		&entry.InputCacheTokens,
		&entry.RequestHeadersJSON,
		&entry.RequestBodyPreview,
		&entry.ResponseHeadersJSON,
		&entry.ResponseBodyPreview,
		&previewTruncated,
		&isStream,
		&createdAt,
	)
	if err != nil {
		return domain.UsageLog{}, err
	}
	entry.PreviewTruncated = previewTruncated == 1
	entry.IsStream = isStream == 1
	entry.CreatedAt = parseTime(createdAt)
	return entry, nil
}

func (s *Store) listUsageLogsSince(ctx context.Context, since time.Time) ([]domain.UsageLog, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, request_id, client_id, client_name, client_token_prefix,
			method, path, query, endpoint, model, backend_id, backend_name, proxy_id, proxy_name,
			attempts, status_code, status_family, duration_ms, error_message, client_ip, user_agent, trace_id,
			request_bytes, response_bytes, input_tokens, output_tokens, input_cache_tokens,
			request_headers_json, request_body_preview, response_headers_json, response_body_preview,
			preview_truncated, is_stream, created_at
		FROM usage_logs
		WHERE created_at >= ?
		ORDER BY created_at ASC, id ASC
	`, formatTime(since.UTC()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []domain.UsageLog
	for rows.Next() {
		entry, err := scanUsageLog(rows)
		if err != nil {
			return nil, err
		}
		logs = append(logs, entry)
	}
	return logs, rows.Err()
}

func (s *Store) listUsageLogsByBackendID(ctx context.Context, id int64, limit int) ([]domain.UsageLog, error) {
	return s.listUsageLogsByColumn(ctx, "backend_id", id, limit)
}

func (s *Store) listUsageLogsByClientID(ctx context.Context, id int64, limit int) ([]domain.UsageLog, error) {
	return s.listUsageLogsByColumn(ctx, "client_id", id, limit)
}

func (s *Store) listUsageLogsByProxyID(ctx context.Context, id int64, limit int) ([]domain.UsageLog, error) {
	return s.listUsageLogsByColumn(ctx, "proxy_id", id, limit)
}

func (s *Store) listUsageLogsByColumn(ctx context.Context, column string, id int64, limit int) ([]domain.UsageLog, error) {
	switch column {
	case "backend_id", "client_id", "proxy_id":
	default:
		return nil, fmt.Errorf("unsupported usage log lookup column %q", column)
	}

	limit = normalizeLimit(limit, 10, 100)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, request_id, client_id, client_name, client_token_prefix,
			method, path, query, endpoint, model, backend_id, backend_name, proxy_id, proxy_name,
			attempts, status_code, status_family, duration_ms, error_message, client_ip, user_agent, trace_id,
			request_bytes, response_bytes, input_tokens, output_tokens, input_cache_tokens,
			request_headers_json, request_body_preview, response_headers_json, response_body_preview,
			preview_truncated, is_stream, created_at
		FROM usage_logs
		WHERE `+column+` = ?
		ORDER BY id DESC
		LIMIT ?
	`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []domain.UsageLog
	for rows.Next() {
		entry, err := scanUsageLog(rows)
		if err != nil {
			return nil, err
		}
		logs = append(logs, entry)
	}
	return logs, rows.Err()
}

func statusFamily(statusCode int) string {
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
