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
			if got := request.URL.RawQuery; got != "scope=personal&scope=shared" {
				t.Fatalf("unexpected raw query %q", got)
			}
			if got := request.URL.Query()["scope"]; !reflect.DeepEqual(got, []string{"personal", "shared"}) {
				t.Fatalf("unexpected repeated scope query: %#v", got)
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
      "request":{
        "method":"GET",
        "path":"/api/keys?scope={{scope}}&scope=shared"
      },
      "extract":[
        {"alias":"api_keys_base","expression":"[.data.items[] | {id:(.id|tostring),name:(.name // \"\"),key:.key,group:(.group.name // \"default\")}]"},
        {"alias":"key_ids","expression":"$vars.api_keys_base | map(.id)"}
      ]
    },
    {
      "id":"usage",
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
		InitialAliases: map[string]any{"scope": "personal"},
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

func TestGeneralWorkflowCarriesResponseCookiesAndGlobalHeaders(t *testing.T) {
	requestIndex := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestIndex++
		if request.Header.Get("X-Backend") != "backend-value" || request.Header.Get("X-Global") != "global-value" || request.Header.Get("X-Configured") != "relay-value" {
			t.Fatalf("missing inherited headers on request %d: %v", requestIndex, request.Header)
		}
		responseHeaders := http.Header{"Content-Type": []string{"application/json"}}
		switch request.URL.Path {
		case "/login":
			if got := request.Header.Get("Cookie"); got != "seed=old" {
				t.Fatalf("initial cookie=%q", got)
			}
			if got := request.Header.Get("X-Override"); got != "global" {
				t.Fatalf("global header override value=%q", got)
			}
			responseHeaders["Set-Cookie"] = []string{
				"session=fresh; Path=/; HttpOnly",
				"theme=dark; Path=/",
			}
		case "/profile":
			if got := request.Header.Get("X-Override"); got != "global" {
				t.Fatalf("global header value=%q", got)
			}
			for name, want := range map[string]string{"seed": "old", "session": "fresh", "theme": "dark"} {
				cookie, err := request.Cookie(name)
				if err != nil || cookie.Value != want {
					t.Fatalf("request cookie %s=%v err=%v", name, cookie, err)
				}
			}
			responseHeaders["Set-Cookie"] = []string{"seed=replaced; Path=/"}
		default:
			t.Fatalf("unexpected request path %q", request.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     responseHeaders,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    request,
		}, nil
	})}
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v4","id":"cookies-and-headers","name":"Cookies and headers",
  "headers":{
    "X-Global":"global-value",
    "X-Override":"global",
    "X-Configured":"{{runtime#/headers/X-Relay}}"
  },
  "steps":[
    {"id":"login","request":{"method":"POST","path":"/login"}},
    {"id":"profile","request":{"method":"GET","path":"/profile"},"extract":[{"alias":"sent_cookies","expression":"$request.headers.cookie[0]"}]}
  ],
  "output":{"sent_cookies":"{{sent_cookies}}"}
}`)
	logs := make([]GeneralWorkflowDebugLog, 0)
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: client})
	result, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{
		BaseURL: "https://console.example",
		Headers: http.Header{
			"Cookie":    []string{"seed=old"},
			"X-Backend": []string{"backend-value"},
		},
		Runtime:  map[string]any{"headers": map[string]string{"X-Relay": "relay-value"}},
		DebugLog: func(log GeneralWorkflowDebugLog) { logs = append(logs, log) },
	})
	if err != nil {
		t.Fatalf("execute cookies workflow: %v", err)
	}
	if requestIndex != 2 {
		t.Fatalf("request count=%d", requestIndex)
	}
	if len(result.ResponseCookies) != 3 {
		t.Fatalf("response cookies=%#v", result.ResponseCookies)
	}
	sentCookies := result.Output.(map[string]any)["sent_cookies"].(string)
	for _, value := range []string{"seed=old", "session=fresh", "theme=dark"} {
		if !strings.Contains(sentCookies, value) {
			t.Fatalf("$request cookie preview %q does not contain %q", sentCookies, value)
		}
	}
	encodedLogs, err := json.Marshal(logs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encodedLogs), "session=fresh") || !strings.Contains(string(encodedLogs), "set-cookie") {
		t.Fatalf("cookie values were not emitted in plaintext debug logs: %s", encodedLogs)
	}
}

func TestGeneralWorkflowStepHeadersOverrideGlobalHeaders(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		path := request.URL.Path
		switch path {
		case "/step1":
			if got := request.Header.Get("X-Global"); got != "global-value" {
				t.Fatalf("step1 X-Global=%q, want global-value", got)
			}
			if got := request.Header.Get("X-Override"); got != "step1-override" {
				t.Fatalf("step1 X-Override=%q, want step1-override", got)
			}
			if got := request.Header.Get("X-Step-Only"); got != "step1-only" {
				t.Fatalf("step1 X-Step-Only=%q, want step1-only", got)
			}
		case "/step2":
			if got := request.Header.Get("X-Global"); got != "global-value" {
				t.Fatalf("step2 X-Global=%q, want global-value", got)
			}
			if got := request.Header.Get("X-Override"); got != "step2-override" {
				t.Fatalf("step2 X-Override=%q, want step2-override", got)
			}
			if request.Header.Get("X-Step-Only") != "" {
				t.Fatalf("step2 X-Step-Only should not exist")
			}
		default:
			t.Fatalf("unexpected path %q", path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    request,
		}, nil
	})}
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v5","id":"step-headers","name":"Step headers",
  "headers":{"X-Global":"global-value","X-Override":"global-override"},
  "steps":[
    {"id":"step1","request":{"method":"GET","path":"/step1","headers":{"X-Override":"step1-override","X-Step-Only":"step1-only"}}},
    {"id":"step2","request":{"method":"GET","path":"/step2","headers":{"x-override":"step2-override"}}}
  ],
  "output":{}
}`)
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: client})
	_, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{
		BaseURL: "https://console.example",
	})
	if err != nil {
		t.Fatalf("execute step headers workflow: %v", err)
	}
}

func TestGeneralWorkflowStepHeaderNullDeletesGlobalHeader(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("X-Should-Delete") != "" {
			t.Fatalf("X-Should-Delete should not exist, got %q", request.Header.Get("X-Should-Delete"))
		}
		if got := request.Header.Get("X-Keep"); got != "keep-value" {
			t.Fatalf("X-Keep=%q, want keep-value", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    request,
		}, nil
	})}
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v5","id":"null-header","name":"Null header",
  "headers":{"X-Should-Delete":"global-value","X-Keep":"keep-value"},
  "steps":[
    {"id":"test","request":{"method":"GET","path":"/test","headers":{"X-Should-Delete":null}}}
  ],
  "output":{}
}`)
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: client})
	_, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{
		BaseURL: "https://console.example",
	})
	if err != nil {
		t.Fatalf("execute null header workflow: %v", err)
	}
}

func TestGeneralWorkflowStepHeaderOverridesBaseHeader(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if header := request.Header.Get("X-Custom"); header != "workflow" {
			t.Errorf("expected X-Custom=workflow, got %q", header)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Request:    request,
		}, nil
	})}
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v5","id":"override","name":"Override",
  "steps":[
    {"id":"test","request":{"method":"GET","path":"/test","headers":{"X-Custom":"workflow"}}}
  ],
  "output":{}
}`)
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: client})
	_, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{
		BaseURL: "https://console.example",
		Headers: http.Header{"X-Custom": []string{"base"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGeneralWorkflowStepHeaderProtectedHeaderRejected(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(``)),
			Request:    request,
		}, nil
	})}
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v5","id":"protected","name":"Protected",
  "steps":[
    {"id":"test","request":{"method":"GET","path":"/test","headers":{"Authorization":"Bearer bad"}}}
  ],
  "output":{}
}`)
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{
		HTTPClient:       client,
		ProtectedHeaders: []string{"authorization", "cookie"},
	})
	_, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{
		BaseURL: "https://console.example",
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "authorization") {
		t.Fatalf("expected authorization protected error, got %v", err)
	}
}

func TestGeneralWorkflowStepHeadersTemplateReferences(t *testing.T) {
	requestCount := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestCount++
		if requestCount == 1 {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"token":"alias-value"}`)),
				Request:    request,
			}, nil
		}
		if got := request.Header.Get("X-Alias"); got != "alias-value" {
			t.Fatalf("X-Alias=%q, want alias-value", got)
		}
		if got := request.Header.Get("X-Runtime"); got != "runtime-value" {
			t.Fatalf("X-Runtime=%q, want runtime-value", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    request,
		}, nil
	})}
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v5","id":"template","name":"Template",
  "steps":[
    {"id":"step1","request":{"method":"GET","path":"/step1"},"extract":[{"alias":"token","expression":"$response.body.token"}]},
    {"id":"step2","request":{"method":"GET","path":"/step2","headers":{"X-Alias":"{{token}}","X-Runtime":"{{runtime#/headers/X-Custom}}"}}}
  ],
  "output":{}
}`)
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: client})
	_, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{
		BaseURL: "https://console.example",
		Runtime: map[string]any{"headers": map[string]string{"X-Custom": "runtime-value"}},
	})
	if err != nil {
		t.Fatalf("execute template workflow: %v", err)
	}
}

func TestGeneralWorkflowStepHeadersInForeach(t *testing.T) {
	received := make([]string, 0)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/init" {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Request:    request,
			}, nil
		}
		received = append(received, request.Header.Get("X-Item"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    request,
		}, nil
	})}
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v5","id":"foreach","name":"Foreach",
  "steps":[
    {"id":"initial","request":{"method":"GET","path":"/init"},"extract":[{"alias":"items","expression":"[\"apple\",\"banana\",\"cherry\"]"}]},
    {"id":"loop","foreach":{"alias":"items","as":"item"},"request":{"method":"GET","path":"/item","headers":{"X-Item":"{{item}}"}}}
  ],
  "output":{}
}`)
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: client})
	_, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{
		BaseURL: "https://console.example",
	})
	if err != nil {
		t.Fatalf("execute foreach workflow: %v", err)
	}
	if len(received) != 3 || received[0] != "apple" || received[1] != "banana" || received[2] != "cherry" {
		t.Fatalf("received items=%v, want [apple banana cherry]", received)
	}
}

func TestGeneralWorkflowStepHeadersInRequestPreview(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    request,
		}, nil
	})}
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v5","id":"preview","name":"Preview",
  "headers":{"X-Global":"global"},
  "steps":[
    {"id":"test","request":{"method":"GET","path":"/test","headers":{"X-Step":"step"}},"extract":[{"alias":"preview","expression":"$request.headers"}]}
  ],
  "output":{"preview":"{{preview}}"}
}`)
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: client})
	result, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{
		BaseURL: "https://console.example",
	})
	if err != nil {
		t.Fatalf("execute preview workflow: %v", err)
	}
	preview := result.Output.(map[string]any)["preview"].(map[string]any)
	if len(preview["x-global"].([]any)) == 0 || preview["x-global"].([]any)[0] != "global" {
		t.Fatalf("preview missing x-global: %v", preview)
	}
	if len(preview["x-step"].([]any)) == 0 || preview["x-step"].([]any)[0] != "step" {
		t.Fatalf("preview missing x-step: %v", preview)
	}
}

func TestGeneralWorkflowRuntimeIsAvailableToJQAndTemplates(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode runtime request body: %v", err)
		}
		want := map[string]any{"username": "alice", "password": "secret", "user_id": "user-42"}
		if !reflect.DeepEqual(payload, want) {
			t.Fatalf("unexpected runtime request body: got %#v want %#v", payload, want)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    request,
		}, nil
	})}
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v1","id":"runtime-context","name":"Runtime context",
  "steps":[{
    "id":"login",
    "request":{
      "method":"POST","path":"/login",
      "body":{"username":"{{runtime#/username}}","password":"{{runtime#/password}}","user_id":"{{runtime#/user_id}}"}
    },
    "expect":"$runtime.username == \"alice\" and $runtime.password == \"secret\" and $runtime.user_id == \"user-42\" and $runtime.headers[\"X-Console\"] == \"configured\"",
    "extract":[{"alias":"runtime_username","expression":"$runtime.username"}]
  }],
  "output":{"username":"{{runtime#/username}}","copied_username":"{{runtime_username}}"}
}`)

	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{HTTPClient: client})
	result, err := workflow.Execute(context.Background(), definition, GeneralWorkflowRunOptions{
		BaseURL: "https://console.example",
		Runtime: map[string]any{
			"username": "alice",
			"password": "secret",
			"user_id":  "user-42",
			"headers":  map[string]string{"X-Console": "configured"},
		},
	})
	if err != nil {
		t.Fatalf("execute runtime workflow: %v", err)
	}
	output := result.Output.(map[string]any)
	if output["username"] != "alice" || output["copied_username"] != "alice" {
		t.Fatalf("unexpected runtime output: %#v", output)
	}
	if _, exists := result.Aliases["runtime"]; exists {
		t.Fatalf("runtime leaked into result aliases: %#v", result.Aliases)
	}
}

func TestGeneralWorkflowRuntimeHasStableCredentialDefaults(t *testing.T) {
	workflow := NewGeneralWorkflow(GeneralWorkflowOptions{})
	runtime, err := workflow.buildRuntime("runtime-defaults", nil)
	if err != nil {
		t.Fatalf("build default runtime: %v", err)
	}
	for _, field := range []string{"username", "password", "user_id"} {
		if runtime[field] != "" {
			t.Fatalf("runtime %s default = %#v, want empty string", field, runtime[field])
		}
	}
	headers, ok := runtime["headers"].(map[string]any)
	if !ok || len(headers) != 0 {
		t.Fatalf("runtime headers default = %#v, want empty object", runtime["headers"])
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
    {"id":"list","request":{"method":"GET","path":"/items"},"extract":[{"alias":"items","expression":".data"}]},
    {
      "id":"usage",
      "foreach":{"alias":"items","as":"item","index_as":"item_index"},
      "request":{"method":"GET","path":"/api/{{item#/key}}/usage"},
      "extract":[
        {"alias":"usage_rows","expression":"{key:$vars.item.key,index:$vars.item_index,usage:.usage}"},
        {"alias":"usage_labels","expression":"($vars.usage_rows.key + \"=\" + ($vars.usage_rows.usage | tostring))"}
      ],
      "when":{"expression":"$vars.usage_rows | length == 2","goto":"done"}
    },
    {"id":"skipped","request":{"method":"GET","path":"/skipped"}},
    {"id":"done","request":{"method":"GET","path":"/done"}}
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
    {"id":"usage","foreach":{"alias":"items","as":"item"},"request":{"method":"GET","path":"/api/{{item#/key}}/usage"},"extract":[{"alias":"usage_rows","expression":".usage"}],"when":{"expression":"($vars.usage_rows == []) and (. == null) and ($response == null) and ($request == null)","goto":"done"}},
    {"id":"skipped","request":{"method":"GET","path":"/skipped"}},
    {"id":"done","request":{"method":"GET","path":"/done"}}
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
    {"id":"usage","foreach":{"alias":"items","as":"item"},"request":{"method":"GET","path":"/api/{{item#/key}}/usage"},"expect":{"routes":[{"statuses":[409],"goto":"fallback"}]},"extract":[{"alias":"usage_rows","expression":".usage"}]},
    {"id":"fallback","request":{"method":"GET","path":"/fallback/{{usage_rows#/0}}"}}
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
    {"id":"first","request":{"method":"GET","path":"/first"},"expect":{"routes":[{"statuses":[401],"goto":"target"}]},"extract":[{"alias":"must_not_run","expression":"error(\"expect route must skip extract\")"}]},
    {"id":"skipped","request":{"method":"GET","path":"/skipped"}},
    {"id":"target","request":{"method":"GET","path":"/target"}},
    {"id":"tail","request":{"method":"GET","path":"/tail"}}
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
    {"id":"first","request":{"method":"GET","path":"/first"},"when":{"expression":"$vars.value == 99","goto":"tail"},"extract":[{"alias":"value","expression":".value"}]},
    {"id":"tail","request":{"method":"GET","path":"/tail"}}
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
    {"id":"first","request":{"method":"GET","path":"/first"},"extract":[{"alias":"a","expression":".a"}],"when":{"expression":"$vars.a == true","goto":"request2"}},
    {"id":"request3","request":{"method":"GET","path":"/request3"},"when":{"expression":"true","goto":"end"}},
    {"id":"request2","request":{"method":"GET","path":"/request2"}},
    {"id":"end","request":{"method":"GET","path":"/end"}}
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
    {"id":"first","request":{"method":"GET","path":"/first"},"expect":{"routes":[{"statuses":[401],"goto":"tail"}]},"extract":[{"alias":"a","expression":".a"}]},
    {"id":"tail","request":{"method":"GET","path":"/tail"}}
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
    {"id":"first","request":{"method":"GET","path":"/first"},"expect":{"accepted_statuses":[409]},"extract":[{"alias":"a","expression":".a"}],"when":{"expression":"$vars.a == true","goto":"target"}},
    {"id":"skipped","request":{"method":"GET","path":"/skipped"}},
    {"id":"target","request":{"method":"GET","path":"/target"}}
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
  "steps":[{"id":"loop","request":{"method":"GET","path":"/loop"},"when":{"expression":"true","goto":"loop"}}],
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
  "steps":[{"id":"request","request":{"method":"POST","path":"/api?api_key=query-secret","body":{"password":"request-secret"}},"expect":"$response.status == 200"}],
  "output":{}
}`)
	logs := make([]GeneralWorkflowDebugLog, 0)
	recorder := NewRequestRecorder()
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
			name:       "v3 global headers",
			definition: `{"spec":"http-workflow/v3","id":"test","name":"Test","headers":{"X-Test":"value"},"steps":[],"output":{}}`,
			want:       "workflow headers require spec \"http-workflow/v4\"",
		},
		{
			name:       "forbidden jq",
			definition: `{"spec":"http-workflow/v1","id":"test","name":"Test","steps":[{"id":"step","request":{"method":"GET","path":"/"},"extract":[{"alias":"value","expression":"now"}]}],"output":{}}`,
			want:       "not allowed",
		},
		{
			name:       "v1 when",
			definition: `{"spec":"http-workflow/v1","id":"test","name":"Test","steps":[{"id":"step","request":{"method":"GET","path":"/"},"when":{"expression":"true","goto":"step"}}],"output":{}}`,
			want:       "when requires spec \"http-workflow/v2\"",
		},
		{
			name:       "v2 foreach",
			definition: `{"spec":"http-workflow/v2","id":"test","name":"Test","steps":[{"id":"step","foreach":{"alias":"items","as":"item"},"request":{"method":"GET","path":"/"}}],"output":{}}`,
			want:       "foreach requires spec \"http-workflow/v3\"",
		},
		{
			name:       "foreach alias collision",
			definition: `{"spec":"http-workflow/v3","id":"test","name":"Test","steps":[{"id":"step","foreach":{"alias":"items","as":"items"},"request":{"method":"GET","path":"/"}}],"output":{}}`,
			want:       "foreach alias must differ",
		},
		{
			name:       "foreach extract alias collision",
			definition: `{"spec":"http-workflow/v3","id":"test","name":"Test","steps":[{"id":"step","foreach":{"alias":"items","as":"item"},"request":{"method":"GET","path":"/"},"extract":[{"alias":"item","expression":"."}]}],"output":{}}`,
			want:       "conflicts with a foreach iteration alias",
		},
		{
			name:       "invalid expect status",
			definition: `{"spec":"http-workflow/v2","id":"test","name":"Test","steps":[{"id":"step","request":{"method":"GET","path":"/"},"expect":{"routes":[{"statuses":[99],"goto":"step"}]}}],"output":{}}`,
			want:       "HTTP status code",
		},
		{
			name:       "v1 structured expect",
			definition: `{"spec":"http-workflow/v1","id":"test","name":"Test","steps":[{"id":"step","request":{"method":"GET","path":"/"},"expect":{"routes":[{"statuses":[401],"goto":"step"}]}}],"output":{}}`,
			want:       "structured expect requires spec \"http-workflow/v2\"",
		},
		{
			name:       "v2 string expect",
			definition: `{"spec":"http-workflow/v2","id":"test","name":"Test","steps":[{"id":"step","request":{"method":"GET","path":"/"},"expect":"true"}],"output":{}}`,
			want:       "string expect requires spec \"http-workflow/v1\"",
		},
		{
			name:       "accepted and routed status conflict",
			definition: `{"spec":"http-workflow/v2","id":"test","name":"Test","steps":[{"id":"step","request":{"method":"GET","path":"/"},"expect":{"accepted_statuses":[409],"routes":[{"statuses":[409],"goto":"step"}]}}],"output":{}}`,
			want:       "cannot be both accepted and routed",
		},
		{
			name:       "missing when target",
			definition: `{"spec":"http-workflow/v2","id":"test","name":"Test","steps":[{"id":"step","request":{"method":"GET","path":"/"},"when":{"expression":"true","goto":"missing"}}],"output":{}}`,
			want:       "does not exist",
		},
		{
			name:       "request query is unsupported",
			definition: `{"spec":"http-workflow/v1","id":"test","name":"Test","steps":[{"id":"step","request":{"method":"GET","path":"/","query":{}}}],"output":{}}`,
			want:       "unknown field \"query\"",
		},
		{
			name:       "request headers require spec v5",
			definition: `{"spec":"http-workflow/v4","id":"test","name":"Test","steps":[{"id":"step","request":{"method":"GET","path":"/","headers":{"X-Test":"value"}}}],"output":{}}`,
			want:       "request headers require spec \"http-workflow/v5\"",
		},
		{
			name:       "request headers must be an object",
			definition: `{"spec":"http-workflow/v5","id":"test","name":"Test","steps":[{"id":"step","request":{"method":"GET","path":"/","headers":"value"}}],"output":{}}`,
			want:       "request/headers must be an object",
		},
		{
			name:       "step name is unsupported",
			definition: `{"spec":"http-workflow/v1","id":"test","name":"Test","steps":[{"id":"step","name":"Step","request":{"method":"GET","path":"/"}}],"output":{}}`,
			want:       "unknown field \"name\"",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseGeneralWorkflow([]byte(test.definition))
			if err == nil || test.want != "" && !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected error containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestGeneralWorkflowForeachValidatesRuntimeSource(t *testing.T) {
	definition := mustParseGeneralWorkflow(t, `{
  "spec":"http-workflow/v3","id":"foreach-source","name":"Foreach source",
  "steps":[{"id":"request","foreach":{"alias":"items","as":"item"},"request":{"method":"GET","path":"/api"}}],
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
  "steps":[{"id":"request","request":{"method":"GET","path":"/api"},"extract":[{"alias":"value","expression":` + strconv.Quote(expression) + `}]}],
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
  "steps":[{"id":"request","request":{"method":"GET","path":"/api"}}],
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
  "steps":[{"id":"request","request":{"method":"GET","path":"/api"},"extract":[{"alias":"items","expression":".[]"}]}],
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
  "steps":[{"id":"request","request":{"method":"GET","path":"/api"},"extract":[{"alias":"value","expression":"def recurse: recurse; recurse"}]}],
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
    {"id":"absent","request":{"method":"POST","path":"/absent"},"expect":"$response.status == 204 and ($response.has_body | not)"},
    {"id":"null","request":{"method":"POST","path":"/null","body":null},"expect":"$response.status == 204"}
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
