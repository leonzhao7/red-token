package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"red-token/internal/config"
	"red-token/internal/domain"
	"red-token/internal/store"
)

func TestWorkflowHandlerCRUDAndValidation(t *testing.T) {
	st := openWorkflowHandlerStore(t)
	handler := NewWorkflowHandler(st)
	mux := workflowTestMux(handler)

	create := workflowTestDefinition("workflow-crud")
	response := workflowRequest(t, mux, http.MethodPost, "/admin/api/workflows", create)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}

	response = workflowRequest(t, mux, http.MethodPost, "/admin/api/workflows", create)
	if response.Code != http.StatusConflict {
		t.Fatalf("duplicate create status=%d body=%s", response.Code, response.Body.String())
	}

	response = workflowRequest(t, mux, http.MethodGet, "/admin/api/workflows?page=1&limit=10", "")
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	var list struct {
		Items []json.RawMessage `json:"items"`
		Total int               `json:"total"`
	}
	decodeWorkflowResponse(t, response, &list)
	if list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("unexpected list response: total=%d items=%d", list.Total, len(list.Items))
	}

	response = workflowRequest(t, mux, http.MethodGet, "/admin/api/workflows/workflow-crud", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"definition"`) {
		t.Fatalf("get status=%d body=%s", response.Code, response.Body.String())
	}

	mismatch := strings.Replace(create, `"id":"workflow-crud"`, `"id":"other-workflow"`, 1)
	response = workflowRequest(t, mux, http.MethodPut, "/admin/api/workflows/workflow-crud", mismatch)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("mismatched update status=%d body=%s", response.Code, response.Body.String())
	}

	invalid := strings.Replace(create, `"expression":".user_id"`, `"expression":".["`, 1)
	response = workflowRequest(t, mux, http.MethodPost, "/admin/api/workflows", invalid)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid create status=%d body=%s", response.Code, response.Body.String())
	}

	updated := strings.Replace(create, `"name":"Workflow test"`, `"name":"Updated workflow"`, 1)
	response = workflowRequest(t, mux, http.MethodPut, "/admin/api/workflows/workflow-crud", updated)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Updated workflow") {
		t.Fatalf("update status=%d body=%s", response.Code, response.Body.String())
	}

	response = workflowRequest(t, mux, http.MethodDelete, "/admin/api/workflows/workflow-crud", "")
	if response.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", response.Code, response.Body.String())
	}
	response = workflowRequest(t, mux, http.MethodGet, "/admin/api/workflows/workflow-crud", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("get deleted status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestWorkflowHandlerExecutePersistsOnlySuccessfulOutput(t *testing.T) {
	var mode atomic.Int32
	var received atomic.Bool
	client := &http.Client{Transport: workflowRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		status := http.StatusOK
		body := `{"user_id":"user-1","username":"alice","balance":12.5,"used_balance":3,"api_keys":[{"id":"key-1","name":"main","key":"sk-value","group":"default","total_cost":3},{"id":"key-2","name":"secondary","key":"existing-secondary","group":"secondary","total_cost":0}],"models":[{"name":"model-a","cheapest_groups":["default"],"in_price":1,"out_price":2},{"name":"model-b","cheapest_groups":["default"],"in_price":3,"out_price":4}]}`
		expectedNewAPIUser := "account-42"
		if mode.Load() != 0 {
			expectedNewAPIUser = "user-1"
		}
		if r.URL.Scheme != "https" || r.URL.Host != "selected-console.test" || r.URL.Path != "/snapshot" {
			status = http.StatusNotFound
			body = `{"error":"wrong target"}`
		}
		if r.Header.Get("Authorization") != "Bearer console-token" ||
			r.Header.Get("Cookie") != "session=console" ||
			r.Header.Get("X-Console") != "configured" ||
			r.Header.Get("New-Api-User") != expectedNewAPIUser ||
			r.Header.Get("User-Agent") != "Workflow-Test/1.0" {
			status = http.StatusUnauthorized
			body = `{"error":"missing host headers"}`
		}
		received.Store(true)
		switch mode.Load() {
		case 1:
			status = http.StatusInternalServerError
			body = `{"error":"temporary"}`
		case 2:
			body = `{"user_id":"user-2","username":"broken","balance":8,"api_keys":[],"models":[]}`
		}
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})}

	st := openWorkflowHandlerStore(t)
	backend, err := st.CreateBackend(context.Background(), domain.Backend{
		Name:                 "selected-backend",
		BackendType:          domain.BackendTypeNewAPI,
		BaseURL:              "https://api.example.com",
		ConsoleURL:           "https://selected-console.test",
		ConsoleAuthorization: "Bearer console-token",
		ConsoleHeaders: map[string]string{
			"Cookie":    "session=console",
			"X-Console": "configured",
		},
		ConsoleAccountJSON: `{"id":"account-42"}`,
		APIKeys: []domain.BackendAPIKey{{
			APIKey:       "sk-value",
			Name:         "old-main-name",
			Group:        "old-main-group",
			Models:       []string{"configured-main-model"},
			ModelMapping: map[string]string{"configured-main-model": "provider-main-model"},
		}, {
			APIKey:       "existing-secondary",
			Name:         "secondary",
			Group:        "secondary",
			Models:       []string{"upstream-only-model"},
			ModelMapping: map[string]string{"upstream-only-model": "provider-model"},
		}},
	})
	if err != nil {
		t.Fatalf("create backend: %v", err)
	}
	handler := NewWorkflowHandler(st)
	handler.SetConfig(&config.Config{BackendConsoleUserAgent: "Workflow-Test/1.0", FocusModels: "model-a"})
	handler.SetHTTPClient(client)
	mux := workflowTestMux(handler)

	response := workflowRequest(t, mux, http.MethodPost, "/admin/api/workflows", workflowTestDefinition("execute-workflow"))
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	executeBody := `{"backend_id":` + jsonInt64(backend.ID) + `}`
	response = workflowRequest(t, mux, http.MethodPost, "/admin/api/workflows/missing-workflow/execute", executeBody)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing workflow execute status=%d body=%s", response.Code, response.Body.String())
	}
	response = workflowRequest(t, mux, http.MethodPost, "/admin/api/workflows/execute-workflow/execute", `{"backend_id":999999}`)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing backend execute status=%d body=%s", response.Code, response.Body.String())
	}
	response = workflowRequest(t, mux, http.MethodPost, "/admin/api/workflows/execute-workflow/execute", executeBody)
	if response.Code != http.StatusOK {
		t.Fatalf("execute status=%d body=%s", response.Code, response.Body.String())
	}
	if !received.Load() {
		t.Fatal("selected backend console was not called")
	}
	var execution struct {
		Output     map[string]any `json:"output"`
		Aliases    map[string]any `json:"aliases"`
		Requests   []any          `json:"requests"`
		ExecutedAt string         `json:"executed_at"`
	}
	decodeWorkflowResponse(t, response, &execution)
	if execution.Output["user_id"] != "user-1" || execution.Aliases["username"] != "alice" || len(execution.Requests) != 1 || execution.ExecutedAt == "" {
		t.Fatalf("unexpected execution response: %+v", execution)
	}
	updatedBackend, err := st.GetBackend(context.Background(), backend.ID)
	if err != nil {
		t.Fatalf("get updated backend: %v", err)
	}
	if updatedBackend.APIKey != "sk-value" || len(updatedBackend.APIKeys) != 2 {
		t.Fatalf("workflow did not update backend api keys: %+v", updatedBackend.APIKeys)
	}
	if len(updatedBackend.APIKeys[0].Models) != 1 || updatedBackend.APIKeys[0].Models[0] != "configured-main-model" || updatedBackend.APIKeys[0].ModelMapping["configured-main-model"] != "provider-main-model" {
		t.Fatalf("workflow changed the primary API key routing configuration: %+v", updatedBackend.APIKeys[0])
	}
	if len(updatedBackend.APIKeys[1].Models) != 1 || updatedBackend.APIKeys[1].Models[0] != "upstream-only-model" || updatedBackend.APIKeys[1].ModelMapping["upstream-only-model"] != "provider-model" {
		t.Fatalf("workflow changed the secondary API key routing configuration: %+v", updatedBackend.APIKeys[1])
	}
	account := decodeJSONMap(updatedBackend.ConsoleAccountJSON)
	if account["id"] != "user-1" || account["username"] != "alice" || account["balance"] != 12.5 || account["total_actual_cost"] != 3.0 {
		t.Fatalf("workflow did not update backend account: %s", updatedBackend.ConsoleAccountJSON)
	}
	pricing := decodeJSONMap(updatedBackend.ConsolePricingJSON)
	pricingModels, ok := pricing["data"].([]any)
	if !ok || len(pricingModels) != 1 || pricingModels[0].(map[string]any)["model_name"] != "model-a" {
		t.Fatalf("workflow did not update backend pricing: %s", updatedBackend.ConsolePricingJSON)
	}

	resultPath := "/admin/api/workflows/execute-workflow/results/" + jsonInt64(backend.ID)
	response = workflowRequest(t, mux, http.MethodGet, resultPath, "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"user_id":"user-1"`) {
		t.Fatalf("get result status=%d body=%s", response.Code, response.Body.String())
	}
	successfulSnapshot := response.Body.String()
	backendAfterSuccess := updatedBackend

	mode.Store(1)
	response = workflowRequest(t, mux, http.MethodPost, "/admin/api/workflows/execute-workflow/execute", executeBody)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), `"requests"`) || !strings.Contains(response.Body.String(), `"debug_logs"`) || !strings.Contains(response.Body.String(), `"phase":"request"`) {
		t.Fatalf("failed execute status=%d body=%s", response.Code, response.Body.String())
	}
	response = workflowRequest(t, mux, http.MethodGet, resultPath, "")
	if response.Code != http.StatusOK || response.Body.String() != successfulSnapshot {
		t.Fatalf("transport failure replaced snapshot: status=%d body=%s want=%s", response.Code, response.Body.String(), successfulSnapshot)
	}

	mode.Store(2)
	response = workflowRequest(t, mux, http.MethodPost, "/admin/api/workflows/execute-workflow/execute", executeBody)
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "validate output schema") {
		t.Fatalf("invalid output status=%d body=%s", response.Code, response.Body.String())
	}
	response = workflowRequest(t, mux, http.MethodGet, resultPath, "")
	if response.Code != http.StatusOK || response.Body.String() != successfulSnapshot {
		t.Fatalf("schema failure replaced snapshot: status=%d body=%s want=%s", response.Code, response.Body.String(), successfulSnapshot)
	}
	updatedBackend, err = st.GetBackend(context.Background(), backend.ID)
	if err != nil {
		t.Fatalf("get backend after failed workflow: %v", err)
	}
	if updatedBackend.ConsoleAccountJSON != backendAfterSuccess.ConsoleAccountJSON || updatedBackend.ConsolePricingJSON != backendAfterSuccess.ConsolePricingJSON || updatedBackend.APIKey != backendAfterSuccess.APIKey {
		t.Fatalf("failed workflow changed backend data: before=%+v after=%+v", backendAfterSuccess, updatedBackend)
	}
}

func workflowTestDefinition(id string) string {
	return `{"spec":"http-workflow/v1","id":"` + id + `","name":"Workflow test","steps":[{"id":"snapshot","name":"Get snapshot","request":{"method":"GET","path":"/snapshot"},"extract":[{"alias":"user_id","expression":".user_id"},{"alias":"username","expression":".username"},{"alias":"balance","expression":".balance"},{"alias":"used_balance","expression":".used_balance"},{"alias":"api_keys","expression":".api_keys"},{"alias":"models","expression":".models"}]}],"output":{"user_id":"{{user_id}}","username":"{{username}}","balance":"{{balance}}","used_balance":"{{used_balance}}","api_keys":"{{api_keys}}","models":"{{models}}"}}`
}

func openWorkflowHandlerStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "handler-workflows.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func workflowTestMux(handler *WorkflowHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/api/workflows", handler.HandleListWorkflows)
	mux.HandleFunc("POST /admin/api/workflows", handler.HandleCreateWorkflow)
	mux.HandleFunc("GET /admin/api/workflows/{id}", handler.HandleGetWorkflow)
	mux.HandleFunc("PUT /admin/api/workflows/{id}", handler.HandleUpdateWorkflow)
	mux.HandleFunc("DELETE /admin/api/workflows/{id}", handler.HandleDeleteWorkflow)
	mux.HandleFunc("POST /admin/api/workflows/{id}/execute", handler.HandleExecuteWorkflow)
	mux.HandleFunc("GET /admin/api/workflows/{id}/results/{backend_id}", handler.HandleGetWorkflowResult)
	return mux
}

func workflowRequest(t *testing.T, handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodeWorkflowResponse(t *testing.T, response *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, response.Body.String())
	}
}

func jsonInt64(value int64) string {
	return fmt.Sprintf("%d", value)
}

type workflowRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn workflowRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
