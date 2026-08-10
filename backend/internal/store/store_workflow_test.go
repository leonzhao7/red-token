package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"red-token/internal/domain"
)

func TestHTTPWorkflowStoreCRUDResultsAndCascades(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "workflows.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	backend, err := st.CreateBackend(ctx, domain.Backend{
		Name:       "workflow-backend",
		BaseURL:    "https://api.example.com",
		ConsoleURL: "https://console.example.com",
	})
	if err != nil {
		t.Fatalf("create backend: %v", err)
	}

	firstDefinition := json.RawMessage(`{"spec":"http-workflow/v1","id":"first","name":"First","steps":[],"output":{}}`)
	first, err := st.CreateHTTPWorkflow(ctx, "first", "First", firstDefinition)
	if err != nil {
		t.Fatalf("create first workflow: %v", err)
	}
	if first.ID != "first" || first.Name != "First" || first.CreatedAt.IsZero() || first.UpdatedAt.IsZero() {
		t.Fatalf("unexpected created workflow: %+v", first)
	}
	if _, err := st.CreateHTTPWorkflow(ctx, "second", "Second", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("create second workflow: %v", err)
	}
	if total, err := st.CountHTTPWorkflows(ctx); err != nil || total != 2 {
		t.Fatalf("count workflows: total=%d err=%v", total, err)
	}
	page, err := st.ListHTTPWorkflowsPage(ctx, 1, 0)
	if err != nil {
		t.Fatalf("list workflow page: %v", err)
	}
	if len(page) != 1 {
		t.Fatalf("expected one workflow, got %d", len(page))
	}

	updatedDefinition := json.RawMessage(`{"spec":"http-workflow/v1","id":"first","name":"Updated","steps":[],"output":{}}`)
	updated, err := st.UpdateHTTPWorkflow(ctx, "first", "Updated", updatedDefinition)
	if err != nil {
		t.Fatalf("update workflow: %v", err)
	}
	if updated.Name != "Updated" || string(updated.Definition) != string(updatedDefinition) {
		t.Fatalf("unexpected updated workflow: %+v", updated)
	}
	if !updated.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("created_at changed: before=%s after=%s", first.CreatedAt, updated.CreatedAt)
	}
	if _, err := st.UpdateHTTPWorkflow(ctx, "missing", "Missing", json.RawMessage(`{}`)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected missing update to return sql.ErrNoRows, got %v", err)
	}

	firstResult, err := st.UpsertHTTPWorkflowResult(ctx, "first", backend.ID, json.RawMessage(`{"balance":1}`))
	if err != nil {
		t.Fatalf("create workflow result: %v", err)
	}
	if firstResult.WorkflowID != "first" || firstResult.BackendID != backend.ID || string(firstResult.Output) != `{"balance":1}` {
		t.Fatalf("unexpected first result: %+v", firstResult)
	}
	secondResult, err := st.UpsertHTTPWorkflowResult(ctx, "first", backend.ID, json.RawMessage(`{"balance":2}`))
	if err != nil {
		t.Fatalf("update workflow result: %v", err)
	}
	if string(secondResult.Output) != `{"balance":2}` {
		t.Fatalf("result was not replaced: %s", secondResult.Output)
	}
	if _, err := st.UpsertHTTPWorkflowResult(ctx, "first", backend.ID+999, json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected result with missing backend to violate foreign key")
	}

	if err := st.DeleteHTTPWorkflow(ctx, "first"); err != nil {
		t.Fatalf("delete workflow: %v", err)
	}
	if _, err := st.GetHTTPWorkflowResult(ctx, "first", backend.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected workflow delete to cascade result, got %v", err)
	}
	if err := st.DeleteHTTPWorkflow(ctx, "first"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected repeated delete to return sql.ErrNoRows, got %v", err)
	}

	if _, err := st.CreateHTTPWorkflow(ctx, "backend-cascade", "Backend cascade", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("create cascade workflow: %v", err)
	}
	if _, err := st.UpsertHTTPWorkflowResult(ctx, "backend-cascade", backend.ID, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("create cascade result: %v", err)
	}
	if err := st.DeleteBackend(ctx, backend.ID); err != nil {
		t.Fatalf("delete backend: %v", err)
	}
	if _, err := st.GetHTTPWorkflowResult(ctx, "backend-cascade", backend.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected backend delete to cascade result, got %v", err)
	}
}
