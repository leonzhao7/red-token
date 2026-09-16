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

func TestCreateBackendAcceptsUserID(t *testing.T) {
	st := openWorkflowHandlerStore(t)
	handler := NewBackendHandler(st)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/api/backends", handler.HandleCreateBackend)

	request := httptest.NewRequest(http.MethodPost, "/admin/api/backends", strings.NewReader(`{"name":"relay","base_url":"https://relay.example","user_id":"account-42"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}

	var view struct {
		ID             int64  `json:"id"`
		ConsoleAccount string `json:"console_account"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	var account map[string]any
	if err := json.Unmarshal([]byte(view.ConsoleAccount), &account); err != nil {
		t.Fatalf("decode created account: %v", err)
	}
	if view.ID == 0 || account["id"] != "account-42" {
		t.Fatalf("unexpected created backend account: id=%d account=%s", view.ID, view.ConsoleAccount)
	}
	backend, err := st.GetBackend(context.Background(), view.ID)
	if err != nil {
		t.Fatalf("get created backend: %v", err)
	}
	if backend.ConsoleAccountJSON != `{"id":"account-42"}` {
		t.Fatalf("stored account=%s", backend.ConsoleAccountJSON)
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
		ConsoleAccountJSON: `{"id":"49722","username":"alice","quota":342.25,"used_quota":150.5,"today_reward":10,"quota_unit":" ","last_checkin_at":"2026-08-14T17:12:21Z"}`,
		ConsolePricingJSON: `{"data":[{"model_name":"usage-model","price_type":0,"input_price":0.5,"output_price":1.5,"enable_groups":["default","partner"]},{"model_name":"fixed-model","price_type":1,"price":1.75,"enable_groups":["default"]}]}`,
		ConsoleHeaders:     map[string]string{"Cookie": "session=value"},
		ManualCheckin:      true,
		Frozen:             true,
		Status:             domain.BackendStatusNormal,
		Tags:               []string{},
	}
	view := buildBackendFrontendView(backend, 12.5)
	if len(view.APIKeys) != 1 || view.APIKeys[0].ID != "56382" || view.APIKeys[0].Key != "sk-value" {
		t.Fatalf("unexpected frontend API keys: %+v", view.APIKeys)
	}
	if view.APIKeys[0].UsedQuota != 500000 {
		t.Fatalf("frontend API key quota should not be converted: expected 500000, got %f", view.APIKeys[0].UsedQuota)
	}

	var account map[string]any
	if err := json.Unmarshal([]byte(view.ConsoleAccount), &account); err != nil {
		t.Fatalf("decode frontend account: %v", err)
	}
	if account["id"] != "49722" || account["quota"] != 342.25 || account["used_quota"] != 150.5 || account["today_reward"] != 10.0 || account["quota_unit"] != " " {
		t.Fatalf("frontend account values should be passed through unchanged: %+v", account)
	}

	var models []map[string]any
	if err := json.Unmarshal([]byte(view.ConsoleModels), &models); err != nil {
		t.Fatalf("decode frontend models: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0]["name"] != "usage-model" || models[0]["in_price"] != 0.5 || models[0]["out_price"] != 1.5 {
		t.Fatalf("usage model prices should be passed through: %+v", models[0])
	}
	if models[1]["name"] != "fixed-model" || models[1]["price"] != 1.75 {
		t.Fatalf("fixed model price should be passed through: %+v", models[1])
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
		"manual_checkin", "frozen",
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
	if object["frozen"] != true {
		t.Fatalf("frontend frozen=%#v want true", object["frozen"])
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
	raw := `{"data":[{"model_name":"usage-model","enable_groups":["default"],"price_type":0,"input_price":2.5,"output_price":20},{"model_name":"fixed-model","enable_groups":["default"],"price_type":1,"price":1.75}]}`
	var models []map[string]any
	if err := json.Unmarshal([]byte(frontendConsoleModelsJSON(raw)), &models); err != nil {
		t.Fatalf("decode frontend models: %v", err)
	}
	if len(models) != 2 || models[0]["in_price"] != 2.5 || models[0]["out_price"] != 20.0 || models[1]["price"] != 1.75 {
		t.Fatalf("final model prices were converted again: %+v", models)
	}
}
