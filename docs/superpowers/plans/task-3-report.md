# Task 3 Report — Platform package `internal/platform/llm`

## Status: DONE

## Commit
`5b5da88` — `feat(platform): cliente LLM local OpenAI-compatible con fallback disabled`

## Files created

| File | Purpose |
|---|---|
| `internal/platform/llm/doc.go` | Package doc comment (mirrors meilisearch/doc.go) |
| `internal/platform/llm/client.go` | `Client` interface + `Message`, `ChatReq`, `ResponseFormat` types |
| `internal/platform/llm/disabled.go` | `ErrLLMDisabled` sentinel + `disabledClient` (mirrors `notconfigured.go`) |
| `internal/platform/llm/openai_compatible.go` | `realClient` (raw net/http, no SDK); `TransientError`; `IsTransient`; `ErrLLMHTTP`, `ErrLLMEmptyChoices` sentinels |
| `internal/platform/llm/factory.go` | `NewClient(cfg config.LLM) Client` — disabled vs. real selection |
| `internal/platform/llm/llm_test.go` | 9 hermetic httptest-based tests |

## Verification

```
go build ./...                                          PASS
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ...   PASS
go test ./internal/platform/llm/...                    PASS — 9/9 tests (0.38s)
golangci-lint run ./internal/platform/llm/...          0 issues
lefthook pre-commit                                     all hooks green
```

## Test summary

9 tests, all parallel, all httptest-hermetic (no real network, no Ollama):
- `TestDisabledClient_ReturnsErrLLMDisabled` — `errors.Is(err, ErrLLMDisabled)`
- `TestRealClient_HappyPath` — verifies path, method, model, messages, returns content
- `TestRealClient_SendsResponseFormat` — `json_object` propagated in wire body
- `TestRealClient_SendsJSONSchemaResponseFormat` — `json_schema` + name propagated
- `TestRealClient_HTTP500_IsTransient` — `IsTransient == true`
- `TestRealClient_HTTP400_IsPermanent` — `IsTransient == false`
- `TestRealClient_HTTP429_IsTransient` — `IsTransient == true`
- `TestRealClient_InvalidJSON_IsPermanent` — bad JSON body → permanent error
- `TestRealClient_CancelledContext_ReturnsError` — cancelled ctx → error returned

## Design decisions

- **err113 compliance:** dynamic HTTP status codes wrapped as `fmt.Errorf("status %d ...: %w", code, ErrLLMHTTP)` — satisfies the linter while preserving the status in the message.
- **S1016 fix:** `openaiMessage(m)` direct struct conversion instead of field-by-field literal — works because `Message` and `openaiMessage` have identical underlying field layout.
- **`openaiMessage` layout mirrors `Message`** (same field names and types) so the conversion is zero-cost.
- **No apperror dependency** — the llm package is a pure infra utility; callers wrap with apperror if needed.
- **Factory signature** — `NewClient(cfg config.LLM) Client` (no error return, unlike meilisearch which can fail on SDK init). The brief's `Enabled` flag is binary: disabled → stub, enabled → real. Config validation already ensures BaseURL is set when Enabled, so no boot-time failure path is needed.

## Concerns

None. Package is clean, tested, cross-compiles, lint-free.
