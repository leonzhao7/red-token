package store

import (
	"context"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"red-token/internal/domain"
)

type ProxyUsageSummary struct {
	RequestCount int
	TrafficBytes int64
	AvgLatencyMS float64
	LastUsedAt   time.Time
}

type SocksProxyDetailData struct {
	Proxy    domain.SocksProxy
	Backends []domain.Backend
	Usage    []domain.UsageLog
}

func (s *Store) ListSocksProxies(ctx context.Context) ([]domain.SocksProxy, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, address, username, password, enabled, created_at, updated_at
		FROM socks_proxies
		ORDER BY id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var proxies []domain.SocksProxy
	for rows.Next() {
		proxy, err := scanSocksProxy(rows)
		if err != nil {
			return nil, err
		}
		proxies = append(proxies, proxy)
	}
	return proxies, rows.Err()
}

func (s *Store) CountSocksProxies(ctx context.Context) (int, error) {
	return countRows(ctx, s.db, "socks_proxies")
}

func (s *Store) ListSocksProxiesPage(ctx context.Context, limit, offset int) ([]domain.SocksProxy, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, address, username, password, enabled, created_at, updated_at
		FROM socks_proxies
		ORDER BY id DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var proxies []domain.SocksProxy
	for rows.Next() {
		proxy, err := scanSocksProxy(rows)
		if err != nil {
			return nil, err
		}
		proxies = append(proxies, proxy)
	}
	return proxies, rows.Err()
}

func (s *Store) CreateSocksProxy(ctx context.Context, proxy domain.SocksProxy) (domain.SocksProxy, error) {
	now := time.Now().UTC()
	proxy.CreatedAt = now
	proxy.UpdatedAt = now

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO socks_proxies(name, address, username, password, enabled, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)
	`,
		strings.TrimSpace(proxy.Name),
		strings.TrimSpace(proxy.Address),
		strings.TrimSpace(proxy.Username),
		strings.TrimSpace(proxy.Password),
		boolToInt(proxy.Enabled),
		formatTime(now),
		formatTime(now),
	)
	if err != nil {
		return domain.SocksProxy{}, err
	}

	proxy.ID, err = result.LastInsertId()
	return proxy, err
}

func (s *Store) UpdateSocksProxy(ctx context.Context, proxy domain.SocksProxy) (domain.SocksProxy, error) {
	now := time.Now().UTC()
	proxy.UpdatedAt = now

	_, err := s.db.ExecContext(ctx, `
		UPDATE socks_proxies
		SET name = ?, address = ?, username = ?, password = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`,
		strings.TrimSpace(proxy.Name),
		strings.TrimSpace(proxy.Address),
		strings.TrimSpace(proxy.Username),
		strings.TrimSpace(proxy.Password),
		boolToInt(proxy.Enabled),
		formatTime(now),
		proxy.ID,
	)
	if err != nil {
		return domain.SocksProxy{}, err
	}
	return s.GetSocksProxy(ctx, proxy.ID)
}

func (s *Store) GetSocksProxy(ctx context.Context, id int64) (domain.SocksProxy, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, address, username, password, enabled, created_at, updated_at
		FROM socks_proxies
		WHERE id = ?
	`, id)
	return scanSocksProxy(row)
}

func (s *Store) DeleteSocksProxy(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `UPDATE backends SET proxy_id = 0 WHERE proxy_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM socks_proxies WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SocksProxyDetail(ctx context.Context, id int64, limit int) (SocksProxyDetailData, error) {
	proxy, err := s.GetSocksProxy(ctx, id)
	if err != nil {
		return SocksProxyDetailData{}, err
	}
	backends, err := s.listBackendsByProxyID(ctx, id)
	if err != nil {
		return SocksProxyDetailData{}, err
	}
	usage, err := s.listUsageLogsByProxyID(ctx, id, limit)
	if err != nil {
		return SocksProxyDetailData{}, err
	}
	return SocksProxyDetailData{
		Proxy:    proxy,
		Backends: ensureSlice(backends),
		Usage:    ensureSlice(usage),
	}, nil
}

func (s *Store) ProxyUsageSummaryByIDs(ctx context.Context, ids []int64) (map[int64]ProxyUsageSummary, error) {
	if len(ids) == 0 {
		return map[int64]ProxyUsageSummary{}, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT proxy_id, COUNT(*), SUM(request_bytes + response_bytes), AVG(duration_ms), MAX(created_at)
		FROM usage_logs
		WHERE proxy_id IN (`+placeholders+`)
		GROUP BY proxy_id
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int64]ProxyUsageSummary, len(ids))
	for rows.Next() {
		var (
			proxyID    int64
			requests   int
			traffic    int64
			avgLatency float64
			lastUsed   string
		)
		if err := rows.Scan(&proxyID, &requests, &traffic, &avgLatency, &lastUsed); err != nil {
			return nil, err
		}
		out[proxyID] = ProxyUsageSummary{
			RequestCount: requests,
			TrafficBytes: traffic,
			AvgLatencyMS: avgLatency,
			LastUsedAt:   parseTime(lastUsed),
		}
	}
	return out, rows.Err()
}

func scanSocksProxy(s scanner) (domain.SocksProxy, error) {
	var (
		proxy                domain.SocksProxy
		createdAt, updatedAt string
		enabled              int
	)
	err := s.Scan(
		&proxy.ID,
		&proxy.Name,
		&proxy.Address,
		&proxy.Username,
		&proxy.Password,
		&enabled,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return domain.SocksProxy{}, err
	}
	proxy.Enabled = enabled == 1
	proxy.CreatedAt = parseTime(createdAt)
	proxy.UpdatedAt = parseTime(updatedAt)
	return proxy, nil
}
