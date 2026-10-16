package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"red-token/internal/domain"
	"red-token/internal/store"
)

// upstreamRecorder is a fake relay that reports which backend served a request.
type upstreamRecorder struct {
	mu     sync.Mutex
	server *httptest.Server
	hits   int
}

func newUpstreamRecorder(t *testing.T) *upstreamRecorder {
	t.Helper()
	recorder := &upstreamRecorder{}
	recorder.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		recorder.mu.Lock()
		recorder.hits++
		recorder.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		// Non-zero token usage, otherwise the proxy treats the reply as a failure.
		_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`))
	}))
	t.Cleanup(recorder.server.Close)
	return recorder
}

func (u *upstreamRecorder) hitCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.hits
}

// isolationFixture is an app wired to several fake backends serving one model.
type isolationFixture struct {
	app       *App
	token     string
	backends  map[int64]*upstreamRecorder
	nameByURL map[string]int64
}

func newIsolationFixture(t *testing.T, backendCount int) *isolationFixture {
	t.Helper()
	application := newTestApp(t)
	ctx := context.Background()

	fixture := &isolationFixture{
		app:       application,
		token:     "sk-isolation-test",
		backends:  make(map[int64]*upstreamRecorder, backendCount),
		nameByURL: make(map[string]int64, backendCount),
	}

	for i := 0; i < backendCount; i++ {
		upstream := newUpstreamRecorder(t)
		created, err := application.store.CreateBackend(ctx, domain.Backend{
			Name:     "relay-" + string(rune('a'+i)),
			Protocol: domain.BackendProtocolOpenAI,
			BaseURL:  upstream.server.URL,
			Status:   domain.BackendStatusNormal,
			APIKeys: []domain.BackendAPIKey{{
				APIKey: "upstream-key",
				Models: []string{"gpt-test"},
			}},
		})
		if err != nil {
			t.Fatalf("create backend: %v", err)
		}
		fixture.backends[created.ID] = upstream
		fixture.nameByURL[upstream.server.URL] = created.ID
	}

	if _, err := application.store.CreateClientKey(ctx, domain.ClientKey{
		Name:          "isolation-test",
		TokenHash:     store.HashToken(fixture.token),
		Token:         fixture.token,
		TokenPrefix:   fixture.token[:8],
		AllowedModels: "*",
		Enabled:       true,
	}); err != nil {
		t.Fatalf("create client key: %v", err)
	}

	return fixture
}

// request sends one chat completion as the given user and returns the backend
// that served it.
func (f *isolationFixture) request(t *testing.T, clientIP, userAgent string) int64 {
	t.Helper()
	before := make(map[int64]int, len(f.backends))
	for id, upstream := range f.backends {
		before[id] = upstream.hitCount()
	}

	body := strings.NewReader(`{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	req.Header.Set("Authorization", "Bearer "+f.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", clientIP)
	req.Header.Set("User-Agent", userAgent)
	rec := httptest.NewRecorder()
	f.app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proxy request from %s = %d, want 200 (body %s)", clientIP, rec.Code, rec.Body.String())
	}

	served := int64(0)
	for id, upstream := range f.backends {
		if upstream.hitCount() > before[id] {
			if served != 0 {
				t.Fatalf("request hit more than one backend (%d and %d)", served, id)
			}
			served = id
		}
	}
	if served == 0 {
		t.Fatal("no backend recorded the request")
	}
	return served
}

func TestDifferentUsersGetDifferentBackendsForSameModel(t *testing.T) {
	fixture := newIsolationFixture(t, 3)

	// Three distinct callers, same model: each must land on its own relay.
	byUser := map[string]int64{
		"1.1.1.1/curl":   fixture.request(t, "1.1.1.1", "curl/8.0"),
		"2.2.2.2/curl":   fixture.request(t, "2.2.2.2", "curl/8.0"),
		"1.1.1.1/python": fixture.request(t, "1.1.1.1", "python-requests/2.31"),
	}

	used := make(map[int64]string, len(byUser))
	for user, backendID := range byUser {
		if other, taken := used[backendID]; taken {
			t.Fatalf("backend %d served both %q and %q", backendID, other, user)
		}
		used[backendID] = user
	}
}

func TestSameUserKeepsItsBackend(t *testing.T) {
	fixture := newIsolationFixture(t, 3)

	first := fixture.request(t, "1.1.1.1", "curl/8.0")
	// A different caller in between must not displace the first one.
	_ = fixture.request(t, "2.2.2.2", "curl/8.0")

	for i := 0; i < 3; i++ {
		if got := fixture.request(t, "1.1.1.1", "curl/8.0"); got != first {
			t.Fatalf("repeat request %d went to backend %d, want %d", i, got, first)
		}
	}
}

func TestUsersShareABackendOnlyWhenNoneAreFree(t *testing.T) {
	// Two relays, three users: the third has to share rather than fail.
	fixture := newIsolationFixture(t, 2)

	first := fixture.request(t, "1.1.1.1", "curl/8.0")
	second := fixture.request(t, "2.2.2.2", "curl/8.0")
	if first == second {
		t.Fatalf("two users shared backend %d while another was free", first)
	}

	third := fixture.request(t, "3.3.3.3", "curl/8.0")
	if third != first && third != second {
		t.Fatalf("third user hit unknown backend %d", third)
	}
}

func TestUserIsolationDisabledFallsBackToWeightOrder(t *testing.T) {
	fixture := newIsolationFixture(t, 3)
	fixture.app.cfg.SchedulerUserIsolation = false

	// With isolation off, every user follows the same weight ordering.
	first := fixture.request(t, "1.1.1.1", "curl/8.0")
	for _, ip := range []string{"2.2.2.2", "3.3.3.3", "4.4.4.4"} {
		if got := fixture.request(t, ip, "curl/8.0"); got != first {
			t.Fatalf("user %s went to backend %d, want %d with isolation disabled", ip, got, first)
		}
	}
}
