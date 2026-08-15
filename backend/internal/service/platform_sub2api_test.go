package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"red-token/internal/domain"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func newSub2APITestClient(checkinRequests *int) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body string
		switch request.URL.Path {
		case "/api/v1/checkin":
			(*checkinRequests)++
			body = `{"code":0}`
		case "/api/v1/auth/me":
			body = `{"code":0,"data":{"id":1,"username":"tester","email":"tester@example.com","balance":10}}`
		case "/api/v1/usage/stats":
			body = `{"code":0,"data":{"total_actual_cost":2.5}}`
		default:
			body = `{"code":404}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}
}

func TestSub2APISyncRecordsExplicitCheckinCompletion(t *testing.T) {
	now := time.Date(2026, time.August, 8, 1, 2, 3, 0, time.UTC)
	checkinRequests := 0
	client := newSub2APITestClient(&checkinRequests)

	backend := domain.Backend{
		ConsoleURL:           "https://console.example",
		ConsoleAuthorization: "Bearer token",
		ConsoleCheckinPath:   "/api/v1/checkin",
		ConsoleAccountJSON:   `{"last_checkin_at":"2026-08-08T00:10:00Z"}`,
	}
	platform := NewPlatformSub2API(PlatformSub2APIOptions{
		HTTPClient: client,
		Now:        func() time.Time { return now },
	})

	result, err := platform.Sync(context.Background(), backend, nil, Sub2APISyncOptions{
		RecordCompletionAsCheckin: true,
	})
	if err != nil {
		t.Fatalf("sync sub2api backend: %v", err)
	}
	if checkinRequests != 0 {
		t.Fatalf("expected no duplicate upstream checkin request, got %d", checkinRequests)
	}

	account := decodeSub2APIJSONMap(result.Backend.ConsoleAccountJSON)
	if got := account["last_checkin_at"]; got != now.Format(time.RFC3339) {
		t.Fatalf("expected last_checkin_at %q, got %v", now.Format(time.RFC3339), got)
	}
}

func TestSub2APISyncRunsBatchCheckinAfterPreviousLocalDay(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, time.August, 8, 0, 30, 0, 0, location)
	checkinRequests := 0
	platform := NewPlatformSub2API(PlatformSub2APIOptions{
		HTTPClient: newSub2APITestClient(&checkinRequests),
		Now:        func() time.Time { return now },
	})

	backend := domain.Backend{
		ConsoleURL:           "https://console.example",
		ConsoleAuthorization: "Bearer token",
		ConsoleCheckinPath:   "/api/v1/checkin",
		ConsoleAccountJSON:   `{"last_checkin_at":"2026-08-07T15:30:00Z"}`,
	}
	result, err := platform.Sync(context.Background(), backend, nil, Sub2APISyncOptions{})
	if err != nil {
		t.Fatalf("sync sub2api backend: %v", err)
	}
	if checkinRequests != 1 {
		t.Fatalf("expected one batch checkin request, got %d", checkinRequests)
	}

	account := decodeSub2APIJSONMap(result.Backend.ConsoleAccountJSON)
	if got := account["last_checkin_at"]; got != now.UTC().Format(time.RFC3339) {
		t.Fatalf("expected last_checkin_at %q, got %v", now.UTC().Format(time.RFC3339), got)
	}
}

func TestSub2APIConsoleAuthorizationUsesCommonHeaders(t *testing.T) {
	backend := domain.Backend{ConsoleHeaders: map[string]string{"Authorization": "Bearer header-token"}}
	if got := sub2APIConsoleAuthorization(backend); got != "Bearer header-token" {
		t.Fatalf("expected common Authorization header, got %q", got)
	}
	backend.ConsoleAuthorization = "Bearer legacy-token"
	if got := sub2APIConsoleAuthorization(backend); got != "Bearer header-token" {
		t.Fatalf("expected common Authorization header to take precedence, got %q", got)
	}
	backend.ConsoleHeaders = nil
	if got := sub2APIConsoleAuthorization(backend); got != "Bearer legacy-token" {
		t.Fatalf("expected legacy authorization fallback, got %q", got)
	}
}
