package service

import (
	"net/http"
	"testing"
	"time"

	"red-token/internal/domain"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestConsoleHeadersWithResponseCookiesAddsReplacesAndDeletes(t *testing.T) {
	now := time.Date(2026, time.August, 17, 0, 0, 0, 0, time.UTC)
	headers := ConsoleHeadersWithResponseCookies(map[string]string{
		"Cookie": "session=old; removed=value; retained=yes",
		"X-Test": "configured",
	}, []*http.Cookie{
		{Name: "session", Value: "new"},
		{Name: "added", Value: "value"},
		{Name: "removed", MaxAge: -1},
	}, now)
	if got, want := headers["Cookie"], "session=new; retained=yes; added=value"; got != want {
		t.Fatalf("Cookie=%q want %q", got, want)
	}
	if headers["X-Test"] != "configured" {
		t.Fatalf("non-cookie header changed: %#v", headers)
	}
}

func TestConsoleHeadersDeduplicatesStoredCookies(t *testing.T) {
	raw := `session=old; cf_clearance=stale; session=current; cf_clearance=fresh; quoted="a b"; Session=separate; token=a=b`
	want := `session=current; cf_clearance=fresh; quoted="a b"; Session=separate; token=a=b`
	for _, backend := range []domain.Backend{
		{ConsoleHeaders: map[string]string{"cookie": raw, "X-Console": "configured"}},
		{ConsoleCookie: raw},
	} {
		if got := ConsoleHeaders(backend)["Cookie"]; got != want {
			t.Fatalf("normalized Cookie=%q want %q", got, want)
		}
	}
}

func TestConsoleHeadersWithResponseCookiesRemovesExistingDuplicates(t *testing.T) {
	now := time.Now()
	headers := map[string]string{
		"Cookie": "session=old; cf_clearance=old; retained=old; session=current; cf_clearance=older; retained=current; removed=one; removed=two",
		"X-Test": "configured",
	}
	for range 3 {
		headers = ConsoleHeadersWithResponseCookies(headers, []*http.Cookie{
			{Name: "cf_clearance", Value: "fresh"},
			{Name: "removed", MaxAge: -1},
		}, now)
		if got, want := headers["Cookie"], "session=current; cf_clearance=fresh; retained=current"; got != want {
			t.Fatalf("Cookie=%q want %q", got, want)
		}
		if headers["X-Test"] != "configured" {
			t.Fatalf("non-cookie header changed: %#v", headers)
		}
	}
}
