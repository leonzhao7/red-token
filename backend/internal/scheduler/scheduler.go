package scheduler

import (
	"context"
	"errors"
	"path"
	"sort"
	"strings"
	"time"

	"red-token/internal/domain"
	"red-token/internal/store"
)

var ErrNoBackendAvailable = errors.New("no backend available")

type Selection struct {
	Candidates []domain.Backend `json:"candidates"`
}

type Service struct {
	store           *store.Store
	backendCooldown time.Duration
	backendFails    int
}

func New(store *store.Store, backendCooldown time.Duration, backendFails int) *Service {
	if backendFails < 1 {
		backendFails = 1
	}
	return &Service{
		store:           store,
		backendCooldown: backendCooldown,
		backendFails:    backendFails,
	}
}

func (s *Service) SelectBackend(ctx context.Context, endpoint, model string) (Selection, error) {
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
	return Selection{Candidates: candidates}, nil
}

func (s *Service) MarkSuccess(ctx context.Context, backendID int64) error {
	_, err := s.store.MarkBackendSuccess(ctx, backendID)
	return err
}

func (s *Service) MarkFailure(ctx context.Context, backendID int64, _ error) error {
	_, err := s.store.MarkBackendFailure(ctx, backendID, s.backendFails, s.backendCooldown, time.Now().UTC())
	return err
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
