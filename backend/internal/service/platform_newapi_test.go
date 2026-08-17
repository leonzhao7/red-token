package service

import (
	"net/http"
	"testing"
	"time"
)

func TestNewAPITokenMetadataUsesTokenName(t *testing.T) {
	payload := map[string]any{
		"data": map[string]any{
			"items": []any{
				map[string]any{
					"id":         float64(1251),
					"key":        "bsvt**********3xDi",
					"name":       "wahaha",
					"group":      "",
					"used_quota": 40121914.25,
				},
			},
		},
	}

	items, err := newAPITokenMetadataItems(payload)
	if err != nil {
		t.Fatalf("parse token metadata: %v", err)
	}
	if len(items) != 1 || items[0].Name != "wahaha" || items[0].Group != "default" || items[0].UsedQuota != 40121914.25 {
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

func TestConsoleHeadersWithResponseCookiesAddsReplacesAndDeletes(t *testing.T) {
	now := time.Date(2026, time.August, 17, 0, 0, 0, 0, time.UTC)
	headers := ConsoleHeadersWithResponseCookies(map[string]string{
		"Cookie": "session=old; removed=value; retained=yes",
		"X-Test": "configured",
	}, []*http.Cookie{
		{Name: "session", Value: "new"},
		{Name: "added", Value: "value"},
		{Name: "removed", Value: "", MaxAge: -1},
	}, now)
	if got, want := headers["Cookie"], "session=new; retained=yes; added=value"; got != want {
		t.Fatalf("Cookie=%q want %q", got, want)
	}
	if headers["X-Test"] != "configured" {
		t.Fatalf("non-cookie header changed: %#v", headers)
	}

	headers = ConsoleHeadersWithResponseCookies(nil, []*http.Cookie{{Name: "session", Value: "new"}}, now)
	if headers["Cookie"] != "session=new" {
		t.Fatalf("missing Cookie header was not added: %#v", headers)
	}
}
