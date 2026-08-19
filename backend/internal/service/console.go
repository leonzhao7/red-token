package service

import (
	"net/http"
	"strings"
	"time"

	"red-token/internal/domain"
)

type ConsoleRequestLog struct {
	Time       string `json:"time"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
}

type ConsoleRequestRecorder interface {
	Record(method, path string, statusCode int, body string)
}

type RequestRecorder struct {
	Requests []ConsoleRequestLog `json:"requests"`
	onRecord func(ConsoleRequestLog)
}

func recordConsoleRequest(recorder ConsoleRequestRecorder, method, path string, statusCode int, body string) {
	if recorder != nil {
		recorder.Record(method, path, statusCode, body)
	}
}

func NewRequestRecorder(onRecord ...func(ConsoleRequestLog)) *RequestRecorder {
	recorder := &RequestRecorder{Requests: []ConsoleRequestLog{}}
	if len(onRecord) > 0 {
		recorder.onRecord = onRecord[0]
	}
	return recorder
}

func (r *RequestRecorder) Record(method, path string, statusCode int, body string) {
	if r == nil {
		return
	}
	entry := ConsoleRequestLog{
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

func ConsoleHeaders(backend domain.Backend) map[string]string {
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

// ConsoleHeadersWithCookieValue replaces Cookie and preserves other headers.
func ConsoleHeadersWithCookieValue(headers map[string]string, cookieValue string) map[string]string {
	out := make(map[string]string, len(headers)+1)
	for key, value := range headers {
		if strings.EqualFold(key, "Cookie") {
			continue
		}
		canonicalKey := http.CanonicalHeaderKey(strings.TrimSpace(key))
		if canonicalKey != "" {
			out[canonicalKey] = strings.TrimSpace(value)
		}
	}
	if cookieValue = strings.TrimSpace(cookieValue); cookieValue != "" {
		out["Cookie"] = cookieValue
	}
	return out
}

// ConsoleHeadersWithAuthorizationValue replaces Authorization when a non-empty
// browser value is provided. An empty value preserves the configured header.
func ConsoleHeadersWithAuthorizationValue(headers map[string]string, authorization string) map[string]string {
	authorization = strings.TrimSpace(authorization)
	out := make(map[string]string, len(headers)+1)
	for key, value := range headers {
		if authorization != "" && strings.EqualFold(key, "Authorization") {
			continue
		}
		canonicalKey := http.CanonicalHeaderKey(strings.TrimSpace(key))
		if canonicalKey != "" {
			out[canonicalKey] = strings.TrimSpace(value)
		}
	}
	if authorization != "" {
		out["Authorization"] = authorization
	}
	return out
}

// ConsoleHeadersWithResponseCookies applies Set-Cookie mutations to the flat
// Cookie request header. Other configured headers are preserved.
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
			cookieValue = removeCookieValue(cookieValue, cookie.Name)
			continue
		}
		cookieValue = mergeCookieValue(cookieValue, cookie)
	}
	if cookieValue = strings.TrimSpace(cookieValue); cookieValue != "" {
		out["Cookie"] = cookieValue
	}
	return out
}

func mergeCookieValue(raw string, cookie *http.Cookie) string {
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

func removeCookieValue(raw, cookieName string) string {
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
