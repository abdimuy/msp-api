package llm_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/llm"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// cannedResponse returns an http.HandlerFunc that writes a fixed OpenAI-style
// chat completion response containing the given content string.
func cannedResponse(content string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": content}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// newEnabledClient builds a realClient pointed at srv.URL.
func newEnabledClient(t *testing.T, srv *httptest.Server) llm.Client {
	t.Helper()
	return llm.NewClient(config.LLM{
		Enabled: true,
		BaseURL: srv.URL,
		Model:   "test-model",
		Timeout: 5 * time.Second,
	})
}

// ─── disabled client ─────────────────────────────────────────────────────────

func TestDisabledClient_ReturnsErrLLMDisabled(t *testing.T) {
	t.Parallel()
	c := llm.NewClient(config.LLM{Enabled: false})
	_, err := c.Chat(context.Background(), llm.ChatReq{
		Messages: []llm.Message{{Role: "user", Content: "hello"}},
	})
	if !errors.Is(err, llm.ErrLLMDisabled) {
		t.Fatalf("want ErrLLMDisabled, got %v", err)
	}
}

// ─── real client happy path ───────────────────────────────────────────────────

func TestRealClient_HappyPath(t *testing.T) {
	t.Parallel()
	const wantContent = "Hola, mundo!"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify path.
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
		// Verify method.
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %q", r.Method)
		}
		// Decode and inspect the request body.
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["model"] != "test-model" {
			t.Errorf("unexpected model: %v", body["model"])
		}
		msgs, ok := body["messages"].([]any)
		if !ok || len(msgs) == 0 {
			t.Error("expected non-empty messages array")
		}

		// Write canned response.
		cannedResponse(wantContent)(w, r)
	}))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	got, err := c.Chat(context.Background(), llm.ChatReq{
		Messages: []llm.Message{{Role: "user", Content: "di hola"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != wantContent {
		t.Errorf("want %q, got %q", wantContent, got)
	}
}

func TestRealClient_SendsResponseFormat(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		rf, ok := body["response_format"].(map[string]any)
		if !ok {
			t.Fatal("expected response_format in request body")
		}
		if rf["type"] != "json_object" {
			t.Errorf("unexpected response_format.type: %v", rf["type"])
		}
		cannedResponse(`{"score":42}`)(w, r)
	}))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	_, err := c.Chat(context.Background(), llm.ChatReq{
		Messages:       []llm.Message{{Role: "user", Content: "score"}},
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRealClient_SendsJSONSchemaResponseFormat(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		rf, ok := body["response_format"].(map[string]any)
		if !ok {
			t.Fatal("expected response_format")
		}
		if rf["type"] != "json_schema" {
			t.Errorf("unexpected type: %v", rf["type"])
		}
		js, ok := rf["json_schema"].(map[string]any)
		if !ok {
			t.Fatal("expected json_schema key")
		}
		if js["name"] != "my_schema" {
			t.Errorf("unexpected schema name: %v", js["name"])
		}
		cannedResponse(`{}`)(w, r)
	}))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	_, err := c.Chat(context.Background(), llm.ChatReq{
		Messages: []llm.Message{{Role: "user", Content: "json"}},
		ResponseFormat: &llm.ResponseFormat{
			Type:   "json_schema",
			Name:   "my_schema",
			Schema: map[string]any{"type": "object"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ─── error classification ─────────────────────────────────────────────────────

func TestRealClient_HTTP500_IsTransient(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	_, err := c.Chat(context.Background(), llm.ChatReq{
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !llm.IsTransient(err) {
		t.Errorf("want transient error, got: %v", err)
	}
}

func TestRealClient_HTTP400_IsPermanent(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	_, err := c.Chat(context.Background(), llm.ChatReq{
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if llm.IsTransient(err) {
		t.Errorf("want permanent error, got transient: %v", err)
	}
}

func TestRealClient_HTTP429_IsTransient(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	_, err := c.Chat(context.Background(), llm.ChatReq{
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !llm.IsTransient(err) {
		t.Errorf("want transient error for 429, got: %v", err)
	}
}

func TestRealClient_InvalidJSON_IsPermanent(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("this is not json {{{"))
	}))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	_, err := c.Chat(context.Background(), llm.ChatReq{
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if llm.IsTransient(err) {
		t.Errorf("want permanent error for bad JSON, got transient: %v", err)
	}
}

// ─── timeout / context cancellation ──────────────────────────────────────────

func TestRealClient_CancelledContext_ReturnsError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the client context is cancelled.
		<-r.Context().Done()
		http.Error(w, "gone", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	c := newEnabledClient(t, srv)
	_, err := c.Chat(ctx, llm.ChatReq{
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	// Context cancellation is a transient/network-level error.
	if !llm.IsTransient(err) {
		t.Logf("note: cancelled context produced permanent error (acceptable): %v", err)
	}
}
