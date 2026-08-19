package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"red-token/internal/config"
	"red-token/internal/domain"
	"red-token/internal/handler"
	"red-token/internal/proxy"
	"red-token/internal/scheduler"
	"red-token/internal/store"
)

type ctxKey string

const clientKeyContextKey ctxKey = "client-key"
const requestIDContextKey ctxKey = "request-id"

type App struct {
	cfg               config.Config
	store             *store.Store
	backendHandler    *handler.BackendHandler
	workflowHandler   *handler.WorkflowHandler
	usageLogHandler   *handler.UsageLogHandler
	eventHandler      *handler.EventHandler
	clientKeyHandler  *handler.ClientKeyHandler
	socksProxyHandler *handler.ProxyHandler
	settingHandler    *handler.SettingHandler
	dashboardHandler  *handler.DashboardHandler
	scheduler         *scheduler.Service
	proxy             *proxy.Service
	mux               *http.ServeMux
	logger            *slog.Logger
}

type pagedListResponse struct {
	Items []any `json:"items"`
	Total int   `json:"total"`
	Page  int   `json:"page"`
	Limit int   `json:"limit"`
}

type searchResponse struct {
	Query   string                `json:"query"`
	Results searchResultsResponse `json:"results"`
}

type searchResultsResponse struct {
	Backends   []searchResultItem `json:"backends"`
	ClientKeys []searchResultItem `json:"client_keys"`
	Proxies    []searchResultItem `json:"proxies"`
	UsageLogs  []searchResultItem `json:"usage_logs"`
	Events     []searchResultItem `json:"events"`
}

type searchResultItem struct {
	Kind       string         `json:"kind"`
	ID         int64          `json:"id"`
	Title      string         `json:"title"`
	Subtitle   string         `json:"subtitle"`
	Meta       map[string]any `json:"meta"`
	Status     string         `json:"status"`
	TargetPage string         `json:"target_page"`
	TargetID   int64          `json:"target_id"`
}

type detailPlaceholderResponse struct {
	Overview      detailOverviewPlaceholder      `json:"overview"`
	Configuration detailConfigurationPlaceholder `json:"configuration"`
	Metadata      detailMetadataPlaceholder      `json:"metadata"`
	Raw           detailRawPlaceholder           `json:"raw"`
	Activity      detailActivityPlaceholder      `json:"activity"`
}

type detailOverviewPlaceholder struct{}

type detailConfigurationPlaceholder struct{}

type detailMetadataPlaceholder struct {
	ID int64 `json:"id"`
}

type detailRawPlaceholder struct{}

type detailActivityPlaceholder struct {
	Usage    []domain.UsageLog   `json:"usage"`
	Events   []domain.AuditEvent `json:"events"`
	Backends []domain.Backend    `json:"backends"`
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

func New(ctx context.Context, dbPath string) (*App, error) {
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		return nil, err
	}

	cfg, err := config.LoadDatabase(ctx, st)
	if err != nil {
		return nil, err
	}

	app := &App{
		cfg:               cfg,
		store:             st,
		backendHandler:    handler.NewBackendHandler(st),
		workflowHandler:   handler.NewWorkflowHandler(st),
		usageLogHandler:   handler.NewUsageLogHandler(st),
		eventHandler:      handler.NewEventHandler(st),
		clientKeyHandler:  handler.NewClientKeyHandler(st),
		socksProxyHandler: handler.NewProxyHandler(st),
		scheduler:         scheduler.New(st, cfg.BackendCooldown, cfg.BackendFails),
		proxy:             proxy.New(cfg.RequestTimeout),
		mux:               http.NewServeMux(),
		logger:            slog.Default().With("component", "app"),
	}
	app.backendHandler.SetConfig(&app.cfg)
	app.backendHandler.SetWorkflowHandler(app.workflowHandler)
	app.workflowHandler.SetConfig(&app.cfg)
	app.settingHandler = handler.NewSettingHandler(st, &app.cfg)
	app.dashboardHandler = handler.NewDashboardHandler(st, app.backendHandler)
	app.routes()
	return app, nil
}

func (a *App) Close() error {
	return a.store.Close()
}

func (a *App) Handler() http.Handler {
	return a.accessLog(a.mux)
}

func (a *App) StartBackground(ctx context.Context) {
	// Intentionally empty. Red Token does not automatically probe upstream
	// backends; runtime state is derived from real proxied requests only.
}

func (a *App) routes() {
	a.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	a.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/admin/", http.StatusTemporaryRedirect)
	})

	a.mux.Handle("GET /v1/models", a.clientAuth(http.HandlerFunc(a.handlePublicModels)))
	a.mux.Handle("/v1/", a.clientAuth(http.HandlerFunc(a.handleProxy)))

	a.mux.Handle("GET /admin/api/overview", http.HandlerFunc(a.dashboardHandler.HandleOverview))
	a.mux.Handle("GET /admin/api/dashboard/summary", http.HandlerFunc(a.dashboardHandler.HandleDashboardSummary))
	a.mux.Handle("GET /admin/api/dashboard/usage", http.HandlerFunc(a.usageLogHandler.HandleDashboardUsage))
	a.mux.Handle("GET /admin/api/dashboard/activity", http.HandlerFunc(a.dashboardHandler.HandleDashboardActivity))
	a.mux.Handle("GET /admin/api/search", http.HandlerFunc(a.handleSearch))
	a.mux.Handle("GET /admin/api/socks-proxies", http.HandlerFunc(a.socksProxyHandler.HandleListSocksProxies))
	a.mux.Handle("GET /admin/api/socks-proxies/{id}/detail", http.HandlerFunc(a.socksProxyHandler.HandleSocksProxyDetail))
	a.mux.Handle("POST /admin/api/socks-proxies", http.HandlerFunc(a.socksProxyHandler.HandleCreateSocksProxy))
	a.mux.Handle("PUT /admin/api/socks-proxies/{id}", http.HandlerFunc(a.socksProxyHandler.HandleUpdateSocksProxy))
	a.mux.Handle("DELETE /admin/api/socks-proxies/{id}", http.HandlerFunc(a.socksProxyHandler.HandleDeleteSocksProxy))
	a.mux.Handle("GET /admin/api/backends", http.HandlerFunc(a.backendHandler.HandleListBackends))
	a.mux.Handle("GET /admin/api/backends/export", http.HandlerFunc(a.backendHandler.HandleExportBackends))
	a.mux.Handle("GET /admin/api/backends/{id}/detail", http.HandlerFunc(a.backendHandler.HandleBackendDetail))
	a.mux.Handle("POST /admin/api/backends", http.HandlerFunc(a.backendHandler.HandleCreateBackend))
	a.mux.Handle("POST /admin/api/backends/console/sync-summary", http.HandlerFunc(a.backendHandler.HandleBackendConsoleSyncSummary))
	a.mux.Handle("POST /admin/api/backends/{id}/console/sync", http.HandlerFunc(a.backendHandler.HandleBackendConsoleSync))
	a.mux.Handle("POST /admin/api/backends/{id}/console/cookie/sync", http.HandlerFunc(a.backendHandler.HandleBackendConsoleCookieSync))
	a.mux.Handle("POST /admin/api/backends/{id}/console/checkin", http.HandlerFunc(a.backendHandler.HandleBackendConsoleCheckin))
	a.mux.Handle("POST /admin/api/backends/{id}/console/pricing", http.HandlerFunc(a.backendHandler.HandleBackendConsolePricing))
	a.mux.Handle("POST /admin/api/backends/import", http.HandlerFunc(a.backendHandler.HandleImportBackends))
	a.mux.Handle("PUT /admin/api/backends/{id}", http.HandlerFunc(a.backendHandler.HandleUpdateBackend))
	a.mux.Handle("DELETE /admin/api/backends/{id}", http.HandlerFunc(a.backendHandler.HandleDeleteBackend))
	a.mux.Handle("GET /admin/api/workflows", http.HandlerFunc(a.workflowHandler.HandleListWorkflows))
	a.mux.Handle("POST /admin/api/workflows", http.HandlerFunc(a.workflowHandler.HandleCreateWorkflow))
	a.mux.Handle("GET /admin/api/workflows/{id}", http.HandlerFunc(a.workflowHandler.HandleGetWorkflow))
	a.mux.Handle("PUT /admin/api/workflows/{id}", http.HandlerFunc(a.workflowHandler.HandleUpdateWorkflow))
	a.mux.Handle("DELETE /admin/api/workflows/{id}", http.HandlerFunc(a.workflowHandler.HandleDeleteWorkflow))
	a.mux.Handle("POST /admin/api/workflows/{id}/execute", http.HandlerFunc(a.workflowHandler.HandleExecuteWorkflow))
	a.mux.Handle("GET /admin/api/workflows/{id}/results/{backend_id}", http.HandlerFunc(a.workflowHandler.HandleGetWorkflowResult))
	a.mux.Handle("GET /admin/api/client-keys", http.HandlerFunc(a.clientKeyHandler.HandleListClientKeys))
	a.mux.Handle("GET /admin/api/client-keys/{id}/detail", http.HandlerFunc(a.clientKeyHandler.HandleClientKeyDetail))
	a.mux.Handle("POST /admin/api/client-keys", http.HandlerFunc(a.clientKeyHandler.HandleCreateClientKey))
	a.mux.Handle("PUT /admin/api/client-keys/{id}", http.HandlerFunc(a.clientKeyHandler.HandleUpdateClientKey))
	a.mux.Handle("DELETE /admin/api/client-keys/{id}", http.HandlerFunc(a.clientKeyHandler.HandleDeleteClientKey))
	a.mux.Handle("GET /admin/api/events", http.HandlerFunc(a.eventHandler.HandleListEvents))
	a.mux.Handle("GET /admin/api/events/summary", http.HandlerFunc(a.eventHandler.HandleEventSummary))
	a.mux.Handle("GET /admin/api/events/{id}", http.HandlerFunc(a.eventHandler.HandleEventDetail))
	a.mux.Handle("DELETE /admin/api/events", http.HandlerFunc(a.eventHandler.HandleClearEvents))
	a.mux.Handle("GET /admin/api/usage-logs", http.HandlerFunc(a.usageLogHandler.HandleListUsageLogs))
	a.mux.Handle("GET /admin/api/usage-logs/stats", http.HandlerFunc(a.usageLogHandler.HandleUsageLogStats))
	a.mux.Handle("GET /admin/api/backend-hourly-model-stats", http.HandlerFunc(a.backendHandler.HandleBackendHourlyModelStats))
	a.mux.Handle("GET /admin/api/usage-logs/{id}", http.HandlerFunc(a.usageLogHandler.HandleGetUsageLog))
	a.mux.Handle("GET /admin/api/usage-log-options", http.HandlerFunc(a.usageLogHandler.HandleUsageLogOptions))
	a.mux.Handle("DELETE /admin/api/usage-logs", http.HandlerFunc(a.usageLogHandler.HandleClearUsageLogs))
	a.mux.Handle("GET /admin/api/config", http.HandlerFunc(a.settingHandler.HandleGetConfig))
	a.mux.Handle("PUT /admin/api/config", http.HandlerFunc(a.settingHandler.HandleUpdateConfig))
	a.mux.Handle("POST /admin/api/config/reload", http.HandlerFunc(a.settingHandler.HandleReloadConfig))
}

func (a *App) handlePublicModels(w http.ResponseWriter, r *http.Request) {
	backends, err := a.store.ListBackends(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	models := make(map[string]struct{})
	for _, backend := range backends {
		if backend.Status != domain.BackendStatusNormal {
			continue
		}
		for _, apiKey := range backend.APIKeys {
			mappedUpstreamModels := make(map[string]struct{}, len(apiKey.ModelMapping))
			for clientModel, upstreamModel := range apiKey.ModelMapping {
				clientModel = strings.TrimSpace(clientModel)
				upstreamModel = strings.TrimSpace(upstreamModel)
				if clientModel == "" || upstreamModel == "" {
					continue
				}
				mappedUpstreamModels[upstreamModel] = struct{}{}
				models[clientModel] = struct{}{}
			}
			for _, model := range apiKey.Models {
				model = strings.TrimSpace(model)
				if strings.ContainsAny(model, "*?") {
					continue
				}
				if _, mapped := mappedUpstreamModels[model]; mapped {
					continue
				}
				models[model] = struct{}{}
			}
		}
	}

	type modelItem struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		OwnedBy string `json:"owned_by"`
	}
	var data []modelItem
	for model := range models {
		data = append(data, modelItem{ID: model, Object: "model", OwnedBy: "red-token"})
	}
	if data == nil {
		data = []modelItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   data,
	})
}

func (a *App) handleProxy(w http.ResponseWriter, r *http.Request) {
	client, ok := a.clientFromContext(r.Context())
	if !ok {
		a.logEvent(r.Context(), slog.LevelWarn, "proxy_request_rejected",
			slog.String("reason", "missing_client_context"),
		)
		writeError(w, http.StatusUnauthorized, "missing client context")
		return
	}

	startedAt := time.Now()
	usageLog := domain.UsageLog{
		RequestID:          requestIDFromContext(r.Context()),
		TraceID:            requestIDFromContext(r.Context()),
		ClientID:           client.ID,
		ClientName:         client.Name,
		ClientTokenPrefix:  client.TokenPrefix,
		Method:             r.Method,
		Path:               r.URL.Path,
		Query:              r.URL.RawQuery,
		ClientIP:           clientIP(r),
		UserAgent:          r.UserAgent(),
		RequestHeadersJSON: handler.MarshalHeaders(handler.RedactedHeaders(r.Header)),
		CreatedAt:          startedAt.UTC(),
	}
	usageLogStartedAt := startedAt
	skipUsageLog := false
	defer func() {
		usageLog.DurationMS = time.Since(usageLogStartedAt).Milliseconds()
		logCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
		defer cancel()
		if skipUsageLog {
			_ = a.store.RecordClientKeyUsage(logCtx, usageLog)
			return
		}
		_ = a.store.AppendFinalUsageLog(logCtx, usageLog)
	}()

	endpoint := proxy.EndpointForPath(r.URL.Path)
	usageLog.Endpoint = endpoint
	if endpoint == "" || endpoint == domain.EndpointModels {
		usageLog.StatusCode = http.StatusNotFound
		usageLog.ErrorMessage = "unsupported endpoint"
		a.logEvent(r.Context(), slog.LevelWarn, "proxy_request_rejected", append(clientAttrs(client),
			slog.String("reason", "unsupported_endpoint"),
			slog.String("path", r.URL.Path),
		)...)
		writeError(w, http.StatusNotFound, "unsupported endpoint")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		usageLog.StatusCode = http.StatusBadRequest
		usageLog.ErrorMessage = err.Error()
		a.logEvent(r.Context(), slog.LevelWarn, "proxy_request_rejected", append(clientAttrs(client),
			slog.String("reason", "read_body_failed"),
			slog.String("error", err.Error()),
		)...)
		writeError(w, http.StatusBadRequest, "read request body failed")
		return
	}
	_ = r.Body.Close()

	model, err := proxy.ExtractModel(body)
	if err != nil {
		usageLog.StatusCode = http.StatusBadRequest
		usageLog.ErrorMessage = err.Error()
		a.logEvent(r.Context(), slog.LevelWarn, "proxy_request_rejected", append(clientAttrs(client),
			slog.String("reason", "invalid_request_body"),
			slog.String("endpoint", endpoint),
			slog.Int("body_bytes", len(body)),
			slog.String("error", err.Error()),
		)...)
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	a.logEvent(r.Context(), slog.LevelInfo, "proxy_request_received", append(clientAttrs(client),
		slog.String("endpoint", endpoint),
		slog.String("model", model),
		slog.Int("body_bytes", len(body)),
	)...)
	usageLog.Model = model
	usageLog.RequestBytes = int64(len(body))
	usageLog.RequestBodyPreview, usageLog.PreviewTruncated = handler.PreviewText(body, 16*1024)

	selection, err := a.scheduler.SelectBackend(r.Context(), endpoint, model)
	if err != nil {
		if requestContextCanceled(r.Context(), err) {
			a.finishCanceledProxyRequest(w, r, &usageLog, client, endpoint, model, nil, 0, err)
			return
		}
		if errors.Is(err, scheduler.ErrNoBackendAvailable) {
			skipUsageLog = true
		}
		usageLog.StatusCode = http.StatusServiceUnavailable
		usageLog.StatusFamily = handler.StatusFamily(http.StatusServiceUnavailable)
		usageLog.ErrorMessage = err.Error()
		a.logEvent(r.Context(), slog.LevelWarn, "backend_selection_failed", append(clientAttrs(client),
			slog.String("endpoint", endpoint),
			slog.String("model", model),
			slog.String("error", err.Error()),
		)...)
		writeError(w, http.StatusServiceUnavailable, "no backend available")
		return
	}
	a.logEvent(r.Context(), slog.LevelInfo, "backend_selection_completed", append(clientAttrs(client),
		slog.String("endpoint", endpoint),
		slog.String("model", model),
		slog.Int("candidate_count", len(selection.Candidates)),
		slog.Any("candidate_backends", candidateNames(selection.Candidates)),
	)...)

	var (
		lastErr error
	)

	for index, backend := range selection.Candidates {
		attempt := index + 1
		if err := r.Context().Err(); requestContextCanceled(r.Context(), err) {
			a.finishCanceledProxyRequest(w, r, &usageLog, client, endpoint, model, &backend, attempt, err)
			return
		}
		usageLog.Attempts = attempt
		attemptStartedAt := time.Now()
		usageLogStartedAt = attemptStartedAt
		usageLog.CreatedAt = attemptStartedAt.UTC()
		usageLog.BackendID = backend.ID
		usageLog.BackendName = backend.Name
		usageLog.ProxyID = backend.ProxyID
		if backend.Proxy != nil {
			usageLog.ProxyName = backend.Proxy.Name
		} else {
			usageLog.ProxyName = "direct"
		}
		upstreamModel := handler.MappedBackendModel(backend, model)
		requestBody := body
		if upstreamModel != model {
			requestBody, err = proxy.RewriteModel(body, upstreamModel)
			if err != nil {
				usageLog.StatusCode = http.StatusServiceUnavailable
				usageLog.StatusFamily = handler.StatusFamily(http.StatusServiceUnavailable)
				usageLog.ErrorMessage = "rewrite model failed: " + err.Error()
				a.logEvent(r.Context(), slog.LevelWarn, "backend_request_rewrite_failed", append(append(clientAttrs(client),
					backendAttemptAttrs(backend, attempt)...),
					slog.String("endpoint", endpoint),
					slog.String("model", model),
					slog.String("upstream_model", upstreamModel),
					slog.String("error", err.Error()),
				)...)
				lastErr = err
				if index < len(selection.Candidates)-1 {
					a.usageLogHandler.AppendAttemptUsageLog(r.Context(), usageLog, attemptStartedAt)
				}
				continue
			}
		}
		exchange, err := proxy.PrepareExchange(r.URL.Path, backend, requestBody)
		if err != nil {
			if errors.Is(err, proxy.ErrCrossProtocolStreamingNotSupported) {
				a.logEvent(r.Context(), slog.LevelInfo, "backend_request_skipped", append(append(clientAttrs(client),
					backendAttemptAttrs(backend, attempt)...),
					slog.String("endpoint", endpoint),
					slog.String("model", model),
					slog.String("reason", err.Error()),
				)...)
				continue
			}
			usageLog.StatusCode = http.StatusServiceUnavailable
			usageLog.StatusFamily = handler.StatusFamily(http.StatusServiceUnavailable)
			usageLog.ErrorMessage = "prepare exchange failed: " + err.Error()
			a.logEvent(r.Context(), slog.LevelWarn, "backend_request_rewrite_failed", append(append(clientAttrs(client),
				backendAttemptAttrs(backend, attempt)...),
				slog.String("endpoint", endpoint),
				slog.String("model", model),
				slog.String("upstream_model", upstreamModel),
				slog.String("error", err.Error()),
			)...)
			lastErr = err
			if index < len(selection.Candidates)-1 {
				a.usageLogHandler.AppendAttemptUsageLog(r.Context(), usageLog, attemptStartedAt)
			}
			continue
		}
		a.logEvent(r.Context(), slog.LevelInfo, "backend_request_started", append(append(clientAttrs(client),
			backendAttemptAttrs(backend, attempt)...),
			slog.String("endpoint", endpoint),
			slog.String("model", model),
			slog.String("upstream_model", upstreamModel),
			slog.String("method", r.Method),
			slog.String("path", exchange.UpstreamPath),
			slog.String("query", r.URL.RawQuery),
		)...)

		resp, err := a.proxy.DoWithPath(a.withBackendTrace(r.Context(), backend, attempt), r, backend, exchange.RequestBody, exchange.UpstreamPath)
		if err != nil {
			if requestContextCanceled(r.Context(), err) {
				a.finishCanceledProxyRequest(w, r, &usageLog, client, endpoint, model, &backend, attempt, err)
				return
			}
			_ = a.scheduler.MarkFailure(r.Context(), backend.ID, err)
			lastErr = err
			usageLog.StatusCode = http.StatusServiceUnavailable
			usageLog.StatusFamily = handler.StatusFamily(http.StatusServiceUnavailable)
			usageLog.ErrorMessage = err.Error()
			a.logEvent(r.Context(), slog.LevelWarn, "backend_request_failed", append(append(clientAttrs(client),
				backendAttemptAttrs(backend, attempt)...),
				slog.String("endpoint", endpoint),
				slog.String("model", model),
				slog.Duration("duration", time.Since(attemptStartedAt)),
				slog.String("error", err.Error()),
				slog.Bool("will_failover", index < len(selection.Candidates)-1),
			)...)
			if index < len(selection.Candidates)-1 {
				a.usageLogHandler.AppendAttemptUsageLog(r.Context(), usageLog, attemptStartedAt)
				continue
			}
			break
		}
		resp, err = decodeUpstreamResponse(resp)
		if err != nil {
			if requestContextCanceled(r.Context(), err) {
				a.finishCanceledProxyRequest(w, r, &usageLog, client, endpoint, model, &backend, attempt, err)
				return
			}
			_ = a.scheduler.MarkFailure(r.Context(), backend.ID, err)
			lastErr = err
			usageLog.StatusCode = http.StatusServiceUnavailable
			usageLog.StatusFamily = handler.StatusFamily(http.StatusServiceUnavailable)
			usageLog.ErrorMessage = "decode response failed: " + err.Error()
			a.logEvent(r.Context(), slog.LevelWarn, "backend_response_decode_failed", append(append(clientAttrs(client),
				backendAttemptAttrs(backend, attempt)...),
				slog.String("endpoint", endpoint),
				slog.String("model", model),
				slog.String("error", err.Error()),
				slog.Bool("will_failover", index < len(selection.Candidates)-1),
			)...)
			if index < len(selection.Candidates)-1 {
				a.usageLogHandler.AppendAttemptUsageLog(r.Context(), usageLog, attemptStartedAt)
				continue
			}
			break
		}

		forwardBackendResponse := r.URL.Path == "/v1/messages/count_tokens"
		if resp.StatusCode/100 != 2 && !forwardBackendResponse {
			usageLog.StatusCode = resp.StatusCode
			usageLog.StatusFamily = handler.StatusFamily(resp.StatusCode)
			usageLog.ErrorMessage = resp.Status
			bufferedResp, responseBody, responseBytes, responsePreview, truncated, bufferErr := handler.CloneResponseForLogging(resp)
			if bufferErr == nil {
				handler.ApplyResponseLogFields(&usageLog, bufferedResp, responseBody, responseBytes, responsePreview, truncated)
				_ = bufferedResp.Body.Close()
			}
			_ = a.scheduler.MarkFailure(r.Context(), backend.ID, errors.New(resp.Status))
			attrs := append(append(clientAttrs(client), backendAttemptAttrs(backend, attempt)...),
				slog.String("endpoint", endpoint),
				slog.String("model", model),
				slog.Int("status", resp.StatusCode),
				slog.String("status_text", resp.Status),
				slog.String("content_type", resp.Header.Get("Content-Type")),
				slog.Duration("duration", time.Since(attemptStartedAt)),
				slog.Int64("response_bytes", usageLog.ResponseBytes),
				slog.String("response_body_preview", usageLog.ResponseBodyPreview),
				slog.Bool("preview_truncated", usageLog.PreviewTruncated),
				slog.Bool("is_stream", usageLog.IsStream),
				slog.Bool("will_failover", index < len(selection.Candidates)-1),
			)
			if bufferErr != nil {
				attrs = append(attrs, slog.String("response_log_error", bufferErr.Error()))
			}
			a.logEvent(r.Context(), slog.LevelWarn, "backend_response_failed", attrs...)
			lastErr = errors.New(resp.Status)
			if index < len(selection.Candidates)-1 {
				a.usageLogHandler.AppendAttemptUsageLog(r.Context(), usageLog, attemptStartedAt)
				continue
			}
			break
		}

		if resp.StatusCode/100 == 2 {
			_ = a.scheduler.MarkSuccess(r.Context(), backend.ID)
			usageLog.ErrorMessage = ""
		} else {
			_ = a.scheduler.MarkFailure(r.Context(), backend.ID, errors.New(resp.Status))
			usageLog.ErrorMessage = resp.Status
		}
		usageLog.StatusCode = resp.StatusCode
		usageLog.StatusFamily = handler.StatusFamily(resp.StatusCode)
		resp, err = exchange.AdaptResponse(resp)
		if err != nil {
			usageLog.StatusCode = http.StatusServiceUnavailable
			usageLog.StatusFamily = handler.StatusFamily(http.StatusServiceUnavailable)
			usageLog.ErrorMessage = "adapt response failed: " + err.Error()
			a.logEvent(r.Context(), slog.LevelWarn, "backend_response_adapt_failed", append(append(clientAttrs(client),
				backendAttemptAttrs(backend, attempt)...),
				slog.String("endpoint", endpoint),
				slog.String("model", model),
				slog.String("error", err.Error()),
			)...)
			return
		}
		bufferedResp, responseBody, responseBytes, responsePreview, truncated, err := handler.CloneResponseForLogging(resp)
		if err != nil {
			usageLog.StatusCode = http.StatusServiceUnavailable
			usageLog.StatusFamily = handler.StatusFamily(http.StatusServiceUnavailable)
			usageLog.ErrorMessage = err.Error()
			a.logEvent(r.Context(), slog.LevelWarn, "backend_response_buffer_failed", append(append(clientAttrs(client),
				backendAttemptAttrs(backend, attempt)...),
				slog.String("endpoint", endpoint),
				slog.String("model", model),
				slog.String("error", err.Error()),
			)...)
			return
		}
		resp = bufferedResp
		handler.ApplyResponseLogFields(&usageLog, resp, responseBody, responseBytes, responsePreview, truncated)
		if resp.StatusCode == http.StatusOK && usageLog.InputTokens+usageLog.OutputTokens+usageLog.InputCacheTokens == 0 {
			a.logEvent(r.Context(), slog.LevelWarn, "backend_response_zero_tokens", append(append(clientAttrs(client),
				backendAttemptAttrs(backend, attempt)...),
				slog.String("endpoint", endpoint),
				slog.String("model", model),
				slog.Int("status", resp.StatusCode),
				slog.String("content_type", resp.Header.Get("Content-Type")),
				slog.Int64("response_bytes", usageLog.ResponseBytes),
				slog.String("response_headers_json", usageLog.ResponseHeadersJSON),
				slog.String("response_body_preview", usageLog.ResponseBodyPreview),
				slog.Bool("preview_truncated", usageLog.PreviewTruncated),
				slog.Bool("is_stream", usageLog.IsStream),
			)...)
		}

		a.logEvent(r.Context(), slog.LevelInfo, "backend_response_selected", append(append(clientAttrs(client),
			backendAttemptAttrs(backend, attempt)...),
			slog.String("endpoint", endpoint),
			slog.String("model", model),
			slog.Int("status", resp.StatusCode),
			slog.String("status_text", resp.Status),
			slog.String("content_type", resp.Header.Get("Content-Type")),
			slog.Duration("duration", time.Since(attemptStartedAt)),
		)...)
		err = proxy.WriteResponse(w, resp)
		if err != nil {
			usageLog.StatusCode = http.StatusServiceUnavailable
			usageLog.StatusFamily = handler.StatusFamily(http.StatusServiceUnavailable)
			usageLog.ErrorMessage = err.Error()
			a.logEvent(r.Context(), slog.LevelWarn, "client_response_write_failed", append(append(clientAttrs(client),
				backendAttemptAttrs(backend, attempt)...),
				slog.String("endpoint", endpoint),
				slog.String("model", model),
				slog.String("error", err.Error()),
			)...)
			return
		}
		return
	}

	if err := r.Context().Err(); requestContextCanceled(r.Context(), err) {
		a.finishCanceledProxyRequest(w, r, &usageLog, client, endpoint, model, nil, 0, err)
		return
	}
	if lastErr != nil {
		if usageLog.StatusCode == 0 {
			usageLog.StatusCode = http.StatusServiceUnavailable
			usageLog.StatusFamily = handler.StatusFamily(http.StatusServiceUnavailable)
		}
		if usageLog.ErrorMessage == "" {
			usageLog.ErrorMessage = lastErr.Error()
		}
		a.logEvent(r.Context(), slog.LevelWarn, "proxy_request_failed", append(clientAttrs(client),
			slog.String("endpoint", endpoint),
			slog.String("model", model),
			slog.String("error", lastErr.Error()),
		)...)
		writeError(w, http.StatusServiceUnavailable, "no backend available")
		return
	}
	usageLog.StatusCode = http.StatusServiceUnavailable
	usageLog.StatusFamily = handler.StatusFamily(http.StatusServiceUnavailable)
	usageLog.ErrorMessage = "all candidate backends failed"
	a.logEvent(r.Context(), slog.LevelWarn, "proxy_request_failed", append(clientAttrs(client),
		slog.String("endpoint", endpoint),
		slog.String("model", model),
		slog.String("error", "all candidate backends failed"),
	)...)
	writeError(w, http.StatusServiceUnavailable, "no backend available")
}

func requestContextCanceled(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled)
}

func (a *App) finishCanceledProxyRequest(w http.ResponseWriter, r *http.Request, usageLog *domain.UsageLog, client domain.ClientKey, endpoint, model string, backend *domain.Backend, attempt int, err error) {
	usageLog.StatusCode = 499
	usageLog.StatusFamily = handler.StatusFamily(499)
	if err != nil {
		usageLog.ErrorMessage = err.Error()
	} else {
		usageLog.ErrorMessage = context.Canceled.Error()
	}

	attrs := append(clientAttrs(client),
		slog.String("endpoint", endpoint),
		slog.String("model", model),
		slog.String("error", usageLog.ErrorMessage),
	)
	if backend != nil && attempt > 0 {
		attrs = append(attrs, backendAttemptAttrs(*backend, attempt)...)
	}
	a.logEvent(r.Context(), slog.LevelInfo, "proxy_request_canceled", attrs...)
	w.WriteHeader(499)
}

func (a *App) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	results, err := a.store.Search(r.Context(), query, parseLimitQuery(r, 6))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, searchResponse{
		Query: query,
		Results: searchResultsResponse{
			Backends:   toSearchResultItems(results.Backends),
			ClientKeys: toSearchResultItems(results.ClientKeys),
			Proxies:    toSearchResultItems(results.Proxies),
			UsageLogs:  toSearchResultItems(results.UsageLogs),
			Events:     toSearchResultItems(results.Events),
		},
	})
}

func (a *App) clientAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractClientToken(r.Header)
		if token == "" {
			a.logEvent(r.Context(), slog.LevelWarn, "client_auth_failed",
				slog.String("path", r.URL.Path),
				slog.String("method", r.Method),
				slog.String("reason", "missing_api_key"),
			)
			writeError(w, http.StatusUnauthorized, "missing api key")
			return
		}

		client, err := a.store.FindClientKeyByToken(r.Context(), token)
		if err != nil {
			a.logEvent(r.Context(), slog.LevelError, "client_auth_lookup_failed",
				slog.String("path", r.URL.Path),
				slog.String("method", r.Method),
				slog.String("token_prefix", tokenPrefix(token)),
				slog.String("error", err.Error()),
			)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if client == nil {
			a.logEvent(r.Context(), slog.LevelWarn, "client_auth_failed",
				slog.String("path", r.URL.Path),
				slog.String("method", r.Method),
				slog.String("reason", "invalid_api_key"),
				slog.String("token_prefix", tokenPrefix(token)),
			)
			writeError(w, http.StatusUnauthorized, "invalid api key")
			return
		}

		ctx := context.WithValue(r.Context(), clientKeyContextKey, *client)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *App) clientFromContext(ctx context.Context) (domain.ClientKey, bool) {
	value := ctx.Value(clientKeyContextKey)
	client, ok := value.(domain.ClientKey)
	return client, ok
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

func parseID(value string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func extractBearer(value string) string {
	if len(value) >= 7 && strings.EqualFold(value[:7], "Bearer ") {
		return strings.TrimSpace(value[7:])
	}
	return strings.TrimSpace(value)
}

func extractClientToken(header http.Header) string {
	if token := strings.TrimSpace(extractBearer(header.Get("Authorization"))); token != "" {
		return token
	}
	return strings.TrimSpace(header.Get("X-Api-Key"))
}

func tokenPrefix(token string) string {
	token = strings.TrimSpace(token)
	if len(token) <= 8 {
		return token
	}
	return token[:8]
}

func optionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value.UTC()
	return &copy
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

func parseLimitQuery(r *http.Request, fallback int) int {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		return fallback
	}
	return limit
}

func parseTimeQuery(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
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

func toSearchResultItems(items []store.SearchResult) []searchResultItem {
	if items == nil {
		return []searchResultItem{}
	}
	out := make([]searchResultItem, 0, len(items))
	for _, item := range items {
		out = append(out, searchResultItem{
			Kind:       item.Kind,
			ID:         item.ID,
			Title:      item.Title,
			Subtitle:   item.Subtitle,
			Meta:       item.Meta,
			Status:     item.Status,
			TargetPage: item.TargetPage,
			TargetID:   item.TargetID,
		})
	}
	return out
}
