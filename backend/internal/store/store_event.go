package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"red-token/internal/domain"
)

type EventSummary struct {
	Total      int
	Categories []CategoryCount
	Severities []CategoryCount
	Actors     []CategoryCount
}

type CategoryCount struct {
	Name  string
	Count int
}

type EventFilter struct {
	Category string
	Severity string
	Actor    string
	Backend  string
	Query    string
	DateFrom time.Time
	DateTo   time.Time
}

func (s *Store) AppendAuditEvent(ctx context.Context, event domain.AuditEvent) error {
	if strings.TrimSpace(event.Message) == "" {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_events(level, type, category, severity, actor, resource_type, resource_id, message, client_name, model, endpoint, backend_name, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		nonEmpty(event.Level, "info"),
		nonEmpty(event.Type, "system"),
		nonEmpty(event.Category, event.Type),
		nonEmpty(event.Severity, nonEmpty(event.Level, "info")),
		event.Actor,
		event.ResourceType,
		event.ResourceID,
		event.Message,
		event.ClientName,
		event.Model,
		event.Endpoint,
		event.BackendName,
		formatTime(time.Now().UTC()),
	)
	return err
}

func (s *Store) ListAuditEvents(ctx context.Context, limit int) ([]domain.AuditEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, level, type, category, severity, actor, resource_type, resource_id, message, client_name, model, endpoint, backend_name, created_at
		FROM audit_events
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []domain.AuditEvent
	for rows.Next() {
		event, err := scanAuditEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) CountAuditEvents(ctx context.Context) (int, error) {
	return countRows(ctx, s.db, "audit_events")
}

func (s *Store) CountAuditEventsFiltered(ctx context.Context, filter EventFilter) (int, error) {
	where, args := eventFilterClause(filter)
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events`+where, args...)
	var total int
	if err := row.Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (s *Store) ListAuditEventsPage(ctx context.Context, limit, offset int) ([]domain.AuditEvent, error) {
	return s.ListAuditEventsPageFiltered(ctx, EventFilter{}, limit, offset)
}

func (s *Store) ListAuditEventsPageFiltered(ctx context.Context, filter EventFilter, limit, offset int) ([]domain.AuditEvent, error) {
	where, args := eventFilterClause(filter)
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, level, type, category, severity, actor, resource_type, resource_id, message, client_name, model, endpoint, backend_name, created_at
		FROM audit_events
	`+where+`
		ORDER BY id DESC
		LIMIT ? OFFSET ?
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []domain.AuditEvent
	for rows.Next() {
		event, err := scanAuditEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) GetAuditEvent(ctx context.Context, id int64) (domain.AuditEvent, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, level, type, category, severity, actor, resource_type, resource_id, message, client_name, model, endpoint, backend_name, created_at
		FROM audit_events
		WHERE id = ?
	`, id)
	return scanAuditEvent(row)
}

func (s *Store) ClearAuditEvents(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM audit_events`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) EventSummary(ctx context.Context, filter EventFilter) (EventSummary, error) {
	where, args := eventFilterClause(filter)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, level, type, category, severity, actor, resource_type, resource_id, message, client_name, model, endpoint, backend_name, created_at
		FROM audit_events
	`+where+`
		ORDER BY id DESC
	`, args...)
	if err != nil {
		return EventSummary{}, err
	}
	defer rows.Close()

	var (
		total          int
		categoryOrder  []string
		severityOrder  []string
		actorOrder     []string
		categoryCounts = make(map[string]int)
		severityCounts = make(map[string]int)
		actorCounts    = make(map[string]int)
	)

	for rows.Next() {
		event, err := scanAuditEvent(rows)
		if err != nil {
			return EventSummary{}, err
		}
		total++
		category := nonEmpty(strings.TrimSpace(event.Category), nonEmpty(strings.TrimSpace(event.Type), "system"))
		severity := normalizeEventSeverity(nonEmpty(strings.TrimSpace(event.Severity), nonEmpty(strings.TrimSpace(event.Level), "info")))
		actor := nonEmpty(strings.TrimSpace(event.Actor), nonEmpty(strings.TrimSpace(event.ClientName), "system"))
		if _, ok := categoryCounts[category]; !ok {
			categoryOrder = append(categoryOrder, category)
		}
		if _, ok := severityCounts[severity]; !ok {
			severityOrder = append(severityOrder, severity)
		}
		if _, ok := actorCounts[actor]; !ok {
			actorOrder = append(actorOrder, actor)
		}
		categoryCounts[category]++
		severityCounts[severity]++
		actorCounts[actor]++
	}
	if err := rows.Err(); err != nil {
		return EventSummary{}, err
	}

	return EventSummary{
		Total:      total,
		Categories: orderedCategoryCounts(categoryOrder, categoryCounts),
		Severities: orderedCategoryCounts(severityOrder, severityCounts),
		Actors:     orderedCategoryCounts(actorOrder, actorCounts),
	}, nil
}

func eventFilterClause(filter EventFilter) (string, []any) {
	var (
		clauses []string
		args    []any
	)
	if value := strings.TrimSpace(filter.Category); value != "" {
		clauses = append(clauses, `(category = ? OR type = ?)`)
		args = append(args, value, value)
	}
	if value := strings.TrimSpace(filter.Severity); value != "" {
		normalized := normalizeEventSeverity(value)
		alternatives := []string{normalized}
		if normalized == "warning" {
			alternatives = append(alternatives, "warn")
		}
		if normalized == "warn" {
			alternatives = append(alternatives, "warning")
		}
		clauses = append(clauses, `(severity IN (`+strings.TrimSuffix(strings.Repeat("?,", len(alternatives)), ",")+`) OR level IN (`+strings.TrimSuffix(strings.Repeat("?,", len(alternatives)), ",")+`))`)
		for _, item := range alternatives {
			args = append(args, item)
		}
		for _, item := range alternatives {
			args = append(args, item)
		}
	}
	if value := strings.TrimSpace(filter.Actor); value != "" {
		clauses = append(clauses, `(actor = ? OR client_name = ?)`)
		args = append(args, value, value)
	}
	if value := strings.TrimSpace(filter.Backend); value != "" {
		clauses = append(clauses, `backend_name = ?`)
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Query); value != "" {
		like := "%" + strings.ToLower(value) + "%"
		clauses = append(clauses, `(lower(type) LIKE ? OR lower(category) LIKE ? OR lower(message) LIKE ? OR lower(client_name) LIKE ? OR lower(model) LIKE ? OR lower(backend_name) LIKE ?)`)
		args = append(args, like, like, like, like, like, like)
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

func normalizeEventSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "warn", "warning":
		return "warning"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func scanAuditEvent(s scanner) (domain.AuditEvent, error) {
	var (
		event     domain.AuditEvent
		createdAt string
	)
	err := s.Scan(
		&event.ID,
		&event.Level,
		&event.Type,
		&event.Category,
		&event.Severity,
		&event.Actor,
		&event.ResourceType,
		&event.ResourceID,
		&event.Message,
		&event.ClientName,
		&event.Model,
		&event.Endpoint,
		&event.BackendName,
		&createdAt,
	)
	if err != nil {
		return domain.AuditEvent{}, err
	}
	event.CreatedAt = parseTime(createdAt)
	return event, nil
}

func (s *Store) listAuditEventsByBackendName(ctx context.Context, backendName string, limit int) ([]domain.AuditEvent, error) {
	return s.listAuditEventsByColumn(ctx, "backend_name", backendName, limit)
}

func (s *Store) listAuditEventsByClientName(ctx context.Context, clientName string, limit int) ([]domain.AuditEvent, error) {
	return s.listAuditEventsByColumn(ctx, "client_name", clientName, limit)
}

func (s *Store) listAuditEventsByModel(ctx context.Context, model string, limit int) ([]domain.AuditEvent, error) {
	return s.listAuditEventsByColumn(ctx, "model", model, limit)
}

func (s *Store) listAuditEventsByColumn(ctx context.Context, column, value string, limit int) ([]domain.AuditEvent, error) {
	switch column {
	case "backend_name", "client_name", "model":
	default:
		return nil, fmt.Errorf("unsupported audit event lookup column %q", column)
	}

	value = strings.TrimSpace(value)
	if value == "" {
		return []domain.AuditEvent{}, nil
	}
	limit = normalizeLimit(limit, 10, 100)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, level, type, category, severity, actor, resource_type, resource_id, message, client_name, model, endpoint, backend_name, created_at
		FROM audit_events
		WHERE `+column+` = ?
		ORDER BY id DESC
		LIMIT ?
	`, value, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []domain.AuditEvent
	for rows.Next() {
		event, err := scanAuditEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func eventCategorySummary(events []domain.AuditEvent) []DashboardActivitySummary {
	if len(events) == 0 {
		return []DashboardActivitySummary{}
	}

	counts := make(map[string]int)
	order := make([]string, 0)
	for _, event := range events {
		category := strings.TrimSpace(event.Type)
		if category == "" {
			category = strings.TrimSpace(event.Level)
		}
		if category == "" {
			category = "system"
		}
		if _, ok := counts[category]; !ok {
			order = append(order, category)
		}
		counts[category]++
	}

	summary := make([]DashboardActivitySummary, 0, len(order))
	for _, category := range order {
		summary = append(summary, DashboardActivitySummary{
			Category: category,
			Count:    counts[category],
		})
	}
	return summary
}

func orderedCategoryCounts(order []string, counts map[string]int) []CategoryCount {
	if len(order) == 0 {
		return []CategoryCount{}
	}
	items := make([]CategoryCount, 0, len(order))
	for _, name := range order {
		items = append(items, CategoryCount{Name: name, Count: counts[name]})
	}
	return items
}
