package app

import (
	"testing"

	"red-token/internal/domain"
)

func TestHealthCheckAPIKeyForModel(t *testing.T) {
	backend := domain.Backend{
		APIKeys: []domain.BackendAPIKey{
			{APIKey: "direct-key", Models: []string{"gpt-*"}},
			{APIKey: "mapped-key", ModelMapping: map[string]string{"claude-client": "claude-upstream"}},
		},
	}

	if got, ok := healthCheckAPIKeyForModel(backend, "claude-upstream"); !ok || got != "mapped-key" {
		t.Fatalf("mapped model key = %q, %v; want mapped-key, true", got, ok)
	}
	if got, ok := healthCheckAPIKeyForModel(backend, "gpt-4o"); !ok || got != "direct-key" {
		t.Fatalf("wildcard model key = %q, %v; want direct-key, true", got, ok)
	}
	if _, ok := healthCheckAPIKeyForModel(backend, "unknown"); ok {
		t.Fatal("unknown model unexpectedly matched an API key")
	}
}
