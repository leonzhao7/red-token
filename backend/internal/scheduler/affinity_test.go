package scheduler

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"red-token/internal/domain"
)

const testTTL = 10 * time.Minute

func backends(ids ...int64) []domain.Backend {
	out := make([]domain.Backend, 0, len(ids))
	for _, id := range ids {
		out = append(out, domain.Backend{ID: id})
	}
	return out
}

func firstID(t *testing.T, candidates []domain.Backend) int64 {
	t.Helper()
	if len(candidates) == 0 {
		t.Fatal("expected at least one candidate")
	}
	return candidates[0].ID
}

func ids(candidates []domain.Backend) []int64 {
	out := make([]int64, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.ID)
	}
	return out
}

func TestUserKeyStripsPortAndSeparatesUsers(t *testing.T) {
	// A per-connection source port must not turn one caller into many users.
	if a, b := UserKey("10.0.0.1:51000", "curl/8"), UserKey("10.0.0.1:52000", "curl/8"); a != b {
		t.Fatalf("expected same user key, got %q and %q", a, b)
	}
	if a, b := UserKey("10.0.0.1", "curl/8"), UserKey("10.0.0.2", "curl/8"); a == b {
		t.Fatalf("different IPs must differ, both %q", a)
	}
	if a, b := UserKey("10.0.0.1", "curl/8"), UserKey("10.0.0.1", "python/3"); a == b {
		t.Fatalf("different user agents must differ, both %q", a)
	}
}

func TestPrioritizeGivesDistinctBackendsPerUser(t *testing.T) {
	affinity := newUserAffinity()
	now := time.Now()
	all := backends(1, 2, 3)

	seen := make(map[int64]string)
	for _, user := range []string{"a", "b", "c"} {
		got := firstID(t, affinity.prioritize("gpt-5", user, all, testTTL, now))
		if other, taken := seen[got]; taken {
			t.Fatalf("backend %d handed to both %q and %q", got, other, user)
		}
		seen[got] = user
	}
}

func TestPrioritizeIsStableForSameUser(t *testing.T) {
	affinity := newUserAffinity()
	now := time.Now()
	all := backends(1, 2, 3)

	first := firstID(t, affinity.prioritize("gpt-5", "a", all, testTTL, now))
	_ = affinity.prioritize("gpt-5", "b", all, testTTL, now)
	again := firstID(t, affinity.prioritize("gpt-5", "a", all, testTTL, now.Add(time.Minute)))
	if first != again {
		t.Fatalf("user a moved from backend %d to %d", first, again)
	}
}

func TestPrioritizeSharesOnlyWhenExhausted(t *testing.T) {
	affinity := newUserAffinity()
	now := time.Now()
	all := backends(1, 2)

	// Claim at different times so the idle ordering is actually exercised
	// rather than falling out of the input order.
	older := firstID(t, affinity.prioritize("gpt-5", "a", all, testTTL, now))
	_ = affinity.prioritize("gpt-5", "b", all, testTTL, now.Add(time.Minute))

	// A third user has no free backend left, so sharing is allowed, but every
	// backend must still be reachable for failover.
	ordered := affinity.prioritize("gpt-5", "c", all, testTTL, now.Add(2*time.Minute))
	if len(ordered) != len(all) {
		t.Fatalf("expected %d candidates for failover, got %v", len(all), ids(ordered))
	}
	// The holder that has been idle longest is shared first.
	if got := firstID(t, ordered); got != older {
		t.Fatalf("expected the longest-idle backend %d, got %d", older, got)
	}
}

func TestPrioritizeReusesExpiredClaims(t *testing.T) {
	affinity := newUserAffinity()
	now := time.Now()
	all := backends(1, 2)

	held := firstID(t, affinity.prioritize("gpt-5", "a", all, testTTL, now))
	// Once a's claim lapses, b should be able to take that backend back.
	ordered := affinity.prioritize("gpt-5", "b", all, testTTL, now.Add(testTTL+time.Second))
	if got := firstID(t, ordered); got != held {
		t.Fatalf("expected freed backend %d to be reusable, got %d", held, got)
	}
}

func TestPrioritizeIsolatesPerModel(t *testing.T) {
	affinity := newUserAffinity()
	now := time.Now()
	all := backends(1, 2)

	// Claims on one model must not push a different model's traffic around.
	a := firstID(t, affinity.prioritize("gpt-5", "a", all, testTTL, now))
	b := firstID(t, affinity.prioritize("claude", "b", all, testTTL, now))
	if a != b {
		t.Fatalf("per-model claims leaked: %d vs %d", a, b)
	}
}

func TestPrioritizeKeepsAllAPIKeysOfABackendTogether(t *testing.T) {
	affinity := newUserAffinity()
	now := time.Now()
	// One backend contributes a candidate per matching API key; a backend must
	// not be split across tiers.
	all := []domain.Backend{{ID: 1}, {ID: 2}, {ID: 1}, {ID: 2}}

	_ = affinity.prioritize("gpt-5", "a", all, testTTL, now)
	ordered := affinity.prioritize("gpt-5", "b", all, testTTL, now)
	if got := ids(ordered); got[0] != got[1] {
		t.Fatalf("expected both keys of one backend first, got %v", got)
	}
}

func TestNoteMovesClaimToServingBackend(t *testing.T) {
	affinity := newUserAffinity()
	now := time.Now()
	all := backends(1, 2, 3)

	picked := firstID(t, affinity.prioritize("gpt-5", "a", all, testTTL, now))
	// Pretend the first choice failed over to a different backend.
	var served int64
	for _, candidate := range all {
		if candidate.ID != picked {
			served = candidate.ID
			break
		}
	}
	affinity.note("gpt-5", "a", served, testTTL, now)

	if got := firstID(t, affinity.prioritize("gpt-5", "a", all, testTTL, now)); got != served {
		t.Fatalf("expected user pinned to served backend %d, got %d", served, got)
	}
	// The abandoned backend must be free for the next user.
	if got := firstID(t, affinity.prioritize("gpt-5", "b", all, testTTL, now)); got == served {
		t.Fatalf("backend %d should still belong to user a", served)
	}
}

func TestAffinityIsSafeUnderConcurrentUse(t *testing.T) {
	affinity := newUserAffinity()
	now := time.Now()
	all := backends(1, 2, 3, 4)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		user := strconv.Itoa(i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				ordered := affinity.prioritize("gpt-5", user, all, testTTL, now)
				affinity.note("gpt-5", user, ordered[0].ID, testTTL, now)
			}
		}()
	}
	wg.Wait()

	// Every backend must still be present exactly once per selection.
	ordered := affinity.prioritize("gpt-5", "final", all, testTTL, now)
	if len(ordered) != len(all) {
		t.Fatalf("candidate list corrupted: %v", ids(ordered))
	}
	seen := make(map[int64]bool, len(all))
	for _, candidate := range ordered {
		if seen[candidate.ID] {
			t.Fatalf("duplicate candidate in %v", ids(ordered))
		}
		seen[candidate.ID] = true
	}
}

func TestNoteDoesNotStealAnotherUsersBackend(t *testing.T) {
	affinity := newUserAffinity()
	now := time.Now()
	all := backends(1, 2)

	held := firstID(t, affinity.prioritize("gpt-5", "a", all, testTTL, now))
	// b only reached a's backend as a last resort, so the claim must stay with a.
	affinity.note("gpt-5", "b", held, testTTL, now)
	if got := firstID(t, affinity.prioritize("gpt-5", "a", all, testTTL, now.Add(time.Minute))); got != held {
		t.Fatalf("user a lost backend %d, now gets %d", held, got)
	}
}
