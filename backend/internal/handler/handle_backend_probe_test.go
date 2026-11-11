package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"red-token/internal/config"
	"red-token/internal/domain"
	"red-token/internal/store"
)

const probeChatSuccess = `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"call_123","type":"function","function":{"name":"bash","arguments":"{\"command\":\"ls -la\"}"}}]}}]}`
const probeResponsesSuccess = `{"status":"completed","output":[{"type":"function_call","call_id":"call_123","name":"bash","arguments":"{\"command\":\"ls -la\"}"}]}`
const probeMessagesSuccess = `{"type":"message","role":"assistant","stop_reason":"tool_use","content":[{"type":"tool_use","id":"call_123","name":"bash","input":{"command":"ls -la"}}]}`

func TestMappedToolProbeModel(t *testing.T) {
	key := domain.BackendAPIKey{ModelMapping: map[string]string{"client-model": "provider-model"}}
	if got := mappedToolProbeModel(key, " client-model "); got != "provider-model" {
		t.Fatalf("mapped model = %q, want provider-model", got)
	}
	if got := mappedToolProbeModel(key, "other-model"); got != "other-model" {
		t.Fatalf("unmapped model = %q, want other-model", got)
	}
}

func TestBackendToolProbeProtocolsAndSelection(t *testing.T) {
	for _, endpoint := range []string{"chat", "responses", "messages"} {
		for _, mode := range []string{"auto", "forced"} {
			t.Run(endpoint+"/"+mode, func(t *testing.T) {
				var calls atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					path := map[string]string{"chat": "/prefix/v1/chat/completions", "responses": "/prefix/v1/responses", "messages": "/prefix/v1/messages"}[endpoint]
					if r.Method != http.MethodPost || r.URL.Path != path {
						t.Errorf("request=%s %s, want POST %s", r.Method, r.URL.Path, path)
					}
					if r.Header.Get("Cookie") != "" || r.Header.Get("X-Console") != "" {
						t.Error("admin or console headers leaked to upstream")
					}
					if endpoint == "messages" {
						if r.Header.Get("X-Api-Key") != "sk-selected-probe" || r.Header.Get("Anthropic-Version") != "2023-06-01" || r.Header.Get("Authorization") != "" {
							t.Errorf("unexpected Anthropic headers: %v", r.Header)
						}
					} else if r.Header.Get("Authorization") != "Bearer sk-selected-probe" || r.Header.Get("X-Api-Key") != "" {
						t.Errorf("unexpected OpenAI headers: %v", r.Header)
					}
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Errorf("decode request: %v", err)
						return
					}
					if body["model"] != "actual-upstream-model" || body["stream"] != false {
						t.Errorf("unexpected model or stream: %v", body)
					}
					tools, ok := body["tools"].([]any)
					if !ok || len(tools) != 1 {
						t.Errorf("invalid tools: %v", body)
						return
					}
					tool := tools[0].(map[string]any)
					var schema map[string]any
					var prompt string
					switch endpoint {
					case "chat":
						function := tool["function"].(map[string]any)
						if tool["type"] != "function" || function["name"] != "bash" {
							t.Errorf("invalid chat tool: %v", tool)
						}
						schema = function["parameters"].(map[string]any)
						prompt = body["messages"].([]any)[0].(map[string]any)["content"].(string)
						if mode == "forced" {
							choice := body["tool_choice"].(map[string]any)
							if choice["type"] != "function" || choice["function"].(map[string]any)["name"] != "bash" {
								t.Errorf("invalid forced choice: %v", choice)
							}
						} else if body["tool_choice"] != "auto" {
							t.Errorf("invalid auto choice: %v", body["tool_choice"])
						}
					case "responses":
						if tool["type"] != "function" || tool["name"] != "bash" || body["input"] == nil {
							t.Errorf("invalid responses request: %v", body)
						}
						schema = tool["parameters"].(map[string]any)
						prompt = body["input"].(string)
						if mode == "forced" {
							choice := body["tool_choice"].(map[string]any)
							if choice["type"] != "function" || choice["name"] != "bash" {
								t.Errorf("invalid forced choice: %v", choice)
							}
						} else if body["tool_choice"] != "auto" {
							t.Errorf("invalid auto choice: %v", body["tool_choice"])
						}
					case "messages":
						if tool["name"] != "bash" || body["max_tokens"] == nil {
							t.Errorf("invalid messages request: %v", body)
						}
						schema = tool["input_schema"].(map[string]any)
						prompt = body["messages"].([]any)[0].(map[string]any)["content"].(string)
						choice := body["tool_choice"].(map[string]any)
						if mode == "forced" && (choice["type"] != "tool" || choice["name"] != "bash") || mode == "auto" && choice["type"] != "auto" {
							t.Errorf("invalid messages choice: %v", choice)
						}
					}
					command := schema["properties"].(map[string]any)["command"].(map[string]any)
					if schema["type"] != "object" || command["type"] != "string" || prompt != "Run `ls -la` in the current directory." ||
						strings.Contains(strings.ToLower(prompt), "red") || strings.Contains(strings.ToLower(prompt), "token") || strings.Contains(strings.ToLower(prompt), "probe") {
						t.Errorf("invalid schema: %v", schema)
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(map[string]string{"chat": probeChatSuccess, "responses": probeResponsesSuccess, "messages": probeMessagesSuccess}[endpoint]))
				}))
				defer upstream.Close()
				st := openWorkflowHandlerStore(t)
				backend, err := st.CreateBackend(context.Background(), domain.Backend{
					Name: "probe-relay", Protocol: domain.BackendProtocolBoth, BaseURL: upstream.URL + "/prefix/v1",
					APIKeys: []domain.BackendAPIKey{
						{APIKey: "sk-first-probe", Group: "default", Models: []string{"alias"}},
						{APIKey: "sk-selected-probe", Group: "selected", Models: []string{"alias"}, ModelMapping: map[string]string{"alias": "actual-upstream-model"}},
					},
					ConsoleHeaders: map[string]string{"X-Console": "private"},
				})
				if err != nil {
					t.Fatal(err)
				}
				// Probing must still reach this specific relay when disabled.
				disabled := domain.BackendStatusDisabled
				if _, err := st.PatchBackend(context.Background(), backend.ID, store.BackendPatch{Status: &disabled}); err != nil {
					t.Fatal(err)
				}
				h := NewBackendHandler(st)
				result, code := runProbeHandler(t, h, backend.ID, `{"model":" alias ","key_index":1,"endpoint":"`+endpoint+`","tool_choice":"`+mode+`"}`)
				if code != http.StatusOK || result.Status != "supported" || result.UpstreamModel != "actual-upstream-model" || result.KeyIndex != 1 || result.HTTPStatus != 200 || calls.Load() != 1 {
					t.Fatalf("status=%d result=%+v calls=%d", code, result, calls.Load())
				}
				updated, err := st.GetBackend(context.Background(), backend.ID)
				if err != nil || updated.Status != disabled || updated.ConsecutiveFailures != backend.ConsecutiveFailures {
					t.Fatalf("probe changed health state: %+v, %v", updated, err)
				}
			})
		}
	}
}

func TestEvaluateToolProbeDoesNotAcceptTextOrInvalidCalls(t *testing.T) {
	tests := []struct{ name, endpoint, body, status string }{
		{"chat success", "chat", probeChatSuccess, "supported"},
		{"responses success", "responses", probeResponsesSuccess, "supported"},
		{"messages success", "messages", probeMessagesSuccess, "supported"},
		{"text imitation", "chat", `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"bash({command: ls -la})"}}]}`, "unsupported"},
		{"missing call id", "chat", strings.Replace(probeChatSuccess, `"id":"call_123",`, "", 1), "unsupported"},
		{"wrong tool", "chat", strings.Replace(probeChatSuccess, "bash", "other_tool", 1), "unsupported"},
		{"invalid JSON arguments", "chat", strings.Replace(probeChatSuccess, `{\"command\":\"ls -la\"}`, "not-json", 1), "unsupported"},
		{"wrong command", "messages", strings.Replace(probeMessagesSuccess, "ls -la", "pwd", 1), "unsupported"},
		{"extra arguments", "messages", strings.Replace(probeMessagesSuccess, `"command":"ls -la"`, `"command":"ls -la","extra":true`, 1), "unsupported"},
		{"responses text", "responses", `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}]}`, "unsupported"},
		{"messages text", "messages", `{"type":"message","role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"hello"}]}`, "unsupported"},
		{"truncated chat", "chat", `{"choices":[{"finish_reason":"length","message":{"role":"assistant","content":""}}]}`, "inconclusive"},
		{"incomplete responses", "responses", `{"status":"incomplete","output":[]}`, "inconclusive"},
		{"responses refusal", "responses", `{"status":"completed","output":[{"type":"message","content":[{"type":"refusal","refusal":"denied"}]}]}`, "inconclusive"},
		{"messages refusal", "messages", `{"type":"message","role":"assistant","stop_reason":"refusal","content":[]}`, "inconclusive"},
		{"chat refusal", "chat", `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","refusal":"denied"}}]}`, "inconclusive"},
		{"JSON error", "chat", `{"error":{"message":"tools unsupported"}}`, "inconclusive"},
		{"wrong response shape", "chat", probeMessagesSuccess, "inconclusive"},
		{"empty JSON", "chat", `{}`, "inconclusive"},
		{"invalid JSON", "responses", `<html>Error</html>`, "inconclusive"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := backendToolProbeResult{Status: "inconclusive", Endpoint: test.endpoint}
			evaluateToolProbeResponse([]byte(test.body), &result)
			if result.Status != test.status || result.Message == "" {
				t.Fatalf("result=%+v, want %s", result, test.status)
			}
		})
	}
}

func TestBackendToolProbeErrorsAndTimeout(t *testing.T) {
	for _, status := range []int{400, 401, 429, 503, 0} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if status == 0 {
					select {
					case <-r.Context().Done():
					case <-time.After(200 * time.Millisecond):
					}
					return
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"message":"error for sk-error-probe"}}`))
			}))
			defer upstream.Close()
			st := openWorkflowHandlerStore(t)
			backend, err := st.CreateBackend(context.Background(), domain.Backend{Name: "errors", BaseURL: upstream.URL, APIKey: "sk-error-probe", Models: []string{"model"}})
			if err != nil {
				t.Fatal(err)
			}
			h := NewBackendHandler(st)
			h.SetConfig(&config.Config{RequestTimeout: time.Second})
			if status == 0 {
				h.SetConfig(&config.Config{RequestTimeout: 50 * time.Millisecond})
			}
			result, code := runProbeHandler(t, h, backend.ID, `{"model":"model"}`)
			if code != 200 || result.Status != "inconclusive" || result.HTTPStatus != status || strings.Contains(result.ResponseBody, "sk-error-probe") {
				t.Fatalf("code=%d result=%+v", code, result)
			}
		})
	}
}

func TestBackendToolProbeRejectsInvalidConfiguration(t *testing.T) {
	st := openWorkflowHandlerStore(t)
	backend, err := st.CreateBackend(context.Background(), domain.Backend{Name: "validation", Protocol: "anthropic", BaseURL: "https://example.invalid", APIKey: "sk-validation", Models: []string{"model"}})
	if err != nil {
		t.Fatal(err)
	}
	h := NewBackendHandler(st)
	for _, body := range []string{`{}`, `{"model":" "}`, `{"model":"model","key_index":-1}`, `{"model":"model","key_index":2}`, `{"model":"model","endpoint":"chat"}`, `{"model":"model","endpoint":"embeddings"}`, `{"model":"model","tool_choice":"none"}`, `{"model":"model","unknown":true}`} {
		_, code := runProbeHandler(t, h, backend.ID, body)
		if code != 400 {
			t.Errorf("body=%s status=%d, want 400", body, code)
		}
	}
	_, code := runProbeHandler(t, h, backend.ID+1, `{"model":"model"}`)
	if code != 404 {
		t.Errorf("missing backend status=%d, want 404", code)
	}
	empty, err := st.CreateBackend(context.Background(), domain.Backend{Name: "no-keys", BaseURL: "https://example.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	_, code = runProbeHandler(t, h, empty.ID, `{"model":"model"}`)
	if code != 400 {
		t.Errorf("no API keys status=%d, want 400", code)
	}
}

func TestBackendToolProbeUsesConfiguredProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "upstream.invalid" || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("proxy changed upstream host or path: %s %s", r.Host, r.URL.Path)
		}
		_, _ = w.Write([]byte(probeChatSuccess))
	}))
	defer upstream.Close()
	backend := domain.Backend{
		BaseURL: "http://upstream.invalid/v1", APIKey: "sk-proxy-probe", ProxyID: 1,
		Proxy: &domain.ProxyServer{ID: 1, Name: "test-proxy", Protocol: domain.ProxyProtocolTransparent, Address: strings.TrimPrefix(upstream.URL, "http://"), Enabled: true},
	}
	h := NewBackendHandler(nil)
	result := backendToolProbeResult{Status: "inconclusive", UpstreamModel: "model", Endpoint: "chat", ToolChoice: "auto"}
	h.runBackendToolProbe(context.Background(), backend, &result, time.Second, time.Second)
	if result.Status != "supported" {
		t.Fatalf("configured proxy result=%+v", result)
	}
	backend.Proxy.Enabled = false
	result.Status = "inconclusive"
	h.runBackendToolProbe(context.Background(), backend, &result, time.Second, time.Second)
	if result.Status != "inconclusive" || !strings.Contains(result.Message, "disabled") {
		t.Fatalf("disabled proxy result=%+v", result)
	}
}

func TestBackendToolProbeBoundsResponseBody(t *testing.T) {
	for _, size := range []int{17000, toolProbeMaxBody + 1} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(probeChatSuccess + strings.Repeat(" ", size)))
			}))
			defer upstream.Close()
			result := backendToolProbeResult{Status: "inconclusive", UpstreamModel: "model", Endpoint: "chat", ToolChoice: "auto"}
			NewBackendHandler(nil).runBackendToolProbe(context.Background(), domain.Backend{BaseURL: upstream.URL, APIKey: "sk-bounds"}, &result, time.Second, time.Second)
			if size > toolProbeMaxBody {
				if result.Status != "inconclusive" || !strings.Contains(result.Message, "1 MiB") {
					t.Fatalf("oversized result=%+v", result)
				}
			} else if result.Status != "supported" || !strings.HasSuffix(result.ResponseBody, "…（响应已截断）") || len([]rune(result.ResponseBody)) > 16100 {
				t.Fatalf("bounded preview status=%s length=%d", result.Status, len(result.ResponseBody))
			}
		})
	}
}

func runProbeHandler(t *testing.T, h *BackendHandler, id int64, body string) (backendToolProbeResult, int) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/api/backends/{id}/tool-probe", h.HandleBackendToolProbe)
	r := httptest.NewRequest(http.MethodPost, "/admin/api/backends/"+strconv.FormatInt(id, 10)+"/tool-probe", strings.NewReader(body))
	r.Header.Set("Cookie", "admin-session=private")
	r.Header.Set("Authorization", "Bearer admin-token")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	var result backendToolProbeResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v; %s", err, w.Body.String())
	}
	return result, w.Code
}
