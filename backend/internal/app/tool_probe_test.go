package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"red-token/internal/domain"
)

func TestBackendToolProbeRouteRequiresAdminSession(t *testing.T) {
	application := newTestApp(t)
	backend, err := application.store.CreateBackend(context.Background(), domain.Backend{Name: "tool-probe-route", BaseURL: "https://example.invalid", APIKey: "sk-route", Models: []string{"model"}})
	if err != nil {
		t.Fatal(err)
	}
	path := "/admin/api/backends/" + strconv.FormatInt(backend.ID, 10) + "/tool-probe"
	w := httptest.NewRecorder()
	application.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d, want 401", w.Code)
	}
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	r.AddCookie(login(t, application, "admin", "admin"))
	w = httptest.NewRecorder()
	application.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "model is required") {
		t.Fatalf("authenticated route status=%d body=%s", w.Code, w.Body.String())
	}
}
