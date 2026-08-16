package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateCheckinWorkflowOutputTemplate(t *testing.T) {
	valid := json.RawMessage(`{"user_id":"{{user_id}}","username":"{{username}}","quota":"{{quota}}","quota_unit":"{{quota_unit}}","used_quota":"{{used_quota}}","today_reward":"{{today_reward}}","api_keys":"{{api_keys}}","models":"{{models}}"}`)
	if err := ValidateCheckinWorkflowOutputTemplate(valid); err != nil {
		t.Fatalf("validate output template: %v", err)
	}
	legacy := json.RawMessage(`{"user_id":"{{user_id}}","username":"{{username}}","balance":"{{quota}}","quota_unit":"{{quota_unit}}","used_quota":"{{used_quota}}","today_reward":"{{today_reward}}","api_keys":"{{api_keys}}","models":"{{models}}"}`)
	if err := ValidateCheckinWorkflowOutputTemplate(legacy); err == nil || !strings.Contains(err.Error(), "$.balance is not allowed") {
		t.Fatalf("expected legacy balance to be rejected, got %v", err)
	}
}

func TestValidateCheckinWorkflowOutput(t *testing.T) {
	valid := map[string]any{
		"user_id":      "user-1",
		"username":     "alice",
		"quota":        2400.0,
		"quota_unit":   "USD",
		"used_quota":   3.75,
		"today_reward": 123.0,
		"api_keys": []any{
			map[string]any{"id": "key-1", "name": "main", "key": "sk-value", "group": "default", "used_quota": 3.25},
		},
		"models": []any{
			map[string]any{"name": "model-a", "cheapest_groups": []any{"default"}, "in_price": 1.0, "out_price": 2.0, "price_type": 0},
			map[string]any{"name": "fixed-model", "cheapest_groups": []any{"default"}, "price": 0.03, "price_type": 1},
		},
	}
	if err := ValidateCheckinWorkflowOutput(valid); err != nil {
		t.Fatalf("validate output: %v", err)
	}
	legacyFixedPrice := cloneWorkflowOutputFixture(valid)
	legacyFixedPrice["models"].([]any)[1] = map[string]any{"name": "fixed-model", "cheapest_groups": []any{"default"}, "in_price": 0.03, "out_price": 0.03, "price_type": 1}
	if err := ValidateCheckinWorkflowOutput(legacyFixedPrice); err != nil {
		t.Fatalf("validate legacy fixed-price output: %v", err)
	}
	allPriceFields := cloneWorkflowOutputFixture(valid)
	allPriceFields["models"].([]any)[1].(map[string]any)["in_price"] = 0.03
	allPriceFields["models"].([]any)[1].(map[string]any)["out_price"] = 0.03
	if err := ValidateCheckinWorkflowOutput(allPriceFields); err != nil {
		t.Fatalf("validate model with all price fields: %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(map[string]any)
		message string
	}{
		{
			name: "unknown top-level field",
			mutate: func(output map[string]any) {
				output["extra"] = true
			},
			message: "is not allowed",
		},
		{
			name: "invalid quota unit type",
			mutate: func(output map[string]any) {
				output["quota_unit"] = 1
			},
			message: "expected string",
		},
		{
			name: "negative usage",
			mutate: func(output map[string]any) {
				output["used_quota"] = -1
			},
			message: "greater than or equal to zero",
		},
		{
			name: "legacy balance field",
			mutate: func(output map[string]any) {
				output["balance"] = 12.5
			},
			message: "$.balance is not allowed",
		},
		{
			name: "invalid price type",
			mutate: func(output map[string]any) {
				models := output["models"].([]any)
				models[0].(map[string]any)["price_type"] = 2
			},
			message: "must be between 0 and 1",
		},
		{
			name: "fixed price missing",
			mutate: func(output map[string]any) {
				models := output["models"].([]any)
				delete(models[1].(map[string]any), "price")
			},
			message: "at least one of in_price, out_price, or price is required",
		},
		{
			name: "duplicate group",
			mutate: func(output map[string]any) {
				models := output["models"].([]any)
				models[0].(map[string]any)["cheapest_groups"] = []any{"default", "default"}
			},
			message: "duplicate group",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := cloneWorkflowOutputFixture(valid)
			test.mutate(output)
			err := ValidateCheckinWorkflowOutput(output)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("expected error containing %q, got %v", test.message, err)
			}
		})
	}
}

func cloneWorkflowOutputFixture(source map[string]any) map[string]any {
	return map[string]any{
		"user_id":      source["user_id"],
		"username":     source["username"],
		"quota":        source["quota"],
		"quota_unit":   source["quota_unit"],
		"used_quota":   source["used_quota"],
		"today_reward": source["today_reward"],
		"api_keys": []any{
			map[string]any{"id": "key-1", "name": "main", "key": "sk-value", "group": "default", "used_quota": source["api_keys"].([]any)[0].(map[string]any)["used_quota"]},
		},
		"models": []any{
			map[string]any{"name": "model-a", "cheapest_groups": []any{"default"}, "in_price": 1.0, "out_price": 2.0, "price_type": 0},
			map[string]any{"name": "fixed-model", "cheapest_groups": []any{"default"}, "price": 0.03, "price_type": 1},
		},
	}
}
