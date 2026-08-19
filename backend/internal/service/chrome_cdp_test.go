package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCookieHeaderForURLFiltersAndOrdersCookies(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	header := CookieHeaderForURL("https://console.example.com/admin/profile", []ChromeCDPCookie{
		{Name: "root", Value: "yes", Domain: ".example.com", Path: "/"},
		{Name: "admin", Value: "yes", Domain: "console.example.com", Path: "/admin"},
		{Name: "insecure", Value: "no", Domain: "console.example.com", Path: "/", Secure: true},
		{Name: "expired", Value: "no", Domain: "console.example.com", Path: "/", Expires: float64(now.Unix())},
		{Name: "other-host", Value: "no", Domain: "other.example.com", Path: "/"},
		{Name: "other-path", Value: "no", Domain: "console.example.com", Path: "/api"},
		{Name: "invalid;name", Value: "no", Domain: "console.example.com", Path: "/"},
	}, now)
	if want := "admin=yes; insecure=no; root=yes"; header != want {
		t.Fatalf("Cookie header=%q want %q", header, want)
	}
}

func TestCookieHeaderForURLSupportsHTTPAndSubdomains(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cookies := []ChromeCDPCookie{
		{Name: "domain", Value: "yes", Domain: ".example.com", Path: "/"},
		{Name: "secure", Value: "no", Domain: ".example.com", Path: "/", Secure: true},
	}
	if got := CookieHeaderForURL("http://sub.example.com/", cookies, now); got != "domain=yes" {
		t.Fatalf("HTTP Cookie header=%q", got)
	}
	if got := CookieHeaderForURL("https://sub.example.com/", cookies, now); got != "domain=yes; secure=no" {
		t.Fatalf("HTTPS Cookie header=%q", got)
	}
}

func TestCookieHeaderForURLDoesNotSendHostOnlyCookieToSubdomain(t *testing.T) {
	cookies := []ChromeCDPCookie{
		{Name: "host-only", Value: "no", Domain: "example.com", Path: "/"},
		{Name: "domain", Value: "yes", Domain: ".example.com", Path: "/"},
		{Name: "invalid-value", Value: "line\nbreak", Domain: ".example.com", Path: "/"},
	}
	if got := CookieHeaderForURL("https://sub.example.com/", cookies, time.Unix(1_700_000_000, 0)); got != "domain=yes" {
		t.Fatalf("Cookie header=%q", got)
	}
}

func TestWebsocketURLForEndpointRewritesLoopbackAdvertisedByHostChrome(t *testing.T) {
	got, err := websocketURLForEndpoint("ws://127.0.0.1:9222/devtools/browser/test", "http://172.30.64.1:9222")
	if err != nil {
		t.Fatalf("rewrite websocket URL: %v", err)
	}
	if want := "ws://172.30.64.1:9222/devtools/browser/test"; got != want {
		t.Fatalf("websocket URL=%q want %q", got, want)
	}
}

func TestChromeCDPDefaultEndpointIncludesConfiguredEndpointFirst(t *testing.T) {
	service := NewChromeCDPCookieService(ChromeCDPCookieServiceOptions{Endpoint: DefaultChromeCDPEndpoint})
	candidates := service.endpointCandidates()
	if len(candidates) == 0 || candidates[0] != strings.TrimRight(DefaultChromeCDPEndpoint, "/") {
		t.Fatalf("endpoint candidates=%#v", candidates)
	}
}

func TestLinuxDefaultIPv4Gateway(t *testing.T) {
	routes := "Iface\tDestination\tGateway\tFlags\neth0\t00000000\t01401EAC\t0003\n"
	if got := linuxDefaultIPv4Gateway(routes); got != "172.30.64.1" {
		t.Fatalf("gateway=%q", got)
	}
}

func TestCDPWebSocketReadsFragmentedMessage(t *testing.T) {
	frames := []byte{0x01, 5}
	frames = append(frames, []byte(`{"id"`)...)
	frames = append(frames, 0x80, 3)
	frames = append(frames, []byte(`:1}`)...)

	connection := &cdpWebSocket{read: bufio.NewReader(bytes.NewReader(frames))}
	opcode, payload, err := connection.readFrame()
	if err != nil {
		t.Fatalf("read fragmented frame: %v", err)
	}
	if opcode != 0x1 || string(payload) != `{"id":1}` {
		t.Fatalf("opcode=%d payload=%q", opcode, payload)
	}
}

func TestConsoleHeadersWithCookieValuePreservesOtherHeaders(t *testing.T) {
	got := ConsoleHeadersWithCookieValue(map[string]string{
		"authorization": "Bearer token",
		"Cookie":        "old=value",
	}, "new=value")
	if got["Authorization"] != "Bearer token" || got["Cookie"] != "new=value" || len(got) != 2 {
		t.Fatalf("headers=%#v", got)
	}
	got = ConsoleHeadersWithCookieValue(got, "")
	if _, ok := got["Cookie"]; ok {
		t.Fatalf("empty cookie did not remove Cookie header: %#v", got)
	}
	got = ConsoleHeadersWithAuthorizationValue(got, "Bearer browser-token")
	if got["Authorization"] != "Bearer browser-token" || got["X-Console"] != "" {
		t.Fatalf("Authorization update headers=%#v", got)
	}
	got = ConsoleHeadersWithAuthorizationValue(got, "")
	if got["Authorization"] != "Bearer browser-token" {
		t.Fatalf("empty Authorization did not preserve header: %#v", got)
	}
}

func TestAuthorizationHeaderFromStoragePrefersValidAccessToken(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	validToken := testAccessJWT(now.Add(time.Hour).Unix())
	expiredToken := testAccessJWT(now.Add(-time.Hour).Unix())
	entries := []chromeCDPStorageEntry{
		{Storage: "localStorage", Key: "old-token", Value: expiredToken},
		{Storage: "localStorage", Key: "user", Value: fmt.Sprintf(`{"refresh_token":"ignored","access_token":%q}`, validToken)},
	}
	if got, want := authorizationHeaderFromStorage(entries, now), "Bearer "+validToken; got != want {
		t.Fatalf("Authorization=%q want %q", got, want)
	}
}

func TestChromeCDPCookieServiceReadsWebSocket(t *testing.T) {
	tests := []struct {
		name            string
		browserFallback bool
		wantMethod      string
	}{
		{name: "page target", wantMethod: "Network.getAllCookies"},
		{name: "browser fallback", browserFallback: true, wantMethod: "Storage.getCookies"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			accessToken := testAccessJWT(time.Now().Add(time.Hour).Unix())
			client := &http.Client{Transport: chromeCDPRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				status := http.StatusOK
				body := ""
				switch request.URL.Path {
				case "/json/list":
					if test.browserFallback {
						status = http.StatusNotFound
					} else {
						body = `[{"type":"page","url":"https://console.example/","webSocketDebuggerUrl":"ws://chrome.test:9222/devtools/page/test"}]`
					}
				case "/json/version":
					body = `{"webSocketDebuggerUrl":"ws://chrome.test:9222/devtools/browser/test"}`
				default:
					status = http.StatusNotFound
				}
				return &http.Response{
					StatusCode: status,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(body)),
					Request:    request,
				}, nil
			})}
			serverResult := make(chan error, 1)
			dialContext := func(_ context.Context, _, _ string) (net.Conn, error) {
				clientConnection, serverConnection := net.Pipe()
				go func() {
					wantMethods := []string{test.wantMethod}
					if !test.browserFallback {
						wantMethods = append(wantMethods, "Runtime.evaluate")
					}
					serverResult <- serveFakeChromeWebSocket(serverConnection, wantMethods, accessToken, "")
				}()
				return clientConnection, nil
			}
			service := NewChromeCDPCookieService(ChromeCDPCookieServiceOptions{
				Endpoint:    "http://chrome.test:9222",
				HTTPClient:  client,
				DialContext: dialContext,
			})
			credentials, err := service.ReadCredentials(t.Context(), "https://console.example/admin")
			if err != nil {
				t.Fatalf("read Chrome cookies: %v", err)
			}
			if credentials.CookieHeader != "session=from-chrome" {
				t.Fatalf("Cookie header=%q", credentials.CookieHeader)
			}
			if test.browserFallback && credentials.Authorization != "" {
				t.Fatalf("browser fallback Authorization=%q", credentials.Authorization)
			}
			if !test.browserFallback && credentials.Authorization != "Bearer "+accessToken {
				t.Fatalf("page Authorization=%q", credentials.Authorization)
			}
			if err := <-serverResult; err != nil {
				t.Fatalf("fake Chrome WebSocket: %v", err)
			}
		})
	}
}

func TestChromeCDPCookieServiceCapturesAuthorizationFromNetwork(t *testing.T) {
	accessToken := testAccessJWT(time.Now().Add(time.Hour).Unix())
	client := &http.Client{Transport: chromeCDPRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := `[ {"type":"page","url":"https://console.example/","webSocketDebuggerUrl":"ws://chrome.test:9222/devtools/page/test"} ]`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}
	serverResult := make(chan error, 1)
	dialContext := func(_ context.Context, _, _ string) (net.Conn, error) {
		clientConnection, serverConnection := net.Pipe()
		go func() {
			serverResult <- serveFakeChromeWebSocket(serverConnection, []string{
				"Network.getAllCookies",
				"Runtime.evaluate",
				"Network.enable",
				"Page.reload",
			}, "", accessToken)
		}()
		return clientConnection, nil
	}
	service := NewChromeCDPCookieService(ChromeCDPCookieServiceOptions{
		Endpoint:    "http://chrome.test:9222",
		HTTPClient:  client,
		DialContext: dialContext,
	})
	credentials, err := service.ReadCredentials(t.Context(), "https://console.example/admin")
	if err != nil {
		t.Fatalf("read Chrome credentials: %v", err)
	}
	if credentials.Authorization != "Bearer "+accessToken {
		t.Fatalf("Authorization=%q", credentials.Authorization)
	}
	if err := <-serverResult; err != nil {
		t.Fatalf("fake Chrome WebSocket: %v", err)
	}
}

type chromeCDPRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn chromeCDPRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func serveFakeChromeWebSocket(connection net.Conn, wantMethods []string, storageToken, networkToken string) error {
	defer connection.Close()
	reader := bufio.NewReader(connection)
	request, err := http.ReadRequest(reader)
	if err != nil {
		return fmt.Errorf("read handshake: %w", err)
	}
	_ = request.Body.Close()
	key := request.Header.Get("Sec-WebSocket-Key")
	accept := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	if _, err := fmt.Fprintf(connection, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(accept[:])); err != nil {
		return fmt.Errorf("write handshake: %w", err)
	}
	for _, wantMethod := range wantMethods {
		payload, err := readTestClientWebSocketFrame(reader)
		if err != nil {
			return err
		}
		var command struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(payload, &command); err != nil {
			return fmt.Errorf("decode command: %w", err)
		}
		if command.Method != wantMethod {
			return fmt.Errorf("method=%q want %q", command.Method, wantMethod)
		}
		var response []byte
		if command.Method == "Runtime.evaluate" {
			storageValue := "{}"
			if storageToken != "" {
				storageValue = fmt.Sprintf(`{"access_token":%q}`, storageToken)
			}
			response = []byte(fmt.Sprintf(`{"id":%d,"result":{"result":{"type":"object","value":[{"storage":"localStorage","key":"user","value":%q}]}}}`, command.ID, storageValue))
		} else if command.Method == "Page.reload" {
			response = []byte(fmt.Sprintf(`{"id":%d,"result":{}}`, command.ID))
		} else if command.Method == "Network.enable" {
			response = []byte(fmt.Sprintf(`{"id":%d,"result":{}}`, command.ID))
		} else {
			response = []byte(fmt.Sprintf(`{"id":%d,"result":{"cookies":[{"name":"session","value":"from-chrome","domain":"console.example","path":"/"}]}}`, command.ID))
		}
		if err := writeTestServerWebSocketFrame(connection, response); err != nil {
			return err
		}
		if command.Method == "Page.reload" {
			event := []byte(fmt.Sprintf(`{"method":"Network.requestWillBeSent","params":{"request":{"url":"https://console.example/api/status","headers":{"Authorization":%q}}}}`, "Bearer "+networkToken))
			if err := writeTestServerWebSocketFrame(connection, event); err != nil {
				return err
			}
		}
	}
	return nil
}

func testAccessJWT(expires int64) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"token_use":"access","exp":%d}`, expires)))
	signature := base64.RawURLEncoding.EncodeToString([]byte("test-signature"))
	return strings.Join([]string{header, payload, signature}, ".")
}

func readTestClientWebSocketFrame(reader *bufio.Reader) ([]byte, error) {
	first, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	second, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	if first != 0x81 || second&0x80 == 0 {
		return nil, errors.New("client did not send a masked text frame")
	}
	length := uint64(second & 0x7f)
	if length == 126 {
		var extended [2]byte
		if _, err := io.ReadFull(reader, extended[:]); err != nil {
			return nil, err
		}
		length = uint64(binary.BigEndian.Uint16(extended[:]))
	} else if length == 127 {
		var extended [8]byte
		if _, err := io.ReadFull(reader, extended[:]); err != nil {
			return nil, err
		}
		length = binary.BigEndian.Uint64(extended[:])
	}
	if length > 1<<20 {
		return nil, errors.New("client frame is too large")
	}
	var mask [4]byte
	if _, err := io.ReadFull(reader, mask[:]); err != nil {
		return nil, err
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	for index := range payload {
		payload[index] ^= mask[index%4]
	}
	return payload, nil
}

func writeTestServerWebSocketFrame(writer io.Writer, payload []byte) error {
	header := []byte{0x81}
	switch {
	case len(payload) < 126:
		header = append(header, byte(len(payload)))
	case len(payload) <= int(^uint16(0)):
		header = append(header, 126, byte(len(payload)>>8), byte(len(payload)))
	default:
		return errors.New("test response is too large")
	}
	if _, err := writer.Write(header); err != nil {
		return err
	}
	_, err := writer.Write(payload)
	return err
}
