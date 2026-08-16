# Task 5 — Narrativa domain VO + input hash + data-shape fields on PulsoComputado/contract

## Where this fits
The narrativa+traits are owned by analytics and flow out through `ClientePulsoContract` (exactly like the Fase 1 `*Resumen` fields). This task adds the data shapes and the invalidation hash. It does NOT wire the read-path (that's a later task) — here the new fields simply exist and map through, defaulting empty.

## Files / changes

### 1. `internal/analytics/domain/narrativa.go` (new) — pure VO
```go
package domain

import "time"

// Narrativa is the materialized LLM "analyst reading" + AI-selected traits for
// one client. Texto is the Spanish analyst paragraph; Rasgos are validated
// catalog codes (see Rasgo). InputHash ties the row to the facts it was
// generated from — when the facts change, the hash changes and the row is stale.
type Narrativa struct {
	ClienteID  int
	Texto      string
	Rasgos     []string
	InputHash  string
	Modelo     string
	GeneradaEn time.Time
}
```
Domain may import only stdlib (`time` is fine). No behavior needed beyond the struct (add a small constructor only if it matches how other domain VOs in this package are built — otherwise a plain struct is fine; check a sibling VO first and match the style).

### 2. `PulsoComputado` — add two fields
In `internal/analytics/analytics_contracts_mapper.go`, append to the `PulsoComputado` struct (after `CLVResumen`):
```go
	// ─── Lectura del analista (IA) — Fase 2 ──────────────────────────────────────
	Narrativa string   // analyst paragraph (empty when LLM off / not yet generated)
	RasgosIA  []string // resolved Spanish display labels (empty when none)
```

### 3. `ClientePulsoContract` — add two fields + map them
In `internal/analytics/analytics_contracts.go`, add to `ClientePulsoContract` (read the struct first; place near the other computed/Fase-1 fields):
```go
	Narrativa string
	RasgosIA  []string
```
In `ToClientePulsoContract` (analytics_contracts_mapper.go), add the two mapping lines:
```go
		Narrativa: comp.Narrativa,
		RasgosIA:  comp.RasgosIA,
```
Because the LIST path passes a zero `PulsoComputado` for these fields, they map to empty — which is exactly the desired behavior (LIST never carries narrativa). Do not change any LIST code.

### 4. `NarrativaInputHash` — in `app`
Create `internal/analytics/app/narrativa_hash.go`:
```go
package app

// NarrativaInputHash is the invalidation key for a client's cached narrativa.
// It hashes exactly the facts the narrativa is derived from: the three bands and
// the three Fase-1 quantified titulars. When any of these change, the hash
// changes and the cached narrativa is considered stale.
func NarrativaInputHash(comp analytics.PulsoComputado) string {
	// sha256 hex of bandaCredito|bandaRecompra|bandaClv|creditoResumen|recompraResumen|clvResumen
}
```
- Import the root package as it is already imported elsewhere in `app` (check an existing `app` file — the root module package `github.com/abdimuy/msp-api/internal/analytics` is referenced as `analytics`; reuse that import).
- Join the six fields with a `|` separator (a single delimiter that cannot appear ambiguously — `|` is fine; if any field could contain `|`, that's not the case for bands and these titulars, but still join deterministically), `sha256.Sum256`, return `hex.EncodeToString(...)` (64 lowercase hex chars — matches the `CHAR(64)` column).
- Pure function, deterministic, no time/uuid.

## Constraints
- CLAUDE.md §2: domain stays pure. CLAUDE.md §3: English identifiers/comments.
- Do NOT touch the read-path (`ObtenerPulsoCliente`/`ObtenerPulsosClientes`) — only the struct/mapper/hash. New fields default empty everywhere they aren't set.

## Tests
### `internal/analytics/app/narrativa_hash_test.go`
- **Determinism:** same `PulsoComputado` → identical hash across calls; output is 64 lowercase-hex chars.
- **Sensitivity / invalidation:** changing ANY one of the six inputs (each of BandaCredito, BandaRecompra, BandaCLV, CreditoResumen, RecompraResumen, CLVResumen) changes the hash; changing a NON-input field (e.g. `Segmento`, `Score`, `RasgosIA`) does NOT change the hash.
- **Field independence:** swapping values between two fields (e.g. credito vs recompra resumen) yields a different hash than the original (guards against an order-insensitive join). 
### `internal/analytics/domain/narrativa_test.go` (only if you add a constructor) — otherwise the VO is trivially a struct and needs no test; do not write an assertion-free test.
Keep output pristine.

## Verification
- `go build ./...`
- `go test ./internal/analytics/...`
- `golangci-lint run ./internal/analytics/...`

## Commit
`feat(analytics): VO narrativa, hash de invalidación y campos de contrato para lectura IA`. No --no-verify. No Claude attribution footer.

## Report
Full report to `docs/superpowers/plans/task-5-report.md`. Reply ≤15 lines: status, commit SHA+subject, one-line test summary, concerns, report path.
