package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"red-token/internal/config"
	"red-token/internal/domain"
	"red-token/internal/service"
)

func TestValidateBackendAPIKeysAllowsEmptyList(t *testing.T) {
	apiKeys, err := validateBackendAPIKeys(nil, "", nil, nil)
	if err != nil {
		t.Fatalf("validate empty api key list: %v", err)
	}
	if len(apiKeys) != 0 {
		t.Fatalf("expected empty api key list, got %d items", len(apiKeys))
	}
}

func TestBuildBackendFrontendViewNormalizesContract(t *testing.T) {
	backend := domain.Backend{
		ID:       1,
		Name:     "relay",
		Protocol: domain.BackendProtocolBoth,
		BaseURL:  "https://relay.example/v1",
		APIKeys: []domain.BackendAPIKey{{
			ID:           "56382",
			APIKey:       "sk-value",
			Name:         "main",
			Group:        "default",
			Models:       []string{"model-a"},
			ModelMapping: map[string]string{"model-a": "upstream-a"},
			UsedQuota:    500000,
		}},
		ConsoleAccountJSON: `{"id":49722,"username":"alice","quota":1300000000,"used_quota":500000,"last_checkin_reward":500000,"quota_per_unit":500000,"quota_display_type":"USD","last_checkin_at":"2026-08-14T17:12:21Z"}`,
		ConsolePricingJSON: `{"group_ratio":{"default":1,"partner":1,"vip":2},"data":[{"model_name":"usage-model","quota_type":0,"model_ratio":0.25,"completion_ratio":8,"enable_groups":["default","partner","vip"]},{"model_name":"fixed-model","quota_type":1,"model_price":1.75,"enable_groups":["default"]},{"model_name":"tiered-model","quota_type":0,"model_ratio":99,"completion_ratio":99,"enable_groups":["default"],"billing_mode":"tiered_expr","billing_expr":"tier(\"short_context\", p * 5 + c * 30 + cr * 0.5)"}]}`,
		ConsoleHeaders:     map[string]string{"Cookie": "session=value"},
		ManualCheckin:      true,
		Status:             domain.BackendStatusNormal,
		Tags:               []string{},
	}
	view := buildBackendFrontendView(backend, 12.5)
	if len(view.APIKeys) != 1 || view.APIKeys[0].ID != "56382" || view.APIKeys[0].Key != "sk-value" {
		t.Fatalf("unexpected frontend API keys: %+v", view.APIKeys)
	}
	if view.APIKeys[0].UsedQuota != 1 {
		t.Fatalf("frontend API key quota was not converted to final amount: %+v", view.APIKeys[0])
	}

	var account map[string]any
	if err := json.Unmarshal([]byte(view.ConsoleAccount), &account); err != nil {
		t.Fatalf("decode frontend account: %v", err)
	}
	if account["id"] != "49722" || account["quota"] != 2600.0 || account["used_quota"] != 1.0 || account["today_reward"] != 1.0 || account["quota_unit"] != "USD" {
		t.Fatalf("unexpected frontend account: %+v", account)
	}

	var models []map[string]any
	if err := json.Unmarshal([]byte(view.ConsoleModels), &models); err != nil {
		t.Fatalf("decode frontend models: %v", err)
	}
	if len(models) != 3 || models[0]["in_price"] != 0.5 || models[0]["out_price"] != 4.0 || models[1]["price"] != 1.75 || models[2]["in_price"] != 5.0 || models[2]["out_price"] != 30.0 {
		t.Fatalf("unexpected frontend models: %+v", models)
	}
	groups, ok := models[0]["cheapest_groups"].([]any)
	if !ok || len(groups) != 2 || groups[0] != "default" || groups[1] != "partner" {
		t.Fatalf("unexpected equally cheapest groups: %+v", models[0]["cheapest_groups"])
	}

	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("encode frontend view: %v", err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatalf("decode frontend view: %v", err)
	}
	for _, legacy := range []string{"backend_type", "new_api_refresh", "console_cookie", "console_account_json", "console_pricing_json", "console_authorization", "console_checkin_path", "channel_url", "request_count", "last_used_at", "model_count", "hourly_requests", "hourly_failures", "recent_stats", "consecutive_failures", "recover_at"} {
		if _, exists := object[legacy]; exists {
			t.Fatalf("frontend view contains legacy field %q: %s", legacy, encoded)
		}
	}
	requiredFields := []string{
		"id", "name", "protocol", "base_url", "api_keys", "console_url",
		"console_username", "console_password", "console_refresh_token", "console_checkin_workflow_id", "console_headers",
		"manual_checkin",
		"console_models", "console_account", "notes", "proxy_id", "status",
		"weight", "created_at", "updated_at", "avg_latency_ms", "tags",
	}
	if len(object) != len(requiredFields) {
		t.Fatalf("frontend view field count=%d want=%d: %s", len(object), len(requiredFields), encoded)
	}
	for _, required := range requiredFields {
		if _, exists := object[required]; !exists {
			t.Fatalf("frontend view is missing %q: %s", required, encoded)
		}
	}
	if object["manual_checkin"] != true {
		t.Fatalf("frontend manual_checkin=%#v want true", object["manual_checkin"])
	}
	serializedKeys := object["api_keys"].([]any)
	serializedKey := serializedKeys[0].(map[string]any)
	if serializedKey["id"] != "56382" || serializedKey["key"] != "sk-value" {
		t.Fatalf("frontend API key shape is invalid: %+v", serializedKey)
	}
	if _, exists := serializedKey["api_key"]; exists {
		t.Fatalf("frontend API key retained api_key: %+v", serializedKey)
	}
}

func TestMaskedBackendDetailOmitsLegacyConsoleCookie(t *testing.T) {
	encoded, err := json.Marshal(maskedBackendDetail(domain.Backend{ConsoleCookie: "legacy=value"}))
	if err != nil {
		t.Fatalf("encode masked backend detail: %v", err)
	}
	if strings.Contains(string(encoded), "console_cookie") || strings.Contains(string(encoded), "legacy=value") {
		t.Fatalf("masked backend detail exposed legacy console cookie: %s", encoded)
	}
}

func TestBackendConsoleCheckinRequiresWorkflow(t *testing.T) {
	st := openWorkflowHandlerStore(t)
	backend, err := st.CreateBackend(context.Background(), domain.Backend{
		Name:       "workflow-required",
		ConsoleURL: "https://console.example",
	})
	if err != nil {
		t.Fatalf("create backend: %v", err)
	}
	handler := NewBackendHandler(st)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/api/backends/{id}/console/checkin", handler.HandleBackendConsoleCheckin)
	mux.HandleFunc("POST /admin/api/backends/{id}/console/sync", handler.HandleBackendConsoleSync)

	for _, path := range []string{"checkin", "sync"} {
		request := httptest.NewRequest(http.MethodPost, "/admin/api/backends/"+strconv.FormatInt(backend.ID, 10)+"/console/"+path, nil)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "console_checkin_workflow_id is required") {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestBackendConsoleCookieSyncPersistsCookieHeader(t *testing.T) {
	st := openWorkflowHandlerStore(t)
	backend, err := st.CreateBackend(context.Background(), domain.Backend{
		Name:          "cookie-sync",
		ConsoleURL:    "https://console.example/admin",
		ConsoleCookie: "legacy=value",
		ConsoleHeaders: map[string]string{
			"Cookie":    "old=value",
			"X-Console": "retained",
		},
	})
	if err != nil {
		t.Fatalf("create backend: %v", err)
	}

	handler := NewBackendHandler(st)
	handler.SetConfig(&config.Config{ChromeCDPEndpoint: "http://host.test:9222"})
	handler.chromeCredentialRead = func(ctx context.Context, endpoint, consoleURL string) (service.ChromeCDPCredentials, error) {
		if endpoint != "http://host.test:9222" || consoleURL != backend.ConsoleURL {
			t.Fatalf("endpoint=%q consoleURL=%q", endpoint, consoleURL)
		}
		return service.ChromeCDPCredentials{
			CookieHeader:  "session=from-chrome; csrf=token",
			Authorization: "Bearer browser-token",
		}, nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/api/backends/{id}/console/cookie/sync", handler.HandleBackendConsoleCookieSync)
	request := httptest.NewRequest(http.MethodPost, "/admin/api/backends/"+strconv.FormatInt(backend.ID, 10)+"/console/cookie/sync?audit=0", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		CookieCount          int  `json:"cookie_count"`
		AuthorizationUpdated bool `json:"authorization_updated"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || payload.CookieCount != 2 || !payload.AuthorizationUpdated {
		t.Fatalf("response=%s decode_err=%v", response.Body.String(), err)
	}

	updated, err := st.GetBackend(context.Background(), backend.ID)
	if err != nil {
		t.Fatalf("get updated backend: %v", err)
	}
	if got := updated.ConsoleHeaders["Cookie"]; got != "session=from-chrome; csrf=token" {
		t.Fatalf("stored Cookie=%q", got)
	}
	if got := updated.ConsoleHeaders["X-Console"]; got != "retained" {
		t.Fatalf("non-Cookie header=%q", got)
	}
	if got := updated.ConsoleHeaders["Authorization"]; got != "Bearer browser-token" {
		t.Fatalf("stored Authorization=%q", got)
	}
	if updated.ConsoleCookie != "" {
		t.Fatalf("legacy console_cookie=%q", updated.ConsoleCookie)
	}
}

func TestFrontendConsoleModelsKeepsFinalPrices(t *testing.T) {
	raw := `[{"name":"usage-model","cheapest_groups":["default"],"price_type":0,"in_price":2.5,"out_price":20},{"name":"fixed-model","cheapest_groups":["default"],"price_type":1,"price":1.75}]`
	var models []map[string]any
	if err := json.Unmarshal([]byte(frontendConsoleModelsJSON(raw, `{"quota_per_unit":1,"custom_currency_exchange_rate":99}`)), &models); err != nil {
		t.Fatalf("decode frontend models: %v", err)
	}
	if len(models) != 2 || models[0]["in_price"] != 2.5 || models[0]["out_price"] != 20.0 || models[1]["price"] != 1.75 {
		t.Fatalf("final model prices were converted again: %+v", models)
	}
}
