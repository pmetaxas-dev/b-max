// Package groq is the only code that talks to Groq. Only the Go server calls
// it; the key never reaches the extension (architecture-plan-v2 §16).
package groq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ErrNotConfigured = errors.New("groq: no API key configured")

// ErrInvalidJSON: the model's answer failed Groq's JSON validation (HTTP 400,
// json_validate_failed). Retryable, like any rejected answer.
var ErrInvalidJSON = errors.New("groq: the model did not return valid JSON")

// RateLimitError: Groq's per-minute limit was reached (HTTP 429). It says how
// long to wait; a short wait is done here, a long one is told to the user.
type RateLimitError struct {
	After time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("groq returned status 429 (rate_limit_exceeded): retry in %s", e.After.Round(time.Second))
}

// RetryAfterSeconds is how long Groq asked to wait, rounded up.
func (e *RateLimitError) RetryAfterSeconds() int {
	return max(1, int((e.After+time.Second-1)/time.Second))
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

// requestTimeout: one request to Groq. The chat must feel quick, so a slow
// answer is given up on early.
const requestTimeout = 30 * time.Second

// maxCompletionTokens bounds the answer. Groq counts the requested maximum
// toward the per-minute token limit, so a large value can use up the limit by
// itself; Max's JSON answers are short.
const maxCompletionTokens = 2500

// maxRateWait: a 429 that asks for a wait up to this long is waited out, once,
// so the user does not notice; a longer one is reported.
const maxRateWait = 12 * time.Second

func NewClient(baseURL, apiKey, model string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: requestTimeout},
	}
}

// CompleteJSON sends a chat request asking for a JSON object and returns the
// raw message content.
func (c *Client) CompleteJSON(ctx context.Context, msgs []Message) (string, error) {
	if c.apiKey == "" {
		return "", ErrNotConfigured
	}
	payload := map[string]any{
		"model":                 c.model,
		"messages":              msgs,
		"temperature":           0.3,
		"response_format":       map[string]string{"type": "json_object"},
		"max_completion_tokens": maxCompletionTokens,
	}
	if strings.Contains(c.model, "gpt-oss") {
		// Reasoning models spend completion tokens on thinking; without a low
		// effort the JSON answer can be cut off mid-object.
		payload["reasoning_effort"] = "low"
	}
	body, _ := json.Marshal(payload)
	for attempt := 0; ; attempt++ {
		content, err := c.send(ctx, body)
		var limited *RateLimitError
		if attempt == 0 && errors.As(err, &limited) && limited.After <= maxRateWait {
			select {
			case <-time.After(limited.After):
				continue
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		return content, err
	}
}

var tryAgainIn = regexp.MustCompile(`try again in ([0-9hms.]+)`)

// retryAfter reads how long Groq asks to wait: the Retry-After header, else
// the "try again in 7.5s" of its message. Only the number is used, never logged.
func retryAfter(h http.Header, raw []byte) time.Duration {
	if s := h.Get("Retry-After"); s != "" {
		if secs, err := strconv.ParseFloat(s, 64); err == nil {
			return time.Duration(secs * float64(time.Second))
		}
	}
	if m := tryAgainIn.FindSubmatch(raw); m != nil {
		if d, err := time.ParseDuration(strings.TrimRight(string(m[1]), ".")); err == nil {
			return d
		}
	}
	return 20 * time.Second
}

func (c *Client) send(ctx context.Context, body []byte) (string, error) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/openai/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("groq unreachable: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		// Never log the response message/body: it may echo the private goal.
		// Only translate recognized provider codes into our own diagnostics.
		var failure struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &failure)
		log.Printf("groq %s: status %d (%s) after %s", c.model, resp.StatusCode, failure.Error.Code, time.Since(start).Round(time.Millisecond))
		if resp.StatusCode == http.StatusTooManyRequests {
			return "", &RateLimitError{After: retryAfter(resp.Header, raw)}
		}
		switch failure.Error.Code {
		case "json_validate_failed":
			// The model's answer was not valid JSON: the planner treats this like
			// any rejected answer and asks once more.
			return "", fmt.Errorf("%w (groq status %d, json_validate_failed)", ErrInvalidJSON, resp.StatusCode)
		case "context_length_exceeded", "rate_limit_exceeded", "request_too_large":
			// Known codes are safe to name; the message/body never is.
			return "", fmt.Errorf("groq returned status %d (%s)", resp.StatusCode, failure.Error.Code)
		case "model_not_found":
			return "", fmt.Errorf("groq returned status %d (model_not_found): model %q does not exist or this API key cannot access it; set GROQ_MODEL to an accessible model from GET /openai/v1/models", resp.StatusCode, c.model)
		case "invalid_api_key":
			return "", fmt.Errorf("groq returned status %d (invalid_api_key): check GROQ_API_KEY and restart the server", resp.StatusCode)
		}
		if resp.StatusCode == http.StatusNotFound {
			return "", fmt.Errorf("groq returned status 404 without a recognized error code: check GROQ_BASE_URL (expected https://api.groq.com) and GROQ_MODEL")
		}
		return "", fmt.Errorf("groq returned status %d", resp.StatusCode)
	}
	var out struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
		Usage struct {
			Prompt     int `json:"prompt_tokens"`
			Completion int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		return "", errors.New("groq returned an unreadable response")
	}
	// How long and how big, never what: it is how a slow chat is understood.
	log.Printf("groq %s: %s, prompt %d tokens, answer %d tokens", c.model, time.Since(start).Round(time.Millisecond), out.Usage.Prompt, out.Usage.Completion)
	return out.Choices[0].Message.Content, nil
}
