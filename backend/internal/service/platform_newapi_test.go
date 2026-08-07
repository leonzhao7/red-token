package service

import "testing"

func TestNewAPITokenMetadataUsesTokenName(t *testing.T) {
	payload := map[string]any{
		"data": map[string]any{
			"items": []any{
				map[string]any{
					"id":         float64(1251),
					"key":        "bsvt**********3xDi",
					"name":       "wahaha",
					"group":      "",
					"used_quota": float64(40121914),
				},
			},
		},
	}

	items, err := newAPITokenMetadataItems(payload)
	if err != nil {
		t.Fatalf("parse token metadata: %v", err)
	}
	if len(items) != 1 || items[0].Name != "wahaha" || items[0].Group != "default" {
		t.Fatalf("expected token name %q and default group, got %#v", "wahaha", items)
	}
}

func TestNewAPITokenMetadataKeepsMissingNameEmpty(t *testing.T) {
	payload := map[string]any{
		"data": map[string]any{
			"items": []any{
				map[string]any{
					"id":         float64(1251),
					"key":        "sk-token",
					"group":      "vip",
					"used_quota": float64(0),
				},
			},
		},
	}

	items, err := newAPITokenMetadataItems(payload)
	if err != nil {
		t.Fatalf("parse token metadata: %v", err)
	}
	if len(items) != 1 || items[0].Name != "" || items[0].Group != "vip" {
		t.Fatalf("expected empty token name and group %q, got %#v", "vip", items)
	}
}
