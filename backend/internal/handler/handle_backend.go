package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	pathpkg "path"
	"strconv"
	"strings"
	"time"

	"red-token/internal/config"
	"red-token/internal/domain"
	"red-token/internal/service"
	"red-token/internal/store"
)

type BackendHandler struct {
	store             *store.Store
	cfg               *config.Config
	consoleHTTPClient *http.Client
	logger            *slog.Logger
}

func NewBackendHandler(st *store.Store) *BackendHandler {
	return &BackendHandler{
		store:  st,
		logger: slog.Default().With("component", "backend_handler"),
	}
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

func (h *BackendHandler) newAPIPlatform() *service.PlatformNewAPI {
	return service.NewPlatformNewAPI(service.PlatformNewAPIOptions{
		HTTPClient: h.consoleHTTPClient,
		UserAgent:  h.backendConsoleUserAgent(),
		Logger:     h.logger,
	})
}

func (h *BackendHandler) sub2APIPlatform() *service.PlatformSub2API {
	return service.NewPlatformSub2API(service.PlatformSub2APIOptions{
		HTTPClient: h.consoleHTTPClient,
		UserAgent:  h.backendConsoleUserAgent(),
	})
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
	Name                 *string                 `json:"name"`
	Protocol             *string                 `json:"protocol"`
	BackendType          *string                 `json:"backend_type"`
	BaseURL              *string                 `json:"base_url"`
	APIKeys              *[]domain.BackendAPIKey `json:"api_keys"`
	APIKey               *string                 `json:"api_key"`
	ConsoleURL           *string                 `json:"console_url"`
	Tags                 *[]string               `json:"tags"`
	ConsoleUsername      *string                 `json:"console_username"`
	ConsolePassword      *string                 `json:"console_password"`
	NewAPIRefresh        *string                 `json:"new_api_refresh"`
	ConsoleAuthorization *string                 `json:"console_authorization"`
	ConsoleCheckinPath   *string                 `json:"console_checkin_path"`
	ChannelURL           *string                 `json:"channel_url"`
	ConsoleCookie        *string                 `json:"console_cookie"`
	ConsoleHeaders       *map[string]string      `json:"console_headers"`
	ConsoleUserID        *string                 `json:"console_user_id"`
	Notes                *string                 `json:"notes"`
	ProxyID              *int64                  `json:"proxy_id"`
	Status               *string                 `json:"status"`
	Weight               *int                    `json:"weight"`
	Models               *[]string               `json:"models"`
	ModelMapping         *map[string]string      `json:"model_mapping"`
	Endpoints            *[]string               `json:"endpoints"`
}

type backendImportExportItem struct {
	Name                 string                 `json:"name"`
	Protocol             string                 `json:"protocol"`
	BackendType          string                 `json:"backend_type"`
	BaseURL              string                 `json:"base_url"`
	APIKeys              []domain.BackendAPIKey `json:"api_keys"`
	APIKey               string                 `json:"api_key,omitempty"`
	ConsoleURL           string                 `json:"console_url"`
	Tags                 []string               `json:"tags"`
	ConsoleUsername      string                 `json:"console_username"`
	ConsolePassword      string                 `json:"console_password"`
	NewAPIRefresh        string                 `json:"new_api_refresh"`
	ConsoleAuthorization string                 `json:"console_authorization"`
	ConsoleCheckinPath   string                 `json:"console_checkin_path"`
	ChannelURL           string                 `json:"channel_url"`
	ConsoleCookie        string                 `json:"console_cookie,omitempty"`
	ConsoleHeaders       map[string]string      `json:"console_headers"`
	ConsoleAccountJSON   string                 `json:"console_account_json"`
	ConsolePricingJSON   string                 `json:"console_pricing_json"`
	Notes                string                 `json:"notes"`
	ProxyID              int64                  `json:"proxy_id"`
	Status               string                 `json:"status"`
	ConsecutiveFailures  int                    `json:"consecutive_failures"`
	Weight               int                    `json:"weight"`
	Models               []string               `json:"models,omitempty"`
	ModelMapping         map[string]string      `json:"model_mapping,omitempty"`
	Endpoints            []string               `json:"endpoints,omitempty"`
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
	stats, err := h.store.BackendRequestStatsSince(r.Context(), time.Now().UTC().Add(-30*time.Minute))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	hourlyStats, err := h.store.BackendHourlyStatsByIDs(r.Context(), backendIDs(backends), time.Now().UTC().Add(-1*time.Hour))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	summaries, err := h.BackendUsageSummaryMap(r.Context(), backends)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := BuildBackendViews(backends, summaries, stats, hourlyStats)
	writeJSON(w, http.StatusOK, pagedResponse(EnsureBackendViews(response), total, page, limit))
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
		"backends": imported,
	})
}

func (h *BackendHandler) HandleCreateBackend(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Name                 string                 `json:"name"`
		Protocol             string                 `json:"protocol"`
		BackendType          string                 `json:"backend_type"`
		BaseURL              string                 `json:"base_url"`
		APIKeys              []domain.BackendAPIKey `json:"api_keys"`
		APIKey               string                 `json:"api_key"`
		ConsoleURL           string                 `json:"console_url"`
		Tags                 []string               `json:"tags"`
		ConsoleUsername      string                 `json:"console_username"`
		ConsolePassword      string                 `json:"console_password"`
		NewAPIRefresh        string                 `json:"new_api_refresh"`
		ConsoleAuthorization string                 `json:"console_authorization"`
		ConsoleCheckinPath   string                 `json:"console_checkin_path"`
		ChannelURL           string                 `json:"channel_url"`
		ConsoleCookie        string                 `json:"console_cookie"`
		ConsoleHeaders       map[string]string      `json:"console_headers"`
		ConsoleUserID        *string                `json:"console_user_id"`
		Notes                string                 `json:"notes"`
		ProxyID              int64                  `json:"proxy_id"`
		Status               string                 `json:"status"`
		Weight               int                    `json:"weight"`
		Models               []string               `json:"models"`
		ModelMapping         map[string]string      `json:"model_mapping"`
		Endpoints            []string               `json:"endpoints"`
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
	backendType := domain.NormalizeBackendType(payload.BackendType)
	consoleAuthorization := strings.TrimSpace(payload.ConsoleAuthorization)
	consoleCheckinPath := normalizeConsoleAPIPath(payload.ConsoleCheckinPath)
	channelURL := normalizeConsoleAPIPath(payload.ChannelURL)
	if backendType != domain.BackendTypeSub2API {
		consoleAuthorization = ""
		consoleCheckinPath = ""
		channelURL = ""
	}
	consoleHeaders := payload.ConsoleHeaders
	if len(consoleHeaders) == 0 && strings.TrimSpace(payload.ConsoleCookie) != "" {
		consoleHeaders = map[string]string{"Cookie": strings.TrimSpace(payload.ConsoleCookie)}
	}
	consoleHeaders, err = normalizeConsoleHeaders(consoleHeaders)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if backendType != domain.BackendTypeNewAPI {
		consoleHeaders = map[string]string{}
	}
	newAPIRefresh := ""
	if backendType == domain.BackendTypeNewAPI {
		newAPIRefresh = strings.TrimSpace(payload.NewAPIRefresh)
	}
	consoleAccountJSON := ""
	if payload.ConsoleUserID != nil {
		var accountErr error
		consoleAccountJSON, accountErr = consoleAccountJSONWithUserID("", *payload.ConsoleUserID)
		if accountErr != nil {
			writeError(w, http.StatusBadRequest, accountErr.Error())
			return
		}
	}

	backend, err := h.store.CreateBackend(r.Context(), domain.Backend{
		Name:                 payload.Name,
		Protocol:             domain.NormalizeBackendProtocol(payload.Protocol),
		BackendType:          backendType,
		BaseURL:              payload.BaseURL,
		APIKeys:              apiKeys,
		ConsoleURL:           payload.ConsoleURL,
		Tags:                 payload.Tags,
		ConsoleUsername:      payload.ConsoleUsername,
		ConsolePassword:      payload.ConsolePassword,
		NewAPIRefresh:        newAPIRefresh,
		ConsoleAuthorization: consoleAuthorization,
		ConsoleCheckinPath:   consoleCheckinPath,
		ChannelURL:           channelURL,
		ConsoleHeaders:       consoleHeaders,
		ConsoleAccountJSON:   consoleAccountJSON,
		Notes:                payload.Notes,
		ProxyID:              payload.ProxyID,
		Weight:               payload.Weight,
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
	writeJSON(w, http.StatusCreated, backend)
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
		BackendType:     payload.BackendType,
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

	backendType := current.BackendType
	if payload.BackendType != nil {
		backendType = domain.NormalizeBackendType(*payload.BackendType)
	}
	if payload.NewAPIRefresh != nil || (payload.BackendType != nil && backendType != domain.BackendTypeNewAPI) {
		value := ""
		if backendType == domain.BackendTypeNewAPI && payload.NewAPIRefresh != nil {
			value = strings.TrimSpace(*payload.NewAPIRefresh)
		}
		patch.NewAPIRefresh = &value
	}
	if payload.ConsoleAuthorization != nil {
		value := ""
		if backendType == domain.BackendTypeSub2API {
			value = strings.TrimSpace(*payload.ConsoleAuthorization)
		}
		patch.ConsoleAuthorization = &value
	}
	if payload.ConsoleCheckinPath != nil {
		value := ""
		if backendType == domain.BackendTypeSub2API {
			value = normalizeConsoleAPIPath(*payload.ConsoleCheckinPath)
		}
		patch.ConsoleCheckinPath = &value
	}
	if payload.ChannelURL != nil {
		value := ""
		if backendType == domain.BackendTypeSub2API {
			value = normalizeConsoleAPIPath(*payload.ChannelURL)
		}
		patch.ChannelURL = &value
	}
	if payload.ConsoleCookie != nil {
		value := map[string]string{}
		if backendType == domain.BackendTypeNewAPI {
			cookie := strings.TrimSpace(*payload.ConsoleCookie)
			if cookie != "" {
				value["Cookie"] = cookie
			}
		}
		patch.ConsoleHeaders = &value
		emptyCookie := ""
		patch.ConsoleCookie = &emptyCookie
	}
	if payload.ConsoleHeaders != nil {
		value := map[string]string{}
		if backendType == domain.BackendTypeNewAPI {
			value, err = normalizeConsoleHeaders(*payload.ConsoleHeaders)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		patch.ConsoleHeaders = &value
		emptyCookie := ""
		patch.ConsoleCookie = &emptyCookie
	}
	if payload.ConsoleUserID != nil {
		accountJSON, accountErr := consoleAccountJSONWithUserID(current.ConsoleAccountJSON, *payload.ConsoleUserID)
		if accountErr != nil {
			writeError(w, http.StatusBadRequest, accountErr.Error())
			return
		}
		patch.ConsoleAccountJSON = &accountJSON
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
	writeJSON(w, http.StatusOK, backend)
}

func (h *BackendHandler) HandleBackendConsoleCheckin(w http.ResponseWriter, r *http.Request) {
	recorder := newNewAPIConsoleRequestRecorder()
	backend, err := h.consoleBackend(r)
	if err != nil {
		h.logConsoleEvent(r.Context(), slog.LevelWarn, "newapi_console_checkin_rejected",
			slog.String("error", err.Error()),
		)
		writeConsoleError(w, http.StatusBadRequest, err.Error(), recorder)
		return
	}
	h.logConsoleEvent(r.Context(), slog.LevelInfo, "newapi_console_checkin_started", consoleBackendAttrs(backend)...)

	accountID := consoleStoredAccountID(backend)
	directCheckin := hasNewAPIDirectCheckinCredentials(backend, accountID)
	var selfResult newAPIConsoleResult
	if directCheckin {
		h.logConsoleEvent(r.Context(), slog.LevelInfo, "newapi_console_checkin_account_identified", append(consoleBackendAttrs(backend),
			slog.String("stage", "stored_account"),
			slog.String("new_api_user", accountID),
		)...)
	} else {
		selfResult, backend, accountID, err = h.newAPIConsoleSelfWithLogin(r.Context(), backend, accountID, recorder)
		if err != nil {
			h.logConsoleEvent(r.Context(), slog.LevelWarn, "newapi_console_checkin_failed", append(consoleBackendAttrs(backend),
				slog.String("stage", "initial_self"),
				slog.String("error", err.Error()),
			)...)
			writeConsoleError(w, http.StatusBadGateway, err.Error(), recorder)
			return
		}
		accountID = firstNonEmpty(consoleAccountID(selfResult.Payload), accountID)
		if accountID == "" {
			h.logConsoleEvent(r.Context(), slog.LevelWarn, "newapi_console_checkin_failed", append(consoleBackendAttrs(backend),
				slog.String("stage", "account_id"),
				slog.String("error", "new-api self response missing user id"),
			)...)
			writeConsoleError(w, http.StatusBadGateway, "new-api self response missing user id", recorder)
			return
		}
		h.logConsoleEvent(r.Context(), slog.LevelInfo, "newapi_console_checkin_account_identified", append(consoleBackendAttrs(backend),
			slog.String("new_api_user", accountID),
		)...)
	}

	result, err := h.newAPIPlatform().Checkin(r.Context(), backend, accountID, recorder)
	if err != nil {
		h.logConsoleEvent(r.Context(), slog.LevelWarn, "newapi_console_checkin_failed", append(consoleBackendAttrs(backend),
			slog.String("stage", "checkin_request"),
			slog.String("new_api_user", accountID),
			slog.String("error", err.Error()),
		)...)
		writeConsoleError(w, http.StatusBadGateway, err.Error(), recorder)
		return
	}

	if result.LoginRequired() && canRecoverNewAPIConsoleCheckinAuth(backend) {
		h.logConsoleEvent(r.Context(), slog.LevelInfo, "newapi_console_checkin_login_required", append(append(consoleBackendAttrs(backend),
			slog.String("stage", "checkin_result"),
			slog.String("new_api_user", accountID),
		), consoleResultAttrs(result)...)...)
		var recovered bool
		backend, accountID, recovered, err = h.recoverNewAPIConsoleCheckinAuth(r.Context(), backend, accountID, recorder)
		if err != nil {
			h.logConsoleEvent(r.Context(), slog.LevelWarn, "newapi_console_checkin_failed", append(consoleBackendAttrs(backend),
				slog.String("stage", "authentication_after_checkin"),
				slog.String("new_api_user", accountID),
				slog.String("error", err.Error()),
			)...)
			writeConsoleError(w, http.StatusBadGateway, err.Error(), recorder)
			return
		}
		if recovered {
			result, err = h.newAPIPlatform().Checkin(r.Context(), backend, accountID, recorder)
			if err != nil {
				h.logConsoleEvent(r.Context(), slog.LevelWarn, "newapi_console_checkin_failed", append(consoleBackendAttrs(backend),
					slog.String("stage", "checkin_retry_request"),
					slog.String("new_api_user", accountID),
					slog.String("error", err.Error()),
				)...)
				writeConsoleError(w, http.StatusBadGateway, err.Error(), recorder)
				return
			}
		}
	}
	if !result.Success() && !result.AlreadyCheckedIn() {
		h.logConsoleEvent(r.Context(), slog.LevelWarn, "newapi_console_checkin_failed", append(append(consoleBackendAttrs(backend),
			slog.String("stage", "checkin_result"),
			slog.String("new_api_user", accountID),
		), consoleResultAttrs(result)...)...)
		writeConsoleError(w, http.StatusBadGateway, result.ErrorMessage("new-api checkin failed"), recorder)
		return
	}
	lastCheckinAt := time.Now().UTC()

	selfResult, err = h.newAPIPlatform().Self(r.Context(), backend, accountID, recorder)
	if err != nil {
		h.logConsoleEvent(r.Context(), slog.LevelWarn, "newapi_console_checkin_failed", append(consoleBackendAttrs(backend),
			slog.String("stage", "refresh_self"),
			slog.String("new_api_user", accountID),
			slog.String("error", err.Error()),
		)...)
		writeConsoleError(w, http.StatusBadGateway, err.Error(), recorder)
		return
	}
	if !selfResult.Success() {
		h.logConsoleEvent(r.Context(), slog.LevelWarn, "newapi_console_checkin_failed", append(append(consoleBackendAttrs(backend),
			slog.String("stage", "refresh_self_result"),
			slog.String("new_api_user", accountID),
		), consoleResultAttrs(selfResult)...)...)
		writeConsoleError(w, http.StatusBadGateway, selfResult.ErrorMessage("new-api self request failed"), recorder)
		return
	}
	accountJSON, err := consoleAccountSummaryJSON(selfResult.Payload, nil, lastCheckinAt, consoleCheckinReward(result.Payload), backend.ConsoleAccountJSON)
	if err != nil {
		h.logConsoleEvent(r.Context(), slog.LevelWarn, "newapi_console_checkin_failed", append(consoleBackendAttrs(backend),
			slog.String("stage", "account_summary"),
			slog.String("new_api_user", accountID),
			slog.String("error", err.Error()),
		)...)
		writeConsoleError(w, http.StatusBadGateway, err.Error(), recorder)
		return
	}
	backend.ConsoleAccountJSON = accountJSON
	updated, err := h.store.UpdateBackend(r.Context(), backend)
	if err != nil {
		h.logConsoleEvent(r.Context(), slog.LevelError, "newapi_console_checkin_failed", append(consoleBackendAttrs(backend),
			slog.String("stage", "save_account_summary"),
			slog.String("new_api_user", accountID),
			slog.String("error", err.Error()),
		)...)
		writeConsoleError(w, http.StatusInternalServerError, err.Error(), recorder)
		return
	}
	h.logConsoleEvent(r.Context(), slog.LevelInfo, "newapi_console_account_summary_saved", append(consoleBackendAttrs(updated),
		slog.String("new_api_user", accountID),
		slog.Int("account_summary_bytes", len(accountJSON)),
	)...)
	h.logConsoleEvent(r.Context(), slog.LevelInfo, "newapi_console_checkin_completed", append(append(consoleBackendAttrs(updated),
		slog.String("new_api_user", accountID),
	), consoleResultAttrs(result)...)...)

	writeJSON(w, http.StatusOK, map[string]any{
		"backend":  updated,
		"checkin":  result.Payload,
		"account":  decodeJSONMap(accountJSON),
		"requests": recorder.Requests,
	})
}

func (h *BackendHandler) HandleBackendConsolePricing(w http.ResponseWriter, r *http.Request) {
	recorder := newNewAPIConsoleRequestRecorder()
	backend, err := h.consoleBackend(r)
	if err != nil {
		writeConsoleError(w, http.StatusBadRequest, err.Error(), recorder)
		return
	}

	accountID := consoleStoredAccountID(backend)
	result, err := h.newAPIPlatform().Pricing(r.Context(), backend, accountID, recorder)
	if err != nil {
		writeConsoleError(w, http.StatusBadGateway, err.Error(), recorder)
		return
	}
	if !result.Success() {
		writeConsoleError(w, http.StatusBadGateway, result.ErrorMessage("new-api pricing request failed"), recorder)
		return
	}
	filteredPricing := filterConsolePricingPayload(result.Payload, h.focusModelPatterns())
	pricingJSON, err := json.Marshal(filteredPricing)
	if err != nil {
		writeConsoleError(w, http.StatusBadGateway, err.Error(), recorder)
		return
	}
	backend.ConsolePricingJSON = string(pricingJSON)
	updated, err := h.store.UpdateBackend(r.Context(), backend)
	if err != nil {
		writeConsoleError(w, http.StatusInternalServerError, err.Error(), recorder)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"backend":  updated,
		"pricing":  filteredPricing,
		"requests": recorder.Requests,
	})
}

func (h *BackendHandler) HandleBackendConsoleSync(w http.ResponseWriter, r *http.Request) {
	stream := newConsoleSyncStream(w, r)
	recorder := newNewAPIConsoleRequestRecorder(func(entry newAPIConsoleRequestLog) {
		if stream != nil {
			stream.write(map[string]any{
				"type":    "request",
				"request": entry,
			})
		}
	})
	backend, err := h.consoleSyncBackend(r)
	if err != nil {
		writeConsoleSyncError(w, http.StatusBadRequest, err.Error(), recorder, stream)
		return
	}

	switch domain.NormalizeBackendType(backend.BackendType) {
	case domain.BackendTypeNewAPI:
		h.handleNewAPIConsoleSync(w, r, backend, recorder, stream)
	case domain.BackendTypeSub2API:
		h.handleSub2APIConsoleSync(w, r, backend, recorder, stream)
	default:
		writeConsoleSyncError(w, http.StatusBadRequest, "backend_type must be new-api or sub2api", recorder, stream)
	}
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

func (h *BackendHandler) handleNewAPIConsoleSync(w http.ResponseWriter, r *http.Request, backend domain.Backend, recorder *newAPIConsoleRequestRecorder, stream *consoleSyncStream) {
	accountID := consoleStoredAccountID(backend)
	if len(newAPIConsoleHeaders(backend)) == 0 {
		updated, loginAccountID, err := h.loginNewAPIConsole(r.Context(), backend, recorder)
		if err != nil {
			writeConsoleSyncError(w, http.StatusBadGateway, err.Error(), recorder, stream)
			return
		}
		backend = updated
		accountID = firstNonEmpty(loginAccountID, accountID)
	}

	statusResult, err := h.newAPIPlatform().Status(r.Context(), backend, recorder)
	if err != nil {
		writeConsoleSyncError(w, http.StatusBadGateway, err.Error(), recorder, stream)
		return
	}
	if !statusResult.Success() {
		writeConsoleSyncError(w, http.StatusBadGateway, statusResult.ErrorMessage("new-api status request failed"), recorder, stream)
		return
	}

	lastCheckinAt, checkedInToday := consoleLastCheckinStatus(backend, time.Now().UTC())
	checkinEnabled := service.NewAPIStatusCheckinEnabled(statusResult.Payload)
	recordSyncCompletionAsCheckin := !checkinEnabled
	var selfResult newAPIConsoleResult
	var checkinPayload map[string]any
	if checkedInToday || !checkinEnabled {
		selfResult, backend, accountID, err = h.newAPIConsoleSelfWithLogin(r.Context(), backend, accountID, recorder)
		if err != nil {
			writeConsoleSyncError(w, http.StatusBadGateway, err.Error(), recorder, stream)
			return
		}
		accountID = firstNonEmpty(consoleAccountID(selfResult.Payload), accountID)
		if accountID == "" {
			writeConsoleSyncError(w, http.StatusBadGateway, "new-api self response missing user id", recorder, stream)
			return
		}
	} else {
		checkinResult, updatedBackend, updatedAccountID, err := h.performNewAPIConsoleCheckin(r.Context(), backend, accountID, recorder)
		if err != nil {
			writeConsoleSyncError(w, http.StatusBadGateway, err.Error(), recorder, stream)
			return
		}
		backend = updatedBackend
		accountID = firstNonEmpty(updatedAccountID, accountID)
		checkinPayload = checkinResult.Payload
		lastCheckinAt = time.Now().UTC()

		selfResult, err = h.newAPIPlatform().Self(r.Context(), backend, accountID, recorder)
		if err != nil {
			writeConsoleSyncError(w, http.StatusBadGateway, err.Error(), recorder, stream)
			return
		}
		if !selfResult.Success() {
			writeConsoleSyncError(w, http.StatusBadGateway, selfResult.ErrorMessage("new-api self request failed"), recorder, stream)
			return
		}
		accountID = firstNonEmpty(consoleAccountID(selfResult.Payload), accountID)
	}

	accountJSON, err := consoleAccountSummaryJSON(selfResult.Payload, statusResult.Payload, lastCheckinAt, consoleCheckinReward(checkinPayload), backend.ConsoleAccountJSON)
	if err != nil {
		writeConsoleSyncError(w, http.StatusBadGateway, err.Error(), recorder, stream)
		return
	}
	backend.ConsoleAccountJSON = accountJSON
	backend, err = h.store.UpdateBackend(r.Context(), backend)
	if err != nil {
		writeConsoleSyncError(w, http.StatusInternalServerError, err.Error(), recorder, stream)
		return
	}

	pricingResult, err := h.newAPIPlatform().Pricing(r.Context(), backend, accountID, recorder)
	if err != nil {
		writeConsoleSyncError(w, http.StatusBadGateway, err.Error(), recorder, stream)
		return
	}
	if !pricingResult.Success() {
		writeConsoleSyncError(w, http.StatusBadGateway, pricingResult.ErrorMessage("new-api pricing request failed"), recorder, stream)
		return
	}
	filteredPricing := filterConsolePricingPayload(pricingResult.Payload, h.focusModelPatterns())
	pricingJSON, err := json.Marshal(filteredPricing)
	if err != nil {
		writeConsoleSyncError(w, http.StatusBadGateway, err.Error(), recorder, stream)
		return
	}
	if recordSyncCompletionAsCheckin {
		lastCheckinAt = time.Now().UTC()
		accountJSON, err = consoleAccountSummaryJSON(selfResult.Payload, statusResult.Payload, lastCheckinAt, nil, backend.ConsoleAccountJSON)
		if err != nil {
			writeConsoleSyncError(w, http.StatusBadGateway, err.Error(), recorder, stream)
			return
		}
		backend.ConsoleAccountJSON = accountJSON
	}
	backend, err = h.syncNewAPIConsoleTokens(r.Context(), backend, accountID, recorder)
	if err != nil {
		writeConsoleSyncError(w, http.StatusBadGateway, err.Error(), recorder, stream)
		return
	}
	backend.ConsolePricingJSON = string(pricingJSON)

	updated, err := h.store.UpdateBackend(r.Context(), backend)
	if err != nil {
		writeConsoleSyncError(w, http.StatusInternalServerError, err.Error(), recorder, stream)
		return
	}
	h.appendBackendConsoleSyncAudit(r, updated)

	writeConsoleSyncSuccess(w, map[string]any{
		"backend":  updated,
		"status":   statusResult.Payload,
		"checkin":  checkinPayload,
		"account":  decodeJSONMap(accountJSON),
		"pricing":  filteredPricing,
		"requests": recorder.Requests,
	}, stream)
}

func (h *BackendHandler) handleSub2APIConsoleSync(w http.ResponseWriter, r *http.Request, backend domain.Backend, recorder *newAPIConsoleRequestRecorder, stream *consoleSyncStream) {
	result, err := h.sub2APIPlatform().Sync(r.Context(), backend, recorder)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, service.ErrSub2APIConsoleAuthorizationRequired) {
			status = http.StatusBadRequest
		}
		writeConsoleSyncError(w, status, err.Error(), recorder, stream)
		return
	}

	updated, err := h.store.UpdateBackend(r.Context(), result.Backend)
	if err != nil {
		writeConsoleSyncError(w, http.StatusInternalServerError, err.Error(), recorder, stream)
		return
	}
	h.appendBackendConsoleSyncAudit(r, updated)

	writeConsoleSyncSuccess(w, map[string]any{
		"backend":  updated,
		"checkin":  result.Checkin,
		"account":  result.Account,
		"pricing":  result.Pricing,
		"requests": recorder.Requests,
	}, stream)
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
		backendType := domain.NormalizeBackendType(item.BackendType)
		consoleHeaders := item.ConsoleHeaders
		if len(consoleHeaders) == 0 && strings.TrimSpace(item.ConsoleCookie) != "" {
			consoleHeaders = map[string]string{"Cookie": strings.TrimSpace(item.ConsoleCookie)}
		}
		consoleHeaders, err = normalizeConsoleHeaders(consoleHeaders)
		if err != nil {
			return nil, fmt.Errorf("backend %q: %w", name, err)
		}
		if backendType != domain.BackendTypeNewAPI {
			consoleHeaders = map[string]string{}
		}
		newAPIRefresh := ""
		if backendType == domain.BackendTypeNewAPI {
			newAPIRefresh = strings.TrimSpace(item.NewAPIRefresh)
		}
		apiKeys, apiKeysErr := validateBackendAPIKeys(item.APIKeys, item.APIKey, item.Models, item.ModelMapping)
		if apiKeysErr != nil {
			return nil, fmt.Errorf("backend %q: %w", name, apiKeysErr)
		}
		backends = append(backends, domain.Backend{
			Name:                 name,
			Protocol:             domain.NormalizeBackendProtocol(item.Protocol),
			BackendType:          backendType,
			BaseURL:              item.BaseURL,
			APIKeys:              apiKeys,
			ConsoleURL:           item.ConsoleURL,
			Tags:                 item.Tags,
			ConsoleUsername:      item.ConsoleUsername,
			ConsolePassword:      item.ConsolePassword,
			NewAPIRefresh:        newAPIRefresh,
			ConsoleAuthorization: item.ConsoleAuthorization,
			ConsoleCheckinPath:   normalizeConsoleAPIPath(item.ConsoleCheckinPath),
			ChannelURL:           normalizeConsoleAPIPath(item.ChannelURL),
			ConsoleHeaders:       consoleHeaders,
			ConsoleAccountJSON:   item.ConsoleAccountJSON,
			ConsolePricingJSON:   item.ConsolePricingJSON,
			Notes:                item.Notes,
			ProxyID:              item.ProxyID,
			Status:               status,
			ConsecutiveFailures:  item.ConsecutiveFailures,
			Weight:               item.Weight,
		})
	}
	return backends, nil
}

func backendToImportExportItem(backend domain.Backend) backendImportExportItem {
	return backendImportExportItem{
		Name:                 backend.Name,
		Protocol:             domain.NormalizeBackendProtocol(backend.Protocol),
		BackendType:          domain.NormalizeBackendType(backend.BackendType),
		BaseURL:              backend.BaseURL,
		APIKeys:              backend.APIKeys,
		ConsoleURL:           backend.ConsoleURL,
		Tags:                 backend.Tags,
		ConsoleUsername:      backend.ConsoleUsername,
		ConsolePassword:      backend.ConsolePassword,
		NewAPIRefresh:        backend.NewAPIRefresh,
		ConsoleAuthorization: backend.ConsoleAuthorization,
		ConsoleCheckinPath:   backend.ConsoleCheckinPath,
		ChannelURL:           backend.ChannelURL,
		ConsoleHeaders:       newAPIConsoleHeaders(backend),
		ConsoleAccountJSON:   backend.ConsoleAccountJSON,
		ConsolePricingJSON:   backend.ConsolePricingJSON,
		Notes:                backend.Notes,
		ProxyID:              backend.ProxyID,
		Status:               backend.Status,
		ConsecutiveFailures:  backend.ConsecutiveFailures,
		Weight:               backend.Weight,
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
			detailEntry("backend_type", "Backend Type", domain.NormalizeBackendType(detail.Backend.BackendType)),
			detailEntry("console_url", "Console URL", detail.Backend.ConsoleURL),
			detailEntry("console_username", "Console Username", detail.Backend.ConsoleUsername),
			detailEntry("console_checkin_path", "Console Check-in Path", detail.Backend.ConsoleCheckinPath),
			detailEntry("channel_url", "Channel URL", detail.Backend.ChannelURL),
			detailEntry("console_password", "Console Password", secretPresenceValue(detail.Backend.ConsolePassword)),
			detailEntry("new_api_refresh", "new_api_refresh", secretPresenceValue(detail.Backend.NewAPIRefresh)),
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

func writeConsoleError(w http.ResponseWriter, status int, message string, recorder *newAPIConsoleRequestRecorder) {
	requests := []newAPIConsoleRequestLog{}
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

func writeConsoleSyncError(w http.ResponseWriter, status int, message string, recorder *newAPIConsoleRequestRecorder, stream *consoleSyncStream) {
	if stream != nil {
		requests := []newAPIConsoleRequestLog{}
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
		value.APIKey = strings.TrimSpace(value.APIKey)
		value.Group = strings.TrimSpace(value.Group)
		if value.APIKey == "" {
			return nil, fmt.Errorf("api_keys[%d].api_key is required", index)
		}
		if value.Group == "" {
			return nil, fmt.Errorf("api_keys[%d].group is required", index)
		}
		if value.UsedQuota < 0 {
			return nil, fmt.Errorf("api_keys[%d].used_quota must be >= 0", index)
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
		slog.String("backend_type", domain.NormalizeBackendType(backend.BackendType)),
		slog.String("backend_status", backend.Status),
		slog.Int("console_header_count", len(newAPIConsoleHeaders(backend))),
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

func consoleRequestAttrs(backend domain.Backend, method, path string, body []byte, headers map[string]string, newAPIUser string) []slog.Attr {
	newAPIUser = strings.TrimSpace(newAPIUser)
	attrs := append(consoleBackendAttrs(backend),
		slog.String("method", method),
		slog.String("path", path),
		slog.Int("request_body_bytes", len(body)),
		slog.Int("configured_header_count", len(headers)),
		slog.Bool("new_api_user_present", newAPIUser != ""),
	)
	if newAPIUser != "" {
		attrs = append(attrs, slog.String("new_api_user", newAPIUser))
	}
	return attrs
}

func consoleResultAttrs(result newAPIConsoleResult) []slog.Attr {
	attrs := []slog.Attr{
		slog.Int("console_http_status", result.StatusCode),
		slog.Bool("console_success", result.Success()),
		slog.Bool("console_login_required", result.LoginRequired()),
	}
	if message := consoleResultMessage(result); message != "" {
		attrs = append(attrs, slog.String("console_message", message))
	}
	return attrs
}

func consoleResultMessage(result newAPIConsoleResult) string {
	message := strings.TrimSpace(fmt.Sprint(result.Payload["message"]))
	if message == "" || message == "<nil>" {
		return ""
	}
	const limit = 200
	if len([]rune(message)) <= limit {
		return message
	}
	return string([]rune(message)[:limit]) + "..."
}

type newAPIConsoleResult = service.NewAPIResult
type newAPIConsoleRequestLog = service.NewAPIRequestLog
type newAPIConsoleRequestRecorder = service.NewAPIRequestRecorder

func newNewAPIConsoleRequestRecorder(onRecord ...func(newAPIConsoleRequestLog)) *newAPIConsoleRequestRecorder {
	return service.NewNewAPIRequestRecorder(onRecord...)
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

func (h *BackendHandler) consoleBackend(r *http.Request) (domain.Backend, error) {
	backend, err := h.loadConsoleBackend(r)
	if err != nil {
		return domain.Backend{}, err
	}
	if domain.NormalizeBackendType(backend.BackendType) != domain.BackendTypeNewAPI {
		return domain.Backend{}, errors.New("backend_type must be new-api")
	}
	return backend, nil
}

func (h *BackendHandler) consoleSyncBackend(r *http.Request) (domain.Backend, error) {
	backend, err := h.loadConsoleBackend(r)
	if err != nil {
		return domain.Backend{}, err
	}
	switch domain.NormalizeBackendType(backend.BackendType) {
	case domain.BackendTypeNewAPI, domain.BackendTypeSub2API:
		return backend, nil
	default:
		return domain.Backend{}, errors.New("backend_type must be new-api or sub2api")
	}
}

func (h *BackendHandler) performNewAPIConsoleCheckin(ctx context.Context, backend domain.Backend, accountID string, recorder *newAPIConsoleRequestRecorder) (newAPIConsoleResult, domain.Backend, string, error) {
	result, err := h.newAPIPlatform().Checkin(ctx, backend, accountID, recorder)
	if err != nil {
		return newAPIConsoleResult{}, backend, accountID, err
	}
	if result.LoginRequired() && canRecoverNewAPIConsoleCheckinAuth(backend) {
		updated, updatedAccountID, recovered, err := h.recoverNewAPIConsoleCheckinAuth(ctx, backend, accountID, recorder)
		if err != nil {
			return newAPIConsoleResult{}, backend, accountID, err
		}
		backend = updated
		accountID = firstNonEmpty(updatedAccountID, accountID)
		if recovered {
			result, err = h.newAPIPlatform().Checkin(ctx, backend, accountID, recorder)
			if err != nil {
				return newAPIConsoleResult{}, backend, accountID, err
			}
		}
	}
	if !result.Success() && !result.AlreadyCheckedIn() {
		return newAPIConsoleResult{}, backend, accountID, errors.New(result.ErrorMessage("new-api checkin failed"))
	}
	return result, backend, accountID, nil
}

func (h *BackendHandler) recoverNewAPIConsoleCheckinAuth(ctx context.Context, backend domain.Backend, accountID string, recorder *newAPIConsoleRequestRecorder) (domain.Backend, string, bool, error) {
	if hasNewAPIConsoleLoginCredentials(backend) {
		updated, loginAccountID, err := h.loginNewAPIConsole(ctx, backend, recorder)
		if err != nil {
			return backend, accountID, false, err
		}
		accountID = firstNonEmpty(loginAccountID, accountID)
		selfResult, updated, updatedAccountID, err := h.newAPIConsoleSelfWithLogin(ctx, updated, accountID, recorder)
		if err != nil {
			return updated, accountID, false, err
		}
		accountID = firstNonEmpty(updatedAccountID, consoleAccountID(selfResult.Payload), accountID)
		if accountID == "" {
			return updated, accountID, false, errors.New("new-api self response missing user id")
		}
		return updated, accountID, true, nil
	}
	if hasNewAPIConsoleRefreshCredentials(backend) {
		updated, refreshAccountID, err := h.refreshNewAPIConsole(ctx, backend, recorder)
		if err != nil {
			return backend, accountID, false, err
		}
		return updated, firstNonEmpty(refreshAccountID, accountID), true, nil
	}
	return backend, accountID, false, nil
}

func (h *BackendHandler) focusModelPatterns() string {
	if h.cfg == nil {
		return ""
	}
	return h.cfg.FocusModels
}

func (h *BackendHandler) newAPIConsoleSelfWithLogin(ctx context.Context, backend domain.Backend, accountID string, recorder *newAPIConsoleRequestRecorder) (newAPIConsoleResult, domain.Backend, string, error) {
	h.logConsoleEvent(ctx, slog.LevelInfo, "newapi_console_self_started", consoleBackendAttrs(backend)...)
	selfResult, err := h.newAPIPlatform().Self(ctx, backend, accountID, recorder)
	if err != nil {
		h.logConsoleEvent(ctx, slog.LevelWarn, "newapi_console_self_failed", append(consoleBackendAttrs(backend),
			slog.String("stage", "request"),
			slog.String("error", err.Error()),
		)...)
		return newAPIConsoleResult{}, backend, accountID, err
	}
	if selfResult.Success() {
		h.logConsoleEvent(ctx, slog.LevelInfo, "newapi_console_self_completed", append(consoleBackendAttrs(backend), consoleResultAttrs(selfResult)...)...)
		return selfResult, backend, firstNonEmpty(consoleAccountID(selfResult.Payload), accountID), nil
	}
	if !selfResult.LoginRequired() {
		h.logConsoleEvent(ctx, slog.LevelWarn, "newapi_console_self_failed", append(append(consoleBackendAttrs(backend),
			slog.String("stage", "result"),
		), consoleResultAttrs(selfResult)...)...)
		return newAPIConsoleResult{}, backend, accountID, errors.New(selfResult.ErrorMessage("new-api self request failed"))
	}
	h.logConsoleEvent(ctx, slog.LevelInfo, "newapi_console_self_authentication_required", append(consoleBackendAttrs(backend), consoleResultAttrs(selfResult)...)...)

	var (
		updated          domain.Backend
		authAccountID    string
		authenticationBy string
	)
	switch {
	case hasNewAPIConsoleLoginCredentials(backend):
		authenticationBy = "login"
		updated, authAccountID, err = h.loginNewAPIConsole(ctx, backend, recorder)
	case hasNewAPIConsoleRefreshCredentials(backend):
		authenticationBy = "refresh"
		updated, authAccountID, err = h.refreshNewAPIConsole(ctx, backend, recorder)
	default:
		return newAPIConsoleResult{}, backend, accountID, errors.New(selfResult.ErrorMessage("new-api self request failed"))
	}
	if err != nil {
		h.logConsoleEvent(ctx, slog.LevelWarn, "newapi_console_self_failed", append(consoleBackendAttrs(backend),
			slog.String("stage", authenticationBy),
			slog.String("error", err.Error()),
		)...)
		return newAPIConsoleResult{}, backend, accountID, err
	}
	accountID = firstNonEmpty(authAccountID, accountID)
	selfResult, err = h.newAPIPlatform().Self(ctx, updated, accountID, recorder)
	if err != nil {
		h.logConsoleEvent(ctx, slog.LevelWarn, "newapi_console_self_failed", append(consoleBackendAttrs(updated),
			slog.String("stage", "request_after_"+authenticationBy),
			slog.String("error", err.Error()),
		)...)
		return newAPIConsoleResult{}, updated, accountID, err
	}
	if !selfResult.Success() {
		h.logConsoleEvent(ctx, slog.LevelWarn, "newapi_console_self_failed", append(append(consoleBackendAttrs(updated),
			slog.String("stage", "result_after_"+authenticationBy),
		), consoleResultAttrs(selfResult)...)...)
		return newAPIConsoleResult{}, updated, accountID, errors.New(selfResult.ErrorMessage("new-api self request failed"))
	}
	accountID = firstNonEmpty(consoleAccountID(selfResult.Payload), accountID)
	h.logConsoleEvent(ctx, slog.LevelInfo, "newapi_console_self_completed", append(append(consoleBackendAttrs(updated),
		slog.String("stage", "after_"+authenticationBy),
	), consoleResultAttrs(selfResult)...)...)
	return selfResult, updated, accountID, nil
}

func (h *BackendHandler) loginNewAPIConsole(ctx context.Context, backend domain.Backend, recorder *newAPIConsoleRequestRecorder) (domain.Backend, string, error) {
	h.logConsoleEvent(ctx, slog.LevelInfo, "newapi_console_login_started", append(consoleBackendAttrs(backend),
		slog.Bool("console_username_present", strings.TrimSpace(backend.ConsoleUsername) != ""),
		slog.Bool("console_password_present", strings.TrimSpace(backend.ConsolePassword) != ""),
	)...)
	loginResult, err := h.newAPIPlatform().Login(ctx, backend, recorder)
	if err != nil {
		h.logConsoleEvent(ctx, slog.LevelWarn, "newapi_console_login_failed", append(consoleBackendAttrs(backend),
			slog.String("stage", "request"),
			slog.String("error", err.Error()),
		)...)
		return domain.Backend{}, "", err
	}
	if !loginResult.Success() {
		h.logConsoleEvent(ctx, slog.LevelWarn, "newapi_console_login_failed", append(append(consoleBackendAttrs(backend),
			slog.String("stage", "result"),
		), consoleResultAttrs(loginResult)...)...)
		return domain.Backend{}, "", errors.New(loginResult.ErrorMessage("new-api login failed"))
	}
	accessToken := service.NewAPIAccessToken(loginResult.Payload)
	cookies, rotatedRefresh := service.SplitNewAPIRefreshCookie(service.NewAPIResponseCookies(loginResult.Header))
	if accessToken == "" && len(cookies) == 0 {
		h.logConsoleEvent(ctx, slog.LevelWarn, "newapi_console_login_failed", append(append(consoleBackendAttrs(backend),
			slog.String("stage", "response_credentials"),
		), consoleResultAttrs(loginResult)...)...)
		return domain.Backend{}, "", errors.New("new-api login did not return access token or cookies")
	}
	accountID := consoleAccountID(loginResult.Payload)
	previousHeaders := newAPIConsoleHeaders(backend)
	backend.ConsoleHeaders = service.NewAPIConsoleLoginHeaders(previousHeaders, cookies, accessToken)
	backend.ConsoleCookie = ""
	if rotatedRefresh != "" {
		backend.NewAPIRefresh = rotatedRefresh
	}
	h.logConsoleEvent(ctx, slog.LevelInfo, "newapi_console_login_headers_refreshed", append(consoleBackendAttrs(backend),
		slog.Int("previous_header_count", len(previousHeaders)),
		slog.Int("updated_header_count", len(backend.ConsoleHeaders)),
	)...)
	updated, err := h.store.UpdateBackend(ctx, backend)
	if err != nil {
		h.logConsoleEvent(ctx, slog.LevelError, "newapi_console_login_failed", append(consoleBackendAttrs(backend),
			slog.String("stage", "save_headers"),
			slog.String("error", err.Error()),
		)...)
		return domain.Backend{}, accountID, err
	}
	h.logConsoleEvent(ctx, slog.LevelInfo, "newapi_console_login_completed", append(append(consoleBackendAttrs(updated),
		slog.Bool("new_api_user_present", accountID != ""),
	), consoleResultAttrs(loginResult)...)...)
	return updated, accountID, nil
}

func (h *BackendHandler) refreshNewAPIConsole(ctx context.Context, backend domain.Backend, recorder *newAPIConsoleRequestRecorder) (domain.Backend, string, error) {
	h.logConsoleEvent(ctx, slog.LevelInfo, "newapi_console_refresh_started", append(consoleBackendAttrs(backend),
		slog.Bool("cookie_present", newAPIConsoleCookieValue(backend) != ""),
		slog.Bool("new_api_refresh_present", strings.TrimSpace(backend.NewAPIRefresh) != ""),
	)...)
	refreshResult, err := h.newAPIPlatform().Refresh(ctx, backend, recorder)
	if err != nil {
		h.logConsoleEvent(ctx, slog.LevelWarn, "newapi_console_refresh_failed", append(consoleBackendAttrs(backend),
			slog.String("stage", "request"),
			slog.String("error", err.Error()),
		)...)
		return backend, "", err
	}
	if !refreshResult.Success() {
		h.logConsoleEvent(ctx, slog.LevelWarn, "newapi_console_refresh_failed", append(append(consoleBackendAttrs(backend),
			slog.String("stage", "result"),
		), consoleResultAttrs(refreshResult)...)...)
		return backend, "", errors.New(refreshResult.ErrorMessage("new-api refresh failed"))
	}

	accessToken := service.NewAPIAccessToken(refreshResult.Payload)
	if accessToken == "" {
		return backend, "", errors.New("new-api refresh did not return an access token")
	}
	previousHeaders := newAPIConsoleHeaders(backend)
	responseCookies, rotatedRefresh := service.SplitNewAPIRefreshCookie(service.NewAPIResponseCookies(refreshResult.Header))
	backend.ConsoleHeaders = service.NewAPIConsoleLoginHeaders(previousHeaders, responseCookies, accessToken)
	backend.ConsoleCookie = ""
	if rotatedRefresh != "" {
		backend.NewAPIRefresh = rotatedRefresh
	}
	accountID := consoleAccountID(refreshResult.Payload)
	updated, err := h.store.UpdateBackend(ctx, backend)
	if err != nil {
		h.logConsoleEvent(ctx, slog.LevelError, "newapi_console_refresh_failed", append(consoleBackendAttrs(backend),
			slog.String("stage", "save_credentials"),
			slog.String("error", err.Error()),
		)...)
		return backend, accountID, err
	}
	h.logConsoleEvent(ctx, slog.LevelInfo, "newapi_console_refresh_completed", append(append(consoleBackendAttrs(updated),
		slog.Bool("new_api_user_present", accountID != ""),
	), consoleResultAttrs(refreshResult)...)...)
	return updated, accountID, nil
}

func (h *BackendHandler) syncNewAPIConsoleTokens(ctx context.Context, backend domain.Backend, accountID string, recorder *newAPIConsoleRequestRecorder) (domain.Backend, error) {
	apiKeys, err := h.newAPIPlatform().SyncTokens(ctx, backend, accountID, recorder)
	if err != nil {
		return backend, err
	}
	backend.APIKeys = apiKeys
	return backend, nil
}

func (h *BackendHandler) backendConsoleUserAgent() string {
	if h.cfg == nil {
		return config.DefaultBackendConsoleUserAgent
	}
	value := strings.TrimSpace(h.cfg.BackendConsoleUserAgent)
	if value == "" {
		return config.DefaultBackendConsoleUserAgent
	}
	return value
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

func newAPIConsoleHeaders(backend domain.Backend) map[string]string {
	return service.NewAPIConsoleHeaders(backend)
}

func newAPIConsoleCookieValue(backend domain.Backend) string {
	return service.NewAPIConsoleCookieValue(backend)
}

func normalizeConsoleAPIPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	return value
}

func consoleAccountSummaryJSON(payload map[string]any, statusPayload map[string]any, lastCheckinAt time.Time, checkinReward any, existingAccountJSON string) (string, error) {
	data, ok := payload["data"].(map[string]any)
	if !ok {
		return "", errors.New("new-api self response missing data")
	}
	summary := map[string]any{
		"display_name": data["display_name"],
		"group":        data["group"],
		"id":           data["id"],
		"quota":        data["quota"],
		"role":         data["role"],
		"status":       data["status"],
		"used_quota":   data["used_quota"],
		"username":     data["username"],
	}
	if !lastCheckinAt.IsZero() {
		summary["last_checkin_at"] = lastCheckinAt.UTC().Format(time.RFC3339)
	}
	if checkinReward != nil {
		summary["last_checkin_reward"] = checkinReward
	} else if existing := decodeJSONMap(existingAccountJSON); existing != nil {
		if v, ok := existing["last_checkin_reward"]; ok && v != nil {
			summary["last_checkin_reward"] = v
		}
	}
	if statusData, ok := statusPayload["data"].(map[string]any); ok {
		for _, key := range []string{"custom_currency_exchange_rate", "custom_currency_symbol", "quota_display_type", "quota_per_unit"} {
			if value, ok := statusData[key]; ok {
				summary[key] = value
			}
		}
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func consoleCheckinReward(payload map[string]any) any {
	if payload == nil {
		return nil
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		return nil
	}
	for _, key := range []string{"quota", "reward"} {
		if value, ok := data[key]; ok && value != nil {
			return value
		}
	}
	return nil
}

func consoleLastCheckinStatus(backend domain.Backend, now time.Time) (time.Time, bool) {
	payload := decodeJSONMap(backend.ConsoleAccountJSON)
	parsed, ok := consoleLastCheckinTime(payload, now)
	if !ok {
		return time.Time{}, false
	}
	return parsed, sameConsoleDate(parsed, now)
}

func consoleLastCheckinTime(payload map[string]any, now time.Time) (time.Time, bool) {
	for _, key := range []string{
		"last_checkin_at",
		"last_checkin_time",
		"checkin_time",
		"checkin_at",
		"last_checkin_date",
		"checkin_date",
	} {
		parsed, ok := consoleCheckinTimeValue(payload[key], now)
		if ok {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func consoleCheckinTimeValue(value any, now time.Time) (time.Time, bool) {
	switch value := value.(type) {
	case string:
		return parseConsoleCheckinTime(value, now)
	case float64:
		if value <= 0 {
			return time.Time{}, false
		}
		return time.Unix(int64(value), 0), true
	case int:
		if value <= 0 {
			return time.Time{}, false
		}
		return time.Unix(int64(value), 0), true
	case int64:
		if value <= 0 {
			return time.Time{}, false
		}
		return time.Unix(value, 0), true
	default:
		return time.Time{}, false
	}
}

func parseConsoleCheckinTime(raw string, now time.Time) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006/01/02 15:04:05",
		"2006/01/02 15:04",
	} {
		if parsed, err := time.ParseInLocation(layout, raw, consoleCheckinLocation(now)); err == nil {
			return parsed, true
		}
	}
	for _, layout := range []string{"2006-01-02", "2006/01/02"} {
		if parsed, err := time.ParseInLocation(layout, raw, consoleCheckinLocation(now)); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func consoleCheckinLocation(now time.Time) *time.Location {
	if now.Location() != nil && now.Location() != time.UTC {
		return now.Location()
	}
	if time.Local != nil {
		return time.Local
	}
	return time.UTC
}

func sameConsoleDate(value, now time.Time) bool {
	for _, location := range []*time.Location{consoleCheckinLocation(now), value.Location(), now.Location(), time.UTC} {
		if location == nil {
			continue
		}
		valueInLocation := value.In(location)
		nowInLocation := now.In(location)
		if valueInLocation.Year() == nowInLocation.Year() && valueInLocation.YearDay() == nowInLocation.YearDay() {
			return true
		}
	}
	return false
}

func filterConsolePricingPayload(payload map[string]any, focusPatterns string) map[string]any {
	out := cloneJSONMap(payload)
	if strings.TrimSpace(focusPatterns) == "" {
		return out
	}
	data, ok := out["data"].([]any)
	if !ok {
		return out
	}
	filtered := make([]any, 0, len(data))
	for _, item := range data {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if modelNameMatchesFocusPatterns(fmt.Sprint(record["model_name"]), focusPatterns) {
			filtered = append(filtered, item)
		}
	}
	out["data"] = filtered
	return out
}

func cloneJSONMap(value map[string]any) map[string]any {
	encoded, err := json.Marshal(value)
	if err != nil {
		out := make(map[string]any, len(value))
		for key, item := range value {
			out[key] = item
		}
		return out
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		out = make(map[string]any, len(value))
		for key, item := range value {
			out[key] = item
		}
	}
	return out
}

func modelNameMatchesFocusPatterns(modelName, patterns string) bool {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return false
	}
	for _, pattern := range strings.FieldsFunc(patterns, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t'
	}) {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if pattern == "*" || pattern == modelName {
			return true
		}
		if strings.ContainsAny(pattern, "*?") {
			if ok, err := pathpkg.Match(pattern, modelName); err == nil && ok {
				return true
			}
		}
	}
	return false
}

func consoleAccountID(payload map[string]any) string {
	return service.NewAPIAccountID(payload)
}

func consoleStoredAccountID(backend domain.Backend) string {
	payload := decodeJSONMap(backend.ConsoleAccountJSON)
	return consoleIDValue(payload["id"])
}

func hasNewAPIDirectCheckinCredentials(backend domain.Backend, accountID string) bool {
	return len(newAPIConsoleHeaders(backend)) > 0 && strings.TrimSpace(accountID) != ""
}

func hasNewAPIConsoleLoginCredentials(backend domain.Backend) bool {
	return service.HasNewAPIConsoleLoginCredentials(backend)
}

func hasNewAPIConsoleRefreshCredentials(backend domain.Backend) bool {
	return service.HasNewAPIConsoleRefreshCredentials(backend)
}

func canRecoverNewAPIConsoleCheckinAuth(backend domain.Backend) bool {
	return hasNewAPIConsoleLoginCredentials(backend) || hasNewAPIConsoleRefreshCredentials(backend)
}

func consoleAccountJSONWithUserID(raw string, userID string) (string, error) {
	payload := decodeJSONMap(raw)
	userID = strings.TrimSpace(userID)
	if userID == "" {
		delete(payload, "id")
	} else {
		payload["id"] = userID
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
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
	if len(newAPIConsoleHeaders(backend)) == 0 {
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
	copy.NewAPIRefresh = secretPresenceValue(copy.NewAPIRefresh)
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
