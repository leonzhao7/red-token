package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"red-token/internal/domain"
	"red-token/internal/handler"
	"red-token/internal/proxy"
)

const healthRequestMaxAge = time.Hour

type savedHealthRequest struct {
	CreatedAt time.Time   `json:"created_at"`
	Method    string      `json:"method"`
	Path      string      `json:"path"`
	Query     string      `json:"query"`
	Headers   http.Header `json:"headers"`
	Body      []byte      `json:"body"`
}

func (a *App) healthRequestPath(id int64) string {
	base := strings.TrimSpace(a.cfg.DBPath)
	if base == "" || base == ":memory:" {
		base = "."
	}
	return filepath.Join(filepath.Dir(base), "health-check-requests", fmt.Sprintf("%d.json", id))
}

func (a *App) saveHealthRequest(ctx context.Context, backendID int64, method, upstreamPath, query string, incomingHeaders http.Header, body []byte) {
	if !a.healthChecksEnabledFor(backendID) {
		return
	}
	path := a.healthRequestPath(backendID)
	if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) < healthRequestMaxAge {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		a.logger.Warn("health_check_request_save_failed", "backend_id", backendID, "error", err)
		return
	}
	headers := make(http.Header)
	for key, values := range incomingHeaders {
		if strings.EqualFold(key, "Authorization") || strings.EqualFold(key, "X-Api-Key") || strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Host") {
			continue
		}
		headers[key] = append([]string(nil), values...)
	}
	packet := savedHealthRequest{
		CreatedAt: time.Now().UTC(),
		Method:    method,
		Path:      upstreamPath,
		Query:     query,
		Headers:   headers,
		Body:      append([]byte(nil), body...),
	}
	encoded, err := json.Marshal(packet)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0600); err != nil {
		a.logger.Warn("health_check_request_save_failed", "backend_id", backendID, "error", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		a.logger.Warn("health_check_request_save_failed", "backend_id", backendID, "error", err)
	}
}

func (a *App) healthChecksEnabledFor(id int64) bool {
	for _, raw := range strings.Split(a.cfg.HealthCheckBackends, ",") {
		parsed, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err == nil && parsed == id {
			return true
		}
	}
	return false
}

func (a *App) runHealthChecks(ctx context.Context) {
	a.checkConfiguredBackends(ctx)
	for {
		interval := a.cfg.HealthCheckInterval
		if interval <= 0 {
			interval = time.Minute
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		a.checkConfiguredBackends(ctx)
	}
}

func (a *App) checkConfiguredBackends(ctx context.Context) {
	backends, err := a.store.ListBackends(ctx)
	if err != nil {
		a.logger.Warn("health_check_backend_list_failed", "error", err)
		return
	}
	for _, backend := range backends {
		if ctx.Err() != nil {
			return
		}
		if !a.healthChecksEnabledFor(backend.ID) {
			continue
		}
		packet, err := a.loadHealthRequest(backend.ID)
		if err != nil {
			continue
		}
		healthy := a.replayHealthRequest(ctx, backend, packet)
		if _, err := a.store.SetBackendHealthStatus(ctx, backend.ID, healthy); err != nil {
			a.logger.Warn("health_check_status_update_failed", "backend_id", backend.ID, "error", err)
		}
	}
}

func (a *App) loadHealthRequest(id int64) (savedHealthRequest, error) {
	data, err := os.ReadFile(a.healthRequestPath(id))
	if err != nil {
		return savedHealthRequest{}, err
	}
	var packet savedHealthRequest
	if err := json.Unmarshal(data, &packet); err != nil {
		return packet, err
	}
	if packet.Method == "" || packet.Path == "" {
		return packet, errors.New("invalid health-check request")
	}
	return packet, nil
}

func (a *App) replayHealthRequest(ctx context.Context, backend domain.Backend, packet savedHealthRequest) bool {
	// The saved packet contains the final upstream body. Require its model and
	// resolve the matching API key before sending anything upstream. This keeps
	// health checks valid when the original client model was rewritten.
	model, err := proxy.ExtractModel(packet.Body)
	if err != nil || strings.TrimSpace(model) == "" {
		return false
	}
	apiKey, ok := healthCheckAPIKeyForModel(backend, model)
	if !ok {
		return false
	}
	backend.APIKey = apiKey

	requestURL := &url.URL{Path: packet.Path, RawQuery: packet.Query}
	request, err := http.NewRequestWithContext(ctx, packet.Method, requestURL.String(), bytes.NewReader(packet.Body))
	if err != nil {
		return false
	}
	for key, values := range packet.Headers {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	resp, err := a.proxy.DoWithPath(ctx, request, backend, packet.Body, packet.Path)
	if err != nil {
		return false
	}
	resp, err = decodeUpstreamResponse(resp)
	if err != nil {
		return false
	}
	resp, responseBody, _, _, _, err := handler.CloneResponseForLogging(resp)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var usage domain.UsageLog
	handler.ApplyResponseLogFields(&usage, resp, responseBody, int64(len(responseBody)), "", false)
	return resp.StatusCode == http.StatusOK && usage.InputTokens+usage.OutputTokens+usage.InputCacheTokens > 0
}

func healthCheckAPIKeyForModel(backend domain.Backend, model string) (string, bool) {
	model = strings.TrimSpace(model)
	for _, key := range backend.APIKeys {
		if strings.TrimSpace(key.APIKey) == "" {
			continue
		}
		for _, pattern := range key.Models {
			if healthModelPatternMatches(pattern, model) {
				return key.APIKey, true
			}
		}
		for _, upstreamModel := range key.ModelMapping {
			if healthModelPatternMatches(upstreamModel, model) {
				return key.APIKey, true
			}
		}
	}
	// Keep compatibility with legacy rows that predate api_keys_json.
	if len(backend.APIKeys) == 0 && strings.TrimSpace(backend.APIKey) != "" {
		return backend.APIKey, true
	}
	return "", false
}

func healthModelPatternMatches(pattern, model string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || pattern == "*" {
		return true
	}
	if !strings.ContainsAny(pattern, "*?") {
		return pattern == model
	}
	ok, err := path.Match(pattern, model)
	return err == nil && ok
}
