package handler

import "testing"

func TestValidateBackendAPIKeysAllowsEmptyList(t *testing.T) {
	apiKeys, err := validateBackendAPIKeys(nil, "", nil, nil)
	if err != nil {
		t.Fatalf("validate empty api key list: %v", err)
	}
	if len(apiKeys) != 0 {
		t.Fatalf("expected empty api key list, got %d items", len(apiKeys))
	}
}
