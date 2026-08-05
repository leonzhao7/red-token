package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"red-token/internal/config"
	"red-token/internal/domain"
	proxypkg "red-token/internal/proxy"
)

var ErrSub2APIConsoleAuthorizationRequired = errors.New("console_authorization is required")

type PlatformSub2APIOptions struct {
	HTTPClient *http.Client
	UserAgent  string
	Now        func() time.Time
}

// PlatformSub2API owns the console synchronization workflow for sub2api backends.
type PlatformSub2API struct {
	httpClient *http.Client
	userAgent  string
	now        func() time.Time
}

func NewPlatformSub2API(options PlatformSub2APIOptions) *PlatformSub2API {
	userAgent := strings.TrimSpace(options.UserAgent)
	if userAgent == "" {
		userAgent = config.DefaultBackendConsoleUserAgent
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &PlatformSub2API{
		httpClient: options.HTTPClient,
		userAgent:  userAgent,
		now:        now,
	}
}

type ConsoleRequestRecorder interface {
	Record(method, path string, statusCode int, body string)
}

type Sub2APIResult struct {
	StatusCode int
	Raw        string
	Payload    map[string]any
}

func (r Sub2APIResult) Success() bool {
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return false
	}
	switch value := r.Payload["code"].(type) {
	case float64:
		return value == 0
	case int:
		return value == 0
	case int64:
		return value == 0
	case json.Number:
		code, err := value.Int64()
		return err == nil && code == 0
	case string:
		return strings.TrimSpace(value) == "0"
	default:
		return true
	}
}

func (r Sub2APIResult) ErrorMessage(fallback string) string {
	message := strings.TrimSpace(fmt.Sprint(r.Payload["message"]))
	if message != "" && message != "<nil>" {
		return message
	}
	return fallback
}

type Sub2APISyncResult struct {
	Backend domain.Backend
	Checkin map[string]any
	Account map[string]any
	Pricing map[string]any
}

func (p *PlatformSub2API) Sync(ctx context.Context, backend domain.Backend, recorder ConsoleRequestRecorder) (Sub2APISyncResult, error) {
	if strings.TrimSpace(backend.ConsoleAuthorization) == "" {
		return Sub2APISyncResult{}, ErrSub2APIConsoleAuthorizationRequired
	}

	now := p.now()
	lastCheckinAt, checkedInToday := sub2APILastCheckinStatus(backend.ConsoleAccountJSON, now)
	var (
		checkinPayload map[string]any
		pricingPayload map[string]any
	)
	recordSyncCompletionAsCheckin := normalizeSub2APIPath(backend.ConsoleCheckinPath) == ""
	if checkinPath := normalizeSub2APIPath(backend.ConsoleCheckinPath); checkinPath != "" && !checkedInToday {
		checkinResult, err := p.Checkin(ctx, backend, checkinPath, recorder)
		if err != nil {
			return Sub2APISyncResult{}, err
		}
		if !checkinResult.Success() {
			return Sub2APISyncResult{}, errors.New(checkinResult.ErrorMessage("sub2api checkin failed"))
		}
		checkinPayload = checkinResult.Payload
		lastCheckinAt = p.now().UTC()
	}

	accountResult, err := p.Account(ctx, backend, recorder)
	if err != nil {
		return Sub2APISyncResult{}, err
	}
	if !accountResult.Success() {
		return Sub2APISyncResult{}, errors.New(accountResult.ErrorMessage("sub2api auth/me request failed"))
	}
	usageResult, err := p.UsageStats(ctx, backend, now, recorder)
	if err != nil {
		return Sub2APISyncResult{}, err
	}
	if !usageResult.Success() {
		return Sub2APISyncResult{}, errors.New(usageResult.ErrorMessage("sub2api usage stats request failed"))
	}
	totalActualCost, err := sub2APITotalActualCost(usageResult.Payload)
	if err != nil {
		return Sub2APISyncResult{}, err
	}

	account, accountJSON, err := sub2APIAccountSummary(accountResult.Payload, backend.ConsoleAccountJSON, lastCheckinAt, totalActualCost)
	if err != nil {
		return Sub2APISyncResult{}, err
	}
	backend.ConsoleAccountJSON = accountJSON

	if channelPath := normalizeSub2APIPath(backend.ChannelURL); channelPath != "" {
		channelResult, err := p.Channel(ctx, backend, channelPath, recorder)
		if err != nil {
			return Sub2APISyncResult{}, err
		}
		if !channelResult.Success() {
			return Sub2APISyncResult{}, errors.New(channelResult.ErrorMessage("sub2api channel request failed"))
		}
		pricingPayload = channelResult.Payload
		pricingJSON, err := json.Marshal(pricingPayload)
		if err != nil {
			return Sub2APISyncResult{}, err
		}
		backend.ConsolePricingJSON = string(pricingJSON)
	}
	if recordSyncCompletionAsCheckin {
		lastCheckinAt = p.now().UTC()
		account, accountJSON, err = sub2APIAccountSummary(accountResult.Payload, backend.ConsoleAccountJSON, lastCheckinAt, totalActualCost)
		if err != nil {
			return Sub2APISyncResult{}, err
		}
		backend.ConsoleAccountJSON = accountJSON
	}

	return Sub2APISyncResult{
		Backend: backend,
		Checkin: checkinPayload,
		Account: account,
		Pricing: pricingPayload,
	}, nil
}

func (p *PlatformSub2API) Checkin(ctx context.Context, backend domain.Backend, path string, recorder ConsoleRequestRecorder) (Sub2APIResult, error) {
	return p.doJSON(ctx, backend, http.MethodPost, path, []byte("{}"), recorder)
}

func (p *PlatformSub2API) Account(ctx context.Context, backend domain.Backend, recorder ConsoleRequestRecorder) (Sub2APIResult, error) {
	return p.doJSON(ctx, backend, http.MethodGet, "/api/v1/auth/me", nil, recorder)
}

func (p *PlatformSub2API) UsageStats(ctx context.Context, backend domain.Backend, now time.Time, recorder ConsoleRequestRecorder) (Sub2APIResult, error) {
	endDate := now.AddDate(0, 0, 1).Format("2006-01-02")
	path := "/api/v1/usage/stats?start_date=2026-01-01&end_date=" + endDate
	return p.doJSON(ctx, backend, http.MethodGet, path, nil, recorder)
}

func (p *PlatformSub2API) Channel(ctx context.Context, backend domain.Backend, path string, recorder ConsoleRequestRecorder) (Sub2APIResult, error) {
	return p.doJSON(ctx, backend, http.MethodGet, path, nil, recorder)
}

func (p *PlatformSub2API) doJSON(ctx context.Context, backend domain.Backend, method, path string, body []byte, recorder ConsoleRequestRecorder) (Sub2APIResult, error) {
	normalizedPath := normalizeSub2APIPath(path)
	target := sub2APIURL(backend.ConsoleURL, normalizedPath)
	request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return Sub2APIResult{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", p.userAgent)
	request.Header.Set("Authorization", strings.TrimSpace(backend.ConsoleAuthorization))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	client, err := p.clientForBackend(backend)
	if err != nil {
		recordConsoleRequest(recorder, method, normalizedPath, 0, err.Error())
		return Sub2APIResult{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		recordConsoleRequest(recorder, method, normalizedPath, 0, err.Error())
		return Sub2APIResult{}, err
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		recordConsoleRequest(recorder, method, normalizedPath, response.StatusCode, err.Error())
		return Sub2APIResult{}, err
	}
	raw := compactSub2APIJSON(responseBody)
	recordConsoleRequest(recorder, method, normalizedPath, response.StatusCode, raw)

	var payload map[string]any
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return Sub2APIResult{}, fmt.Errorf("decode sub2api console response: %w", err)
	}
	return Sub2APIResult{
		StatusCode: response.StatusCode,
		Raw:        raw,
		Payload:    payload,
	}, nil
}

func (p *PlatformSub2API) clientForBackend(backend domain.Backend) (*http.Client, error) {
	if p.httpClient != nil {
		return p.httpClient, nil
	}
	return proxypkg.NewHTTPClientForBackend(backend, 30*time.Second, 30*time.Second)
}

func sub2APIAccountSummary(payload map[string]any, existingRaw string, lastCheckinAt time.Time, totalActualCost any) (map[string]any, string, error) {
	data, ok := payload["data"].(map[string]any)
	if !ok {
		return nil, "", errors.New("sub2api auth/me response missing data")
	}
	summary := map[string]any{
		"id":                data["id"],
		"username":          data["username"],
		"email":             data["email"],
		"balance":           data["balance"],
		"total_actual_cost": totalActualCost,
	}
	if !lastCheckinAt.IsZero() {
		summary["last_checkin_at"] = lastCheckinAt.UTC().Format(time.RFC3339)
	} else if value, ok := decodeSub2APIJSONMap(existingRaw)["last_checkin_at"]; ok && value != nil && strings.TrimSpace(fmt.Sprint(value)) != "" {
		summary["last_checkin_at"] = value
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		return nil, "", err
	}
	return summary, string(encoded), nil
}

func sub2APITotalActualCost(payload map[string]any) (any, error) {
	data, ok := payload["data"].(map[string]any)
	if !ok {
		return nil, errors.New("sub2api usage stats response missing data")
	}
	value, ok := data["total_actual_cost"]
	if !ok || value == nil {
		return nil, errors.New("sub2api usage stats response missing total_actual_cost")
	}
	return value, nil
}

func sub2APIURL(consoleURL, path string) string {
	base := strings.TrimRight(strings.TrimSpace(consoleURL), "/")
	if path = normalizeSub2APIPath(path); path != "" {
		return base + path
	}
	return base
}

func normalizeSub2APIPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	return value
}

func compactSub2APIJSON(data []byte) string {
	var out bytes.Buffer
	if err := json.Compact(&out, data); err != nil {
		return strings.TrimSpace(string(data))
	}
	return out.String()
}

func decodeSub2APIJSONMap(raw string) map[string]any {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return map[string]any{}
	}
	return payload
}

func sub2APILastCheckinStatus(raw string, now time.Time) (time.Time, bool) {
	value := strings.TrimSpace(fmt.Sprint(decodeSub2APIJSONMap(raw)["last_checkin_at"]))
	if value == "" || value == "<nil>" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, false
	}
	location := now.Location()
	parsed = parsed.In(location)
	now = now.In(location)
	return parsed, parsed.Year() == now.Year() && parsed.YearDay() == now.YearDay()
}

func recordConsoleRequest(recorder ConsoleRequestRecorder, method, path string, statusCode int, body string) {
	if recorder != nil {
		recorder.Record(method, path, statusCode, body)
	}
}
