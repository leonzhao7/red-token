package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"red-token/internal/domain"
)

func TestClientKeyUsageStatsCountOnlyFinalResultsAndSurviveLogClear(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "client-key-stats.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	client, err := st.CreateClientKey(ctx, domain.ClientKey{
		Name:        "client-a",
		TokenHash:   HashToken("client-a-token"),
		Token:       "client-a-token",
		TokenPrefix: "client-a",
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("create client key: %v", err)
	}

	attempt := domain.UsageLog{
		RequestID:    "request-1",
		ClientID:     client.ID,
		ClientName:   client.Name,
		StatusCode:   502,
		InputTokens:  100,
		OutputTokens: 50,
	}
	if err := st.AppendUsageLog(ctx, attempt); err != nil {
		t.Fatalf("append attempt usage log: %v", err)
	}
	assertClientKeyUsageSummary(t, st, client.ID, ClientKeyUsageSummary{})

	finalSuccess := attempt
	finalSuccess.StatusCode = 200
	finalSuccess.InputTokens = 10
	finalSuccess.OutputTokens = 20
	if err := st.AppendFinalUsageLog(ctx, finalSuccess); err != nil {
		t.Fatalf("append final success usage log: %v", err)
	}

	finalFailure := attempt
	finalFailure.RequestID = "request-2"
	finalFailure.StatusCode = 503
	finalFailure.InputTokens = 3
	finalFailure.OutputTokens = 4
	if err := st.RecordClientKeyUsage(ctx, finalFailure); err != nil {
		t.Fatalf("record final failure without usage log: %v", err)
	}

	want := ClientKeyUsageSummary{
		UsageCount:        2,
		RequestSuccesses:  1,
		RequestFailures:   1,
		InputTokensTotal:  13,
		OutputTokensTotal: 24,
	}
	assertClientKeyUsageSummary(t, st, client.ID, want)

	if _, err := st.ClearUsageLogs(ctx); err != nil {
		t.Fatalf("clear usage logs: %v", err)
	}
	assertClientKeyUsageSummary(t, st, client.ID, want)

	finalSuccess.RequestID = "request-3"
	finalSuccess.InputTokens = 1
	finalSuccess.OutputTokens = 2
	if err := st.AppendFinalUsageLog(ctx, finalSuccess); err != nil {
		t.Fatalf("append later final success usage log: %v", err)
	}
	want.UsageCount++
	want.RequestSuccesses++
	want.InputTokensTotal++
	want.OutputTokensTotal += 2
	assertClientKeyUsageSummary(t, st, client.ID, want)
}

func assertClientKeyUsageSummary(t *testing.T, st *Store, clientID int64, want ClientKeyUsageSummary) {
	t.Helper()

	summaries, err := st.ClientKeyUsageSummaryByIDs(context.Background(), []int64{clientID})
	if err != nil {
		t.Fatalf("load client key usage summary: %v", err)
	}
	got := summaries[clientID]
	if got.UsageCount != want.UsageCount ||
		got.RequestSuccesses != want.RequestSuccesses ||
		got.RequestFailures != want.RequestFailures ||
		got.InputTokensTotal != want.InputTokensTotal ||
		got.OutputTokensTotal != want.OutputTokensTotal {
		t.Fatalf("unexpected client key usage summary: got %+v, want %+v", got, want)
	}
}

func TestUsageLogStoresActualAndRequestedModels(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "usage-models.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	entry := domain.UsageLog{
		RequestID:      "request-models",
		Model:          "provider-model",
		RequestedModel: "client-model",
		StatusCode:     200,
	}
	if err := st.AppendFinalUsageLog(ctx, entry); err != nil {
		t.Fatalf("append usage log: %v", err)
	}

	logs, err := st.ListUsageLogsPage(ctx, 10, 0)
	if err != nil {
		t.Fatalf("list usage logs: %v", err)
	}
	if len(logs) != 1 || logs[0].Model != "provider-model" || logs[0].RequestedModel != "client-model" {
		t.Fatalf("unexpected usage models: %+v", logs)
	}

	count, err := st.CountUsageLogsFiltered(ctx, UsageLogFilter{Model: "provider-model"})
	if err != nil {
		t.Fatalf("filter usage logs by actual model: %v", err)
	}
	if count != 1 {
		t.Fatalf("actual model filter count = %d, want 1", count)
	}
}

func TestBackendHourlyModelStatsUseActualModel(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "hourly-models.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if err := st.AppendFinalUsageLog(ctx, domain.UsageLog{
		Model:          "provider-model",
		RequestedModel: "client-model",
		BackendID:      42,
		BackendName:    "relay",
		StatusCode:     200,
		CreatedAt:      time.Now().UTC(),
	}); err != nil {
		t.Fatalf("append usage log: %v", err)
	}

	stats, err := st.ListBackendHourlyModelStats(ctx, BackendHourlyModelStatsFilter{Model: "provider-model"})
	if err != nil {
		t.Fatalf("list hourly model stats: %v", err)
	}
	if len(stats.Rows) != 1 || stats.Rows[0].Model != "provider-model" {
		t.Fatalf("unexpected hourly model stats: %+v", stats.Rows)
	}
}

func TestOpenMigratesLegacyUsageModelsForDashboardStats(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy-usage-models.db")
	st, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	backend, err := st.CreateBackend(ctx, domain.Backend{
		Name: "relay",
		APIKeys: []domain.BackendAPIKey{{
			APIKey:       "backend-key",
			Models:       []string{"client-model"},
			ModelMapping: map[string]string{"client-model": "provider-model"},
		}},
	})
	if err != nil {
		_ = st.Close()
		t.Fatalf("create backend: %v", err)
	}
	createdAt := time.Now().UTC().Add(-time.Hour)
	_, err = st.db.ExecContext(ctx, `
		INSERT INTO usage_logs(request_id, model, backend_id, backend_name, status_code, created_at)
		VALUES(?, ?, ?, ?, ?, ?)
	`, "legacy-request", "client-model", backend.ID, backend.Name, 200, formatTime(createdAt))
	if err != nil {
		_ = st.Close()
		t.Fatalf("insert legacy usage log: %v", err)
	}
	if _, err = st.db.ExecContext(ctx, `
		INSERT INTO backend_hourly_model_stats(backend_id, backend_name, model, hour_start_utc, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?)
	`, backend.ID, backend.Name, "client-model", formatTime(createdAt.Truncate(time.Hour)), formatTime(createdAt), formatTime(createdAt)); err != nil {
		_ = st.Close()
		t.Fatalf("insert legacy hourly stats: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close legacy store: %v", err)
	}

	st, err = Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	log, err := st.GetUsageLog(ctx, 1)
	if err != nil {
		t.Fatalf("load migrated usage log: %v", err)
	}
	if log.Model != "provider-model" || log.RequestedModel != "client-model" {
		t.Fatalf("unexpected migrated usage models: %+v", log)
	}
	stats, err := st.ListBackendHourlyModelStats(ctx, BackendHourlyModelStatsFilter{Model: "provider-model"})
	if err != nil {
		t.Fatalf("load migrated hourly stats: %v", err)
	}
	if len(stats.Rows) != 1 || stats.Rows[0].Model != "provider-model" {
		t.Fatalf("unexpected migrated hourly stats: %+v", stats.Rows)
	}
}
