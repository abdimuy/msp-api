# Reporte Cobranza Semanal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend `internal/rutas/` to add a weekly cobranza report to `GET /v2/rutas` (two metrics per zona) and a new `GET /v2/rutas/{zona}/cobranza` breakdown endpoint.

**Architecture:** Add a new outbound port `CalendarioCobradorClient` (Firestore) and `CobranzaRepo` (Firebird) to the rutas module; implement both adapters outside the module boundary (Firestore adapter in `cmd/api/rutas_wiring.go`, Firebird repo in `rutasfb`); wire into the existing `Service`; extend Huma operations in `rutashttp`. The `aporteVenta` formula lives as a pure Go function in `domain/` and is unit-tested exhaustively before any I/O code is written.

**Tech Stack:** Go 1.25, Firebird (firebirdsql), Firestore (`cloud.google.com/go/firestore`), `github.com/shopspring/decimal`, Huma v2, chi v5, testify, fx.

## Global Constraints

- No logic in the database (§1 CLAUDE.md): all computation in Go; read-only queries only, no migrations.
- Vertical slices (§2): `rutas` must NOT import `internal/auth/infra/firebase` nor any other module's `domain/`, `app/`, or `infra/`. The Firestore adapter lives in `cmd/api/` and satisfies the rutas outbound port interface.
- Code English / messages Spanish (§3).
- Money: `decimal.Decimal` in domain/app; `string` in DTO (`.StringFixed(2)`).
- `golangci-lint run ./...` must pass with 0 issues before every commit (see pre-commit hook).
- All tests: `go test ./internal/rutas/...`; Firebird integration tests use `requireFBEnv` + `fbtestutil.WithTestTransaction`; do not write to shared DB.
- Commit: `feat(rutas): <subject>` style; no Co-Authored-By or attribution footer.
- Module path: `github.com/abdimuy/msp-api`.
- `//nolint:misspell // rutas vocabulary is Spanish per project convention.` on every file in `internal/rutas/`.

---

## File Map

**New files:**
- `internal/rutas/domain/aporte.go` — pure `AporteVenta` formula + `Frecuencia` type + `cadenciaDias`.
- `internal/rutas/domain/aporte_test.go` — 5 mandatory unit tests + edge cases.
- `internal/rutas/domain/cobranza.go` — read-models: `VentaCobranza`, `ReporteZona`.
- `internal/rutas/ports/outbound/cobranza.go` — `CobranzaRepo` + `CalendarioCobradorClient` interfaces.
- `internal/rutas/app/cobranza_semanal.go` — `ReporteCobranzaSemanal` + `DesglosePorZona` methods on `Service`.
- `internal/rutas/app/cobranza_semanal_test.go` — service tests with fakes.
- `internal/rutas/infra/rutasfb/cobranza_repo.go` — Firebird implementation of `CobranzaRepo`.
- `internal/rutas/infra/rutasfb/cobranza_repo_test.go` — integration test (requireFBEnv).
- `internal/rutas/infra/rutashttp/cobranza_dto.go` — new DTOs for the two new endpoints.
- `internal/rutas/infra/rutashttp/cobranza_handlers.go` — handlers for enriched listar + desglose.

**Modified files:**
- `internal/rutas/domain/ruta.go` — add `PctCoberturaSemanal`, `PctPonderadoSemanal`, `FechaInicioSemana` fields to `RutaResumen`.
- `internal/rutas/ports/outbound/repos.go` — add `CobranzaRepo` and `CalendarioCobradorClient` to the interface file (or keep in the new file; new file preferred to avoid touching the old one).
- `internal/rutas/app/listar_rutas.go` — `Service` gains two new dependency fields; `NewService` signature changes to accept them; `ListarRutas` merges metrics from Firestore + Firebird.
- `internal/rutas/app/listar_rutas_test.go` — update `fakeRutasRepo` + test to cover new fields.
- `internal/rutas/infra/rutashttp/dto.go` — add `PctCoberturaSemanal *string`, `PctPonderadoSemanal *string`, `FechaInicioSemana *string` to `RutaResumenDTO`.
- `internal/rutas/infra/rutashttp/handlers.go` — `toRutaResumenDTOs` maps new fields.
- `internal/rutas/infra/rutashttp/routes.go` — register new `GET /rutas/{zona_id}/cobranza` operation.
- `cmd/api/rutas_wiring.go` — add `provideCalendarioCobradorClient`, `provideCobranzaRutasRepo`, update `provideRutasService`.
- `cmd/api/server.go` — update `provideRootHandler` signature to inject new deps for rutas.

---

## Task 1: Pure formula `AporteVenta` + unit tests

**Files:**
- Create: `internal/rutas/domain/aporte.go`
- Create: `internal/rutas/domain/aporte_test.go`

**Interfaces:**
- Produces: `Frecuencia` string type (`Semanal`, `Quincenal`, `Mensual`), `CadenciaDias(f Frecuencia) int`, `AporteInput` struct, `CalcAporte(in AporteInput) decimal.Decimal`.

- [ ] **Step 1: Write the failing tests**

Create `internal/rutas/domain/aporte_test.go`:

```go
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

func TestCalcAporte(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		parcialidad  string
		plazos       string
		totalImporte string
		abonoSemana  string
		saldoHoy     string
		want         string
	}{
		// 1. Al corriente, paga 1x
		{"al_corriente_1x", "100", "10", "4000", "100", "2900", "1.00"},
		// 2. Atrasado, paga 2x
		{"atrasado_2x", "100", "10", "4000", "200", "3300", "2.00"},
		// 3. Paga la mitad — verifica división decimal
		{"paga_mitad", "200", "5", "4000", "100", "3000", "0.50"},
		// 4. Vieja pasada de plazo — verifica tope debia
		{"vieja_tope_debia", "200", "400", "4000", "600", "1400", "3.00"},
		// 5. Al corriente, paga de más
		{"al_corriente_paga_demas", "100", "10", "4000", "300", "2800", "2.00"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := rutasdomain.AporteInput{
				Parcialidad:  decimal.RequireFromString(tc.parcialidad),
				Plazos:       decimal.RequireFromString(tc.plazos),
				TotalImporte: decimal.RequireFromString(tc.totalImporte),
				AbonoSemana:  decimal.RequireFromString(tc.abonoSemana),
				SaldoHoy:     decimal.RequireFromString(tc.saldoHoy),
			}
			got := rutasdomain.CalcAporte(in)
			assert.True(t,
				decimal.RequireFromString(tc.want).Equal(got),
				"CalcAporte(%+v) = %s, want %s", in, got.StringFixed(4), tc.want,
			)
		})
	}
}

func TestCalcAporte_ZeroParcialidad(t *testing.T) {
	t.Parallel()
	in := rutasdomain.AporteInput{
		Parcialidad:  decimal.Zero,
		Plazos:       decimal.NewFromInt(5),
		TotalImporte: decimal.NewFromInt(4000),
		AbonoSemana:  decimal.NewFromInt(100),
		SaldoHoy:     decimal.NewFromInt(3000),
	}
	got := rutasdomain.CalcAporte(in)
	assert.True(t, decimal.Zero.Equal(got), "zero parcialidad must yield 0, got %s", got)
}

func TestCadenciaDias(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 7, rutasdomain.CadenciaDias(rutasdomain.Semanal))
	assert.Equal(t, 15, rutasdomain.CadenciaDias(rutasdomain.Quincenal))
	assert.Equal(t, 30, rutasdomain.CadenciaDias(rutasdomain.Mensual))
	assert.Equal(t, 7, rutasdomain.CadenciaDias("UNKNOWN")) // default Semanal
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd /Volumes/M2-1TB/Developer/msp-api && go test ./internal/rutas/domain/... 2>&1 | head -20
```
Expected: compile error — `domain` package has no `CalcAporte`, `AporteInput`, `Frecuencia` symbols yet.

- [ ] **Step 3: Implement `aporte.go`**

Create `internal/rutas/domain/aporte.go`:

```go
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package domain

import "github.com/shopspring/decimal"

// Frecuencia represents the payment cadence stored in LIBRES_CARGOS_CC.FORMA_DE_PAGO
// (resolved via LISTAS_ATRIBUTOS.VALOR_DESPLEGADO → "SEMANAL"/"QUINCENAL"/"MENSUAL").
type Frecuencia string

const (
	Semanal   Frecuencia = "SEMANAL"
	Quincenal Frecuencia = "QUINCENAL"
	Mensual   Frecuencia = "MENSUAL"
)

// CadenciaDias returns the number of days between expected payments for a
// given cadence. Defaults to 7 (semanal) for any unrecognized value.
func CadenciaDias(f Frecuencia) int {
	switch f {
	case Quincenal:
		return 15
	case Mensual:
		return 30
	default:
		return 7
	}
}

// AporteInput holds the inputs to CalcAporte. All fields are decimal to
// guarantee fractional precision — NUMERIC columns from Firebird must be
// scanned with firebird.ScanDecimal before being placed here.
type AporteInput struct {
	// Parcialidad is the expected periodic payment amount (LIBRES_CARGOS_CC).
	Parcialidad decimal.Decimal
	// Plazos is (fechaInicioSemana − fechaCargo) / cadenciaDias, may be fractional.
	Plazos decimal.Decimal
	// TotalImporte is the original credit total (MSP_SALDOS_VENTAS.TOTAL_IMPORTE).
	TotalImporte decimal.Decimal
	// AbonoSemana is the sum of all valid payments in the reporting window.
	AbonoSemana decimal.Decimal
	// SaldoHoy is the current outstanding balance (MSP_SALDOS_VENTAS.SALDO).
	SaldoHoy decimal.Decimal
}

// CalcAporte computes how many "quotas" this venta contributed during the
// reporting window, capped correctly for overdue accounts.
//
// Formula (all decimal, never integer division):
//
//	saldoAlInicio = SaldoHoy + AbonoSemana
//	pagadoAntes   = TotalImporte − saldoAlInicio
//	debia         = MIN(Parcialidad × Plazos, TotalImporte)   ← cap to credit total
//	vencidas      = MAX(0, (debia − pagadoAntes) / Parcialidad)
//	aporte        = MIN(AbonoSemana / Parcialidad, vencidas + 1)
//
// Returns decimal.Zero when Parcialidad ≤ 0 (guard against divide-by-zero and
// non-credit accounts).
func CalcAporte(in AporteInput) decimal.Decimal {
	if in.Parcialidad.IsZero() || in.Parcialidad.IsNegative() {
		return decimal.Zero
	}

	saldoAlInicio := in.SaldoHoy.Add(in.AbonoSemana)
	pagadoAntes := in.TotalImporte.Sub(saldoAlInicio)

	// debia = MIN(Parcialidad × Plazos, TotalImporte)
	expectedDebt := in.Parcialidad.Mul(in.Plazos)
	debia := decimal.Min(expectedDebt, in.TotalImporte)

	// vencidas = MAX(0, (debia − pagadoAntes) / Parcialidad)
	diff := debia.Sub(pagadoAntes)
	vencidasRaw := diff.Div(in.Parcialidad)
	vencidas := decimal.Max(decimal.Zero, vencidasRaw)

	// aporte = MIN(AbonoSemana / Parcialidad, vencidas + 1)
	abonoEnCuotas := in.AbonoSemana.Div(in.Parcialidad)
	return decimal.Min(abonoEnCuotas, vencidas.Add(decimal.NewFromInt(1)))
}
```

- [ ] **Step 4: Run tests and confirm all 7 pass**

```bash
cd /Volumes/M2-1TB/Developer/msp-api && go test ./internal/rutas/domain/... -v -run TestCalcAporte 2>&1
```
Expected: 7 tests PASS (5 mandatory cases + ZeroParcialidad + CadenciaDias).

- [ ] **Step 5: Commit**

```bash
cd /Volumes/M2-1TB/Developer/msp-api
git add internal/rutas/domain/aporte.go internal/rutas/domain/aporte_test.go
git commit -m "feat(rutas): fórmula CalcAporte + 7 casos unitarios"
```

---

## Task 2: Domain read-models for cobranza

**Files:**
- Create: `internal/rutas/domain/cobranza.go`

**Interfaces:**
- Produces: `VentaCobranza` struct, `ReporteZona` struct — used by the outbound port and app service.

- [ ] **Step 1: Create `cobranza.go`**

```go
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// VentaCobranza is the per-sale read-model used by the cobranza breakdown
// endpoint (GET /v2/rutas/{zona_id}/cobranza).
type VentaCobranza struct {
	// VentaID is MSP_SALDOS_VENTAS.DOCTO_CC_ID.
	VentaID int
	// ClienteID is MSP_SALDOS_VENTAS.CLIENTE_ID.
	ClienteID int
	// ZonaID is MSP_SALDOS_VENTAS.ZONA_CLIENTE_ID.
	ZonaID int
	// Parcialidad from LIBRES_CARGOS_CC.
	Parcialidad decimal.Decimal
	// Frecuencia resolved from LISTAS_ATRIBUTOS via LIBRES_CARGOS_CC.FORMA_DE_PAGO.
	Frecuencia Frecuencia
	// AbonoSemana is the sum of valid payments in the reporting window.
	AbonoSemana decimal.Decimal
	// Vencidas is the overdue quota count used in the aporte calculation.
	Vencidas decimal.Decimal
	// Aporte is the calculated contribution (CalcAporte result).
	Aporte decimal.Decimal
	// Saldo is MSP_SALDOS_VENTAS.SALDO (current outstanding balance).
	Saldo decimal.Decimal
	// FechaUltPago from MSP_SALDOS_VENTAS (used for next-due inference).
	FechaUltPago *time.Time
	// FechaCargo from MSP_SALDOS_VENTAS (used for plazos computation).
	FechaCargo time.Time
	// TotalImporte from MSP_SALDOS_VENTAS.
	TotalImporte decimal.Decimal
}

// ReporteZona aggregates the two weekly metrics for a single zona.
// Nil pointers mean Firestore had no calendar entry for the cobrador
// (dev mode or unconfigured) — the API returns JSON null for those fields.
type ReporteZona struct {
	// ZonaID matches RutaResumen.ZonaID.
	ZonaID int
	// PctCoberturaSemanal is numerador/denominador×100 for coverage.
	// Nil when FechaInicioSemana is unknown.
	PctCoberturaSemanal *decimal.Decimal
	// PctPonderadoSemanal is SUM(aporte)/denominadorPonderado×100.
	// Nil when FechaInicioSemana is unknown.
	PctPonderadoSemanal *decimal.Decimal
	// FechaInicioSemana is the cobrador's week-start timestamp from Firestore.
	// Nil when not available.
	FechaInicioSemana *time.Time
}
```

- [ ] **Step 2: Build check**

```bash
cd /Volumes/M2-1TB/Developer/msp-api && go build ./internal/rutas/... 2>&1
```
Expected: success (no errors).

- [ ] **Step 3: Commit**

```bash
git add internal/rutas/domain/cobranza.go
git commit -m "feat(rutas): read-models VentaCobranza y ReporteZona"
```

---

## Task 3: Outbound ports for cobranza

**Files:**
- Create: `internal/rutas/ports/outbound/cobranza.go`

**Interfaces:**
- Consumes: `domain.VentaCobranza`, `domain.ReporteZona` (from Task 2).
- Produces: `CobranzaRepo` interface, `CalendarioCobradorClient` interface.

- [ ] **Step 1: Create the port file**

```go
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package outbound

import (
	"context"
	"time"

	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

// CobranzaRepo is the read port for cobranza weekly metrics and breakdown.
// Implemented by rutasfb.CobranzaRepo; fakes used in app-layer tests.
type CobranzaRepo interface {
	// VentasPorZona returns the enriched venta rows for a single zona,
	// restricted to active sales (CARGO_CANCELADO <> 'S', SALDO > 0 OR
	// paid in window). The caller provides the reporting window so the
	// query can filter pagos without a second round-trip.
	VentasPorZona(ctx context.Context, zonaID int, desde, hasta time.Time) ([]rutasdomain.VentaCobranza, error)
}

// CalendarioCobradorClient is the read port for the Firestore-backed
// cobrador calendar. Returns a map from COBRADOR_ID to the week-start
// FECHA_CARGA_INICIAL. Missing cobradores in the map → nil FechaInicioSemana
// (the service treats them as "no calendar entry").
//
// Implementations must be safe for concurrent use. A Firestore-unavailable
// environment (dev mode, unconfigured) should return an empty map without error.
type CalendarioCobradorClient interface {
	FechaInicioPorCobrador(ctx context.Context) (map[int]time.Time, error)
}
```

- [ ] **Step 2: Build check**

```bash
cd /Volumes/M2-1TB/Developer/msp-api && go build ./internal/rutas/... 2>&1
```
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add internal/rutas/ports/outbound/cobranza.go
git commit -m "feat(rutas): puertos outbound CobranzaRepo y CalendarioCobradorClient"
```

---

## Task 4: Extend `RutaResumen` domain type + update Service signature

**Files:**
- Modify: `internal/rutas/domain/ruta.go`
- Modify: `internal/rutas/app/listar_rutas.go`
- Modify: `internal/rutas/app/listar_rutas_test.go`

**Interfaces:**
- Consumes: `domain.ReporteZona` (Task 2), `outbound.CobranzaRepo` + `outbound.CalendarioCobradorClient` (Task 3).
- Produces: updated `Service` with `NewService(repo, cobranzaRepo, calendario)`.

- [ ] **Step 1: Extend `RutaResumen`**

In `internal/rutas/domain/ruta.go`, add three new fields after `SaldoTotal`:

```go
	// PctCoberturaSemanal is the coverage percentage for the current week.
	// Nil when the cobrador has no Firestore calendar entry (dev/unconfigured).
	PctCoberturaSemanal *decimal.Decimal
	// PctPonderadoSemanal is the weighted payment percentage for the current week.
	// Nil when the cobrador has no Firestore calendar entry.
	PctPonderadoSemanal *decimal.Decimal
	// FechaInicioSemana is the cobrador's week-start timestamp from Firestore.
	// Nil when not available.
	FechaInicioSemana *time.Time
```

Also add `"time"` to the imports.

- [ ] **Step 2: Update `Service` in `listar_rutas.go`**

Replace the entire `listar_rutas.go`:

```go
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package app

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
	"github.com/abdimuy/msp-api/internal/rutas/ports/outbound"
)

// Service is the rutas module's query surface.
type Service struct {
	repo       outbound.RutasRepo
	cobranza   outbound.CobranzaRepo
	calendario outbound.CalendarioCobradorClient
}

// NewService builds a Service wired against the given dependencies.
func NewService(
	repo outbound.RutasRepo,
	cobranza outbound.CobranzaRepo,
	calendario outbound.CalendarioCobradorClient,
) *Service {
	return &Service{repo: repo, cobranza: cobranza, calendario: calendario}
}

// ListarRutas returns all zonas with cobrador, client count, total balance,
// and weekly cobranza metrics (pct_cobertura_semanal, pct_ponderado_semanal).
// Zones whose cobrador has no Firestore calendar entry return nil metrics.
func (s *Service) ListarRutas(ctx context.Context) ([]rutasdomain.RutaResumen, error) {
	rutas, err := s.repo.ListarRutas(ctx)
	if err != nil {
		return nil, err
	}

	// Fetch cobrador calendar. A missing/empty map is non-fatal: zones
	// without a calendar entry will have nil metrics.
	calendario, err := s.calendario.FechaInicioPorCobrador(ctx)
	if err != nil {
		// Non-fatal: log is surfaced upstream; return rutas without metrics.
		calendario = map[int]time.Time{}
	}

	now := time.Now().UTC()

	for i, r := range rutas {
		if r.CobradorID == nil {
			continue
		}
		fechaInicio, ok := calendario[*r.CobradorID]
		if !ok {
			continue
		}
		// Fetch ventas for this zona within the reporting window.
		ventas, verr := s.cobranza.VentasPorZona(ctx, r.ZonaID, fechaInicio, now)
		if verr != nil {
			// Non-fatal per-zona: leave metrics nil and continue.
			continue
		}
		reporte := calcReporteZona(r.ZonaID, ventas, fechaInicio, now)
		fi := fechaInicio
		rutas[i].FechaInicioSemana = &fi
		rutas[i].PctCoberturaSemanal = reporte.PctCoberturaSemanal
		rutas[i].PctPonderadoSemanal = reporte.PctPonderadoSemanal
	}
	return rutas, nil
}

// calcReporteZona computes the two weekly metrics for a set of ventas.
// This function is pure (no I/O) and is exercised by unit tests.
func calcReporteZona(
	zonaID int,
	ventas []rutasdomain.VentaCobranza,
	fechaInicio, now time.Time,
) rutasdomain.ReporteZona {
	var (
		coberturaNum int
		coberturaDen int
		aporteSum    decimal.Decimal
		aporteDen    int
	)

	for _, v := range ventas {
		// Denominador cobertura: ventas activas (SALDO > 0 OR pagó en ventana).
		coberturaDen++

		// Numerador cobertura: pagó algo en la ventana.
		if v.AbonoSemana.IsPositive() {
			coberturaNum++
		}

		// Ponderado: cadencia determina si la venta "aplica".
		// SEMANAL: siempre aplica.
		// QUINCENAL/MENSUAL: solo si su próximo vencimiento cae en la ventana.
		// NOTE: La regla de quincenal/mensual es un supuesto a confirmar en
		// producción. El próximo vencimiento se infiere como
		// FechaUltPago + cadenciaDias. Si FechaUltPago es nil (nunca pagó)
		// se usa FechaCargo como base.
		if v.Frecuencia == rutasdomain.Semanal || ventaAplicaEnVentana(v, fechaInicio, now) {
			aporteDen++
			aporteSum = aporteSum.Add(v.Aporte)
		}
	}

	reporte := rutasdomain.ReporteZona{ZonaID: zonaID}

	if coberturaDen > 0 {
		pct := decimal.NewFromInt(int64(coberturaNum)).
			Div(decimal.NewFromInt(int64(coberturaDen))).
			Mul(decimal.NewFromInt(100))
		reporte.PctCoberturaSemanal = &pct
	}

	if aporteDen > 0 {
		pct := aporteSum.
			Div(decimal.NewFromInt(int64(aporteDen))).
			Mul(decimal.NewFromInt(100))
		reporte.PctPonderadoSemanal = &pct
	}

	return reporte
}

// ventaAplicaEnVentana returns true when a QUINCENAL or MENSUAL venta has
// its next expected payment falling within [fechaInicio, now].
// The next due date is inferred as FechaUltPago + cadenciaDias; when
// FechaUltPago is nil, FechaCargo is used as the base.
func ventaAplicaEnVentana(v rutasdomain.VentaCobranza, fechaInicio, now time.Time) bool {
	cadencia := rutasdomain.CadenciaDias(v.Frecuencia)
	base := v.FechaCargo
	if v.FechaUltPago != nil {
		base = *v.FechaUltPago
	}
	nextDue := base.AddDate(0, 0, cadencia)
	return !nextDue.Before(fechaInicio) && !nextDue.After(now)
}
```

- [ ] **Step 3: Update `listar_rutas_test.go`**

Replace the entire test file:

```go
//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

// fakeRutasRepo is a test double for outbound.RutasRepo.
type fakeRutasRepo struct {
	rows []rutasdomain.RutaResumen
	err  error
}

func (f *fakeRutasRepo) ListarRutas(_ context.Context) ([]rutasdomain.RutaResumen, error) {
	return f.rows, f.err
}

// fakeCobranzaRepo is a test double for outbound.CobranzaRepo.
type fakeCobranzaRepo struct {
	rows map[int][]rutasdomain.VentaCobranza
	err  error
}

func (f *fakeCobranzaRepo) VentasPorZona(_ context.Context, zonaID int, _, _ time.Time) ([]rutasdomain.VentaCobranza, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows[zonaID], nil
}

// fakeCalendario is a test double for outbound.CalendarioCobradorClient.
type fakeCalendario struct {
	m   map[int]time.Time
	err error
}

func (f *fakeCalendario) FechaInicioPorCobrador(_ context.Context) (map[int]time.Time, error) {
	return f.m, f.err
}

func intPtr(v int) *int { return &v }

func TestService_ListarRutas_NoMetrics(t *testing.T) {
	t.Parallel()

	cobradorID := 5
	rows := []rutasdomain.RutaResumen{
		{
			ZonaID:         1,
			ZonaNombre:     "Norte",
			CobradorID:     &cobradorID,
			CobradorNombre: "Juan Pérez",
			NumClientes:    42,
			SaldoTotal:     decimal.NewFromFloat(15000.50),
		},
		{
			ZonaID:         2,
			ZonaNombre:     "Sur",
			CobradorID:     nil,
			CobradorNombre: "",
			NumClientes:    0,
			SaldoTotal:     decimal.Zero,
		},
	}

	// Empty calendar → no metrics.
	svc := NewService(
		&fakeRutasRepo{rows: rows},
		&fakeCobranzaRepo{rows: map[int][]rutasdomain.VentaCobranza{}},
		&fakeCalendario{m: map[int]time.Time{}},
	)
	got, err := svc.ListarRutas(context.Background())

	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, 1, got[0].ZonaID)
	assert.Equal(t, "Norte", got[0].ZonaNombre)
	assert.Equal(t, intPtr(5), got[0].CobradorID)
	assert.Nil(t, got[0].PctCoberturaSemanal, "no calendar → nil metrics")
	assert.Nil(t, got[0].PctPonderadoSemanal)

	assert.Nil(t, got[1].CobradorID)
	assert.Nil(t, got[1].PctCoberturaSemanal)
}

func TestService_ListarRutas_WithMetrics(t *testing.T) {
	t.Parallel()

	cobradorID := 5
	zonaID := 1
	rows := []rutasdomain.RutaResumen{
		{ZonaID: zonaID, CobradorID: &cobradorID, ZonaNombre: "Norte",
			SaldoTotal: decimal.NewFromInt(1000)},
	}

	fechaInicio := time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC)

	ventas := []rutasdomain.VentaCobranza{
		{
			VentaID:      1,
			ClienteID:    100,
			ZonaID:       zonaID,
			Parcialidad:  decimal.NewFromInt(100),
			Frecuencia:   rutasdomain.Semanal,
			AbonoSemana:  decimal.NewFromInt(100),
			Vencidas:     decimal.NewFromInt(0),
			Aporte:       decimal.NewFromInt(1),
			Saldo:        decimal.NewFromInt(900),
			TotalImporte: decimal.NewFromInt(4000),
			FechaCargo:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			VentaID:      2,
			ClienteID:    101,
			ZonaID:       zonaID,
			Parcialidad:  decimal.NewFromInt(200),
			Frecuencia:   rutasdomain.Semanal,
			AbonoSemana:  decimal.Zero, // no pagó
			Vencidas:     decimal.NewFromInt(1),
			Aporte:       decimal.Zero,
			Saldo:        decimal.NewFromInt(2000),
			TotalImporte: decimal.NewFromInt(4000),
			FechaCargo:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	svc := NewService(
		&fakeRutasRepo{rows: rows},
		&fakeCobranzaRepo{rows: map[int][]rutasdomain.VentaCobranza{zonaID: ventas}},
		&fakeCalendario{m: map[int]time.Time{cobradorID: fechaInicio}},
	)
	got, err := svc.ListarRutas(context.Background())

	require.NoError(t, err)
	require.Len(t, got, 1)

	require.NotNil(t, got[0].PctCoberturaSemanal, "coverage should be computed")
	// 1 of 2 paid → 50%
	assert.True(t,
		decimal.NewFromFloat(50.0).Equal(*got[0].PctCoberturaSemanal),
		"cobertura %s", got[0].PctCoberturaSemanal,
	)

	require.NotNil(t, got[0].PctPonderadoSemanal)
	// aporte sum=1, den=2 → 50%
	assert.True(t,
		decimal.NewFromFloat(50.0).Equal(*got[0].PctPonderadoSemanal),
		"ponderado %s", got[0].PctPonderadoSemanal,
	)
}
```

- [ ] **Step 4: Run tests**

```bash
cd /Volumes/M2-1TB/Developer/msp-api && go test ./internal/rutas/... -v 2>&1
```
Expected: all tests pass. If `cmd/api` doesn't compile yet (provideRutasService signature), that's OK — we'll fix it in Task 7.

- [ ] **Step 5: Commit**

```bash
git add internal/rutas/domain/ruta.go internal/rutas/app/listar_rutas.go internal/rutas/app/listar_rutas_test.go
git commit -m "feat(rutas): Service acepta CobranzaRepo y CalendarioCobradorClient, métricas semanales"
```

---

## Task 5: Firebird `CobranzaRepo` implementation

**Files:**
- Create: `internal/rutas/infra/rutasfb/cobranza_repo.go`
- Create: `internal/rutas/infra/rutasfb/cobranza_repo_test.go`

**Interfaces:**
- Consumes: `outbound.CobranzaRepo`, `domain.VentaCobranza`, `domain.AporteInput`, `domain.CalcAporte`.
- Produces: `rutasfb.CobranzaRepo` (concrete type).

The query must:
1. Join `MSP_SALDOS_VENTAS` with `LIBRES_CARGOS_CC` and `LISTAS_ATRIBUTOS` (for `FORMA_DE_PAGO` → frecuencia string).
2. Aggregate `MSP_PAGOS_VENTAS` payments in the window per `DOCTO_CC_ACR_ID`.
3. Filter: `CARGO_CANCELADO <> 'S'` AND (`SALDO > 0` OR `abono_semana > 0`).

The Firebird query computes `abono_semana` as a correlated subquery (or inline aggregation) on `MSP_PAGOS_VENTAS` filtered by `ZONA_CLIENTE_ID`, `FECHA BETWEEN ? AND ?`, `CANCELADO = 'N'`.

Then Go code:
- Computes `plazos = (fechaInicio − fechaCargo).Days / cadenciaDias` as decimal.
- Calls `domain.CalcAporte` and `domain.CadenciaDias` per venta.
- Computes `vencidas` inline (matching the formula in `CalcAporte` so the DTO can show it).

- [ ] **Step 1: Create `cobranza_repo.go`**

```go
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutasfb

import (
	"context"
	"database/sql"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
	"github.com/abdimuy/msp-api/internal/rutas/ports/outbound"
)

// Compile-time assertion.
var _ outbound.CobranzaRepo = (*CobranzaRepo)(nil)

// CobranzaRepo is the Firebird-backed implementation of outbound.CobranzaRepo.
// Reads MSP_SALDOS_VENTAS, LIBRES_CARGOS_CC, LISTAS_ATRIBUTOS, and
// MSP_PAGOS_VENTAS — no tables are written.
type CobranzaRepo struct {
	pool *firebird.Pool
}

// NewCobranzaRepo builds a CobranzaRepo wired to the given pool.
func NewCobranzaRepo(pool *firebird.Pool) *CobranzaRepo {
	return &CobranzaRepo{pool: pool}
}

// queryVentasPorZona returns one row per active venta for the given zona,
// including the sum of valid payments in [desde, hasta] and the frecuencia
// string resolved from LISTAS_ATRIBUTOS.
//
// CAST(SUM(...) AS NUMERIC(18,2)) is mandatory — firebirdsql v0.9.x returns
// NUMERIC aggregates unscaled without the explicit cast.
//
// Parameters: $1=zonaID, $2=zonaID (subquery), $3=desde, $4=hasta, $5=zonaID (outer filter).
const queryVentasPorZona = `
SELECT
  s.DOCTO_CC_ID,
  s.CLIENTE_ID,
  s.ZONA_CLIENTE_ID,
  CAST(COALESCE(l.PARCIALIDAD, 0) AS NUMERIC(18,2))        AS PARCIALIDAD,
  COALESCE(UPPER(lfp.VALOR_DESPLEGADO), 'SEMANAL')         AS FRECUENCIA,
  CAST(COALESCE(p.ABONO_SEMANA, 0) AS NUMERIC(18,2))       AS ABONO_SEMANA,
  CAST(s.SALDO        AS NUMERIC(18,2))                    AS SALDO,
  CAST(s.TOTAL_IMPORTE AS NUMERIC(18,2))                   AS TOTAL_IMPORTE,
  s.FECHA_CARGO,
  s.FECHA_ULT_PAGO
FROM MSP_SALDOS_VENTAS s
LEFT JOIN LIBRES_CARGOS_CC l    ON l.DOCTO_CC_ID      = s.DOCTO_CC_ID
LEFT JOIN LISTAS_ATRIBUTOS lfp  ON lfp.LISTA_ATRIB_ID = l.FORMA_DE_PAGO
LEFT JOIN (
  SELECT DOCTO_CC_ACR_ID,
         CAST(SUM(IMPORTE) AS NUMERIC(18,2)) AS ABONO_SEMANA
  FROM MSP_PAGOS_VENTAS
  WHERE ZONA_CLIENTE_ID = ?
    AND CANCELADO = 'N'
    AND FECHA >= ?
    AND FECHA <= ?
  GROUP BY DOCTO_CC_ACR_ID
) p ON p.DOCTO_CC_ACR_ID = s.DOCTO_CC_ID
WHERE s.ZONA_CLIENTE_ID = ?
  AND s.CARGO_CANCELADO <> 'S'
  AND (s.SALDO > 0 OR COALESCE(p.ABONO_SEMANA, 0) > 0)
ORDER BY s.DOCTO_CC_ID`

// VentasPorZona implements outbound.CobranzaRepo.
func (r *CobranzaRepo) VentasPorZona(
	ctx context.Context, zonaID int, desde, hasta time.Time,
) ([]rutasdomain.VentaCobranza, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, queryVentasPorZona,
		zonaID, // subquery param
		firebird.ToWallClock(desde),
		firebird.ToWallClock(hasta),
		zonaID, // outer WHERE
	)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	var result []rutasdomain.VentaCobranza
	for rows.Next() {
		v, serr := scanVentaCobranza(rows)
		if serr != nil {
			return nil, firebird.MapError(serr)
		}
		result = append(result, v)
	}
	if rerr := rows.Err(); rerr != nil {
		return nil, firebird.MapError(rerr)
	}
	return result, nil
}

// ventaCobranzaRaw holds raw scan targets for one cobranza row.
type ventaCobranzaRaw struct {
	ventaID       int
	clienteID     int
	zonaID        int
	parcialidadRaw any
	frecuencia    string
	abonoRaw      any
	saldoRaw      any
	totalRaw      any
	fechaCargo    time.Time
	fechaUltPago  sql.NullTime
}

func scanVentaCobranza(s scannable) (rutasdomain.VentaCobranza, error) {
	var raw ventaCobranzaRaw
	if err := s.Scan(
		&raw.ventaID,
		&raw.clienteID,
		&raw.zonaID,
		&raw.parcialidadRaw,
		&raw.frecuencia,
		&raw.abonoRaw,
		&raw.saldoRaw,
		&raw.totalRaw,
		&raw.fechaCargo,
		&raw.fechaUltPago,
	); err != nil {
		return rutasdomain.VentaCobranza{}, err
	}

	parcialidad, err := firebird.ScanDecimal(raw.parcialidadRaw, 2)
	if err != nil {
		return rutasdomain.VentaCobranza{}, err
	}
	abono, err := firebird.ScanDecimal(raw.abonoRaw, 2)
	if err != nil {
		return rutasdomain.VentaCobranza{}, err
	}
	saldo, err := firebird.ScanDecimal(raw.saldoRaw, 2)
	if err != nil {
		return rutasdomain.VentaCobranza{}, err
	}
	total, err := firebird.ScanDecimal(raw.totalRaw, 2)
	if err != nil {
		return rutasdomain.VentaCobranza{}, err
	}

	var fechaUltPago *time.Time
	if raw.fechaUltPago.Valid {
		t := raw.fechaUltPago.Time
		fechaUltPago = &t
	}

	// Aporte and Vencidas are computed here so the domain type is
	// self-contained and the HTTP handler only needs to map, not compute.
	// Plazos is set to zero here — the app service sets it properly because
	// it owns the fechaInicio from Firestore. The repo exposes raw fields;
	// the service calls enrichVenta after fetching.
	// See NOTE in app/cobranza_semanal.go.
	return rutasdomain.VentaCobranza{
		VentaID:      raw.ventaID,
		ClienteID:    raw.clienteID,
		ZonaID:       raw.zonaID,
		Parcialidad:  parcialidad,
		Frecuencia:   rutasdomain.Frecuencia(raw.frecuencia),
		AbonoSemana:  abono,
		Saldo:        saldo,
		TotalImporte: total,
		FechaCargo:   firebird.ScanUTCTime(raw.fechaCargo),
		FechaUltPago: fechaUltPago,
		// Aporte and Vencidas are computed in the app layer after Plazos is known.
		Aporte:   decimal.Zero,
		Vencidas: decimal.Zero,
	}, nil
}
```

> **Architecture note:** The repo returns raw fields; the app service (`cobranza_semanal.go`) will call `enrichVentas(ventas, fechaInicio)` to fill in `Plazos`, `Vencidas`, and `Aporte` using `domain.CalcAporte`. This keeps the formula in the domain layer (Task 1) and the repo as a dumb scanner.

- [ ] **Step 2: Create integration test**

```go
//nolint:misspell // rutas vocabulary is Spanish per project convention.
//nolint:paralleltest // serial: shares rollback-only tx.
package rutasfb_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/rutas/infra/rutasfb"
)

func TestCobranzaRepo_VentasPorZona(t *testing.T) {
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird integration tests")
	}
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := rutasfb.NewCobranzaRepo(pool)
		// Use a known zona from MSP_CFG_ZONA_CAJA seed (12271).
		// Window: last 30 days to capture any real data.
		desde := time.Now().UTC().AddDate(0, 0, -30)
		hasta := time.Now().UTC()

		ventas, err := repo.VentasPorZona(ctx, 12271, desde, hasta)
		require.NoError(t, err)
		assert.NotNil(t, ventas)
		t.Logf("VentasPorZona(12271) returned %d ventas", len(ventas))
		for _, v := range ventas {
			assert.NotZero(t, v.VentaID)
			assert.NotZero(t, v.ClienteID)
			// Parcialidad may be 0 for unregistered credits — just check scan.
			assert.False(t, v.Saldo.IsNegative(), "saldo must not be negative")
		}
	})
}
```

- [ ] **Step 3: Build + unit test (skip integration)**

```bash
cd /Volumes/M2-1TB/Developer/msp-api && go build ./internal/rutas/... && go test ./internal/rutas/... -v -short 2>&1
```
Expected: build success; unit tests pass; integration test skipped (no FB_DATABASE).

- [ ] **Step 4: Commit**

```bash
git add internal/rutas/infra/rutasfb/cobranza_repo.go internal/rutas/infra/rutasfb/cobranza_repo_test.go
git commit -m "feat(rutas): CobranzaRepo Firebird — VentasPorZona"
```

---

## Task 6: App service `cobranza_semanal.go` — enrich ventas + `DesglosePorZona`

**Files:**
- Create: `internal/rutas/app/cobranza_semanal.go`
- Create: `internal/rutas/app/cobranza_semanal_test.go`

**Interfaces:**
- Consumes: `domain.CalcAporte`, `domain.CadenciaDias`, `outbound.CobranzaRepo`, `outbound.CalendarioCobradorClient`.
- Produces: `Service.DesglosePorZona(ctx, zonaID int) ([]domain.VentaCobranza, *time.Time, error)`.

- [ ] **Step 1: Create `cobranza_semanal.go`**

```go
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package app

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

// DesglosePorZona returns the per-sale breakdown for the given zona's
// reporting window. Returns the ventas slice and the fechaInicioSemana
// for the zona's cobrador (nil when no calendar entry exists).
//
// The caller (HTTP handler) uses fechaInicioSemana to populate the
// response; nil means the cobrador has no Firestore entry.
func (s *Service) DesglosePorZona(
	ctx context.Context, zonaID int,
) ([]rutasdomain.VentaCobranza, *time.Time, error) {
	// Resolve cobrador for this zona.
	rutas, err := s.repo.ListarRutas(ctx)
	if err != nil {
		return nil, nil, err
	}
	var cobradorID *int
	for _, r := range rutas {
		if r.ZonaID == zonaID {
			cobradorID = r.CobradorID
			break
		}
	}
	if cobradorID == nil {
		// Zona exists but has no cobrador — return empty breakdown.
		return []rutasdomain.VentaCobranza{}, nil, nil
	}

	calendario, err := s.calendario.FechaInicioPorCobrador(ctx)
	if err != nil {
		calendario = map[int]time.Time{}
	}

	fechaInicio, ok := calendario[*cobradorID]
	if !ok {
		return []rutasdomain.VentaCobranza{}, nil, nil
	}

	now := time.Now().UTC()
	ventas, err := s.cobranza.VentasPorZona(ctx, zonaID, fechaInicio, now)
	if err != nil {
		return nil, nil, err
	}

	// Enrich: compute Plazos, Vencidas, and Aporte now that we have fechaInicio.
	enrichVentas(ventas, fechaInicio)

	fi := fechaInicio
	return ventas, &fi, nil
}

// enrichVentas populates Aporte and Vencidas on each venta using fechaInicio.
// This is called after the repo returns raw rows (repo does not have fechaInicio).
// NOTE: This function mutates the slice in-place.
func enrichVentas(ventas []rutasdomain.VentaCobranza, fechaInicio time.Time) {
	for i := range ventas {
		v := &ventas[i]
		cadencia := rutasdomain.CadenciaDias(v.Frecuencia)
		windowDays := fechaInicio.UTC().Sub(v.FechaCargo.UTC()).Hours() / 24.0
		plazos := decimal.NewFromFloat(windowDays / float64(cadencia))

		in := rutasdomain.AporteInput{
			Parcialidad:  v.Parcialidad,
			Plazos:       plazos,
			TotalImporte: v.TotalImporte,
			AbonoSemana:  v.AbonoSemana,
			SaldoHoy:     v.Saldo,
		}
		v.Aporte = rutasdomain.CalcAporte(in)

		// Compute vencidas for the DTO (informational).
		if v.Parcialidad.IsPositive() {
			saldoAlInicio := v.Saldo.Add(v.AbonoSemana)
			pagadoAntes := v.TotalImporte.Sub(saldoAlInicio)
			expectedDebt := v.Parcialidad.Mul(plazos)
			debia := decimal.Min(expectedDebt, v.TotalImporte)
			diff := debia.Sub(pagadoAntes)
			v.Vencidas = decimal.Max(decimal.Zero, diff.Div(v.Parcialidad))
		}
	}
}
```

- [ ] **Step 2: Create `cobranza_semanal_test.go`**

```go
//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

func TestDesglosePorZona_NoCobrador(t *testing.T) {
	t.Parallel()

	rows := []rutasdomain.RutaResumen{
		{ZonaID: 99, CobradorID: nil, ZonaNombre: "Sin cobrador"},
	}
	svc := NewService(
		&fakeRutasRepo{rows: rows},
		&fakeCobranzaRepo{rows: map[int][]rutasdomain.VentaCobranza{}},
		&fakeCalendario{m: map[int]time.Time{}},
	)
	ventas, fi, err := svc.DesglosePorZona(context.Background(), 99)
	require.NoError(t, err)
	assert.Empty(t, ventas)
	assert.Nil(t, fi)
}

func TestDesglosePorZona_NoCalendario(t *testing.T) {
	t.Parallel()

	cobradorID := 7
	rows := []rutasdomain.RutaResumen{
		{ZonaID: 1, CobradorID: &cobradorID},
	}
	svc := NewService(
		&fakeRutasRepo{rows: rows},
		&fakeCobranzaRepo{rows: map[int][]rutasdomain.VentaCobranza{}},
		&fakeCalendario{m: map[int]time.Time{}}, // cobrador 7 not in calendar
	)
	ventas, fi, err := svc.DesglosePorZona(context.Background(), 1)
	require.NoError(t, err)
	assert.Empty(t, ventas)
	assert.Nil(t, fi)
}

func TestDesglosePorZona_EnrichAporte(t *testing.T) {
	t.Parallel()

	cobradorID := 5
	zonaID := 1
	rows := []rutasdomain.RutaResumen{
		{ZonaID: zonaID, CobradorID: &cobradorID},
	}
	// fechaInicio: 10 days ago; cadencia SEMANAL=7 → plazos≈1.43
	fechaInicio := time.Now().UTC().AddDate(0, 0, -10)
	// Venta: parcialidad=100, saldo=2900, total=4000, abono=100
	// pagado_antes = 4000 - (2900+100) = 1000
	// plazos ≈ 1.43, debia = MIN(100×1.43, 4000) = 143
	// vencidas = MAX(0, (143-1000)/100) = 0 (pagó más de lo debido)
	// aporte = MIN(100/100, 0+1) = 1.00
	ventas := []rutasdomain.VentaCobranza{
		{
			VentaID:      1,
			ClienteID:    100,
			ZonaID:       zonaID,
			Parcialidad:  decimal.NewFromInt(100),
			Frecuencia:   rutasdomain.Semanal,
			AbonoSemana:  decimal.NewFromInt(100),
			Saldo:        decimal.NewFromInt(2900),
			TotalImporte: decimal.NewFromInt(4000),
			FechaCargo:   fechaInicio.AddDate(0, 0, -30), // 40 days before now
		},
	}

	svc := NewService(
		&fakeRutasRepo{rows: rows},
		&fakeCobranzaRepo{rows: map[int][]rutasdomain.VentaCobranza{zonaID: ventas}},
		&fakeCalendario{m: map[int]time.Time{cobradorID: fechaInicio}},
	)
	got, fi, err := svc.DesglosePorZona(context.Background(), zonaID)
	require.NoError(t, err)
	require.NotNil(t, fi)
	require.Len(t, got, 1)
	// Aporte must be 1.00 (al corriente pays 1 cuota).
	assert.True(t, decimal.NewFromInt(1).Equal(got[0].Aporte),
		"aporte=%s", got[0].Aporte)
}
```

- [ ] **Step 3: Run tests**

```bash
cd /Volumes/M2-1TB/Developer/msp-api && go test ./internal/rutas/... -v -short 2>&1
```
Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/rutas/app/cobranza_semanal.go internal/rutas/app/cobranza_semanal_test.go
git commit -m "feat(rutas): DesglosePorZona + enrichVentas en la capa de app"
```

---

## Task 7: HTTP DTOs + handlers for cobranza endpoints

**Files:**
- Create: `internal/rutas/infra/rutashttp/cobranza_dto.go`
- Create: `internal/rutas/infra/rutashttp/cobranza_handlers.go`
- Modify: `internal/rutas/infra/rutashttp/dto.go` — add three new fields to `RutaResumenDTO`.
- Modify: `internal/rutas/infra/rutashttp/handlers.go` — update `toRutaResumenDTOs`.
- Modify: `internal/rutas/infra/rutashttp/routes.go` — register new operation.

**Interfaces:**
- Consumes: `Service.DesglosePorZona(ctx, zonaID)`.
- Produces: `GET /v2/rutas/{zona_id}/cobranza` Huma operation.

- [ ] **Step 1: Extend `RutaResumenDTO` in `dto.go`**

Add after `SaldoTotal` in the `RutaResumenDTO` struct:

```go
	PctCoberturaSemanal *string `json:"pct_cobertura_semanal" doc:"Porcentaje de cobertura semanal (ventas que pagaron / total). Nulo si el cobrador no tiene fecha de inicio de semana en Firestore."`
	PctPonderadoSemanal *string `json:"pct_ponderado_semanal" doc:"Porcentaje ponderado semanal (aporte total / denominador). Puede superar 100%. Nulo si el cobrador no tiene fecha de inicio."`
	FechaInicioSemana   *string `json:"fecha_inicio_semana"   doc:"Fecha de inicio de semana del cobrador (RFC3339 UTC). Nulo si no configurado."`
```

- [ ] **Step 2: Update `toRutaResumenDTOs` in `handlers.go`**

In the loop body, after setting `SaldoTotal`, add:

```go
		if r.PctCoberturaSemanal != nil {
			s := r.PctCoberturaSemanal.StringFixed(2)
			dtos[i].PctCoberturaSemanal = &s
		}
		if r.PctPonderadoSemanal != nil {
			s := r.PctPonderadoSemanal.StringFixed(2)
			dtos[i].PctPonderadoSemanal = &s
		}
		if r.FechaInicioSemana != nil {
			s := r.FechaInicioSemana.UTC().Format(time.RFC3339)
			dtos[i].FechaInicioSemana = &s
		}
```

Add `"time"` to the import block of `handlers.go`.

- [ ] **Step 3: Create `cobranza_dto.go`**

```go
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutashttp

// DesglosePorZonaInput holds the path parameter for GET /rutas/{zona_id}/cobranza.
type DesglosePorZonaInput struct {
	ZonaID int `path:"zona_id" doc:"ID de la zona de ventas"`
}

// DesglosePorZonaOutput wraps the response body for the cobranza breakdown.
type DesglosePorZonaOutput struct {
	Body struct {
		ZonaID            int                  `json:"zona_id"`
		FechaInicioSemana *string              `json:"fecha_inicio_semana"`
		Items             []VentaCobranzaDTO   `json:"items"`
	}
}

// VentaCobranzaDTO is the wire representation of one venta's cobranza metrics.
// All money fields are strings to avoid floating-point rounding.
type VentaCobranzaDTO struct {
	VentaID     int    `json:"venta_id"     doc:"DOCTO_CC_ID de la venta"`
	ClienteID   int    `json:"cliente_id"   doc:"CLIENTE_ID"`
	Parcialidad string `json:"parcialidad"  doc:"Cuota esperada en pesos (2 decimales)"`
	Frecuencia  string `json:"frecuencia"   doc:"Cadencia de pago: SEMANAL, QUINCENAL o MENSUAL"`
	AbonoSemana string `json:"abono_semana" doc:"Total abonado en la ventana semanal (2 decimales)"`
	Vencidas    string `json:"vencidas"     doc:"Cuotas vencidas al inicio de la ventana (puede ser fracción)"`
	Aporte      string `json:"aporte"       doc:"Aporte calculado para el reporte ponderado (puede ser fracción)"`
	Saldo       string `json:"saldo"        doc:"Saldo pendiente actual (2 decimales)"`
}
```

- [ ] **Step 4: Create `cobranza_handlers.go`**

```go
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutashttp

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/abdimuy/msp-api/internal/auth"
	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

// DesglosePorZona handles GET /rutas/{zona_id}/cobranza.
// Requires auth.PermRutasLeer.
func (h *Handlers) DesglosePorZona(ctx context.Context, in *DesglosePorZonaInput) (*DesglosePorZonaOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermRutasLeer); err != nil {
		return nil, err
	}

	ventas, fechaInicio, err := h.svc.DesglosePorZona(ctx, in.ZonaID)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &DesglosePorZonaOutput{}
	out.Body.ZonaID = in.ZonaID
	if fechaInicio != nil {
		s := fechaInicio.UTC().Format(time.RFC3339)
		out.Body.FechaInicioSemana = &s
	}
	out.Body.Items = toVentaCobranzaDTOs(ventas)
	return out, nil
}

func toVentaCobranzaDTOs(ventas []rutasdomain.VentaCobranza) []VentaCobranzaDTO {
	if ventas == nil {
		return []VentaCobranzaDTO{}
	}
	dtos := make([]VentaCobranzaDTO, len(ventas))
	for i, v := range ventas {
		dtos[i] = VentaCobranzaDTO{
			VentaID:     v.VentaID,
			ClienteID:   v.ClienteID,
			Parcialidad: v.Parcialidad.StringFixed(2),
			Frecuencia:  string(v.Frecuencia),
			AbonoSemana: v.AbonoSemana.StringFixed(2),
			Vencidas:    v.Vencidas.StringFixed(4),
			Aporte:      v.Aporte.StringFixed(4),
			Saldo:       v.Saldo.StringFixed(2),
		}
	}
	return dtos
}

// Compile-time check that huma.Handler[In,Out] is satisfied (implicit via Register).
var _ = (*DesglosePorZonaInput)(nil)
var _ = (*DesglosePorZonaOutput)(nil)

// suppressUnusedImport keeps the huma import used when there are no direct huma calls.
var _ = huma.NewError
```

Wait — the `var _ = huma.NewError` trick will cause a lint issue. Instead ensure the import is used via the `huma.Operation` in routes.go. Remove that line. Instead only import what's used:

Actually `huma` is not directly used in `cobranza_handlers.go` since `mapAppError`, `currentUserOrError`, `requirePerm` are in `auth.go`. Import only `context`, `net/http` (not used either), `time`, `auth`, `rutasdomain`. Let's simplify:

```go
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutashttp

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/auth"
	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

// DesglosePorZona handles GET /rutas/{zona_id}/cobranza.
// Requires auth.PermRutasLeer.
func (h *Handlers) DesglosePorZona(ctx context.Context, in *DesglosePorZonaInput) (*DesglosePorZonaOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermRutasLeer); err != nil {
		return nil, err
	}

	ventas, fechaInicio, err := h.svc.DesglosePorZona(ctx, in.ZonaID)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &DesglosePorZonaOutput{}
	out.Body.ZonaID = in.ZonaID
	if fechaInicio != nil {
		s := fechaInicio.UTC().Format(time.RFC3339)
		out.Body.FechaInicioSemana = &s
	}
	out.Body.Items = toVentaCobranzaDTOs(ventas)
	return out, nil
}

func toVentaCobranzaDTOs(ventas []rutasdomain.VentaCobranza) []VentaCobranzaDTO {
	if ventas == nil {
		return []VentaCobranzaDTO{}
	}
	dtos := make([]VentaCobranzaDTO, len(ventas))
	for i, v := range ventas {
		dtos[i] = VentaCobranzaDTO{
			VentaID:     v.VentaID,
			ClienteID:   v.ClienteID,
			Parcialidad: v.Parcialidad.StringFixed(2),
			Frecuencia:  string(v.Frecuencia),
			AbonoSemana: v.AbonoSemana.StringFixed(2),
			Vencidas:    v.Vencidas.StringFixed(4),
			Aporte:      v.Aporte.StringFixed(4),
			Saldo:       v.Saldo.StringFixed(2),
		}
	}
	return dtos
}
```

- [ ] **Step 5: Register the new operation in `routes.go`**

In `registerOperations`, after the existing `listar-rutas` registration, add:

```go
	huma.Register(api, huma.Operation{
		OperationID:   "desglose-cobranza-por-zona",
		Method:        http.MethodGet,
		Path:          "/rutas/{zona_id}/cobranza",
		Summary:       "Desglose de cobranza semanal por zona",
		Description:   "Devuelve el detalle por venta del reporte semanal de cobranza para una zona: abono, cuotas vencidas, aporte y saldo.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.DesglosePorZona)
```

- [ ] **Step 6: Build check**

```bash
cd /Volumes/M2-1TB/Developer/msp-api && go build ./internal/rutas/... 2>&1
```
Expected: build success.

- [ ] **Step 7: Run rutas tests**

```bash
cd /Volumes/M2-1TB/Developer/msp-api && go test ./internal/rutas/... -v -short 2>&1
```
Expected: all tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/rutas/infra/rutashttp/cobranza_dto.go \
        internal/rutas/infra/rutashttp/cobranza_handlers.go \
        internal/rutas/infra/rutashttp/dto.go \
        internal/rutas/infra/rutashttp/handlers.go \
        internal/rutas/infra/rutashttp/routes.go
git commit -m "feat(rutas): HTTP DTOs + handlers para métricas y desglose de cobranza"
```

---

## Task 8: Firestore adapter + wiring in `cmd/api/`

**Files:**
- Create: `internal/rutas/infra/rutasfirestore/calendario_client.go`
- Modify: `cmd/api/rutas_wiring.go`
- Modify: `cmd/api/server.go`
- Modify: `cmd/api/main.go` (add `provideCobranzaRutasRepo` + `provideCalendarioCobradorClient` to the provides list)

**Interfaces:**
- Produces: `CalendarioCobradorClient` satisfied by `rutasfirestore.CalendarioClient` (real) or `noopCalendarioClient` (dev).
- Consumes: `*firestore.Client` — built from the same service account as auth's resolver.

**Separation rule (§2):** The adapter lives in `internal/rutas/infra/rutasfirestore/` and imports only `cloud.google.com/go/firestore`, `internal/rutas/ports/outbound`, and stdlib. It does NOT import `internal/auth/infra/firebase`. The Firebase app is initialized fresh from config in `cmd/api/rutas_wiring.go`, mirroring how `provideAuthNombreResolver` does it.

- [ ] **Step 1: Create the Firestore adapter**

Create `internal/rutas/infra/rutasfirestore/calendario_client.go`:

```go
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutasfirestore

import (
	"context"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/abdimuy/msp-api/internal/rutas/ports/outbound"
)

// Compile-time check.
var _ outbound.CalendarioCobradorClient = (*CalendarioClient)(nil)

// usersCollection is the Firestore collection holding cobrador profiles.
const usersCollection = "users"

// CalendarioClient reads FECHA_CARGA_INICIAL + COBRADOR_ID from the Firestore
// `users` collection and returns a COBRADOR_ID → time.Time map.
type CalendarioClient struct {
	fs *firestore.Client
}

// NewCalendarioClient builds a CalendarioClient backed by the given Firestore client.
func NewCalendarioClient(fs *firestore.Client) *CalendarioClient {
	return &CalendarioClient{fs: fs}
}

// FechaInicioPorCobrador iterates all documents in the `users` collection and
// returns a map from COBRADOR_ID (int) to FECHA_CARGA_INICIAL (time.Time UTC).
// Documents that lack either field are silently skipped.
// A Firestore read error is surfaced — the caller (app service) treats it as
// "no calendar" and returns nil metrics without failing the request.
func (c *CalendarioClient) FechaInicioPorCobrador(ctx context.Context) (map[int]time.Time, error) {
	docs, err := c.fs.Collection(usersCollection).Documents(ctx).GetAll()
	if err != nil {
		return nil, err
	}

	result := make(map[int]time.Time, len(docs))
	for _, doc := range docs {
		data := doc.Data()

		cobradorRaw, ok := data["COBRADOR_ID"]
		if !ok {
			continue
		}
		cobradorID, ok := toInt(cobradorRaw)
		if !ok || cobradorID <= 0 {
			continue
		}

		fechaRaw, ok := data["FECHA_CARGA_INICIAL"]
		if !ok {
			continue
		}
		t, ok := toTime(fechaRaw)
		if !ok {
			continue
		}
		result[cobradorID] = t.UTC()
	}
	return result, nil
}

// toInt converts a Firestore numeric value (float64 or int64) to int.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

// toTime converts a Firestore Timestamp value to time.Time.
func toTime(v any) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, true
	}
	return time.Time{}, false
}
```

- [ ] **Step 2: Create `NoopCalendarioClient` in same package for dev**

Append to the same file (or a separate `noop.go`):

Add to `calendario_client.go` after the main struct:

```go
// NoopCalendarioClient returns an empty map. Used in dev mode / unconfigured.
// Compile-time check.
var _ outbound.CalendarioCobradorClient = NoopCalendarioClient{}

// NoopCalendarioClient is the fallback when Firestore is unavailable.
type NoopCalendarioClient struct{}

// FechaInicioPorCobrador always returns an empty map without error.
func (NoopCalendarioClient) FechaInicioPorCobrador(_ context.Context) (map[int]time.Time, error) {
	return map[int]time.Time{}, nil
}
```

- [ ] **Step 3: Update `rutas_wiring.go`**

Replace the entire file:

```go
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package main

import (
	"context"
	"log/slog"

	firebasesdk "firebase.google.com/go/v4"
	"google.golang.org/api/option"

	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	rutasapp "github.com/abdimuy/msp-api/internal/rutas/app"
	rutasfb "github.com/abdimuy/msp-api/internal/rutas/infra/rutasfb"
	rutasfirestore "github.com/abdimuy/msp-api/internal/rutas/infra/rutasfirestore"
	"github.com/abdimuy/msp-api/internal/rutas/ports/outbound"
)

// provideRutasRepo builds the Firebird-backed RutasRepo for the rutas module.
func provideRutasRepo(pool *firebird.Pool) *rutasfb.RutasRepo {
	return rutasfb.NewRutasRepo(pool)
}

// provideCobranzaRutasRepo builds the Firebird-backed CobranzaRepo for the rutas module.
func provideCobranzaRutasRepo(pool *firebird.Pool) *rutasfb.CobranzaRepo {
	return rutasfb.NewCobranzaRepo(pool)
}

// provideCalendarioCobradorClient builds the Firestore-backed CalendarioCobradorClient.
// Returns a noop implementation when Firestore is unavailable (dev mode / unconfigured).
// Failures are logged but never fatal — missing calendar → nil metrics, not a crash.
func provideCalendarioCobradorClient(cfg *config.Config) outbound.CalendarioCobradorClient {
	if cfg.Firebase.DevMode || cfg.Firebase.ProjectID == "" {
		slog.Info("rutas.calendario: firestore no configurado; usando noop")
		return rutasfirestore.NoopCalendarioClient{}
	}
	ctx := context.Background()
	app, err := firebasesdk.NewApp(ctx,
		&firebasesdk.Config{ProjectID: cfg.Firebase.ProjectID},
		option.WithCredentialsFile(cfg.Firebase.ServiceAccountPath),
	)
	if err != nil {
		slog.Error("rutas.calendario: no se pudo inicializar firebase; usando noop", "error", err)
		return rutasfirestore.NoopCalendarioClient{}
	}
	fs, err := app.Firestore(ctx)
	if err != nil {
		slog.Error("rutas.calendario: no se pudo obtener cliente firestore; usando noop", "error", err)
		return rutasfirestore.NoopCalendarioClient{}
	}
	return rutasfirestore.NewCalendarioClient(fs)
}

// provideRutasService assembles the rutas read-only query service.
func provideRutasService(
	repo *rutasfb.RutasRepo,
	cobranza *rutasfb.CobranzaRepo,
	calendario outbound.CalendarioCobradorClient,
) *rutasapp.Service {
	return rutasapp.NewService(repo, cobranza, calendario)
}
```

- [ ] **Step 4: Update `cmd/api/main.go` — add new providers to fx.Provide list**

In `appOptions()`, in the `// Rutas module.` section, add the two new providers:

```go
			// Rutas module.
			provideRutasRepo,
			provideCobranzaRutasRepo,
			provideCalendarioCobradorClient,
			provideRutasService,
```

(Replace the two-line block that was `provideRutasRepo, provideRutasService`.)

- [ ] **Step 5: Update `cmd/api/server.go` — no signature change needed**

`provideRootHandler` receives `rutasSvc *rutasapp.Service` which is unchanged. The new providers are added to the DI graph but do not need to be injected into the root handler directly. Verify the function signature does NOT need updating.

If `go build ./cmd/api/...` succeeds without adding new params to `provideRootHandler`, we're done. Otherwise, check if fx can auto-wire the new providers via their return types (it can, since `*rutasfb.CobranzaRepo` and `outbound.CalendarioCobradorClient` are both injected into `provideRutasService`).

- [ ] **Step 6: Full build**

```bash
cd /Volumes/M2-1TB/Developer/msp-api && go build ./... 2>&1
```
Expected: success.

- [ ] **Step 7: Run all rutas tests**

```bash
cd /Volumes/M2-1TB/Developer/msp-api && go test ./internal/rutas/... -v -short 2>&1
```
Expected: all unit tests pass; integration tests skipped.

- [ ] **Step 8: Run golangci-lint**

```bash
cd /Volumes/M2-1TB/Developer/msp-api && golangci-lint run ./... 2>&1
```
Expected: 0 issues. Fix any issues before committing.

- [ ] **Step 9: Commit**

```bash
git add internal/rutas/infra/rutasfirestore/calendario_client.go \
        cmd/api/rutas_wiring.go \
        cmd/api/main.go
git commit -m "feat(rutas): adapter Firestore CalendarioCobradorClient + cableado fx"
```

---

## Task 9: Verification — build, test, lint, report

**Files:**
- Create: `.git/sdd/reporte-cobranza-be.md` (output report)

- [ ] **Step 1: Full build**

```bash
cd /Volumes/M2-1TB/Developer/msp-api && go build ./... 2>&1
```
Expected: 0 errors.

- [ ] **Step 2: Run all rutas tests with verbose output**

```bash
cd /Volumes/M2-1TB/Developer/msp-api && go test ./internal/rutas/... -v -short -count=1 2>&1
```
Expected: all tests PASS; Firebird integration tests SKIP.

Confirm the 5 mandatory cases pass explicitly:
```bash
cd /Volumes/M2-1TB/Developer/msp-api && go test ./internal/rutas/domain/... -v -run TestCalcAporte -count=1 2>&1
```

- [ ] **Step 3: Full lint**

```bash
cd /Volumes/M2-1TB/Developer/msp-api && golangci-lint run ./... 2>&1
```
Expected: 0 issues. Fix anything reported.

- [ ] **Step 4: Write the report**

Create `/Volumes/M2-1TB/Developer/msp-api/.git/sdd/reporte-cobranza-be.md` with the implementation summary per the spec instructions (Status, commits, test summary, concerns).

---

## Self-Review Checklist

**Spec coverage:**
- [x] `GET /v2/rutas` extended with `pct_cobertura_semanal`, `pct_ponderado_semanal`, `fecha_inicio_semana` → Task 4 (domain), Task 7 (DTO).
- [x] `GET /v2/rutas/{zona_id}/cobranza` desglose endpoint → Task 7 (handler + routes).
- [x] `CalendarioCobradorClient` outbound port (Firestore, FECHA_CARGA_INICIAL, COBRADOR_ID) → Task 3, Task 8.
- [x] `CobranzaRepo` outbound port (MSP_SALDOS_VENTAS, LIBRES_CARGOS_CC, MSP_PAGOS_VENTAS) → Task 3, Task 5.
- [x] 5 mandatory unit test cases for `CalcAporte` → Task 1.
- [x] Division decimal (not integer): `AporteInput` uses `decimal.Decimal`; `Div` on decimal is always decimal → Task 1.
- [x] `MIN(PARCIALIDAD × plazos, TOTAL_IMPORTE)` cap → `decimal.Min(expectedDebt, in.TotalImporte)` in Task 1.
- [x] Cobertura denominator: `SALDO > 0 OR paid in window` → SQL WHERE in Task 5.
- [x] Ponderado denominator: SEMANAL always, QUINCENAL/MENSUAL only if next due in window → `ventaAplicaEnVentana` in Task 4.
- [x] Null metrics when no Firestore calendar entry → Task 4 service logic.
- [x] CARGO_CANCELADO <> 'S' filter → SQL WHERE in Task 5.
- [x] `NoopCalendarioClient` for dev mode → Task 8.
- [x] No migrations → confirmed (read-only queries).
- [x] §2 vertical slices: Firestore adapter in `rutas/infra/rutasfirestore/`, no import of `auth/infra/firebase` → Task 8.
- [x] Money in DTOs as string (`.StringFixed(2)`) → Tasks 7.
- [x] `golangci-lint` gate → Task 9.
- [x] Report file → Task 9.

**Type consistency check:**
- `CalcAporte(AporteInput) decimal.Decimal` defined Task 1, consumed Task 6 ✓
- `CobranzaRepo.VentasPorZona(ctx, zonaID, desde, hasta) []VentaCobranza` defined Task 3, implemented Task 5, called Task 6 ✓
- `CalendarioCobradorClient.FechaInicioPorCobrador(ctx) map[int]time.Time` defined Task 3, implemented Task 8, called Task 4 ✓
- `Service.DesglosePorZona(ctx, zonaID) ([]VentaCobranza, *time.Time, error)` defined Task 6, called Task 7 ✓
- `NewService(repo, cobranza, calendario)` — 3-arg signature defined Task 4, wired Task 8 ✓

**Placeholder scan:** None found. All code blocks are complete.
