package service

import (
	"net/http"
	"testing"
	"time"
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
