package scheduler

import (
	"context"
	"errors"
	"path"
	"sort"
	"strings"
	"time"

	"red-token/internal/config"
	"red-token/internal/domain"
	"red-token/internal/store"
)

var ErrNoBackendAvailable = errors.New("no backend available")

type Selection struct {
	Candidates []domain.Backend `json:"candidates"`
}

type Service struct {
	store *store.Store
	// cfg is the live configuration, so settings changed from the admin UI
	// apply to the next selection instead of waiting for a restart.
	cfg      *config.Config
	affinity *userAffinity
}

func New(store *store.Store, cfg *config.Config) *Service {
	return &Service{
		store:    store,
		cfg:      cfg,
		affinity: newUserAffinity(),
	}
}

// SelectBackend returns the failover-ordered candidates for a request. user is
// the caller identity from UserKey; when user isolation is enabled, candidates
// held by other users are pushed to the back of the chain.
func (s *Service) SelectBackend(ctx context.Context, endpoint, model, user string) (Selection, error) {
	_ = endpoint
	if err := s.store.RecoverExpiredBackends(ctx, time.Now().UTC()); err != nil {
		return Selection{}, err
	}

	backends, err := s.store.ListBackends(ctx)
	if err != nil {
		return Selection{}, err
	}

	candidates := make([]domain.Backend, 0, len(backends))
	for _, backend := range backends {
		if backend.Status != domain.BackendStatusNormal {
			continue
		}
		for _, apiKey := range backend.APIKeys {
			if !supportsBackendAPIKeyModel(apiKey, model) {
				continue
			}
			candidate := backend
			candidate.APIKey = apiKey.APIKey
			candidate.Models = apiKey.Models
			candidate.ModelMapping = apiKey.ModelMapping
			candidates = append(candidates, candidate)
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Weight == candidates[j].Weight {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].Weight > candidates[j].Weight
	})

	if len(candidates) == 0 {
		return Selection{}, ErrNoBackendAvailable
	}
	if s.cfg.SchedulerUserIsolation {
		candidates = s.affinity.prioritize(strings.TrimSpace(model), user, candidates, s.affinityTTL(), time.Now())
	}
	return Selection{Candidates: candidates}, nil
}

// NoteBackendUsed records the backend that actually served the request so the
// user stays on it next time instead of the candidate that failed.
func (s *Service) NoteBackendUsed(model, user string, backendID int64) {
	if !s.cfg.SchedulerUserIsolation || backendID <= 0 {
		return
	}
	s.affinity.note(strings.TrimSpace(model), user, backendID, s.affinityTTL(), time.Now())
}

func (s *Service) affinityTTL() time.Duration {
	if s.cfg.SchedulerUserAffinityTTL > 0 {
		return s.cfg.SchedulerUserAffinityTTL
	}
	return config.DefaultSchedulerUserAffinityTTL
}

func (s *Service) MarkSuccess(ctx context.Context, backendID int64) error {
	_, err := s.store.MarkBackendSuccess(ctx, backendID)
	return err
}

func (s *Service) MarkFailure(ctx context.Context, backendID int64, _ error) error {
	_, err := s.store.MarkBackendFailure(ctx, backendID, s.backendFails(), s.cfg.BackendCooldown, time.Now().UTC())
	return err
}

func (s *Service) backendFails() int {
	if s.cfg.BackendFails < 1 {
		return 1
	}
	return s.cfg.BackendFails
}

func supportsModel(patterns []string, model string) bool {
	for _, candidate := range patterns {
		if matchPattern(candidate, model) {
			return true
		}
	}
	return false
}

func supportsBackendAPIKeyModel(apiKey domain.BackendAPIKey, model string) bool {
	if supportsModel(apiKey.Models, model) {
		return true
	}

	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	for clientModel, upstreamModel := range apiKey.ModelMapping {
		if strings.TrimSpace(clientModel) != model {
			continue
		}
		if strings.TrimSpace(upstreamModel) == "" {
			continue
		}
		return true
	}
	return false
}

func matchPattern(pattern, value string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || pattern == "*" {
		return true
	}
	if !strings.ContainsAny(pattern, "*?") {
		return pattern == value
	}
	ok, err := path.Match(pattern, value)
	return err == nil && ok
}
