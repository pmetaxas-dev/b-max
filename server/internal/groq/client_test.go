package groq

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderErrorDiagnosticsDoNotExposeResponse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"model access", 404, `{"error":{"code":"model_not_found","message":"private-goal secret-key"}}`, "model_not_found"},
		{"invalid key", 401, `{"error":{"code":"invalid_api_key","message":"private-goal secret-key"}}`, "invalid_api_key"},
		{"wrong endpoint", 404, `<html>private-goal secret-key</html>`, "check GROQ_BASE_URL"},
		{"unknown code", 500, `{"error":{"code":"private-goal secret-key","message":"private-goal secret-key"}}`, "groq returned status 500"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/openai/v1/chat/completions" {
					t.Errorf("unexpected request path: %s", r.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			client := NewClient(srv.URL, "secret-key", "test-model")
			_, err := client.CompleteJSON(context.Background(), []Message{{Role: "user", Content: "private-goal"}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
			for _, private := range []string{"private-goal", "secret-key"} {
				if strings.Contains(err.Error(), private) {
					t.Errorf("error exposes private response data")
				}
			}
		})
	}
}

func TestARateLimitIsWaitedOutOnceIfShortAndReportedIfLong(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0.05")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"code":"rate_limit_exceeded","message":"private"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"{}"}}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`)
	}))
	defer srv.Close()
	client := NewClient(srv.URL, "key", "m")
	if got, err := client.CompleteJSON(context.Background(), []Message{{Role: "user", Content: "x"}}); err != nil || got != "{}" || calls != 2 {
		t.Fatalf("a short rate limit is waited out: %q %v (calls %d)", got, err, calls)
	}
	long := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"code":"rate_limit_exceeded","message":"Please try again in 45.5s. private"}}`)
	}))
	defer long.Close()
	_, err := NewClient(long.URL, "key", "m").CompleteJSON(context.Background(), []Message{{Role: "user", Content: "x"}})
	var limited *RateLimitError
	if !errors.As(err, &limited) || limited.RetryAfterSeconds() != 46 || strings.Contains(err.Error(), "private") {
		t.Fatalf("a long rate limit is reported with its wait and nothing private: %v", err)
	}
}
