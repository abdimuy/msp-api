# Task 8 Report — LLM Narrative Generator

## Commit SHA

`88acdbc`

## Packages created

- `internal/analytics/infra/llm` — `Generator` struct implementing `outbound.NarrativeGenerator`
- `internal/analytics/infra/llm/llmfake` — deterministic `Generator` fake for tests

## Package naming choice

The infra package lives at `internal/analytics/infra/llm` (package name `llm`). This collides with `internal/platform/llm` in any file that imports both. The importas config was extended with two new required aliases:

- `internal/platform/llm` → `platformllm`
- `internal/analytics/infra/llm` → `analyticsllm`

Both aliases are registered in `.golangci.yml` under `importas.alias` with `no-extra-aliases: true` enforcing their use.

## How `extractJSON` handles think/markdown wrappers

1. **`<think>...</think>` stripping**: iterative scan-and-remove loop. Handles multiple think blocks. Stops when no `<think>` is found.
2. **Markdown fences** (`\`\`\`json ... \`\`\``): handled naturally — the brace-scan ignores non-JSON wrapper text and finds the first `{`.
3. **Brace-depth scanner**: walks the string tracking `depth`, enters/exits string literals (with backslash-escape handling) to avoid counting `{`/`}` inside JSON string values. Returns the substring from the first `{` to the matching `}`.

## Test summary

8 tests, all pass (`ok github.com/abdimuy/msp-api/internal/analytics/infra/llm 1.1s`):

| Test | What it covers |
|------|----------------|
| `TestExtractJSON/bare_object` | plain JSON object |
| `TestExtractJSON/fenced` | markdown-fenced response |
| `TestExtractJSON/think_prefix` | `<think>` wrapper |
| `TestExtractJSON/nested` | nested JSON object |
| `TestExtractJSON/with_string_braces` | brace chars inside string values |
| `TestExtractJSON/no_json` | no JSON → `false` |
| `TestExtractJSON/only_think` | think-only → `false` |
| `TestGenerar_HappyParse` | full happy path, field mapping |
| `TestGenerar_PromptAnchoring` | prompt contains facts; schema enum matches catalog |
| `TestGenerar_MarkdownFence` | markdown-wrapped LLM response |
| `TestGenerar_ThinkWrapper` | think-wrapped LLM response |
| `TestGenerar_BadJSON` | non-JSON response → error |
| `TestGenerar_ClientError` | `ErrLLMDisabled` propagated via `errors.Is` |

## Concerns

None. All 55 linters pass (`0 issues`), pre-commit hook clean, Windows cross-compile clean.

---

## Fix pass on 88acdbc — commit f136fbc

**Status: DONE**

**Commit:** `f136fbc fix(analytics): temperatura determinista explícita en cliente LLM + pulidos de parseo`

### Fixes applied

| Fix | Change |
|-----|--------|
| FIX 1 | `ChatReq.Temperature float64` → `*float64`; added `Float64(v float64) *float64` helper in `client.go`; `buildRequestBody` now gates on `!= nil` instead of `!= 0`; generator uses `platformllm.Float64(0)` |
| FIX 2 | `TestGenerar_PromptAnchoring` now asserts `Temperature` is non-nil and `== 0` |
| FIX 3 | Deleted local `strContains` helper; replaced all call sites with `strings.Contains` (stdlib) |
| FIX 4 | `extractJSON` strips unclosed `<think>` to end-of-string (`s = s[:start]; break`) instead of `break`; added `TestExtractJSON/unclosed_think` case |

### Verification

```
go build ./...                                           PASS (no output)
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ...   PASS (no output)
golangci-lint run ./internal/platform/llm/... ./...     0 issues
```

```
go test ./internal/platform/llm/... ./internal/analytics/infra/llm/... -v
ok  github.com/abdimuy/msp-api/internal/platform/llm   0.388s  (11 tests PASS)
ok  github.com/abdimuy/msp-api/internal/analytics/infra/llm  0.586s  (14 tests PASS)
```

New platform/llm tests covering FIX 1:
- `TestRealClient_ExplicitZeroTemperature_IsSerialized` — asserts `"temperature":0` present in wire body when `Float64(0)` is set
- `TestRealClient_NilTemperature_IsOmitted` — asserts `temperature` key absent when `Temperature` is nil

**Explicit Temperature 0 is now serialized.** A bare `0.0` field was previously a Go zero value and was silently omitted; the pointer change makes omission (nil) and explicit-zero (`Float64(0)`) distinct at the type level.

## Lint fixes applied

- `importas`: added `platformllm`/`analyticsllm` aliases to `.golangci.yml`
- `err113`: replaced raw `fmt.Errorf` sentinel with `var ErrNoJSONInResponse = errors.New(...)`
- `revive/unhandled-error`: used `_, _ = fmt.Fprintf(&sb, ...)` pattern (matches codebase convention in fuzz tests) with `//nolint:revive` on `buildUserMessage`
- `staticcheck/QF1012`: switched from `WriteString(fmt.Sprintf(...))` to `fmt.Fprintf(&sb, ...)`
- `paralleltest`: added `t.Parallel()` to all tests and subtests
- `gofumpt`/`goimports`: auto-fixed import grouping via `gofumpt -w`
