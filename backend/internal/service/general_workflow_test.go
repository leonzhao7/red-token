package service

import (
	"context"
	"encoding/json"
	"errors"
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
