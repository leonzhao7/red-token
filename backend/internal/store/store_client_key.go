package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"red-token/internal/domain"
)

type ClientKeyUsageSummary struct {
	UsageCount int
	LastUsedAt time.Time
}

type ClientKeyDetailData struct {
	Client domain.ClientKey
	Usage  []domain.UsageLog
	Events []domain.AuditEvent
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Store) FindClientKeyByToken(ctx context.Context, token string) (*domain.ClientKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, token_hash, token, token_prefix, allowed_models, enabled, created_at, updated_at
		FROM client_keys
		WHERE token_hash = ? AND enabled = 1
	`, HashToken(strings.TrimSpace(token)))

	client, err := scanClientKey(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &client, nil
}

func (s *Store) ListClientKeys(ctx context.Context) ([]domain.ClientKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, token_hash, token, token_prefix, allowed_models, enabled, created_at, updated_at
		FROM client_keys
		ORDER BY id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clients []domain.ClientKey
	for rows.Next() {
		client, err := scanClientKey(rows)
		if err != nil {
			return nil, err
		}
		clients = append(clients, client)
	}
	return clients, rows.Err()
}

func (s *Store) CountClientKeys(ctx context.Context) (int, error) {
	return countRows(ctx, s.db, "client_keys")
}

func (s *Store) ListClientKeysPage(ctx context.Context, limit, offset int) ([]domain.ClientKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, token_hash, token, token_prefix, allowed_models, enabled, created_at, updated_at
		FROM client_keys
		ORDER BY id DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clients []domain.ClientKey
	for rows.Next() {
		client, err := scanClientKey(rows)
		if err != nil {
			return nil, err
		}
		clients = append(clients, client)
	}
	return clients, rows.Err()
}

func (s *Store) CreateClientKey(ctx context.Context, client domain.ClientKey) (domain.ClientKey, error) {
	now := time.Now().UTC()
	client.CreatedAt = now
	client.UpdatedAt = now

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO client_keys(name, token_hash, token, token_prefix, allowed_models, enabled, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)
	`,
		strings.TrimSpace(client.Name),
		client.TokenHash,
		strings.TrimSpace(client.Token),
		client.TokenPrefix,
		client.AllowedModels,
		boolToInt(client.Enabled),
		formatTime(now),
		formatTime(now),
	)
	if err != nil {
		return domain.ClientKey{}, err
	}

	client.ID, err = result.LastInsertId()
	return client, err
}

func (s *Store) UpdateClientKey(ctx context.Context, client domain.ClientKey) (domain.ClientKey, error) {
	now := time.Now().UTC()
	client.UpdatedAt = now

	_, err := s.db.ExecContext(ctx, `
		UPDATE client_keys
		SET name = ?, allowed_models = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`,
		strings.TrimSpace(client.Name),
		client.AllowedModels,
		boolToInt(client.Enabled),
		formatTime(now),
		client.ID,
	)
	if err != nil {
		return domain.ClientKey{}, err
	}
	return s.GetClientKey(ctx, client.ID)
}

func (s *Store) GetClientKey(ctx context.Context, id int64) (domain.ClientKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, token_hash, token, token_prefix, allowed_models, enabled, created_at, updated_at
		FROM client_keys
		WHERE id = ?
	`, id)
	return scanClientKey(row)
}

func (s *Store) DeleteClientKey(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM client_keys WHERE id = ?`, id)
	return err
}


func (s *Store) ClientKeyDetail(ctx context.Context, id int64, limit int) (ClientKeyDetailData, error) {
	client, err := s.GetClientKey(ctx, id)
	if err != nil {
		return ClientKeyDetailData{}, err
	}
	usage, err := s.listUsageLogsByClientID(ctx, id, limit)
	if err != nil {
		return ClientKeyDetailData{}, err
	}
	events, err := s.listAuditEventsByClientName(ctx, client.Name, limit)
	if err != nil {
		return ClientKeyDetailData{}, err
	}
	return ClientKeyDetailData{
		Client: client,
		Usage:  ensureSlice(usage),
		Events: ensureSlice(events),
	}, nil
}

func (s *Store) ClientKeyUsageSummaryByIDs(ctx context.Context, ids []int64) (map[int64]ClientKeyUsageSummary, error) {
	if len(ids) == 0 {
		return map[int64]ClientKeyUsageSummary{}, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT client_id, COUNT(*), MAX(created_at)
		FROM usage_logs
		WHERE client_id IN (`+placeholders+`)
		GROUP BY client_id
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int64]ClientKeyUsageSummary, len(ids))
	for rows.Next() {
		var (
			clientID int64
			count    int
			lastUsed string
			summary  ClientKeyUsageSummary
		)
		if err := rows.Scan(&clientID, &count, &lastUsed); err != nil {
			return nil, err
		}
		summary.UsageCount = count
		summary.LastUsedAt = parseTime(lastUsed)
		out[clientID] = summary
	}
	return out, rows.Err()
}

func scanClientKey(s scanner) (domain.ClientKey, error) {
	var (
		client               domain.ClientKey
		createdAt, updatedAt string
		enabled              int
	)
	err := s.Scan(
		&client.ID,
		&client.Name,
		&client.TokenHash,
		&client.Token,
		&client.TokenPrefix,
		&client.AllowedModels,
		&enabled,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return domain.ClientKey{}, err
	}
	client.Enabled = enabled == 1
	client.CreatedAt = parseTime(createdAt)
	client.UpdatedAt = parseTime(updatedAt)
	return client, nil
}

func activityClientKey(log domain.UsageLog) string {
	if log.ClientID > 0 {
		return fmt.Sprintf("id:%d", log.ClientID)
	}
	if name := strings.TrimSpace(log.ClientName); name != "" {
		return "name:" + name
	}
	return ""
}
