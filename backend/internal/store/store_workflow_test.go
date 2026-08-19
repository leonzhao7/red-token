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
		ConsoleHeaders: map[string]string{
			"Cookie": "session=old",
		},
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

	firstResult, err := st.UpsertHTTPWorkflowResult(ctx, "first", backend.ID, json.RawMessage(`{"quota":1}`))
	if err != nil {
		t.Fatalf("create workflow result: %v", err)
	}
	if firstResult.WorkflowID != "first" || firstResult.BackendID != backend.ID || string(firstResult.Output) != `{"quota":1}` {
		t.Fatalf("unexpected first result: %+v", firstResult)
	}
	secondResult, err := st.UpsertHTTPWorkflowResult(ctx, "first", backend.ID, json.RawMessage(`{"quota":2}`))
	if err != nil {
		t.Fatalf("update workflow result: %v", err)
	}
	if string(secondResult.Output) != `{"quota":2}` {
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

func TestBackendManualCheckinPersistsAndPatches(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "manual-checkin.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	defaultBackend, err := st.CreateBackend(ctx, domain.Backend{
		Name:    "manual-checkin-default",
		BaseURL: "https://api.example.com",
	})
	if err != nil {
		t.Fatalf("create default backend: %v", err)
	}
	if defaultBackend.ManualCheckin {
		t.Fatalf("manual_checkin default = true, want false")
	}

	backend, err := st.CreateBackend(ctx, domain.Backend{
		Name:          "manual-checkin-enabled",
		BaseURL:       "https://api.example.com",
		ManualCheckin: true,
	})
	if err != nil {
		t.Fatalf("create enabled backend: %v", err)
	}
	if !backend.ManualCheckin {
		t.Fatal("manual_checkin was not persisted on create")
	}

	disabled := false
	backend, err = st.PatchBackend(ctx, backend.ID, BackendPatch{ManualCheckin: &disabled})
	if err != nil {
		t.Fatalf("disable manual_checkin: %v", err)
	}
	if backend.ManualCheckin {
		t.Fatal("manual_checkin was not persisted on patch")
	}
}

func TestApplyHTTPWorkflowResultUpdatesBackendAndSnapshotAtomically(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "workflow-apply.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	backend, err := st.CreateBackend(ctx, domain.Backend{
		Name:       "workflow-backend",
		BaseURL:    "https://api.example.com",
		ConsoleURL: "https://console.example.com",
		APIKeys: []domain.BackendAPIKey{{
			APIKey: "old-key", Group: "old", Models: []string{"old-model"}, ModelMapping: map[string]string{},
		}},
		ConsoleAccountJSON: `{"quota":1}`,
		ConsolePricingJSON: `{"data":[]}`,
	})
	if err != nil {
		t.Fatalf("create backend: %v", err)
	}
	workflow, err := st.CreateHTTPWorkflow(ctx, "apply", "Apply", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	updated := backend
	updated.APIKeys = []domain.BackendAPIKey{{APIKey: "new-key", Group: "new", Models: []string{"new-model"}}}
	updated.ConsoleAccountJSON = `{"quota":9}`
	updated.ConsolePricingJSON = `{"data":[{"model_name":"new-model"}]}`
	updated.ConsoleHeaders = map[string]string{"Cookie": "session=new", "X-Test": "configured"}
	output := json.RawMessage(`{"quota":9}`)
	gotBackend, gotResult, err := st.ApplyHTTPWorkflowResult(ctx, workflow.ID, updated, output)
	if err != nil {
		t.Fatalf("apply workflow result: %v", err)
	}
	if gotBackend.APIKey != "new-key" || gotBackend.ConsoleAccountJSON != updated.ConsoleAccountJSON || gotBackend.ConsolePricingJSON != updated.ConsolePricingJSON || gotBackend.ConsoleHeaders["Cookie"] != "session=new" || gotBackend.ConsoleHeaders["X-Test"] != "configured" {
		t.Fatalf("backend fields were not applied: %+v", gotBackend)
	}
	if string(gotResult.Output) != string(output) || gotResult.BackendID != backend.ID {
		t.Fatalf("snapshot was not applied: %+v", gotResult)
	}

	rollbackCandidate := gotBackend
	rollbackCandidate.APIKeys = []domain.BackendAPIKey{{APIKey: "should-rollback", Group: "rollback", Models: []string{"rollback-model"}}}
	rollbackCandidate.ConsoleAccountJSON = `{"quota":999}`
	rollbackCandidate.ConsoleHeaders = map[string]string{"Cookie": "session=should-rollback"}
	if _, _, err := st.ApplyHTTPWorkflowResult(ctx, "missing-workflow", rollbackCandidate, json.RawMessage(`{"quota":999}`)); err == nil {
		t.Fatal("expected missing workflow foreign key to fail")
	}
	afterRollback, err := st.GetBackend(ctx, backend.ID)
	if err != nil {
		t.Fatalf("get backend after rollback: %v", err)
	}
	if afterRollback.APIKey != gotBackend.APIKey || afterRollback.ConsoleAccountJSON != gotBackend.ConsoleAccountJSON || afterRollback.ConsoleHeaders["Cookie"] != gotBackend.ConsoleHeaders["Cookie"] {
		t.Fatalf("failed snapshot write did not roll back backend: before=%+v after=%+v", gotBackend, afterRollback)
	}
}
