package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"red-token/internal/config"
	"red-token/internal/domain"
	"red-token/internal/service"
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
	legacyOutput := strings.Replace(create, `"quota":"{{quota}}"`, `"balance":"{{quota}}"`, 1)
	response = workflowRequest(t, mux, http.MethodPost, "/admin/api/workflows", legacyOutput)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "balance is not allowed") {
		t.Fatalf("legacy output create status=%d body=%s", response.Code, response.Body.String())
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
		body := `{"user_id":"user-1","username":"alice","quota":2400.5,"quota_unit":"USD","used_quota":3.75,"today_reward":123.25,"api_keys":[{"id":"key-1","name":"main","key":"sk-value","group":"default","used_quota":3.25},{"id":"key-2","name":"secondary","key":"existing-secondary","group":"secondary","used_quota":0.5}],"models":[{"name":"model-a","cheapest_groups":["default"],"in_price":1,"out_price":2,"price_type":0},{"name":"model-b","cheapest_groups":["default"],"in_price":3,"out_price":4,"price_type":0}]}`
		if r.URL.Scheme != "https" || r.URL.Host != "selected-console.test" || r.URL.Path != "/snapshot" {
			status = http.StatusNotFound
			body = `{"error":"wrong target"}`
		}
		if r.Header.Get("Authorization") != "Bearer console-token" ||
			r.Header.Get("Cookie") != "session=console" ||
			r.Header.Get("X-Console") != "configured" ||
			r.Header.Get("New-Api-User") != "" ||
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
			body = `{"user_id":"user-2","username":"broken","quota":8,"api_keys":[],"models":[]}`
		case 3:
			body = strings.Replace(body, `"today_reward":123.25`, `"today_reward":0`, 1)
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
		ConsoleAccountJSON: `{"id":"account-42","balance":999,"total_actual_cost":999,"last_checkin_reward":999}`,
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
	if execution.Output["quota"] != 2400.5 || execution.Output["quota_unit"] != "USD" || execution.Output["used_quota"] != 3.75 || execution.Output["today_reward"] != 123.25 {
		t.Fatalf("workflow response did not use the fixed quota output: %+v", execution.Output)
	}
	if _, exists := execution.Output["balance"]; exists {
		t.Fatalf("workflow response retained legacy balance: %+v", execution.Output)
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
	if updatedBackend.APIKeys[0].ID != "key-1" || updatedBackend.APIKeys[1].ID != "key-2" || updatedBackend.APIKeys[0].UsedQuota != 3.25 || updatedBackend.APIKeys[1].UsedQuota != 0.5 {
		t.Fatalf("workflow did not persist API key usage: %+v", updatedBackend.APIKeys)
	}
	if len(updatedBackend.APIKeys[1].Models) != 1 || updatedBackend.APIKeys[1].Models[0] != "upstream-only-model" || updatedBackend.APIKeys[1].ModelMapping["upstream-only-model"] != "provider-model" {
		t.Fatalf("workflow changed the secondary API key routing configuration: %+v", updatedBackend.APIKeys[1])
	}
	account := decodeJSONMap(updatedBackend.ConsoleAccountJSON)
	if account["id"] != "user-1" || account["username"] != "alice" || account["quota"] != 2400.5 || account["quota_unit"] != "USD" || account["used_quota"] != 3.75 || account["today_reward"] != 123.25 {
		t.Fatalf("workflow did not update backend account: %s", updatedBackend.ConsoleAccountJSON)
	}
	if _, exists := account["balance"]; exists {
		t.Fatalf("workflow retained legacy balance: %s", updatedBackend.ConsoleAccountJSON)
	}
	if _, exists := account["total_actual_cost"]; exists {
		t.Fatalf("workflow retained legacy total_actual_cost: %s", updatedBackend.ConsoleAccountJSON)
	}
	pricing := decodeJSONMap(updatedBackend.ConsolePricingJSON)
	pricingModels, ok := pricing["data"].([]any)
	if !ok || len(pricingModels) != 1 || pricingModels[0].(map[string]any)["model_name"] != "model-a" {
		t.Fatalf("workflow did not update backend pricing: %s", updatedBackend.ConsolePricingJSON)
	}
	if pricingModels[0].(map[string]any)["price_type"] != float64(0) || pricingModels[0].(map[string]any)["quota_type"] != float64(0) {
		t.Fatalf("workflow did not persist model price_type: %s", updatedBackend.ConsolePricingJSON)
	}

	mode.Store(3)
	response = workflowRequest(t, mux, http.MethodPost, "/admin/api/workflows/execute-workflow/execute", executeBody)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"today_reward":123.25`) {
		t.Fatalf("zero workflow reward did not preserve previous value: status=%d body=%s", response.Code, response.Body.String())
	}
	updatedBackend, err = st.GetBackend(context.Background(), backend.ID)
	if err != nil {
		t.Fatalf("get backend after zero reward workflow: %v", err)
	}
	account = decodeJSONMap(updatedBackend.ConsoleAccountJSON)
	if account["today_reward"] != 123.25 {
		t.Fatalf("zero workflow reward overwrote account value: %s", updatedBackend.ConsoleAccountJSON)
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

func TestWorkflowHandlerPersistsResponseCookiesInBackendHeaders(t *testing.T) {
	client := &http.Client{Transport: workflowRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Host {
		case "cookie-console.test":
			if request.Header.Get("Cookie") != "session=old; retained=yes" {
				t.Fatalf("initial Cookie header=%q", request.Header.Get("Cookie"))
			}
		case "empty-cookie-console.test", "failed-workflow-console.test":
			if request.Header.Get("Cookie") != "" {
				t.Fatalf("cookie leaked between workflow runs: %q", request.Header.Get("Cookie"))
			}
		default:
			t.Fatalf("unexpected host %q", request.URL.Host)
		}
		statusCode := http.StatusOK
		if request.URL.Host == "failed-workflow-console.test" {
			statusCode = http.StatusInternalServerError
		}
		return &http.Response{
			StatusCode: statusCode,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				"Set-Cookie": []string{
					"session=new; Path=/; HttpOnly",
					"added=value; Path=/",
				},
			},
			Body:    io.NopCloser(strings.NewReader(`{"user_id":"user-1","username":"alice","quota":1,"quota_unit":"USD","used_quota":0,"today_reward":1,"api_keys":[],"models":[]}`)),
			Request: request,
		}, nil
	})}

	st := openWorkflowHandlerStore(t)
	definition := json.RawMessage(workflowTestDefinition("cookie-persistence"))
	if _, err := st.CreateHTTPWorkflow(context.Background(), "cookie-persistence", "Cookie persistence", definition); err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	backend, err := st.CreateBackend(context.Background(), domain.Backend{
		Name:        "cookie-backend",
		BackendType: domain.BackendTypeNewAPI,
		ConsoleURL:  "https://cookie-console.test",
		ConsoleHeaders: map[string]string{
			"Cookie":    "session=old; retained=yes",
			"X-Console": "configured",
		},
	})
	if err != nil {
		t.Fatalf("create backend: %v", err)
	}
	handler := NewWorkflowHandler(st)
	handler.SetHTTPClient(client)
	outcome, err := handler.runCheckinWorkflow(context.Background(), backend, "cookie-persistence", nil, nil, nil)
	if err != nil {
		t.Fatalf("execute workflow: %v", err)
	}
	wantCookie := "session=new; retained=yes; added=value"
	if got := outcome.Backend.ConsoleHeaders["Cookie"]; got != wantCookie {
		t.Fatalf("stored Cookie=%q want %q", got, wantCookie)
	}
	if got := outcome.Backend.ConsoleHeaders["X-Console"]; got != "configured" {
		t.Fatalf("non-cookie header changed: %q", got)
	}
	stored, err := st.GetBackend(context.Background(), backend.ID)
	if err != nil {
		t.Fatalf("get backend: %v", err)
	}
	if stored.ConsoleHeaders["Cookie"] != wantCookie {
		t.Fatalf("persisted Cookie=%q want %q", stored.ConsoleHeaders["Cookie"], wantCookie)
	}

	emptyBackend, err := st.CreateBackend(context.Background(), domain.Backend{
		Name:           "empty-cookie-backend",
		BackendType:    domain.BackendTypeNewAPI,
		ConsoleURL:     "https://empty-cookie-console.test",
		ConsoleHeaders: map[string]string{"X-Console": "configured"},
	})
	if err != nil {
		t.Fatalf("create empty-cookie backend: %v", err)
	}
	emptyOutcome, err := handler.runCheckinWorkflow(context.Background(), emptyBackend, "cookie-persistence", nil, nil, nil)
	if err != nil {
		t.Fatalf("execute empty-cookie workflow: %v", err)
	}
	if got := emptyOutcome.Backend.ConsoleHeaders["Cookie"]; got != "session=new; added=value" {
		t.Fatalf("new Cookie header=%q", got)
	}

	failedBackend, err := st.CreateBackend(context.Background(), domain.Backend{
		Name:        "failed-workflow-backend",
		BackendType: domain.BackendTypeNewAPI,
		ConsoleURL:  "https://failed-workflow-console.test",
	})
	if err != nil {
		t.Fatalf("create failed-workflow backend: %v", err)
	}
	if _, err := handler.runCheckinWorkflow(context.Background(), failedBackend, "cookie-persistence", nil, nil, nil); err == nil {
		t.Fatal("expected failed workflow")
	}
	failedStored, err := st.GetBackend(context.Background(), failedBackend.ID)
	if err != nil {
		t.Fatalf("get failed-workflow backend: %v", err)
	}
	if got := failedStored.ConsoleHeaders["Cookie"]; got != "session=new; added=value" {
		t.Fatalf("failed workflow did not persist response cookies: %q", got)
	}
}

func TestWorkflowHandlerInjectsBackendRuntime(t *testing.T) {
	client := &http.Client{Transport: workflowRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode runtime body: %v", err)
		}
		want := map[string]any{
			"username":      "relay-user",
			"password":      "relay-password",
			"user_id":       "account-42",
			"authorization": "Bearer console-token",
			"header":        "configured",
		}
		if !reflect.DeepEqual(payload, want) {
			t.Fatalf("unexpected backend runtime body: got %#v want %#v", payload, want)
		}
		responseBody := `{"user_id":"account-42","username":"relay-user","quota":1,"quota_unit":"USD","used_quota":0,"today_reward":1,"api_keys":[],"models":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(responseBody)),
			Request:    request,
		}, nil
	})}

	st := openWorkflowHandlerStore(t)
	definitionText := strings.Replace(
		workflowTestDefinition("runtime-workflow"),
		`"request":{"method":"GET","path":"/snapshot"}`,
		`"request":{"method":"POST","path":"/snapshot","body":{"username":"{{runtime#/username}}","password":"{{runtime#/password}}","user_id":"{{runtime#/user_id}}","authorization":"{{runtime#/headers/Authorization}}","header":"{{runtime#/headers/X-Console}}"}}`,
		1,
	)
	definition, err := service.ParseGeneralWorkflow([]byte(definitionText))
	if err != nil {
		t.Fatalf("parse runtime workflow: %v", err)
	}
	encodedDefinition, err := json.Marshal(definition)
	if err != nil {
		t.Fatalf("encode runtime workflow: %v", err)
	}
	if _, err := st.CreateHTTPWorkflow(context.Background(), definition.ID, definition.Name, encodedDefinition); err != nil {
		t.Fatalf("create runtime workflow: %v", err)
	}
	backend, err := st.CreateBackend(context.Background(), domain.Backend{
		Name:                 "runtime-backend",
		BackendType:          domain.BackendTypeNewAPI,
		ConsoleURL:           "https://runtime-console.test",
		ConsoleUsername:      "relay-user",
		ConsolePassword:      "relay-password",
		ConsoleAuthorization: "Bearer console-token",
		ConsoleHeaders:       map[string]string{"X-Console": "configured"},
		ConsoleAccountJSON:   `{"id":"account-42"}`,
	})
	if err != nil {
		t.Fatalf("create runtime backend: %v", err)
	}

	handler := NewWorkflowHandler(st)
	handler.SetHTTPClient(client)
	outcome, err := handler.runCheckinWorkflow(context.Background(), backend, definition.ID, nil, nil, nil)
	if err != nil {
		t.Fatalf("run backend runtime workflow: %v", err)
	}
	if _, exists := outcome.Aliases["runtime"]; exists {
		t.Fatalf("runtime leaked into handler aliases: %#v", outcome.Aliases)
	}
}

func TestWorkflowConsoleSyncStreamsWorkflowLogsWithRequests(t *testing.T) {
	client := &http.Client{Transport: workflowRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"user_id":"user-1","username":"alice","quota":2400,"quota_unit":"USD","used_quota":3,"today_reward":123,"api_keys":[],"models":[]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})}

	st := openWorkflowHandlerStore(t)
	definition := json.RawMessage(workflowTestDefinition("sync-workflow"))
	if _, err := st.CreateHTTPWorkflow(context.Background(), "sync-workflow", "Sync workflow", definition); err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	backend, err := st.CreateBackend(context.Background(), domain.Backend{
		Name:                   "workflow-backend",
		BackendType:            domain.BackendTypeNewAPI,
		ConsoleURL:             "https://workflow-console.test",
		ConsoleCheckinWorkflow: "sync-workflow",
		ConsoleAccountJSON:     `{"id":"account-42"}`,
	})
	if err != nil {
		t.Fatalf("create backend: %v", err)
	}

	workflowHandler := NewWorkflowHandler(st)
	workflowHandler.SetConfig(&config.Config{BackendConsoleUserAgent: "Workflow-Test/1.0"})
	workflowHandler.SetHTTPClient(client)
	backendHandler := NewBackendHandler(st)
	backendHandler.SetWorkflowHandler(workflowHandler)
	backendHandler.SetConsoleHTTPClient(client)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/api/backends/{id}/console/sync", backendHandler.HandleBackendConsoleSync)
	request := httptest.NewRequest(http.MethodPost, "/admin/api/backends/"+jsonInt64(backend.ID)+"/console/sync?stream=1&checkin=1", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", response.Code, response.Body.String())
	}

	lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected workflow log, request, and completion events, got %d: %s", len(lines), response.Body.String())
	}
	var eventTypes []string
	workflowLogCount := 0
	requestCount := 0
	for index, line := range lines {
		var event struct {
			Type     string                          `json:"type"`
			Log      service.GeneralWorkflowDebugLog `json:"log"`
			Response *map[string]any                 `json:"response"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode stream line %d: %v; line=%s", index, err, line)
		}
		eventTypes = append(eventTypes, event.Type)
		switch event.Type {
		case "workflow_log":
			workflowLogCount++
			if event.Log.Message == "" || event.Log.Phase == "" {
				t.Fatalf("workflow log missing message or phase: %+v", event.Log)
			}
		case "request":
			requestCount++
		case "complete":
			if event.Response == nil {
				t.Fatal("completion event missing response")
			}
		}
	}
	if eventTypes[0] != "workflow_log" || eventTypes[len(eventTypes)-1] != "complete" || workflowLogCount == 0 || requestCount != 1 {
		t.Fatalf("unexpected stream event sequence: %v", eventTypes)
	}
}

func TestWorkflowOutputPricingJSONPersistsFixedPriceType(t *testing.T) {
	encoded, err := workflowOutputPricingJSON([]any{
		map[string]any{
			"name":            "fixed-model",
			"cheapest_groups": []any{"default"},
			"price":           1.75,
			"price_type":      1,
		},
	}, "")
	if err != nil {
		t.Fatalf("persist fixed model pricing: %v", err)
	}
	pricing := decodeJSONMap(encoded)
	models, ok := pricing["data"].([]any)
	if !ok || len(models) != 1 {
		t.Fatalf("unexpected pricing payload: %s", encoded)
	}
	model := models[0].(map[string]any)
	if model["price_type"] != float64(1) || model["quota_type"] != float64(1) || model["price"] != 1.75 || model["model_price"] != 1.75 || model["billing_mode"] != "fixed" {
		t.Fatalf("fixed price type was not persisted: %s", encoded)
	}
}

func TestWorkflowOutputPricingJSONAcceptsLegacyFixedPriceShape(t *testing.T) {
	encoded, err := workflowOutputPricingJSON([]any{
		map[string]any{
			"name":            "fixed-model",
			"cheapest_groups": []any{"default"},
			"in_price":        1.75,
			"out_price":       1.75,
			"price_type":      1,
		},
	}, "")
	if err != nil || !strings.Contains(encoded, `"model_price":1.75`) {
		t.Fatalf("persist legacy fixed model pricing: encoded=%s err=%v", encoded, err)
	}
}

func TestWorkflowOutputPricingJSONAcceptsPriceForUsageType(t *testing.T) {
	encoded, err := workflowOutputPricingJSON([]any{
		map[string]any{
			"name":            "usage-model",
			"cheapest_groups": []any{"default"},
			"price":           2.5,
			"price_type":      0,
		},
	}, "")
	if err != nil || !strings.Contains(encoded, `"input_price":2.5`) || !strings.Contains(encoded, `"output_price":2.5`) {
		t.Fatalf("persist usage model price fallback: encoded=%s err=%v", encoded, err)
	}
}

func workflowTestDefinition(id string) string {
	return `{"spec":"http-workflow/v1","id":"` + id + `","name":"Workflow test","steps":[{"id":"snapshot","name":"Get snapshot","request":{"method":"GET","path":"/snapshot"},"extract":[{"alias":"user_id","expression":".user_id"},{"alias":"username","expression":".username"},{"alias":"quota","expression":".quota"},{"alias":"quota_unit","expression":".quota_unit"},{"alias":"used_quota","expression":".used_quota"},{"alias":"today_reward","expression":".today_reward"},{"alias":"api_keys","expression":".api_keys"},{"alias":"models","expression":".models"}]}],"output":{"user_id":"{{user_id}}","username":"{{username}}","quota":"{{quota}}","quota_unit":"{{quota_unit}}","used_quota":"{{used_quota}}","today_reward":"{{today_reward}}","api_keys":"{{api_keys}}","models":"{{models}}"}}`
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
