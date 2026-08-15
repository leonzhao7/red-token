package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
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

func (h *WorkflowHandler) focusModelPatterns() string {
	if h.cfg == nil {
		return ""
	}
	return h.cfg.FocusModels
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
	WorkflowID string                            `json:"workflow_id"`
	Backend    workflowBackendRef                `json:"backend"`
	Output     any                               `json:"output"`
	Aliases    map[string]any                    `json:"aliases"`
	ExecutedAt time.Time                         `json:"executed_at"`
	Requests   []service.NewAPIRequestLog        `json:"requests"`
	DebugLogs  []service.GeneralWorkflowDebugLog `json:"debug_logs"`
}

type workflowDebugLogCollector struct {
	Logs     []service.GeneralWorkflowDebugLog
	OnRecord func(service.GeneralWorkflowDebugLog)
}

func (c *workflowDebugLogCollector) Record(log service.GeneralWorkflowDebugLog) {
	if c == nil {
		return
	}
	c.Logs = append(c.Logs, log)
	if c.OnRecord != nil {
		c.OnRecord(log)
	}
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
	backend, err := h.store.GetBackend(r.Context(), payload.BackendID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "backend not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	outcome, err := h.runCheckinWorkflow(r.Context(), backend, r.PathValue("id"), payload.Aliases, nil, nil)
	if err != nil {
		status := http.StatusBadGateway
		var runErr *workflowRunError
		if errors.As(err, &runErr) {
			status = runErr.status
		}
		writeWorkflowExecutionError(w, status, err.Error(), outcome.Requests, outcome.DebugLogs)
		return
	}
	writeJSON(w, http.StatusOK, workflowExecuteResponse{
		WorkflowID: outcome.WorkflowID,
		Backend:    workflowBackendRef{ID: backend.ID, Name: backend.Name},
		Output:     outcome.Output,
		Aliases:    outcome.Aliases,
		ExecutedAt: outcome.ExecutedAt,
		Requests:   outcome.Requests,
		DebugLogs:  outcome.DebugLogs,
	})
}

type checkinWorkflowOutcome struct {
	WorkflowID string
	Backend    domain.Backend
	Output     any
	Aliases    map[string]any
	ExecutedAt time.Time
	Requests   []service.NewAPIRequestLog
	DebugLogs  []service.GeneralWorkflowDebugLog
}

type workflowRunError struct {
	status  int
	message string
}

func (e *workflowRunError) Error() string { return e.message }

// runCheckinWorkflow executes a stored workflow against a backend and applies
// its validated output to the backend, persisting both the output snapshot and
// the updated backend runtime fields.
func (h *WorkflowHandler) runCheckinWorkflow(ctx context.Context, backend domain.Backend, workflowID string, aliases map[string]any, recorder *service.NewAPIRequestRecorder, debugLogs *workflowDebugLogCollector) (checkinWorkflowOutcome, error) {
	record, err := h.store.GetHTTPWorkflow(ctx, workflowID)
	if errors.Is(err, sql.ErrNoRows) {
		return checkinWorkflowOutcome{}, &workflowRunError{status: http.StatusNotFound, message: "workflow not found"}
	}
	if err != nil {
		return checkinWorkflowOutcome{}, err
	}
	definition, err := service.ParseGeneralWorkflow(record.Definition)
	if err != nil {
		return checkinWorkflowOutcome{}, &workflowRunError{status: http.StatusInternalServerError, message: "stored workflow is invalid: " + err.Error()}
	}
	if strings.TrimSpace(backend.ConsoleURL) == "" {
		return checkinWorkflowOutcome{}, &workflowRunError{status: http.StatusBadRequest, message: "backend console_url is required"}
	}
	if err := validateWorkflowConsoleURL(backend.ConsoleURL); err != nil {
		return checkinWorkflowOutcome{}, &workflowRunError{status: http.StatusBadRequest, message: err.Error()}
	}
	client, err := h.httpClientForBackend(backend)
	if err != nil {
		return checkinWorkflowOutcome{}, &workflowRunError{status: http.StatusBadRequest, message: err.Error()}
	}
	headers := workflowConsoleHeaders(backend)
	if recorder == nil {
		recorder = service.NewNewAPIRequestRecorder()
	}
	if debugLogs == nil {
		debugLogs = &workflowDebugLogCollector{Logs: []service.GeneralWorkflowDebugLog{}}
	}
	engine := service.NewGeneralWorkflow(service.GeneralWorkflowOptions{
		HTTPClient:       client,
		UserAgent:        h.consoleUserAgent(),
		ProtectedHeaders: []string{"authorization", "cookie"},
	})
	result, err := engine.Execute(ctx, definition, service.GeneralWorkflowRunOptions{
		BaseURL:        backend.ConsoleURL,
		Headers:        headers,
		InitialAliases: aliases,
		Runtime: map[string]any{
			"backend_id":   backend.ID,
			"backend_name": backend.Name,
		},
		Recorder:       recorder,
		DebugLog:       debugLogs.Record,
		ValidateOutput: service.ValidateCheckinWorkflowOutput,
	})
	if err != nil {
		return checkinWorkflowOutcome{Requests: recorder.Requests, DebugLogs: debugLogs.Logs}, &workflowRunError{status: http.StatusBadGateway, message: err.Error()}
	}
	preserveWorkflowTodayReward(result.Output, backend.ConsoleAccountJSON)
	outputJSON, err := json.Marshal(result.Output)
	if err != nil {
		return checkinWorkflowOutcome{Requests: recorder.Requests, DebugLogs: debugLogs.Logs}, &workflowRunError{status: http.StatusInternalServerError, message: err.Error()}
	}
	updatedBackend, err := applyWorkflowOutputToBackend(backend, result.Output, h.focusModelPatterns())
	if err != nil {
		return checkinWorkflowOutcome{Requests: recorder.Requests, DebugLogs: debugLogs.Logs}, &workflowRunError{status: http.StatusInternalServerError, message: err.Error()}
	}
	storedBackend, snapshot, err := h.store.ApplyHTTPWorkflowResult(ctx, definition.ID, updatedBackend, outputJSON)
	if err != nil {
		return checkinWorkflowOutcome{Requests: recorder.Requests, DebugLogs: debugLogs.Logs}, &workflowRunError{status: http.StatusInternalServerError, message: err.Error()}
	}
	return checkinWorkflowOutcome{
		WorkflowID: definition.ID,
		Backend:    storedBackend,
		Output:     result.Output,
		Aliases:    result.Aliases,
		ExecutedAt: snapshot.ExecutedAt,
		Requests:   recorder.Requests,
		DebugLogs:  debugLogs.Logs,
	}, nil
}

func preserveWorkflowTodayReward(value any, existingAccountJSON string) {
	output, ok := value.(map[string]any)
	if !ok {
		return
	}
	todayReward, ok := workflowOutputNumber(output["today_reward"])
	if !ok || todayReward != 0 {
		return
	}
	account := decodeJSONMap(existingAccountJSON)
	var previousZero float64
	hasPreviousZero := false
	for _, field := range []string{"today_reward", "last_checkin_reward"} {
		previous, ok := workflowOutputNumber(account[field])
		if !ok {
			continue
		}
		if previous != 0 {
			output["today_reward"] = previous
			return
		}
		if !hasPreviousZero {
			previousZero = previous
			hasPreviousZero = true
		}
	}
	if hasPreviousZero {
		output["today_reward"] = previousZero
	}
}

func applyWorkflowOutputToBackend(backend domain.Backend, value any, focusPatterns string) (domain.Backend, error) {
	output, ok := value.(map[string]any)
	if !ok {
		return domain.Backend{}, errors.New("workflow output must be an object")
	}
	userID, _ := output["user_id"].(string)
	username, _ := output["username"].(string)
	quotaUnit, _ := output["quota_unit"].(string)
	quota, ok := workflowOutputNumber(output["quota"])
	if !ok {
		return domain.Backend{}, errors.New("workflow output quota is not a number")
	}
	usedQuota, ok := workflowOutputNumber(output["used_quota"])
	if !ok {
		return domain.Backend{}, errors.New("workflow output used_quota is not a number")
	}
	todayReward, ok := workflowOutputNumber(output["today_reward"])
	if !ok {
		return domain.Backend{}, errors.New("workflow output today_reward is not a number")
	}

	executedAt := time.Now().UTC().Format(time.RFC3339Nano)
	account := decodeJSONMap(backend.ConsoleAccountJSON)
	account["id"] = userID
	account["username"] = username
	if strings.TrimSpace(fmt.Sprint(account["email"])) == "" || fmt.Sprint(account["email"]) == "<nil>" {
		account["email"] = username
	}
	delete(account, "balance")
	delete(account, "total_actual_cost")
	delete(account, "last_checkin_reward")
	account["quota"] = quota
	account["quota_unit"] = quotaUnit
	account["used_quota"] = usedQuota
	account["today_reward"] = todayReward
	account["last_checkin_at"] = executedAt
	account["last_workflow_at"] = executedAt
	accountJSON, err := json.Marshal(account)
	if err != nil {
		return domain.Backend{}, err
	}

	pricingJSON, err := workflowOutputPricingJSON(output["models"], focusPatterns)
	if err != nil {
		return domain.Backend{}, err
	}
	apiKeys, err := workflowOutputAPIKeys(output["api_keys"], backend.APIKeys)
	if err != nil {
		return domain.Backend{}, err
	}
	backend.APIKeys = apiKeys
	backend.ConsoleAccountJSON = string(accountJSON)
	backend.ConsolePricingJSON = pricingJSON
	return backend, nil
}

func workflowOutputAPIKeys(value any, existing []domain.BackendAPIKey) ([]domain.BackendAPIKey, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, errors.New("workflow output api_keys must be an array")
	}
	existingByAPIKey := make(map[string]domain.BackendAPIKey, len(existing))
	for _, apiKey := range existing {
		existingByAPIKey[apiKey.APIKey] = apiKey
	}
	keys := make([]domain.BackendAPIKey, 0, len(items))
	for index, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("workflow output api_keys[%d] must be an object", index)
		}
		id, _ := object["id"].(string)
		key, _ := object["key"].(string)
		name, _ := object["name"].(string)
		group, _ := object["group"].(string)
		if strings.TrimSpace(group) == "" {
			group = "default"
		}
		if strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("workflow output api_keys[%d].key is required", index)
		}
		usedQuota, ok := workflowOutputNonNegativeInt64(object["used_quota"])
		if !ok {
			return nil, fmt.Errorf("workflow output api_keys[%d].used_quota must be a non-negative safe integer", index)
		}
		models := []string{}
		modelMapping := map[string]string{}
		if previous, exists := existingByAPIKey[key]; exists {
			if strings.TrimSpace(id) == "" {
				id = previous.ID
			}
			models = append([]string(nil), previous.Models...)
			modelMapping = copyStringMap(previous.ModelMapping)
		}
		keys = append(keys, domain.BackendAPIKey{
			ID:           id,
			APIKey:       key,
			Name:         name,
			Group:        group,
			Models:       append([]string(nil), models...),
			ModelMapping: modelMapping,
			UsedQuota:    usedQuota,
		})
	}
	return keys, nil
}

func copyStringMap(values map[string]string) map[string]string {
	copy := make(map[string]string, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}

func workflowOutputPricingJSON(value any, focusPatterns string) (string, error) {
	items, ok := value.([]any)
	if !ok {
		return "", errors.New("workflow output models must be an array")
	}
	models := make([]any, 0, len(items))
	for index, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return "", fmt.Errorf("workflow output models[%d] must be an object", index)
		}
		name, _ := object["name"].(string)
		if strings.TrimSpace(focusPatterns) != "" && !modelNameMatchesFocusPatterns(name, focusPatterns) {
			continue
		}
		groups, ok := object["cheapest_groups"].([]any)
		if !ok {
			return "", fmt.Errorf("workflow output models[%d].cheapest_groups must be an array", index)
		}
		inPrice, ok := workflowOutputNumber(object["in_price"])
		if !ok {
			return "", fmt.Errorf("workflow output models[%d].in_price is not a number", index)
		}
		outPrice, ok := workflowOutputNumber(object["out_price"])
		if !ok {
			return "", fmt.Errorf("workflow output models[%d].out_price is not a number", index)
		}
		priceTypeValue, ok := workflowOutputNumber(object["price_type"])
		if !ok || math.Trunc(priceTypeValue) != priceTypeValue || priceTypeValue < 0 || priceTypeValue > 1 {
			return "", fmt.Errorf("workflow output models[%d].price_type must be 0 or 1", index)
		}
		priceType := int(priceTypeValue)
		groupNames := make([]any, 0, len(groups))
		for groupIndex, group := range groups {
			groupName, ok := group.(string)
			if !ok {
				return "", fmt.Errorf("workflow output models[%d].cheapest_groups[%d] must be a string", index, groupIndex)
			}
			groupNames = append(groupNames, groupName)
		}
		model := map[string]any{
			"model_name":    name,
			"enable_groups": groupNames,
			"input_price":   inPrice,
			"output_price":  outPrice,
			"price_type":    priceType,
			"quota_type":    priceType,
			"billing_mode":  "token",
		}
		if priceType == 1 {
			model["model_price"] = inPrice
			model["billing_mode"] = "fixed"
		}
		models = append(models, model)
	}
	pricing := map[string]any{"code": 0, "message": "workflow", "data": models}
	encoded, err := json.Marshal(pricing)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func workflowOutputNumber(value any) (float64, bool) {
	switch value := value.(type) {
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case float64:
		return value, !math.IsNaN(value) && !math.IsInf(value, 0)
	case json.Number:
		parsed, err := value.Float64()
		return parsed, err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0)
	default:
		return 0, false
	}
}

func workflowOutputNonNegativeInt64(value any) (int64, bool) {
	number, ok := workflowOutputNumber(value)
	if !ok || number < 0 || math.Trunc(number) != number || number > 1<<53-1 {
		return 0, false
	}
	return int64(number), true
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
	if err := service.ValidateCheckinWorkflowOutputTemplate(definition.Output); err != nil {
		return service.GeneralWorkflowDefinition{}, nil, fmt.Errorf("validate workflow output: %w", err)
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
	if backend.BackendType == domain.BackendTypeNewAPI && headers.Get("New-Api-User") == "" {
		if accountID := consoleStoredAccountID(backend); accountID != "" {
			headers.Set("New-Api-User", accountID)
		}
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

func writeWorkflowExecutionError(w http.ResponseWriter, status int, message string, requests []service.NewAPIRequestLog, debugLogs []service.GeneralWorkflowDebugLog) {
	if requests == nil {
		requests = []service.NewAPIRequestLog{}
	}
	if debugLogs == nil {
		debugLogs = []service.GeneralWorkflowDebugLog{}
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "red_token_error",
		},
		"requests":   requests,
		"debug_logs": debugLogs,
	})
}
