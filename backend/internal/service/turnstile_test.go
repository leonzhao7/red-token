package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestTurnstileCookiesAreScopedToRequestURL(t *testing.T) {
	const targetURL = "https://console.example/api/checkin"
	clientConnection, serverConnection := net.Pipe()
	t.Cleanup(func() { _ = clientConnection.Close() })
	t.Cleanup(func() { _ = serverConnection.Close() })
	deadline := time.Now().Add(5 * time.Second)
	_ = clientConnection.SetDeadline(deadline)
	_ = serverConnection.SetDeadline(deadline)
	serverResult := make(chan error, 1)
	go func() {
		serverResult <- func() error {
			payload, err := readTestClientWebSocketFrame(bufio.NewReader(serverConnection))
			if err != nil {
				return err
			}
			var command struct {
				ID     int    `json:"id"`
				Method string `json:"method"`
				Params struct {
					URLs []string `json:"urls"`
				} `json:"params"`
			}
			if err := json.Unmarshal(payload, &command); err != nil {
				return err
			}
			if command.Method != "Network.getCookies" || len(command.Params.URLs) != 1 || command.Params.URLs[0] != targetURL {
				_ = serverConnection.Close()
				return fmt.Errorf("unscoped cookie request: %s", payload)
			}
			return writeTestServerWebSocketFrame(serverConnection, []byte(fmt.Sprintf(`{"id":%d,"result":{"cookies":[{"name":"cf_clearance","value":"fresh","domain":"console.example","path":"/","secure":true}]}}`, command.ID)))
		}()
	}()
	client := newTurnstileCDPClient(&cdpWebSocket{conn: clientConnection, read: bufio.NewReader(clientConnection)})
	cookies, err := client.cookies(context.Background(), targetURL)
	if serverErr := <-serverResult; serverErr != nil {
		t.Fatalf("fake Chrome: %v", serverErr)
	}
	if err != nil {
		t.Fatalf("read cookies: %v", err)
	}
	if len(cookies) != 1 || cookies[0].Name != "cf_clearance" || cookies[0].Value != "fresh" || cookies[0].Domain != "console.example" || !cookies[0].Secure {
		t.Fatalf("browser cookies=%#v", cookies)
	}
}

func TestTurnstileCloudflareChallengeActive(t *testing.T) {
	tests := []struct {
		name     string
		snapshot turnstilePageSnapshot
		want     bool
		failed   bool
	}{
		{
			name: "clear relay page",
			snapshot: turnstilePageSnapshot{
				URL:   "https://console.example/",
				Title: "Console",
				Ready: "complete",
				Text:  "Sign in to your account",
			},
			want: false,
		},
		{
			name: "human verification title",
			snapshot: turnstilePageSnapshot{
				URL:   "https://console.example/",
				Title: "Verify you are human",
				Ready: "complete",
			},
			want: true,
		},
		{
			name: "challenge platform path",
			snapshot: turnstilePageSnapshot{
				URL:       "https://console.example/cdn-cgi/challenge-platform/h/g/orchestrate",
				Title:     "Console",
				Ready:     "complete",
				Challenge: true,
			},
			want: true,
		},
		{
			name: "localized human verification text",
			snapshot: turnstilePageSnapshot{
				URL:   "https://console.example/",
				Title: "安全检查",
				Ready: "complete",
				Text:  "请完成人机验证",
			},
			want: true,
		},
		{
			name: "verification failure page",
			snapshot: turnstilePageSnapshot{
				URL:   "https://console.example/",
				Title: "Verification failed",
				Ready: "complete",
			},
			want:   false,
			failed: true,
		},
		{
			name: "unexpected host",
			snapshot: turnstilePageSnapshot{
				URL:   "https://challenge.example/",
				Title: "Console",
				Ready: "complete",
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := turnstileCloudflareChallengeActive(test.snapshot, "console.example"); got != test.want {
				t.Fatalf("turnstileCloudflareChallengeActive()=%t, want %t", got, test.want)
			}
			if got := turnstileCloudflareVerificationFailed(test.snapshot); got != test.failed {
				t.Fatalf("turnstileCloudflareVerificationFailed()=%t, want %t", got, test.failed)
			}
		})
	}
}
