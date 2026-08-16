package handler

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"red-token/internal/domain"
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
	BackendType            string                  `json:"backend_type"`
	BaseURL                string                  `json:"base_url"`
	APIKeys                []backendFrontendAPIKey `json:"api_keys"`
	ConsoleURL             string                  `json:"console_url"`
	ConsoleUsername        string                  `json:"console_username"`
	ConsolePassword        string                  `json:"console_password"`
	ConsoleCheckinWorkflow string                  `json:"console_checkin_workflow_id"`
	ConsoleHeaders         map[string]string       `json:"console_headers"`
	ConsoleModels          string                  `json:"console_models"`
	ConsoleAccount         string                  `json:"console_account"`
	NewAPIRefresh          string                  `json:"new_api_refresh"`
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
	account := decodeJSONMap(backend.ConsoleAccountJSON)
	apiKeyQuotaFactor := frontendQuotaConversionFactor(account)
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
			UsedQuota:    apiKey.UsedQuota * apiKeyQuotaFactor,
		})
	}
	tags := append([]string(nil), backend.Tags...)
	if tags == nil {
		tags = []string{}
	}
	headers := newAPIConsoleHeaders(backend)
	if headers == nil {
		headers = map[string]string{}
	}
	if authorization := strings.TrimSpace(backend.ConsoleAuthorization); domain.NormalizeBackendType(backend.BackendType) == domain.BackendTypeSub2API && authorization != "" {
		if _, exists := headers["Authorization"]; !exists {
			headers["Authorization"] = authorization
		}
	}
	return backendFrontendView{
		ID:                     backend.ID,
		Name:                   backend.Name,
		Protocol:               domain.NormalizeBackendProtocol(backend.Protocol),
		BackendType:            domain.NormalizeBackendType(backend.BackendType),
		BaseURL:                backend.BaseURL,
		APIKeys:                apiKeys,
		ConsoleURL:             backend.ConsoleURL,
		ConsoleUsername:        backend.ConsoleUsername,
		ConsolePassword:        backend.ConsolePassword,
		ConsoleCheckinWorkflow: backend.ConsoleCheckinWorkflow,
		ConsoleHeaders:         headers,
		ConsoleModels:          frontendConsoleModelsJSON(backend.ConsolePricingJSON, backend.ConsoleAccountJSON),
		ConsoleAccount:         frontendConsoleAccountJSON(backend.ConsoleAccountJSON),
		NewAPIRefresh:          backend.NewAPIRefresh,
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
	quotaUnit := frontendQuotaUnit(account)
	quota, hasQuota := frontendNumber(account["quota"])
	usedQuota, hasUsedQuota := frontendNumber(account["used_quota"])
	todayReward, hasTodayReward := frontendNumber(account["today_reward"])
	valuesAlreadyFinal := frontendQuotaValuesAreFinal(account)

	if balance, ok := frontendNumber(account["balance"]); ok {
		quota, hasQuota = balance, true
	}
	if used, ok := frontendNumber(account["total_actual_cost"]); ok {
		usedQuota, hasUsedQuota = used, true
	}
	if reward, ok := frontendNumber(account["last_checkin_reward"]); ok && !hasTodayReward {
		todayReward, hasTodayReward = reward, true
	}
	if !valuesAlreadyFinal {
		factor := frontendQuotaConversionFactor(account)
		quota *= factor
		usedQuota *= factor
		todayReward *= factor
	}

	normalized := map[string]any{
		"id":           frontendString(account["id"]),
		"username":     firstNonEmpty(frontendString(account["username"]), frontendString(account["email"])),
		"quota":        valueOrZero(quota, hasQuota),
		"quota_unit":   quotaUnit,
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

func frontendQuotaValuesAreFinal(account map[string]any) bool {
	if strings.TrimSpace(frontendString(account["quota_unit"])) != "" {
		return true
	}
	_, hasBalance := account["balance"]
	_, hasActualCost := account["total_actual_cost"]
	return hasBalance || hasActualCost
}

func frontendQuotaConversionFactor(account map[string]any) float64 {
	if frontendQuotaValuesAreFinal(account) {
		return 1
	}
	unit, ok := frontendNumber(account["quota_per_unit"])
	if !ok || unit <= 0 {
		unit = 500000
	}
	exchangeRate, ok := frontendNumber(account["custom_currency_exchange_rate"])
	if !ok || exchangeRate <= 0 {
		exchangeRate = 1
	}
	return exchangeRate / unit
}

func frontendQuotaUnit(account map[string]any) string {
	if value := strings.TrimSpace(frontendString(account["quota_unit"])); value != "" {
		return value
	}
	displayType := strings.TrimSpace(frontendString(account["quota_display_type"]))
	if strings.EqualFold(displayType, "CUSTOM") {
		if symbol := strings.TrimSpace(frontendString(account["custom_currency_symbol"])); symbol != "" {
			return symbol
		}
	}
	if displayType != "" {
		return displayType
	}
	return "USD"
}

func frontendConsoleModelsJSON(pricingRaw, accountRaw string) string {
	var payload any
	if err := json.Unmarshal([]byte(pricingRaw), &payload); err != nil {
		return "[]"
	}
	var records []any
	groupRatios := map[string]any{}
	switch value := payload.(type) {
	case []any:
		records = value
	case map[string]any:
		records, _ = value["data"].([]any)
		groupRatios, _ = value["group_ratio"].(map[string]any)
	}
	if records == nil {
		return "[]"
	}
	account := decodeJSONMap(accountRaw)
	unit, ok := frontendNumber(account["quota_per_unit"])
	if !ok || unit <= 0 {
		unit = 500000
	}
	exchangeRate, ok := frontendNumber(account["custom_currency_exchange_rate"])
	if !ok || exchangeRate <= 0 {
		exchangeRate = 1
	}

	models := make([]any, 0, len(records))
	for _, rawRecord := range records {
		record, ok := rawRecord.(map[string]any)
		if !ok {
			continue
		}
		name := firstNonEmpty(frontendString(record["name"]), frontendString(record["model_name"]), frontendString(record["model"]))
		if name == "" {
			continue
		}
		priceTypeValue, _ := frontendNumber(firstPresent(record, "price_type", "quota_type"))
		priceType := 0
		if priceTypeValue == 1 {
			priceType = 1
		}
		groups, groupRatio := frontendCheapestGroups(record, groupRatios)
		model := map[string]any{
			"name":            name,
			"cheapest_groups": groups,
			"price_type":      priceType,
		}
		if priceType == 1 {
			price, _ := frontendNumber(firstPresent(record, "price", "model_price", "in_price", "input_price"))
			if _, alreadyFinal := record["price"]; !alreadyFinal {
				if _, workflowPrice := record["input_price"]; !workflowPrice {
					price *= groupRatio
				}
			}
			model["price"] = price
		} else {
			inPrice, directIn := frontendNumber(firstPresent(record, "in_price", "input_price", "prompt_price", "input_cost"))
			outPrice, directOut := frontendNumber(firstPresent(record, "out_price", "output_price", "completion_price", "output_cost"))
			if !directIn && !directOut {
				if tieredIn, tieredOut, ok := frontendTieredPrices(record, groupRatio, exchangeRate); ok {
					inPrice = tieredIn
					outPrice = tieredOut
				} else {
					ratio, _ := frontendNumber(firstPresent(record, "model_ratio", "model_price"))
					completionRatio, ok := frontendNumber(record["completion_ratio"])
					if !ok || completionRatio <= 0 {
						completionRatio = 1
					}
					inPrice = ratio * 1000000 / unit * exchangeRate * groupRatio
					outPrice = inPrice * completionRatio
				}
			} else if !directOut {
				outPrice = inPrice
			}
			model["in_price"] = inPrice
			model["out_price"] = outPrice
		}
		models = append(models, model)
	}
	return frontendJSONString(models, "[]")
}

var (
	frontendTierExpressionPattern = regexp.MustCompile(`(?i)tier\s*\(\s*["'][^"']+["']\s*,\s*([^)]*)\)`)
	frontendNumberPattern         = `[-+]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][-+]?\d+)?`
)

func frontendTieredPrices(record map[string]any, groupRatio, exchangeRate float64) (float64, float64, bool) {
	if !strings.EqualFold(strings.TrimSpace(frontendString(record["billing_mode"])), "tiered_expr") {
		return 0, 0, false
	}
	expression := frontendString(record["billing_expr"])
	match := frontendTierExpressionPattern.FindStringSubmatch(expression)
	if len(match) != 2 {
		return 0, 0, false
	}
	inputCoefficient, inputOK := frontendExpressionCoefficient(match[1], "p")
	outputCoefficient, outputOK := frontendExpressionCoefficient(match[1], "c")
	if !inputOK || !outputOK {
		return 0, 0, false
	}
	return groupRatio * inputCoefficient * exchangeRate,
		groupRatio * outputCoefficient * exchangeRate,
		true
}

func frontendExpressionCoefficient(expression, variable string) (float64, bool) {
	escaped := regexp.QuoteMeta(variable)
	patterns := []string{
		`(?i)\b` + escaped + `\b\s*\*\s*(` + frontendNumberPattern + `)`,
		`(?i)(` + frontendNumberPattern + `)\s*\*\s*\b` + escaped + `\b`,
	}
	for _, pattern := range patterns {
		match := regexp.MustCompile(pattern).FindStringSubmatch(expression)
		if len(match) != 2 {
			continue
		}
		coefficient, err := strconv.ParseFloat(match[1], 64)
		if err == nil && !math.IsNaN(coefficient) && !math.IsInf(coefficient, 0) {
			return coefficient, true
		}
	}
	if regexp.MustCompile(`(?i)\b` + escaped + `\b`).MatchString(expression) {
		return 1, true
	}
	return 0, false
}

func frontendCheapestGroups(record, ratios map[string]any) ([]string, float64) {
	if groups := frontendStringList(record["cheapest_groups"]); len(groups) > 0 {
		return groups, 1
	}
	groups := frontendStringList(record["enable_groups"])
	if len(groups) == 0 {
		return []string{}, 1
	}
	minimum := math.Inf(1)
	cheapest := make([]string, 0, len(groups))
	for _, group := range groups {
		ratio, ok := frontendNumber(ratios[group])
		if !ok || ratio < 0 {
			continue
		}
		switch {
		case ratio < minimum:
			minimum = ratio
			cheapest = []string{group}
		case ratio == minimum:
			cheapest = append(cheapest, group)
		}
	}
	if len(cheapest) == 0 {
		return groups, 1
	}
	return cheapest, minimum
}

func frontendStringList(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		if values, ok := value.([]string); ok {
			return append([]string(nil), values...)
		}
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

func firstPresent(object map[string]any, fields ...string) any {
	for _, field := range fields {
		if value, exists := object[field]; exists && value != nil {
			return value
		}
	}
	return nil
}

func frontendNumber(value any) (float64, bool) {
	if number, ok := workflowOutputNumber(value); ok {
		return number, true
	}
	if text, ok := value.(string); ok {
		number, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		return number, err == nil && !math.IsNaN(number) && !math.IsInf(number, 0)
	}
	return 0, false
}

func frontendString(value any) string {
	if value == nil {
		return ""
	}
	text := fmt.Sprint(value)
	if text == "<nil>" {
		return ""
	}
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
