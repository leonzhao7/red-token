package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddedFrontendIsServedAtRoot(t *testing.T) {
	application := newTestApp(t)
	rec := httptest.NewRecorder()
	application.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("GET / content type = %q, want HTML", rec.Header().Get("Content-Type"))
	}
}

func TestEmbeddedFrontendFallbackAndMissingAssets(t *testing.T) {
	application := newTestApp(t)
	for _, test := range []struct {
		path string
		want int
	}{
		{path: "/admin/", want: http.StatusOK},
		{path: "/assets/not-found.js", want: http.StatusNotFound},
	} {
		rec := httptest.NewRecorder()
		application.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, test.path, nil))
		if rec.Code != test.want {
			t.Errorf("GET %s = %d, want %d", test.path, rec.Code, test.want)
		}
	}
}
