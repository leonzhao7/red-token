package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"red-token/internal/handler"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	application, err := New(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = application.Close() })
	return application
}

func login(t *testing.T, application *App, username, password string) *http.Cookie {
	t.Helper()
	body := strings.NewReader(`{"username":"` + username + `","password":"` + password + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/auth/login", body)
	rec := httptest.NewRecorder()
	application.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == handler.SessionCookieName {
			return cookie
		}
	}
	t.Fatal("login did not set a session cookie")
	return nil
}

func TestAdminAPIRequiresSession(t *testing.T) {
	application := newTestApp(t)

	paths := []string{
		"/admin/api/overview",
		"/admin/api/backends",
		"/admin/api/client-keys",
		"/admin/api/config",
		"/admin/api/users",
		"/admin/api/auth/me",
	}
	for _, path := range paths {
		rec := httptest.NewRecorder()
		application.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without session = %d, want 401", path, rec.Code)
		}
	}
}

func TestDefaultAdminLoginGrantsAccess(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "admin", "admin")

	req := httptest.NewRequest(http.MethodGet, "/admin/api/auth/me", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	application.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/api/auth/me = %d, want 200", rec.Code)
	}

	var payload struct {
		User struct {
			Username          string `json:"username"`
			IsDefaultPassword bool   `json:"is_default_password"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.User.Username != "admin" {
		t.Errorf("username = %q, want admin", payload.User.Username)
	}
	if !payload.User.IsDefaultPassword {
		t.Error("is_default_password = false, want true for seeded admin")
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	application := newTestApp(t)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/auth/login", strings.NewReader(`{"username":"admin","password":"nope"}`))
	rec := httptest.NewRecorder()
	application.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("login with wrong password = %d, want 401", rec.Code)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Error("failed login should not set cookies")
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "admin", "admin")

	logoutReq := httptest.NewRequest(http.MethodPost, "/admin/api/auth/logout", nil)
	logoutReq.AddCookie(cookie)
	logoutRec := httptest.NewRecorder()
	application.Handler().ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusOK {
		t.Fatalf("logout = %d, want 200", logoutRec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/api/users", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	application.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("reusing logged-out cookie = %d, want 401", rec.Code)
	}
}

func TestProxyEndpointsStillUseClientKeyAuth(t *testing.T) {
	application := newTestApp(t)

	rec := httptest.NewRecorder()
	application.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /v1/models without key = %d, want 401", rec.Code)
	}

	// A session cookie must not substitute for a client API key.
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.AddCookie(login(t, application, "admin", "admin"))
	sessionRec := httptest.NewRecorder()
	application.Handler().ServeHTTP(sessionRec, req)
	if sessionRec.Code != http.StatusUnauthorized {
		t.Errorf("GET /v1/models with session cookie = %d, want 401", sessionRec.Code)
	}

	healthRec := httptest.NewRecorder()
	application.Handler().ServeHTTP(healthRec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if healthRec.Code != http.StatusOK {
		t.Errorf("GET /healthz = %d, want 200", healthRec.Code)
	}
}
