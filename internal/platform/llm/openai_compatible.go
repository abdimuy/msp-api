package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/abdimuy/msp-api/internal/platform/config"
)

// ErrLLMHTTP is the sentinel wrapped into errors returned for non-2xx HTTP
// responses. Use fmt.Errorf("...: %w", ErrLLMHTTP) so err113 is satisfied
// while the dynamic status code is still captured in the message.
var ErrLLMHTTP = errors.New("llm: http error")

// ErrLLMEmptyChoices is returned when the server responds with an empty
// choices array. This is a permanent error — the model decided to return
// nothing.
var ErrLLMEmptyChoices = errors.New("llm: empty choices in response")

// TransientError wraps errors that are safe to retry: network/timeout/context
// errors, HTTP 429 (rate-limited), and HTTP 5xx (server-side failures).
//
// Use IsTransient(err) rather than type-asserting directly; it traverses the
// error chain so that wrapping with fmt.Errorf("%w", ...) still works.
type TransientError struct {
	// Cause is the underlying error.
	Cause error
}

// Error implements the error interface.
func (e *TransientError) Error() string {
	return fmt.Sprintf("llm: transient error: %v", e.Cause)
}

// Unwrap exposes the underlying cause for errors.Is / errors.As traversal.
func (e *TransientError) Unwrap() error { return e.Cause }

// IsTransient reports whether err (or any error in its chain) is a
// TransientError. Transient errors are safe to retry; permanent errors
// (4xx except 429, JSON-decode failures) should not be retried.
func IsTransient(err error) bool {
	var t *TransientError
	return errors.As(err, &t)
}

// transient wraps cause in a TransientError.
func transient(cause error) error { return &TransientError{Cause: cause} }

// ─── wire types ──────────────────────────────────────────────────────────────

// openaiMessage is the JSON wire shape for a single chat message.
type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openaiResponseFormat is the JSON wire shape for response_format.
type openaiResponseFormat struct {
	Type       string        `json:"type"`
	JSONSchema *openaiSchema `json:"json_schema,omitempty"`
}

// openaiSchema is the JSON wire shape for a named json_schema.
type openaiSchema struct {
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema"`
	Strict bool           `json:"strict"`
}

// openaiRequest is the JSON body sent to /chat/completions.
type openaiRequest struct {
	Model          string                `json:"model"`
	Messages       []openaiMessage       `json:"messages"`
	Temperature    *float64              `json:"temperature,omitempty"`
	ResponseFormat *openaiResponseFormat `json:"response_format,omitempty"`
}

// openaiChoice is one item in the choices array of the response.
type openaiChoice struct {
	Message openaiMessage `json:"message"`
}

// openaiResponse is the top-level JSON response from /chat/completions.
type openaiResponse struct {
	Choices []openaiChoice `json:"choices"`
}

// ─── realClient ──────────────────────────────────────────────────────────────

// realClient is the production LLM client. It POSTs to an OpenAI-compatible
// /chat/completions endpoint using raw net/http (no third-party SDK) so it
// cross-compiles cleanly for GOOS=windows GOARCH=amd64 CGO_ENABLED=0.
type realClient struct {
	baseURL    string
	model      string
	httpClient *http.Client
}

// Compile-time assertion: realClient must satisfy Client.
var _ Client = (*realClient)(nil)

// newRealClient constructs a realClient from config. No network connection is
// attempted at construction time; the first Chat call validates reachability.
func newRealClient(cfg config.LLM) *realClient {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	slog.Info("llm.real_client_ready", "base_url", cfg.BaseURL, "model", cfg.Model)
	return &realClient{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		model:   cfg.Model,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Chat sends a chat-completion request and returns the content of
// choices[0].message.content on success.
//
// Error classification:
//   - Network/timeout/ctx errors → TransientError (retry-safe).
//   - HTTP 429 or 5xx           → TransientError wrapping ErrLLMHTTP.
//   - HTTP 4xx (except 429)     → permanent error wrapping ErrLLMHTTP.
//   - JSON-decode failure        → permanent error (do not retry).
func (c *realClient) Chat(ctx context.Context, req ChatReq) (string, error) {
	body, err := c.buildRequestBody(req)
	if err != nil {
		// JSON-encode failure is permanent (bad input).
		return "", fmt.Errorf("llm: marshal request: %w", err)
	}

	endpoint := c.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", transient(fmt.Errorf("build http request: %w", err))
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		// Network/timeout/ctx cancellation errors are transient.
		return "", transient(fmt.Errorf("http do: %w", err))
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", transient(fmt.Errorf("read response body: %w", err))
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		snippet := strings.TrimSpace(string(respBody))
		return "", transient(fmt.Errorf("status %d %s: %w", resp.StatusCode, snippet, ErrLLMHTTP))
	}
	if resp.StatusCode >= 400 {
		// 4xx (except 429) are permanent: bad request, auth failure, etc.
		snippet := strings.TrimSpace(string(respBody))
		return "", fmt.Errorf("status %d %s: %w", resp.StatusCode, snippet, ErrLLMHTTP)
	}

	var openaiResp openaiResponse
	if err := json.Unmarshal(respBody, &openaiResp); err != nil {
		// Malformed JSON from the server is a permanent error.
		return "", fmt.Errorf("llm: decode response: %w", err)
	}
	if len(openaiResp.Choices) == 0 {
		return "", ErrLLMEmptyChoices
	}

	return openaiResp.Choices[0].Message.Content, nil
}

// buildRequestBody marshals a ChatReq into the OpenAI-compatible JSON wire
// format. Temperature is omitted when zero.
func (c *realClient) buildRequestBody(req ChatReq) ([]byte, error) {
	msgs := make([]openaiMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = openaiMessage(m)
	}

	body := openaiRequest{
		Model:    c.model,
		Messages: msgs,
	}

	if req.Temperature != 0 {
		t := req.Temperature
		body.Temperature = &t
	}

	if req.ResponseFormat != nil {
		rf := &openaiResponseFormat{Type: req.ResponseFormat.Type}
		if req.ResponseFormat.Type == "json_schema" {
			rf.JSONSchema = &openaiSchema{
				Name:   req.ResponseFormat.Name,
				Schema: req.ResponseFormat.Schema,
				Strict: true,
			}
		}
		body.ResponseFormat = rf
	}

	return json.Marshal(body)
}
