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
	"net/http/cookiejar"
	"net/textproto"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/itchyny/gojq"

	"red-token/internal/config"
)

const (
	GeneralWorkflowSpecV1              = "http-workflow/v1"
	GeneralWorkflowSpecV2              = "http-workflow/v2"
	GeneralWorkflowSpecV3              = "http-workflow/v3"
	GeneralWorkflowSpecV4              = "http-workflow/v4"
	GeneralWorkflowSpec                = GeneralWorkflowSpecV4
	defaultGeneralWorkflowBodyLimit    = int64(10 << 20)
	defaultGeneralWorkflowHTTPTimeout  = 30 * time.Second
	defaultGeneralWorkflowJQTimeout    = 5 * time.Second
	defaultGeneralWorkflowVisitLimit   = 100
	defaultGeneralWorkflowForeachLimit = 1000
	generalWorkflowDebugPreviewBytes   = 64 << 10
	maxSafeJSONInteger                 = int64(1<<53 - 1)
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
	Spec    string                `json:"spec"`
	ID      string                `json:"id"`
	Name    string                `json:"name"`
	Headers map[string]any        `json:"headers,omitempty"`
	Steps   []GeneralWorkflowStep `json:"steps"`
	Output  json.RawMessage       `json:"output"`
}

type GeneralWorkflowStep struct {
	ID      string                      `json:"id"`
	Name    string                      `json:"name"`
	Foreach *GeneralWorkflowForeach     `json:"foreach,omitempty"`
	Request GeneralWorkflowRequest      `json:"request"`
	Expect  *GeneralWorkflowExpect      `json:"expect,omitempty"`
	Extract []GeneralWorkflowExtraction `json:"extract,omitempty"`
	When    *GeneralWorkflowWhen        `json:"when,omitempty"`
}

type GeneralWorkflowForeach struct {
	Alias   string `json:"alias"`
	As      string `json:"as"`
	IndexAs string `json:"index_as,omitempty"`
}

type GeneralWorkflowExpect struct {
	Expression       string
	Routes           []GeneralWorkflowStatusRoute
	AcceptedStatuses []int
}

type GeneralWorkflowStatusRoute struct {
	Statuses []int  `json:"statuses"`
	Goto     string `json:"goto"`
}

type GeneralWorkflowWhen struct {
	Expression string `json:"expression"`
	Goto       string `json:"goto"`
}

func (expect *GeneralWorkflowExpect) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return errors.New("expect is required")
	}
	if data[0] == '"' {
		return json.Unmarshal(data, &expect.Expression)
	}
	var object struct {
		Routes           []GeneralWorkflowStatusRoute `json:"routes"`
		AcceptedStatuses []int                        `json:"accepted_statuses"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&object); err != nil {
		return err
	}
	expect.Routes = object.Routes
	expect.AcceptedStatuses = object.AcceptedStatuses
	return nil
}

func (expect GeneralWorkflowExpect) MarshalJSON() ([]byte, error) {
	if expect.Expression != "" {
		return json.Marshal(expect.Expression)
	}
	return json.Marshal(struct {
		Routes           []GeneralWorkflowStatusRoute `json:"routes,omitempty"`
		AcceptedStatuses []int                        `json:"accepted_statuses,omitempty"`
	}{Routes: expect.Routes, AcceptedStatuses: expect.AcceptedStatuses})
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
	Output          any            `json:"output"`
	Aliases         map[string]any `json:"aliases"`
	ResponseCookies []*http.Cookie `json:"-"`
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
	definition       GeneralWorkflowStep
	expect           *gojq.Code
	expectRoutes     map[int]compiledGeneralWorkflowStatusRoute
	acceptedStatuses map[int]struct{}
	when             *gojq.Code
	whenGotoIndex    int
	extract          []compiledGeneralWorkflowExtraction
}

type compiledGeneralWorkflowStatusRoute struct {
	definition GeneralWorkflowStatusRoute
	gotoIndex  int
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

type generalWorkflowCookieJar struct {
	inner   http.CookieJar
	mu      sync.Mutex
	updates []*http.Cookie
}

func newGeneralWorkflowCookieJar() (*generalWorkflowCookieJar, error) {
	inner, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &generalWorkflowCookieJar{inner: inner}, nil
}

func (jar *generalWorkflowCookieJar) SetCookies(target *url.URL, cookies []*http.Cookie) {
	jar.inner.SetCookies(target, cookies)
	jar.mu.Lock()
	defer jar.mu.Unlock()
	for _, cookie := range cookies {
		if cookie == nil {
			continue
		}
		copy := *cookie
		jar.updates = append(jar.updates, &copy)
	}
}

func (jar *generalWorkflowCookieJar) Cookies(target *url.URL) []*http.Cookie {
	return jar.inner.Cookies(target)
}

func (jar *generalWorkflowCookieJar) seed(target *url.URL, cookies []*http.Cookie) {
	jar.inner.SetCookies(target, cookies)
}

func (jar *generalWorkflowCookieJar) responseCookies() []*http.Cookie {
	jar.mu.Lock()
	defer jar.mu.Unlock()
	result := make([]*http.Cookie, len(jar.updates))
	for index, cookie := range jar.updates {
		copy := *cookie
		result[index] = &copy
	}
	return result
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
	if headers, exists := root["headers"]; exists {
		if _, ok := headers.(map[string]any); !ok {
			return errors.New("$/headers must be an object")
		}
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
		if foreachValue, exists := step["foreach"]; exists {
			foreach, ok := foreachValue.(map[string]any)
			if !ok {
				return fmt.Errorf("%s/foreach must be an object", path)
			}
			for _, field := range []string{"alias", "as"} {
				if _, err := requiredGeneralWorkflowString(foreach, field, path+"/foreach/"+field); err != nil {
					return err
				}
			}
			if _, exists := foreach["index_as"]; exists {
				indexAs, err := requiredGeneralWorkflowString(foreach, "index_as", path+"/foreach/index_as")
				if err != nil {
					return err
				}
				if strings.TrimSpace(indexAs) == "" {
					return fmt.Errorf("%s/foreach/index_as must not be empty", path)
				}
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
			switch expect := expect.(type) {
			case string:
				if strings.TrimSpace(expect) == "" {
					return fmt.Errorf("%s/expect must not be empty", path)
				}
			case map[string]any:
				routes := []any{}
				if routesValue, exists := expect["routes"]; exists {
					var ok bool
					routes, ok = routesValue.([]any)
					if !ok {
						return fmt.Errorf("%s/expect/routes must be an array", path)
					}
				}
				for routeIndex, routeValue := range routes {
					routePath := fmt.Sprintf("%s/expect/routes/%d", path, routeIndex)
					route, ok := routeValue.(map[string]any)
					if !ok {
						return fmt.Errorf("%s must be an object", routePath)
					}
					if _, err := requiredGeneralWorkflowString(route, "goto", routePath+"/goto"); err != nil {
						return err
					}
					statuses, ok := route["statuses"].([]any)
					if !ok || len(statuses) == 0 {
						return fmt.Errorf("%s/statuses must be a non-empty integer array", routePath)
					}
					for statusIndex, status := range statuses {
						if number, ok := status.(int64); !ok || number < 100 || number > 599 {
							return fmt.Errorf("%s/statuses/%d must be an HTTP status code", routePath, statusIndex)
						}
					}
				}
				accepted, hasAccepted := expect["accepted_statuses"]
				if len(routes) == 0 && !hasAccepted {
					return fmt.Errorf("%s/expect requires routes or accepted_statuses", path)
				}
				if hasAccepted {
					statuses, ok := accepted.([]any)
					if !ok || len(statuses) == 0 {
						return fmt.Errorf("%s/expect/accepted_statuses must be a non-empty integer array", path)
					}
					for statusIndex, status := range statuses {
						if number, ok := status.(int64); !ok || number < 100 || number > 599 {
							return fmt.Errorf("%s/expect/accepted_statuses/%d must be an HTTP status code", path, statusIndex)
						}
					}
				}
			default:
				return fmt.Errorf("%s/expect must be a string or object", path)
			}
		}
		if when, exists := step["when"]; exists {
			object, ok := when.(map[string]any)
			if !ok {
				return fmt.Errorf("%s/when must be an object", path)
			}
			for _, field := range []string{"expression", "goto"} {
				if _, err := requiredGeneralWorkflowString(object, field, path+"/when/"+field); err != nil {
					return err
				}
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
	if definition.Spec != GeneralWorkflowSpecV1 && definition.Spec != GeneralWorkflowSpecV2 && definition.Spec != GeneralWorkflowSpecV3 && definition.Spec != GeneralWorkflowSpecV4 {
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
	if definition.Headers != nil {
		if definition.Spec != GeneralWorkflowSpecV4 {
			return compiledGeneralWorkflow{}, fmt.Errorf("workflow headers require spec %q", GeneralWorkflowSpecV4)
		}
		headers, err := canonicalGeneralWorkflowJSON(definition.Headers, "workflow headers")
		if err != nil {
			return compiledGeneralWorkflow{}, err
		}
		if err := validateGeneralWorkflowTemplate(headers); err != nil {
			return compiledGeneralWorkflow{}, fmt.Errorf("validate workflow headers template: %w", err)
		}
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
	stepIDs := make(map[string]int, len(definition.Steps))
	for index, step := range definition.Steps {
		if !generalWorkflowIDPattern.MatchString(step.ID) {
			return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d]: invalid step id %q", index, step.ID)
		}
		if _, exists := stepIDs[step.ID]; exists {
			return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d]: duplicate step id %q", index, step.ID)
		}
		stepIDs[step.ID] = index
		if strings.TrimSpace(step.Name) == "" {
			return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d]: step name is required", index)
		}
		if step.Foreach != nil {
			if definition.Spec != GeneralWorkflowSpecV3 && definition.Spec != GeneralWorkflowSpecV4 {
				return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d]: foreach requires spec %q or later", index, GeneralWorkflowSpecV3)
			}
			if err := validateGeneralWorkflowAlias(step.Foreach.Alias); err != nil {
				return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] foreach alias: %w", index, err)
			}
			if err := validateGeneralWorkflowAlias(step.Foreach.As); err != nil {
				return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] foreach as: %w", index, err)
			}
			if step.Foreach.IndexAs != "" {
				if err := validateGeneralWorkflowAlias(step.Foreach.IndexAs); err != nil {
					return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] foreach index_as: %w", index, err)
				}
			}
			if step.Foreach.Alias == step.Foreach.As || step.Foreach.IndexAs != "" && step.Foreach.Alias == step.Foreach.IndexAs {
				return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d]: foreach alias must differ from iteration aliases", index)
			}
			if step.Foreach.IndexAs != "" && step.Foreach.As == step.Foreach.IndexAs {
				return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d]: foreach as and index_as must differ", index)
			}
		}
		if err := validateGeneralWorkflowRequest(step.Request); err != nil {
			return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] request: %w", index, err)
		}
		var expect *gojq.Code
		expectRoutes := make(map[int]compiledGeneralWorkflowStatusRoute)
		if definition.Spec == GeneralWorkflowSpecV1 && step.Expect != nil && (len(step.Expect.Routes) > 0 || len(step.Expect.AcceptedStatuses) > 0) {
			return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d]: structured expect requires spec %q", index, GeneralWorkflowSpecV2)
		}
		if step.Expect != nil && step.Expect.Expression != "" {
			if definition.Spec != GeneralWorkflowSpecV1 {
				return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d]: string expect requires spec %q", index, GeneralWorkflowSpecV1)
			}
			expect, err = compileGeneralWorkflowJQ(step.Expect.Expression)
			if err != nil {
				return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] expect: %w", index, err)
			}
		} else if definition.Spec == GeneralWorkflowSpecV1 {
			expect, err = compileGeneralWorkflowJQ("$response.status >= 200 and $response.status < 300")
			if err != nil {
				return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] expect: %w", index, err)
			}
		} else if step.Expect != nil && len(step.Expect.Routes) == 0 && len(step.Expect.AcceptedStatuses) == 0 {
			return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d]: expect requires routes or accepted_statuses", index)
		}
		var when *gojq.Code
		if step.When != nil {
			if definition.Spec != GeneralWorkflowSpecV2 && definition.Spec != GeneralWorkflowSpecV3 && definition.Spec != GeneralWorkflowSpecV4 {
				return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d]: when requires spec %q or later", index, GeneralWorkflowSpecV2)
			}
			if strings.TrimSpace(step.When.Expression) == "" || strings.TrimSpace(step.When.Goto) == "" {
				return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d]: when expression and goto are required", index)
			}
			when, err = compileGeneralWorkflowJQ(step.When.Expression)
			if err != nil {
				return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] when: %w", index, err)
			}
		}
		compiledStep := compiledGeneralWorkflowStep{
			definition:       step,
			expect:           expect,
			expectRoutes:     expectRoutes,
			acceptedStatuses: make(map[int]struct{}),
			when:             when,
			whenGotoIndex:    -1,
			extract:          make([]compiledGeneralWorkflowExtraction, 0, len(step.Extract)),
		}
		extractAliases := make(map[string]struct{}, len(step.Extract))
		for extractIndex, extraction := range step.Extract {
			if err := validateGeneralWorkflowAlias(extraction.Alias); err != nil {
				return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] extract[%d]: %w", index, extractIndex, err)
			}
			if strings.TrimSpace(extraction.Expression) == "" {
				return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] extract[%d]: expression is required", index, extractIndex)
			}
			if step.Foreach != nil {
				if extraction.Alias == step.Foreach.As || step.Foreach.IndexAs != "" && extraction.Alias == step.Foreach.IndexAs {
					return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] extract[%d]: alias %q conflicts with a foreach iteration alias", index, extractIndex, extraction.Alias)
				}
				if _, duplicate := extractAliases[extraction.Alias]; duplicate {
					return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] extract[%d]: duplicate foreach extract alias %q", index, extractIndex, extraction.Alias)
				}
				extractAliases[extraction.Alias] = struct{}{}
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
	for index := range compiled.steps {
		step := &compiled.steps[index]
		if step.definition.Expect != nil {
			for statusIndex, status := range step.definition.Expect.AcceptedStatuses {
				if status < 100 || status > 599 {
					return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] expect accepted_statuses[%d] invalid HTTP status %d", index, statusIndex, status)
				}
				if _, duplicate := step.acceptedStatuses[status]; duplicate {
					return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] duplicate accepted status %d", index, status)
				}
				step.acceptedStatuses[status] = struct{}{}
			}
			for routeIndex, route := range step.definition.Expect.Routes {
				target, ok := stepIDs[route.Goto]
				if !ok {
					return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] expect routes[%d] target %q does not exist", index, routeIndex, route.Goto)
				}
				for _, status := range route.Statuses {
					if status < 100 || status > 599 {
						return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] expect routes[%d] invalid HTTP status %d", index, routeIndex, status)
					}
					if _, duplicate := step.expectRoutes[status]; duplicate {
						return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] duplicate expect status %d", index, status)
					}
					if _, accepted := step.acceptedStatuses[status]; accepted {
						return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] expect status %d cannot be both accepted and routed", index, status)
					}
					step.expectRoutes[status] = compiledGeneralWorkflowStatusRoute{definition: route, gotoIndex: target}
				}
			}
		}
		if step.definition.When != nil {
			target, ok := stepIDs[step.definition.When.Goto]
			if !ok {
				return compiledGeneralWorkflow{}, fmt.Errorf("steps[%d] when target %q does not exist", index, step.definition.When.Goto)
			}
			step.whenGotoIndex = target
		}
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
	runClient, cookieJar, err := workflow.newRunHTTPClient(baseURL, baseHeaders)
	if err != nil {
		return GeneralWorkflowResult{}, &GeneralWorkflowError{Phase: "initialize cookies", Err: err}
	}
	defer func() {
		result.ResponseCookies = cookieJar.responseCookies()
	}()
	workflow.emitDebug(options.DebugLog, GeneralWorkflowDebugLog{
		Level:   "debug",
		Phase:   "validation",
		Message: "workflow definition and runtime inputs validated",
		Details: map[string]any{
			"initial_aliases": sortedGeneralWorkflowKeys(aliases),
			"runtime_fields":  sortedGeneralWorkflowKeys(runtime),
		},
	})

	stepIndex := 0
	executionCount := 0
	stepVisits := make([]int, len(compiled.steps))
stepLoop:
	for stepIndex < len(compiled.steps) {
		executionCount++
		stepVisits[stepIndex]++
		if stepVisits[stepIndex] > defaultGeneralWorkflowVisitLimit {
			return GeneralWorkflowResult{}, &GeneralWorkflowError{StepID: compiled.steps[stepIndex].definition.ID, Phase: "control flow", Err: fmt.Errorf("step visit limit exceeded (%d); possible goto loop", defaultGeneralWorkflowVisitLimit)}
		}
		step := compiled.steps[stepIndex]
		stepStartedAt := time.Now()
		stepLog := GeneralWorkflowDebugLog{StepID: step.definition.ID, StepName: step.definition.Name}
		foreachItems := []any{nil}
		stepDetails := map[string]any(nil)
		if step.definition.Foreach != nil {
			foreach := step.definition.Foreach
			source, exists := aliases[foreach.Alias]
			if !exists {
				return GeneralWorkflowResult{}, &GeneralWorkflowError{StepID: step.definition.ID, Phase: "foreach", Err: fmt.Errorf("source alias %q does not exist", foreach.Alias)}
			}
			var ok bool
			foreachItems, ok = source.([]any)
			if !ok {
				return GeneralWorkflowResult{}, &GeneralWorkflowError{StepID: step.definition.ID, Phase: "foreach", Err: fmt.Errorf("source alias %q must be an array, got %s", foreach.Alias, generalWorkflowJSONType(source))}
			}
			if len(foreachItems) > defaultGeneralWorkflowForeachLimit {
				return GeneralWorkflowResult{}, &GeneralWorkflowError{StepID: step.definition.ID, Phase: "foreach", Err: fmt.Errorf("source alias %q has %d items; limit is %d", foreach.Alias, len(foreachItems), defaultGeneralWorkflowForeachLimit)}
			}
			stepDetails = map[string]any{"foreach_alias": foreach.Alias, "item_count": len(foreachItems)}
		}
		workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, "info", "step_start", "step execution started", stepDetails, 0))

		aggregatedAliases := make(map[string][]any, len(step.extract))
		if step.definition.Foreach != nil {
			for _, extraction := range step.extract {
				aggregatedAliases[extraction.definition.Alias] = []any{}
			}
		}
		var lastRequest renderedGeneralWorkflowRequest
		var lastResponse generalWorkflowHTTPResponse
		for iterationIndex, foreachItem := range foreachItems {
			iterationAliases := aliases
			if step.definition.Foreach != nil {
				iterationAliases, err = cloneGeneralWorkflowObject(aliases)
				if err != nil {
					return GeneralWorkflowResult{}, &GeneralWorkflowError{StepID: step.definition.ID, Phase: "stage foreach aliases", Err: err}
				}
				iterationAliases[step.definition.Foreach.As] = foreachItem
				if step.definition.Foreach.IndexAs != "" {
					iterationAliases[step.definition.Foreach.IndexAs] = iterationIndex
				}
			}
			withIteration := func(details map[string]any) map[string]any {
				if step.definition.Foreach == nil {
					return details
				}
				if details == nil {
					details = map[string]any{}
				}
				details["iteration_index"] = iterationIndex
				details["iteration_item"] = generalWorkflowDebugValue(step.definition.Foreach.As, foreachItem)
				return details
			}
			iterationError := func(iterationErr error) error {
				if step.definition.Foreach == nil {
					return iterationErr
				}
				return fmt.Errorf("foreach iteration %d: %w", iterationIndex, iterationErr)
			}
			if step.definition.Foreach != nil {
				workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, "debug", "foreach_iteration", "foreach iteration started", withIteration(nil), 0))
			}

			request, err := workflow.renderRequest(compiled.definition.Headers, step.definition.Request, iterationAliases, runtime, baseHeaders)
			if err != nil {
				return GeneralWorkflowResult{}, &GeneralWorkflowError{StepID: step.definition.ID, Phase: "render request", Err: iterationError(err)}
			}
			if err := applyGeneralWorkflowCookiePreview(baseURL, &request, cookieJar); err != nil {
				return GeneralWorkflowResult{}, &GeneralWorkflowError{StepID: step.definition.ID, Phase: "render request cookies", Err: iterationError(err)}
			}
			workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, "debug", "request", "HTTP request rendered", withIteration(map[string]any{
				"method":   request.method,
				"path":     request.path,
				"query":    generalWorkflowDebugValue("query", request.query),
				"headers":  request.jqValue["headers"],
				"has_body": request.hasBody,
				"body":     generalWorkflowDebugValue("body", request.body),
			}), 0))
			requestStartedAt := time.Now()
			response, err := workflow.doRequest(ctx, runClient, baseURL, request, options.Recorder)
			if err != nil {
				return GeneralWorkflowResult{}, &GeneralWorkflowError{StepID: step.definition.ID, Phase: "request", Err: iterationError(err)}
			}
			lastRequest = request
			lastResponse = response
			workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, "debug", "response", "HTTP response received", withIteration(map[string]any{
				"status_code": response.envelope["status"],
				"headers":     response.envelope["headers"],
				"has_body":    response.envelope["has_body"],
				"body":        generalWorkflowDebugValue("body", response.body),
			}), time.Since(requestStartedAt).Milliseconds()))
			status, ok := response.envelope["status"].(int)
			if !ok {
				return GeneralWorkflowResult{}, &GeneralWorkflowError{StepID: step.definition.ID, Phase: "expect", Err: iterationError(errors.New("response status is unavailable"))}
			}
			if route, matched := step.expectRoutes[status]; matched {
				workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, "info", "expect_goto", "response status matched expect route; skipping extract and jumping to target", withIteration(map[string]any{
					"response_status": status,
					"goto":            route.definition.Goto,
				}), time.Since(stepStartedAt).Milliseconds()))
				stepIndex = route.gotoIndex
				continue stepLoop
			}
			if step.expect != nil {
				expected, err := workflow.runJQ(ctx, step.expect, response.body, response.envelope, request.jqValue, iterationAliases, runtime)
				if err != nil {
					return GeneralWorkflowResult{}, &GeneralWorkflowError{StepID: step.definition.ID, Phase: "evaluate expect", Err: iterationError(err)}
				}
				accepted, ok := expected.(bool)
				if !ok || !accepted {
					terminalErrorLogged = true
					expectSource := "$response.status >= 200 and $response.status < 300"
					if step.definition.Expect != nil {
						expectSource = step.definition.Expect.Expression
					}
					workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, "error", "expect", fmt.Sprintf("expect expression rejected the response (HTTP %d)", status), withIteration(map[string]any{
						"expression":      expectSource,
						"result_type":     generalWorkflowJSONType(expected),
						"result":          expected,
						"response_status": status,
					}), time.Since(stepStartedAt).Milliseconds()))
					return GeneralWorkflowResult{}, &GeneralWorkflowError{StepID: step.definition.ID, Phase: "expect", Err: iterationError(fmt.Errorf("expression returned %s instead of true", generalWorkflowJSONType(expected)))}
				}
			} else if _, accepted := step.acceptedStatuses[status]; (status < 200 || status >= 300) && !accepted {
				terminalErrorLogged = true
				workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, "error", "expect", fmt.Sprintf("response status HTTP %d did not match a route and is not 2xx", status), withIteration(map[string]any{
					"response_status": status,
					"response_body":   generalWorkflowDebugValue("body", response.body),
				}), time.Since(stepStartedAt).Milliseconds()))
				return GeneralWorkflowResult{}, &GeneralWorkflowError{StepID: step.definition.ID, Phase: "expect", Err: iterationError(fmt.Errorf("HTTP %d did not match an expect route", status))}
			} else {
				workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, "debug", "expect", "response status is accepted; continuing to extract", withIteration(map[string]any{"response_status": status}), time.Since(stepStartedAt).Milliseconds()))
			}

			stagedAliases, err := cloneGeneralWorkflowObject(iterationAliases)
			if err != nil {
				return GeneralWorkflowResult{}, &GeneralWorkflowError{StepID: step.definition.ID, Phase: "stage aliases", Err: iterationError(err)}
			}
			for _, extraction := range step.extract {
				value, err := workflow.runJQ(ctx, extraction.code, response.body, response.envelope, request.jqValue, stagedAliases, runtime)
				if err != nil {
					terminalErrorLogged = true
					workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, "error", "extract", "alias extraction failed", withIteration(map[string]any{
						"alias":      extraction.definition.Alias,
						"expression": extraction.definition.Expression,
						"error":      err.Error(),
					}), time.Since(stepStartedAt).Milliseconds()))
					return GeneralWorkflowResult{}, &GeneralWorkflowError{
						StepID: step.definition.ID,
						Phase:  fmt.Sprintf("extract alias %q", extraction.definition.Alias),
						Err:    iterationError(err),
					}
				}
				stagedAliases[extraction.definition.Alias] = value
				if step.definition.Foreach != nil {
					aggregatedAliases[extraction.definition.Alias] = append(aggregatedAliases[extraction.definition.Alias], value)
				}
				workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, "debug", "extract", "alias extracted", withIteration(map[string]any{
					"alias":       extraction.definition.Alias,
					"expression":  extraction.definition.Expression,
					"result_type": generalWorkflowJSONType(value),
					"result":      generalWorkflowDebugValue(extraction.definition.Alias, value),
				}), 0))
			}
			if step.definition.Foreach == nil {
				aliases = stagedAliases
			} else {
				workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, "debug", "foreach_iteration", "foreach iteration completed", withIteration(nil), time.Since(requestStartedAt).Milliseconds()))
			}
		}
		if step.definition.Foreach != nil {
			stagedAliases, err := cloneGeneralWorkflowObject(aliases)
			if err != nil {
				return GeneralWorkflowResult{}, &GeneralWorkflowError{StepID: step.definition.ID, Phase: "commit foreach aliases", Err: err}
			}
			for _, extraction := range step.extract {
				stagedAliases[extraction.definition.Alias] = aggregatedAliases[extraction.definition.Alias]
			}
			aliases = stagedAliases
			workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, "debug", "foreach_commit", "foreach extract aliases committed", map[string]any{
				"item_count": len(foreachItems),
				"aliases":    sortedGeneralWorkflowKeysFromSlices(aggregatedAliases),
			}, time.Since(stepStartedAt).Milliseconds()))
		}
		if step.when != nil {
			var whenResponse any = lastResponse.envelope
			var whenRequest any = lastRequest.jqValue
			if step.definition.Foreach != nil && len(foreachItems) == 0 {
				whenResponse = nil
				whenRequest = nil
			}
			condition, err := workflow.runJQ(ctx, step.when, lastResponse.body, whenResponse, whenRequest, aliases, runtime)
			if err != nil {
				return GeneralWorkflowResult{}, &GeneralWorkflowError{StepID: step.definition.ID, Phase: "evaluate when after extract", Err: err}
			}
			matched, ok := condition.(bool)
			if !ok {
				return GeneralWorkflowResult{}, &GeneralWorkflowError{StepID: step.definition.ID, Phase: "when after extract", Err: fmt.Errorf("expression returned %s instead of boolean", generalWorkflowJSONType(condition))}
			}
			if matched {
				workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, "info", "step_goto", "step condition matched after alias extraction; jumping to target", map[string]any{
					"expression":      step.definition.When.Expression,
					"result":          true,
					"response_status": lastResponse.envelope["status"],
					"goto":            step.definition.When.Goto,
				}, time.Since(stepStartedAt).Milliseconds()))
				stepIndex = step.whenGotoIndex
				continue
			}
			workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, "debug", "step_when", "step condition remained false after alias extraction", map[string]any{
				"expression": step.definition.When.Expression,
				"result":     false,
			}, time.Since(stepStartedAt).Milliseconds()))
		}
		workflow.emitDebug(options.DebugLog, generalWorkflowDebugWith(stepLog, "info", "step_complete", "step execution completed", map[string]any{
			"alias_count": len(aliases),
		}, time.Since(stepStartedAt).Milliseconds()))
		stepIndex++
	}

	output, err := renderGeneralWorkflowTemplate(compiled.output, generalWorkflowTemplateValues(aliases, runtime))
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
			"alias_count":     len(resultAliases),
			"step_count":      len(compiled.steps),
			"execution_count": executionCount,
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

func sortedGeneralWorkflowKeysFromSlices(values map[string][]any) []string {
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
	runtime := map[string]any{
		"username": "",
		"password": "",
		"user_id":  "",
		"headers":  map[string]any{},
	}
	if values != nil {
		normalized, err := canonicalGeneralWorkflowJSON(values, "runtime")
		if err != nil {
			return nil, err
		}
		provided, ok := normalized.(map[string]any)
		if !ok {
			return nil, errors.New("runtime must be an object")
		}
		for name, value := range provided {
			runtime[name] = value
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

func (workflow *GeneralWorkflow) renderRequest(globalHeaderDefinition map[string]any, definition GeneralWorkflowRequest, aliases, runtime map[string]any, baseHeaders http.Header) (renderedGeneralWorkflowRequest, error) {
	templateValues := generalWorkflowTemplateValues(aliases, runtime)
	renderedPath, err := renderGeneralWorkflowTemplate(definition.Path, templateValues)
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
		rendered, err := renderGeneralWorkflowTemplate(queryTemplate, templateValues)
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

	globalHeaders, err := renderGeneralWorkflowHeaderTemplate(globalHeaderDefinition, templateValues, "workflow headers")
	if err != nil {
		return renderedGeneralWorkflowRequest{}, err
	}
	workflowHeaders := map[string]any{}
	if definition.Headers != nil {
		workflowHeaders, err = renderGeneralWorkflowHeaderTemplate(definition.Headers, templateValues, "headers")
		if err != nil {
			return renderedGeneralWorkflowRequest{}, err
		}
	}
	workflowHeaders, err = mergeGeneralWorkflowHeaderValues(globalHeaders, workflowHeaders)
	if err != nil {
		return renderedGeneralWorkflowRequest{}, err
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
		request.body, err = renderGeneralWorkflowTemplate(bodyTemplate, templateValues)
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

func renderGeneralWorkflowHeaderTemplate(definition map[string]any, templateValues map[string]any, label string) (map[string]any, error) {
	if definition == nil {
		return map[string]any{}, nil
	}
	headerTemplate, err := canonicalGeneralWorkflowJSON(definition, label)
	if err != nil {
		return nil, err
	}
	rendered, err := renderGeneralWorkflowTemplate(headerTemplate, templateValues)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	headers, ok := rendered.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must render as an object", label)
	}
	return headers, nil
}

func mergeGeneralWorkflowHeaderValues(global, step map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(global)+len(step))
	names := make(map[string]string, len(global)+len(step))
	for _, source := range []struct {
		label  string
		values map[string]any
	}{{label: "workflow", values: global}, {label: "step", values: step}} {
		seen := make(map[string]struct{}, len(source.values))
		for name, value := range source.values {
			lower := strings.ToLower(name)
			if _, duplicate := seen[lower]; duplicate {
				return nil, fmt.Errorf("duplicate %s header %q", source.label, name)
			}
			seen[lower] = struct{}{}
			if previous, exists := names[lower]; exists {
				delete(result, previous)
			}
			result[name] = value
			names[lower] = name
		}
	}
	return result, nil
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

func (workflow *GeneralWorkflow) doRequest(ctx context.Context, client *http.Client, baseURL *url.URL, rendered renderedGeneralWorkflowRequest, recorder ConsoleRequestRecorder) (generalWorkflowHTTPResponse, error) {
	target, err := buildGeneralWorkflowTargetURL(baseURL, rendered.path, rendered.query)
	if err != nil {
		return generalWorkflowHTTPResponse{}, err
	}
	request, err := http.NewRequestWithContext(ctx, rendered.method, target.String(), bytes.NewReader(rendered.bodyBytes))
	if err != nil {
		return generalWorkflowHTTPResponse{}, err
	}
	request.Header = rendered.headers.Clone()
	response, err := client.Do(request)
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

func (workflow *GeneralWorkflow) newRunHTTPClient(baseURL *url.URL, baseHeaders http.Header) (*http.Client, *generalWorkflowCookieJar, error) {
	jar, err := newGeneralWorkflowCookieJar()
	if err != nil {
		return nil, nil, err
	}
	if baseURL != nil {
		cookieRequest := &http.Request{Header: http.Header{"Cookie": append([]string(nil), baseHeaders.Values("Cookie")...)}}
		cookies := cookieRequest.Cookies()
		for _, cookie := range cookies {
			cookie.Path = "/"
		}
		jar.seed(baseURL, cookies)
	}
	baseHeaders.Del("Cookie")
	client := *workflow.httpClient
	client.Jar = jar
	return &client, jar, nil
}

func applyGeneralWorkflowCookiePreview(baseURL *url.URL, rendered *renderedGeneralWorkflowRequest, jar http.CookieJar) error {
	if baseURL == nil || rendered == nil || jar == nil {
		return nil
	}
	target, err := buildGeneralWorkflowTargetURL(baseURL, rendered.path, rendered.query)
	if err != nil {
		return err
	}
	request := &http.Request{Header: rendered.headers.Clone()}
	for _, cookie := range jar.Cookies(target) {
		request.AddCookie(cookie)
	}
	rendered.jqValue["headers"] = generalWorkflowHeaderObject(request.Header)
	return nil
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

func generalWorkflowTemplateValues(aliases, runtime map[string]any) map[string]any {
	values := make(map[string]any, len(aliases)+1)
	for name, value := range aliases {
		values[name] = value
	}
	values["runtime"] = runtime
	return values
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
	if alias != "runtime" {
		if err := validateGeneralWorkflowAlias(alias); err != nil {
			return generalWorkflowTemplateReference{}, err
		}
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
