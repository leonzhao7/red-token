package handler

import (
	"encoding/json"
	"strings"
	"time"

	"red-token/internal/domain"
	"red-token/internal/service"
)

type backendFrontendAPIKey struct {
	ID           string            `json:"id"`
	Key          string            `json:"key"`
	Name         string            `json:"name"`
	Group        string            `json:"group"`
	Models       []string          `json:"models"`
	ModelMapping map[string]string `json:"model_mapping"`
	UsedQuota    float64           `json:"used_quota"`
}

type backendFrontendView struct {
	ID                     int64                   `json:"id"`
	Name                   string                  `json:"name"`
	Protocol               string                  `json:"protocol"`
	BaseURL                string                  `json:"base_url"`
	APIKeys                []backendFrontendAPIKey `json:"api_keys"`
	ConsoleURL             string                  `json:"console_url"`
	ConsoleUsername        string                  `json:"console_username"`
	ConsolePassword        string                  `json:"console_password"`
	ConsoleRefreshToken    string                  `json:"console_refresh_token"`
	ConsoleCheckinWorkflow string                  `json:"console_checkin_workflow_id"`
	ManualCheckin          bool                    `json:"manual_checkin"`
	Frozen                 bool                    `json:"frozen"`
	ConsoleHeaders         map[string]string       `json:"console_headers"`
	ConsoleModels          string                  `json:"console_models"`
	ConsoleAccount         string                  `json:"console_account"`
	Notes                  string                  `json:"notes"`
	ProxyID                int64                   `json:"proxy_id"`
	Status                 string                  `json:"status"`
	Weight                 int                     `json:"weight"`
	CreatedAt              time.Time               `json:"created_at"`
	UpdatedAt              time.Time               `json:"updated_at"`
	AvgLatencyMS           float64                 `json:"avg_latency_ms"`
	Tags                   []string                `json:"tags"`
}

func buildBackendFrontendViews(backends []domain.Backend, averageLatency map[int64]float64) []backendFrontendView {
	views := make([]backendFrontendView, 0, len(backends))
	for _, backend := range backends {
		views = append(views, buildBackendFrontendView(backend, averageLatency[backend.ID]))
	}
	return views
}

func buildBackendFrontendView(backend domain.Backend, avgLatencyMS float64) backendFrontendView {
	apiKeys := make([]backendFrontendAPIKey, 0, len(backend.APIKeys))
	for _, apiKey := range backend.APIKeys {
		models := append([]string(nil), apiKey.Models...)
		if models == nil {
			models = []string{}
		}
		mapping := copyStringMap(apiKey.ModelMapping)
		apiKeys = append(apiKeys, backendFrontendAPIKey{
			ID:           apiKey.ID,
			Key:          apiKey.APIKey,
			Name:         apiKey.Name,
			Group:        apiKey.Group,
			Models:       models,
			ModelMapping: mapping,
			UsedQuota:    apiKey.UsedQuota,
		})
	}
	tags := append([]string(nil), backend.Tags...)
	if tags == nil {
		tags = []string{}
	}
	headers := service.ConsoleHeaders(backend)
	if headers == nil {
		headers = map[string]string{}
	}
	return backendFrontendView{
		ID:                     backend.ID,
		Name:                   backend.Name,
		Protocol:               domain.NormalizeBackendProtocol(backend.Protocol),
		BaseURL:                backend.BaseURL,
		APIKeys:                apiKeys,
		ConsoleURL:             backend.ConsoleURL,
		ConsoleUsername:        backend.ConsoleUsername,
		ConsolePassword:        backend.ConsolePassword,
		ConsoleRefreshToken:    backend.ConsoleRefreshToken,
		ConsoleCheckinWorkflow: backend.ConsoleCheckinWorkflow,
		ManualCheckin:          backend.ManualCheckin,
		Frozen:                 backend.Frozen,
		ConsoleHeaders:         headers,
		ConsoleModels:          frontendConsoleModelsJSON(backend.ConsolePricingJSON),
		ConsoleAccount:         frontendConsoleAccountJSON(backend.ConsoleAccountJSON),
		Notes:                  backend.Notes,
		ProxyID:                backend.ProxyID,
		Status:                 backend.Status,
		Weight:                 backend.Weight,
		CreatedAt:              backend.CreatedAt,
		UpdatedAt:              backend.UpdatedAt,
		AvgLatencyMS:           avgLatencyMS,
		Tags:                   tags,
	}
}

func frontendConsoleAccountJSON(raw string) string {
	account := decodeJSONMap(raw)
	if len(account) == 0 {
		return "{}"
	}
	quota, hasQuota := frontendNumber(account["quota"])
	usedQuota, hasUsedQuota := frontendNumber(account["used_quota"])
	todayReward, hasTodayReward := frontendNumber(account["today_reward"])

	normalized := map[string]any{
		"id":           frontendString(account["id"]),
		"username":     firstNonEmpty(frontendString(account["username"]), frontendString(account["email"])),
		"quota":        valueOrZero(quota, hasQuota),
		"quota_unit":   frontendString(account["quota_unit"]),
		"used_quota":   valueOrZero(usedQuota, hasUsedQuota),
		"today_reward": valueOrZero(todayReward, hasTodayReward),
	}
	for _, field := range []string{"last_checkin_at", "last_workflow_at"} {
		if value := strings.TrimSpace(frontendString(account[field])); value != "" {
			normalized[field] = value
		}
	}
	return frontendJSONString(normalized, "{}")
}

func frontendConsoleModelsJSON(pricingRaw string) string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(pricingRaw), &payload); err != nil {
		return "[]"
	}
	records, _ := payload["data"].([]any)
	if records == nil {
		return "[]"
	}

	models := make([]any, 0, len(records))
	for _, rawRecord := range records {
		record, ok := rawRecord.(map[string]any)
		if !ok {
			continue
		}
		name := frontendString(record["model_name"])
		if name == "" {
			continue
		}
		priceTypeValue, _ := frontendNumber(record["price_type"])
		priceType := 0
		if priceTypeValue == 1 {
			priceType = 1
		}
		model := map[string]any{
			"name":            name,
			"cheapest_groups": frontendStringList(record["enable_groups"]),
			"price_type":      priceType,
		}
		if priceType == 1 {
			price, _ := frontendNumber(record["price"])
			model["price"] = price
		} else {
			inPrice, _ := frontendNumber(record["input_price"])
			outPrice, _ := frontendNumber(record["output_price"])
			model["in_price"] = inPrice
			model["out_price"] = outPrice
		}
		models = append(models, model)
	}
	return frontendJSONString(models, "[]")
}

func frontendStringList(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return []string{}
	}
	values := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		value := strings.TrimSpace(frontendString(item))
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

func frontendNumber(value any) (float64, bool) {
	number, ok := workflowOutputNumber(value)
	return number, ok
}

func frontendString(value any) string {
	text, _ := value.(string)
	return text
}

func valueOrZero(value float64, ok bool) float64 {
	if !ok {
		return 0
	}
	return value
}

func frontendJSONString(value any, fallback string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fallback
	}
	return string(encoded)
}
