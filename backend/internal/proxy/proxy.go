package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"red-token/internal/domain"
)

type Service struct {
	client                *http.Client
	directClient          *http.Client
	connectTimeout        time.Duration
	responseHeaderTimeout time.Duration
	mu                    sync.Mutex
	proxyClients          map[string]*http.Client
}

func New(connectTimeout, responseHeaderTimeout time.Duration) *Service {
	return &Service{
		directClient:          &http.Client{Transport: newTransport(connectTimeout, responseHeaderTimeout, nil)},
		connectTimeout:        connectTimeout,
		responseHeaderTimeout: responseHeaderTimeout,
		proxyClients:          make(map[string]*http.Client),
	}
}

func NewWithHTTPClient(client *http.Client) *Service {
	if client == nil {
		client = http.DefaultClient
	}
	return &Service{client: client}
}

func EndpointForPath(path string) string {
	switch path {
	case "/v1/chat/completions":
		return domain.EndpointChat
	case "/v1/responses":
		return domain.EndpointResponses
	case "/v1/embeddings":
		return domain.EndpointEmbeddings
	case "/v1/images/generations":
		return domain.EndpointImages
	case "/v1/messages", "/v1/messages/count_tokens":
		return domain.EndpointMessages
	case "/v1/models":
		return domain.EndpointModels
	default:
		return ""
	}
}

func ExtractModel(body []byte) (string, error) {
	var payload struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.Model) == "" {
		return "", errors.New("missing model")
	}
	return payload.Model, nil
}

func RewriteModel(body []byte, model string) ([]byte, error) {
	if strings.TrimSpace(model) == "" {
		return body, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	payload["model"] = model
	return json.Marshal(payload)
}

func (s *Service) Do(ctx context.Context, incoming *http.Request, backend domain.Backend, body []byte) (*http.Response, error) {
	return s.DoWithPath(ctx, incoming, backend, body, incoming.URL.Path)
}

func (s *Service) DoWithPath(ctx context.Context, incoming *http.Request, backend domain.Backend, body []byte, upstreamPath string) (*http.Response, error) {
	target, err := buildTargetURL(backend.BaseURL, incoming.URL, upstreamPath)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, incoming.Method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	copyHeaders(request.Header, incoming.Header)
	removeHopByHopHeaders(request.Header)
	request.Header.Del("Authorization")
	request.Header.Del("X-Api-Key")
	request.Header.Del("Host")
	request.Header.Del("Content-Length")
	sanitizeBackendHeaders(request.Header, backend, upstreamPath)
	applyBackendAuth(request.Header, backend, upstreamPath)

	client, err := s.clientForBackend(backend)
	if err != nil {
		return nil, err
	}
	return client.Do(request)
}

func NewHTTPClientForBackend(backend domain.Backend, responseHeaderTimeout, timeout time.Duration) (*http.Client, error) {
	if backend.ProxyID == 0 {
		return &http.Client{
			Timeout:   timeout,
			Transport: newTransport(timeout, responseHeaderTimeout, nil),
		}, nil
	}
	if backend.Proxy == nil {
		return nil, errors.New("backend socks5 proxy not found")
	}
	if !backend.Proxy.Enabled {
		return nil, errors.New("backend socks5 proxy is disabled")
	}
	if strings.TrimSpace(backend.Proxy.Address) == "" {
		return nil, errors.New("backend socks5 proxy address is empty")
	}

	dialer := &socks5Dialer{
		address:        backend.Proxy.Address,
		username:       backend.Proxy.Username,
		password:       backend.Proxy.Password,
		connectTimeout: timeout,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: newTransport(timeout, responseHeaderTimeout, dialer.DialContext),
	}, nil
}

func (s *Service) clientForBackend(backend domain.Backend) (*http.Client, error) {
	if s.client != nil {
		return s.client, nil
	}
	if backend.ProxyID == 0 {
		return s.directClient, nil
	}
	if backend.Proxy == nil {
		return nil, errors.New("backend socks5 proxy not found")
	}
	if !backend.Proxy.Enabled {
		return nil, errors.New("backend socks5 proxy is disabled")
	}
	if strings.TrimSpace(backend.Proxy.Address) == "" {
		return nil, errors.New("backend socks5 proxy address is empty")
	}

	key := socksProxyKey(*backend.Proxy)
	s.mu.Lock()
	defer s.mu.Unlock()
	if client, ok := s.proxyClients[key]; ok {
		return client, nil
	}

	dialer := &socks5Dialer{
		address:        backend.Proxy.Address,
		username:       backend.Proxy.Username,
		password:       backend.Proxy.Password,
		connectTimeout: s.connectTimeout,
	}
	client := &http.Client{
		Transport: newTransport(s.connectTimeout, s.responseHeaderTimeout, dialer.DialContext),
	}
	s.proxyClients[key] = client
	return client, nil
}

func newTransport(connectTimeout, responseHeaderTimeout time.Duration, dialContext func(context.Context, string, string) (net.Conn, error)) *http.Transport {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DisableCompression:    true,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   50,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: responseHeaderTimeout,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if dialContext != nil {
		transport.Proxy = nil
		transport.DialContext = dialContext
	} else {
		transport.DialContext = (&net.Dialer{Timeout: connectTimeout}).DialContext
	}
	return transport
}

func socksProxyKey(proxy domain.SocksProxy) string {
	return strings.Join([]string{
		"proxy",
		strings.TrimSpace(proxy.Name),
		strings.TrimSpace(proxy.Address),
		strings.TrimSpace(proxy.Username),
		proxy.Password,
	}, "\x00")
}

func WriteResponse(w http.ResponseWriter, resp *http.Response) error {
	defer resp.Body.Close()

	copyHeaders(w.Header(), resp.Header)
	removeHopByHopHeaders(w.Header())
	w.WriteHeader(resp.StatusCode)

	if flusher, ok := w.(http.Flusher); ok && strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		_, err := io.Copy(flushWriter{writer: w, flusher: flusher}, resp.Body)
		return err
	}

	_, err := io.Copy(w, resp.Body)
	return err
}

func RetryableStatus(status int) bool {
	return domain.IsBackendFailureStatus(status)
}

type flushWriter struct {
	writer  io.Writer
	flusher http.Flusher
}

func (f flushWriter) Write(p []byte) (int, error) {
	n, err := f.writer.Write(p)
	if n > 0 {
		f.flusher.Flush()
	}
	return n, err
}

func buildTargetURL(base string, incoming *url.URL, requestPath string) (string, error) {
	baseURL, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return "", err
	}

	target := *baseURL
	if strings.TrimSpace(requestPath) == "" {
		requestPath = incoming.Path
	}
	target.Path = joinBasePath(baseURL.Path, requestPath)
	target.RawPath = target.Path
	target.RawQuery = incoming.RawQuery
	return target.String(), nil
}

func joinBasePath(basePath, requestPath string) string {
	basePath = strings.TrimRight(basePath, "/")
	if strings.HasSuffix(basePath, "/v1") && strings.HasPrefix(requestPath, "/v1/") {
		return basePath + strings.TrimPrefix(requestPath, "/v1")
	}
	if basePath == "" {
		return requestPath
	}
	return basePath + requestPath
}

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func removeHopByHopHeaders(header http.Header) {
	header.Del("Connection")
	header.Del("Keep-Alive")
	header.Del("Proxy-Authenticate")
	header.Del("Proxy-Authorization")
	header.Del("Proxy-Connection")
	header.Del("Te")
	header.Del("Trailer")
	header.Del("Transfer-Encoding")
	header.Del("Upgrade")
}

func bearerValue(raw string) string {
	value := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(value), "bearer ") {
		return value
	}
	return "Bearer " + value
}

func EffectiveBackendProtocolForPath(backend domain.Backend, path string) string {
	protocol := domain.NormalizeBackendProtocol(backend.Protocol)
	if protocol != domain.BackendProtocolBoth {
		return protocol
	}
	if EndpointForPath(path) == domain.EndpointMessages {
		return domain.BackendProtocolAnthropic
	}
	return domain.BackendProtocolOpenAI
}

func applyBackendAuth(header http.Header, backend domain.Backend, path string) {
	switch EffectiveBackendProtocolForPath(backend, path) {
	case domain.BackendProtocolAnthropic:
		header.Set("X-Api-Key", strings.TrimSpace(backend.APIKey))
	default:
		header.Set("Authorization", bearerValue(backend.APIKey))
	}
}

func sanitizeBackendHeaders(header http.Header, backend domain.Backend, path string) {
	switch EffectiveBackendProtocolForPath(backend, path) {
	case domain.BackendProtocolAnthropic:
		return
	default:
		header.Del("Anthropic-Version")
		header.Del("Anthropic-Beta")
	}
}
