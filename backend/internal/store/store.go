package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"red-token/internal/domain"
)

type scanner interface {
	Scan(dest ...any) error
}

type Store struct {
	db *sql.DB
}

type SearchResults struct {
	Backends   []SearchResult
	ClientKeys []SearchResult
	Proxies    []SearchResult
	UsageLogs  []SearchResult
	Events     []SearchResult
}

type SearchResult struct {
	Kind       string
	ID         int64
	Title      string
	Subtitle   string
	Meta       map[string]any
	Status     string
	TargetPage string
	TargetID   int64
}

func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite3", sqliteDSN(path))
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(0)

	stmts := []string{
		`PRAGMA journal_mode = WAL;`,
		`PRAGMA busy_timeout = 5000;`,
		`PRAGMA foreign_keys = ON;`,
		`CREATE TABLE IF NOT EXISTS client_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			token TEXT NOT NULL DEFAULT '',
			token_prefix TEXT NOT NULL,
			allowed_models TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			request_success_count INTEGER NOT NULL DEFAULT 0,
			request_failure_count INTEGER NOT NULL DEFAULT 0,
			input_tokens_total INTEGER NOT NULL DEFAULT 0,
			output_tokens_total INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_client_keys_enabled ON client_keys(enabled);`,
		`CREATE TABLE IF NOT EXISTS socks_proxies (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			address TEXT NOT NULL,
			username TEXT NOT NULL DEFAULT '',
			password TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_socks_proxies_enabled ON socks_proxies(enabled);`,
		// Retired platform columns stay in the physical layout so existing SQLite
		// databases can upgrade without a destructive table rebuild.
		`CREATE TABLE IF NOT EXISTS backends (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			protocol TEXT NOT NULL DEFAULT 'openai',
			backend_type TEXT NOT NULL DEFAULT '',
			base_url TEXT NOT NULL,
			api_key TEXT NOT NULL,
			api_keys_json TEXT NOT NULL DEFAULT '[]',
			console_url TEXT NOT NULL DEFAULT '',
			tag_list TEXT NOT NULL DEFAULT '[]',
			console_username TEXT NOT NULL DEFAULT '',
			console_password TEXT NOT NULL DEFAULT '',
			new_api_refresh TEXT NOT NULL DEFAULT '',
			console_authorization TEXT NOT NULL DEFAULT '',
				console_checkin_path TEXT NOT NULL DEFAULT '',
				console_checkin_workflow_id TEXT NOT NULL DEFAULT '',
				manual_checkin INTEGER NOT NULL DEFAULT 0,
				channel_url TEXT NOT NULL DEFAULT '',
			console_cookie TEXT NOT NULL DEFAULT '',
			console_headers_json TEXT NOT NULL DEFAULT '{}',
			console_refresh_token TEXT NOT NULL DEFAULT '',
			console_account_json TEXT NOT NULL DEFAULT '{}',
			console_pricing_json TEXT NOT NULL DEFAULT '{}',
			notes TEXT NOT NULL DEFAULT '',
			proxy_id INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'normal',
			consecutive_failures INTEGER NOT NULL DEFAULT 0,
			recover_at TEXT NOT NULL DEFAULT '',
			weight INTEGER NOT NULL DEFAULT 1,
			model_list TEXT NOT NULL,
			model_mapping TEXT NOT NULL DEFAULT '{}',
			endpoint_list TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS audit_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			level TEXT NOT NULL,
			type TEXT NOT NULL,
			category TEXT NOT NULL DEFAULT '',
			severity TEXT NOT NULL DEFAULT '',
			actor TEXT NOT NULL DEFAULT '',
			resource_type TEXT NOT NULL DEFAULT '',
			resource_id INTEGER NOT NULL DEFAULT 0,
			message TEXT NOT NULL,
			client_name TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			endpoint TEXT NOT NULL DEFAULT '',
			backend_name TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_created_at ON audit_events(created_at DESC);`,
		`CREATE TABLE IF NOT EXISTS usage_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id TEXT NOT NULL,
			client_id INTEGER NOT NULL DEFAULT 0,
			client_name TEXT NOT NULL DEFAULT '',
			client_token_prefix TEXT NOT NULL DEFAULT '',
			method TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL DEFAULT '',
			query TEXT NOT NULL DEFAULT '',
			endpoint TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			backend_id INTEGER NOT NULL DEFAULT 0,
			backend_name TEXT NOT NULL DEFAULT '',
			proxy_id INTEGER NOT NULL DEFAULT 0,
			proxy_name TEXT NOT NULL DEFAULT '',
			attempts INTEGER NOT NULL DEFAULT 0,
			status_code INTEGER NOT NULL DEFAULT 0,
			status_family TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0,
			error_message TEXT NOT NULL DEFAULT '',
			client_ip TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			trace_id TEXT NOT NULL DEFAULT '',
			request_bytes INTEGER NOT NULL DEFAULT 0,
			response_bytes INTEGER NOT NULL DEFAULT 0,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			input_cache_tokens INTEGER NOT NULL DEFAULT 0,
			request_headers_json TEXT NOT NULL DEFAULT '{}',
			request_body_preview TEXT NOT NULL DEFAULT '',
			response_headers_json TEXT NOT NULL DEFAULT '{}',
			response_body_preview TEXT NOT NULL DEFAULT '',
			preview_truncated INTEGER NOT NULL DEFAULT 0,
			is_stream INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_usage_logs_created_at ON usage_logs(created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_usage_logs_client_id_created_at ON usage_logs(client_id, created_at DESC);`,
		`CREATE TABLE IF NOT EXISTS backend_hourly_model_stats (
			backend_id INTEGER NOT NULL,
			backend_name TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL,
			hour_start_utc TEXT NOT NULL,
			success_count INTEGER NOT NULL DEFAULT 0,
			failure_count INTEGER NOT NULL DEFAULT 0,
			success_duration_ms_sum INTEGER NOT NULL DEFAULT 0,
			success_request_bytes_sum INTEGER NOT NULL DEFAULT 0,
			success_response_bytes_sum INTEGER NOT NULL DEFAULT 0,
			success_input_tokens_sum INTEGER NOT NULL DEFAULT 0,
			success_output_tokens_sum INTEGER NOT NULL DEFAULT 0,
			success_input_cache_tokens_sum INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (backend_id, model, hour_start_utc)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_backend_hourly_model_stats_hour ON backend_hourly_model_stats(hour_start_utc DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_backend_hourly_model_stats_model_hour ON backend_hourly_model_stats(model, hour_start_utc DESC);`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS workflow_definitions (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			definition_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_definitions_updated_at ON workflow_definitions(updated_at DESC);`,
		`CREATE TABLE IF NOT EXISTS workflow_results (
			workflow_id TEXT NOT NULL,
			backend_id INTEGER NOT NULL,
			output_json TEXT NOT NULL,
			executed_at TEXT NOT NULL,
			PRIMARY KEY (workflow_id, backend_id),
			FOREIGN KEY (workflow_id) REFERENCES workflow_definitions(id) ON DELETE CASCADE,
			FOREIGN KEY (backend_id) REFERENCES backends(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_results_backend_id ON workflow_results(backend_id);`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("exec schema: %w", err)
		}
	}

	if err := ensureColumn(ctx, db, "client_keys", "token", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate client_keys token: %w", err)
	}
	if err := ensureColumn(ctx, db, "client_keys", "allowed_models", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate client_keys allowed_models: %w", err)
	}
	for _, column := range []struct {
		name       string
		definition string
	}{
		{name: "request_success_count", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "request_failure_count", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "input_tokens_total", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "output_tokens_total", definition: "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := ensureColumn(ctx, db, "client_keys", column.name, column.definition); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("migrate client_keys %s: %w", column.name, err)
		}
	}
	if err := ensureColumn(ctx, db, "backends", "proxy_id", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends proxy_id: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "protocol", "TEXT NOT NULL DEFAULT 'openai'"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends protocol: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "backend_type", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends backend_type: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "model_mapping", "TEXT NOT NULL DEFAULT '{}'"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends model_mapping: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "api_keys_json", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends api_keys_json: %w", err)
	}
	if err := migrateBackendAPIKeys(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backend api keys: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "status", "TEXT NOT NULL DEFAULT 'normal'"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends status: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "consecutive_failures", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends consecutive_failures: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "recover_at", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends recover_at: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "console_url", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends console_url: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "tag_list", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends tag_list: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "console_username", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends console_username: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "console_password", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends console_password: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "new_api_refresh", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends new_api_refresh: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "console_authorization", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends console_authorization: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "console_checkin_path", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends console_checkin_path: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "console_checkin_workflow_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends console_checkin_workflow_id: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "manual_checkin", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends manual_checkin: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "channel_url", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends channel_url: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "console_cookie", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends console_cookie: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "console_headers_json", "TEXT NOT NULL DEFAULT '{}'"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends console_headers_json: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "console_refresh_token", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends console_refresh_token: %w", err)
	}
	if err := migrateBackendConsoleCookies(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backend console cookies to headers: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "console_account_json", "TEXT NOT NULL DEFAULT '{}'"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends console_account_json: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "console_pricing_json", "TEXT NOT NULL DEFAULT '{}'"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends console_pricing_json: %w", err)
	}
	if err := ensureColumn(ctx, db, "backends", "notes", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends notes: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_backends_status ON backends(status);`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate backends status index: %w", err)
	}
	if err := ensureColumn(ctx, db, "audit_events", "category", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate audit_events category: %w", err)
	}
	if err := ensureColumn(ctx, db, "audit_events", "severity", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate audit_events severity: %w", err)
	}
	if err := ensureColumn(ctx, db, "audit_events", "actor", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate audit_events actor: %w", err)
	}
	if err := ensureColumn(ctx, db, "audit_events", "resource_type", "TEXT NOT NULL DEFAULT ''"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate audit_events resource_type: %w", err)
	}
	if err := ensureColumn(ctx, db, "audit_events", "resource_id", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate audit_events resource_id: %w", err)
	}
	for _, column := range []struct {
		name       string
		definition string
	}{
		{name: "proxy_id", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "proxy_name", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "status_family", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "trace_id", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "request_bytes", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "response_bytes", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "input_tokens", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "output_tokens", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "input_cache_tokens", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "request_headers_json", definition: "TEXT NOT NULL DEFAULT '{}'"},
		{name: "request_body_preview", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "response_headers_json", definition: "TEXT NOT NULL DEFAULT '{}'"},
		{name: "response_body_preview", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "preview_truncated", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "is_stream", definition: "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := ensureColumn(ctx, db, "usage_logs", column.name, column.definition); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("migrate usage_logs %s: %w", column.name, err)
		}
	}
	for _, column := range []struct {
		name       string
		definition string
	}{
		{name: "success_input_tokens_sum", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "success_output_tokens_sum", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "success_input_cache_tokens_sum", definition: "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := ensureColumn(ctx, db, "backend_hourly_model_stats", column.name, column.definition); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("migrate backend_hourly_model_stats %s: %w", column.name, err)
		}
	}

	return &Store{db: db}, nil
}

func sqliteDSN(path string) string {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return path + separator + "_foreign_keys=on"
}

func (s *Store) Close() error {
	return s.db.Close()
}

func ensureColumn(ctx context.Context, db *sql.DB, table, column, definition string) error {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid          int
			name         string
			columnType   string
			notNull      int
			defaultValue sql.NullString
			primaryKey   int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

func migrateBackendConsoleCookies(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `
		SELECT id, console_cookie
		FROM backends
		WHERE TRIM(console_cookie) <> ''
			AND (TRIM(console_headers_json) = '' OR TRIM(console_headers_json) = '{}')
	`)
	if err != nil {
		return err
	}
	type legacyCookie struct {
		id     int64
		cookie string
	}
	var cookies []legacyCookie
	for rows.Next() {
		var item legacyCookie
		if err := rows.Scan(&item.id, &item.cookie); err != nil {
			rows.Close()
			return err
		}
		cookies = append(cookies, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range cookies {
		raw, err := json.Marshal(map[string]string{"Cookie": strings.TrimSpace(item.cookie)})
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `UPDATE backends SET console_headers_json = ?, console_cookie = '' WHERE id = ?`, string(raw), item.id); err != nil {
			return err
		}
	}
	return nil
}

func migrateBackendAPIKeys(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `
		SELECT id, api_key, model_list, model_mapping, api_keys_json
		FROM backends
	`)
	if err != nil {
		return err
	}
	type legacyBackendAPIKeys struct {
		id           int64
		apiKey       string
		modelList    string
		modelMapping string
		apiKeysJSON  string
	}
	var items []legacyBackendAPIKeys
	for rows.Next() {
		var item legacyBackendAPIKeys
		if err := rows.Scan(&item.id, &item.apiKey, &item.modelList, &item.modelMapping, &item.apiKeysJSON); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range items {
		if len(decodeBackendAPIKeys(item.apiKeysJSON)) > 0 {
			continue
		}
		legacy := domain.Backend{
			APIKey:       item.apiKey,
			Models:       decodeList(item.modelList),
			ModelMapping: decodeMap(item.modelMapping),
		}
		apiKeys := effectiveBackendAPIKeys(legacy)
		if _, err := db.ExecContext(ctx, `UPDATE backends SET api_keys_json = ? WHERE id = ?`, mustEncodeBackendAPIKeys(apiKeys), item.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Search(ctx context.Context, query string, limit int) (SearchResults, error) {
	results := SearchResults{
		Backends:   []SearchResult{},
		ClientKeys: []SearchResult{},
		Proxies:    []SearchResult{},
		UsageLogs:  []SearchResult{},
		Events:     []SearchResult{},
	}

	term := strings.ToLower(strings.TrimSpace(query))
	if term == "" {
		return results, nil
	}
	like := "%" + term + "%"
	prefix := term + "%"
	limit = normalizeLimit(limit, 5, 20)

	backendRows, err := s.db.QueryContext(ctx, `
		SELECT id, name, base_url, status
		FROM backends
		WHERE lower(name) LIKE ? OR lower(base_url) LIKE ? OR lower(status) LIKE ?
		ORDER BY
			CASE
				WHEN lower(name) = ? THEN 0
				WHEN lower(name) LIKE ? THEN 1
				WHEN lower(base_url) = ? THEN 2
				WHEN lower(base_url) LIKE ? THEN 3
				WHEN lower(status) = ? THEN 4
				WHEN lower(status) LIKE ? THEN 5
				ELSE 6
			END,
			id DESC
		LIMIT ?
	`, like, like, like, term, prefix, term, prefix, term, prefix, limit)
	if err != nil {
		return SearchResults{}, err
	}
	defer backendRows.Close()
	for backendRows.Next() {
		var (
			id      int64
			name    string
			baseURL string
			status  string
		)
		if err := backendRows.Scan(&id, &name, &baseURL, &status); err != nil {
			return SearchResults{}, err
		}
		results.Backends = append(results.Backends, SearchResult{
			Kind:       "backend",
			ID:         id,
			Title:      name,
			Subtitle:   baseURL,
			Meta:       map[string]any{"base_url": baseURL},
			Status:     status,
			TargetPage: "backends",
			TargetID:   id,
		})
	}
	if err := backendRows.Err(); err != nil {
		return SearchResults{}, err
	}

	clientRows, err := s.db.QueryContext(ctx, `
		SELECT id, name, token_prefix, enabled
		FROM client_keys
		WHERE lower(name) LIKE ? OR lower(token_prefix) LIKE ?
		ORDER BY
			CASE
				WHEN lower(name) = ? THEN 0
				WHEN lower(name) LIKE ? THEN 1
				WHEN lower(token_prefix) = ? THEN 2
				WHEN lower(token_prefix) LIKE ? THEN 3
				ELSE 4
			END,
			id DESC
		LIMIT ?
	`, like, like, term, prefix, term, prefix, limit)
	if err != nil {
		return SearchResults{}, err
	}
	defer clientRows.Close()
	for clientRows.Next() {
		var (
			id          int64
			name        string
			tokenPrefix string
			enabled     int
		)
		if err := clientRows.Scan(&id, &name, &tokenPrefix, &enabled); err != nil {
			return SearchResults{}, err
		}
		results.ClientKeys = append(results.ClientKeys, SearchResult{
			Kind:       "client_key",
			ID:         id,
			Title:      name,
			Subtitle:   tokenPrefix,
			Meta:       map[string]any{},
			Status:     enabledStatus(enabled == 1),
			TargetPage: "client-keys",
			TargetID:   id,
		})
	}
	if err := clientRows.Err(); err != nil {
		return SearchResults{}, err
	}

	proxyRows, err := s.db.QueryContext(ctx, `
		SELECT id, name, address, username, enabled
		FROM socks_proxies
		WHERE lower(name) LIKE ? OR lower(address) LIKE ? OR lower(username) LIKE ?
		ORDER BY
			CASE
				WHEN lower(name) = ? THEN 0
				WHEN lower(name) LIKE ? THEN 1
				WHEN lower(address) = ? THEN 2
				WHEN lower(address) LIKE ? THEN 3
				WHEN lower(username) = ? THEN 4
				WHEN lower(username) LIKE ? THEN 5
				ELSE 6
			END,
			id DESC
		LIMIT ?
	`, like, like, like, term, prefix, term, prefix, term, prefix, limit)
	if err != nil {
		return SearchResults{}, err
	}
	defer proxyRows.Close()
	for proxyRows.Next() {
		var (
			id       int64
			name     string
			address  string
			username string
			enabled  int
		)
		if err := proxyRows.Scan(&id, &name, &address, &username, &enabled); err != nil {
			return SearchResults{}, err
		}
		results.Proxies = append(results.Proxies, SearchResult{
			Kind:       "socks_proxy",
			ID:         id,
			Title:      name,
			Subtitle:   address,
			Meta:       map[string]any{"username": username},
			Status:     enabledStatus(enabled == 1),
			TargetPage: "socks-proxies",
			TargetID:   id,
		})
	}
	if err := proxyRows.Err(); err != nil {
		return SearchResults{}, err
	}

	usageRows, err := s.db.QueryContext(ctx, `
		SELECT id, request_id, client_name, model, backend_name, status_code
		FROM usage_logs
		WHERE lower(request_id) LIKE ? OR lower(client_name) LIKE ? OR lower(model) LIKE ? OR lower(backend_name) LIKE ?
		ORDER BY
			CASE
				WHEN lower(request_id) = ? THEN 0
				WHEN lower(request_id) LIKE ? THEN 1
				WHEN lower(client_name) = ? THEN 2
				WHEN lower(client_name) LIKE ? THEN 3
				WHEN lower(model) = ? THEN 4
				WHEN lower(model) LIKE ? THEN 5
				WHEN lower(backend_name) = ? THEN 6
				WHEN lower(backend_name) LIKE ? THEN 7
				ELSE 8
			END,
			id DESC
		LIMIT ?
	`, like, like, like, like, term, prefix, term, prefix, term, prefix, term, prefix, limit)
	if err != nil {
		return SearchResults{}, err
	}
	defer usageRows.Close()
	for usageRows.Next() {
		var (
			id          int64
			requestID   string
			clientName  string
			model       string
			backendName string
			statusCode  int
		)
		if err := usageRows.Scan(&id, &requestID, &clientName, &model, &backendName, &statusCode); err != nil {
			return SearchResults{}, err
		}
		results.UsageLogs = append(results.UsageLogs, SearchResult{
			Kind:       "usage_log",
			ID:         id,
			Title:      requestID,
			Subtitle:   firstNonEmpty(model, clientName),
			Meta:       map[string]any{"client_name": clientName, "backend_name": backendName},
			Status:     strconv.Itoa(statusCode),
			TargetPage: "usage-logs",
			TargetID:   id,
		})
	}
	if err := usageRows.Err(); err != nil {
		return SearchResults{}, err
	}

	eventRows, err := s.db.QueryContext(ctx, `
		SELECT id, type, message, backend_name, model, level
		FROM audit_events
		WHERE lower(type) LIKE ? OR lower(message) LIKE ? OR lower(backend_name) LIKE ? OR lower(model) LIKE ?
		ORDER BY
			CASE
				WHEN lower(type) = ? THEN 0
				WHEN lower(type) LIKE ? THEN 1
				WHEN lower(message) = ? THEN 2
				WHEN lower(message) LIKE ? THEN 3
				WHEN lower(backend_name) = ? THEN 4
				WHEN lower(backend_name) LIKE ? THEN 5
				WHEN lower(model) = ? THEN 6
				WHEN lower(model) LIKE ? THEN 7
				ELSE 8
			END,
			id DESC
		LIMIT ?
	`, like, like, like, like, term, prefix, term, prefix, term, prefix, term, prefix, limit)
	if err != nil {
		return SearchResults{}, err
	}
	defer eventRows.Close()
	for eventRows.Next() {
		var (
			id          int64
			eventType   string
			message     string
			backendName string
			model       string
			level       string
		)
		if err := eventRows.Scan(&id, &eventType, &message, &backendName, &model, &level); err != nil {
			return SearchResults{}, err
		}
		results.Events = append(results.Events, SearchResult{
			Kind:       "event",
			ID:         id,
			Title:      eventType,
			Subtitle:   message,
			Meta:       map[string]any{"backend_name": backendName, "model": model},
			Status:     level,
			TargetPage: "events",
			TargetID:   id,
		})
	}
	if err := eventRows.Err(); err != nil {
		return SearchResults{}, err
	}

	return results, nil
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func parseOptionalTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed := parseTime(value)
	if parsed.IsZero() {
		return nil
	}
	return &parsed
}

func countRows(ctx context.Context, db *sql.DB, table string) (int, error) {
	row := db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table))
	var total int
	if err := row.Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	values := make([]string, count)
	for index := range values {
		values[index] = "?"
	}
	return strings.Join(values, ",")
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatTime(*value)
}

func mustEncodeList(values []string) string {
	normalized := normalizeList(values)
	data, _ := json.Marshal(normalized)
	return string(data)
}

func mustEncodeMap(values map[string]string) string {
	normalized := normalizeMap(values)
	data, _ := json.Marshal(normalized)
	return string(data)
}

func mustEncodeBackendAPIKeys(values []domain.BackendAPIKey) string {
	data, _ := json.Marshal(normalizeBackendAPIKeys(values))
	return string(data)
}

func decodeBackendAPIKeys(raw string) []domain.BackendAPIKey {
	var values []domain.BackendAPIKey
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return []domain.BackendAPIKey{}
	}
	return normalizeBackendAPIKeys(values)
}

func normalizeBackendAPIKeys(values []domain.BackendAPIKey) []domain.BackendAPIKey {
	if len(values) == 0 {
		return []domain.BackendAPIKey{}
	}
	normalized := make([]domain.BackendAPIKey, 0, len(values))
	for _, value := range values {
		value.ID = strings.TrimSpace(value.ID)
		value.APIKey = strings.TrimSpace(value.APIKey)
		value.Name = strings.TrimSpace(value.Name)
		value.Group = strings.TrimSpace(value.Group)
		if value.Group == "" {
			value.Group = "default"
		}
		value.Models = normalizeList(value.Models)
		value.ModelMapping = normalizeMap(value.ModelMapping)
		if value.UsedQuota < 0 || math.IsNaN(value.UsedQuota) || math.IsInf(value.UsedQuota, 0) {
			value.UsedQuota = 0
		}
		normalized = append(normalized, value)
	}
	return normalized
}

func effectiveBackendAPIKeys(backend domain.Backend) []domain.BackendAPIKey {
	if len(backend.APIKeys) > 0 {
		return normalizeBackendAPIKeys(backend.APIKeys)
	}
	if strings.TrimSpace(backend.APIKey) == "" && len(backend.Models) == 0 && len(backend.ModelMapping) == 0 {
		return []domain.BackendAPIKey{}
	}
	return normalizeBackendAPIKeys([]domain.BackendAPIKey{{
		APIKey:       backend.APIKey,
		Group:        "default",
		Models:       backend.Models,
		ModelMapping: backend.ModelMapping,
		UsedQuota:    0,
	}})
}

func legacyBackendRoutingFields(values []domain.BackendAPIKey) (string, []string, map[string]string) {
	values = normalizeBackendAPIKeys(values)
	apiKey := ""
	models := []string{}
	mappings := map[string]string{}
	if len(values) > 0 {
		apiKey = values[0].APIKey
	}
	for _, value := range values {
		models = append(models, value.Models...)
		for clientModel, upstreamModel := range value.ModelMapping {
			if _, exists := mappings[clientModel]; !exists {
				mappings[clientModel] = upstreamModel
			}
		}
	}
	return apiKey, normalizeList(models), mappings
}

func decodeList(raw string) []string {
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil
	}
	return normalizeList(values)
}

func decodeMap(raw string) map[string]string {
	var values map[string]string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return map[string]string{}
	}
	return normalizeMap(values)
}

func normalizeLimit(value, fallback, max int) int {
	if value <= 0 {
		return fallback
	}
	if max > 0 && value > max {
		return max
	}
	return value
}

func enabledStatus(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func normalizeList(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}

	seen := make(map[string]struct{}, len(values))
	var normalized []string
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func normalizeMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	normalized := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		normalized[key] = value
	}
	if len(normalized) == 0 {
		return map[string]string{}
	}
	return normalized
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func ensureSlice[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}
