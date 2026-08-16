# Task 9 — Narrativa validation (direction check + trait filtering, with deterministic fallback)

## Where this fits
"Lo determinista filtra al LLM." The generator (Task 8) returns the model's RAW output. This task validates it before it is cached/served:
- **(a) Direction check** on the narrativa text — must not contradict the deterministic risk. On failure → fall back to the Fase-1 deterministic titulars and emit NO AI traits.
- **(b) Trait filtering** — keep only catalog codes, dedup, cap to 3.
The worker (Task 10) calls this and persists the validated result. Degrading to titulars (no regression) is the whole point.

## Read first
- `internal/analytics/ports/outbound/narrative_generator.go` (Task 6) — `NarrativeOutput`.
- `internal/analytics/analytics_contracts_mapper.go` — `PulsoComputado` (has `TierRiesgo`, `EstadoPago`, `BandaCredito`, and the Fase-1 titulars `CreditoResumen`/`RecompraResumen`/`CLVResumen`).
- `internal/analytics/app/rasgos_catalogo.go` (Task 4) — `EsRasgoValido`.
- Domain enum values (use these EXACT canonical strings; they live in `internal/analytics/domain/`): `TierRiesgo` ∈ {`AL_DIA`,`VIGILANCIA`,`EN_RIESGO`,`CRITICO`}; `BandaCredito` ∈ {`BAJO`,`MEDIO`,`ALTO`,`CRITICO`}; `EstadoPago` ∈ {`SIN_CREDITO`,`LIQUIDADO`,`AL_CORRIENTE`,`ATRASADO`,`MOROSO`}. Compare against the domain constants (`domain.TierRiesgoCritico`, etc.) — do NOT hardcode raw strings if the domain exports the constants; import and use them.

## File — `internal/analytics/app/narrativa_validate.go`

```go
package app

// ValidatedNarrativa is the post-validation result the worker persists.
type ValidatedNarrativa struct {
	Texto        string
	Rasgos       []string // validated catalog codes (≤3, deduped); empty on fallback
	UsedFallback bool     // true ⇒ direction check failed, Texto is the deterministic titular
}

// ValidarNarrativa enforces that the model's output does not contradict the
// deterministic risk, and constrains traits to the curated catalog. On a failed
// direction check it returns an EMPTY narrativa with NO traits (UsedFallback=true)
// so the ficha simply omits the AI reading and keeps showing the deterministic
// Fase-1 titulars in their existing place (no regression, no duplication, no
// contradictory AI text).
func ValidarNarrativa(raw outbound.NarrativeOutput, comp analytics.PulsoComputado) ValidatedNarrativa
```

### (a) Direction check — fail (→ empty fallback) if ANY of:
1. **Empty/degenerate length:** `strings.TrimSpace(raw.Narrativa)` is empty, shorter than ~40 runes, or longer than ~1200 runes (runaway). Use `utf8.RuneCountInString`.
2. **Contradicts high risk:** define `riesgoAlto := comp.TierRiesgo == "CRITICO" || comp.TierRiesgo == "EN_RIESGO" || comp.EstadoPago == "MOROSO" || comp.BandaCredito == "CRITICO"` (use domain constants). If `riesgoAlto` AND the narrativa (NFC-normalized + lowercased) contains any forbidden "good-payer" phrase, fail. Forbidden phrases (lowercase, Spanish): `"buen pagador"`, `"excelente pagador"`, `"muy buen pagador"`, `"buen cliente de crédito"`, `"pagador confiable"`, `"bajo riesgo"`, `"riesgo bajo"`, `"sin riesgo"`, `"paga puntual"`, `"paga a tiempo"`, `"muy cumplido"`. Keep this list as a package-level `var` so it is testable and the user can tune it at the gated step. (Normalize accents consistently — compare on the same normalization you apply to the phrases; lowercasing + NFC is enough since phrases are written in their canonical accented form.)

### Fallback result (when direction check fails)
Return `ValidatedNarrativa{Texto: "", Rasgos: nil, UsedFallback: true}`. The narrativa is intentionally EMPTY — the ficha will omit the AI reading entirely, and the deterministic Fase-1 titulars (`CreditoResumen`/`RecompraResumen`/`CLVResumen`) keep rendering in their existing UI place. Do NOT concatenate the titulars into Texto (that would duplicate them under an "IA" label). The worker still persists this empty result keyed by InputHash, so a contradictory generation is cached and not endlessly retried until the facts change.

### (b) Trait filtering (only when direction check PASSES)
From `raw.Rasgos`: keep codes where `EsRasgoValido(code)`, dedup preserving first-seen order, cap to the first 3. Result may be empty (valid narrativa, zero traits — fine). Set `UsedFallback=false`, `Texto = strings.TrimSpace(raw.Narrativa)`.

## Tests — `internal/analytics/app/narrativa_validate_test.go`
- **Happy path:** valid narrativa + `["loyal_but_stagnant","churn_risk"]` (low-risk comp) → `UsedFallback=false`, Texto preserved (trimmed), both traits kept.
- **Trait filtering:** raw rasgos `["loyal_but_stagnant","NOT_A_CODE","loyal_but_stagnant","churn_risk","price_sensitive","cash_reliable"]` → invalid dropped, dedup applied, capped to 3, order preserved → `["loyal_but_stagnant","churn_risk","price_sensitive"]`.
- **Direction fail — contradiction:** comp with `TierRiesgo="CRITICO"` + narrativa containing "es un buen pagador" → `UsedFallback=true`, `Texto==""`, `Rasgos` empty.
- **Direction fail — empty/too short:** blank or 5-char narrativa → fallback (`Texto==""`, `UsedFallback=true`).
- **Direction fail — too long:** >1200-rune narrativa → fallback.
- **Low risk + positive phrase is OK:** comp `TierRiesgo="AL_DIA"`, narrativa "buen pagador..." → NOT a fallback; `Texto` preserved, `UsedFallback=false`.
Assert real behavior; pristine output.

## Constraints
- CLAUDE.md §2: this is `app` — may import `domain`, `ports/outbound`, root `analytics`, stdlib. §3: English identifiers/comments; the forbidden-phrase strings are Spanish data.
- Pure function — no time/uuid/IO. Deterministic.
- The validator does NOT build a `domain.Narrativa` (no clienteID/hash/modelo) — that's the worker's job. It only returns `ValidatedNarrativa`.

## Verification
- `go build ./...`
- `go test ./internal/analytics/...`
- `golangci-lint run ./internal/analytics/...`

## Commit
`feat(analytics): validación de narrativa (dirección + rasgos) con fallback determinista`. No --no-verify. No Claude attribution footer.

## Report
Full report to `docs/superpowers/plans/task-9-report.md`. Reply ≤15 lines: status, commit SHA+subject, one-line test summary, concerns, report path.
