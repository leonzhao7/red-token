package service

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const DefaultChromeCDPEndpoint = "http://127.0.0.1:9222"

const chromeCDPOperationTimeout = 15 * time.Second

var defaultChromeCDPHTTPClient = newChromeCDPHTTPClient()

// ChromeCDPCookie is the subset of a Chrome cookie needed to build a request
// Cookie header. The shape matches the Network.getAllCookies response.
type ChromeCDPCookie struct {
	Name    string  `json:"name"`
	Value   string  `json:"value"`
	Domain  string  `json:"domain"`
	Path    string  `json:"path"`
	Expires float64 `json:"expires"`
	Secure  bool    `json:"secure"`
}

// ChromeCDPCredentials contains request credentials imported from a matching
// Chrome page. Authorization is empty when no valid browser-stored access
// token was found.
type ChromeCDPCredentials struct {
	CookieHeader  string
	Authorization string
}

type chromeCDPStorageEntry struct {
	Storage string `json:"storage"`
	Key     string `json:"key"`
	Value   string `json:"value"`
}

const chromeCDPStorageExpression = `(() => {
  const entries = [];
  for (const [name, storage] of [['localStorage', window.localStorage], ['sessionStorage', window.sessionStorage]]) {
    try {
      for (let index = 0; index < storage.length; index += 1) {
        const key = storage.key(index);
        if (key !== null) entries.push({ storage: name, key, value: storage.getItem(key) || '' });
      }
    } catch (_) {}
  }
  return entries;
})()`

type ChromeCDPCookieServiceOptions struct {
	Endpoint    string
	HTTPClient  *http.Client
	DialContext func(context.Context, string, string) (net.Conn, error)
	Now         func() time.Time
}

// ChromeCDPCookieService reads cookies from the Chrome instance exposed by
// the DevTools remote debugging endpoint. It intentionally uses only the
// small CDP/WebSocket subset needed here, so the backend has no browser-side
// runtime dependency.
type ChromeCDPCookieService struct {
	endpoint    string
	httpClient  *http.Client
	dialContext func(context.Context, string, string) (net.Conn, error)
	now         func() time.Time
}

func NewChromeCDPCookieService(options ChromeCDPCookieServiceOptions) *ChromeCDPCookieService {
	endpoint := strings.TrimSpace(options.Endpoint)
	if endpoint == "" {
		endpoint = DefaultChromeCDPEndpoint
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = defaultChromeCDPHTTPClient
	}
	dialContext := options.DialContext
	if dialContext == nil {
		dialer := &net.Dialer{Timeout: 5 * time.Second}
		dialContext = dialer.DialContext
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &ChromeCDPCookieService{
		endpoint:    endpoint,
		httpClient:  httpClient,
		dialContext: dialContext,
		now:         now,
	}
}

func newChromeCDPHTTPClient() *http.Client {
	transport := http.DefaultTransport
	if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
		directTransport := defaultTransport.Clone()
		directTransport.Proxy = nil
		transport = directTransport
	}
	return &http.Client{Transport: transport}
}

// ReadCredentials returns request credentials visible to targetURL. targetURL
// is the configured relay console URL.
func (s *ChromeCDPCookieService) ReadCredentials(ctx context.Context, targetURL string) (ChromeCDPCredentials, error) {
	if s == nil {
		return ChromeCDPCredentials{}, errors.New("chrome CDP service is nil")
	}
	ctx, cancel := context.WithTimeout(ctx, chromeCDPOperationTimeout)
	defer cancel()
	var pageFailures []string
	for _, endpoint := range s.endpointCandidates() {
		cookies, _, authorization, err := s.readCredentialsFromPage(ctx, endpoint, targetURL)
		if err != nil {
			pageFailures = append(pageFailures, endpoint+": "+err.Error())
			continue
		}
		if cookies != nil {
			return ChromeCDPCredentials{
				CookieHeader:  CookieHeaderForURL(targetURL, cookies, s.now()),
				Authorization: authorization,
			}, nil
		}
	}
	// Older or restricted CDP proxies may expose /json/version but not
	// /json/list. Storage.getCookies is available on the browser WebSocket.
	cookies, err := s.ReadCookies(ctx)
	if err != nil {
		if len(pageFailures) > 0 {
			return ChromeCDPCredentials{}, fmt.Errorf("%w (page targets: %s)", err, strings.Join(pageFailures, "; "))
		}
		return ChromeCDPCredentials{}, err
	}
	return ChromeCDPCredentials{CookieHeader: CookieHeaderForURL(targetURL, cookies, s.now())}, nil
}

// ReadCookieHeader is retained for callers that only need browser cookies.
func (s *ChromeCDPCookieService) ReadCookieHeader(ctx context.Context, targetURL string) (string, error) {
	credentials, err := s.ReadCredentials(ctx, targetURL)
	return credentials.CookieHeader, err
}

// ReadCookies obtains all cookies from Chrome's browser CDP endpoint.
func (s *ChromeCDPCookieService) ReadCookies(ctx context.Context) ([]ChromeCDPCookie, error) {
	if s == nil {
		return nil, errors.New("chrome CDP service is nil")
	}
	ctx, cancel := context.WithTimeout(ctx, chromeCDPOperationTimeout)
	defer cancel()
	var failures []string
	for _, endpoint := range s.endpointCandidates() {
		version, err := s.fetchVersion(ctx, endpoint)
		if err != nil {
			failures = append(failures, endpoint+": "+err.Error())
			continue
		}
		wsURL, err := websocketURLForEndpoint(version.WebSocketDebuggerURL, endpoint)
		if err != nil {
			failures = append(failures, endpoint+": "+err.Error())
			continue
		}
		cookies, err := s.readCookiesFromWebSocket(ctx, wsURL, "Storage.getCookies")
		if err != nil {
			failures = append(failures, endpoint+": "+err.Error())
			continue
		}
		return cookies, nil
	}
	if len(failures) == 0 {
		return nil, errors.New("no Chrome CDP endpoint configured")
	}
	return nil, fmt.Errorf("unable to read Chrome cookies via CDP: %s", strings.Join(failures, "; "))
}

type chromeCDPVersion struct {
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type chromeCDPTarget struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func (s *ChromeCDPCookieService) readCredentialsFromPage(ctx context.Context, endpoint, targetURL string) ([]ChromeCDPCookie, []chromeCDPStorageEntry, string, error) {
	parsed, err := parseCDPEndpoint(endpoint)
	if err != nil {
		return nil, nil, "", err
	}
	listURL := strings.TrimRight(parsed.String(), "/") + "/json/list"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return nil, nil, "", err
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		return nil, nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, nil, "", fmt.Errorf("target list returned HTTP %d", response.StatusCode)
	}
	var targets []chromeCDPTarget
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&targets); err != nil {
		return nil, nil, "", fmt.Errorf("decode target list: %w", err)
	}
	if len(targets) == 0 {
		return nil, nil, "", errors.New("Chrome has no debuggable pages")
	}
	targetHost := ""
	if parsedTarget, parseErr := url.Parse(strings.TrimSpace(targetURL)); parseErr == nil {
		targetHost = strings.ToLower(parsedTarget.Hostname())
	}
	selected := chromeCDPTarget{}
	for _, target := range targets {
		if target.Type != "page" || strings.TrimSpace(target.WebSocketDebuggerURL) == "" {
			continue
		}
		if selected.WebSocketDebuggerURL == "" {
			selected = target
		}
		pageURL, parseErr := url.Parse(target.URL)
		if targetHost != "" && parseErr == nil && strings.EqualFold(pageURL.Hostname(), targetHost) {
			selected = target
			break
		}
	}
	if selected.WebSocketDebuggerURL == "" {
		return nil, nil, "", errors.New("Chrome target list has no debuggable page")
	}
	wsURL, err := websocketURLForEndpoint(selected.WebSocketDebuggerURL, endpoint)
	if err != nil {
		return nil, nil, "", err
	}
	return s.readCredentialsFromPageWebSocket(ctx, wsURL, targetHost)
}

func (s *ChromeCDPCookieService) fetchVersion(ctx context.Context, endpoint string) (chromeCDPVersion, error) {
	parsed, err := parseCDPEndpoint(endpoint)
	if err != nil {
		return chromeCDPVersion{}, err
	}
	versionURL := strings.TrimRight(parsed.String(), "/") + "/json/version"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, versionURL, nil)
	if err != nil {
		return chromeCDPVersion{}, err
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		return chromeCDPVersion{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return chromeCDPVersion{}, fmt.Errorf("version endpoint returned HTTP %d", response.StatusCode)
	}
	var version chromeCDPVersion
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&version); err != nil {
		return chromeCDPVersion{}, fmt.Errorf("decode version endpoint: %w", err)
	}
	if strings.TrimSpace(version.WebSocketDebuggerURL) == "" {
		return chromeCDPVersion{}, errors.New("version endpoint did not return webSocketDebuggerUrl")
	}
	return version, nil
}

func (s *ChromeCDPCookieService) readCookiesFromWebSocket(ctx context.Context, rawURL, method string) ([]ChromeCDPCookie, error) {
	connection, err := dialWebSocket(ctx, rawURL, s.dialContext)
	if err != nil {
		return nil, err
	}
	defer connection.close()
	stopContext := closeWebSocketOnContext(ctx, connection)
	defer close(stopContext)

	result, err := readCDPCommand(ctx, connection, 1, method, nil)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Cookies []ChromeCDPCookie `json:"cookies"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, fmt.Errorf("decode %s result: %w", method, err)
	}
	return payload.Cookies, nil
}

func (s *ChromeCDPCookieService) readCredentialsFromPageWebSocket(ctx context.Context, rawURL, targetHost string) ([]ChromeCDPCookie, []chromeCDPStorageEntry, string, error) {
	connection, err := dialWebSocket(ctx, rawURL, s.dialContext)
	if err != nil {
		return nil, nil, "", err
	}
	defer connection.close()
	stopContext := closeWebSocketOnContext(ctx, connection)
	defer close(stopContext)

	cookieResult, err := readCDPCommand(ctx, connection, 1, "Network.getAllCookies", nil)
	if err != nil {
		return nil, nil, "", err
	}
	var cookiePayload struct {
		Cookies []ChromeCDPCookie `json:"cookies"`
	}
	if err := json.Unmarshal(cookieResult, &cookiePayload); err != nil {
		return nil, nil, "", fmt.Errorf("decode Network.getAllCookies result: %w", err)
	}

	storageResult, err := readCDPCommand(ctx, connection, 2, "Runtime.evaluate", map[string]any{
		"expression":    chromeCDPStorageExpression,
		"returnByValue": true,
	})
	if err != nil {
		return nil, nil, "", err
	}
	var storagePayload struct {
		Result struct {
			Value []chromeCDPStorageEntry `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(storageResult, &storagePayload); err != nil {
		return nil, nil, "", fmt.Errorf("decode Runtime.evaluate result: %w", err)
	}
	if len(storagePayload.ExceptionDetails) > 0 && string(storagePayload.ExceptionDetails) != "null" {
		return nil, nil, "", errors.New("Runtime.evaluate could not read browser storage")
	}
	authorization := authorizationHeaderFromStorage(storagePayload.Result.Value, s.now())
	if authorization == "" {
		if captured, captureErr := s.captureAuthorizationAfterReload(ctx, connection, targetHost); captureErr == nil {
			authorization = captured
		}
	}
	return cookiePayload.Cookies, storagePayload.Result.Value, authorization, nil
}

func (s *ChromeCDPCookieService) captureAuthorizationAfterReload(ctx context.Context, connection *cdpWebSocket, targetHost string) (string, error) {
	reloadCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	stopContext := closeWebSocketOnContext(reloadCtx, connection)
	defer close(stopContext)
	if _, err := readCDPCommand(reloadCtx, connection, 3, "Network.enable", nil); err != nil {
		return "", err
	}
	if err := writeCDPCommand(connection, 4, "Page.reload", map[string]any{"ignoreCache": false}); err != nil {
		return "", fmt.Errorf("send Page.reload: %w", err)
	}
	for {
		opcode, payload, err := connection.readFrame()
		if err != nil {
			if reloadCtx.Err() != nil {
				return "", reloadCtx.Err()
			}
			return "", fmt.Errorf("read Chrome network event: %w", err)
		}
		switch opcode {
		case 0x8:
			return "", errors.New("Chrome closed the CDP WebSocket during reload")
		case 0x9:
			if err := connection.writeFrame(0xA, payload); err != nil {
				return "", fmt.Errorf("send CDP pong: %w", err)
			}
		case 0x1, 0x2:
			var message struct {
				Method string `json:"method"`
				Params struct {
					Request struct {
						URL     string         `json:"url"`
						Headers map[string]any `json:"headers"`
					} `json:"request"`
				} `json:"params"`
			}
			if err := json.Unmarshal(payload, &message); err != nil {
				continue
			}
			if message.Method != "Network.requestWillBeSent" {
				continue
			}
			requestURL, parseErr := url.Parse(message.Params.Request.URL)
			if targetHost != "" && (parseErr != nil || !strings.EqualFold(requestURL.Hostname(), targetHost)) {
				continue
			}
			if authorization := authorizationHeaderFromRequest(message.Params.Request.Headers, s.now()); authorization != "" {
				return authorization, nil
			}
		}
	}
}

func authorizationHeaderFromRequest(headers map[string]any, now time.Time) string {
	for key, rawValue := range headers {
		if !strings.EqualFold(key, "Authorization") {
			continue
		}
		value, ok := rawValue.(string)
		if !ok {
			value = fmt.Sprint(rawValue)
		}
		return canonicalBearerAuthorization(value, now)
	}
	return ""
}

func closeWebSocketOnContext(ctx context.Context, connection *cdpWebSocket) chan struct{} {
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.conn.Close()
		case <-stopped:
		}
	}()
	return stopped
}

func readCDPCommand(ctx context.Context, connection *cdpWebSocket, commandID int, method string, params map[string]any) (json.RawMessage, error) {
	if err := writeCDPCommand(connection, commandID, method, params); err != nil {
		return nil, fmt.Errorf("send %s: %w", method, err)
	}

	for {
		opcode, payload, err := connection.readFrame()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("read CDP response: %w", err)
		}
		switch opcode {
		case 0x8:
			return nil, errors.New("Chrome closed the CDP WebSocket")
		case 0x9:
			if err := connection.writeFrame(0xA, payload); err != nil {
				return nil, fmt.Errorf("send CDP pong: %w", err)
			}
		case 0x1, 0x2:
			var response struct {
				ID     int             `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  *struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(payload, &response); err != nil {
				return nil, fmt.Errorf("decode CDP response: %w", err)
			}
			if response.ID != commandID {
				continue
			}
			if response.Error != nil {
				return nil, fmt.Errorf("CDP %s failed (%d): %s", method, response.Error.Code, response.Error.Message)
			}
			return response.Result, nil
		}
	}
}

func writeCDPCommand(connection *cdpWebSocket, commandID int, method string, params map[string]any) error {
	commandPayload := map[string]any{
		"id":     commandID,
		"method": method,
	}
	if len(params) > 0 {
		commandPayload["params"] = params
	}
	command, err := json.Marshal(commandPayload)
	if err != nil {
		return err
	}
	return connection.writeFrame(1, command)
}

// CookieHeaderForURL applies the browser's domain, path, secure and expiry
// rules before serializing cookies in the order expected by HTTP servers.
func CookieHeaderForURL(targetURL string, cookies []ChromeCDPCookie, now time.Time) string {
	parsed, err := url.Parse(strings.TrimSpace(targetURL))
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	requestPath := parsed.Path
	if requestPath == "" {
		requestPath = "/"
	}
	secureRequest := parsed.Scheme == "https"
	if now.IsZero() {
		now = time.Now()
	}
	type eligibleCookie struct {
		cookie ChromeCDPCookie
		index  int
	}
	eligible := make([]eligibleCookie, 0, len(cookies))
	for index, cookie := range cookies {
		name := strings.TrimSpace(cookie.Name)
		if name == "" || !validCookieName(name) || !validCookieValue(cookie.Value) || cookie.Expires > 0 && cookie.Expires <= float64(now.Unix()) {
			continue
		}
		rawDomain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(cookie.Domain), "."))
		domainCookie := strings.HasPrefix(rawDomain, ".")
		domain := strings.TrimPrefix(rawDomain, ".")
		if domain == "" || host != domain && (!domainCookie || !strings.HasSuffix(host, "."+domain)) {
			continue
		}
		pathValue := cookie.Path
		if pathValue == "" {
			pathValue = "/"
		}
		if !cookiePathMatches(requestPath, pathValue) || cookie.Secure && !secureRequest {
			continue
		}
		cookie.Name = name
		cookie.Path = pathValue
		eligible = append(eligible, eligibleCookie{cookie: cookie, index: index})
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		left, right := eligible[i].cookie, eligible[j].cookie
		if len(left.Path) != len(right.Path) {
			return len(left.Path) > len(right.Path)
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return eligible[i].index < eligible[j].index
	})
	parts := make([]string, 0, len(eligible))
	for _, item := range eligible {
		parts = append(parts, item.cookie.Name+"="+item.cookie.Value)
	}
	return strings.Join(parts, "; ")
}

func authorizationHeaderFromStorage(entries []chromeCDPStorageEntry, now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	bestAuthorization := ""
	bestRank := 1000
	consider := func(keyHint, value string) {
		authorization := canonicalBearerAuthorization(value, now)
		if authorization == "" {
			return
		}
		rank := storageAuthorizationRank(keyHint)
		if rank < bestRank {
			bestAuthorization = authorization
			bestRank = rank
		}
	}
	for _, entry := range entries {
		consider(entry.Key, entry.Value)
		var decoded any
		if err := json.Unmarshal([]byte(entry.Value), &decoded); err == nil {
			walkStorageAuthorizationValues(decoded, entry.Key, 0, consider)
		}
	}
	return bestAuthorization
}

func walkStorageAuthorizationValues(value any, keyPath string, depth int, consider func(string, string)) {
	if depth > 8 {
		return
	}
	switch typed := value.(type) {
	case string:
		consider(keyPath, typed)
	case map[string]any:
		for key, nested := range typed {
			walkStorageAuthorizationValues(nested, keyPath+"."+key, depth+1, consider)
		}
	case []any:
		for _, nested := range typed {
			walkStorageAuthorizationValues(nested, keyPath, depth+1, consider)
		}
	}
}

func storageAuthorizationRank(keyHint string) int {
	normalized := strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(strings.ToLower(keyHint))
	switch {
	case strings.Contains(normalized, "authorization"):
		return 0
	case strings.Contains(normalized, "access_token"), strings.Contains(normalized, "accesstoken"):
		return 1
	case strings.Contains(normalized, "access") && strings.Contains(normalized, "token"):
		return 1
	case normalized == "token", strings.HasSuffix(normalized, "_token"):
		return 2
	default:
		return 10
	}
}

func canonicalBearerAuthorization(value string, now time.Time) string {
	value = strings.TrimSpace(value)
	if len(value) >= len("Bearer ") && strings.EqualFold(value[:len("Bearer ")], "Bearer ") {
		value = strings.TrimSpace(value[len("Bearer "):])
	}
	if !validJWTAt(value, now) {
		return ""
	}
	return "Bearer " + value
}

func validJWTAt(token string, now time.Time) bool {
	if len(token) < 40 {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return false
	}
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var headerValue map[string]any
	var claims map[string]any
	if json.Unmarshal(header, &headerValue) != nil || json.Unmarshal(payload, &claims) != nil || len(headerValue) == 0 || len(claims) == 0 {
		return false
	}
	if expires, ok := claims["exp"].(float64); ok && expires > 0 && expires <= float64(now.Unix()) {
		return false
	}
	return true
}

func cookiePathMatches(requestPath, cookiePath string) bool {
	if requestPath == cookiePath {
		return true
	}
	if !strings.HasPrefix(requestPath, cookiePath) {
		return false
	}
	return strings.HasSuffix(cookiePath, "/") || len(requestPath) > len(cookiePath) && requestPath[len(cookiePath)] == '/'
}

func validCookieName(name string) bool {
	for _, char := range name {
		if char <= 0x20 || char >= 0x7f || strings.ContainsRune("()<>@,;:\\\"/[]?={}\t", char) {
			return false
		}
	}
	return true
}

func validCookieValue(value string) bool {
	for index := 0; index < len(value); index++ {
		char := value[index]
		if char < 0x20 || char >= 0x7f || char == '"' || char == ';' || char == '\\' {
			return false
		}
	}
	return true
}

func parseCDPEndpoint(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid Chrome CDP endpoint: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return nil, errors.New("Chrome CDP endpoint must be an HTTP(S) URL with a host")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed, nil
}

func (s *ChromeCDPCookieService) endpointCandidates() []string {
	parsed, err := parseCDPEndpoint(s.endpoint)
	if err != nil {
		return []string{s.endpoint}
	}
	base := strings.TrimRight(parsed.String(), "/")
	candidates := []string{base}
	host := strings.ToLower(parsed.Hostname())
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return candidates
	}
	for _, gateway := range wslHostCandidates() {
		candidateURL := *parsed
		candidateURL.Host = gateway
		if port := parsed.Port(); port != "" {
			candidateURL.Host = net.JoinHostPort(gateway, port)
		} else if strings.Contains(gateway, ":") {
			candidateURL.Host = "[" + gateway + "]"
		}
		candidate := strings.TrimRight(candidateURL.String(), "/")
		if candidate != base {
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

func wslHostCandidates() []string {
	seen := make(map[string]struct{})
	var candidates []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || value == "127.0.0.1" || value == "::1" || value == "localhost" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		candidates = append(candidates, value)
	}
	if data, err := os.ReadFile("/proc/net/route"); err == nil {
		add(linuxDefaultIPv4Gateway(string(data)))
	}
	if data, err := os.ReadFile("/etc/resolv.conf"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == "nameserver" {
				add(strings.Trim(fields[1], "[]"))
			}
		}
	}
	add("host.docker.internal")
	return candidates
}

func linuxDefaultIPv4Gateway(routeTable string) string {
	for _, line := range strings.Split(routeTable, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[1] != "00000000" {
			continue
		}
		gateway, err := strconv.ParseUint(fields[2], 16, 32)
		if err != nil || gateway == 0 {
			continue
		}
		return net.IPv4(byte(gateway), byte(gateway>>8), byte(gateway>>16), byte(gateway>>24)).String()
	}
	return ""
}

func websocketURLForEndpoint(rawWebSocketURL, endpoint string) (string, error) {
	websocketURL, err := url.Parse(strings.TrimSpace(rawWebSocketURL))
	if err != nil || websocketURL.Scheme != "ws" && websocketURL.Scheme != "wss" || websocketURL.Hostname() == "" {
		return "", errors.New("invalid Chrome webSocketDebuggerUrl")
	}
	base, err := parseCDPEndpoint(endpoint)
	if err != nil {
		return "", err
	}
	wsHost := strings.ToLower(websocketURL.Hostname())
	baseHost := strings.ToLower(base.Hostname())
	if (wsHost == "127.0.0.1" || wsHost == "localhost" || wsHost == "::1") && baseHost != "127.0.0.1" && baseHost != "localhost" && baseHost != "::1" {
		port := websocketURL.Port()
		if port == "" {
			port = base.Port()
		}
		if port != "" {
			websocketURL.Host = net.JoinHostPort(baseHost, port)
		} else if strings.Contains(baseHost, ":") {
			websocketURL.Host = "[" + baseHost + "]"
		} else {
			websocketURL.Host = baseHost
		}
	}
	return websocketURL.String(), nil
}

type cdpWebSocket struct {
	conn    net.Conn
	read    *bufio.Reader
	writeMu sync.Mutex
}

func dialWebSocket(ctx context.Context, rawURL string, dialContext func(context.Context, string, string) (net.Conn, error)) (*cdpWebSocket, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "ws" && parsed.Scheme != "wss" || parsed.Hostname() == "" || parsed.User != nil {
		return nil, errors.New("invalid Chrome WebSocket URL")
	}
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "wss" {
			port = "443"
		} else {
			port = "80"
		}
	}
	connection, err := dialContext(ctx, "tcp", net.JoinHostPort(parsed.Hostname(), port))
	if err != nil {
		return nil, err
	}
	if parsed.Scheme == "wss" {
		tlsConnection := tls.Client(connection, &tls.Config{ServerName: parsed.Hostname(), MinVersion: tls.VersionTLS12})
		if err := tlsConnection.HandshakeContext(ctx); err != nil {
			_ = connection.Close()
			return nil, err
		}
		connection = tlsConnection
	}
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		_ = connection.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	if parsed.RawQuery != "" {
		path += "?" + parsed.RawQuery
	}
	host := parsed.Host
	request := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := io.WriteString(connection, request); err != nil {
		_ = connection.Close()
		return nil, err
	}
	reader := bufio.NewReaderSize(connection, 64*1024)
	handshakeDone := make(chan struct{})
	defer close(handshakeDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.Close()
		case <-handshakeDone:
		}
	}()
	status, headers, err := readWebSocketHandshake(reader)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	if status != http.StatusSwitchingProtocols {
		_ = connection.Close()
		return nil, fmt.Errorf("WebSocket handshake returned HTTP %d", status)
	}
	expected := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	if !strings.EqualFold(strings.TrimSpace(headers.Get("Sec-WebSocket-Accept")), base64.StdEncoding.EncodeToString(expected[:])) {
		_ = connection.Close()
		return nil, errors.New("invalid WebSocket handshake response")
	}
	return &cdpWebSocket{conn: connection, read: reader}, nil
}

func readWebSocketHandshake(reader *bufio.Reader) (int, http.Header, error) {
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	return response.StatusCode, response.Header, nil
}

func (c *cdpWebSocket) close() {
	if c != nil && c.conn != nil {
		_ = c.conn.Close()
	}
}

func (c *cdpWebSocket) writeFrame(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if uint64(len(payload)) > uint64(^uint32(0)) {
		return errors.New("WebSocket payload is too large")
	}
	frame := make([]byte, 0, len(payload)+14)
	frame = append(frame, 0x80|opcode)
	length := len(payload)
	switch {
	case length < 126:
		frame = append(frame, byte(length)|0x80)
	case length <= int(^uint16(0)):
		frame = append(frame, 126|0x80, byte(length>>8), byte(length))
	default:
		frame = append(frame, 127|0x80)
		var extended [8]byte
		binary.BigEndian.PutUint64(extended[:], uint64(length))
		frame = append(frame, extended[:]...)
	}
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	frame = append(frame, mask...)
	for index, value := range payload {
		frame = append(frame, value^mask[index%4])
	}
	for len(frame) > 0 {
		written, err := c.conn.Write(frame)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		frame = frame[written:]
	}
	return nil
}

func (c *cdpWebSocket) readFrame() (byte, []byte, error) {
	opcode, payload, fin, err := c.readFramePart()
	if err != nil {
		return 0, nil, err
	}
	if opcode == 0x8 || opcode == 0x9 || opcode == 0xA {
		return opcode, payload, nil
	}
	if opcode != 0x1 && opcode != 0x2 {
		return 0, nil, errors.New("invalid fragmented CDP WebSocket message")
	}
	if fin {
		return opcode, payload, nil
	}
	message := append([]byte(nil), payload...)
	for {
		nextOpcode, nextPayload, nextFin, err := c.readFramePart()
		if err != nil {
			return 0, nil, err
		}
		switch nextOpcode {
		case 0x8:
			return nextOpcode, nextPayload, nil
		case 0x9:
			if err := c.writeFrame(0xA, nextPayload); err != nil {
				return 0, nil, fmt.Errorf("send CDP pong: %w", err)
			}
			continue
		case 0xA:
			continue
		case 0:
		default:
			return 0, nil, errors.New("invalid CDP WebSocket continuation frame")
		}
		if len(message)+len(nextPayload) > 64<<20 {
			return 0, nil, errors.New("CDP WebSocket message is too large")
		}
		message = append(message, nextPayload...)
		if nextFin {
			return opcode, message, nil
		}
	}
}

func (c *cdpWebSocket) readFramePart() (byte, []byte, bool, error) {
	first, err := c.read.ReadByte()
	if err != nil {
		return 0, nil, false, err
	}
	second, err := c.read.ReadByte()
	if err != nil {
		return 0, nil, false, err
	}
	fin := first&0x80 != 0
	opcode := first & 0x0F
	if first&0x70 != 0 {
		return 0, nil, false, errors.New("CDP WebSocket frame uses unsupported RSV bits")
	}
	if opcode != 0 && opcode != 0x1 && opcode != 0x2 && opcode != 0x8 && opcode != 0x9 && opcode != 0xA {
		return 0, nil, false, errors.New("CDP WebSocket frame has an invalid opcode")
	}
	length := uint64(second & 0x7F)
	if length == 126 {
		var extended [2]byte
		if _, err := io.ReadFull(c.read, extended[:]); err != nil {
			return 0, nil, false, err
		}
		length = uint64(binary.BigEndian.Uint16(extended[:]))
	} else if length == 127 {
		var extended [8]byte
		if _, err := io.ReadFull(c.read, extended[:]); err != nil {
			return 0, nil, false, err
		}
		length = binary.BigEndian.Uint64(extended[:])
	}
	if length > 64<<20 {
		return 0, nil, false, errors.New("CDP WebSocket frame is too large")
	}
	if opcode >= 0x8 && (!fin || length > 125) {
		return 0, nil, false, errors.New("invalid CDP WebSocket control frame")
	}
	if second&0x80 != 0 {
		return 0, nil, false, errors.New("Chrome returned a masked WebSocket frame")
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(c.read, payload); err != nil {
		return 0, nil, false, err
	}
	return opcode, payload, fin, nil
}
