package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"net/textproto"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/itchyny/gojq"

	"red-token/internal/config"
)

const (
	GeneralWorkflowSpec               = "http-workflow/v1"
	defaultGeneralWorkflowBodyLimit   = int64(10 << 20)
	defaultGeneralWorkflowHTTPTimeout = 30 * time.Second
	defaultGeneralWorkflowJQTimeout   = 5 * time.Second
	generalWorkflowDebugPreviewBytes  = 64 << 10
	maxSafeJSONInteger                = int64(1<<53 - 1)
)

var (
	generalWorkflowIDPattern    = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	generalWorkflowAliasPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	generalWorkflowTokenPattern = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")
	generalWorkflowReservedVars = map[string]struct{}{
		"response": {},
		"request":  {},
		"runtime":  {},
		"vars":     {},
	}
	generalWorkflowJQVariables = []string{"$response", "$request", "$vars", "$runtime"}
	generalWorkflowForbiddenJQ = map[string]struct{}{
		"$ENV":       {},
		"debug":      {},
		"env":        {},
		"halt":       {},
		"halt_error": {},
		"input":      {},
		"inputs":     {},
		"now":        {},
		"stderr":     {},
	}
)

// GeneralWorkflowDefinition is the declarative workflow described by
// docs/http_workflow.md. Output and request bodies remain raw so JSON null can
// be distinguished from an omitted field.
type GeneralWorkflowDefinition struct {
	Spec   string                `json:"spec"`
	ID     string                `json:"id"`
	Name   string                `json:"name"`
	Steps  []GeneralWorkflowStep `json:"steps"`
	Output json.RawMessage       `json:"output"`
}

type GeneralWorkflowStep struct {
	ID      string                      `json:"id"`
	Name    string                      `json:"name"`
	Request GeneralWorkflowRequest      `json:"request"`
	Expect  string                      `json:"expect,omitempty"`
	Extract []GeneralWorkflowExtraction `json:"extract,omitempty"`
}

type GeneralWorkflowRequest struct {
	Method  string          `json:"method"`
	Path    string          `json:"path"`
	Query   map[string]any  `json:"query,omitempty"`
	Headers map[string]any  `json:"headers,omitempty"`
	Body    json.RawMessage `json:"body,omitempty"`
}

type GeneralWorkflowExtraction struct {
	Alias      string `json:"alias"`
	Expression string `json:"expression"`
}

type GeneralWorkflowOptions struct {
	HTTPClient       *http.Client
	UserAgent        string
	Now              func() time.Time
	MaxResponseBytes int64
	JQTimeout        time.Duration
	ProtectedHeaders []string
}

type GeneralWorkflowRunOptions struct {
	BaseURL        string
	Headers        http.Header
	InitialAliases map[string]any
	Runtime        map[string]any
	Recorder       ConsoleRequestRecorder
	DebugLog       func(GeneralWorkflowDebugLog)
	ValidateOutput func(any) error
}

type GeneralWorkflowDebugLog struct {
	Time       string         `json:"time"`
	Level      string         `json:"level"`
	StepID     string         `json:"step_id,omitempty"`
	StepName   string         `json:"step_name,omitempty"`
	Phase      string         `json:"phase"`
	Message    string         `json:"message"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

type GeneralWorkflowResult struct {
	Output  any            `json:"output"`
	Aliases map[string]any `json:"aliases"`
}

type GeneralWorkflow struct {
	httpClient       *http.Client
	userAgent        string
	now              func() time.Time
	maxResponseBytes int64
	jqTimeout        time.Duration
	protectedHeaders map[string]struct{}
}

type GeneralWorkflowError struct {
	StepID string
	Phase  string
	Err    error
}

func (e *GeneralWorkflowError) Error() string {
	if e.StepID == "" {
		return fmt.Sprintf("general workflow %s: %v", e.Phase, e.Err)
	}
	return fmt.Sprintf("general workflow step %q %s: %v", e.StepID, e.Phase, e.Err)
}

func (e *GeneralWorkflowError) Unwrap() error { return e.Err }

type compiledGeneralWorkflow struct {
	definition GeneralWorkflowDefinition
	steps      []compiledGeneralWorkflowStep
	output     any
}

type compiledGeneralWorkflowStep struct {
	definition GeneralWorkflowStep
	expect     *gojq.Code
	extract    []compiledGeneralWorkflowExtraction
}

type compiledGeneralWorkflowExtraction struct {
	definition GeneralWorkflowExtraction
	code       *gojq.Code
}

type renderedGeneralWorkflowRequest struct {
	method    string
	path      string
	query     map[string]any
	headers   http.Header
	hasBody   bool
	body      any
	bodyBytes []byte
	jqValue   map[string]any
}

type generalWorkflowHTTPResponse struct {
	body     any
	envelope map[string]any
}

func NewGeneralWorkflow(options GeneralWorkflowOptions) *GeneralWorkflow {
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultGeneralWorkflowHTTPTimeout}
	}
	userAgent := strings.TrimSpace(options.UserAgent)
	if userAgent == "" {
		userAgent = config.DefaultBackendConsoleUserAgent
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	maxResponseBytes := options.MaxResponseBytes
	if maxResponseBytes <= 0 {
		maxResponseBytes = defaultGeneralWorkflowBodyLimit
	}
	jqTimeout := options.JQTimeout
	if jqTimeout <= 0 {
		jqTimeout = defaultGeneralWorkflowJQTimeout
	}
	protectedHeaders := map[string]struct{}{
		"content-length": {},
		"host":           {},
		"user-agent":     {},
	}
	for _, name := range options.ProtectedHeaders {
		if normalized := strings.ToLower(strings.TrimSpace(name)); normalized != "" {
			protectedHeaders[normalized] = struct{}{}
		}
	}
	return &GeneralWorkflow{
		httpClient:       client,
		userAgent:        userAgent,
		now:              now,
		maxResponseBytes: maxResponseBytes,
		jqTimeout:        jqTimeout,
		protectedHeaders: protectedHeaders,
	}
}

// ParseGeneralWorkflow strictly decodes a workflow definition. Unknown fields,
// duplicate JSON keys, unsafe integers, and trailing JSON values are rejected.
func ParseGeneralWorkflow(data []byte) (GeneralWorkflowDefinition, error) {
	raw, err := decodeGeneralWorkflowJSON(data)
	if err != nil {
		return GeneralWorkflowDefinition{}, fmt.Errorf("decode workflow JSON: %w", err)
	}
	if err := validateGeneralWorkflowDefinitionShape(raw); err != nil {
		return GeneralWorkflowDefinition{}, fmt.Errorf("decode workflow definition: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	var definition GeneralWorkflowDefinition
	if err := decoder.Decode(&definition); err != nil {
		return GeneralWorkflowDefinition{}, fmt.Errorf("decode workflow definition: %w", err)
	}
	if err := requireGeneralWorkflowEOF(decoder); err != nil {
		return GeneralWorkflowDefinition{}, fmt.Errorf("decode workflow definition: %w", err)
	}
	if err := ValidateGeneralWorkflow(definition); err != nil {
		return GeneralWorkflowDefinition{}, err
	}
	return definition, nil
}

func validateGeneralWorkflowDefinitionShape(value any) error {
	root, ok := value.(map[string]any)
	if !ok {
		return errors.New("workflow must be an object")
	}
	for _, field := range []string{"spec", "id", "name"} {
		if _, err := requiredGeneralWorkflowString(root, field, "$"+"/"+field); err != nil {
			return err
		}
	}
	stepsValue, exists := root["steps"]
	if !exists {
		return errors.New("$/steps is required")
	}
	steps, ok := stepsValue.([]any)
	if !ok {
		return errors.New("$/steps must be an array")
	}
	if _, exists := root["output"]; !exists {
		return errors.New("$/output is required")
	}
	for stepIndex, stepValue := range steps {
		path := fmt.Sprintf("$/steps/%d", stepIndex)
		step, ok := stepValue.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		for _, field := range []string{"id", "name"} {
			if _, err := requiredGeneralWorkflowString(step, field, path+"/"+field); err != nil {
				return err
			}
		}
		requestValue, exists := step["request"]
		if !exists {
			return fmt.Errorf("%s/request is required", path)
		}
		request, ok := requestValue.(map[string]any)
		if !ok {
			return fmt.Errorf("%s/request must be an object", path)
		}
		for _, field := range []string{"method", "path"} {
			if _, err := requiredGeneralWorkflowString(request, field, path+"/request/"+field); err != nil {
				return err
			}
		}
		for _, field := range []string{"query", "headers"} {
			if item, exists := request[field]; exists {
				if _, ok := item.(map[string]any); !ok {
					return fmt.Errorf("%s/request/%s must be an object", path, field)
				}
			}
		}
		if expect, exists := step["expect"]; exists {
			text, ok := expect.(string)
			if !ok {
				return fmt.Errorf("%s/expect must be a string", path)
			}
			if strings.TrimSpace(text) == "" {
				return fmt.Errorf("%s/expect must not be empty", path)
			}
		}
		extractValue, exists := step["extract"]
		if !exists {
			continue
		}
		extract, ok := extractValue.([]any)
		if !ok {
			return fmt.Errorf("%s/extract must be an array", path)
		}
		for extractIndex, extractionValue := range extract {
			extractPath := fmt.Sprintf("%s/extract/%d", path, extractIndex)
			extraction, ok := extractionValue.(map[string]any)
			if !ok {
				return fmt.Errorf("%s must be an object", extractPath)
			}
			for _, field := range []string{"alias", "expression"} {
				if _, err := requiredGeneralWorkflowString(extraction, field, extractPath+"/"+field); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func requiredGeneralWorkflowString(object map[string]any, field, path string) (string, error) {
	value, exists := object[field]
	if !exists {
		return "", fmt.Errorf("%s is required", path)
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", path)
	}
	return text, nil
}

// ValidateGeneralWorkflow validates the complete definition and compiles every
// jq expression before any HTTP side effect can occur.
func ValidateGeneralWorkflow(definition GeneralWorkflowDefinition) error {
	_, err := compileGeneralWorkflow(definition)
	return err
}

func compileGeneralWorkflow(definition GeneralWorkflowDefinition) (compiledGeneralWorkflow, error) {
	if definition.Spec != GeneralWorkflowSpec {
		return compiledGeneralWorkflow{}, fmt.Errorf("unsupported spec %q", definition.Spec)
	}
	if !generalWorkflowIDPattern.MatchString(definition.ID) {
		return compiledGeneralWorkflow{}, fmt.Errorf("invalid workflow id %q", definition.ID)
	}
	if strings.TrimSpace(definition.Name) == "" {
		return compiledGeneralWorkflow{}, errors.New("workflow name is required")
	}
	if definition.Steps == nil {
		return compiledGeneralWorkflow{}, errors.New("workflow steps are required")
	}
	if len(definition.Output) == 0 {
		return compiledGeneralWorkflow{}, errors.New("workflow output is required")
	}
	output, err := decodeGeneralWorkflowJSON(definition.Output)
	if err != nil {
		return compiledGeneralWorkflow{}, fmt.Errorf("decode workflow output: %w", err)
	}
	if err := validateGeneralWorkflowTemplate(output); err != nil {
		return compiledGeneralWorkflow{}, fmt.Errorf("validate workflow output template: %w", err)
	}

	compiled := compiledGeneralWorkflow{
		definition: definition,
		steps:      make([]compiledGeneralWorkflowStep, 0, len(definition.Steps)),
		output:     output,
	}
	stepIDs := make(map[string]struct{}, len(definition.Steps))
	for index, step := range definition.Steps {
		if !generalWorkflowIDPattern.MatchString(step.ID) {
			return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d]: invalid step id %q", index, step.ID)
		}
		if _, exists := stepIDs[step.ID]; exists {
			return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d]: duplicate step id %q", index, step.ID)
		}
		stepIDs[step.ID] = struct{}{}
		if strings.TrimSpace(step.Name) == "" {
			return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d]: step name is required", index)
		}
		if err := validateGeneralWorkflowRequest(step.Request); err != nil {
			return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] request: %w", index, err)
		}

		expectSource := strings.TrimSpace(step.Expect)
		if expectSource == "" {
			expectSource = "$response.status >= 200 and $response.status < 300"
		}
		expect, err := compileGeneralWorkflowJQ(expectSource)
		if err != nil {
			return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] expect: %w", index, err)
		}
		compiledStep := compiledGeneralWorkflowStep{
			definition: step,
			expect:     expect,
			extract:    make([]compiledGeneralWorkflowExtraction, 0, len(step.Extract)),
		}
		for extractIndex, extraction := range step.Extract {
			if err := validateGeneralWorkflowAlias(extraction.Alias); err != nil {
				return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] extract[%d]: %w", index, extractIndex, err)
			}
			if strings.TrimSpace(extraction.Expression) == "" {
				return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] extract[%d]: expression is required", index, extractIndex)
			}
			code, err := compileGeneralWorkflowJQ(extraction.Expression)
			if err != nil {
				return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] extract[%d]: %w", index, extractIndex, err)
			}
			compiledStep.extract = append(compiledStep.extract, compiledGeneralWorkflowExtraction{
				definition: extraction,
				code:       code,
			})
		}
		compiled.steps = append(compiled.steps, compiledStep)
	}
	return compiled, nil
}

func validateGeneralWorkflowRequest(request GeneralWorkflowRequest) error {
	method := strings.TrimSpace(request.Method)
	if method == "" || !generalWorkflowTokenPattern.MatchString(method) {
		return fmt.Errorf("invalid method %q", request.Method)
	}
	if request.Path == "" {
		return errors.New("path is required")
	}
	if err := validateGeneralWorkflowTemplate(request.Path); err != nil {
		return fmt.Errorf("path template: %w", err)
	}
	if request.Query != nil {
		query, err := canonicalGeneralWorkflowJSON(request.Query, "query")
		if err != nil {
			return err
		}
		if err := validateGeneralWorkflowTemplate(query); err != nil {
			return fmt.Errorf("query template: %w", err)
		}
	}
	if request.Headers != nil {
		headers, err := canonicalGeneralWorkflowJSON(request.Headers, "headers")
		if err != nil {
			return err
		}
		if err := validateGeneralWorkflowTemplate(headers); err != nil {
			return fmt.Errorf("headers template: %w", err)
		}
	}
	if len(request.Body) > 0 {
		body, err := decodeGeneralWorkflowJSON(request.Body)
		if err != nil {
			return fmt.Errorf("body: %w", err)
		}
		if err := validateGeneralWorkflowTemplate(body); err != nil {
			return fmt.Errorf("body template: %w", err)
		}
	}
	return nil
}

func validateGeneralWorkflowAlias(alias string) error {
	if !generalWorkflowAliasPattern.MatchString(alias) {
		return fmt.Errorf("invalid alias %q", alias)
	}
	if _, reserved := generalWorkflowReservedVars[alias]; reserved {
		return fmt.Errorf("alias %q is reserved", alias)
	}
	return nil
}

func compileGeneralWorkflowJQ(source string) (*gojq.Code, error) {
	query, err := gojq.Parse(source)
	if err != nil {
		return nil, fmt.Errorf("parse jq expression: %w", err)
	}
	if err := validateGeneralWorkflowJQ(query); err != nil {
		return nil, err
	}
	code, err := gojq.Compile(query, gojq.WithVariables(generalWorkflowJQVariables))
	if err != nil {
		return nil, fmt.Errorf("compile jq expression: %w", err)
	}
	return code, nil
}

func validateGeneralWorkflowJQ(query *gojq.Query) error {
	if query.Meta != nil || len(query.Imports) > 0 {
		return errors.New("jq modules and imports are not allowed")
	}
	var rejected string
	var visit func(reflect.Value)
	visit = func(value reflect.Value) {
		if rejected != "" || !value.IsValid() {
			return
		}
		if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return
			}
			visit(value.Elem())
			return
		}
		if value.Type() == reflect.TypeOf(gojq.Func{}) {
			name := value.FieldByName("Name").String()
			if _, forbidden := generalWorkflowForbiddenJQ[name]; forbidden {
				rejected = name
				return
			}
		}
		switch value.Kind() {
		case reflect.Struct:
			typeInfo := value.Type()
			for i := 0; i < value.NumField(); i++ {
				if typeInfo.Field(i).PkgPath == "" {
					visit(value.Field(i))
				}
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < value.Len(); i++ {
				visit(value.Index(i))
			}
		case reflect.Map:
			iterator := value.MapRange()
			for iterator.Next() {
				visit(iterator.Key())
				visit(iterator.Value())
			}
		}
	}
	visit(reflect.ValueOf(query))
	if rejected != "" {
		return fmt.Errorf("jq function %q is not allowed", rejected)
	}
	return nil
}

func (workflow *GeneralWorkflow) Execute(ctx context.Context, definition GeneralWorkflowDefinition, options GeneralWorkflowRunOptions) (result GeneralWorkflowResult, resultErr error) {
	workflowStartedAt := time.Now()
	terminalErrorLogged := false
	workflow.emitDebug(options.DebugLog, GeneralWorkflowDebugLog{
		Level:   "info",
		Phase:   "workflow_start",
		Message: "workflow execution started",
		Details: map[string]any{
			"workflow_id": definition.ID,
			"name":        definition.Name,
			"spec":        definition.Spec,
			"step_count":  len(definition.Steps),
			"base_url":    options.BaseURL,
		},
	})
	defer func() {
		if resultErr == nil || terminalErrorLogged {
			return
		}
		entry := GeneralWorkflowDebugLog{
			Level:      "error",
			Phase:      "workflow",
			Message:    resultErr.Error(),
			DurationMS: time.Since(workflowStartedAt).Milliseconds(),
			Details:    map[string]any{"error": resultErr.Error()},
		}
		var workflowErr *GeneralWorkflowError
		if errors.As(resultErr, &workflowErr) {
			entry.StepID = workflowErr.StepID
			entry.StepName = generalWorkflowStepName(definition, workflowErr.StepID)
			entry.Phase = workflowErr.Phase
			entry.Details["error"] = workflowErr.Err.Error()
		}
		workflow.emitDebug(options.DebugLog, entry)
	}()

	compiled, err := compileGeneralWorkflow(definition)
	if err != nil {
		return GeneralWorkflowResult{}, &GeneralWorkflowError{Phase: "validate", Err: err}
	}
	var baseURL *url.URL
	if len(compiled.steps) > 0 || strings.TrimSpace(options.BaseURL) != "" {
		baseURL, err = parseGeneralWorkflowBaseURL(options.BaseURL)
		if err != nil {
			return GeneralWorkflowResult{}, &GeneralWorkflowError{Phase: "validate base URL", Err: err}
		}
	}
	aliases, err := normalizeGeneralWorkflowAliases(options.InitialAliases)
	if err != nil {
		return GeneralWorkflowResult{}, &GeneralWorkflowError{Phase: "validate initial aliases", Err: err}
	}
	runtime, err := workflow.buildRuntime(compiled.definition.ID, options.Runtime)
	if err != nil {
		return GeneralWorkflowResult{}, &GeneralWorkflowError{Phase: "validate runtime", Err: err}
	}
	baseHeaders, err := normalizeGeneralWorkflowBaseHeaders(options.Headers)
	if err != nil {
		return GeneralWorkflowResult{}, &GeneralWorkflowError{Phase: "validate base headers", Err: err}
	}
	workflow.emitDebug(options.DebugLog, GeneralWorkflowDebugLog{
		Level:   "debug",
		Phase:   "validation",
		Message: "workflow definition and runtime inputs validated",
		Details: map[string]any{
			"initial_aliases": sortedGeneralWorkflowKeys(aliases),
			"runtime_fields":  sortedGeneralWorkflowKeys(runtime),
		},
	})

	for _, step := range compiled.steps {
		stepStartedAt := time.Now()
		stepLog := GeneralWorkflowDebugLog{StepID: step.definition.ID, StepName: step.definition.Name}
		workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, "info", "step_start", "step execution started", nil, 0))
		request, err := workflow.renderRequest(step.definition.Request, aliases, baseHeaders)
		if err != nil {
			return GeneralWorkflowResult{}, &GeneralWorkflowError{StepID: step.definition.ID, Phase: "render request", Err: err}
		}
		workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, "debug", "request", "HTTP request rendered", map[string]any{
			"method":   request.method,
			"path":     request.path,
			"query":    generalWorkflowDebugValue("query", request.query),
			"headers":  generalWorkflowHeaderObject(request.headers),
			"has_body": request.hasBody,
			"body":     generalWorkflowDebugValue("body", request.body),
		}, 0))
		requestStartedAt := time.Now()
		response, err := workflow.doRequest(ctx, baseURL, request, options.Recorder)
		if err != nil {
			return GeneralWorkflowResult{}, &GeneralWorkflowError{StepID: step.definition.ID, Phase: "request", Err: err}
		}
		workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, "debug", "response", "HTTP response received", map[string]any{
			"status_code": response.envelope["status"],
			"headers":     response.envelope["headers"],
			"has_body":    response.envelope["has_body"],
			"body":        generalWorkflowDebugValue("body", response.body),
		}, time.Since(requestStartedAt).Milliseconds()))
		expected, err := workflow.runJQ(ctx, step.expect, response.body, response.envelope, request.jqValue, aliases, runtime)
		if err != nil {
			return GeneralWorkflowResult{}, &GeneralWorkflowError{StepID: step.definition.ID, Phase: "evaluate expect", Err: err}
		}
		expectSource := strings.TrimSpace(step.definition.Expect)
		if expectSource == "" {
			expectSource = "$response.status >= 200 and $response.status < 300"
		}
		accepted, ok := expected.(bool)
		expectLevel := "debug"
		expectMessage := "expect expression returned true"
		expectDetails := map[string]any{
			"expression":  expectSource,
			"result_type": generalWorkflowJSONType(expected),
			"result":      expected,
		}
		if !ok || !accepted {
			expectLevel = "error"
			expectMessage = fmt.Sprintf("expect expression rejected the response (HTTP %v)", response.envelope["status"])
			expectDetails["response_status"] = response.envelope["status"]
			expectDetails["response_body"] = generalWorkflowDebugValue("body", response.body)
			terminalErrorLogged = true
		}
		workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, expectLevel, "expect", expectMessage, expectDetails, time.Since(stepStartedAt).Milliseconds()))
		if !ok || !accepted {
			return GeneralWorkflowResult{}, &GeneralWorkflowError{
				StepID: step.definition.ID,
				Phase:  "expect",
				Err:    fmt.Errorf("expression returned %s instead of true", generalWorkflowJSONType(expected)),
			}
		}

		stagedAliases, err := cloneGeneralWorkflowObject(aliases)
		if err != nil {
			return GeneralWorkflowResult{}, &GeneralWorkflowError{StepID: step.definition.ID, Phase: "stage aliases", Err: err}
		}
		for _, extraction := range step.extract {
			value, err := workflow.runJQ(ctx, extraction.code, response.body, response.envelope, request.jqValue, stagedAliases, runtime)
			if err != nil {
				terminalErrorLogged = true
				workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, "error", "extract", "alias extraction failed", map[string]any{
					"alias":      extraction.definition.Alias,
					"expression": extraction.definition.Expression,
					"error":      err.Error(),
				}, time.Since(stepStartedAt).Milliseconds()))
				return GeneralWorkflowResult{}, &GeneralWorkflowError{
					StepID: step.definition.ID,
					Phase:  fmt.Sprintf("extract alias %q", extraction.definition.Alias),
					Err:    err,
				}
			}
			stagedAliases[extraction.definition.Alias] = value
			workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, "debug", "extract", "alias extracted", map[string]any{
				"alias":       extraction.definition.Alias,
				"expression":  extraction.definition.Expression,
				"result_type": generalWorkflowJSONType(value),
				"result":      generalWorkflowDebugValue(extraction.definition.Alias, value),
			}, 0))
		}
		aliases = stagedAliases
		workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, "info", "step_complete", "step execution completed", map[string]any{
			"alias_count": len(aliases),
		}, time.Since(stepStartedAt).Milliseconds()))
	}

	output, err := renderGeneralWorkflowTemplate(compiled.output, aliases)
	if err != nil {
		return GeneralWorkflowResult{}, &GeneralWorkflowError{Phase: "render output", Err: err}
	}
	output, err = canonicalGeneralWorkflowJSON(output, "output")
	if err != nil {
		return GeneralWorkflowResult{}, &GeneralWorkflowError{Phase: "validate output JSON", Err: err}
	}
	workflow.emitDebug(options.DebugLog, GeneralWorkflowDebugLog{
		Level:   "debug",
		Phase:   "output_render",
		Message: "workflow output rendered",
		Details: map[string]any{"output": generalWorkflowDebugValue("output", output)},
	})
	if options.ValidateOutput != nil {
		validationValue, err := canonicalGeneralWorkflowJSON(output, "output validation value")
		if err != nil {
			return GeneralWorkflowResult{}, &GeneralWorkflowError{Phase: "copy output for schema validation", Err: err}
		}
		if err := options.ValidateOutput(validationValue); err != nil {
			return GeneralWorkflowResult{}, &GeneralWorkflowError{Phase: "validate output schema", Err: err}
		}
	}
	workflow.emitDebug(options.DebugLog, GeneralWorkflowDebugLog{
		Level:   "debug",
		Phase:   "output_validation",
		Message: "workflow output schema validated",
	})
	resultAliases, err := cloneGeneralWorkflowObject(aliases)
	if err != nil {
		return GeneralWorkflowResult{}, &GeneralWorkflowError{Phase: "copy result aliases", Err: err}
	}
	workflow.emitDebug(options.DebugLog, GeneralWorkflowDebugLog{
		Level:      "info",
		Phase:      "workflow_complete",
		Message:    "workflow execution completed",
		DurationMS: time.Since(workflowStartedAt).Milliseconds(),
		Details: map[string]any{
			"alias_count": len(resultAliases),
			"step_count":  len(compiled.steps),
		},
	})
	return GeneralWorkflowResult{Output: output, Aliases: resultAliases}, nil
}

func (workflow *GeneralWorkflow) emitDebug(callback func(GeneralWorkflowDebugLog), entry GeneralWorkflowDebugLog) {
	if callback == nil {
		return
	}
	entry.Time = workflow.now().UTC().Format(time.RFC3339Nano)
	callback(entry)
}

func generalWorkflowDebugWith(base GeneralWorkflowDebugLog, level, phase, message string, details map[string]any, durationMS int64) GeneralWorkflowDebugLog {
	base.Level = level
	base.Phase = phase
	base.Message = message
	base.Details = details
	base.DurationMS = durationMS
	return base
}

func generalWorkflowStepName(definition GeneralWorkflowDefinition, stepID string) string {
	for _, step := range definition.Steps {
		if step.ID == stepID {
			return step.Name
		}
	}
	return ""
}

func sortedGeneralWorkflowKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func generalWorkflowDebugValue(name string, value any) any {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) <= generalWorkflowDebugPreviewBytes {
		return value
	}
	return string(encoded[:generalWorkflowDebugPreviewBytes]) + "... [truncated]"
}

func (workflow *GeneralWorkflow) buildRuntime(workflowID string, values map[string]any) (map[string]any, error) {
	runtime := map[string]any{}
	if values != nil {
		normalized, err := canonicalGeneralWorkflowJSON(values, "runtime")
		if err != nil {
			return nil, err
		}
		var ok bool
		runtime, ok = normalized.(map[string]any)
		if !ok {
			return nil, errors.New("runtime must be an object")
		}
	}
	startedAt := workflow.now().UTC()
	runtime["workflow_id"] = workflowID
	runtime["started_at"] = startedAt.Format(time.RFC3339Nano)
	runtime["started_at_ms"] = startedAt.UnixMilli()
	return runtime, nil
}

func normalizeGeneralWorkflowAliases(values map[string]any) (map[string]any, error) {
	aliases := make(map[string]any, len(values))
	for name, value := range values {
		if err := validateGeneralWorkflowAlias(name); err != nil {
			return nil, err
		}
		normalized, err := canonicalGeneralWorkflowJSON(value, "alias "+name)
		if err != nil {
			return nil, err
		}
		aliases[name] = normalized
	}
	return aliases, nil
}

func parseGeneralWorkflowBaseURL(raw string) (*url.URL, error) {
	baseURL, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, errors.New("base URL scheme must be http or https")
	}
	if baseURL.Host == "" || baseURL.User != nil {
		return nil, errors.New("base URL must contain a host and no user info")
	}
	if baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, errors.New("base URL must not contain query or fragment")
	}
	return baseURL, nil
}

func (workflow *GeneralWorkflow) renderRequest(definition GeneralWorkflowRequest, aliases map[string]any, baseHeaders http.Header) (renderedGeneralWorkflowRequest, error) {
	renderedPath, err := renderGeneralWorkflowTemplate(definition.Path, aliases)
	if err != nil {
		return renderedGeneralWorkflowRequest{}, fmt.Errorf("path: %w", err)
	}
	path, ok := renderedPath.(string)
	if !ok {
		return renderedGeneralWorkflowRequest{}, fmt.Errorf("path rendered as %s, expected string", generalWorkflowJSONType(renderedPath))
	}
	if err := validateRenderedGeneralWorkflowPath(path); err != nil {
		return renderedGeneralWorkflowRequest{}, err
	}

	query := map[string]any{}
	if definition.Query != nil {
		queryTemplate, err := canonicalGeneralWorkflowJSON(definition.Query, "query")
		if err != nil {
			return renderedGeneralWorkflowRequest{}, err
		}
		rendered, err := renderGeneralWorkflowTemplate(queryTemplate, aliases)
		if err != nil {
			return renderedGeneralWorkflowRequest{}, fmt.Errorf("query: %w", err)
		}
		query, ok = rendered.(map[string]any)
		if !ok {
			return renderedGeneralWorkflowRequest{}, errors.New("query must render as an object")
		}
		if _, err := encodeGeneralWorkflowQuery(query); err != nil {
			return renderedGeneralWorkflowRequest{}, err
		}
	}

	workflowHeaders := map[string]any{}
	if definition.Headers != nil {
		headerTemplate, err := canonicalGeneralWorkflowJSON(definition.Headers, "headers")
		if err != nil {
			return renderedGeneralWorkflowRequest{}, err
		}
		rendered, err := renderGeneralWorkflowTemplate(headerTemplate, aliases)
		if err != nil {
			return renderedGeneralWorkflowRequest{}, fmt.Errorf("headers: %w", err)
		}
		workflowHeaders, ok = rendered.(map[string]any)
		if !ok {
			return renderedGeneralWorkflowRequest{}, errors.New("headers must render as an object")
		}
	}
	headers, err := workflow.renderHeaders(workflowHeaders, baseHeaders, len(definition.Body) > 0)
	if err != nil {
		return renderedGeneralWorkflowRequest{}, err
	}

	request := renderedGeneralWorkflowRequest{
		method:  strings.ToUpper(strings.TrimSpace(definition.Method)),
		path:    path,
		query:   query,
		headers: headers,
		hasBody: len(definition.Body) > 0,
	}
	if request.hasBody {
		bodyTemplate, err := decodeGeneralWorkflowJSON(definition.Body)
		if err != nil {
			return renderedGeneralWorkflowRequest{}, fmt.Errorf("decode body: %w", err)
		}
		request.body, err = renderGeneralWorkflowTemplate(bodyTemplate, aliases)
		if err != nil {
			return renderedGeneralWorkflowRequest{}, fmt.Errorf("body: %w", err)
		}
		request.body, err = canonicalGeneralWorkflowJSON(request.body, "body")
		if err != nil {
			return renderedGeneralWorkflowRequest{}, err
		}
		request.bodyBytes, err = json.Marshal(request.body)
		if err != nil {
			return renderedGeneralWorkflowRequest{}, fmt.Errorf("encode body: %w", err)
		}
	}
	request.jqValue = map[string]any{
		"method":   request.method,
		"path":     request.path,
		"query":    request.query,
		"headers":  generalWorkflowHeaderObject(request.headers),
		"has_body": request.hasBody,
		"body":     request.body,
	}
	return request, nil
}

func validateRenderedGeneralWorkflowPath(path string) error {
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return errors.New("path must start with one slash")
	}
	parsed, err := url.Parse(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	if parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("path must not contain scheme, authority, query, or fragment")
	}
	return nil
}

func normalizeGeneralWorkflowBaseHeaders(headers http.Header) (http.Header, error) {
	normalized := make(http.Header, len(headers))
	seen := make(map[string]struct{}, len(headers))
	for name, values := range headers {
		if !generalWorkflowTokenPattern.MatchString(name) {
			return nil, fmt.Errorf("invalid base header name %q", name)
		}
		lower := strings.ToLower(name)
		if _, exists := seen[lower]; exists {
			return nil, fmt.Errorf("duplicate base header %q", name)
		}
		seen[lower] = struct{}{}
		canonical := textproto.CanonicalMIMEHeaderKey(name)
		for _, value := range values {
			if err := validateGeneralWorkflowHeaderValue(value); err != nil {
				return nil, fmt.Errorf("base header %q: %w", name, err)
			}
			normalized.Add(canonical, value)
		}
	}
	return normalized, nil
}

func (workflow *GeneralWorkflow) renderHeaders(values map[string]any, baseHeaders http.Header, hasBody bool) (http.Header, error) {
	headers := make(http.Header, len(values)+len(baseHeaders)+3)
	seen := make(map[string]struct{}, len(values))
	for name, value := range values {
		if !generalWorkflowTokenPattern.MatchString(name) {
			return nil, fmt.Errorf("invalid header name %q", name)
		}
		lower := strings.ToLower(name)
		if _, exists := seen[lower]; exists {
			return nil, fmt.Errorf("duplicate rendered header %q", name)
		}
		seen[lower] = struct{}{}
		if _, protected := workflow.protectedHeaders[lower]; protected {
			return nil, fmt.Errorf("header %q is protected", name)
		}
		if _, hostProvided := generalWorkflowHeaderLookup(baseHeaders, lower); hostProvided {
			return nil, fmt.Errorf("header %q conflicts with a host-provided header", name)
		}
		converted, omit, err := generalWorkflowStringValues(value, true)
		if err != nil {
			return nil, fmt.Errorf("header %q: %w", name, err)
		}
		if omit {
			continue
		}
		canonical := textproto.CanonicalMIMEHeaderKey(name)
		for _, item := range converted {
			if err := validateGeneralWorkflowHeaderValue(item); err != nil {
				return nil, fmt.Errorf("header %q: %w", name, err)
			}
			headers.Add(canonical, item)
		}
	}
	for name, values := range baseHeaders {
		for _, value := range values {
			headers.Add(name, value)
		}
	}
	if _, configured := generalWorkflowHeaderLookup(headers, "accept"); !configured {
		headers.Set("Accept", "application/json")
	}
	if _, configured := generalWorkflowHeaderLookup(headers, "content-type"); hasBody && !configured {
		headers.Set("Content-Type", "application/json")
	}
	if _, configured := generalWorkflowHeaderLookup(headers, "user-agent"); workflow.userAgent != "" && !configured {
		headers.Set("User-Agent", workflow.userAgent)
	}
	return headers, nil
}

func generalWorkflowHeaderLookup(headers http.Header, lowerName string) ([]string, bool) {
	for name, values := range headers {
		if strings.EqualFold(name, lowerName) {
			return values, true
		}
	}
	return nil, false
}

func validateGeneralWorkflowHeaderValue(value string) error {
	for _, char := range []byte(value) {
		if char == '\r' || char == '\n' || char == 0 || char == 0x7f || char < 0x20 && char != '\t' {
			return errors.New("value contains an invalid control character")
		}
	}
	return nil
}

func encodeGeneralWorkflowQuery(values map[string]any) (string, error) {
	query := url.Values{}
	for name, value := range values {
		converted, omit, err := generalWorkflowStringValues(value, true)
		if err != nil {
			return "", fmt.Errorf("query parameter %q: %w", name, err)
		}
		if omit {
			continue
		}
		for _, item := range converted {
			query.Add(name, item)
		}
	}
	return query.Encode(), nil
}

func generalWorkflowStringValues(value any, allowArray bool) ([]string, bool, error) {
	if value == nil {
		return nil, true, nil
	}
	if array, ok := value.([]any); ok {
		if !allowArray {
			return nil, false, errors.New("arrays are not allowed")
		}
		values := make([]string, 0, len(array))
		for index, item := range array {
			if item == nil {
				return nil, false, fmt.Errorf("array item %d is null", index)
			}
			converted, err := generalWorkflowScalarString(item)
			if err != nil {
				return nil, false, fmt.Errorf("array item %d: %w", index, err)
			}
			values = append(values, converted)
		}
		return values, false, nil
	}
	converted, err := generalWorkflowScalarString(value)
	if err != nil {
		return nil, false, err
	}
	return []string{converted}, false, nil
}

func generalWorkflowScalarString(value any) (string, error) {
	switch value := value.(type) {
	case string:
		return value, nil
	case bool:
		return strconv.FormatBool(value), nil
	case int:
		return strconv.Itoa(value), nil
	case int64:
		return strconv.FormatInt(value, 10), nil
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return "", errors.New("number is not finite")
		}
		return strconv.FormatFloat(value, 'g', -1, 64), nil
	case json.Number:
		if _, err := normalizeGeneralWorkflowNumber(value); err != nil {
			return "", err
		}
		return value.String(), nil
	default:
		return "", fmt.Errorf("expected a scalar, got %s", generalWorkflowJSONType(value))
	}
}

func (workflow *GeneralWorkflow) doRequest(ctx context.Context, baseURL *url.URL, rendered renderedGeneralWorkflowRequest, recorder ConsoleRequestRecorder) (generalWorkflowHTTPResponse, error) {
	target, err := buildGeneralWorkflowTargetURL(baseURL, rendered.path, rendered.query)
	if err != nil {
		return generalWorkflowHTTPResponse{}, err
	}
	request, err := http.NewRequestWithContext(ctx, rendered.method, target.String(), bytes.NewReader(rendered.bodyBytes))
	if err != nil {
		return generalWorkflowHTTPResponse{}, err
	}
	request.Header = rendered.headers.Clone()
	response, err := workflow.httpClient.Do(request)
	if err != nil {
		recordConsoleRequest(recorder, rendered.method, generalWorkflowRecordedRequestURI(request.URL), 0, err.Error())
		return generalWorkflowHTTPResponse{}, err
	}
	defer response.Body.Close()
	body, err := readGeneralWorkflowBody(response.Body, workflow.maxResponseBytes)
	if err != nil {
		recordConsoleRequest(recorder, rendered.method, generalWorkflowRecordedRequestURI(request.URL), response.StatusCode, err.Error())
		return generalWorkflowHTTPResponse{}, err
	}
	recordedBody := generalWorkflowRecordedBody(body)
	recordConsoleRequest(recorder, rendered.method, generalWorkflowRecordedRequestURI(request.URL), response.StatusCode, recordedBody)
	if !utf8.Valid(body) {
		return generalWorkflowHTTPResponse{}, errors.New("response body is not valid UTF-8")
	}
	var decoded any
	if len(body) > 0 {
		decoded, err = decodeGeneralWorkflowJSON(body)
		if err != nil {
			return generalWorkflowHTTPResponse{}, fmt.Errorf("decode response JSON: %w", err)
		}
	}
	envelope := map[string]any{
		"status":   response.StatusCode,
		"headers":  generalWorkflowHeaderObject(response.Header),
		"has_body": len(body) > 0,
		"body":     decoded,
		"text":     string(body),
	}
	return generalWorkflowHTTPResponse{body: decoded, envelope: envelope}, nil
}

func buildGeneralWorkflowTargetURL(baseURL *url.URL, path string, query map[string]any) (*url.URL, error) {
	reference, err := url.Parse(path)
	if err != nil {
		return nil, err
	}
	target := baseURL.ResolveReference(reference)
	encodedQuery, err := encodeGeneralWorkflowQuery(query)
	if err != nil {
		return nil, err
	}
	target.RawQuery = encodedQuery
	target.Fragment = ""
	return target, nil
}

func readGeneralWorkflowBody(reader io.Reader, limit int64) ([]byte, error) {
	limited := io.LimitReader(reader, limit+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response body exceeds %d bytes", limit)
	}
	return body, nil
}

func compactGeneralWorkflowJSON(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	var output bytes.Buffer
	if err := json.Compact(&output, data); err == nil {
		return output.String()
	}
	return strings.TrimSpace(string(data))
}

func generalWorkflowRecordedBody(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	text := compactGeneralWorkflowJSON(data)
	if len(text) > generalWorkflowDebugPreviewBytes {
		return text[:generalWorkflowDebugPreviewBytes] + "... [truncated]"
	}
	return text
}

func generalWorkflowRecordedRequestURI(target *url.URL) string {
	if target == nil {
		return ""
	}
	return target.RequestURI()
}

func generalWorkflowHeaderObject(headers http.Header) map[string]any {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	result := make(map[string]any, len(headers))
	for _, name := range names {
		lower := strings.ToLower(name)
		values := make([]any, len(headers[name]))
		for index, value := range headers[name] {
			values[index] = value
		}
		if existing, ok := result[lower].([]any); ok {
			result[lower] = append(existing, values...)
		} else {
			result[lower] = values
		}
	}
	return result
}

func (workflow *GeneralWorkflow) runJQ(ctx context.Context, code *gojq.Code, input, response, request any, aliases, runtime map[string]any) (any, error) {
	jqContext, cancel := context.WithTimeout(ctx, workflow.jqTimeout)
	defer cancel()
	varsSnapshot, err := cloneGeneralWorkflowObject(aliases)
	if err != nil {
		return nil, err
	}
	runtimeSnapshot, err := cloneGeneralWorkflowObject(runtime)
	if err != nil {
		return nil, err
	}
	responseSnapshot, err := canonicalGeneralWorkflowJSON(response, "response")
	if err != nil {
		return nil, err
	}
	requestSnapshot, err := canonicalGeneralWorkflowJSON(request, "request")
	if err != nil {
		return nil, err
	}
	inputSnapshot, err := canonicalGeneralWorkflowJSON(input, "response body")
	if err != nil {
		return nil, err
	}
	iterator := code.RunWithContext(jqContext, inputSnapshot, responseSnapshot, requestSnapshot, varsSnapshot, runtimeSnapshot)
	first, ok := iterator.Next()
	if !ok {
		return nil, errors.New("jq expression produced no result")
	}
	if err, ok := first.(error); ok {
		return nil, err
	}
	if second, ok := iterator.Next(); ok {
		if err, isError := second.(error); isError {
			return nil, err
		}
		return nil, errors.New("jq expression produced multiple results; collect them with [ ... ]")
	}
	return canonicalGeneralWorkflowJSON(first, "jq result")
}

type generalWorkflowTemplateReference struct {
	alias   string
	pointer []string
}

type generalWorkflowTemplatePart struct {
	literal   string
	reference *generalWorkflowTemplateReference
}

func validateGeneralWorkflowTemplate(value any) error {
	switch value := value.(type) {
	case nil, bool, json.Number, int, int64, float64:
		return nil
	case string:
		_, err := parseGeneralWorkflowTemplate(value)
		return err
	case []any:
		for index, item := range value {
			if err := validateGeneralWorkflowTemplate(item); err != nil {
				return fmt.Errorf("array item %d: %w", index, err)
			}
		}
		return nil
	case map[string]any:
		for key, item := range value {
			if _, err := parseGeneralWorkflowTemplate(key); err != nil {
				return fmt.Errorf("object key %q: %w", key, err)
			}
			if err := validateGeneralWorkflowTemplate(item); err != nil {
				return fmt.Errorf("object key %q: %w", key, err)
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported template value %T", value)
	}
}

func renderGeneralWorkflowTemplate(value any, aliases map[string]any) (any, error) {
	switch value := value.(type) {
	case nil, bool, int, int64, float64, json.Number:
		return canonicalGeneralWorkflowJSON(value, "template value")
	case string:
		return renderGeneralWorkflowTemplateString(value, aliases, true)
	case []any:
		result := make([]any, len(value))
		for index, item := range value {
			rendered, err := renderGeneralWorkflowTemplate(item, aliases)
			if err != nil {
				return nil, fmt.Errorf("array item %d: %w", index, err)
			}
			result[index] = rendered
		}
		return result, nil
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			renderedKey, err := renderGeneralWorkflowTemplateString(key, aliases, false)
			if err != nil {
				return nil, fmt.Errorf("object key %q: %w", key, err)
			}
			keyString, ok := renderedKey.(string)
			if !ok || keyString == "" {
				return nil, fmt.Errorf("object key %q did not render as a non-empty string", key)
			}
			if _, exists := result[keyString]; exists {
				return nil, fmt.Errorf("object keys collide after rendering: %q", keyString)
			}
			renderedItem, err := renderGeneralWorkflowTemplate(item, aliases)
			if err != nil {
				return nil, fmt.Errorf("object key %q: %w", key, err)
			}
			result[keyString] = renderedItem
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported template value %T", value)
	}
}

func renderGeneralWorkflowTemplateString(source string, aliases map[string]any, preserveExact bool) (any, error) {
	parts, err := parseGeneralWorkflowTemplate(source)
	if err != nil {
		return nil, err
	}
	if preserveExact && len(parts) == 1 && parts[0].reference != nil {
		value, err := resolveGeneralWorkflowReference(*parts[0].reference, aliases)
		if err != nil {
			return nil, err
		}
		return canonicalGeneralWorkflowJSON(value, "template reference")
	}
	var builder strings.Builder
	for _, part := range parts {
		if part.reference == nil {
			builder.WriteString(part.literal)
			continue
		}
		value, err := resolveGeneralWorkflowReference(*part.reference, aliases)
		if err != nil {
			return nil, err
		}
		text, err := generalWorkflowScalarString(value)
		if err != nil {
			return nil, fmt.Errorf("reference %q cannot be interpolated: %w", part.reference.alias, err)
		}
		builder.WriteString(text)
	}
	return builder.String(), nil
}

func parseGeneralWorkflowTemplate(source string) ([]generalWorkflowTemplatePart, error) {
	parts := make([]generalWorkflowTemplatePart, 0, 3)
	var literal strings.Builder
	flushLiteral := func() {
		if literal.Len() > 0 {
			parts = append(parts, generalWorkflowTemplatePart{literal: literal.String()})
			literal.Reset()
		}
	}
	for index := 0; index < len(source); {
		escaped := source[index] == '\\' && index+2 < len(source) && source[index+1:index+3] == "{{"
		if !escaped && (index+1 >= len(source) || source[index:index+2] != "{{") {
			literal.WriteByte(source[index])
			index++
			continue
		}
		start := index
		if escaped {
			start++
		}
		endOffset := strings.Index(source[start+2:], "}}")
		if endOffset < 0 {
			return nil, errors.New("template reference is missing closing braces")
		}
		end := start + 2 + endOffset
		reference, err := parseGeneralWorkflowReference(source[start+2 : end])
		if err != nil {
			return nil, err
		}
		if escaped {
			literal.WriteString(source[start : end+2])
		} else {
			flushLiteral()
			copy := reference
			parts = append(parts, generalWorkflowTemplatePart{reference: &copy})
		}
		index = end + 2
	}
	flushLiteral()
	if len(parts) == 0 {
		parts = append(parts, generalWorkflowTemplatePart{literal: ""})
	}
	return parts, nil
}

func parseGeneralWorkflowReference(source string) (generalWorkflowTemplateReference, error) {
	if source == "" || strings.TrimSpace(source) != source {
		return generalWorkflowTemplateReference{}, fmt.Errorf("invalid template reference %q", source)
	}
	alias, pointerText, hasPointer := strings.Cut(source, "#")
	if err := validateGeneralWorkflowAlias(alias); err != nil {
		return generalWorkflowTemplateReference{}, err
	}
	reference := generalWorkflowTemplateReference{alias: alias}
	if hasPointer {
		pointer, err := parseGeneralWorkflowJSONPointer(pointerText)
		if err != nil {
			return generalWorkflowTemplateReference{}, fmt.Errorf("alias %q JSON Pointer: %w", alias, err)
		}
		reference.pointer = pointer
	}
	return reference, nil
}

func parseGeneralWorkflowJSONPointer(pointer string) ([]string, error) {
	if pointer == "" {
		return []string{}, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, errors.New("must be empty or start with /")
	}
	rawTokens := strings.Split(pointer[1:], "/")
	tokens := make([]string, len(rawTokens))
	for index, raw := range rawTokens {
		var token strings.Builder
		for i := 0; i < len(raw); i++ {
			if raw[i] != '~' {
				token.WriteByte(raw[i])
				continue
			}
			if i+1 >= len(raw) || raw[i+1] != '0' && raw[i+1] != '1' {
				return nil, fmt.Errorf("token %d contains an invalid ~ escape", index)
			}
			i++
			if raw[i] == '0' {
				token.WriteByte('~')
			} else {
				token.WriteByte('/')
			}
		}
		tokens[index] = token.String()
	}
	return tokens, nil
}

func resolveGeneralWorkflowReference(reference generalWorkflowTemplateReference, aliases map[string]any) (any, error) {
	value, exists := aliases[reference.alias]
	if !exists {
		return nil, fmt.Errorf("alias %q does not exist", reference.alias)
	}
	for _, token := range reference.pointer {
		switch current := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = current[token]
			if !ok {
				return nil, fmt.Errorf("alias %q JSON Pointer member %q does not exist", reference.alias, token)
			}
		case []any:
			if token == "" || token != "0" && strings.HasPrefix(token, "0") {
				return nil, fmt.Errorf("alias %q JSON Pointer array index %q is invalid", reference.alias, token)
			}
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(current) {
				return nil, fmt.Errorf("alias %q JSON Pointer array index %q is out of range", reference.alias, token)
			}
			value = current[index]
		default:
			return nil, fmt.Errorf("alias %q JSON Pointer cannot traverse %s", reference.alias, generalWorkflowJSONType(value))
		}
	}
	return value, nil
}

func decodeGeneralWorkflowJSON(data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("JSON is not valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := readGeneralWorkflowJSONValue(decoder, "$")
	if err != nil {
		return nil, err
	}
	if err := requireGeneralWorkflowEOF(decoder); err != nil {
		return nil, err
	}
	return value, nil
}

func readGeneralWorkflowJSONValue(decoder *json.Decoder, path string) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch token := token.(type) {
	case json.Delim:
		switch token {
		case '{':
			object := map[string]any{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, fmt.Errorf("%s: object key is not a string", path)
				}
				if _, duplicate := object[key]; duplicate {
					return nil, fmt.Errorf("%s: duplicate object key %q", path, key)
				}
				value, err := readGeneralWorkflowJSONValue(decoder, path+"/"+escapeGeneralWorkflowJSONPointerToken(key))
				if err != nil {
					return nil, err
				}
				object[key] = value
			}
			closing, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			if closing != json.Delim('}') {
				return nil, fmt.Errorf("%s: object is not closed", path)
			}
			return object, nil
		case '[':
			array := []any{}
			for decoder.More() {
				value, err := readGeneralWorkflowJSONValue(decoder, fmt.Sprintf("%s/%d", path, len(array)))
				if err != nil {
					return nil, err
				}
				array = append(array, value)
			}
			closing, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			if closing != json.Delim(']') {
				return nil, fmt.Errorf("%s: array is not closed", path)
			}
			return array, nil
		default:
			return nil, fmt.Errorf("%s: unexpected JSON delimiter %q", path, token)
		}
	case json.Number:
		value, err := normalizeGeneralWorkflowNumber(token)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return value, nil
	case nil, bool, string:
		return token, nil
	default:
		return nil, fmt.Errorf("%s: unsupported JSON token %T", path, token)
	}
}

func requireGeneralWorkflowEOF(decoder *json.Decoder) error {
	if _, err := decoder.Token(); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("JSON contains multiple top-level values")
}

func escapeGeneralWorkflowJSONPointerToken(token string) string {
	token = strings.ReplaceAll(token, "~", "~0")
	return strings.ReplaceAll(token, "/", "~1")
}

func normalizeGeneralWorkflowNumber(number json.Number) (any, error) {
	raw := number.String()
	if strings.ContainsAny(raw, ".eE") {
		value, err := number.Float64()
		if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
			return nil, fmt.Errorf("invalid finite JSON number %q", raw)
		}
		if math.Trunc(value) == value && math.Abs(value) > float64(maxSafeJSONInteger) {
			return nil, fmt.Errorf("integer %q exceeds the safe JSON integer range", raw)
		}
		return value, nil
	}
	integer, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return nil, fmt.Errorf("invalid JSON integer %q", raw)
	}
	max := big.NewInt(maxSafeJSONInteger)
	min := new(big.Int).Neg(new(big.Int).Set(max))
	if integer.Cmp(min) < 0 || integer.Cmp(max) > 0 {
		return nil, fmt.Errorf("integer %q exceeds the safe JSON integer range", raw)
	}
	return integer.Int64(), nil
}

func canonicalGeneralWorkflowJSON(value any, path string) (any, error) {
	switch value := value.(type) {
	case nil, bool, string:
		return value, nil
	case json.Number:
		return normalizeGeneralWorkflowNumber(value)
	case int:
		if int64(value) < -maxSafeJSONInteger || int64(value) > maxSafeJSONInteger {
			return nil, fmt.Errorf("%s: integer exceeds the safe JSON integer range", path)
		}
		return value, nil
	case int8:
		return int64(value), nil
	case int16:
		return int64(value), nil
	case int32:
		return int64(value), nil
	case int64:
		if value < -maxSafeJSONInteger || value > maxSafeJSONInteger {
			return nil, fmt.Errorf("%s: integer exceeds the safe JSON integer range", path)
		}
		return value, nil
	case uint:
		if uint64(value) > uint64(maxSafeJSONInteger) {
			return nil, fmt.Errorf("%s: integer exceeds the safe JSON integer range", path)
		}
		return int64(value), nil
	case uint8:
		return int64(value), nil
	case uint16:
		return int64(value), nil
	case uint32:
		return int64(value), nil
	case uint64:
		if value > uint64(maxSafeJSONInteger) {
			return nil, fmt.Errorf("%s: integer exceeds the safe JSON integer range", path)
		}
		return int64(value), nil
	case float32:
		return canonicalGeneralWorkflowJSON(float64(value), path)
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("%s: number is not finite", path)
		}
		if math.Trunc(value) == value && math.Abs(value) > float64(maxSafeJSONInteger) {
			return nil, fmt.Errorf("%s: integer exceeds the safe JSON integer range", path)
		}
		return value, nil
	case *big.Int:
		if value == nil {
			return nil, nil
		}
		max := big.NewInt(maxSafeJSONInteger)
		min := new(big.Int).Neg(new(big.Int).Set(max))
		if value.Cmp(min) < 0 || value.Cmp(max) > 0 {
			return nil, fmt.Errorf("%s: integer exceeds the safe JSON integer range", path)
		}
		return value.Int64(), nil
	case []any:
		result := make([]any, len(value))
		for index, item := range value {
			normalized, err := canonicalGeneralWorkflowJSON(item, fmt.Sprintf("%s/%d", path, index))
			if err != nil {
				return nil, err
			}
			result[index] = normalized
		}
		return result, nil
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			normalized, err := canonicalGeneralWorkflowJSON(item, path+"/"+escapeGeneralWorkflowJSONPointerToken(key))
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
		return result, nil
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("%s: value %T is not JSON-compatible: %w", path, value, err)
		}
		normalized, err := decodeGeneralWorkflowJSON(encoded)
		if err != nil {
			return nil, fmt.Errorf("%s: value %T is not JSON-compatible: %w", path, value, err)
		}
		return normalized, nil
	}
}

func cloneGeneralWorkflowObject(value map[string]any) (map[string]any, error) {
	cloned, err := canonicalGeneralWorkflowJSON(value, "object")
	if err != nil {
		return nil, err
	}
	return cloned.(map[string]any), nil
}

func generalWorkflowJSONType(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number, *big.Int:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", value)
	}
}
