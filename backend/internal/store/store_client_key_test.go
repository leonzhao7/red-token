package store

import (
	"context"
	"path/filepath"
	"testing"

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
