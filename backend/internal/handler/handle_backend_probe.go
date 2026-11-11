package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"red-token/internal/domain"
	"red-token/internal/proxy"
)

const (
	toolProbeName    = "bash"
	toolProbeCommand = "ls -la"
	toolProbeMaxBody = 1 << 20
)

type backendToolProbeRequest struct {
	Model      string `json:"model"`
	KeyIndex   *int   `json:"key_index"`
	Endpoint   string `json:"endpoint"`
	ToolChoice string `json:"tool_choice"`
}

type backendToolProbeResult struct {
	Status        string          `json:"status"`
	Message       string          `json:"message"`
	Model         string          `json:"model"`
	UpstreamModel string          `json:"upstream_model"`
	KeyIndex      int             `json:"key_index"`
	Endpoint      string          `json:"endpoint"`
	ToolChoice    string          `json:"tool_choice"`
	HTTPStatus    int             `json:"http_status"`
	DurationMS    int64           `json:"duration_ms"`
	CheckedAt     time.Time       `json:"checked_at"`
	ToolName      string          `json:"tool_name,omitempty"`
	Arguments     json.RawMessage `json:"arguments,omitempty"`
	ResponseBody  string          `json:"response_body,omitempty"`
}

// HandleBackendToolProbe sends one request directly to the selected backend.
// A capability probe must not fail over or change the backend's health status.
func (h *BackendHandler) HandleBackendToolProbe(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	backend, err := h.store.GetBackend(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "backend not found")
		return
	}
	var payload backendToolProbeRequest
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	payload.Model = strings.TrimSpace(payload.Model)
	if payload.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}
	keyIndex := 0
	if payload.KeyIndex != nil {
		keyIndex = *payload.KeyIndex
	}
	if keyIndex < 0 || keyIndex >= len(backend.APIKeys) {
		writeError(w, http.StatusBadRequest, "invalid key_index or no API Key configured")
		return
	}
	key := backend.APIKeys[keyIndex]
	if strings.TrimSpace(key.APIKey) == "" {
		writeError(w, http.StatusBadRequest, "selected API Key is empty")
		return
	}
	if err := validateURL(backend.BaseURL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	protocol := domain.NormalizeBackendProtocol(backend.Protocol)
	if payload.Endpoint == "" {
		payload.Endpoint = domain.EndpointChat
		if protocol == domain.BackendProtocolAnthropic {
			payload.Endpoint = domain.EndpointMessages
		}
	}
	if (payload.Endpoint != domain.EndpointChat && payload.Endpoint != domain.EndpointResponses && payload.Endpoint != domain.EndpointMessages) ||
		(protocol == domain.BackendProtocolAnthropic && payload.Endpoint != domain.EndpointMessages) ||
		(protocol == domain.BackendProtocolOpenAI && payload.Endpoint == domain.EndpointMessages) {
		writeError(w, http.StatusBadRequest, "endpoint is not compatible with backend protocol")
		return
	}
	if payload.ToolChoice == "" {
		payload.ToolChoice = "auto"
	}
	if payload.ToolChoice != "auto" && payload.ToolChoice != "forced" {
		writeError(w, http.StatusBadRequest, "tool_choice must be auto or forced")
		return
	}

	upstreamModel := mappedToolProbeModel(key, payload.Model)
	backend.APIKey = key.APIKey
	result := backendToolProbeResult{
		Status: "inconclusive", Model: payload.Model, UpstreamModel: upstreamModel,
		KeyIndex: keyIndex, Endpoint: payload.Endpoint, ToolChoice: payload.ToolChoice,
		CheckedAt: time.Now().UTC(),
	}
	connectTimeout, requestTimeout := 10*time.Second, 120*time.Second
	if h.cfg != nil {
		if h.cfg.ConnectTimeout > 0 {
			connectTimeout = h.cfg.ConnectTimeout
		}
		if h.cfg.RequestTimeout > 0 {
			requestTimeout = h.cfg.RequestTimeout
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	started := time.Now()
	h.runBackendToolProbe(ctx, backend, &result, connectTimeout, requestTimeout)
	result.DurationMS = time.Since(started).Milliseconds()
	// Upstreams sometimes echo credentials in errors. Keep them out of results.
	for _, secret := range []string{strings.TrimSpace(key.APIKey), strings.TrimSpace(strings.TrimPrefix(key.APIKey, "Bearer "))} {
		if secret != "" {
			result.Message = strings.ReplaceAll(result.Message, secret, "[redacted]")
			result.ResponseBody = strings.ReplaceAll(result.ResponseBody, secret, "[redacted]")
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func mappedToolProbeModel(key domain.BackendAPIKey, clientModel string) string {
	clientModel = strings.TrimSpace(clientModel)
	if mapped := strings.TrimSpace(key.ModelMapping[clientModel]); mapped != "" {
		return mapped
	}
	return clientModel
}

func (h *BackendHandler) runBackendToolProbe(ctx context.Context, backend domain.Backend, result *backendToolProbeResult, connectTimeout, requestTimeout time.Duration) {
	client, err := proxy.NewHTTPClientForBackend(backend, connectTimeout, requestTimeout)
	if err != nil {
		result.Message = "无法连接中转站：" + err.Error()
		return
	}
	defer client.CloseIdleConnections()
	path, body := toolProbePayload(result.UpstreamModel, result.Endpoint, result.ToolChoice)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		result.Message = "无法创建检测请求：" + err.Error()
		return
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Encoding", "identity")
	if result.Endpoint == domain.EndpointMessages {
		request.Header.Set("Anthropic-Version", "2023-06-01")
	}
	response, err := proxy.NewWithHTTPClient(client).Do(ctx, request, backend, body)
	if err != nil {
		result.Message = "检测请求失败，无法判断工具调用支持情况：" + err.Error()
		return
	}
	defer response.Body.Close()
	result.HTTPStatus = response.StatusCode
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, toolProbeMaxBody+1))
	if err != nil {
		result.Message = "读取检测响应失败：" + err.Error()
		return
	}
	if len(responseBody) > toolProbeMaxBody {
		result.Message = "检测响应超过 1 MiB，无法判断"
		return
	}
	preview := []rune(string(responseBody))
	if len(preview) > 16000 {
		preview = append(preview[:16000], []rune("\n…（响应已截断）")...)
	}
	result.ResponseBody = string(preview)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		result.Message = "中转站请求失败（HTTP " + strconv.Itoa(response.StatusCode) + "），请查看响应详情；本次无法判断工具调用支持情况"
		return
	}
	evaluateToolProbeResponse(responseBody, result)
}

func toolProbePayload(model, endpoint, choice string) (string, []byte) {
	parameters := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{"type": "string", "description": "Bash command to run in the local workspace"},
		},
		"required": []string{"command"}, "additionalProperties": false,
	}
	prompt := "Run `" + toolProbeCommand + "` in the current directory."
	function := map[string]any{"name": toolProbeName, "description": "Execute a bash command in the terminal.", "parameters": parameters}
	payload := map[string]any{"model": model, "stream": false}
	path := "/v1/chat/completions"
	switch endpoint {
	case domain.EndpointResponses:
		path = "/v1/responses"
		payload["input"] = prompt
		payload["max_output_tokens"] = 2048
		payload["tools"] = []any{map[string]any{"type": "function", "name": toolProbeName, "description": function["description"], "parameters": parameters}}
		payload["tool_choice"] = "auto"
		if choice == "forced" {
			payload["tool_choice"] = map[string]any{"type": "function", "name": toolProbeName}
		}
	case domain.EndpointMessages:
		path = "/v1/messages"
		payload["messages"] = []any{map[string]any{"role": "user", "content": prompt}}
		payload["max_tokens"] = 2048
		payload["tools"] = []any{map[string]any{"name": toolProbeName, "description": function["description"], "input_schema": parameters}}
		payload["tool_choice"] = map[string]any{"type": "auto"}
		if choice == "forced" {
			payload["tool_choice"] = map[string]any{"type": "tool", "name": toolProbeName}
		}
	default:
		payload["messages"] = []any{map[string]any{"role": "user", "content": prompt}}
		payload["max_completion_tokens"] = 2048
		payload["tools"] = []any{map[string]any{"type": "function", "function": function}}
		payload["tool_choice"] = "auto"
		if choice == "forced" {
			payload["tool_choice"] = map[string]any{"type": "function", "function": map[string]any{"name": toolProbeName}}
		}
	}
	body, _ := json.Marshal(payload)
	return path, body
}

func evaluateToolProbeResponse(body []byte, result *backendToolProbeResult) {
	var payload struct {
		Error      json.RawMessage `json:"error"`
		Status     string          `json:"status"`
		StopReason string          `json:"stop_reason"`
		Type       string          `json:"type"`
		Role       string          `json:"role"`
		Choices    []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Role      string          `json:"role"`
				Refusal   json.RawMessage `json:"refusal"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Output []struct {
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Type string `json:"type"`
			} `json:"content"`
		} `json:"output"`
		Content []struct {
			Type  string          `json:"type"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || (len(payload.Error) > 0 && string(payload.Error) != "null") {
		result.Message = "中转站返回错误或无效的 JSON，无法判断；请查看响应详情"
		return
	}
	validResponse, interrupted := false, false
	switch result.Endpoint {
	case domain.EndpointChat:
		for _, choice := range payload.Choices {
			if choice.Message.Role != "assistant" {
				continue
			}
			validResponse = validResponse || choice.FinishReason != ""
			interrupted = interrupted || choice.FinishReason == "length" || choice.FinishReason == "content_filter" || (len(choice.Message.Refusal) > 0 && string(choice.Message.Refusal) != "null" && string(choice.Message.Refusal) != `""`)
			for _, call := range choice.Message.ToolCalls {
				if call.Type == "function" && acceptToolProbeCall(call.ID, call.Function.Name, []byte(call.Function.Arguments), result) {
					return
				}
			}
		}
	case domain.EndpointResponses:
		validResponse = payload.Output != nil && payload.Status == "completed"
		interrupted = payload.Status == "incomplete" || payload.Status == "failed" || payload.Status == "cancelled" || payload.Status == "in_progress" || payload.Status == "queued"
		for _, call := range payload.Output {
			for _, content := range call.Content {
				interrupted = interrupted || content.Type == "refusal"
			}
			if call.Type == "function_call" && acceptToolProbeCall(call.CallID, call.Name, []byte(call.Arguments), result) {
				return
			}
		}
	case domain.EndpointMessages:
		validResponse = payload.Content != nil && payload.Type == "message" && payload.Role == "assistant" && payload.StopReason != ""
		interrupted = payload.StopReason == "max_tokens" || payload.StopReason == "refusal" || payload.StopReason == "pause_turn"
		for _, call := range payload.Content {
			if call.Type == "tool_use" && acceptToolProbeCall(call.ID, call.Name, call.Input, result) {
				return
			}
		}
	}
	if !validResponse || interrupted {
		result.Message = "响应格式异常、生成未完成或请求被拒绝，无法判断；请查看响应详情"
		return
	}
	result.Status = "unsupported"
	result.Message = "未通过检测：未收到工具名称、调用 ID 和参数均有效的结构化工具调用。可切换调用模式重试，本次结果不代表模型在所有接口下都不支持工具调用。"
}

func acceptToolProbeCall(id, name string, arguments []byte, result *backendToolProbeResult) bool {
	var values map[string]any
	if strings.TrimSpace(id) == "" || name != toolProbeName || json.Unmarshal(arguments, &values) != nil || len(values) != 1 {
		return false
	}
	command, ok := values["command"].(string)
	if !ok || strings.TrimSpace(command) != toolProbeCommand {
		return false
	}
	result.Status = "supported"
	result.Message = "支持工具调用：收到结构化工具调用，工具名称、调用 ID 和参数校验通过"
	result.ToolName = name
	result.Arguments = append(json.RawMessage(nil), arguments...)
	return true
}
