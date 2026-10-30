package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	turnstilePageTimeout = 180 * time.Second
	turnstileDefaultWait = 120 * time.Second
)

// SolveTurnstile connects to the configured Chrome CDP endpoint, renders a
// Turnstile widget in the real browser, and returns the token plus cookies
// acquired while passing the challenge. It never starts another browser.
func SolveTurnstile(ctx context.Context, endpoint string, request GeneralWorkflowTurnstileRequest) (GeneralWorkflowTurnstileResult, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return GeneralWorkflowTurnstileResult{}, errors.New("Chrome CDP address is empty")
	}
	target, err := url.Parse(strings.TrimSpace(request.URL))
	if err != nil || target.Scheme != "http" && target.Scheme != "https" || target.Host == "" {
		return GeneralWorkflowTurnstileResult{}, errors.New("invalid Turnstile request URL")
	}
	sitekey := strings.TrimSpace(request.SiteKey)
	if sitekey == "" {
		return GeneralWorkflowTurnstileResult{}, errors.New("Turnstile sitekey is not configured for this workflow step")
	}
	pageURL := *target
	pageURL.Path = "/"
	pageURL.RawPath = ""
	pageURL.RawQuery = ""
	pageURL.Fragment = ""

	connection, err := connectTurnstilePage(ctx, endpoint, target.Hostname())
	if err != nil {
		return GeneralWorkflowTurnstileResult{}, fmt.Errorf("connect Chrome CDP: %w", err)
	}
	defer connection.close()
	client := newTurnstileCDPClient(connection)
	stopContext := closeWebSocketOnContext(ctx, connection)
	defer close(stopContext)

	if err := client.enable(pageURL.String(), request.Headers); err != nil {
		return GeneralWorkflowTurnstileResult{}, err
	}
	if _, err := client.command(ctx, "Page.navigate", map[string]any{"url": pageURL.String()}); err != nil {
		return GeneralWorkflowTurnstileResult{}, fmt.Errorf("navigate Chrome page: %w", err)
	}
	if err := client.waitForCloudflarePass(ctx, target.Hostname()); err != nil {
		return GeneralWorkflowTurnstileResult{}, err
	}

	wait := turnstileDefaultWait
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < wait {
			wait = remaining
		}
	}
	token, err := client.solve(ctx, sitekey, wait)
	if err != nil {
		return GeneralWorkflowTurnstileResult{}, err
	}
	retryURL, retryBody, err := turnstileRetryRequest(request, token)
	if err != nil {
		return GeneralWorkflowTurnstileResult{}, err
	}
	retryResponse, err := client.retry(ctx, request, retryURL, retryBody)
	if err != nil {
		return GeneralWorkflowTurnstileResult{}, err
	}
	cookies, err := client.cookies(ctx)
	if err != nil {
		return GeneralWorkflowTurnstileResult{}, fmt.Errorf("read browser cookies: %w", err)
	}
	return GeneralWorkflowTurnstileResult{Token: token, Cookies: cookies, Response: retryResponse}, nil
}

func turnstileRetryRequest(request GeneralWorkflowTurnstileRequest, token string) (string, []byte, error) {
	target, err := url.Parse(strings.TrimSpace(request.URL))
	if err != nil {
		return "", nil, fmt.Errorf("parse Turnstile retry URL: %w", err)
	}
	query := target.Query()
	query.Set("turnstile", token)
	target.RawQuery = query.Encode()

	body := map[string]any{}
	if len(request.Body) > 0 {
		var decoded any
		if err := json.Unmarshal(request.Body, &decoded); err != nil {
			return "", nil, fmt.Errorf("decode request body for Turnstile retry: %w", err)
		}
		var ok bool
		body, ok = decoded.(map[string]any)
		if !ok {
			return "", nil, errors.New("request body must be a JSON object for Turnstile retry")
		}
	}
	body["turnstile"] = token
	encodedBody, err := json.Marshal(body)
	if err != nil {
		return "", nil, fmt.Errorf("encode request body for Turnstile retry: %w", err)
	}
	return target.String(), encodedBody, nil
}

func connectTurnstilePage(ctx context.Context, endpoint, targetHost string) (*cdpWebSocket, error) {
	parsed, err := parseCDPEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(parsed.String(), "/")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/json/list", nil)
	if err != nil {
		return nil, err
	}
	response, err := defaultChromeCDPHTTPClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Chrome target list returned HTTP %d", response.StatusCode)
	}
	var targets []chromeCDPTarget
	if err := json.NewDecoder(response.Body).Decode(&targets); err != nil {
		return nil, fmt.Errorf("decode Chrome target list: %w", err)
	}
	var selected chromeCDPTarget
	for _, candidate := range targets {
		if candidate.Type != "page" || strings.TrimSpace(candidate.WebSocketDebuggerURL) == "" {
			continue
		}
		if selected.WebSocketDebuggerURL == "" {
			selected = candidate
		}
		candidateURL, parseErr := url.Parse(candidate.URL)
		if parseErr == nil && strings.EqualFold(candidateURL.Hostname(), targetHost) {
			selected = candidate
			break
		}
	}
	if selected.WebSocketDebuggerURL == "" {
		return nil, errors.New("Chrome CDP has no debuggable page")
	}
	websocketURL, err := websocketURLForEndpoint(selected.WebSocketDebuggerURL, endpoint)
	if err != nil {
		return nil, err
	}
	connection, err := dialWebSocket(ctx, websocketURL, (&netDialer{}).DialContext)
	if err != nil {
		return nil, err
	}
	return connection, nil
}

// netDialer keeps the Turnstile solver independent from the HTTP transport's
// proxy settings while still honoring the context used by the CDP operation.
type netDialer struct{}

func (*netDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, address)
}

type turnstileCDPClient struct {
	connection *cdpWebSocket
	nextID     int
}

func newTurnstileCDPClient(connection *cdpWebSocket) *turnstileCDPClient {
	return &turnstileCDPClient{connection: connection, nextID: 1}
}

func (client *turnstileCDPClient) command(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	id := client.nextID
	client.nextID++
	return readCDPCommand(ctx, client.connection, id, method, params)
}

func (client *turnstileCDPClient) evaluate(ctx context.Context, expression string) (any, error) {
	result, err := client.command(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	})
	if err != nil {
		return nil, err
	}
	var payload struct {
		Result struct {
			Value            json.RawMessage `json:"value"`
			ExceptionDetails json.RawMessage `json:"exceptionDetails"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, fmt.Errorf("decode Runtime.evaluate result: %w", err)
	}
	if len(payload.Result.ExceptionDetails) > 0 && string(payload.Result.ExceptionDetails) != "null" {
		return nil, errors.New("Chrome Runtime.evaluate raised an exception")
	}
	if len(payload.Result.Value) == 0 || string(payload.Result.Value) == "null" {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(payload.Result.Value, &value); err != nil {
		return nil, fmt.Errorf("decode Runtime.evaluate value: %w", err)
	}
	return value, nil
}

func (client *turnstileCDPClient) enable(pageURL string, headers http.Header) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.command(ctx, "Page.enable", nil); err != nil {
		return fmt.Errorf("enable Chrome Page domain: %w", err)
	}
	if _, err := client.command(ctx, "Network.enable", nil); err != nil {
		return fmt.Errorf("enable Chrome Network domain: %w", err)
	}
	for name, values := range headers {
		if !strings.EqualFold(name, "cookie") {
			continue
		}
		cookieRequest := &http.Request{Header: http.Header{"Cookie": []string{strings.Join(values, "; ")}}}
		for _, cookie := range cookieRequest.Cookies() {
			if _, err := client.command(ctx, "Network.setCookie", map[string]any{
				"name":  cookie.Name,
				"value": cookie.Value,
				"url":   pageURL,
			}); err != nil {
				return fmt.Errorf("set Chrome cookie %q: %w", cookie.Name, err)
			}
		}
	}
	return nil
}

type turnstilePageSnapshot struct {
	URL       string
	Title     string
	Ready     string
	Text      string
	Challenge bool
}

func (client *turnstileCDPClient) waitForCloudflarePass(ctx context.Context, expectedHost string) error {
	deadline := time.Now().Add(turnstilePageTimeout)
	clearSnapshots := 0
	var lastSnapshot turnstilePageSnapshot
	for time.Now().Before(deadline) {
		value, err := client.evaluate(ctx, `(() => ({
  url: location.href || '',
  title: document.title || '',
  ready: document.readyState || '',
  text: (document.body && document.body.innerText || '').slice(0, 3000),
  challenge: Array.from(document.querySelectorAll('#challenge-form, #cf-challenge-running, #challenge-stage, iframe[src*="/cdn-cgi/challenge-platform/"]')).some(element => {
      const rect = element.getBoundingClientRect();
      const style = getComputedStyle(element);
      return rect.width > 0 && rect.height > 0 && style.display !== 'none' && style.visibility !== 'hidden';
    })
    || location.pathname.includes('/cdn-cgi/challenge-platform/')
    || location.search.includes('__cf_chl')
}))()`)
		if err == nil {
			if state, ok := value.(map[string]any); ok {
				lastSnapshot = turnstilePageSnapshot{
					URL:       stringFromMap(state, "url"),
					Title:     stringFromMap(state, "title"),
					Ready:     stringFromMap(state, "ready"),
					Text:      stringFromMap(state, "text"),
					Challenge: state["challenge"] == true,
				}
				if turnstileCloudflareVerificationFailed(lastSnapshot) {
					return fmt.Errorf("Cloudflare verification failed on the relay page (title: %q)", lastSnapshot.Title)
				}
				if turnstileCloudflareChallengeActive(lastSnapshot, expectedHost) || lastSnapshot.Ready == "loading" {
					clearSnapshots = 0
				} else {
					clearSnapshots++
					if clearSnapshots >= 3 {
						return nil
					}
				}
			}
		}
		if err := sleepWithContext(ctx, time.Second); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if turnstileCloudflareChallengeActive(lastSnapshot, expectedHost) {
		return fmt.Errorf("Cloudflare verification is still active after %s (page title: %q); complete the browser check and retry", turnstilePageTimeout, lastSnapshot.Title)
	}
	return fmt.Errorf("waiting for the relay page to finish loading timed out after %s", turnstilePageTimeout)
}

func turnstileCloudflareVerificationFailed(snapshot turnstilePageSnapshot) bool {
	pageText := strings.ToLower(snapshot.Title + "\n" + snapshot.Text)
	for _, marker := range []string{"verification failed", "验证失败", "验证未通过"} {
		if strings.Contains(pageText, marker) {
			return true
		}
	}
	return false
}

func turnstileCloudflareChallengeActive(snapshot turnstilePageSnapshot, expectedHost string) bool {
	parsed, err := url.Parse(strings.TrimSpace(snapshot.URL))
	if err != nil || !strings.EqualFold(parsed.Hostname(), expectedHost) {
		return true
	}
	if snapshot.Challenge {
		return true
	}
	pageText := strings.ToLower(snapshot.Title + "\n" + snapshot.Text)
	for _, marker := range []string{
		"just a moment",
		"attention required",
		"checking your browser",
		"verify you are human",
		"verify you are a human",
		"performing security verification",
		"complete the security check",
		"security verification",
		"验证您是人类",
		"验证您是真人",
		"确认您是人类",
		"请完成人机验证",
		"正在验证您的浏览器",
	} {
		if strings.Contains(pageText, marker) {
			return true
		}
	}
	return false
}

func stringFromMap(value map[string]any, key string) string {
	result, _ := value[key].(string)
	return result
}

func (client *turnstileCDPClient) solve(ctx context.Context, sitekey string, wait time.Duration) (string, error) {
	if wait <= 0 {
		wait = turnstileDefaultWait
	}
	render := `(async (sitekey) => {
  if (!document.getElementById('red-token-turnstile')) {
    const holder = document.createElement('div');
    holder.id = 'red-token-turnstile';
    holder.style.cssText = 'position:fixed;top:10px;left:10px;z-index:2147483647;background:#fff;padding:10px;';
    document.body.appendChild(holder);
  }
  if (!document.querySelector('script[src*="challenges.cloudflare.com/turnstile"]')) {
    const script = document.createElement('script');
    script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit';
    document.head.appendChild(script);
  }
  for (let i = 0; i < 40; i += 1) {
    if (window.turnstile && window.turnstile.render) break;
    await new Promise(resolve => setTimeout(resolve, 500));
  }
  if (!window.turnstile || !window.turnstile.render) throw new Error('Turnstile API initialization failed');
  window.__redTokenTurnstileToken = null;
  window.__redTokenTurnstileError = null;
  window.__redTokenTurnstileInteractive = false;
  window.__redTokenTurnstileSitekey = sitekey;
  window.__redTokenTurnstileWidget = window.turnstile.render(document.getElementById('red-token-turnstile'), {
    sitekey,
    callback: token => window.__redTokenTurnstileToken = token,
    'error-callback': error => window.__redTokenTurnstileError = String(error),
    'before-interactive-callback': () => window.__redTokenTurnstileInteractive = true,
  });
})`
	if _, err := client.evaluate(ctx, render+"("+strconv.Quote(sitekey)+")"); err != nil {
		return "", fmt.Errorf("render Turnstile widget: %w", err)
	}
	reset := `(() => {
  try { window.turnstile.remove(window.__redTokenTurnstileWidget); } catch (_) {}
  window.__redTokenTurnstileToken = null;
  window.__redTokenTurnstileError = null;
  window.__redTokenTurnstileInteractive = false;
  window.__redTokenTurnstileWidget = window.turnstile.render(document.getElementById('red-token-turnstile'), {
    sitekey: window.__redTokenTurnstileSitekey,
    callback: token => window.__redTokenTurnstileToken = token,
    'error-callback': error => window.__redTokenTurnstileError = String(error),
    'before-interactive-callback': () => window.__redTokenTurnstileInteractive = true,
  });
})()`
	poll := `(() => {
  const host = document.querySelector('#red-token-turnstile > div');
  const rect = host ? host.getBoundingClientRect() : null;
  return {
    token: window.__redTokenTurnstileToken || '',
    error: window.__redTokenTurnstileError || '',
    interactive: !!window.__redTokenTurnstileInteractive,
    box: rect ? {x: rect.x, y: rect.y, width: rect.width, height: rect.height} : null,
  };
})()`
	deadline := time.Now().Add(wait)
	clicked := false
	clickStarted := time.Time{}
	rounds := 0
	for time.Now().Before(deadline) {
		value, err := client.evaluate(ctx, poll)
		if err == nil {
			if state, ok := value.(map[string]any); ok {
				if token, ok := state["token"].(string); ok && strings.TrimSpace(token) != "" {
					return token, nil
				}
				if challengeError, _ := state["error"].(string); challengeError != "" {
					rounds++
					if rounds >= 4 {
						return "", fmt.Errorf("Turnstile challenge failed: %s", challengeError)
					}
					if _, resetErr := client.evaluate(ctx, reset); resetErr != nil {
						return "", fmt.Errorf("reset Turnstile widget: %w", resetErr)
					}
					clicked = false
					continue
				}
				if !clicked {
					if interactive, _ := state["interactive"].(bool); interactive {
						if box, ok := state["box"].(map[string]any); ok {
							x, xOK := box["x"].(float64)
							y, yOK := box["y"].(float64)
							width, widthOK := box["width"].(float64)
							height, heightOK := box["height"].(float64)
							if xOK && yOK && widthOK && heightOK && width > 50 && height > 20 {
								if err := sleepWithContext(ctx, 2*time.Second); err != nil {
									return "", err
								}
								clickX := x + 30
								clickY := y + height/2
								if err := client.click(ctx, clickX, clickY); err != nil {
									return "", err
								}
								clicked = true
								clickStarted = time.Now()
							}
						}
					}
				} else if time.Since(clickStarted) > 30*time.Second {
					rounds++
					if rounds >= 4 {
						return "", errors.New("Turnstile challenge timed out after repeated clicks")
					}
					if _, resetErr := client.evaluate(ctx, reset); resetErr != nil {
						return "", fmt.Errorf("reset Turnstile widget: %w", resetErr)
					}
					clicked = false
				}
			}
		}
		if err := sleepWithContext(ctx, time.Second); err != nil {
			return "", err
		}
	}
	return "", errors.New("Turnstile token wait timed out")
}

func (client *turnstileCDPClient) click(ctx context.Context, x, y float64) error {
	for _, event := range []map[string]any{
		{"type": "mouseMoved", "x": x - 15, "y": y - 8},
		{"type": "mouseMoved", "x": x - 3, "y": y},
		{"type": "mousePressed", "x": x, "y": y, "button": "left", "clickCount": 1, "buttons": 1},
		{"type": "mouseReleased", "x": x, "y": y, "button": "left", "clickCount": 1, "buttons": 0},
	} {
		if _, err := client.command(ctx, "Input.dispatchMouseEvent", event); err != nil {
			return fmt.Errorf("click Turnstile widget: %w", err)
		}
		if event["type"] == "mouseMoved" {
			if err := sleepWithContext(ctx, 400*time.Millisecond); err != nil {
				return err
			}
		}
	}
	return nil
}

func (client *turnstileCDPClient) cookies(ctx context.Context) ([]*http.Cookie, error) {
	result, err := client.command(ctx, "Network.getAllCookies", nil)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Cookies []ChromeCDPCookie `json:"cookies"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, err
	}
	cookies := make([]*http.Cookie, 0, len(payload.Cookies))
	for _, cookie := range payload.Cookies {
		converted := &http.Cookie{
			Name:   cookie.Name,
			Value:  cookie.Value,
			Domain: cookie.Domain,
			Path:   cookie.Path,
			Secure: cookie.Secure,
		}
		if cookie.Expires > 0 {
			converted.Expires = time.Unix(int64(cookie.Expires), 0)
		}
		cookies = append(cookies, converted)
	}
	return cookies, nil
}

func (client *turnstileCDPClient) retry(ctx context.Context, request GeneralWorkflowTurnstileRequest, retryURL string, body []byte) (*GeneralWorkflowTurnstileResponse, error) {
	headerValues := make(map[string]string, len(request.Headers))
	for name, values := range request.Headers {
		switch {
		case strings.EqualFold(name, "cookie"), strings.EqualFold(name, "content-length"), strings.EqualFold(name, "host"), strings.EqualFold(name, "connection"), strings.EqualFold(name, "transfer-encoding"), strings.EqualFold(name, "user-agent"):
			continue
		case len(values) > 0:
			headerValues[name] = strings.Join(values, ", ")
		}
	}
	hasContentType := false
	for name := range headerValues {
		if strings.EqualFold(name, "content-type") {
			hasContentType = true
			break
		}
	}
	if !hasContentType {
		headerValues["Content-Type"] = "application/json"
	}
	args := map[string]any{
		"url":     retryURL,
		"method":  request.Method,
		"headers": headerValues,
		"body":    string(body),
	}
	encodedArgs, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	expression := `(async (args) => {
  const options = {method: args.method, headers: args.headers, credentials: 'include'};
  if (args.method !== 'GET' && args.method !== 'HEAD') options.body = args.body;
  const response = await fetch(args.url, options);
  const headers = {};
  response.headers.forEach((value, key) => { headers[key] = value; });
  return {status: response.status, headers, body: await response.text()};
})(` + string(encodedArgs) + `)`
	value, err := client.evaluate(ctx, expression)
	if err != nil {
		return nil, fmt.Errorf("retry Turnstile request in Chrome: %w", err)
	}
	result, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("Chrome returned an invalid Turnstile retry response")
	}
	statusValue, ok := result["status"].(float64)
	if !ok || statusValue < 100 || statusValue > 599 {
		return nil, errors.New("Chrome returned an invalid Turnstile retry status")
	}
	bodyText, ok := result["body"].(string)
	if !ok {
		return nil, errors.New("Chrome returned an invalid Turnstile retry body")
	}
	if len(bodyText) > 10<<20 {
		return nil, errors.New("Turnstile retry response exceeds 10 MB")
	}
	responseHeaders := make(http.Header)
	if rawHeaders, ok := result["headers"].(map[string]any); ok {
		for name, rawValue := range rawHeaders {
			if value, ok := rawValue.(string); ok {
				responseHeaders.Set(name, value)
			}
		}
	}
	return &GeneralWorkflowTurnstileResponse{
		Status:  int(statusValue),
		Headers: responseHeaders,
		Body:    []byte(bodyText),
	}, nil
}

func sleepWithContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
