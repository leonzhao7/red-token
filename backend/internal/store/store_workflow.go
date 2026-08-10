package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// HTTPWorkflow is a validated http-workflow/v1 definition stored by its
// stable, configuration-supplied ID.
type HTTPWorkflow struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Definition json.RawMessage `json:"definition"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// HTTPWorkflowResult is the latest successful business output for one
// workflow/backend pair. Runtime helper aliases are intentionally not part of
// the persisted snapshot.
type HTTPWorkflowResult struct {
	WorkflowID string          `json:"workflow_id"`
	BackendID  int64           `json:"backend_id"`
	Output     json.RawMessage `json:"output"`
	ExecutedAt time.Time       `json:"executed_at"`
}

func (s *Store) CountHTTPWorkflows(ctx context.Context) (int, error) {
	return countRows(ctx, s.db, "workflow_definitions")
}

func (s *Store) ListHTTPWorkflowsPage(ctx context.Context, limit, offset int) ([]HTTPWorkflow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, definition_json, created_at, updated_at
		FROM workflow_definitions
		ORDER BY updated_at DESC, id ASC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	workflows := make([]HTTPWorkflow, 0)
	for rows.Next() {
		workflow, err := scanHTTPWorkflow(rows)
		if err != nil {
			return nil, err
		}
		workflows = append(workflows, workflow)
	}
	return workflows, rows.Err()
}

func (s *Store) GetHTTPWorkflow(ctx context.Context, id string) (HTTPWorkflow, error) {
	return scanHTTPWorkflow(s.db.QueryRowContext(ctx, `
		SELECT id, name, definition_json, created_at, updated_at
		FROM workflow_definitions
		WHERE id = ?
	`, id))
}

func (s *Store) CreateHTTPWorkflow(ctx context.Context, id, name string, definition json.RawMessage) (HTTPWorkflow, error) {
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO workflow_definitions(id, name, definition_json, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?)
	`, id, name, string(definition), formatTime(now), formatTime(now)); err != nil {
		return HTTPWorkflow{}, err
	}
	return s.GetHTTPWorkflow(ctx, id)
}

func (s *Store) UpdateHTTPWorkflow(ctx context.Context, id, name string, definition json.RawMessage) (HTTPWorkflow, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
		UPDATE workflow_definitions
		SET name = ?, definition_json = ?, updated_at = ?
		WHERE id = ?
	`, name, string(definition), formatTime(now), id)
	if err != nil {
		return HTTPWorkflow{}, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return HTTPWorkflow{}, err
	} else if affected == 0 {
		return HTTPWorkflow{}, sql.ErrNoRows
	}
	return s.GetHTTPWorkflow(ctx, id)
}

func (s *Store) DeleteHTTPWorkflow(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM workflow_definitions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) UpsertHTTPWorkflowResult(ctx context.Context, workflowID string, backendID int64, output json.RawMessage) (HTTPWorkflowResult, error) {
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO workflow_results(workflow_id, backend_id, output_json, executed_at)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(workflow_id, backend_id) DO UPDATE SET
			output_json = excluded.output_json,
			executed_at = excluded.executed_at
	`, workflowID, backendID, string(output), formatTime(now)); err != nil {
		return HTTPWorkflowResult{}, err
	}
	return s.GetHTTPWorkflowResult(ctx, workflowID, backendID)
}

func (s *Store) GetHTTPWorkflowResult(ctx context.Context, workflowID string, backendID int64) (HTTPWorkflowResult, error) {
	var (
		result     HTTPWorkflowResult
		outputJSON string
		executedAt string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT workflow_id, backend_id, output_json, executed_at
		FROM workflow_results
		WHERE workflow_id = ? AND backend_id = ?
	`, workflowID, backendID).Scan(&result.WorkflowID, &result.BackendID, &outputJSON, &executedAt)
	if err != nil {
		return HTTPWorkflowResult{}, err
	}
	result.Output = json.RawMessage(outputJSON)
	result.ExecutedAt = parseTime(executedAt)
	return result, nil
}

func scanHTTPWorkflow(row scanner) (HTTPWorkflow, error) {
	var (
		workflow   HTTPWorkflow
		definition string
		createdAt  string
		updatedAt  string
	)
	if err := row.Scan(&workflow.ID, &workflow.Name, &definition, &createdAt, &updatedAt); err != nil {
		return HTTPWorkflow{}, err
	}
	workflow.Definition = json.RawMessage(definition)
	workflow.CreatedAt = parseTime(createdAt)
	workflow.UpdatedAt = parseTime(updatedAt)
	return workflow, nil
}
