package service

import (
	"encoding/json"
	"fmt"
	"math"
)

var checkinWorkflowOutputFields = map[string]struct{}{
	"user_id":      {},
	"username":     {},
	"balance":      {},
	"used_balance": {},
	"api_keys":     {},
	"models":       {},
}

var checkinWorkflowAPIKeyFields = map[string]struct{}{
	"id":         {},
	"name":       {},
	"key":        {},
	"group":      {},
	"total_cost": {},
}

var checkinWorkflowModelFields = map[string]struct{}{
	"name":            {},
	"cheapest_groups": {},
	"in_price":        {},
	"out_price":       {},
}

// ValidateCheckinWorkflowOutput enforces the fixed business snapshot defined
// by docs/http_workflow.md. The workflow engine remains schema-agnostic; API
// execution opts into this validator before persisting a result.
func ValidateCheckinWorkflowOutput(value any) error {
	output, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("$: expected object, got %T", value)
	}
	if err := validateWorkflowObjectFields("$", output, checkinWorkflowOutputFields); err != nil {
		return err
	}
	for _, field := range []string{"user_id", "username"} {
		if _, err := requireWorkflowString(output, field, "$."+field); err != nil {
			return err
		}
	}
	if _, err := requireWorkflowNumber(output, "balance", "$.balance", false); err != nil {
		return err
	}
	if _, err := requireWorkflowNumber(output, "used_balance", "$.used_balance", true); err != nil {
		return err
	}

	apiKeysValue, exists := output["api_keys"]
	if !exists {
		return fmt.Errorf("$.api_keys is required")
	}
	apiKeys, ok := apiKeysValue.([]any)
	if !ok {
		return fmt.Errorf("$.api_keys: expected array, got %T", apiKeysValue)
	}
	keyIDs := make(map[string]struct{}, len(apiKeys))
	for index, value := range apiKeys {
		path := fmt.Sprintf("$.api_keys[%d]", index)
		item, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: expected object, got %T", path, value)
		}
		if err := validateWorkflowObjectFields(path, item, checkinWorkflowAPIKeyFields); err != nil {
			return err
		}
		id, err := requireWorkflowString(item, "id", path+".id")
		if err != nil {
			return err
		}
		for _, field := range []string{"name", "key", "group"} {
			if _, err := requireWorkflowString(item, field, path+"."+field); err != nil {
				return err
			}
		}
		if _, err := requireWorkflowNumber(item, "total_cost", path+".total_cost", true); err != nil {
			return err
		}
		if id != "" {
			if _, duplicate := keyIDs[id]; duplicate {
				return fmt.Errorf("%s.id: duplicate non-empty API key id %q", path, id)
			}
			keyIDs[id] = struct{}{}
		}
	}

	modelsValue, exists := output["models"]
	if !exists {
		return fmt.Errorf("$.models is required")
	}
	models, ok := modelsValue.([]any)
	if !ok {
		return fmt.Errorf("$.models: expected array, got %T", modelsValue)
	}
	modelNames := make(map[string]struct{}, len(models))
	for index, value := range models {
		path := fmt.Sprintf("$.models[%d]", index)
		item, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: expected object, got %T", path, value)
		}
		if err := validateWorkflowObjectFields(path, item, checkinWorkflowModelFields); err != nil {
			return err
		}
		name, err := requireWorkflowString(item, "name", path+".name")
		if err != nil {
			return err
		}
		if _, duplicate := modelNames[name]; duplicate {
			return fmt.Errorf("%s.name: duplicate model name %q", path, name)
		}
		modelNames[name] = struct{}{}

		groupsValue, exists := item["cheapest_groups"]
		if !exists {
			return fmt.Errorf("%s.cheapest_groups is required", path)
		}
		groups, ok := groupsValue.([]any)
		if !ok {
			return fmt.Errorf("%s.cheapest_groups: expected array, got %T", path, groupsValue)
		}
		groupNames := make(map[string]struct{}, len(groups))
		for groupIndex, groupValue := range groups {
			group, ok := groupValue.(string)
			if !ok {
				return fmt.Errorf("%s.cheapest_groups[%d]: expected string, got %T", path, groupIndex, groupValue)
			}
			if _, duplicate := groupNames[group]; duplicate {
				return fmt.Errorf("%s.cheapest_groups[%d]: duplicate group %q", path, groupIndex, group)
			}
			groupNames[group] = struct{}{}
		}
		for _, field := range []string{"in_price", "out_price"} {
			if _, err := requireWorkflowNumber(item, field, path+"."+field, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateWorkflowObjectFields(path string, object map[string]any, allowed map[string]struct{}) error {
	for field := range object {
		if _, ok := allowed[field]; !ok {
			return fmt.Errorf("%s.%s is not allowed", path, field)
		}
	}
	for field := range allowed {
		if _, exists := object[field]; !exists {
			return fmt.Errorf("%s.%s is required", path, field)
		}
	}
	return nil
}

func requireWorkflowString(object map[string]any, field, path string) (string, error) {
	value, exists := object[field]
	if !exists {
		return "", fmt.Errorf("%s is required", path)
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s: expected string, got %T", path, value)
	}
	return text, nil
}

func requireWorkflowNumber(object map[string]any, field, path string, nonNegative bool) (float64, error) {
	value, exists := object[field]
	if !exists {
		return 0, fmt.Errorf("%s is required", path)
	}
	number, ok := workflowNumber(value)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("%s: expected finite number, got %T", path, value)
	}
	if nonNegative && number < 0 {
		return 0, fmt.Errorf("%s must be greater than or equal to zero", path)
	}
	return number, nil
}

func workflowNumber(value any) (float64, bool) {
	switch value := value.(type) {
	case int:
		return float64(value), true
	case int8:
		return float64(value), true
	case int16:
		return float64(value), true
	case int32:
		return float64(value), true
	case int64:
		return float64(value), true
	case uint:
		return float64(value), true
	case uint8:
		return float64(value), true
	case uint16:
		return float64(value), true
	case uint32:
		return float64(value), true
	case uint64:
		return float64(value), true
	case float32:
		return float64(value), true
	case float64:
		return value, true
	case json.Number:
		number, err := value.Float64()
		return number, err == nil
	default:
		return 0, false
	}
}
