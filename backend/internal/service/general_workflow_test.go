package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGeneralWorkflowExecuteEndToEnd(t *testing.T) {
	t.Helper()
	var requests []*http.Request
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.Clone(request.Context()))
		var responseBody string
		switch request.URL.Path {
		case "/api/profile":
			if request.Header.Get("Authorization") != "Bearer host-token" {
				t.Fatalf("expected host authorization header, got %q", request.Header.Get("Authorization"))
			}
			responseBody = `{"code":0,"data":{"id":42,"email":"user@example.com","free_balance":"10.5"}}`
		case "/api/keys":
			if got := request.URL.Query()["scope"]; !reflect.DeepEqual(got, []string{"personal", "shared"}) {
				t.Fatalf("unexpected repeated scope query: %#v", got)
			}
			if got := request.Header.Get("X-Profile"); got != "user-42" {
				t.Fatalf("unexpected interpolated header %q", got)
			}
			responseBody = `{"data":{"items":[{"id":7,"name":"first","key":"sk-a","group":{"name":"vip"}},{"id":8,"name":null,"key":"sk-b","group":null}]}}`
		case "/api/usage":
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			ids, ok := payload["ids"].([]any)
			if !ok || len(ids) != 2 {
				t.Fatalf("expected typed ids array, got %#v", payload["ids"])
			}
			responseBody = `{"data":{"stats":{"7":{"total_actual_cost":"1.25"},"8":{"total_actual_cost":2}}}}`
		default:
			t.Fatalf("unexpected request path %q", request.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Test": []string{"a", "b"}},
			Body:       io.NopCloser(strings.NewReader(responseBody)),
			Request:    request,
		}, nil
	})}

	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v1",
  "id":"end-to-end",
  "name":"End to end",
  "steps":[
    {
      "id":"profile",
      "name":"Profile",
      "request":{"method":"GET","path":"/api/profile"},
      "expect":"$response.status == 200 and .code == 0",
      "extract":[
        {"alias":"user_id","expression":".data.id | tostring"},
        {"alias":"username","expression":".data.email"},
        {"alias":"balance","expression":".data.free_balance | tonumber"}
      ]
    },
    {
      "id":"keys",
      "name":"Keys",
      "request":{
        "method":"GET",
        "path":"/api/keys",
        "query":{"scope":"{{scopes}}","omit":null},
        "headers":{"X-Profile":"user-{{user_id}}"}
      },
      "extract":[
        {"alias":"api_keys_base","expression":"[.data.items[] | {id:(.id|tostring),name:(.name // \"\"),key:.key,group:(.group.name // \"default\")}]"},
        {"alias":"key_ids","expression":"$vars.api_keys_base | map(.id)"}
      ]
    },
    {
      "id":"usage",
      "name":"Usage",
      "request":{"method":"POST","path":"/api/usage","body":{"ids":"{{key_ids}}"}},
      "extract":[
        {"alias":"api_keys","expression":"$vars.api_keys_base | map(. as $key | $key + {total_cost: (($response.body.data.stats[$key.id].total_actual_cost // 0) | tonumber)})"},
        {"alias":"used_balance","expression":"$vars.api_keys | map(.total_cost) | add // 0"}
      ]
    }
  ],
  "output":{
    "user_id":"{{user_id}}",
    "username":"{{username}}",
    "balance":"{{balance}}",
    "used_balance":"{{used_balance}}",
    "first_key_id":"{{api_keys#/0/id}}",
    "keys":"{{api_keys}}",
    "literal":"\\{{user_id}}"
  }
}`)

	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{
		HTTPClient: client,
		Now: func() time.Time {
			return time.Date(2026, time.August, 10, 1, 2, 3, 4, time.UTC)
		},
	})
	validatorCalled := false
	result, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{
		BaseURL:        "https://console.example/base",
		Headers:        http.Header{"Authorization": []string{"Bearer host-token"}},
		InitialAliases: map[string]any{"scopes": []string{"personal", "shared"}},
		ValidateOutput: func(value any) error {
			validatorCalled = true
			output := value.(map[string]any)
			if output["first_key_id"] != "7" {
				return errors.New("unexpected first key")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("execute workflow: %v", err)
	}
	if !validatorCalled {
		t.Fatal("expected output validator to be called")
	}
	if len(requests) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(requests))
	}
	output := result.Output.(map[string]any)
	if output["user_id"] != "42" || output["username"] != "user@example.com" {
		t.Fatalf("unexpected profile output: %#v", output)
	}
	if output["balance"] != float64(10.5) || output["used_balance"] != float64(3.25) {
		t.Fatalf("unexpected balance output: %#v", output)
	}
	if output["literal"] != "{{user_id}}" {
		t.Fatalf("unexpected escaped template output: %#v", output["literal"])
	}
	keys := output["keys"].([]any)
	if len(keys) != 2 || keys[0].(map[string]any)["total_cost"] != float64(1.25) || keys[1].(map[string]any)["total_cost"] != int(2) {
		t.Fatalf("unexpected joined keys: %#v", keys)
	}
}

func TestGeneralWorkflowForeachRendersObjectMembersAndAggregatesExtracts(t *testing.T) {
	var paths []string
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		paths = append(paths, request.URL.Path)
		body := `{"ok":true}`
		switch request.URL.Path {
		case "/items":
			body = `{"data":[{"key":"123"},{"key":"abc"}]}`
		case "/api/123/usage":
			body = `{"usage":12}`
		case "/api/abc/usage":
			body = `{"usage":34}`
		case "/done":
		default:
			t.Fatalf("unexpected request path %q", request.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})}})
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v3","id":"foreach-objects","name":"Foreach objects",
  "steps":[
    {"id":"list","name":"List","request":{"method":"GET","path":"/items"},"extract":[{"alias":"items","expression":".data"}]},
    {
      "id":"usage","name":"Usage",
      "foreach":{"alias":"items","as":"item","index_as":"item_index"},
      "request":{"method":"GET","path":"/api/{{item#/key}}/usage"},
      "extract":[
        {"alias":"usage_rows","expression":"{key:$vars.item.key,index:$vars.item_index,usage:.usage}"},
        {"alias":"usage_labels","expression":"($vars.usage_rows.key + \"=\" + ($vars.usage_rows.usage | tostring))"}
      ],
      "when":{"expression":"$vars.usage_rows | length == 2","goto":"done"}
    },
    {"id":"skipped","name":"Skipped","request":{"method":"GET","path":"/skipped"}},
    {"id":"done","name":"Done","request":{"method":"GET","path":"/done"}}
  ],
  "output":{"rows":"{{usage_rows}}","labels":"{{usage_labels}}"}
}`)
	var logs []GeneralWorkflowDebugLog
	result, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{
		BaseURL:  "https://example.com",
		DebugLog: func(log GeneralWorkflowDebugLog) { logs = append(logs, log) },
	})
	if err != nil {
		t.Fatalf("execute foreach workflow: %v", err)
	}
	if want := []string{"/items", "/api/123/usage", "/api/abc/usage", "/done"}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("unexpected request order: got %#v want %#v", paths, want)
	}
	rows, ok := result.Aliases["usage_rows"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("expected two aggregated usage rows, got %#v", result.Aliases["usage_rows"])
	}
	first := rows[0].(map[string]any)
	second := rows[1].(map[string]any)
	if first["key"] != "123" || first["index"] != 0 || first["usage"] != 12 || second["key"] != "abc" || second["index"] != 1 || second["usage"] != 34 {
		t.Fatalf("unexpected aggregated rows: %#v", rows)
	}
	if want := []any{"123=12", "abc=34"}; !reflect.DeepEqual(result.Aliases["usage_labels"], want) {
		t.Fatalf("unexpected aggregated labels: %#v", result.Aliases["usage_labels"])
	}
	if _, exists := result.Aliases["item"]; exists {
		t.Fatalf("foreach item alias leaked into result: %#v", result.Aliases)
	}
	if _, exists := result.Aliases["item_index"]; exists {
		t.Fatalf("foreach index alias leaked into result: %#v", result.Aliases)
	}
	iterationLogs := 0
	for _, log := range logs {
		if log.Phase == "foreach_iteration" && log.Message == "foreach iteration started" {
			iterationLogs++
			if _, exists := log.Details["iteration_index"]; !exists {
				t.Fatalf("foreach log is missing iteration index: %#v", log)
			}
		}
	}
	if iterationLogs != 2 {
		t.Fatalf("expected two foreach iteration logs, got %d", iterationLogs)
	}
}

func TestGeneralWorkflowForeachEmptyArrayCommitsEmptyExtracts(t *testing.T) {
	var paths []string
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		paths = append(paths, request.URL.Path)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Request: request}, nil
	})}})
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v3","id":"foreach-empty","name":"Foreach empty",
  "steps":[
    {"id":"usage","name":"Usage","foreach":{"alias":"items","as":"item"},"request":{"method":"GET","path":"/api/{{item#/key}}/usage"},"extract":[{"alias":"usage_rows","expression":".usage"}],"when":{"expression":"($vars.usage_rows == []) and (. == null) and ($response == null) and ($request == null)","goto":"done"}},
    {"id":"skipped","name":"Skipped","request":{"method":"GET","path":"/skipped"}},
    {"id":"done","name":"Done","request":{"method":"GET","path":"/done"}}
  ],
  "output":{"rows":"{{usage_rows}}"}
}`)
	result, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{
		BaseURL:        "https://example.com",
		InitialAliases: map[string]any{"items": []any{}},
	})
	if err != nil {
		t.Fatalf("execute empty foreach workflow: %v", err)
	}
	if want := []string{"/done"}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("empty foreach sent an iteration request: got %#v want %#v", paths, want)
	}
	if rows, ok := result.Aliases["usage_rows"].([]any); !ok || len(rows) != 0 {
		t.Fatalf("empty foreach did not commit an empty aggregate: %#v", result.Aliases["usage_rows"])
	}
}

func TestGeneralWorkflowForeachExpectRouteDiscardsPartialAggregates(t *testing.T) {
	var paths []string
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		paths = append(paths, request.URL.Path)
		status := http.StatusOK
		body := `{"usage":12}`
		if request.URL.Path == "/api/abc/usage" {
			status = http.StatusConflict
			body = `{"error":"conflict"}`
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})}})
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v3","id":"foreach-route","name":"Foreach route",
  "steps":[
    {"id":"usage","name":"Usage","foreach":{"alias":"items","as":"item"},"request":{"method":"GET","path":"/api/{{item#/key}}/usage"},"expect":{"routes":[{"statuses":[409],"goto":"fallback"}]},"extract":[{"alias":"usage_rows","expression":".usage"}]},
    {"id":"fallback","name":"Fallback","request":{"method":"GET","path":"/fallback/{{usage_rows#/0}}"}}
  ],
  "output":{"rows":"{{usage_rows}}"}
}`)
	oldRows := []any{"old"}
	result, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{
		BaseURL: "https://example.com",
		InitialAliases: map[string]any{
			"items":      []any{map[string]any{"key": "123"}, map[string]any{"key": "abc"}},
			"usage_rows": oldRows,
		},
	})
	if err != nil {
		t.Fatalf("execute routed foreach workflow: %v", err)
	}
	if want := []string{"/api/123/usage", "/api/abc/usage", "/fallback/old"}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("unexpected routed request order: got %#v want %#v", paths, want)
	}
	if !reflect.DeepEqual(result.Aliases["usage_rows"], oldRows) {
		t.Fatalf("partial foreach aggregate was committed: %#v", result.Aliases["usage_rows"])
	}
}

func TestGeneralWorkflowGotoOnResponseStatusContinuesFromTarget(t *testing.T) {
	var paths []string
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		paths = append(paths, request.URL.Path)
		status := http.StatusOK
		if request.URL.Path == "/first" {
			status = http.StatusUnauthorized
		}
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    request,
		}, nil
	})}})
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v2","id":"goto-status","name":"Goto status",
  "steps":[
    {"id":"first","name":"First","request":{"method":"GET","path":"/first"},"expect":{"routes":[{"statuses":[401],"goto":"target"}]},"extract":[{"alias":"must_not_run","expression":"error(\"expect route must skip extract\")"}]},
    {"id":"skipped","name":"Skipped","request":{"method":"GET","path":"/skipped"}},
    {"id":"target","name":"Target","request":{"method":"GET","path":"/target"}},
    {"id":"tail","name":"Tail","request":{"method":"GET","path":"/tail"}}
  ],
  "output":{}
}`)
	var logs []GeneralWorkflowDebugLog
	if _, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{
		BaseURL:  "https://example.com",
		DebugLog: func(log GeneralWorkflowDebugLog) { logs = append(logs, log) },
	}); err != nil {
		t.Fatalf("execute goto workflow: %v", err)
	}
	if want := []string{"/first", "/target", "/tail"}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("unexpected request order: got %#v want %#v", paths, want)
	}
	for _, log := range logs {
		if log.Phase == "expect_goto" && log.Details["goto"] == "target" {
			return
		}
	}
	t.Fatal("expected expect_goto debug log")
}

func TestGeneralWorkflowWhenFalseContinuesCurrentStep(t *testing.T) {
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: generalWorkflowJSONClient(`{"value":7}`, http.StatusOK)})
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v2","id":"when-false","name":"When false",
  "steps":[
    {"id":"first","name":"First","request":{"method":"GET","path":"/first"},"when":{"expression":"$vars.value == 99","goto":"tail"},"extract":[{"alias":"value","expression":".value"}]},
    {"id":"tail","name":"Tail","request":{"method":"GET","path":"/tail"}}
  ],
  "output":{"value":"{{value}}"}
}`)
	result, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{BaseURL: "https://example.com"})
	if err != nil {
		t.Fatalf("execute when-false workflow: %v", err)
	}
	output := result.Output.(map[string]any)
	if value := fmt.Sprint(output["value"]); value != "7" {
		t.Fatalf("current step extract did not run: %#v", output)
	}
}

func TestGeneralWorkflowWhenRoutesUsingExtractedAlias(t *testing.T) {
	for _, a := range []bool{true, false} {
		t.Run(strconv.FormatBool(a), func(t *testing.T) {
			var paths []string
			workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				paths = append(paths, request.URL.Path)
				body := `{"ok":true}`
				if request.URL.Path == "/first" {
					body = fmt.Sprintf(`{"a":%t}`, a)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
			})}})
			definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v2","id":"when-alias","name":"When alias",
  "steps":[
    {"id":"first","name":"First","request":{"method":"GET","path":"/first"},"extract":[{"alias":"a","expression":".a"}],"when":{"expression":"$vars.a == true","goto":"request2"}},
    {"id":"request3","name":"Request 3","request":{"method":"GET","path":"/request3"},"when":{"expression":"true","goto":"end"}},
    {"id":"request2","name":"Request 2","request":{"method":"GET","path":"/request2"}},
    {"id":"end","name":"End","request":{"method":"GET","path":"/end"}}
  ],
  "output":{"a":"{{a}}"}
}`)
			result, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{BaseURL: "https://example.com"})
			if err != nil {
				t.Fatalf("execute alias when workflow: %v", err)
			}
			want := []string{"/first", "/request3", "/end"}
			if a {
				want = []string{"/first", "/request2", "/end"}
			}
			if !reflect.DeepEqual(paths, want) {
				t.Fatalf("unexpected request order: got %#v want %#v", paths, want)
			}
			if result.Aliases["a"] != a {
				t.Fatalf("expected extracted alias before goto: %#v", result.Aliases)
			}
		})
	}
}

func TestGeneralWorkflowExpectUnmatchedNon2xxFailsWithoutExtract(t *testing.T) {
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: generalWorkflowJSONClient(`{"a":true}`, http.StatusInternalServerError)})
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v2","id":"expect-failure","name":"Expect failure",
  "steps":[
    {"id":"first","name":"First","request":{"method":"GET","path":"/first"},"expect":{"routes":[{"statuses":[401],"goto":"tail"}]},"extract":[{"alias":"a","expression":".a"}]},
    {"id":"tail","name":"Tail","request":{"method":"GET","path":"/tail"}}
  ],
  "output":{}
}`)
	result, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{BaseURL: "https://example.com"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 500 did not match") {
		t.Fatalf("expected unmatched non-2xx failure, got result=%#v err=%v", result, err)
	}
}

func TestGeneralWorkflowAcceptedStatusContinuesToExtractAndWhen(t *testing.T) {
	var paths []string
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		paths = append(paths, request.URL.Path)
		status := http.StatusOK
		body := `{"ok":true}`
		if request.URL.Path == "/first" {
			status = http.StatusConflict
			body = `{"a":true}`
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})}})
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v2","id":"accepted-status","name":"Accepted status",
  "steps":[
    {"id":"first","name":"First","request":{"method":"GET","path":"/first"},"expect":{"accepted_statuses":[409]},"extract":[{"alias":"a","expression":".a"}],"when":{"expression":"$vars.a == true","goto":"target"}},
    {"id":"skipped","name":"Skipped","request":{"method":"GET","path":"/skipped"}},
    {"id":"target","name":"Target","request":{"method":"GET","path":"/target"}}
  ],
  "output":{"a":"{{a}}"}
}`)
	result, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{BaseURL: "https://example.com"})
	if err != nil {
		t.Fatalf("execute accepted status workflow: %v", err)
	}
	if want := []string{"/first", "/target"}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("unexpected request order: got %#v want %#v", paths, want)
	}
	if result.Aliases["a"] != true {
		t.Fatalf("accepted status did not continue through extract: %#v", result.Aliases)
	}
}

func TestGeneralWorkflowGotoLoopIsBounded(t *testing.T) {
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: generalWorkflowJSONClient(`{}`, http.StatusOK)})
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v2","id":"goto-loop","name":"Goto loop",
  "steps":[{"id":"loop","name":"Loop","request":{"method":"GET","path":"/loop"},"when":{"expression":"true","goto":"loop"}}],
  "output":{}
}`)
	_, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{BaseURL: "https://example.com"})
	if err == nil || !strings.Contains(err.Error(), "step visit limit exceeded") {
		t.Fatalf("expected bounded goto loop failure, got %v", err)
	}
}

func TestGeneralWorkflowDebugLogsIncludeFailureContext(t *testing.T) {
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{
		HTTPClient: generalWorkflowJSONClient(`{"ok":false,"message":"upstream rejected","api_key":"response-secret"}`, http.StatusBadRequest),
	})
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v1","id":"debug-failure","name":"Debug failure",
  "steps":[{"id":"request","name":"Request","request":{"method":"POST","path":"/api","query":{"api_key":"query-secret"},"body":{"password":"request-secret"}},"expect":"$response.status == 200"}],
  "output":{}
}`)
	logs := make([]GeneralWorkflowDebugLog, 0)
	recorder := NewNewAPIRequestRecorder()
	_, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{
		BaseURL:  "https://console.example",
		Recorder: recorder,
		DebugLog: func(log GeneralWorkflowDebugLog) { logs = append(logs, log) },
	})
	if err == nil || !strings.Contains(err.Error(), `step "request" expect`) {
		t.Fatalf("expected expect failure, got %v", err)
	}
	if len(logs) < 5 {
		t.Fatalf("expected detailed debug logs, got %d: %#v", len(logs), logs)
	}
	encoded, marshalErr := json.Marshal(logs)
	if marshalErr != nil {
		t.Fatalf("marshal debug logs: %v", marshalErr)
	}
	if !strings.Contains(string(encoded), "request-secret") || !strings.Contains(string(encoded), "response-secret") || !strings.Contains(string(encoded), "query-secret") {
		t.Fatalf("debug logs did not preserve raw request/response values: %s", encoded)
	}
	if len(recorder.Requests) != 1 || !strings.Contains(recorder.Requests[0].Path, "query-secret") || !strings.Contains(recorder.Requests[0].Body, "response-secret") {
		t.Fatalf("request log did not preserve raw values: %#v", recorder.Requests)
	}
	var responseLogged, failureLogged bool
	errorLogs := 0
	for _, log := range logs {
		if log.Phase == "response" {
			responseLogged = true
		}
		if log.Level == "error" {
			errorLogs++
			if log.Phase == "expect" && strings.Contains(log.Message, "HTTP 400") && log.Details["response_status"] == http.StatusBadRequest {
				failureLogged = true
			}
		}
	}
	if !responseLogged || !failureLogged || errorLogs != 1 {
		t.Fatalf("missing response/failure debug logs: %#v", logs)
	}
}

func TestGeneralWorkflowDocumentationExampleParses(t *testing.T) {
	document, err := os.ReadFile("../../../docs/http_workflow.md")
	if err != nil {
		t.Fatalf("read workflow documentation: %v", err)
	}
	section := strings.Index(string(document), "## 12. 完整示例")
	if section < 0 {
		t.Fatal("workflow documentation is missing the complete example section")
	}
	example := string(document[section:])
	start := strings.Index(example, "```json\n")
	if start < 0 {
		t.Fatal("workflow documentation is missing the complete example JSON block")
	}
	example = example[start+len("```json\n"):]
	end := strings.Index(example, "\n```")
	if end < 0 {
		t.Fatal("workflow documentation example JSON block is not closed")
	}
	if _, err := ParseGeneralWorkflow([]byte(example[:end])); err != nil {
		t.Fatalf("parse workflow documentation example: %v", err)
	}
}

func TestParseGeneralWorkflowRejectsInvalidDefinitions(t *testing.T) {
	tests := []struct {
		name       string
		definition string
		want       string
	}{
		{
			name:       "unknown field",
			definition: `{"spec":"http-workflow/v1","id":"test","name":"Test","steps":[],"output":{},"unknown":true}`,
			want:       "unknown field",
		},
		{
			name:       "duplicate key",
			definition: `{"spec":"http-workflow/v1","id":"test","id":"again","name":"Test","steps":[],"output":{}}`,
			want:       "duplicate object key",
		},
		{
			name:       "unsafe integer",
			definition: `{"spec":"http-workflow/v1","id":"test","name":"Test","steps":[],"output":{"id":9007199254740992}}`,
			want:       "safe JSON integer",
		},
		{
			name:       "forbidden jq",
			definition: `{"spec":"http-workflow/v1","id":"test","name":"Test","steps":[{"id":"step","name":"Step","request":{"method":"GET","path":"/"},"extract":[{"alias":"value","expression":"now"}]}],"output":{}}`,
			want:       "not allowed",
		},
		{
			name:       "v1 when",
			definition: `{"spec":"http-workflow/v1","id":"test","name":"Test","steps":[{"id":"step","name":"Step","request":{"method":"GET","path":"/"},"when":{"expression":"true","goto":"step"}}],"output":{}}`,
			want:       "when requires spec \"http-workflow/v2\"",
		},
		{
			name:       "v2 foreach",
			definition: `{"spec":"http-workflow/v2","id":"test","name":"Test","steps":[{"id":"step","name":"Step","foreach":{"alias":"items","as":"item"},"request":{"method":"GET","path":"/"}}],"output":{}}`,
			want:       "foreach requires spec \"http-workflow/v3\"",
		},
		{
			name:       "foreach alias collision",
			definition: `{"spec":"http-workflow/v3","id":"test","name":"Test","steps":[{"id":"step","name":"Step","foreach":{"alias":"items","as":"items"},"request":{"method":"GET","path":"/"}}],"output":{}}`,
			want:       "foreach alias must differ",
		},
		{
			name:       "foreach extract alias collision",
			definition: `{"spec":"http-workflow/v3","id":"test","name":"Test","steps":[{"id":"step","name":"Step","foreach":{"alias":"items","as":"item"},"request":{"method":"GET","path":"/"},"extract":[{"alias":"item","expression":"."}]}],"output":{}}`,
			want:       "conflicts with a foreach iteration alias",
		},
		{
			name:       "invalid expect status",
			definition: `{"spec":"http-workflow/v2","id":"test","name":"Test","steps":[{"id":"step","name":"Step","request":{"method":"GET","path":"/"},"expect":{"routes":[{"statuses":[99],"goto":"step"}]}}],"output":{}}`,
			want:       "HTTP status code",
		},
		{
			name:       "v1 structured expect",
			definition: `{"spec":"http-workflow/v1","id":"test","name":"Test","steps":[{"id":"step","name":"Step","request":{"method":"GET","path":"/"},"expect":{"routes":[{"statuses":[401],"goto":"step"}]}}],"output":{}}`,
			want:       "structured expect requires spec \"http-workflow/v2\"",
		},
		{
			name:       "v2 string expect",
			definition: `{"spec":"http-workflow/v2","id":"test","name":"Test","steps":[{"id":"step","name":"Step","request":{"method":"GET","path":"/"},"expect":"true"}],"output":{}}`,
			want:       "string expect requires spec \"http-workflow/v1\"",
		},
		{
			name:       "accepted and routed status conflict",
			definition: `{"spec":"http-workflow/v2","id":"test","name":"Test","steps":[{"id":"step","name":"Step","request":{"method":"GET","path":"/"},"expect":{"accepted_statuses":[409],"routes":[{"statuses":[409],"goto":"step"}]}}],"output":{}}`,
			want:       "cannot be both accepted and routed",
		},
		{
			name:       "missing when target",
			definition: `{"spec":"http-workflow/v2","id":"test","name":"Test","steps":[{"id":"step","name":"Step","request":{"method":"GET","path":"/"},"when":{"expression":"true","goto":"missing"}}],"output":{}}`,
			want:       "does not exist",
		},
		{
			name:       "null query",
			definition: `{"spec":"http-workflow/v1","id":"test","name":"Test","steps":[{"id":"step","name":"Step","request":{"method":"GET","path":"/","query":null}}],"output":{}}`,
			want:       "query must be an object",
		},
		{
			name:       "query in path",
			definition: `{"spec":"http-workflow/v1","id":"test","name":"Test","steps":[{"id":"step","name":"Step","request":{"method":"GET","path":"/api?a=1"}}],"output":{}}`,
			want:       "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition, err := ParseGeneralWorkflow([]byte(test.definition))
			if test.name == "query in path" {
				if err != nil {
					t.Fatalf("path is rendered at execution, parse should succeed: %v", err)
				}
				workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					t.Fatal("invalid rendered path must fail before sending")
					return nil, nil
				})}})
				_, err = workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{BaseURL: "https://example.com"})
			}
			if err == nil || test.want != "" && !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected error containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestGeneralWorkflowForeachValidatesRuntimeSource(t *testing.T) {
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v3","id":"foreach-source","name":"Foreach source",
  "steps":[{"id":"request","name":"Request","foreach":{"alias":"items","as":"item"},"request":{"method":"GET","path":"/api"}}],
  "output":{}
}`)
	requests := 0
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: request}, nil
	})}})

	_, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{
		BaseURL:        "https://example.com",
		InitialAliases: map[string]any{"items": map[string]any{"key": "123"}},
	})
	if err == nil || !strings.Contains(err.Error(), `source alias "items" must be an array`) {
		t.Fatalf("expected non-array foreach source error, got %v", err)
	}

	tooMany := make([]any, defaultGeneralWorkflowForeachLimit+1)
	_, err = workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{
		BaseURL:        "https://example.com",
		InitialAliases: map[string]any{"items": tooMany},
	})
	if err == nil || !strings.Contains(err.Error(), "limit is 1000") {
		t.Fatalf("expected foreach item limit error, got %v", err)
	}
	if requests != 0 {
		t.Fatalf("invalid foreach sources sent %d requests", requests)
	}
}

func TestGeneralWorkflowRejectsForbiddenJQCapabilities(t *testing.T) {
	for _, expression := range []string{"env", "$ENV", "input", "inputs", "now", "debug", "stderr", "halt", "halt_error(1)"} {
		t.Run(expression, func(t *testing.T) {
			definition := `{
  "spec":"http-workflow/v1","id":"forbidden-jq","name":"Forbidden jq",
  "steps":[{"id":"request","name":"Request","request":{"method":"GET","path":"/api"},"extract":[{"alias":"value","expression":` + strconv.Quote(expression) + `}]}],
  "output":{}
}`
			_, err := ParseGeneralWorkflow([]byte(definition))
			if err == nil || !strings.Contains(err.Error(), "not allowed") {
				t.Fatalf("expected forbidden jq error, got %v", err)
			}
		})
	}
}

func TestGeneralWorkflowRejectsDuplicateResponseKeys(t *testing.T) {
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: generalWorkflowJSONClient(`{"data":{"id":1,"id":2}}`, http.StatusOK)})
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v1","id":"duplicate-response","name":"Duplicate response",
  "steps":[{"id":"request","name":"Request","request":{"method":"GET","path":"/api"}}],
  "output":{}
}`)
	_, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{BaseURL: "https://example.com"})
	if err == nil || !strings.Contains(err.Error(), "duplicate object key") {
		t.Fatalf("expected duplicate key error, got %v", err)
	}
}

func TestGeneralWorkflowRequiresSingleJQResult(t *testing.T) {
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: generalWorkflowJSONClient(`[1,2]`, http.StatusOK)})
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v1","id":"multiple-results","name":"Multiple results",
  "steps":[{"id":"request","name":"Request","request":{"method":"GET","path":"/api"},"extract":[{"alias":"items","expression":".[]"}]}],
  "output":{}
}`)
	_, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{BaseURL: "https://example.com"})
	if err == nil || !strings.Contains(err.Error(), "multiple results") {
		t.Fatalf("expected multiple result error, got %v", err)
	}
}

func TestGeneralWorkflowLimitsJQExecutionTime(t *testing.T) {
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{
		HTTPClient: generalWorkflowJSONClient(`{}`, http.StatusOK),
		JQTimeout:  10 * time.Millisecond,
	})
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v1","id":"jq-timeout","name":"JQ timeout",
  "steps":[{"id":"request","name":"Request","request":{"method":"GET","path":"/api"},"extract":[{"alias":"value","expression":"def recurse: recurse; recurse"}]}],
  "output":{}
}`)
	_, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{BaseURL: "https://example.com"})
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("expected jq timeout error, got %v", err)
	}
}

func TestGeneralWorkflowOutputValidatorFailure(t *testing.T) {
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{})
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v1","id":"output-validation","name":"Output validation","steps":[],"output":{"value":1}
}`)
	_, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{
		ValidateOutput: func(any) error {
			return errors.New("schema mismatch")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "schema mismatch") {
		t.Fatalf("expected schema validation error, got %v", err)
	}
}

func TestGeneralWorkflowDistinguishesAbsentAndNullBody(t *testing.T) {
	var bodies []string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, string(body))
		return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{}, Body: http.NoBody, Request: request}, nil
	})}
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: client})
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v1","id":"null-body","name":"Null body",
  "steps":[
    {"id":"absent","name":"Absent","request":{"method":"POST","path":"/absent"},"expect":"$response.status == 204 and ($response.has_body | not)"},
    {"id":"null","name":"Null","request":{"method":"POST","path":"/null","body":null},"expect":"$response.status == 204"}
  ],
  "output":{}
}`)
	if _, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{BaseURL: "https://example.com"}); err != nil {
		t.Fatalf("execute workflow: %v", err)
	}
	if !reflect.DeepEqual(bodies, []string{"", "null"}) {
		t.Fatalf("unexpected bodies: %#v", bodies)
	}
}

func generalWorkflowJSONClient(body string, status int) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}
}

func mustParseGeneralWorkflow(t *testing.T, definition string) GeneralWorkflowDefinition {
	t.Helper()
	workflow, err := ParseGeneralWorkflow([]byte(definition))
	if err != nil {
		t.Fatalf("parse workflow: %v", err)
	}
	return workflow
}
