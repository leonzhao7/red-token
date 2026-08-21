package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"red-token/internal/config"
	"red-token/internal/domain"
	"red-token/internal/service"
	"red-token/internal/store"
)

type BackendHandler struct {
	store                *store.Store
	cfg                  *config.Config
	consoleHTTPClient    *http.Client
	chromeCredentialRead func(context.Context, string, string) (service.ChromeCDPCredentials, error)
	logger               *slog.Logger
	workflowHandler      *WorkflowHandler
}

func NewBackendHandler(st *store.Store) *BackendHandler {
	return &BackendHandler{
		store:  st,
		logger: slog.Default().With("component", "backend_handler"),
	}
}

func (h *BackendHandler) SetWorkflowHandler(workflowHandler *WorkflowHandler) {
	h.workflowHandler = workflowHandler
}

func (h *BackendHandler) SetConsoleHTTPClient(client *http.Client) {
	h.consoleHTTPClient = client
}

func (h *BackendHandler) SetConfig(cfg *config.Config) {
	h.cfg = cfg
}

func (h *BackendHandler) SetLogger(logger *slog.Logger) {
	if logger == nil {
		return
	}
	h.logger = logger.With("component", "backend_handler")
}

type BackendView struct {
	domain.Backend
	RequestCount   int                `json:"request_count"`
	AvgLatencyMS   float64            `json:"avg_latency_ms"`
	LastUsedAt     *time.Time         `json:"last_used_at,omitempty"`
	ModelCount     int                `json:"model_count"`
	HourlyRequests int                `json:"hourly_requests"`
	HourlyFailures int                `json:"hourly_failures"`
	RecentStats    BackendRecentStats `json:"recent_stats"`
}

type BackendRecentStats struct {
	WindowMinutes int `json:"window_minutes"`
	Successes     int `json:"successes"`
	Failures      int `json:"failures"`
}

type backendImportExportPayload struct {
	Backends []backendImportExportItem `json:"backends"`
}

type backendConsoleSyncSummary struct {
	Total        int `json:"total"`
	SuccessCount int `json:"success_count"`
	FailureCount int `json:"failure_count"`
}

type backendUpdatePayload struct {
	Name                   *string                 `json:"name"`
	Protocol               *string                 `json:"protocol"`
	BaseURL                *string                 `json:"base_url"`
	APIKeys                *[]domain.BackendAPIKey `json:"api_keys"`
	APIKey                 *string                 `json:"api_key"`
	ConsoleURL             *string                 `json:"console_url"`
	Tags                   *[]string               `json:"tags"`
	ConsoleUsername        *string                 `json:"console_username"`
	ConsolePassword        *string                 `json:"console_password"`
	ConsoleCheckinWorkflow *string                 `json:"console_checkin_workflow_id"`
	ManualCheckin          *bool                   `json:"manual_checkin"`
	ConsoleHeaders         *map[string]string      `json:"console_headers"`
	ConsoleRefreshToken    *string                 `json:"console_refresh_token,omitempty"`
	UserID                 *string                 `json:"user_id,omitempty"`
	Notes                  *string                 `json:"notes"`
	ProxyID                *int64                  `json:"proxy_id"`
	Status                 *string                 `json:"status"`
	Weight                 *int                    `json:"weight"`
	Models                 *[]string               `json:"models"`
	ModelMapping           *map[string]string      `json:"model_mapping"`
	Endpoints              *[]string               `json:"endpoints"`
}

type backendImportExportItem struct {
	Name                   string                 `json:"name"`
	Protocol               string                 `json:"protocol"`
	BaseURL                string                 `json:"base_url"`
	APIKeys                []domain.BackendAPIKey `json:"api_keys"`
	APIKey                 string                 `json:"api_key,omitempty"`
	ConsoleURL             string                 `json:"console_url"`
	Tags                   []string               `json:"tags"`
	ConsoleUsername        string                 `json:"console_username"`
	ConsolePassword        string                 `json:"console_password"`
	ConsoleCheckinWorkflow string                 `json:"console_checkin_workflow_id,omitempty"`
	ManualCheckin          bool                   `json:"manual_checkin"`
	ConsoleHeaders         map[string]string      `json:"console_headers"`
	ConsoleRefreshToken    string                 `json:"console_refresh_token"`
	ConsoleAccountJSON     string                 `json:"console_account_json"`
	ConsolePricingJSON     string                 `json:"console_pricing_json"`
	Notes                  string                 `json:"notes"`
	ProxyID                int64                  `json:"proxy_id"`
	Status                 string                 `json:"status"`
	ConsecutiveFailures    int                    `json:"consecutive_failures"`
	Weight                 int                    `json:"weight"`
	Models                 []string               `json:"models,omitempty"`
	ModelMapping           map[string]string      `json:"model_mapping,omitempty"`
	Endpoints              []string               `json:"endpoints,omitempty"`
}

type BackendUsageSummary struct {
	RequestCount int
	AvgLatencyMS float64
	LastUsedAt   *time.Time
}

type backendHourlyModelStatsResponse struct {
	Query backendHourlyModelStatsQuery  `json:"query"`
	Scope backendHourlyModelStatsScope  `json:"scope"`
	Items []backendHourlyModelStatsItem `json:"items"`
}

type backendHourlyModelStatsQuery struct {
	Backend   *string `json:"backend"`
	Model     *string `json:"model"`
	StartHour *string `json:"start_hour"`
	EndHour   *string `json:"end_hour"`
}

type backendHourlyModelStatsScope struct {
	Backends  []backendHourlyModelStatsBackendRef `json:"backends"`
	Models    []string                            `json:"models"`
	TimeRange backendHourlyModelStatsTimeRange    `json:"time_range"`
}

type backendHourlyModelStatsBackendRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type backendHourlyModelStatsTimeRange struct {
	StartHour *string `json:"start_hour"`
	EndHour   *string `json:"end_hour"`
	Timezone  string  `json:"timezone"`
}

type backendHourlyModelStatsItem struct {
	BackendID            int64   `json:"backend_id"`
	Backend              string  `json:"backend"`
	Model                string  `json:"model"`
	Hour                 string  `json:"hour"`
	Requests             int     `json:"requests"`
	Successes            int     `json:"successes"`
	Failures             int     `json:"failures"`
	InputTokens          int64   `json:"input_tokens"`
	OutputTokens         int64   `json:"output_tokens"`
	InputCacheTokens     int64   `json:"input_cache_tokens"`
	SuccessAvgDurationMS float64 `json:"success_avg_duration_ms"`
	SuccessRequestBytes  int64   `json:"success_request_bytes"`
	SuccessResponseBytes int64   `json:"success_response_bytes"`
}

type resourceDetailEntry struct {
	Key   string `json:"key,omitempty"`
	Label string `json:"label"`
	Value any    `json:"value"`
}

type resourceDetailActivity struct {
	Usage     []domain.UsageLog   `json:"usage,omitempty"`
	UsageLogs []domain.UsageLog   `json:"usage_logs,omitempty"`
	Events    []domain.AuditEvent `json:"events,omitempty"`
	Backends  []domain.Backend    `json:"backends,omitempty"`
}

type resourceDetailPayload struct {
	Overview      []resourceDetailEntry  `json:"overview"`
	Configuration []resourceDetailEntry  `json:"configuration"`
	Metadata      []resourceDetailEntry  `json:"metadata"`
	Raw           any                    `json:"raw"`
	Activity      resourceDetailActivity `json:"activity"`
}

func (h *BackendHandler) HandleListBackends(w http.ResponseWriter, r *http.Request) {
	page, limit := parsePageQuery(r)
	total, err := h.store.CountBackends(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	backends, err := h.store.ListBackendsPage(r.Context(), limit, pageOffset(page, limit))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	averageLatency, err := h.store.BackendAverageLatencyByIDs(r.Context(), backendIDs(backends))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, pagedResponse(buildBackendFrontendViews(backends, averageLatency), total, page, limit))
}

func (h *BackendHandler) HandleExportBackends(w http.ResponseWriter, r *http.Request) {
	backends, err := h.store.ListBackends(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]backendImportExportItem, 0, len(backends))
	for _, backend := range backends {
		items = append(items, backendToImportExportItem(backend))
	}
	w.Header().Set("Content-Disposition", `attachment; filename="red-token-backends.json"`)
	writeJSON(w, http.StatusOK, backendImportExportPayload{Backends: items})
}

func (h *BackendHandler) HandleImportBackends(w http.ResponseWriter, r *http.Request) {
	var payload backendImportExportPayload
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	backends, err := h.validateBackendImportPayload(r.Context(), payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	imported, err := h.store.ImportBackends(r.Context(), backends)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"imported": len(imported),
		"backends": buildBackendFrontendViews(imported, nil),
	})
}

func (h *BackendHandler) HandleCreateBackend(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Name                   string                 `json:"name"`
		Protocol               string                 `json:"protocol"`
		BaseURL                string                 `json:"base_url"`
		APIKeys                []domain.BackendAPIKey `json:"api_keys"`
		APIKey                 string                 `json:"api_key"`
		ConsoleURL             string                 `json:"console_url"`
		Tags                   []string               `json:"tags"`
		ConsoleUsername        string                 `json:"console_username"`
		ConsolePassword        string                 `json:"console_password"`
		ConsoleCheckinWorkflow string                 `json:"console_checkin_workflow_id"`
		ManualCheckin          bool                   `json:"manual_checkin"`
		ConsoleHeaders         map[string]string      `json:"console_headers"`
		ConsoleRefreshToken    string                 `json:"console_refresh_token"`
		Notes                  string                 `json:"notes"`
		ProxyID                int64                  `json:"proxy_id"`
		Status                 string                 `json:"status"`
		Weight                 int                    `json:"weight"`
		Models                 []string               `json:"models"`
		ModelMapping           map[string]string      `json:"model_mapping"`
		Endpoints              []string               `json:"endpoints"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateURL(payload.BaseURL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(payload.ConsoleURL) != "" {
		if err := validateURL(payload.ConsoleURL); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := h.validateSocksProxyReference(r.Context(), payload.ProxyID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	apiKeys, err := validateBackendAPIKeys(payload.APIKeys, payload.APIKey, payload.Models, payload.ModelMapping)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	consoleCheckinWorkflow := strings.TrimSpace(payload.ConsoleCheckinWorkflow)
	consoleHeaders := payload.ConsoleHeaders
	consoleHeaders, err = normalizeConsoleHeaders(consoleHeaders)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.validateCheckinWorkflowRef(r.Context(), consoleCheckinWorkflow); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	backend, err := h.store.CreateBackend(r.Context(), domain.Backend{
		Name:                   payload.Name,
		Protocol:               domain.NormalizeBackendProtocol(payload.Protocol),
		BaseURL:                payload.BaseURL,
		APIKeys:                apiKeys,
		ConsoleURL:             payload.ConsoleURL,
		Tags:                   payload.Tags,
		ConsoleUsername:        payload.ConsoleUsername,
		ConsolePassword:        payload.ConsolePassword,
		ConsoleCheckinWorkflow: consoleCheckinWorkflow,
		ManualCheckin:          payload.ManualCheckin,
		ConsoleHeaders:         consoleHeaders,
		ConsoleRefreshToken:    payload.ConsoleRefreshToken,
		Notes:                  payload.Notes,
		ProxyID:                payload.ProxyID,
		Weight:                 payload.Weight,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = h.store.AppendAuditEvent(r.Context(), domain.AuditEvent{
		Level:        "warn",
		Type:         "admin_backend_create",
		Actor:        "admin",
		ResourceType: "backend",
		ResourceID:   backend.ID,
		Message:      "backend created: " + backend.Name,
		BackendName:  backend.Name,
	})
	writeJSON(w, http.StatusCreated, buildBackendFrontendView(backend, 0))
}

func (h *BackendHandler) HandleUpdateBackend(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	current, err := h.store.GetBackend(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "backend not found")
		return
	}

	var payload backendUpdatePayload
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if payload.BaseURL != nil {
		if err := validateURL(*payload.BaseURL); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if payload.ConsoleURL != nil && strings.TrimSpace(*payload.ConsoleURL) != "" {
		if err := validateURL(*payload.ConsoleURL); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if payload.ProxyID != nil {
		if err := h.validateSocksProxyReference(r.Context(), *payload.ProxyID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	patch := store.BackendPatch{
		Name:            payload.Name,
		Protocol:        payload.Protocol,
		BaseURL:         payload.BaseURL,
		APIKeys:         nil,
		ConsoleURL:      payload.ConsoleURL,
		Tags:            payload.Tags,
		ConsoleUsername: payload.ConsoleUsername,
		ConsolePassword: payload.ConsolePassword,
		Notes:           payload.Notes,
		ProxyID:         payload.ProxyID,
		Weight:          payload.Weight,
	}
	if payload.APIKeys != nil {
		apiKeys, apiKeysErr := validateBackendAPIKeys(*payload.APIKeys, "", nil, nil)
		if apiKeysErr != nil {
			writeError(w, http.StatusBadRequest, apiKeysErr.Error())
			return
		}
		patch.APIKeys = &apiKeys
	} else if payload.APIKey != nil || payload.Models != nil || payload.ModelMapping != nil {
		apiKeys := append([]domain.BackendAPIKey(nil), current.APIKeys...)
		if len(apiKeys) == 0 {
			apiKeys = []domain.BackendAPIKey{{Group: "default"}}
		}
		if payload.APIKey != nil && strings.TrimSpace(*payload.APIKey) != "" {
			apiKeys[0].APIKey = *payload.APIKey
		}
		if payload.Models != nil {
			apiKeys[0].Models = *payload.Models
		}
		if payload.ModelMapping != nil {
			apiKeys[0].ModelMapping = *payload.ModelMapping
		}
		apiKeys, apiKeysErr := validateBackendAPIKeys(apiKeys, "", nil, nil)
		if apiKeysErr != nil {
			writeError(w, http.StatusBadRequest, apiKeysErr.Error())
			return
		}
		patch.APIKeys = &apiKeys
	}

	if payload.ConsoleCheckinWorkflow != nil {
		value := strings.TrimSpace(*payload.ConsoleCheckinWorkflow)
		if err := h.validateCheckinWorkflowRef(r.Context(), value); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		patch.ConsoleCheckinWorkflow = &value
	}
	if payload.ManualCheckin != nil {
		patch.ManualCheckin = payload.ManualCheckin
	}
	if payload.ConsoleHeaders != nil {
		value, err := normalizeConsoleHeaders(*payload.ConsoleHeaders)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		patch.ConsoleHeaders = &value
		emptyCookie := ""
		patch.ConsoleCookie = &emptyCookie
	}
	if payload.ConsoleRefreshToken != nil {
		patch.ConsoleRefreshToken = payload.ConsoleRefreshToken
	}
	if payload.UserID != nil && strings.TrimSpace(*payload.UserID) != "" {
		account := decodeJSONMap(current.ConsoleAccountJSON)
		account["id"] = strings.TrimSpace(*payload.UserID)
		accountJSON, err := json.Marshal(account)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update user_id")
			return
		}
		accountJSONStr := string(accountJSON)
		patch.ConsoleAccountJSON = &accountJSONStr
	}
	if payload.Status != nil {
		switch *payload.Status {
		case "":
		case domain.BackendStatusNormal, domain.BackendStatusDisabled:
			patch.Status = payload.Status
			patch.ResetRuntimeState = current.Status != *payload.Status
		default:
			writeError(w, http.StatusBadRequest, "invalid backend status")
			return
		}
	}

	backend, err := h.store.PatchBackend(r.Context(), current.ID, patch)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, buildBackendFrontendView(backend, 0))
}

func (h *BackendHandler) HandleBackendConsoleCheckin(w http.ResponseWriter, r *http.Request) {
	recorder := newConsoleRequestRecorder()
	backend, err := h.loadConsoleBackend(r)
	if err != nil {
		h.logConsoleEvent(r.Context(), slog.LevelWarn, "backend_console_checkin_rejected",
			slog.String("error", err.Error()),
		)
		writeConsoleError(w, http.StatusBadRequest, err.Error(), recorder)
		return
	}
	workflowID := strings.TrimSpace(backend.ConsoleCheckinWorkflow)
	if workflowID == "" {
		writeConsoleError(w, http.StatusBadRequest, "console_checkin_workflow_id is required", recorder)
		return
	}
	h.handleWorkflowConsoleCheckin(w, r, backend, workflowID, recorder)
	return
}

// HandleBackendConsoleCookieSync imports Cookie and Authorization credentials
// for a relay console from the Chrome instance exposed through CDP. Chrome is
// commonly running on the Windows host while this service runs in WSL; the
// service resolves the host gateway when the default CDP URL is used.
func (h *BackendHandler) HandleBackendConsoleCookieSync(w http.ResponseWriter, r *http.Request) {
	backend, err := h.loadConsoleBackend(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	endpoint := service.DefaultChromeCDPEndpoint
	if h.cfg != nil && strings.TrimSpace(h.cfg.ChromeCDPEndpoint) != "" {
		endpoint = h.cfg.ChromeCDPEndpoint
	}
	credentials, err := h.readChromeCredentials(r.Context(), endpoint, backend.ConsoleURL)
	if err != nil {
		h.logConsoleEvent(r.Context(), slog.LevelWarn, "backend_console_cookie_sync_failed", append(consoleBackendAttrs(backend),
			slog.String("error", err.Error()),
		)...)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	headers := service.ConsoleHeadersWithCookieValue(service.ConsoleHeaders(backend), credentials.CookieHeader)
	headers = service.ConsoleHeadersWithAuthorizationValue(headers, credentials.Authorization)
	emptyLegacyCookie := ""
	updated, err := h.store.PatchBackend(r.Context(), backend.ID, store.BackendPatch{
		ConsoleHeaders: &headers,
		ConsoleCookie:  &emptyLegacyCookie,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if r.URL.Query().Get("audit") != "0" {
		_ = h.store.AppendAuditEvent(r.Context(), domain.AuditEvent{
			Level:        "info",
			Type:         "admin_backend_cookie_sync",
			Actor:        "admin",
			ResourceType: "backend",
			ResourceID:   backend.ID,
			Message:      "backend console cookies imported from Chrome: " + backend.Name,
			BackendName:  backend.Name,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"backend":               buildBackendFrontendView(updated, 0),
		"cookie_count":          countCookieHeaderValues(credentials.CookieHeader),
		"authorization_updated": credentials.Authorization != "",
	})
}

func (h *BackendHandler) readChromeCredentials(ctx context.Context, endpoint, consoleURL string) (service.ChromeCDPCredentials, error) {
	if h.chromeCredentialRead != nil {
		return h.chromeCredentialRead(ctx, endpoint, consoleURL)
	}
	cookieService := service.NewChromeCDPCookieService(service.ChromeCDPCookieServiceOptions{
		Endpoint:   endpoint,
		HTTPClient: h.consoleHTTPClient,
	})
	return cookieService.ReadCredentials(ctx, consoleURL)
}

func (h *BackendHandler) handleWorkflowConsoleCheckin(w http.ResponseWriter, r *http.Request, backend domain.Backend, workflowID string, recorder *consoleRequestRecorder) {
	if h.workflowHandler == nil {
		h.logConsoleEvent(r.Context(), slog.LevelWarn, "console_checkin_workflow_unavailable", consoleBackendAttrs(backend)...)
		writeConsoleError(w, http.StatusInternalServerError, "workflow handler unavailable", recorder)
		return
	}
	outcome, err := h.workflowHandler.runCheckinWorkflow(r.Context(), backend, workflowID, nil, recorder, nil)
	if err != nil {
		status := http.StatusBadGateway
		var runErr *workflowRunError
		if errors.As(err, &runErr) {
			status = runErr.status
		}
		h.logConsoleEvent(r.Context(), slog.LevelWarn, "console_checkin_workflow_failed", append(consoleBackendAttrs(backend),
			slog.String("workflow_id", workflowID),
			slog.String("error", err.Error()),
		)...)
		writeConsoleError(w, status, err.Error(), recorder)
		return
	}
	h.logConsoleEvent(r.Context(), slog.LevelInfo, "console_checkin_workflow_completed", append(consoleBackendAttrs(outcome.Backend),
		slog.String("workflow_id", outcome.WorkflowID),
	)...)
	writeJSON(w, http.StatusOK, map[string]any{
		"backend":    buildBackendFrontendView(outcome.Backend, 0),
		"checkin":    outcome.Output,
		"account":    decodeJSONMap(outcome.Backend.ConsoleAccountJSON),
		"requests":   outcome.Requests,
		"debug_logs": outcome.DebugLogs,
	})
}

func (h *BackendHandler) handleWorkflowConsoleSync(w http.ResponseWriter, r *http.Request, backend domain.Backend, workflowID string, recorder *consoleRequestRecorder, stream *consoleSyncStream) {
	if h.workflowHandler == nil {
		writeConsoleSyncError(w, http.StatusInternalServerError, "workflow handler unavailable", recorder, stream)
		return
	}
	debugLogs := &workflowDebugLogCollector{Logs: []service.GeneralWorkflowDebugLog{}}
	if stream != nil {
		debugLogs.OnRecord = func(log service.GeneralWorkflowDebugLog) {
			stream.write(map[string]any{
				"type": "workflow_log",
				"log":  log,
			})
		}
	}
	outcome, err := h.workflowHandler.runCheckinWorkflow(r.Context(), backend, workflowID, nil, recorder, debugLogs)
	if err != nil {
		status := http.StatusBadGateway
		var runErr *workflowRunError
		if errors.As(err, &runErr) {
			status = runErr.status
		}
		writeConsoleSyncError(w, status, err.Error(), recorder, stream)
		return
	}
	h.appendBackendConsoleSyncAudit(r, outcome.Backend)
	writeConsoleSyncSuccess(w, map[string]any{
		"backend":    buildBackendFrontendView(outcome.Backend, 0),
		"checkin":    outcome.Output,
		"account":    decodeJSONMap(outcome.Backend.ConsoleAccountJSON),
		"pricing":    decodeJSONMap(outcome.Backend.ConsolePricingJSON),
		"requests":   outcome.Requests,
		"debug_logs": outcome.DebugLogs,
	}, stream)
}

func (h *BackendHandler) HandleBackendConsoleSync(w http.ResponseWriter, r *http.Request) {
	stream := newConsoleSyncStream(w, r)
	recorder := newConsoleRequestRecorder(func(entry consoleRequestLog) {
		if stream != nil {
			stream.write(map[string]any{
				"type":    "request",
				"request": entry,
			})
		}
	})
	backend, err := h.loadConsoleBackend(r)
	if err != nil {
		writeConsoleSyncError(w, http.StatusBadRequest, err.Error(), recorder, stream)
		return
	}
	workflowID := strings.TrimSpace(backend.ConsoleCheckinWorkflow)
	if workflowID == "" {
		writeConsoleSyncError(w, http.StatusBadRequest, "console_checkin_workflow_id is required", recorder, stream)
		return
	}
	h.handleWorkflowConsoleSync(w, r, backend, workflowID, recorder, stream)
}

func (h *BackendHandler) HandleBackendConsoleSyncSummary(w http.ResponseWriter, r *http.Request) {
	var summary backendConsoleSyncSummary
	if err := decodeJSON(r, &summary); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if summary.Total <= 0 || summary.SuccessCount < 0 || summary.FailureCount < 0 || summary.SuccessCount+summary.FailureCount != summary.Total {
		writeError(w, http.StatusBadRequest, "invalid backend sync summary")
		return
	}

	event := domain.AuditEvent{
		Level:        "info",
		Type:         "admin_backends_sync",
		Actor:        "admin",
		ResourceType: "backend",
		Message:      fmt.Sprintf("global backend sync completed: %d/%d succeeded, %d failed", summary.SuccessCount, summary.Total, summary.FailureCount),
	}
	if err := h.store.AppendAuditEvent(r.Context(), event); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *BackendHandler) validateBackendImportPayload(ctx context.Context, payload backendImportExportPayload) ([]domain.Backend, error) {
	existing, err := h.store.ListBackends(ctx)
	if err != nil {
		return nil, err
	}
	existingNames := make(map[string]struct{}, len(existing))
	for _, backend := range existing {
		existingNames[strings.ToLower(strings.TrimSpace(backend.Name))] = struct{}{}
	}

	seenNames := make(map[string]struct{}, len(payload.Backends))
	backends := make([]domain.Backend, 0, len(payload.Backends))
	for i, item := range payload.Backends {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			return nil, fmt.Errorf("backend %d name is required", i+1)
		}
		nameKey := strings.ToLower(name)
		if _, ok := seenNames[nameKey]; ok {
			return nil, fmt.Errorf("duplicate backend name in import: %s", name)
		}
		seenNames[nameKey] = struct{}{}
		if _, ok := existingNames[nameKey]; ok {
			return nil, fmt.Errorf("backend name already exists: %s", name)
		}
		if err := validateURL(item.BaseURL); err != nil {
			return nil, fmt.Errorf("backend %q base_url: %w", name, err)
		}
		if strings.TrimSpace(item.ConsoleURL) != "" {
			if err := validateURL(item.ConsoleURL); err != nil {
				return nil, fmt.Errorf("backend %q console_url: %w", name, err)
			}
		}
		if err := h.validateSocksProxyReference(ctx, item.ProxyID); err != nil {
			return nil, fmt.Errorf("backend %q: %w", name, err)
		}
		status := strings.ToLower(strings.TrimSpace(item.Status))
		if status == "" {
			status = domain.BackendStatusNormal
		}
		switch status {
		case domain.BackendStatusNormal, domain.BackendStatusAbnormal, domain.BackendStatusDisabled:
		default:
			return nil, fmt.Errorf("backend %q invalid status", name)
		}
		if item.ConsecutiveFailures < 0 {
			return nil, fmt.Errorf("backend %q consecutive_failures must be >= 0", name)
		}
		consoleHeaders := item.ConsoleHeaders
		consoleHeaders, err = normalizeConsoleHeaders(consoleHeaders)
		if err != nil {
			return nil, fmt.Errorf("backend %q: %w", name, err)
		}
		apiKeys, apiKeysErr := validateBackendAPIKeys(item.APIKeys, item.APIKey, item.Models, item.ModelMapping)
		if apiKeysErr != nil {
			return nil, fmt.Errorf("backend %q: %w", name, apiKeysErr)
		}
		consoleCheckinWorkflow := strings.TrimSpace(item.ConsoleCheckinWorkflow)
		if err := h.validateCheckinWorkflowRef(ctx, consoleCheckinWorkflow); err != nil {
			return nil, fmt.Errorf("backend %q: %w", name, err)
		}
		backends = append(backends, domain.Backend{
			Name:                   name,
			Protocol:               domain.NormalizeBackendProtocol(item.Protocol),
			BaseURL:                item.BaseURL,
			APIKeys:                apiKeys,
			ConsoleURL:             item.ConsoleURL,
			Tags:                   item.Tags,
			ConsoleUsername:        item.ConsoleUsername,
			ConsolePassword:        item.ConsolePassword,
			ConsoleCheckinWorkflow: consoleCheckinWorkflow,
			ManualCheckin:          item.ManualCheckin,
			ConsoleHeaders:         consoleHeaders,
			ConsoleAccountJSON:     item.ConsoleAccountJSON,
			ConsolePricingJSON:     item.ConsolePricingJSON,
			Notes:                  item.Notes,
			ProxyID:                item.ProxyID,
			Status:                 status,
			ConsecutiveFailures:    item.ConsecutiveFailures,
			Weight:                 item.Weight,
		})
	}
	return backends, nil
}

func backendToImportExportItem(backend domain.Backend) backendImportExportItem {
	return backendImportExportItem{
		Name:                   backend.Name,
		Protocol:               domain.NormalizeBackendProtocol(backend.Protocol),
		BaseURL:                backend.BaseURL,
		APIKeys:                backend.APIKeys,
		ConsoleURL:             backend.ConsoleURL,
		Tags:                   backend.Tags,
		ConsoleUsername:        backend.ConsoleUsername,
		ConsolePassword:        backend.ConsolePassword,
		ConsoleCheckinWorkflow: backend.ConsoleCheckinWorkflow,
		ManualCheckin:          backend.ManualCheckin,
		ConsoleHeaders:         service.ConsoleHeaders(backend),
		ConsoleAccountJSON:     backend.ConsoleAccountJSON,
		ConsolePricingJSON:     backend.ConsolePricingJSON,
		Notes:                  backend.Notes,
		ProxyID:                backend.ProxyID,
		Status:                 backend.Status,
		ConsecutiveFailures:    backend.ConsecutiveFailures,
		Weight:                 backend.Weight,
	}
}

func (h *BackendHandler) HandleDeleteBackend(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	backend, err := h.store.GetBackend(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "backend not found")
		return
	}
	if err := h.store.DeleteBackend(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = h.store.AppendAuditEvent(r.Context(), domain.AuditEvent{
		Level:        "warn",
		Type:         "admin_backend_delete",
		Actor:        "admin",
		ResourceType: "backend",
		ResourceID:   backend.ID,
		Message:      "backend deleted: " + backend.Name,
		BackendName:  backend.Name,
	})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

func (h *BackendHandler) appendBackendConsoleSyncAudit(r *http.Request, backend domain.Backend) {
	if r.URL.Query().Get("audit") == "0" {
		return
	}
	_ = h.store.AppendAuditEvent(r.Context(), domain.AuditEvent{
		Level:        "info",
		Type:         "admin_backend_sync",
		Actor:        "admin",
		ResourceType: "backend",
		ResourceID:   backend.ID,
		Message:      "backend console synced: " + backend.Name,
		BackendName:  backend.Name,
	})
}

func (h *BackendHandler) HandleBackendDetail(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	detail, err := h.store.BackendDetail(r.Context(), id, 10)
	if err != nil {
		writeError(w, http.StatusNotFound, "backend not found")
		return
	}
	writeJSON(w, http.StatusOK, resourceDetailPayload{
		Overview: []resourceDetailEntry{
			detailEntry("name", "Name", detail.Backend.Name),
			detailEntry("console_url", "Console URL", detail.Backend.ConsoleURL),
			detailEntry("console_username", "Console Username", detail.Backend.ConsoleUsername),
			detailEntry("console_password", "Console Password", secretPresenceValue(detail.Backend.ConsolePassword)),
			detailEntry("console_checkin_workflow_id", "Check-in Workflow", detail.Backend.ConsoleCheckinWorkflow),
			detailEntry("console_headers", "Console Headers", consoleHeaderPresence(detail.Backend)),
			detailEntry("status", "Status", detail.Backend.Status),
			detailEntry("consecutive_failures", "Consecutive Failures", detail.Backend.ConsecutiveFailures),
			detailEntry("recover_at", "Recover At", optionalTimePointer(detail.Backend.RecoverAt)),
			detailEntry("proxy", "Proxy", backendProxyDisplay(detail.Backend)),
			detailEntry("proxy_id", "Proxy ID", detail.Backend.ProxyID),
			detailEntry("protocol", "Protocol", detail.Backend.Protocol),
			detailEntry("weight", "Weight", detail.Backend.Weight),
		},
		Configuration: []resourceDetailEntry{
			detailEntry("api_keys", "API Keys", maskedBackendAPIKeys(detail.Backend.APIKeys)),
			detailEntry("tags", "Tags", detail.Backend.Tags),
			detailEntry("notes", "Notes", detail.Backend.Notes),
			detailEntry("base_url", "Base URL", detail.Backend.BaseURL),
			detailEntry("console_account", "Console Account", decodeJSONMap(detail.Backend.ConsoleAccountJSON)),
			detailEntry("console_pricing", "Console Pricing", decodeJSONMap(detail.Backend.ConsolePricingJSON)),
		},
		Metadata: []resourceDetailEntry{
			detailEntry("id", "ID", detail.Backend.ID),
			detailEntry("created_at", "Created At", detail.Backend.CreatedAt),
			detailEntry("updated_at", "Updated At", detail.Backend.UpdatedAt),
		},
		Raw: maskedBackendDetail(detail.Backend),
		Activity: resourceDetailActivity{
			Usage:     EnsureUsageLogs(detail.Usage),
			UsageLogs: EnsureUsageLogs(detail.Usage),
			Events:    EnsureAuditEvents(detail.Events),
			Backends:  []domain.Backend{},
		},
	})
}

func (h *BackendHandler) HandleBackendHourlyModelStats(w http.ResponseWriter, r *http.Request) {
	startHour, err := parseOptionalUTCHourQuery(r.URL.Query().Get("start_hour"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	endHour, err := parseOptionalUTCHourQuery(r.URL.Query().Get("end_hour"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !startHour.IsZero() && !endHour.IsZero() && startHour.After(endHour) {
		writeError(w, http.StatusBadRequest, "start_hour must be before or equal to end_hour")
		return
	}

	filter := store.BackendHourlyModelStatsFilter{
		BackendName: strings.TrimSpace(r.URL.Query().Get("backend")),
		Model:       strings.TrimSpace(r.URL.Query().Get("model")),
		StartHour:   startHour,
		EndHour:     endHour,
	}
	result, err := h.store.ListBackendHourlyModelStats(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]backendHourlyModelStatsItem, 0, len(result.Rows))
	for _, row := range result.Rows {
		avg := 0.0
		if row.Successes > 0 {
			avg = float64(row.SuccessDurationMSSum) / float64(row.Successes)
		}
		items = append(items, backendHourlyModelStatsItem{
			BackendID:            row.BackendID,
			Backend:              row.BackendName,
			Model:                row.Model,
			Hour:                 row.HourStart.UTC().Format(time.RFC3339),
			Requests:             row.Successes + row.Failures,
			Successes:            row.Successes,
			Failures:             row.Failures,
			InputTokens:          row.SuccessInputTokens,
			OutputTokens:         row.SuccessOutputTokens,
			InputCacheTokens:     row.SuccessInputCacheTokens,
			SuccessAvgDurationMS: avg,
			SuccessRequestBytes:  row.SuccessRequestBytes,
			SuccessResponseBytes: row.SuccessResponseBytes,
		})
	}

	backends := make([]backendHourlyModelStatsBackendRef, 0, len(result.Backends))
	for _, backend := range result.Backends {
		backends = append(backends, backendHourlyModelStatsBackendRef{
			ID:   backend.ID,
			Name: backend.Name,
		})
	}

	writeJSON(w, http.StatusOK, backendHourlyModelStatsResponse{
		Query: backendHourlyModelStatsQuery{
			Backend:   optionalString(filter.BackendName),
			Model:     optionalString(filter.Model),
			StartHour: formatOptionalUTCTime(optionalTimeValue(startHour)),
			EndHour:   formatOptionalUTCTime(optionalTimeValue(endHour)),
		},
		Scope: backendHourlyModelStatsScope{
			Backends: backends,
			Models:   result.Models,
			TimeRange: backendHourlyModelStatsTimeRange{
				StartHour: formatOptionalUTCTime(result.RangeStart),
				EndHour:   formatOptionalUTCTime(result.RangeEnd),
				Timezone:  "UTC",
			},
		},
		Items: items,
	})
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "red_token_error",
		},
	})
}

func writeConsoleError(w http.ResponseWriter, status int, message string, recorder *consoleRequestRecorder) {
	requests := []consoleRequestLog{}
	if recorder != nil {
		requests = recorder.Requests
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "red_token_error",
		},
		"requests": requests,
	})
}

type consoleSyncStream struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func newConsoleSyncStream(w http.ResponseWriter, r *http.Request) *consoleSyncStream {
	if !wantsConsoleSyncStream(r) {
		return nil
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &consoleSyncStream{w: w, flusher: flusher}
}

func wantsConsoleSyncStream(r *http.Request) bool {
	if r.URL.Query().Get("stream") == "1" {
		return true
	}
	return strings.Contains(r.Header.Get("Accept"), "application/x-ndjson")
}

func (s *consoleSyncStream) write(event map[string]any) {
	if s == nil {
		return
	}
	_ = json.NewEncoder(s.w).Encode(event)
	s.flusher.Flush()
}

func writeConsoleSyncError(w http.ResponseWriter, status int, message string, recorder *consoleRequestRecorder, stream *consoleSyncStream) {
	if stream != nil {
		requests := []consoleRequestLog{}
		if recorder != nil {
			requests = recorder.Requests
		}
		stream.write(map[string]any{
			"type":     "error",
			"status":   status,
			"message":  message,
			"requests": requests,
		})
		return
	}
	writeConsoleError(w, status, message, recorder)
}

func writeConsoleSyncSuccess(w http.ResponseWriter, payload map[string]any, stream *consoleSyncStream) {
	if stream != nil {
		stream.write(map[string]any{
			"type":     "complete",
			"response": payload,
		})
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func parseID(value string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func validateURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return errors.New("invalid base_url")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("base_url must use http or https")
	}
	if parsed.Host == "" {
		return errors.New("base_url must include host")
	}
	return nil
}

func (h *BackendHandler) validateSocksProxyReference(ctx context.Context, proxyID int64) error {
	if proxyID < 0 {
		return errors.New("proxy_id must be >= 0")
	}
	if proxyID == 0 {
		return nil
	}
	if _, err := h.store.GetSocksProxy(ctx, proxyID); err != nil {
		return errors.New("socks proxy not found")
	}
	return nil
}

func (h *BackendHandler) validateCheckinWorkflowRef(ctx context.Context, workflowID string) error {
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		return nil
	}
	if _, err := h.store.GetHTTPWorkflow(ctx, workflowID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("console checkin workflow not found: %s", workflowID)
		}
		return err
	}
	return nil
}

func EnsureBackendViews(values []BackendView) []BackendView {
	if values == nil {
		return []BackendView{}
	}
	return values
}

func (h *BackendHandler) BackendUsageSummaryMap(ctx context.Context, backends []domain.Backend) (map[int64]BackendUsageSummary, error) {
	ids := make([]int64, 0, len(backends))
	for _, backend := range backends {
		ids = append(ids, backend.ID)
	}

	storeSummaries, err := h.store.BackendUsageSummaryByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	out := make(map[int64]BackendUsageSummary, len(storeSummaries))
	for backendID, summary := range storeSummaries {
		summaryValue := BackendUsageSummary{
			RequestCount: summary.RequestCount,
			AvgLatencyMS: summary.AvgLatencyMS,
		}
		if !summary.LastUsedAt.IsZero() {
			lastUsedAt := summary.LastUsedAt
			summaryValue.LastUsedAt = &lastUsedAt
		}
		out[backendID] = summaryValue
	}
	return out, nil
}

func BuildBackendViews(backends []domain.Backend, summaries map[int64]BackendUsageSummary, stats map[int64]store.BackendRequestStats, hourlyStats map[int64]store.BackendHourlyStats) []BackendView {
	views := make([]BackendView, 0, len(backends))
	for _, backend := range backends {
		stat := stats[backend.ID]
		summary := summaries[backend.ID]
		hourly := hourlyStats[backend.ID]
		views = append(views, BackendView{
			Backend:        backend,
			RequestCount:   summary.RequestCount,
			AvgLatencyMS:   summary.AvgLatencyMS,
			LastUsedAt:     summary.LastUsedAt,
			ModelCount:     len(backend.Models),
			HourlyRequests: hourly.Requests,
			HourlyFailures: hourly.Failures,
			RecentStats: BackendRecentStats{
				WindowMinutes: 30,
				Successes:     stat.Successes,
				Failures:      stat.Failures,
			},
		})
	}
	return views
}

func backendIDs(values []domain.Backend) []int64 {
	if len(values) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(values))
	for _, value := range values {
		ids = append(ids, value.ID)
	}
	return ids
}

func MappedBackendModel(backend domain.Backend, clientModel string) string {
	if backend.ModelMapping == nil {
		return clientModel
	}
	if mapped := strings.TrimSpace(backend.ModelMapping[strings.TrimSpace(clientModel)]); mapped != "" {
		return mapped
	}
	return clientModel
}

func validateBackendAPIKeys(values []domain.BackendAPIKey, legacyAPIKey string, legacyModels []string, legacyModelMapping map[string]string) ([]domain.BackendAPIKey, error) {
	if len(values) == 0 && (strings.TrimSpace(legacyAPIKey) != "" || len(legacyModels) > 0 || len(legacyModelMapping) > 0) {
		values = []domain.BackendAPIKey{{
			APIKey:       legacyAPIKey,
			Group:        "default",
			Models:       legacyModels,
			ModelMapping: legacyModelMapping,
		}}
	}
	if len(values) == 0 {
		return []domain.BackendAPIKey{}, nil
	}

	seen := make(map[string]struct{}, len(values))
	normalized := make([]domain.BackendAPIKey, 0, len(values))
	for index, value := range values {
		value.ID = strings.TrimSpace(value.ID)
		value.APIKey = strings.TrimSpace(value.APIKey)
		value.Name = strings.TrimSpace(value.Name)
		value.Group = strings.TrimSpace(value.Group)
		if value.APIKey == "" {
			return nil, fmt.Errorf("api_keys[%d].key is required", index)
		}
		if value.Group == "" {
			return nil, fmt.Errorf("api_keys[%d].group is required", index)
		}
		if value.UsedQuota < 0 || math.IsNaN(value.UsedQuota) || math.IsInf(value.UsedQuota, 0) {
			return nil, fmt.Errorf("api_keys[%d].used_quota must be a non-negative finite number", index)
		}
		if _, ok := seen[value.APIKey]; ok {
			return nil, fmt.Errorf("duplicate api key at api_keys[%d]", index)
		}
		seen[value.APIKey] = struct{}{}
		value.Models = normalizeBackendStringList(value.Models)
		if len(value.Models) == 0 {
			return nil, fmt.Errorf("api_keys[%d].models must contain at least one model", index)
		}
		value.ModelMapping = normalizeBackendModelMapping(value.ModelMapping)
		normalized = append(normalized, value)
	}
	return normalized, nil
}

func normalizeBackendStringList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}

func normalizeBackendModelMapping(values map[string]string) map[string]string {
	normalized := make(map[string]string, len(values))
	for clientModel, upstreamModel := range values {
		clientModel = strings.TrimSpace(clientModel)
		upstreamModel = strings.TrimSpace(upstreamModel)
		if clientModel != "" && upstreamModel != "" {
			normalized[clientModel] = upstreamModel
		}
	}
	return normalized
}

func (h *BackendHandler) logConsoleEvent(ctx context.Context, level slog.Level, message string, attrs ...slog.Attr) {
	logger := h.logger
	if logger == nil {
		logger = slog.Default().With("component", "backend_handler")
	}
	logger.LogAttrs(ctx, level, message, attrs...)
}

func consoleBackendAttrs(backend domain.Backend) []slog.Attr {
	attrs := []slog.Attr{
		slog.Int64("backend_id", backend.ID),
		slog.String("backend_name", backend.Name),
		slog.String("backend_status", backend.Status),
		slog.Int("console_header_count", len(service.ConsoleHeaders(backend))),
	}
	if parsed, err := url.Parse(strings.TrimSpace(backend.ConsoleURL)); err == nil {
		attrs = append(attrs,
			slog.String("console_scheme", parsed.Scheme),
			slog.String("console_host", parsed.Host),
		)
	} else {
		attrs = append(attrs,
			slog.String("console_scheme", ""),
			slog.String("console_host", ""),
			slog.String("console_url_error", err.Error()),
		)
	}
	return attrs
}

type consoleRequestLog = service.ConsoleRequestLog
type consoleRequestRecorder = service.RequestRecorder

func newConsoleRequestRecorder(onRecord ...func(consoleRequestLog)) *consoleRequestRecorder {
	return service.NewRequestRecorder(onRecord...)
}

func (h *BackendHandler) loadConsoleBackend(r *http.Request) (domain.Backend, error) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		return domain.Backend{}, err
	}
	backend, err := h.store.GetBackend(r.Context(), id)
	if err != nil {
		return domain.Backend{}, errors.New("backend not found")
	}
	if strings.TrimSpace(backend.ConsoleURL) == "" {
		return domain.Backend{}, errors.New("console_url is required")
	}
	if err := validateURL(backend.ConsoleURL); err != nil {
		return domain.Backend{}, err
	}
	return backend, nil
}

func countCookieHeaderValues(header string) int {
	count := 0
	for _, part := range strings.Split(header, ";") {
		if _, _, ok := strings.Cut(strings.TrimSpace(part), "="); ok {
			count++
		}
	}
	return count
}

func normalizeConsoleHeaders(headers map[string]string) (map[string]string, error) {
	if len(headers) == 0 {
		return map[string]string{}, nil
	}
	normalized := make(map[string]string, len(headers))
	seen := make(map[string]struct{}, len(headers))
	for key, value := range headers {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			return nil, errors.New("console header name is required")
		}
		if !validHTTPHeaderName(key) {
			return nil, fmt.Errorf("invalid console header name %q", key)
		}
		if value == "" {
			return nil, fmt.Errorf("console header %q value is required", key)
		}
		if !validHTTPHeaderValue(value) {
			return nil, fmt.Errorf("invalid value for console header %q", key)
		}
		canonicalKey := http.CanonicalHeaderKey(key)
		lowerKey := strings.ToLower(canonicalKey)
		if _, ok := seen[lowerKey]; ok {
			return nil, fmt.Errorf("duplicate console header %q", key)
		}
		seen[lowerKey] = struct{}{}
		normalized[canonicalKey] = value
	}
	return normalized, nil
}

func validHTTPHeaderName(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		char := value[i]
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') {
			continue
		}
		switch char {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			continue
		default:
			return false
		}
	}
	return true
}

func validHTTPHeaderValue(value string) bool {
	for i := 0; i < len(value); i++ {
		char := value[i]
		if char == '\t' || char >= ' ' && char != 0x7f {
			continue
		}
		return false
	}
	return true
}

func modelNameMatchesFocusPatterns(modelName, patterns string) bool {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return false
	}
	// 取 "/" 之后的部分作为匹配目标，没有 "/" 则用完整名称
	shortName := modelName
	if idx := strings.LastIndex(modelName, "/"); idx >= 0 {
		shortName = modelName[idx+1:]
	}
	shortNameLower := strings.ToLower(shortName)
	for _, pattern := range strings.FieldsFunc(patterns, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t'
	}) {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if pattern == "*" {
			return true
		}
		if strings.EqualFold(pattern, shortNameLower) {
			return true
		}
	}
	return false
}

func consoleStoredAccountID(backend domain.Backend) string {
	payload := decodeJSONMap(backend.ConsoleAccountJSON)
	return consoleIDValue(payload["id"])
}

func consoleIDValue(value any) string {
	switch value := value.(type) {
	case float64:
		if value <= 0 {
			return ""
		}
		return strconv.FormatInt(int64(value), 10)
	case int:
		if value <= 0 {
			return ""
		}
		return strconv.Itoa(value)
	case int64:
		if value <= 0 {
			return ""
		}
		return strconv.FormatInt(value, 10)
	case json.Number:
		id, err := value.Int64()
		if err != nil || id <= 0 {
			return ""
		}
		return strconv.FormatInt(id, 10)
	case string:
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func decodeJSONMap(raw string) map[string]any {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return map[string]any{}
	}
	return payload
}

func optionalTimePointer(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	copy := value.UTC()
	return &copy
}

func detailEntry(key, label string, value any) resourceDetailEntry {
	return resourceDetailEntry{
		Key:   key,
		Label: label,
		Value: value,
	}
}

func parsePageQuery(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 10000 {
		limit = 10000
	}
	return page, limit
}

func parseOptionalUTCHourQuery(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid utc hour %q", value)
	}
	parsed = parsed.UTC()
	if parsed.Minute() != 0 || parsed.Second() != 0 || parsed.Nanosecond() != 0 {
		return time.Time{}, fmt.Errorf("utc hour must be aligned to whole hour: %q", value)
	}
	return parsed, nil
}

func formatOptionalUTCTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

func optionalTimeValue(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value
	return &copy
}

func pageOffset(page, limit int) int {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}
	return (page - 1) * limit
}

func pagedResponse(items any, total, page, limit int) map[string]any {
	return map[string]any{
		"items": items,
		"total": total,
		"page":  page,
		"limit": limit,
	}
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	copy := value
	return &copy
}

func secretPresenceValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return "set"
}

func consoleHeaderPresence(backend domain.Backend) string {
	if len(service.ConsoleHeaders(backend)) == 0 {
		return ""
	}
	return "set"
}

func backendProxyDisplay(backend domain.Backend) string {
	if backend.ProxyID == 0 {
		return "direct"
	}
	if backend.Proxy == nil {
		return fmt.Sprintf("proxy #%d", backend.ProxyID)
	}
	label := strings.TrimSpace(backend.Proxy.Name)
	if label == "" {
		label = fmt.Sprintf("proxy #%d", backend.Proxy.ID)
	}
	if address := strings.TrimSpace(backend.Proxy.Address); address != "" {
		label = fmt.Sprintf("%s (%s)", label, address)
	}
	if !backend.Proxy.Enabled {
		label += " - disabled"
	}
	return label
}

func maskedBackendDetail(backend domain.Backend) domain.Backend {
	copy := backend
	copy.APIKey = secretPresenceValue(copy.APIKey)
	copy.APIKeys = maskedBackendAPIKeys(copy.APIKeys)
	copy.ConsolePassword = secretPresenceValue(copy.ConsolePassword)
	copy.ConsoleCookie = secretPresenceValue(copy.ConsoleCookie)
	if len(copy.ConsoleHeaders) > 0 {
		copy.ConsoleHeaders = make(map[string]string, len(copy.ConsoleHeaders))
		for key := range backend.ConsoleHeaders {
			copy.ConsoleHeaders[key] = "set"
		}
	}
	return copy
}

func maskedBackendAPIKeys(values []domain.BackendAPIKey) []domain.BackendAPIKey {
	masked := make([]domain.BackendAPIKey, len(values))
	for index, value := range values {
		masked[index] = value
		masked[index].APIKey = secretPresenceValue(value.APIKey)
	}
	return masked
}
