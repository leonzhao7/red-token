package scheduler

import (
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"red-token/internal/domain"
)

// Candidate tiers used when spreading users across backends. Lower is better.
const (
	tierOwned  = 0 // already claimed by this user
	tierFree   = 1 // nobody holds it right now
	tierShared = 2 // held by another user; only used when nothing else is left
)

// UserKey identifies a caller by client IP and User-Agent. Two requests that
// agree on both are treated as the same user; everyone else is routed to a
// different backend while spare backends exist.
func UserKey(clientIP, userAgent string) string {
	host := strings.TrimSpace(clientIP)
	// clientIP may still carry the source port when it came from RemoteAddr,
	// and a per-connection port would make every request its own user.
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	return host + "|" + strings.TrimSpace(userAgent)
}

type backendClaim struct {
	user      string
	expiresAt time.Time
}

// userAffinity records which user currently holds which backend per model so
// that different users asking for the same model land on different backends.
type userAffinity struct {
	mu sync.Mutex
	// model -> backend id -> claim. A user holds at most one backend per model.
	claims map[string]map[int64]backendClaim
}

func newUserAffinity() *userAffinity {
	return &userAffinity{claims: make(map[string]map[int64]backendClaim)}
}

// prioritize reorders candidates so the user's own backend comes first, then
// unclaimed backends, then backends another user holds. Nothing is dropped:
// failover still reaches the shared backends once the exclusive ones fail. The
// winning backend is claimed for the user before returning.
func (a *userAffinity) prioritize(model, user string, candidates []domain.Backend, ttl time.Duration, now time.Time) []domain.Backend {
	if len(candidates) == 0 {
		return candidates
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.evictLocked(now)
	claims := a.claims[model]

	// Tier by backend id, not by candidate: one backend contributes a candidate
	// per matching API key, and all of them must share the same tier.
	type tier struct {
		rank    int
		expires time.Time
	}
	tiers := make(map[int64]tier, len(candidates))
	for _, candidate := range candidates {
		if _, ok := tiers[candidate.ID]; ok {
			continue
		}
		claim, claimed := claims[candidate.ID]
		switch {
		case !claimed:
			tiers[candidate.ID] = tier{rank: tierFree}
		case claim.user == user:
			tiers[candidate.ID] = tier{rank: tierOwned}
		default:
			tiers[candidate.ID] = tier{rank: tierShared, expires: claim.expiresAt}
		}
	}

	ordered := make([]domain.Backend, len(candidates))
	copy(ordered, candidates)
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := tiers[ordered[i].ID], tiers[ordered[j].ID]
		if left.rank != right.rank {
			return left.rank < right.rank
		}
		// Among shared backends prefer the one whose holder has been idle
		// longest, so forced sharing stays spread out instead of piling onto
		// the heaviest backend.
		if left.rank == tierShared && !left.expires.Equal(right.expires) {
			return left.expires.Before(right.expires)
		}
		return false
	})

	a.claimLocked(model, user, ordered[0].ID, now.Add(ttl))
	return ordered
}

// note moves the user's claim onto the backend that actually served the
// request, so a failover away from the first candidate does not leave the user
// pinned to a backend they no longer use. A backend another user still holds is
// left alone: the user only reached it as a last resort.
func (a *userAffinity) note(model, user string, backendID int64, ttl time.Duration, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if claim, ok := a.claims[model][backendID]; ok && claim.user != user && claim.expiresAt.After(now) {
		return
	}
	a.claimLocked(model, user, backendID, now.Add(ttl))
}

func (a *userAffinity) claimLocked(model, user string, backendID int64, expiresAt time.Time) {
	claims := a.claims[model]
	if claims == nil {
		claims = make(map[int64]backendClaim)
		a.claims[model] = claims
	}
	// Release the backend this user moved away from so it is free for others.
	for id, claim := range claims {
		if id != backendID && claim.user == user {
			delete(claims, id)
		}
	}
	claims[backendID] = backendClaim{user: user, expiresAt: expiresAt}
}

func (a *userAffinity) evictLocked(now time.Time) {
	for model, claims := range a.claims {
		for id, claim := range claims {
			if !claim.expiresAt.After(now) {
				delete(claims, id)
			}
		}
		if len(claims) == 0 {
			delete(a.claims, model)
		}
	}
}
