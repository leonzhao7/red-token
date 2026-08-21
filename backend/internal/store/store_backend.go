package store

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"red-token/internal/domain"
)

type BackendRequestStats struct {
	Successes int
	Failures  int
}

type BackendHourlyStats struct {
	Requests int
	Failures int
}

type BackendHourlyModelStatsFilter struct {
	BackendName string
	Model       string
	StartHour   time.Time
	EndHour     time.Time
}

type BackendRef struct {
	ID   int64
	Name string
}

type BackendHourlyModelStatsRow struct {
	BackendID               int64
	BackendName             string
	Model                   string
	HourStart               time.Time
	Successes               int
	Failures                int
	SuccessDurationMSSum    int64
	SuccessRequestBytes     int64
	SuccessResponseBytes    int64
	SuccessInputTokens      int64
	SuccessOutputTokens     int64
	SuccessInputCacheTokens int64
}

type BackendHourlyModelStatsResult struct {
	Rows       []BackendHourlyModelStatsRow
	Backends   []BackendRef
	Models     []string
	RangeStart *time.Time
	RangeEnd   *time.Time
}

type BackendUsageSummary struct {
	RequestCount int
	AvgLatencyMS float64
	LastUsedAt   time.Time
}

type BackendDetailData struct {
	Backend domain.Backend
	Usage   []domain.UsageLog
	Events  []domain.AuditEvent
}

type BackendPatch struct {
	Name                   *string
	Protocol               *string
	BaseURL                *string
	APIKeys                *[]domain.BackendAPIKey
	ConsoleURL             *string
	Tags                   *[]string
	ConsoleUsername        *string
	ConsolePassword        *string
	ConsoleCheckinWorkflow *string
	ManualCheckin          *bool
	Frozen                 *bool
	ConsoleCookie          *string
	ConsoleHeaders         *map[string]string
	ConsoleRefreshToken    *string
	ConsoleAccountJSON     *string
	Notes                  *string
	ProxyID                *int64
	Status                 *string
	ResetRuntimeState      bool
	Weight                 *int
}

func (s *Store) ListBackends(ctx context.Context) ([]domain.Backend, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			b.id, b.name, b.protocol, b.backend_type, b.base_url, b.api_key, b.api_keys_json, b.console_url, b.tag_list, b.console_username, b.console_password, b.new_api_refresh, b.console_authorization, b.console_checkin_path, b.console_checkin_workflow_id, b.manual_checkin, b.frozen, b.channel_url, b.console_cookie, b.console_headers_json, b.console_refresh_token, b.console_account_json, b.console_pricing_json, b.notes, b.proxy_id, b.status, b.consecutive_failures, b.recover_at, b.weight, b.model_list, b.model_mapping, b.endpoint_list, b.created_at, b.updated_at,
			p.id, p.name, p.address, p.username, p.password, p.enabled, p.created_at, p.updated_at
		FROM backends b
		LEFT JOIN socks_proxies p ON p.id = b.proxy_id
		ORDER BY b.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var backends []domain.Backend
	for rows.Next() {
		backend, err := scanBackend(rows)
		if err != nil {
			return nil, err
		}
		backends = append(backends, backend)
	}
	return backends, rows.Err()
}

func (s *Store) CountBackends(ctx context.Context) (int, error) {
	return countRows(ctx, s.db, "backends")
}

func (s *Store) BackendDetail(ctx context.Context, id int64, limit int) (BackendDetailData, error) {
	backend, err := s.GetBackend(ctx, id)
	if err != nil {
		return BackendDetailData{}, err
	}
	usage, err := s.listUsageLogsByBackendID(ctx, id, limit)
	if err != nil {
		return BackendDetailData{}, err
	}
	events, err := s.listAuditEventsByBackendName(ctx, backend.Name, limit)
	if err != nil {
		return BackendDetailData{}, err
	}
	return BackendDetailData{
		Backend: backend,
		Usage:   ensureSlice(usage),
		Events:  ensureSlice(events),
	}, nil
}

func (s *Store) BackendRequestStatsSince(ctx context.Context, since time.Time) (map[int64]BackendRequestStats, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			backend_id,
			SUM(CASE WHEN status_code >= 200 AND status_code < 300 THEN 1 ELSE 0 END) AS successes,
			SUM(CASE WHEN status_code > 0 AND (status_code < 200 OR status_code >= 300) THEN 1 ELSE 0 END) AS failures
		FROM usage_logs
		WHERE backend_id > 0 AND created_at >= ?
		GROUP BY backend_id
	`, formatTime(since.UTC()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int64]BackendRequestStats)
	for rows.Next() {
		var (
			backendID int64
			stats     BackendRequestStats
		)
		if err := rows.Scan(&backendID, &stats.Successes, &stats.Failures); err != nil {
			return nil, err
		}
		out[backendID] = stats
	}
	return out, rows.Err()
}

func (s *Store) BackendHourlyStatsByIDs(ctx context.Context, ids []int64, since time.Time) (map[int64]BackendHourlyStats, error) {
	if len(ids) == 0 {
		return map[int64]BackendHourlyStats{}, nil
	}

	query := `SELECT backend_id, status_code FROM usage_logs WHERE backend_id IN (` + placeholders(len(ids)) + `) AND created_at >= ?`
	args := make([]any, 0, len(ids)+1)
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, formatTime(since.UTC()))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make(map[int64]BackendHourlyStats, len(ids))
	for rows.Next() {
		var (
			backendID  int64
			statusCode int
		)
		if err := rows.Scan(&backendID, &statusCode); err != nil {
			return nil, err
		}
		item := stats[backendID]
		item.Requests++
		if domain.IsBackendFailureStatus(statusCode) {
			item.Failures++
		}
		stats[backendID] = item
	}
	return stats, rows.Err()
}

func (s *Store) BackendUsageSummaryByIDs(ctx context.Context, ids []int64) (map[int64]BackendUsageSummary, error) {
	if len(ids) == 0 {
		return map[int64]BackendUsageSummary{}, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT backend_id, COUNT(*), AVG(duration_ms), MAX(created_at)
		FROM usage_logs
		WHERE backend_id IN (`+placeholders+`)
		GROUP BY backend_id
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int64]BackendUsageSummary, len(ids))
	for rows.Next() {
		var (
			backendID  int64
			requests   int
			avgLatency float64
			lastUsed   string
		)
		if err := rows.Scan(&backendID, &requests, &avgLatency, &lastUsed); err != nil {
			return nil, err
		}
		out[backendID] = BackendUsageSummary{
			RequestCount: requests,
			AvgLatencyMS: avgLatency,
			LastUsedAt:   parseTime(lastUsed),
		}
	}
	return out, rows.Err()
}

func (s *Store) BackendAverageLatencyByIDs(ctx context.Context, ids []int64) (map[int64]float64, error) {
	if len(ids) == 0 {
		return map[int64]float64{}, nil
	}
	query := `SELECT backend_id, AVG(duration_ms) FROM usage_logs WHERE backend_id IN (` +
		placeholders(len(ids)) + `) GROUP BY backend_id`
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int64]float64, len(ids))
	for rows.Next() {
		var backendID int64
		var avgLatency float64
		if err := rows.Scan(&backendID, &avgLatency); err != nil {
			return nil, err
		}
		out[backendID] = avgLatency
	}
	return out, rows.Err()
}

func (s *Store) BackendBindingCountByProxyIDs(ctx context.Context, ids []int64) (map[int64]int, error) {
	if len(ids) == 0 {
		return map[int64]int{}, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT proxy_id, COUNT(*)
		FROM backends
		WHERE proxy_id IN (`+placeholders+`)
		GROUP BY proxy_id
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int64]int, len(ids))
	for rows.Next() {
		var (
			proxyID int64
			count   int
		)
		if err := rows.Scan(&proxyID, &count); err != nil {
			return nil, err
		}
		out[proxyID] = count
	}
	return out, rows.Err()
}

func (s *Store) ListBackendsPage(ctx context.Context, limit, offset int) ([]domain.Backend, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			b.id, b.name, b.protocol, b.backend_type, b.base_url, b.api_key, b.api_keys_json, b.console_url, b.tag_list, b.console_username, b.console_password, b.new_api_refresh, b.console_authorization, b.console_checkin_path, b.console_checkin_workflow_id, b.manual_checkin, b.frozen, b.channel_url, b.console_cookie, b.console_headers_json, b.console_refresh_token, b.console_account_json, b.console_pricing_json, b.notes, b.proxy_id, b.status, b.consecutive_failures, b.recover_at, b.weight, b.model_list, b.model_mapping, b.endpoint_list, b.created_at, b.updated_at,
			p.id, p.name, p.address, p.username, p.password, p.enabled, p.created_at, p.updated_at
		FROM backends b
		LEFT JOIN socks_proxies p ON p.id = b.proxy_id
		ORDER BY b.id DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var backends []domain.Backend
	for rows.Next() {
		backend, err := scanBackend(rows)
		if err != nil {
			return nil, err
		}
		backends = append(backends, backend)
	}
	return backends, rows.Err()
}

func (s *Store) CreateBackend(ctx context.Context, backend domain.Backend) (domain.Backend, error) {
	now := time.Now().UTC()
	backend.CreatedAt = now
	backend.UpdatedAt = now
	apiKeys := effectiveBackendAPIKeys(backend)
	legacyAPIKey, legacyModels, legacyModelMapping := legacyBackendRoutingFields(apiKeys)

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO backends(name, protocol, backend_type, base_url, api_key, api_keys_json, console_url, tag_list, console_username, console_password, new_api_refresh, console_authorization, console_checkin_path, console_checkin_workflow_id, manual_checkin, frozen, channel_url, console_cookie, console_headers_json, console_refresh_token, console_account_json, console_pricing_json, notes, proxy_id, status, consecutive_failures, recover_at, weight, model_list, model_mapping, endpoint_list, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		strings.TrimSpace(backend.Name),
		domain.NormalizeBackendProtocol(backend.Protocol),
		"",
		strings.TrimSpace(backend.BaseURL),
		legacyAPIKey,
		mustEncodeBackendAPIKeys(apiKeys),
		strings.TrimSpace(backend.ConsoleURL),
		mustEncodeList(backend.Tags),
		strings.TrimSpace(backend.ConsoleUsername),
		strings.TrimSpace(backend.ConsolePassword),
		"",
		"",
		"",
		strings.TrimSpace(backend.ConsoleCheckinWorkflow),
		backend.ManualCheckin,
		backend.Frozen,
		"",
		strings.TrimSpace(backend.ConsoleCookie),
		mustEncodeMap(backend.ConsoleHeaders),
		strings.TrimSpace(backend.ConsoleRefreshToken),
		normalizeJSONObject(backend.ConsoleAccountJSON),
		normalizeJSONObject(backend.ConsolePricingJSON),
		strings.TrimSpace(backend.Notes),
		backend.ProxyID,
		normalizeBackendStatus(backend.Status),
		0,
		"",
		normalizeWeight(backend.Weight),
		mustEncodeList(legacyModels),
		mustEncodeMap(legacyModelMapping),
		mustEncodeList(backend.Endpoints),
		formatTime(now),
		formatTime(now),
	)
	if err != nil {
		return domain.Backend{}, err
	}

	backend.ID, err = result.LastInsertId()
	if err != nil {
		return domain.Backend{}, err
	}
	return s.GetBackend(ctx, backend.ID)
}

func (s *Store) ImportBackends(ctx context.Context, backends []domain.Backend) ([]domain.Backend, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	created := make([]domain.Backend, 0, len(backends))
	for _, backend := range backends {
		backend.Name = strings.TrimSpace(backend.Name)
		backend.Protocol = domain.NormalizeBackendProtocol(backend.Protocol)
		backend.BaseURL = strings.TrimSpace(backend.BaseURL)
		backend.APIKeys = effectiveBackendAPIKeys(backend)
		backend.APIKey, backend.Models, backend.ModelMapping = legacyBackendRoutingFields(backend.APIKeys)
		backend.ConsoleURL = strings.TrimSpace(backend.ConsoleURL)
		backend.ConsoleUsername = strings.TrimSpace(backend.ConsoleUsername)
		backend.ConsolePassword = strings.TrimSpace(backend.ConsolePassword)
		backend.ConsoleCookie = strings.TrimSpace(backend.ConsoleCookie)
		backend.ConsoleHeaders = normalizeMap(backend.ConsoleHeaders)
		backend.ConsoleAccountJSON = normalizeJSONObject(backend.ConsoleAccountJSON)
		backend.ConsolePricingJSON = normalizeJSONObject(backend.ConsolePricingJSON)
		backend.Notes = strings.TrimSpace(backend.Notes)
		backend.Status = normalizeBackendStatus(backend.Status)
		backend.Weight = normalizeWeight(backend.Weight)
		result, err := tx.ExecContext(ctx, `
			INSERT INTO backends(name, protocol, backend_type, base_url, api_key, api_keys_json, console_url, tag_list, console_username, console_password, console_refresh_token, new_api_refresh, console_authorization, console_checkin_path, console_checkin_workflow_id, manual_checkin, channel_url, console_cookie, console_headers_json, console_account_json, console_pricing_json, notes, proxy_id, status, consecutive_failures, recover_at, weight, model_list, model_mapping, endpoint_list, created_at, updated_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			backend.Name,
			backend.Protocol,
			"",
			backend.BaseURL,
			backend.APIKey,
			mustEncodeBackendAPIKeys(backend.APIKeys),
			backend.ConsoleURL,
			mustEncodeList(backend.Tags),
			backend.ConsoleUsername,
			backend.ConsolePassword,
			backend.ConsoleRefreshToken,
			"",
			"",
			"",
			backend.ConsoleCheckinWorkflow,
			backend.ManualCheckin,
			"",
			backend.ConsoleCookie,
			mustEncodeMap(backend.ConsoleHeaders),
			backend.ConsoleAccountJSON,
			backend.ConsolePricingJSON,
			backend.Notes,
			backend.ProxyID,
			backend.Status,
			backend.ConsecutiveFailures,
			"",
			backend.Weight,
			mustEncodeList(backend.Models),
			mustEncodeMap(backend.ModelMapping),
			mustEncodeList(backend.Endpoints),
			formatTime(now),
			formatTime(now),
		)
		if err != nil {
			return nil, err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return nil, err
		}
		backend.ID = id
		backend.CreatedAt = now
		backend.UpdatedAt = now
		backend.RecoverAt = nil
		created = append(created, backend)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Store) UpdateBackend(ctx context.Context, backend domain.Backend) (domain.Backend, error) {
	now := time.Now().UTC()
	backend.UpdatedAt = now
	apiKeys := effectiveBackendAPIKeys(backend)
	legacyAPIKey, legacyModels, legacyModelMapping := legacyBackendRoutingFields(apiKeys)

	_, err := s.db.ExecContext(ctx, `
		UPDATE backends
		SET name = ?, protocol = ?, backend_type = ?, base_url = ?, api_key = ?, api_keys_json = ?, console_url = ?, tag_list = ?, console_username = ?, console_password = ?, console_refresh_token = ?, new_api_refresh = ?, console_authorization = ?, console_checkin_path = ?, console_checkin_workflow_id = ?, manual_checkin = ?, frozen = ?, channel_url = ?, console_cookie = ?, console_headers_json = ?, console_account_json = ?, console_pricing_json = ?, notes = ?, proxy_id = ?, status = ?, consecutive_failures = ?, recover_at = ?, weight = ?, model_list = ?, model_mapping = ?, endpoint_list = ?, updated_at = ?
		WHERE id = ?
	`,
		strings.TrimSpace(backend.Name),
		domain.NormalizeBackendProtocol(backend.Protocol),
		"",
		strings.TrimSpace(backend.BaseURL),
		legacyAPIKey,
		mustEncodeBackendAPIKeys(apiKeys),
		strings.TrimSpace(backend.ConsoleURL),
		mustEncodeList(backend.Tags),
		strings.TrimSpace(backend.ConsoleUsername),
		strings.TrimSpace(backend.ConsolePassword),
		strings.TrimSpace(backend.ConsoleRefreshToken),
		"",
		"",
		"",
		strings.TrimSpace(backend.ConsoleCheckinWorkflow),
		backend.ManualCheckin,
		backend.Frozen,
		"",
		strings.TrimSpace(backend.ConsoleCookie),
		mustEncodeMap(normalizeMap(backend.ConsoleHeaders)),
		normalizeJSONObject(backend.ConsoleAccountJSON),
		normalizeJSONObject(backend.ConsolePricingJSON),
		strings.TrimSpace(backend.Notes),
		backend.ProxyID,
		normalizeBackendStatus(backend.Status),
		backend.ConsecutiveFailures,
		formatOptionalTime(backend.RecoverAt),
		normalizeWeight(backend.Weight),
		mustEncodeList(legacyModels),
		mustEncodeMap(legacyModelMapping),
		mustEncodeList(backend.Endpoints),
		formatTime(now),
		backend.ID,
	)
	if err != nil {
		return domain.Backend{}, err
	}
	return s.GetBackend(ctx, backend.ID)
}

func (s *Store) PatchBackend(ctx context.Context, id int64, patch BackendPatch) (domain.Backend, error) {
	sets := make([]string, 0, 24)
	args := make([]any, 0, 25)
	add := func(column string, value any) {
		sets = append(sets, column+" = ?")
		args = append(args, value)
	}

	if patch.Name != nil {
		add("name", strings.TrimSpace(*patch.Name))
	}
	if patch.Protocol != nil {
		add("protocol", domain.NormalizeBackendProtocol(*patch.Protocol))
	}
	if patch.BaseURL != nil {
		add("base_url", strings.TrimSpace(*patch.BaseURL))
	}
	if patch.APIKeys != nil {
		apiKeys := normalizeBackendAPIKeys(*patch.APIKeys)
		legacyAPIKey, legacyModels, legacyModelMapping := legacyBackendRoutingFields(apiKeys)
		add("api_key", legacyAPIKey)
		add("api_keys_json", mustEncodeBackendAPIKeys(apiKeys))
		add("model_list", mustEncodeList(legacyModels))
		add("model_mapping", mustEncodeMap(legacyModelMapping))
	}
	if patch.ConsoleURL != nil {
		add("console_url", strings.TrimSpace(*patch.ConsoleURL))
	}
	if patch.Tags != nil {
		add("tag_list", mustEncodeList(*patch.Tags))
	}
	if patch.ConsoleUsername != nil {
		add("console_username", strings.TrimSpace(*patch.ConsoleUsername))
	}
	if patch.ConsolePassword != nil {
		add("console_password", strings.TrimSpace(*patch.ConsolePassword))
	}
	if patch.ConsoleCheckinWorkflow != nil {
		add("console_checkin_workflow_id", strings.TrimSpace(*patch.ConsoleCheckinWorkflow))
	}
	if patch.ManualCheckin != nil {
		add("manual_checkin", *patch.ManualCheckin)
	}
	if patch.Frozen != nil {
		add("frozen", *patch.Frozen)
	}
	if patch.ConsoleCookie != nil {
		add("console_cookie", strings.TrimSpace(*patch.ConsoleCookie))
	}
	if patch.ConsoleHeaders != nil {
		add("console_headers_json", mustEncodeMap(normalizeMap(*patch.ConsoleHeaders)))
	}
	if patch.ConsoleRefreshToken != nil {
		add("console_refresh_token", strings.TrimSpace(*patch.ConsoleRefreshToken))
	}
	if patch.ConsoleAccountJSON != nil {
		add("console_account_json", normalizeJSONObject(*patch.ConsoleAccountJSON))
	}
	if patch.Notes != nil {
		add("notes", strings.TrimSpace(*patch.Notes))
	}
	if patch.ProxyID != nil {
		add("proxy_id", *patch.ProxyID)
	}
	if patch.Status != nil {
		add("status", normalizeBackendStatus(*patch.Status))
	}
	if patch.ResetRuntimeState {
		add("consecutive_failures", 0)
		add("recover_at", "")
	}
	if patch.Weight != nil {
		add("weight", normalizeWeight(*patch.Weight))
	}
	if len(sets) == 0 {
		return s.GetBackend(ctx, id)
	}

	now := time.Now().UTC()
	add("updated_at", formatTime(now))
	args = append(args, id)
	if _, err := s.db.ExecContext(ctx, `UPDATE backends SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		return domain.Backend{}, err
	}
	return s.GetBackend(ctx, id)
}

func (s *Store) GetBackend(ctx context.Context, id int64) (domain.Backend, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			b.id, b.name, b.protocol, b.backend_type, b.base_url, b.api_key, b.api_keys_json, b.console_url, b.tag_list, b.console_username, b.console_password, b.new_api_refresh, b.console_authorization, b.console_checkin_path, b.console_checkin_workflow_id, b.manual_checkin, b.frozen, b.channel_url, b.console_cookie, b.console_headers_json, b.console_refresh_token, b.console_account_json, b.console_pricing_json, b.notes, b.proxy_id, b.status, b.consecutive_failures, b.recover_at, b.weight, b.model_list, b.model_mapping, b.endpoint_list, b.created_at, b.updated_at,
			p.id, p.name, p.address, p.username, p.password, p.enabled, p.created_at, p.updated_at
		FROM backends b
		LEFT JOIN socks_proxies p ON p.id = b.proxy_id
		WHERE b.id = ?
	`, id)
	return scanBackend(row)
}

func (s *Store) MarkBackendSuccess(ctx context.Context, backendID int64) (domain.Backend, error) {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		UPDATE backends
		SET status = ?, consecutive_failures = 0, recover_at = '', updated_at = ?
		WHERE id = ? AND status != ?
	`, domain.BackendStatusNormal, formatTime(now), backendID, domain.BackendStatusDisabled)
	if err != nil {
		return domain.Backend{}, err
	}
	return s.GetBackend(ctx, backendID)
}

func (s *Store) MarkBackendFailure(ctx context.Context, backendID int64, threshold int, cooldown time.Duration, now time.Time) (domain.Backend, error) {
	backend, err := s.GetBackend(ctx, backendID)
	if err != nil {
		return domain.Backend{}, err
	}

	if threshold < 1 {
		threshold = 1
	}
	now = now.UTC()
	failures := backend.ConsecutiveFailures + 1
	status := backend.Status
	recoverAt := backend.RecoverAt
	if status == "" {
		status = domain.BackendStatusNormal
	}
	if status != domain.BackendStatusDisabled && failures >= threshold {
		status = domain.BackendStatusAbnormal
		recoverAtValue := now.Add(cooldown).UTC()
		recoverAt = &recoverAtValue
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE backends
		SET status = ?, consecutive_failures = ?, recover_at = ?, updated_at = ?
		WHERE id = ?
	`, status, failures, formatOptionalTime(recoverAt), formatTime(now), backendID)
	if err != nil {
		return domain.Backend{}, err
	}
	return s.GetBackend(ctx, backendID)
}

func (s *Store) RecoverExpiredBackends(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE backends
		SET status = ?, consecutive_failures = 0, recover_at = '', updated_at = ?
		WHERE status = ? AND recover_at != '' AND recover_at <= ?
	`, domain.BackendStatusNormal, formatTime(now.UTC()), domain.BackendStatusAbnormal, formatTime(now.UTC()))
	return err
}

func (s *Store) DeleteBackend(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM backends WHERE id = ?`, id)
	return err
}

func (s *Store) ListBackendHourlyModelStats(ctx context.Context, filter BackendHourlyModelStatsFilter) (BackendHourlyModelStatsResult, error) {
	where, args := backendHourlyModelStatsFilterClause(filter)
	rows, err := s.db.QueryContext(ctx, `
		SELECT backend_id, backend_name, model, hour_start_utc,
			success_count, failure_count, success_duration_ms_sum,
			success_request_bytes_sum, success_response_bytes_sum,
			success_input_tokens_sum, success_output_tokens_sum, success_input_cache_tokens_sum
		FROM backend_hourly_model_stats
	`+where+`
		ORDER BY hour_start_utc ASC, backend_name ASC, model ASC
	`, args...)
	if err != nil {
		return BackendHourlyModelStatsResult{}, err
	}
	defer rows.Close()

	result := BackendHourlyModelStatsResult{
		Rows:     []BackendHourlyModelStatsRow{},
		Backends: []BackendRef{},
		Models:   []string{},
	}
	backendSeen := make(map[int64]string)
	modelSeen := make(map[string]struct{})
	for rows.Next() {
		var row BackendHourlyModelStatsRow
		var hourStart string
		if err := rows.Scan(
			&row.BackendID,
			&row.BackendName,
			&row.Model,
			&hourStart,
			&row.Successes,
			&row.Failures,
			&row.SuccessDurationMSSum,
			&row.SuccessRequestBytes,
			&row.SuccessResponseBytes,
			&row.SuccessInputTokens,
			&row.SuccessOutputTokens,
			&row.SuccessInputCacheTokens,
		); err != nil {
			return BackendHourlyModelStatsResult{}, err
		}
		row.HourStart = parseTime(hourStart)
		result.Rows = append(result.Rows, row)

		if _, ok := backendSeen[row.BackendID]; !ok {
			backendSeen[row.BackendID] = row.BackendName
		}
		modelSeen[row.Model] = struct{}{}
		if result.RangeStart == nil || row.HourStart.Before(*result.RangeStart) {
			hour := row.HourStart
			result.RangeStart = &hour
		}
		if result.RangeEnd == nil || row.HourStart.After(*result.RangeEnd) {
			hour := row.HourStart
			result.RangeEnd = &hour
		}
	}
	if err := rows.Err(); err != nil {
		return BackendHourlyModelStatsResult{}, err
	}

	for id, name := range backendSeen {
		result.Backends = append(result.Backends, BackendRef{ID: id, Name: name})
	}
	slices.SortFunc(result.Backends, func(a, b BackendRef) int {
		if a.Name != b.Name {
			return strings.Compare(a.Name, b.Name)
		}
		return cmp.Compare(a.ID, b.ID)
	})
	for model := range modelSeen {
		result.Models = append(result.Models, model)
	}
	slices.Sort(result.Models)

	return result, nil
}

func upsertBackendHourlyModelStats(ctx context.Context, tx *sql.Tx, log domain.UsageLog, createdAt time.Time) error {
	if log.BackendID <= 0 || strings.TrimSpace(log.BackendName) == "" || strings.TrimSpace(log.Model) == "" {
		return nil
	}

	successes := 0
	failures := 0
	successDuration := int64(0)
	successRequestBytes := int64(0)
	successResponseBytes := int64(0)
	successInputTokens := int64(0)
	successOutputTokens := int64(0)
	successInputCacheTokens := int64(0)
	if isSuccessStatus(log.StatusCode) {
		successes = 1
		successDuration = log.DurationMS
		successRequestBytes = log.RequestBytes
		successResponseBytes = log.ResponseBytes
		successInputTokens = log.InputTokens
		successOutputTokens = log.OutputTokens
		successInputCacheTokens = log.InputCacheTokens
	} else {
		failures = 1
	}

	now := formatTime(time.Now().UTC())
	_, err := tx.ExecContext(ctx, `
		INSERT INTO backend_hourly_model_stats(
			backend_id, backend_name, model, hour_start_utc,
			success_count, failure_count, success_duration_ms_sum,
			success_request_bytes_sum, success_response_bytes_sum,
			success_input_tokens_sum, success_output_tokens_sum, success_input_cache_tokens_sum,
			created_at, updated_at
		)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(backend_id, model, hour_start_utc) DO UPDATE SET
			backend_name = excluded.backend_name,
			success_count = backend_hourly_model_stats.success_count + excluded.success_count,
			failure_count = backend_hourly_model_stats.failure_count + excluded.failure_count,
			success_duration_ms_sum = backend_hourly_model_stats.success_duration_ms_sum + excluded.success_duration_ms_sum,
			success_request_bytes_sum = backend_hourly_model_stats.success_request_bytes_sum + excluded.success_request_bytes_sum,
			success_response_bytes_sum = backend_hourly_model_stats.success_response_bytes_sum + excluded.success_response_bytes_sum,
			success_input_tokens_sum = backend_hourly_model_stats.success_input_tokens_sum + excluded.success_input_tokens_sum,
			success_output_tokens_sum = backend_hourly_model_stats.success_output_tokens_sum + excluded.success_output_tokens_sum,
			success_input_cache_tokens_sum = backend_hourly_model_stats.success_input_cache_tokens_sum + excluded.success_input_cache_tokens_sum,
			updated_at = excluded.updated_at
	`,
		log.BackendID,
		strings.TrimSpace(log.BackendName),
		strings.TrimSpace(log.Model),
		formatTime(backendHourlyBucketUTC(createdAt)),
		successes,
		failures,
		successDuration,
		successRequestBytes,
		successResponseBytes,
		successInputTokens,
		successOutputTokens,
		successInputCacheTokens,
		now,
		now,
	)
	return err
}

func backendHourlyBucketUTC(createdAt time.Time) time.Time {
	return createdAt.UTC().Truncate(time.Hour)
}

func isSuccessStatus(statusCode int) bool {
	return statusCode >= 200 && statusCode < 300
}

func backendHourlyModelStatsFilterClause(filter BackendHourlyModelStatsFilter) (string, []any) {
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
	if !filter.StartHour.IsZero() {
		clauses = append(clauses, `hour_start_utc >= ?`)
		args = append(args, formatTime(filter.StartHour.UTC()))
	}
	if !filter.EndHour.IsZero() {
		clauses = append(clauses, `hour_start_utc <= ?`)
		args = append(args, formatTime(filter.EndHour.UTC()))
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func scanBackend(s scanner) (domain.Backend, error) {
	var (
		backend                                                                                                                                                                 domain.Backend
		modelList, modelMappingRaw, apiKeysJSON, tagList                                                                                                                        string
		endpointList                                                                                                                                                            string
		createdAt, updatedAt                                                                                                                                                    string
		recoverAt, consoleURL                                                                                                                                                   string
		consoleUsername, consolePassword, newAPIRefresh, consoleAuthorization, consoleCheckinPath, consoleCheckinWorkflow, channelURL, consoleCookie, consoleHeadersJSON, notes string
		consoleRefreshToken, consoleAccountJSON, consolePricingJSON, backendTypeValue                                                                                           string
		manualCheckin                                                                                                                                                           int64
		frozen                                                                                                                                                                  int64
		proxyID                                                                                                                                                                 sql.NullInt64
		proxyName                                                                                                                                                               sql.NullString
		proxyAddress                                                                                                                                                            sql.NullString
		proxyUsername                                                                                                                                                           sql.NullString
		proxyPassword                                                                                                                                                           sql.NullString
		proxyEnabled                                                                                                                                                            sql.NullInt64
		proxyCreatedAt                                                                                                                                                          sql.NullString
		proxyUpdatedAt                                                                                                                                                          sql.NullString
	)
	err := s.Scan(
		&backend.ID,
		&backend.Name,
		&backend.Protocol,
		&backendTypeValue,
		&backend.BaseURL,
		&backend.APIKey,
		&apiKeysJSON,
		&consoleURL,
		&tagList,
		&consoleUsername,
		&consolePassword,
		&newAPIRefresh,
		&consoleAuthorization,
		&consoleCheckinPath,
		&consoleCheckinWorkflow,
		&manualCheckin,
		&frozen,
		&channelURL,
		&consoleCookie,
		&consoleHeadersJSON,
		&consoleRefreshToken,
		&consoleAccountJSON,
		&consolePricingJSON,
		&notes,
		&backend.ProxyID,
		&backend.Status,
		&backend.ConsecutiveFailures,
		&recoverAt,
		&backend.Weight,
		&modelList,
		&modelMappingRaw,
		&endpointList,
		&createdAt,
		&updatedAt,
		&proxyID,
		&proxyName,
		&proxyAddress,
		&proxyUsername,
		&proxyPassword,
		&proxyEnabled,
		&proxyCreatedAt,
		&proxyUpdatedAt,
	)
	if err != nil {
		return domain.Backend{}, err
	}

	backend.Status = normalizeBackendStatus(backend.Status)
	backend.RecoverAt = parseOptionalTime(recoverAt)
	backend.Protocol = domain.NormalizeBackendProtocol(backend.Protocol)
	backend.ConsoleURL = strings.TrimSpace(consoleURL)
	backend.Tags = decodeList(tagList)
	backend.ConsoleUsername = strings.TrimSpace(consoleUsername)
	backend.ConsolePassword = strings.TrimSpace(consolePassword)
	backend.ConsoleCheckinWorkflow = strings.TrimSpace(consoleCheckinWorkflow)
	backend.ManualCheckin = manualCheckin != 0
	backend.Frozen = frozen != 0
	backend.ConsoleCookie = strings.TrimSpace(consoleCookie)
	backend.ConsoleHeaders = decodeMap(consoleHeadersJSON)
	backend.ConsoleRefreshToken = strings.TrimSpace(consoleRefreshToken)
	backend.ConsoleAccountJSON = normalizeJSONObject(consoleAccountJSON)
	backend.ConsolePricingJSON = normalizeJSONObject(consolePricingJSON)
	backend.Notes = strings.TrimSpace(notes)
	backend.APIKeys = decodeBackendAPIKeys(apiKeysJSON)
	if len(backend.APIKeys) == 0 {
		backend.APIKeys = effectiveBackendAPIKeys(domain.Backend{
			APIKey:       backend.APIKey,
			Models:       decodeList(modelList),
			ModelMapping: decodeMap(modelMappingRaw),
		})
	}
	backend.APIKey, backend.Models, backend.ModelMapping = legacyBackendRoutingFields(backend.APIKeys)
	backend.Endpoints = decodeList(endpointList)
	backend.CreatedAt = parseTime(createdAt)
	backend.UpdatedAt = parseTime(updatedAt)
	if proxyID.Valid {
		backend.Proxy = &domain.SocksProxy{
			ID:        proxyID.Int64,
			Name:      proxyName.String,
			Address:   proxyAddress.String,
			Username:  proxyUsername.String,
			Password:  proxyPassword.String,
			Enabled:   proxyEnabled.Int64 == 1,
			CreatedAt: parseTime(proxyCreatedAt.String),
			UpdatedAt: parseTime(proxyUpdatedAt.String),
		}
	}
	return backend, nil
}

func (s *Store) listBackendsByProxyID(ctx context.Context, proxyID int64) ([]domain.Backend, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			b.id, b.name, b.protocol, b.backend_type, b.base_url, b.api_key, b.api_keys_json, b.console_url, b.tag_list, b.console_username, b.console_password, b.new_api_refresh, b.console_authorization, b.console_checkin_path, b.console_checkin_workflow_id, b.manual_checkin, b.frozen, b.channel_url, b.console_cookie, b.console_headers_json, b.console_account_json, b.console_pricing_json, b.notes, b.proxy_id, b.status, b.consecutive_failures, b.recover_at, b.weight, b.model_list, b.model_mapping, b.endpoint_list, b.created_at, b.updated_at,
			p.id, p.name, p.address, p.username, p.password, p.enabled, p.created_at, p.updated_at
		FROM backends b
		LEFT JOIN socks_proxies p ON p.id = b.proxy_id
		WHERE b.proxy_id = ?
		ORDER BY b.id DESC
	`, proxyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var backends []domain.Backend
	for rows.Next() {
		backend, err := scanBackend(rows)
		if err != nil {
			return nil, err
		}
		backends = append(backends, backend)
	}
	return backends, rows.Err()
}

func normalizeBackendStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case domain.BackendStatusAbnormal:
		return domain.BackendStatusAbnormal
	case domain.BackendStatusDisabled:
		return domain.BackendStatusDisabled
	default:
		return domain.BackendStatusNormal
	}
}

func normalizeWeight(value int) int {
	if value < 1 {
		return 1
	}
	return value
}

func normalizeJSONObject(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "{}"
	}
	if !json.Valid([]byte(value)) {
		return "{}"
	}
	return value
}
