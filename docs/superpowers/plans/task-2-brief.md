# Task 2 — Config: LLM sub-struct

## Where this fits
Fase 2 adds a local LLM client (Ollama dev / llama-server prod). This task adds its configuration to the central config struct. The client itself (platform/llm) and its wiring come in later tasks; here we only add config fields with safe defaults. **`Enabled` defaults to `false`** — the model never runs unless explicitly turned on.

## Requirements
In `internal/platform/config/config.go`:
1. Add a sub-struct (match the EXACT style of the existing `Meilisearch` sub-struct — read it first):
   ```go
   type LLM struct {
       BaseURL string        `env:"LLM_BASE_URL"`
       Model   string        `env:"LLM_MODEL" envDefault:"qwen3:4b"`
       Enabled bool          `env:"LLM_ENABLED" envDefault:"false"`
       Timeout time.Duration `env:"LLM_TIMEOUT" envDefault:"30s"`
   }
   ```
   (Use the same field-tag conventions the file already uses. If the file uses a different duration type or parsing approach for timeouts elsewhere — e.g. an existing `*Timeout time.Duration` with `envDefault`, follow that exactly. Confirm `time.Duration` env parsing is supported the same way other timeout fields in this file are; if no other `time.Duration` env field exists, follow how Meilisearch/other sub-structs handle similar fields and keep it consistent.)
2. Embed it in the main `Config` struct as a field `LLM LLM` (match how `Meilisearch` is embedded — same naming/placement style).
3. Extend the `validate()` method: if `LLM.Enabled == true` but `LLM.BaseURL == ""`, return a sentinel validation error (English code, Spanish user-facing message per CLAUDE.md §3 — match how existing sentinel config errors in this file are defined and returned). If `Enabled == false`, BaseURL may be empty (no error). Do NOT validate BaseURL when disabled.

## Constraints
- Follow CLAUDE.md §3: error code/identifier in English; any user-facing message in Spanish, lowercase, no trailing period.
- Match existing patterns in this exact file — do not invent a new config style.
- Defaults must make `LLM_ENABLED` false out of the box.

## Tests
If `config_test.go` (or equivalent) exists for this package, add focused tests:
- Defaults: with no env set, `LLM.Enabled == false`, `LLM.Model == "qwen3:4b"`, `LLM.Timeout == 30s`.
- Validate: `Enabled=true` + empty `BaseURL` → error; `Enabled=true` + non-empty BaseURL → ok; `Enabled=false` + empty BaseURL → ok.
Follow the existing test style in that file. If the package has no test file and config isn't unit-tested, add a minimal `config_test.go` covering the above using the same loading mechanism the app uses (env.Parse or whatever the file uses). Keep test output pristine.

## Verification
- `go build ./...`
- `go test ./internal/platform/config/...`
- `golangci-lint run ./internal/platform/config/...`

## Commit
`feat(config): configuración LLM local (disabled por defecto)`. No --no-verify. No Claude attribution footer.

## Report
Write full report to `docs/superpowers/plans/task-2-report.md`. Reply ≤15 lines: status, commit SHA+subject, one-line test summary, concerns, report path.
