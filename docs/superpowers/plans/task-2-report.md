# Task 2 Report — Config: LLM sub-struct

## Status

DONE

## Commit

`cc5551c feat(config): configuración LLM local (disabled por defecto)`

## What was done

### `internal/platform/config/config.go`

1. **Sentinel error added** — `errLLMBaseURLRequired` in the `var` block alongside the existing sentinel errors, following the English-message convention used by all other config sentinels in this file.

2. **`LLM` struct added** — placed immediately before the `Meilisearch.validate()` method (after the `Meilisearch` type block), mirroring its structure:
   - `BaseURL string` with `env:"LLM_BASE_URL"` (no default — not required when disabled)
   - `Model string` with `env:"LLM_MODEL" envDefault:"qwen3:4b"`
   - `Enabled bool` with `env:"LLM_ENABLED" envDefault:"false"`
   - `Timeout time.Duration` with `env:"LLM_TIMEOUT" envDefault:"30s"` — confirmed `time.Duration` is already supported by `caarlos0/env/v11` in this file (used by `Cobranza.SSEPingEvery`, `HTTP.ReadTimeout`, `Meilisearch.SyncInterval`, etc.)

3. **`LLM LLM` field added to `Config`** — placed after `Meilisearch` in the struct, consistent with the order of sub-structs and their validation calls.

4. **`LLM.validate()` method** — returns `errLLMBaseURLRequired` when `Enabled=true` and `BaseURL=""`. Returns `nil` when disabled (no BaseURL check). Called from `Config.validate()` alongside the other sub-struct validators.

### `internal/platform/config/config_test.go`

Added `LLM_BASE_URL`, `LLM_MODEL`, `LLM_ENABLED`, `LLM_TIMEOUT` to `clearAmbientEnv` so LLM env vars inherited from the shell don't leak into tests.

### `internal/platform/config/llm_test.go` (new file)

Four focused tests in `package config_test`, following the `meilisearch_test.go` pattern exactly (same `setMinimal` helper, same `//nolint:paralleltest` comment, no parallel):

| Test | Asserts |
|------|---------|
| `TestLoad_LLM_Defaults` | `Enabled=false`, `Model="qwen3:4b"`, `Timeout=30s`, `BaseURL=""` |
| `TestLoad_LLM_EnabledWithBaseURL_Valid` | `Enabled=true` + URL set → no error |
| `TestLoad_LLM_EnabledWithoutBaseURL_Fails` | `Enabled=true` + no URL → error containing "LLM_BASE_URL" |
| `TestLoad_LLM_DisabledWithoutBaseURL_Valid` | `Enabled=false` + no URL → no error |

## Verification

```
go build ./...                              ✔ (no output)
go test ./internal/platform/config/...     ✔ 38 tests pass (4 new LLM)
golangci-lint run ./internal/platform/config/...  ✔ 0 issues
lefthook pre-commit                        ✔ all 8 hooks pass
```

## Concerns

None. The implementation is straightforward and follows every existing pattern in the file without deviation.

## Changed files

- `/Volumes/M2-1TB/Developer/msp-api/internal/platform/config/config.go`
- `/Volumes/M2-1TB/Developer/msp-api/internal/platform/config/config_test.go`
- `/Volumes/M2-1TB/Developer/msp-api/internal/platform/config/llm_test.go` (new)
