package service

import (
	"strings"
	"testing"
)

func TestValidateCheckinWorkflowOutput(t *testing.T) {
	valid := map[string]any{
		"user_id":      "user-1",
		"username":     "alice",
		"balance":      12.5,
		"used_balance": int64(3),
		"api_keys": []any{
			map[string]any{"id": "key-1", "name": "main", "key": "sk-value", "group": "default", "total_cost": 3.0},
		},
		"models": []any{
			map[string]any{"name": "model-a", "cheapest_groups": []any{"default"}, "in_price": 1.0, "out_price": 2.0},
		},
	}
	if err := ValidateCheckinWorkflowOutput(valid); err != nil {
		t.Fatalf("validate output: %v", err)
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
			name: "negative usage",
			mutate: func(output map[string]any) {
				output["used_balance"] = -1
			},
			message: "greater than or equal to zero",
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
		"balance":      source["balance"],
		"used_balance": source["used_balance"],
		"api_keys": []any{
			map[string]any{"id": "key-1", "name": "main", "key": "sk-value", "group": "default", "total_cost": 3.0},
		},
		"models": []any{
			map[string]any{"name": "model-a", "cheapest_groups": []any{"default"}, "in_price": 1.0, "out_price": 2.0},
		},
	}
}
