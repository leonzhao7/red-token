package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"red-token/internal/config"
	"red-token/internal/domain"
	proxypkg "red-token/internal/proxy"
)

const newAPITokenListPath = "/api/token/?p=1&size=20"

type PlatformNewAPIOptions struct {
	HTTPClient *http.Client
	UserAgent  string
	Logger     *slog.Logger
}

// PlatformNewAPI owns all HTTP calls to a new-api management console.
type PlatformNewAPI struct {
	httpClient *http.Client
	userAgent  string
	logger     *slog.Logger
}

func NewPlatformNewAPI(options PlatformNewAPIOptions) *PlatformNewAPI {
	userAgent := strings.TrimSpace(options.UserAgent)
	if userAgent == "" {
		userAgent = config.DefaultBackendConsoleUserAgent
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default().With("component", "platform_newapi")
	}
	return &PlatformNewAPI{
		httpClient: options.HTTPClient,
		userAgent:  userAgent,
		logger:     logger,
	}
}

type NewAPIResult struct {
	StatusCode int
	Header     http.Header
	Raw        string
	Payload    map[string]any
}

func (r NewAPIResult) Success() bool {
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return false
	}
	if value, ok := r.Payload["success"].(bool); ok {
		return value
	}
	return true
}

func (r NewAPIResult) LoginRequired() bool {
	if r.StatusCode == http.StatusUnauthorized || r.StatusCode == http.StatusForbidden {
		return true
	}
	if r.Success() {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(fmt.Sprint(r.Payload["message"])))
	return strings.Contains(message, "未登录") ||
		strings.Contains(message, "登录") ||
		strings.Contains(message, "login") ||
		strings.Contains(message, "not logged") ||
		strings.Contains(message, "unauthorized")
}

func (r NewAPIResult) AlreadyCheckedIn() bool {
	message := strings.ToLower(strings.TrimSpace(fmt.Sprint(r.Payload["message"])))
	return strings.Contains(message, "已签到") ||
		strings.Contains(message, "已经签到") ||
		strings.Contains(message, "今日已签") ||
		strings.Contains(message, "今天已签") ||
		(strings.Contains(message, "already") && (strings.Contains(message, "check") || strings.Contains(message, "sign")))
}

func (r NewAPIResult) ErrorMessage(fallback string) string {
	message := strings.TrimSpace(fmt.Sprint(r.Payload["message"]))
	if message != "" && message != "<nil>" {
		return message
	}
	return fallback
}

type NewAPIRequestLog struct {
	Time       string `json:"time"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
}

type NewAPIRequestRecorder struct {
	Requests []NewAPIRequestLog `json:"requests"`
	onRecord func(NewAPIRequestLog)
}

func NewNewAPIRequestRecorder(onRecord ...func(NewAPIRequestLog)) *NewAPIRequestRecorder {
	recorder := &NewAPIRequestRecorder{Requests: []NewAPIRequestLog{}}
	if len(onRecord) > 0 {
		recorder.onRecord = onRecord[0]
	}
	return recorder
}

func (r *NewAPIRequestRecorder) Record(method, path string, statusCode int, body string) {
	if r == nil {
		return
	}
	entry := NewAPIRequestLog{
		Time:       time.Now().UTC().Format(time.RFC3339Nano),
		Method:     method,
		Path:       path,
		StatusCode: statusCode,
		Body:       body,
	}
	r.Requests = append(r.Requests, entry)
	if r.onRecord != nil {
		r.onRecord(entry)
	}
}

func (p *PlatformNewAPI) Status(ctx context.Context, backend domain.Backend, recorder *NewAPIRequestRecorder) (NewAPIResult, error) {
	return p.doJSON(ctx, backend, http.MethodGet, "/api/status", nil, NewAPIConsoleHeaders(backend), "", recorder)
}

func (p *PlatformNewAPI) Self(ctx context.Context, backend domain.Backend, accountID string, recorder *NewAPIRequestRecorder) (NewAPIResult, error) {
	return p.doJSON(ctx, backend, http.MethodGet, "/api/user/self", nil, NewAPIConsoleHeaders(backend), accountID, recorder)
}

func (p *PlatformNewAPI) Checkin(ctx context.Context, backend domain.Backend, accountID string, recorder *NewAPIRequestRecorder) (NewAPIResult, error) {
	return p.doJSON(ctx, backend, http.MethodPost, "/api/user/checkin", nil, NewAPIConsoleHeaders(backend), accountID, recorder)
}

func (p *PlatformNewAPI) Pricing(ctx context.Context, backend domain.Backend, accountID string, recorder *NewAPIRequestRecorder) (NewAPIResult, error) {
	return p.doJSON(ctx, backend, http.MethodGet, "/api/pricing", nil, NewAPIConsoleHeaders(backend), accountID, recorder)
}

func (p *PlatformNewAPI) Login(ctx context.Context, backend domain.Backend, recorder *NewAPIRequestRecorder) (NewAPIResult, error) {
	if strings.TrimSpace(backend.ConsoleUsername) == "" || strings.TrimSpace(backend.ConsolePassword) == "" {
		return NewAPIResult{}, errors.New("console username and password are required")
	}
	body, err := json.Marshal(struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}{
		Username: strings.TrimSpace(backend.ConsoleUsername),
		Password: strings.TrimSpace(backend.ConsolePassword),
	})
	if err != nil {
		return NewAPIResult{}, err
	}
	return p.doJSON(ctx, backend, http.MethodPost, "/api/user/login", body, NewAPIConsoleHeaders(backend), "", recorder)
}

func (p *PlatformNewAPI) Refresh(ctx context.Context, backend domain.Backend, recorder *NewAPIRequestRecorder) (NewAPIResult, error) {
	if !HasNewAPIConsoleRefreshCredentials(backend) {
		return NewAPIResult{}, errors.New("console Cookie and new_api_refresh are required when username is empty")
	}
	return p.doJSON(ctx, backend, http.MethodPost, "/api/user/auth/refresh", nil, NewAPIConsoleRefreshHeaders(backend), "", recorder)
}

func (p *PlatformNewAPI) SyncTokens(ctx context.Context, backend domain.Backend, accountID string, recorder *NewAPIRequestRecorder) ([]domain.BackendAPIKey, error) {
	listResult, err := p.doJSON(ctx, backend, http.MethodGet, newAPITokenListPath, nil, NewAPIConsoleHeaders(backend), accountID, recorder)
	if err != nil {
		return nil, err
	}
	if !listResult.Success() {
		return nil, errors.New(listResult.ErrorMessage("new-api token list request failed"))
	}
	metadata, err := newAPITokenMetadataItems(listResult.Payload)
	if err != nil {
		return nil, err
	}

	tokens := make([]newAPIToken, 0, len(metadata))
	for _, item := range metadata {
		apiKey, complete := newAPIUnmaskedTokenKey(item.Key)
		if !complete {
			keyPath := fmt.Sprintf("/api/token/%d/key", item.ID)
			keyResult, err := p.doJSON(ctx, backend, http.MethodPost, keyPath, nil, NewAPIConsoleHeaders(backend), accountID, recorder)
			if err != nil {
				return nil, err
			}
			if !keyResult.Success() {
				return nil, errors.New(keyResult.ErrorMessage("new-api token key request failed"))
			}
			apiKey, err = newAPITokenKey(keyResult.Payload)
			if err != nil {
				return nil, fmt.Errorf("new-api token %d: %w", item.ID, err)
			}
		}
		tokens = append(tokens, newAPIToken{
			ID:        strconv.FormatInt(item.ID, 10),
			APIKey:    apiKey,
			Name:      item.Name,
			Group:     item.Group,
			UsedQuota: item.UsedQuota,
		})
	}
	return mergeNewAPITokens(backend.APIKeys, tokens), nil
}

func (p *PlatformNewAPI) doJSON(ctx context.Context, backend domain.Backend, method, path string, body []byte, headers map[string]string, newAPIUser string, recorder *NewAPIRequestRecorder) (NewAPIResult, error) {
	target := newAPIURL(backend.ConsoleURL, path)
	request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		p.log(ctx, slog.LevelWarn, "newapi_console_request_build_failed", append(newAPIRequestAttrs(backend, method, path, body, headers, newAPIUser),
			slog.String("error", err.Error()),
		)...)
		return NewAPIResult{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", p.userAgent)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	if newAPIUser = strings.TrimSpace(newAPIUser); newAPIUser != "" {
		request.Header.Set("new-user-id", newAPIUser)
	}
	defer p.sleepAfterRequest(ctx, backend, method, path)

	client, err := p.clientForBackend(backend)
	if err != nil {
		recorder.Record(method, path, 0, err.Error())
		return NewAPIResult{}, err
	}
	startedAt := time.Now()
	p.log(ctx, slog.LevelInfo, "newapi_console_request_started", newAPIRequestAttrs(backend, method, path, body, headers, newAPIUser)...)
	response, err := client.Do(request)
	if err != nil {
		recorder.Record(method, path, 0, err.Error())
		p.log(ctx, slog.LevelWarn, "newapi_console_request_failed", append(newAPIRequestAttrs(backend, method, path, body, headers, newAPIUser),
			slog.Duration("duration", time.Since(startedAt)),
			slog.String("error", err.Error()),
		)...)
		return NewAPIResult{}, err
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		recorder.Record(method, path, response.StatusCode, err.Error())
		p.log(ctx, slog.LevelWarn, "newapi_console_response_read_failed", append(newAPIRequestAttrs(backend, method, path, body, headers, newAPIUser),
			slog.Int("http_status", response.StatusCode),
			slog.String("http_status_text", response.Status),
			slog.String("content_type", response.Header.Get("Content-Type")),
			slog.Duration("duration", time.Since(startedAt)),
			slog.String("error", err.Error()),
		)...)
		return NewAPIResult{}, err
	}
	raw := compactNewAPIJSON(responseBody)
	recordedRaw := redactedNewAPIResponse(path, responseBody, raw)
	recorder.Record(method, path, response.StatusCode, recordedRaw)
	var payload map[string]any
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		p.log(ctx, slog.LevelWarn, "newapi_console_response_decode_failed", append(newAPIRequestAttrs(backend, method, path, body, headers, newAPIUser),
			slog.Int("http_status", response.StatusCode),
			slog.String("http_status_text", response.Status),
			slog.String("content_type", response.Header.Get("Content-Type")),
			slog.Int("response_bytes", len(responseBody)),
			slog.Duration("duration", time.Since(startedAt)),
			slog.String("error", err.Error()),
		)...)
		return NewAPIResult{}, fmt.Errorf("decode new-api console response: %w", err)
	}
	result := NewAPIResult{
		StatusCode: response.StatusCode,
		Header:     response.Header.Clone(),
		Raw:        recordedRaw,
		Payload:    payload,
	}
	p.log(ctx, slog.LevelInfo, "newapi_console_request_finished", append(append(newAPIRequestAttrs(backend, method, path, body, headers, newAPIUser),
		slog.Int("http_status", response.StatusCode),
		slog.String("http_status_text", response.Status),
		slog.String("content_type", response.Header.Get("Content-Type")),
		slog.Int("response_bytes", len(responseBody)),
		slog.Duration("duration", time.Since(startedAt)),
	), newAPIResultAttrs(result)...)...)
	return result, nil
}

func (p *PlatformNewAPI) clientForBackend(backend domain.Backend) (*http.Client, error) {
	if p.httpClient != nil {
		return p.httpClient, nil
	}
	return proxypkg.NewHTTPClientForBackend(backend, 30*time.Second, 30*time.Second)
}

func (p *PlatformNewAPI) sleepAfterRequest(ctx context.Context, backend domain.Backend, method, path string) {
	// Injected clients are used by tests and local probes; they should not incur production pacing.
	if p.httpClient != nil {
		return
	}
	delay := time.Duration(rand.IntN(3)+1) * time.Second
	p.log(ctx, slog.LevelInfo, "newapi_console_request_delay_started", append(newAPIBackendAttrs(backend),
		slog.String("method", method),
		slog.String("path", path),
		slog.Duration("delay", delay),
	)...)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func (p *PlatformNewAPI) log(ctx context.Context, level slog.Level, message string, attrs ...slog.Attr) {
	p.logger.LogAttrs(ctx, level, message, attrs...)
}

func newAPIBackendAttrs(backend domain.Backend) []slog.Attr {
	attrs := []slog.Attr{
		slog.Int64("backend_id", backend.ID),
		slog.String("backend_name", backend.Name),
		slog.String("backend_type", domain.NormalizeBackendType(backend.BackendType)),
		slog.String("backend_status", backend.Status),
		slog.Int("console_header_count", len(NewAPIConsoleHeaders(backend))),
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

func newAPIRequestAttrs(backend domain.Backend, method, path string, body []byte, headers map[string]string, newAPIUser string) []slog.Attr {
	newAPIUser = strings.TrimSpace(newAPIUser)
	attrs := append(newAPIBackendAttrs(backend),
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

func newAPIResultAttrs(result NewAPIResult) []slog.Attr {
	attrs := []slog.Attr{
		slog.Int("console_http_status", result.StatusCode),
		slog.Bool("console_success", result.Success()),
		slog.Bool("console_login_required", result.LoginRequired()),
	}
	message := strings.TrimSpace(fmt.Sprint(result.Payload["message"]))
	if message == "" || message == "<nil>" {
		return attrs
	}
	const limit = 200
	if runes := []rune(message); len(runes) > limit {
		message = string(runes[:limit]) + "..."
	}
	return append(attrs, slog.String("console_message", message))
}

type newAPITokenMetadata struct {
	ID        int64
	Key       string
	Name      string
	Group     string
	UsedQuota float64
}

type newAPIToken struct {
	ID        string
	APIKey    string
	Name      string
	Group     string
	UsedQuota float64
}

func newAPITokenMetadataItems(payload map[string]any) ([]newAPITokenMetadata, error) {
	data, ok := payload["data"].(map[string]any)
	if !ok {
		return nil, errors.New("new-api token list response missing data")
	}
	rawItems, ok := data["items"].([]any)
	if !ok {
		return nil, errors.New("new-api token list response missing data.items")
	}
	items := make([]newAPITokenMetadata, 0, len(rawItems))
	for index, rawItem := range rawItems {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("new-api token list item %d must be an object", index)
		}
		id, ok := nonNegativeNewAPIInteger(item["id"])
		if !ok || id <= 0 {
			return nil, fmt.Errorf("new-api token list item %d has invalid id", index)
		}
		usedQuota, ok := nonNegativeNewAPINumber(item["used_quota"])
		if !ok {
			return nil, fmt.Errorf("new-api token list item %d has invalid used_quota", index)
		}
		name, _ := item["name"].(string)
		group := strings.TrimSpace(fmt.Sprint(item["group"]))
		if group == "" || group == "<nil>" {
			group = "default"
		}
		key, _ := item["key"].(string)
		items = append(items, newAPITokenMetadata{
			ID:        id,
			Key:       strings.TrimSpace(key),
			Name:      strings.TrimSpace(name),
			Group:     group,
			UsedQuota: usedQuota,
		})
	}
	return items, nil
}

func newAPIUnmaskedTokenKey(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "*") {
		return "", false
	}
	if !strings.HasPrefix(value, "sk-") {
		value = "sk-" + value
	}
	return value, true
}

func newAPITokenKey(payload map[string]any) (string, error) {
	data, ok := payload["data"].(map[string]any)
	if !ok {
		return "", errors.New("token key response missing data")
	}
	apiKey, _ := data["key"].(string)
	if apiKey = strings.TrimSpace(apiKey); apiKey == "" {
		return "", errors.New("token key response missing data.key")
	}
	if !strings.HasPrefix(apiKey, "sk-") {
		apiKey = "sk-" + apiKey
	}
	return apiKey, nil
}

func nonNegativeNewAPIInteger(value any) (int64, bool) {
	switch value := value.(type) {
	case float64:
		if value < 0 {
			return 0, false
		}
		integer := int64(value)
		return integer, float64(integer) == value
	case int:
		if value < 0 {
			return 0, false
		}
		return int64(value), true
	case int64:
		return value, value >= 0
	case json.Number:
		integer, err := value.Int64()
		return integer, err == nil && integer >= 0
	default:
		return 0, false
	}
}

func nonNegativeNewAPINumber(value any) (float64, bool) {
	var number float64
	switch value := value.(type) {
	case float64:
		number = value
	case int:
		number = float64(value)
	case int64:
		number = float64(value)
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return 0, false
		}
		number = parsed
	default:
		return 0, false
	}
	return number, number >= 0 && !math.IsNaN(number) && !math.IsInf(number, 0)
}

func mergeNewAPITokens(existing []domain.BackendAPIKey, tokens []newAPIToken) []domain.BackendAPIKey {
	merged := append([]domain.BackendAPIKey(nil), existing...)
	indexByKey := make(map[string]int, len(merged))
	for index, item := range merged {
		if apiKey := strings.TrimSpace(item.APIKey); apiKey != "" {
			indexByKey[apiKey] = index
		}
	}
	for _, token := range tokens {
		apiKey := strings.TrimSpace(token.APIKey)
		if index, ok := indexByKey[apiKey]; ok {
			merged[index].ID = token.ID
			merged[index].Name = token.Name
			merged[index].Group = token.Group
			merged[index].UsedQuota = token.UsedQuota
			continue
		}
		indexByKey[apiKey] = len(merged)
		merged = append(merged, domain.BackendAPIKey{
			ID:           token.ID,
			APIKey:       apiKey,
			Name:         token.Name,
			Group:        token.Group,
			Models:       []string{},
			ModelMapping: map[string]string{},
			UsedQuota:    token.UsedQuota,
		})
	}
	return merged
}

func NewAPIStatusCheckinEnabled(statusPayload map[string]any) bool {
	statusData, ok := statusPayload["data"].(map[string]any)
	if !ok {
		return false
	}
	enabled, ok := statusData["checkin_enabled"].(bool)
	return ok && enabled
}

func NewAPIConsoleHeaders(backend domain.Backend) map[string]string {
	headers := make(map[string]string, len(backend.ConsoleHeaders)+1)
	for key, value := range backend.ConsoleHeaders {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" {
			headers[http.CanonicalHeaderKey(key)] = value
		}
	}
	if len(headers) == 0 {
		if cookie := strings.TrimSpace(backend.ConsoleCookie); cookie != "" {
			headers["Cookie"] = cookie
		}
	}
	return headers
}

func NewAPIConsoleCookieValue(backend domain.Backend) string {
	for key, value := range NewAPIConsoleHeaders(backend) {
		if strings.EqualFold(key, "Cookie") {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// ConsoleHeadersWithResponseCookies applies Set-Cookie mutations to the flat
// Cookie request header stored in a backend configuration. Other configured
// headers are preserved unchanged.
func ConsoleHeadersWithResponseCookies(headers map[string]string, cookies []*http.Cookie, now time.Time) map[string]string {
	out := make(map[string]string, len(headers)+1)
	cookieValue := ""
	for key, value := range headers {
		if strings.EqualFold(key, "Cookie") {
			cookieValue = value
			continue
		}
		out[http.CanonicalHeaderKey(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	for _, cookie := range cookies {
		if cookie == nil || strings.TrimSpace(cookie.Name) == "" {
			continue
		}
		if cookie.MaxAge < 0 || !cookie.Expires.IsZero() && !cookie.Expires.After(now) {
			cookieValue = removeNewAPICookieValue(cookieValue, cookie.Name)
			continue
		}
		cookieValue = mergeNewAPICookieValue(cookieValue, cookie)
	}
	if cookieValue = strings.TrimSpace(cookieValue); cookieValue != "" {
		out["Cookie"] = cookieValue
	}
	return out
}

func NewAPIConsoleRefreshHeaders(backend domain.Backend) map[string]string {
	headers := NewAPIConsoleHeaders(backend)
	out := make(map[string]string, len(headers))
	cookieValue := ""
	for key, value := range headers {
		switch {
		case strings.EqualFold(key, "Authorization"):
			continue
		case strings.EqualFold(key, "Cookie"):
			cookieValue = value
		default:
			out[key] = value
		}
	}
	cookieValue = mergeNewAPICookieValue(cookieValue, &http.Cookie{Name: "new_api_refresh", Value: strings.TrimSpace(backend.NewAPIRefresh)})
	if cookieValue != "" {
		out["Cookie"] = cookieValue
	}
	return out
}

func NewAPIConsoleLoginHeaders(headers map[string]string, cookies []*http.Cookie, accessToken string) map[string]string {
	out := make(map[string]string, len(headers)+2)
	cookieValue := ""
	for key, value := range headers {
		switch {
		case strings.EqualFold(key, "Authorization"):
			continue
		case strings.EqualFold(key, "Cookie"):
			cookieValue = value
		default:
			out[key] = value
		}
	}
	for _, cookie := range cookies {
		cookieValue = mergeNewAPICookieValue(cookieValue, cookie)
	}
	if cookieValue = strings.TrimSpace(cookieValue); cookieValue != "" {
		out["Cookie"] = cookieValue
	}
	if accessToken = strings.TrimSpace(accessToken); accessToken != "" {
		if strings.HasPrefix(strings.ToLower(accessToken), "bearer ") {
			out["Authorization"] = accessToken
		} else {
			out["Authorization"] = "Bearer " + accessToken
		}
	}
	return out
}

func NewAPIResponseCookies(header http.Header) []*http.Cookie {
	response := http.Response{Header: header}
	return response.Cookies()
}

func SplitNewAPIRefreshCookie(cookies []*http.Cookie) ([]*http.Cookie, string) {
	ordinary := make([]*http.Cookie, 0, len(cookies))
	refresh := ""
	for _, cookie := range cookies {
		if cookie != nil && cookie.Name == "new_api_refresh" {
			refresh = strings.TrimSpace(cookie.Value)
			continue
		}
		ordinary = append(ordinary, cookie)
	}
	return ordinary, refresh
}

func NewAPIAccountID(payload map[string]any) string {
	data, ok := payload["data"].(map[string]any)
	if !ok {
		return ""
	}
	if accountID := newAPIIDValue(data["id"]); accountID != "" {
		return accountID
	}
	user, _ := data["user"].(map[string]any)
	return newAPIIDValue(user["id"])
}

func NewAPIAccessToken(payload map[string]any) string {
	data, ok := payload["data"].(map[string]any)
	if !ok {
		return ""
	}
	accessToken, _ := data["access_token"].(string)
	return strings.TrimSpace(accessToken)
}

func HasNewAPIConsoleLoginCredentials(backend domain.Backend) bool {
	return strings.TrimSpace(backend.ConsoleUsername) != "" && strings.TrimSpace(backend.ConsolePassword) != ""
}

func HasNewAPIConsoleRefreshCredentials(backend domain.Backend) bool {
	return strings.TrimSpace(backend.ConsoleUsername) == "" &&
		strings.TrimSpace(backend.NewAPIRefresh) != "" &&
		NewAPIConsoleCookieValue(backend) != ""
}

func newAPIURL(consoleURL, path string) string {
	base := strings.TrimRight(strings.TrimSpace(consoleURL), "/")
	path = normalizeNewAPIPath(path)
	if path == "" {
		return base
	}
	return base + path
}

func normalizeNewAPIPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	return value
}

func compactNewAPIJSON(data []byte) string {
	var out bytes.Buffer
	if err := json.Compact(&out, data); err != nil {
		return strings.TrimSpace(string(data))
	}
	return out.String()
}

func redactedNewAPIResponse(path string, body []byte, fallback string) string {
	normalizedPath := normalizeNewAPIPath(path)
	pathWithoutQuery := strings.SplitN(normalizedPath, "?", 2)[0]
	var sensitiveKeys []string
	tokenKeyResponse := false
	switch {
	case pathWithoutQuery == "/api/user/login", pathWithoutQuery == "/api/user/auth/refresh":
		sensitiveKeys = []string{"access_token", "refresh_token"}
	case strings.HasPrefix(pathWithoutQuery, "/api/token/") && strings.HasSuffix(pathWithoutQuery, "/key"):
		sensitiveKeys = []string{"key"}
		tokenKeyResponse = true
	default:
		return fallback
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		if tokenKeyResponse {
			return "[redacted]"
		}
		return fallback
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		if tokenKeyResponse {
			return "[redacted]"
		}
		return fallback
	}
	for _, key := range sensitiveKeys {
		if _, exists := data[key]; exists {
			data[key] = "[redacted]"
		}
	}
	redacted, err := json.Marshal(payload)
	if err != nil {
		if tokenKeyResponse {
			return "[redacted]"
		}
		return fallback
	}
	return string(redacted)
}

func mergeNewAPICookieValue(raw string, cookie *http.Cookie) string {
	if cookie == nil || strings.TrimSpace(cookie.Name) == "" {
		return strings.TrimSpace(raw)
	}
	replacement := cookie.Name + "=" + cookie.Value
	parts := strings.Split(raw, ";")
	out := make([]string, 0, len(parts)+1)
	replaced := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, _, ok := strings.Cut(part, "=")
		if ok && strings.TrimSpace(name) == cookie.Name {
			out = append(out, replacement)
			replaced = true
			continue
		}
		out = append(out, part)
	}
	if !replaced {
		out = append(out, replacement)
	}
	return strings.Join(out, "; ")
}

func removeNewAPICookieValue(raw, cookieName string) string {
	out := make([]string, 0)
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, _, ok := strings.Cut(part, "=")
		if ok && strings.TrimSpace(name) == cookieName {
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, "; ")
}

func newAPIIDValue(value any) string {
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
