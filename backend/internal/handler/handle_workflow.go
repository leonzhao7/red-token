package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"red-token/internal/config"
	"red-token/internal/domain"
	proxypkg "red-token/internal/proxy"
	"red-token/internal/service"
	"red-token/internal/store"
)

const maxWorkflowDefinitionBytes = 4 << 20

type WorkflowHandler struct {
	store      *store.Store
	cfg        *config.Config
	httpClient *http.Client
}

func NewWorkflowHandler(st *store.Store) *WorkflowHandler {
	return &WorkflowHandler{store: st}
}

func (h *WorkflowHandler) SetConfig(cfg *config.Config) {
	h.cfg = cfg
}

// SetHTTPClient overrides proxy-aware client construction. It is primarily
// useful for tests and controlled embedding environments.
func (h *WorkflowHandler) SetHTTPClient(client *http.Client) {
	h.httpClient = client
}

type workflowView struct {
	ID         string                            `json:"id"`
	Name       string                            `json:"name"`
	Definition service.GeneralWorkflowDefinition `json:"definition"`
	CreatedAt  time.Time                         `json:"created_at"`
	UpdatedAt  time.Time                         `json:"updated_at"`
}

type workflowBackendRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type workflowExecuteRequest struct {
	BackendID int64          `json:"backend_id"`
	Aliases   map[string]any `json:"aliases,omitempty"`
}

type workflowExecuteResponse struct {
	WorkflowID string                     `json:"workflow_id"`
	Backend    workflowBackendRef         `json:"backend"`
	Output     any                        `json:"output"`
	Aliases    map[string]any             `json:"aliases"`
	ExecutedAt time.Time                  `json:"executed_at"`
	Requests   []service.NewAPIRequestLog `json:"requests"`
}

func (h *WorkflowHandler) HandleListWorkflows(w http.ResponseWriter, r *http.Request) {
	page, limit := parsePageQuery(r)
	total, err := h.store.CountHTTPWorkflows(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	records, err := h.store.ListHTTPWorkflowsPage(r.Context(), limit, pageOffset(page, limit))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]workflowView, 0, len(records))
	for _, record := range records {
		view, err := makeWorkflowView(record)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		items = append(items, view)
	}
	writeJSON(w, http.StatusOK, pagedResponse(items, total, page, limit))
}

func (h *WorkflowHandler) HandleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	definition, canonical, err := readWorkflowDefinition(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := h.store.GetHTTPWorkflow(r.Context(), definition.ID); err == nil {
		writeError(w, http.StatusConflict, "workflow already exists")
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	record, err := h.store.CreateHTTPWorkflow(r.Context(), definition.ID, definition.Name, canonical)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	view, err := makeWorkflowView(record)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (h *WorkflowHandler) HandleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	record, err := h.store.GetHTTPWorkflow(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	view, err := makeWorkflowView(record)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *WorkflowHandler) HandleUpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	definition, canonical, err := readWorkflowDefinition(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if definition.ID != id {
		writeError(w, http.StatusBadRequest, "workflow definition id must match path id")
		return
	}
	record, err := h.store.UpdateHTTPWorkflow(r.Context(), id, definition.Name, canonical)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	view, err := makeWorkflowView(record)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *WorkflowHandler) HandleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.DeleteHTTPWorkflow(r.Context(), id); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

func (h *WorkflowHandler) HandleExecuteWorkflow(w http.ResponseWriter, r *http.Request) {
	var payload workflowExecuteRequest
	if err := decodeStrictWorkflowJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if payload.BackendID <= 0 {
		writeError(w, http.StatusBadRequest, "backend_id must be a positive integer")
		return
	}

	record, err := h.store.GetHTTPWorkflow(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	definition, err := service.ParseGeneralWorkflow(record.Definition)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "stored workflow is invalid: "+err.Error())
		return
	}
	backend, err := h.store.GetBackend(r.Context(), payload.BackendID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "backend not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if strings.TrimSpace(backend.ConsoleURL) == "" {
		writeError(w, http.StatusBadRequest, "backend console_url is required")
		return
	}
	if err := validateWorkflowConsoleURL(backend.ConsoleURL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	client, err := h.httpClientForBackend(backend)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	headers := workflowConsoleHeaders(backend)
	recorder := service.NewNewAPIRequestRecorder()
	engine := service.NewGeneralWorkflow(service.GeneralWorkflowOptions{
		HTTPClient:       client,
		UserAgent:        h.consoleUserAgent(),
		ProtectedHeaders: []string{"authorization", "cookie"},
	})
	result, err := engine.Execute(r.Context(), definition, service.GeneralWorkflowRunOptions{
		BaseURL:        backend.ConsoleURL,
		Headers:        headers,
		InitialAliases: payload.Aliases,
		Runtime: map[string]any{
			"backend_id":   backend.ID,
			"backend_name": backend.Name,
		},
		Recorder:       recorder,
		ValidateOutput: service.ValidateCheckinWorkflowOutput,
	})
	if err != nil {
		writeWorkflowExecutionError(w, http.StatusBadGateway, err.Error(), recorder.Requests)
		return
	}
	outputJSON, err := json.Marshal(result.Output)
	if err != nil {
		writeWorkflowExecutionError(w, http.StatusInternalServerError, err.Error(), recorder.Requests)
		return
	}
	snapshot, err := h.store.UpsertHTTPWorkflowResult(r.Context(), definition.ID, backend.ID, outputJSON)
	if err != nil {
		writeWorkflowExecutionError(w, http.StatusInternalServerError, err.Error(), recorder.Requests)
		return
	}
	writeJSON(w, http.StatusOK, workflowExecuteResponse{
		WorkflowID: definition.ID,
		Backend:    workflowBackendRef{ID: backend.ID, Name: backend.Name},
		Output:     result.Output,
		Aliases:    result.Aliases,
		ExecutedAt: snapshot.ExecutedAt,
		Requests:   recorder.Requests,
	})
}

func (h *WorkflowHandler) HandleGetWorkflowResult(w http.ResponseWriter, r *http.Request) {
	backendID, err := parseID(r.PathValue("backend_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.store.GetHTTPWorkflowResult(r.Context(), r.PathValue("id"), backendID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "workflow result not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func readWorkflowDefinition(w http.ResponseWriter, r *http.Request) (service.GeneralWorkflowDefinition, json.RawMessage, error) {
	defer r.Body.Close()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWorkflowDefinitionBytes))
	if err != nil {
		return service.GeneralWorkflowDefinition{}, nil, fmt.Errorf("read workflow definition: %w", err)
	}
	definition, err := service.ParseGeneralWorkflow(body)
	if err != nil {
		return service.GeneralWorkflowDefinition{}, nil, err
	}
	canonical, err := json.Marshal(definition)
	if err != nil {
		return service.GeneralWorkflowDefinition{}, nil, fmt.Errorf("encode workflow definition: %w", err)
	}
	return definition, canonical, nil
}

func makeWorkflowView(record store.HTTPWorkflow) (workflowView, error) {
	definition, err := service.ParseGeneralWorkflow(record.Definition)
	if err != nil {
		return workflowView{}, fmt.Errorf("stored workflow %q is invalid: %w", record.ID, err)
	}
	return workflowView{
		ID:         record.ID,
		Name:       record.Name,
		Definition: definition,
		CreatedAt:  record.CreatedAt,
		UpdatedAt:  record.UpdatedAt,
	}, nil
}

func decodeStrictWorkflowJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON contains multiple top-level values")
		}
		return err
	}
	return nil
}

func workflowConsoleHeaders(backend domain.Backend) http.Header {
	headers := make(http.Header)
	for key, value := range service.NewAPIConsoleHeaders(backend) {
		headers.Set(key, value)
	}
	if authorization := strings.TrimSpace(backend.ConsoleAuthorization); authorization != "" {
		headers.Set("Authorization", authorization)
	}
	return headers
}

func validateWorkflowConsoleURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid backend console_url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("backend console_url must use http or https")
	}
	if parsed.Host == "" || parsed.User != nil {
		return errors.New("backend console_url must contain a host and no user info")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("backend console_url must not contain query or fragment")
	}
	return nil
}

func (h *WorkflowHandler) httpClientForBackend(backend domain.Backend) (*http.Client, error) {
	if h.httpClient != nil {
		return h.httpClient, nil
	}
	return proxypkg.NewHTTPClientForBackend(backend, 30*time.Second, 30*time.Second)
}

func (h *WorkflowHandler) consoleUserAgent() string {
	if h.cfg == nil || strings.TrimSpace(h.cfg.BackendConsoleUserAgent) == "" {
		return config.DefaultBackendConsoleUserAgent
	}
	return strings.TrimSpace(h.cfg.BackendConsoleUserAgent)
}

func writeWorkflowExecutionError(w http.ResponseWriter, status int, message string, requests []service.NewAPIRequestLog) {
	if requests == nil {
		requests = []service.NewAPIRequestLog{}
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "red_token_error",
		},
		"requests": requests,
	})
}
