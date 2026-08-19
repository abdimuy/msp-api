# Task 3 — Platform package `internal/platform/llm` (OpenAI-compatible client + disabled fallback + factory)

## Where this fits
A generic, reusable local-LLM client living in `internal/platform/` (like `internal/platform/meilisearch/`). The analytics narrativa generator (later task) depends on this interface. It MUST follow the **factory + disabled-fallback** pattern of meilisearch so that when LLM is disabled the rest of the app gets a no-op stub that returns a sentinel error — never nil, never a panic.

This package knows NOTHING about analytics, narrativa, or traits. It is a thin OpenAI-compatible chat client.

## Read first (verbatim templates)
- `docs/superpowers/plans/anchor-points.md` → Platform Meilisearch section.
- The actual files: `internal/platform/meilisearch/client.go`, `factory.go`, `notconfigured.go`, and the top of `real.go`. Mirror their structure, doc-comment style, error-sentinel style, and constructor signatures.
- `internal/platform/config/config.go` → the `LLM` config sub-struct (added in Task 2: fields `BaseURL`, `Model`, `Enabled`, `Timeout time.Duration`). If Task 2 isn't merged yet, assume those exact fields.

## Files to create under `internal/platform/llm/`
1. **`client.go`** — the interface + request/response value types:
   ```go
   type Client interface {
       Chat(ctx context.Context, req ChatReq) (string, error)
   }
   type Message struct { Role string; Content string } // role: "system" | "user" | "assistant"
   type ChatReq struct {
       Messages       []Message
       Temperature    float64           // 0 ⇒ omit / let server default; keep simple
       ResponseFormat *ResponseFormat   // optional; nil ⇒ no constraint
   }
   // ResponseFormat carries an OpenAI-style structured-output request.
   // Support type "json_object" and "json_schema" (with a raw schema map).
   type ResponseFormat struct {
       Type   string                 // "json_object" | "json_schema"
       Schema map[string]any         // used when Type == "json_schema"
       Name   string                 // schema name when json_schema
   }
   ```
   `Chat` returns the assistant message **content string** (the caller parses JSON). Document that callers must handle `ErrLLMDisabled`.
2. **`disabled.go`** — `var ErrLLMDisabled = errors.New("llm disabled")` (English; this is an internal sentinel, not user-facing). A `disabledClient` struct whose `Chat` always returns `("", ErrLLMDisabled)`. Mirror meilisearch's `notconfigured.go`.
3. **`openai_compatible.go`** — a `realClient` using raw `net/http` (NO third-party SDK; must cross-compile clean for `GOOS=windows CGO_ENABLED=0`). It POSTs to `{BaseURL}/chat/completions` with an OpenAI-compatible JSON body `{model, messages, temperature?, response_format?}`, honors `ctx` and the configured `Timeout`, decodes `choices[0].message.content`. Constructor `newRealClient(cfg config.LLM) *realClient` (or accept baseURL/model/timeout/httpClient — match meilisearch's real constructor shape).
   - **Error classification:** export helpers or a typed error so callers can distinguish transient vs permanent. Minimal: define `type TransientError struct{ ... }` (wraps cause) returned for: network/timeout/ctx errors, HTTP 429, HTTP 5xx. Return a plain (permanent) error for HTTP 4xx (except 429) and JSON-decode failures. Provide `func IsTransient(err error) bool`. Keep it simple and well-documented.
   - Do NOT log payloads at info level; keep it quiet.
4. **`factory.go`** — `func NewClient(cfg config.LLM) Client`: if `!cfg.Enabled` return the `disabledClient`; else return `newRealClient(cfg)`. Mirror meilisearch's `factory.go` (`NewMeilisearchClient`). Add a package doc comment (a `doc.go` or top-of-factory comment) matching meilisearch's `doc.go` if it has one.

## Tests (`internal/platform/llm/*_test.go`)
- **Disabled:** `NewClient(config.LLM{Enabled:false})` → `Chat` returns `ErrLLMDisabled` (use `errors.Is`).
- **Real happy path:** spin up an `httptest.Server` returning a canned OpenAI `chat/completions` response; assert `Chat` returns the content, sends the right model/messages, and includes `response_format` when set.
- **Error classification:** httptest server returning 500 → `IsTransient(err)==true`; returning 400 → `IsTransient(err)==false`; a body that isn't valid JSON → permanent error.
- **Timeout/ctx cancellation:** canceled context → error (transient is fine).
Keep tests hermetic (httptest only — no real network, no real Ollama). Output pristine.

## Constraints
- Pure stdlib `net/http` + `encoding/json`. No new third-party deps. No cgo.
- Match meilisearch's package conventions exactly (naming, doc comments, sentinel error placement, factory shape).
- CLAUDE.md §3: identifiers/comments/error codes English. (No user-facing Spanish strings here — this is infra.)
- Do NOT call a real LLM anywhere. No `LLM_ENABLED=true` runs.

## Verification
- `go build ./...` and cross-compile sanity: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./internal/platform/llm/...`
- `go test ./internal/platform/llm/...`
- `golangci-lint run ./internal/platform/llm/...`

## Commit
`feat(platform): cliente LLM local OpenAI-compatible con fallback disabled`. No --no-verify. No Claude attribution footer.

## Report
Full report to `docs/superpowers/plans/task-3-report.md`. Reply ≤15 lines: status, commit SHA+subject, one-line test summary, concerns, report path.
