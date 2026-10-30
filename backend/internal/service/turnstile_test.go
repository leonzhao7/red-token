package service

import "testing"

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
