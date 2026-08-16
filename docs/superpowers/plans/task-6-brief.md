# Task 6 — Outbound ports: NarrativeGenerator + NarrativaRepo

## Where this fits
Analytics defines the interfaces it needs from outside: (a) a `NarrativeGenerator` (implemented later by the LLM adapter and by a fake in tests), and (b) a `NarrativaRepo` (implemented later by the Firebird adapter). This task ONLY defines the interfaces and their value types — no implementations.

## Read first
- The existing outbound ports file(s) under `internal/analytics/ports/outbound/` (e.g. `repos.go` where `WinbackRepo.GetCandidato` lives). Match the package name, doc-comment style, and how value types are declared there.
- `internal/analytics/domain/rasgo.go` and `domain/narrativa.go` (from Tasks 4 & 5) — you will reference `domain.Rasgo`.
- Confirm `ports/outbound` may import `domain` (it does already) and `github.com/shopspring/decimal` (used by existing contracts).

## Files

### `internal/analytics/ports/outbound/narrative_generator.go`
```go
package outbound

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// NarrativeInput carries the already-computed facts a generator may narrate.
// The generator MUST anchor on these facts and never invent numbers. Catalogo
// is the curated trait list (with definitions) the generator must choose from.
type NarrativeInput struct {
	ClienteID int
	Nombre    string
	Zona      string

	// Bands & scores (deterministic — the generator must not contradict them)
	Segmento      string
	TierRiesgo    string
	EstadoPago    string
	BandaCredito  string
	ScoreCredito  int
	BandaRecompra string
	ScoreRecompra int
	BandaCLV      string

	// Magnitudes
	Saldo           decimal.Decimal
	Monetary        decimal.Decimal
	MontoCLV        decimal.Decimal
	Frecuencia      int
	RecenciaDias    int
	CadenciaDias    int
	DiasAtrasoProm  int
	PctPagosATiempo decimal.Decimal

	// Fase-1 deterministic titulars (the generator synthesizes ACROSS these)
	CreditoResumen  string
	RecompraResumen string
	CLVResumen      string

	// Drivers (quantified bullet facts)
	CreditoDrivers  []string
	RecompraDrivers []string
	CLVDrivers      []string

	// The finite trait catalog the generator must pick 1-3 codes from.
	Catalogo []domain.Rasgo
}

// NarrativeOutput is the generator's raw result: a Spanish analyst paragraph and
// the trait CODES it selected (unvalidated — the app layer validates against the
// catalog and caps/dedups).
type NarrativeOutput struct {
	Narrativa string
	Rasgos    []string
}

// NarrativeGenerator produces an analyst reading + trait selection for one client
// from already-computed facts. Implementations: the local-LLM adapter (infra/llm)
// and a deterministic fake (tests). A disabled/unavailable generator returns an
// error; callers degrade gracefully.
type NarrativeGenerator interface {
	Generar(ctx context.Context, in NarrativeInput) (NarrativeOutput, error)
}
```
Adjust the exact field set ONLY if a field you reference does not exist on the source types — but all of the above exist on `PulsoComputado` or `WinbackCandidato` (verify against `analytics_contracts_mapper.go` `PulsoComputado` and the candidate accessors). Field names here are the generator's own DTO; keep them as specified.

### `internal/analytics/ports/outbound/narrativa_repo.go`
```go
package outbound

import (
	"context"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// NarrativaRow is the persisted cache record (matches MSP_AN_CLIENTE_NARRATIVA).
type NarrativaRow struct {
	ClienteID int
	Texto     string
	Rasgos    []string // validated catalog codes
	InputHash string
	Modelo    string
}

// PendienteRow is one queued client awaiting generation (MSP_AN_NARRATIVA_PENDIENTE).
type PendienteRow struct {
	ClienteID int
	InputHash string
}

// NarrativaRepo persists the narrativa cache and the bounded pending queue.
// Implemented by the Firebird adapter. All timestamps/UUIDs are set in Go by the
// adapter (CLAUDE.md §1).
type NarrativaRepo interface {
	// GetNarrativa returns the cached row for a client, or (nil, nil) if absent.
	GetNarrativa(ctx context.Context, clienteID int) (*NarrativaRow, error)
	// UpsertNarrativa inserts or updates the cached row (one per CLIENTE_ID).
	UpsertNarrativa(ctx context.Context, n domain.Narrativa) error
	// Encolar idempotently enqueues a client for generation (PK CLIENTE_ID).
	Encolar(ctx context.Context, clienteID int, inputHash string) error
	// ListarPendientes returns up to limit queued clients.
	ListarPendientes(ctx context.Context, limit int) ([]PendienteRow, error)
	// BorrarPendiente removes a client from the queue.
	BorrarPendiente(ctx context.Context, clienteID int) error
}
```

## Constraints
- CLAUDE.md §2: `ports/outbound` may import `domain`, stdlib, and `decimal`. It must NOT import `app` or `infra` (no cycles).
- CLAUDE.md §3: English identifiers/comments. Method names `Generar`/`Encolar`/etc. are the project's established Spanish-flavored domain verbs — match whatever the existing `repos.go` convention is (the existing repo uses `GetCandidato`; mixed EN/ES verbs are the house style — mirror it). Keep doc comments in English.
- Interfaces ONLY — no implementations, no tests needed for pure interface declarations (do not write assertion-free tests). If `ports/outbound` has a compile-check pattern, none is needed yet since impls come later.

## Verification
- `go build ./...`
- `golangci-lint run ./internal/analytics/...`
(There is nothing to unit-test in a pure interface/struct declaration; a build + lint pass is the verification.)

## Commit
`feat(analytics): puertos NarrativeGenerator y NarrativaRepo para lectura IA`. No --no-verify. No Claude attribution footer.

## Report
Full report to `docs/superpowers/plans/task-6-report.md`. Reply ≤15 lines: status, commit SHA+subject, one-line build/lint summary, concerns, report path.
