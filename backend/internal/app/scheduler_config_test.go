package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func getConfigPayload(t *testing.T, application *App, cookie *http.Cookie) map[string]string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin/api/config", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	application.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET config = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	return payload
}

func putConfig(t *testing.T, application *App, cookie *http.Cookie, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/admin/api/config", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	application.Handler().ServeHTTP(rec, req)
	return rec
}

func TestSchedulerSettingsAreExposedAndPersisted(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "admin", "admin")

	initial := getConfigPayload(t, application, cookie)
	if _, ok := initial["scheduler_user_isolation"]; !ok {
		t.Fatal("config response is missing scheduler_user_isolation")
	}
	if _, ok := initial["scheduler_user_affinity_ttl"]; !ok {
		t.Fatal("config response is missing scheduler_user_affinity_ttl")
	}

	rec := putConfig(t, application, cookie, `{"scheduler_user_isolation":"false","scheduler_user_affinity_ttl":"45m"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT config = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// The in-memory config the scheduler reads must change immediately.
	if application.cfg.SchedulerUserIsolation {
		t.Error("scheduler_user_isolation did not apply to the live config")
	}
	if application.cfg.SchedulerUserAffinityTTL != 45*time.Minute {
		t.Errorf("affinity ttl = %s, want 45m", application.cfg.SchedulerUserAffinityTTL)
	}

	updated := getConfigPayload(t, application, cookie)
	if updated["scheduler_user_isolation"] != "false" {
		t.Errorf("persisted isolation = %q, want false", updated["scheduler_user_isolation"])
	}
	if updated["scheduler_user_affinity_ttl"] != "45m" {
		t.Errorf("persisted ttl = %q, want 45m", updated["scheduler_user_affinity_ttl"])
	}

	// Reload must read the same values back out of the database.
	application.cfg.SchedulerUserIsolation = true
	reloadReq := httptest.NewRequest(http.MethodPost, "/admin/api/config/reload", nil)
	reloadReq.AddCookie(cookie)
	reloadRec := httptest.NewRecorder()
	application.Handler().ServeHTTP(reloadRec, reloadReq)
	if reloadRec.Code != http.StatusOK {
		t.Fatalf("reload = %d, want 200", reloadRec.Code)
	}
	if application.cfg.SchedulerUserIsolation {
		t.Error("reload did not restore scheduler_user_isolation from the database")
	}
}

func TestSchedulerSettingsRejectBadValues(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "admin", "admin")

	cases := map[string]string{
		"non-boolean isolation": `{"scheduler_user_isolation":"maybe"}`,
		"unparseable ttl":       `{"scheduler_user_affinity_ttl":"soon"}`,
		"zero ttl":              `{"scheduler_user_affinity_ttl":"0s"}`,
		"negative ttl":          `{"scheduler_user_affinity_ttl":"-5m"}`,
	}
	for name, body := range cases {
		if rec := putConfig(t, application, cookie, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: PUT config = %d, want 400 (body %s)", name, rec.Code, rec.Body.String())
		}
	}

	// A rejected write must not have changed the running config.
	if !application.cfg.SchedulerUserIsolation {
		t.Error("invalid payload altered scheduler_user_isolation")
	}
}
