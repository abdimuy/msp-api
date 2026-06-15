# Analytics EstadoPago Signal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `FECHA_ULTIMO_PAGO` anchor column to `MSP_AN_WINBACK_CANDIDATOS` and compute an `EstadoPago` solvency signal at read time so the winback list distinguishes good payers from morosos.

**Architecture:** New VO `domain.EstadoPago` (pure string type, same pattern as `Segmento`). Pure function `estadoPagoFor(saldo, fechaUltimoPago, now)` lives in `app/scoring.go` (the R1 heuristics file). `FECHA_ULTIMO_PAGO` flows from `saldo_cte` → `AnclaCliente.FechaUltimoPago` → `WinbackCandidato.fechaUltimoPago` → `WinbackListItem.EstadoPago` → DTO. Everything is additive — no existing invariant changes.

**Tech Stack:** Go 1.23, Firebird 5, `nakagami/firebirdsql v0.9.19`, `shopspring/decimal`, `testify`, `fbtestutil.WithTestTransaction`.

---

## File Map

| Action | File |
|--------|------|
| CREATE | `migrations-firebird/000036_add_fecha_ultimo_pago.up.sql` |
| CREATE | `migrations-firebird/000036_add_fecha_ultimo_pago.down.sql` |
| MODIFY | `internal/analytics/domain/estado_pago.go` (new file) |
| MODIFY | `internal/analytics/domain/errors.go` (add sentinel) |
| MODIFY | `internal/analytics/domain/winback_candidato.go` (add field + getters) |
| CREATE | `internal/analytics/domain/estado_pago_test.go` |
| MODIFY | `internal/analytics/domain/winback_candidato_test.go` (add FechaUltimoPago assertions) |
| MODIFY | `internal/analytics/ports/outbound/microsip.go` (add FechaUltimoPago to AnclaCliente) |
| MODIFY | `internal/analytics/app/scoring.go` (add thresholds + `estadoPagoFor`) |
| MODIFY | `internal/analytics/app/scoring_test.go` (add `estadoPagoFor` table tests) |
| MODIFY | `internal/analytics/app/winback_query.go` (add `EstadoPago` to `WinbackListItem`, compute in loop) |
| MODIFY | `internal/analytics/app/winback_query_test.go` (assert EstadoPago populated) |
| MODIFY | `internal/analytics/app/refrescar_command.go` (`buildCandidatos` → pass `FechaUltimoPago`) |
| MODIFY | `internal/analytics/infra/analyticsfb/queries.go` (`candidatoCols` + `leerAnclasRFMClose`) |
| MODIFY | `internal/analytics/infra/analyticsfb/rowmappers.go` (`candidatoRowRaw` + `anclaRowRaw`) |
| MODIFY | `internal/analytics/infra/analyticsfb/repo.go` (`buildUpsertBlock`) |
| MODIFY | `internal/analytics/infra/analyticsfb/repo_test.go` (update `makeCandidato`; add round-trip assertion) |
| MODIFY | `internal/analytics/analytics_contracts.go` (add `FechaUltimoPago`, `EstadoPago` to contract) |
| MODIFY | `internal/analytics/analytics_contracts_mapper.go` (map `FechaUltimoPago`; note EstadoPago intentionally empty) |
| MODIFY | `internal/analytics/infra/analyticshttp/dtos.go` (add `EstadoPago`, `FechaUltimoPago` to `WinbackItemDTO`) |
| MODIFY | `internal/analytics/infra/analyticshttp/dto_mapper.go` (map new fields) |
| MODIFY | `internal/analytics/infra/analyticshttp/handlers_test.go` (assert new fields in 200 response) |

---

## Task 1: Migration files (structural only)

**Files:**
- Create: `migrations-firebird/000036_add_fecha_ultimo_pago.up.sql`
- Create: `migrations-firebird/000036_add_fecha_ultimo_pago.down.sql`

- [ ] **Step 1.1: Write the up migration**

```sql
-- migrations-firebird/000036_add_fecha_ultimo_pago.up.sql
-- ============================================================================
-- Migración 000036: agrega FECHA_ULTIMO_PAGO a MSP_AN_WINBACK_CANDIDATOS
-- ============================================================================
--
-- Por qué:
--   La señal de solvencia EstadoPago necesita la fecha del último pago del
--   cliente para distinguir deudores morosos de buenos pagadores dormidos.
--   El valor viene de MAX(MSP_SALDOS_VENTAS.FECHA_ULT_PAGO) por cliente
--   (cargos CARGO_CANCELADO='N') y es mutable (cambia en cada refresh).
--
-- Restricciones:
--   NULLABLE — un cliente nuevo puede no tener historial de pagos.
--   Sin DEFAULT ni trigger (CLAUDE.md §1). El valor lo pasa Go explícitamente.
-- ============================================================================

ALTER TABLE MSP_AN_WINBACK_CANDIDATOS
  ADD FECHA_ULTIMO_PAGO TIMESTAMP;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (36, '000036_add_fecha_ultimo_pago', CURRENT_TIMESTAMP);
COMMIT;
```

- [ ] **Step 1.2: Write the down migration**

```sql
-- migrations-firebird/000036_add_fecha_ultimo_pago.down.sql
ALTER TABLE MSP_AN_WINBACK_CANDIDATOS
  DROP FECHA_ULTIMO_PAGO;

DELETE FROM MSP_MIGRATIONS WHERE ID = 36;
COMMIT;
```

- [ ] **Step 1.3: Apply migration locally**

```bash
cd /Volumes/M2-1TB/Developer/msp-api
make fb-migrate-up
```

Expected: `Applying 000036_add_fecha_ultimo_pago...` line in output. No error.

- [ ] **Step 1.4: Verify column exists**

```bash
FB_DATABASE=$(grep FB_DATABASE .env | cut -d= -f2) \
  isql-fb -user sysdba -password masterkey "$FB_DATABASE" \
  -e "SELECT FIELD_NAME FROM RDB\$RELATION_FIELDS WHERE RDB\$RELATION_NAME='MSP_AN_WINBACK_CANDIDATOS' AND FIELD_NAME STARTING WITH 'FECHA'"
```

Expected: both `FECHA_ULTIMA_COMPRA` and `FECHA_ULTIMO_PAGO` appear.

- [ ] **Step 1.5: Commit**

```bash
git add migrations-firebird/000036_add_fecha_ultimo_pago.up.sql \
        migrations-firebird/000036_add_fecha_ultimo_pago.down.sql
git commit -m "chore(analytics): add migration 000036 for FECHA_ULTIMO_PAGO column"
```

---

## Task 2: Domain VO `EstadoPago`

**Files:**
- Create: `internal/analytics/domain/estado_pago.go`
- Modify: `internal/analytics/domain/errors.go`
- Create: `internal/analytics/domain/estado_pago_test.go`

- [ ] **Step 2.1: Write failing VO tests**

Create `internal/analytics/domain/estado_pago_test.go`:

```go
package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

func TestParseEstadoPago(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    domain.EstadoPago
		wantErr error
	}{
		{"SIN_CREDITO is valid", "SIN_CREDITO", domain.EstadoPagoSinCredito, nil},
		{"LIQUIDADO is valid", "LIQUIDADO", domain.EstadoPagoLiquidado, nil},
		{"AL_CORRIENTE is valid", "AL_CORRIENTE", domain.EstadoPagoAlCorriente, nil},
		{"ATRASADO is valid", "ATRASADO", domain.EstadoPagoAtrasado, nil},
		{"MOROSO is valid", "MOROSO", domain.EstadoPagoMoroso, nil},
		{"empty string is invalid", "", "", domain.ErrEstadoPagoInvalido},
		{"lowercase is invalid", "moroso", "", domain.ErrEstadoPagoInvalido},
		{"unknown value is invalid", "BUENO", "", domain.ErrEstadoPagoInvalido},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := domain.ParseEstadoPago(tc.input)
			if tc.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
				assert.Empty(t, string(got))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestEstadoPagoIsValid(t *testing.T) {
	t.Parallel()

	valid := []domain.EstadoPago{
		domain.EstadoPagoSinCredito,
		domain.EstadoPagoLiquidado,
		domain.EstadoPagoAlCorriente,
		domain.EstadoPagoAtrasado,
		domain.EstadoPagoMoroso,
	}
	for _, ep := range valid {
		ep := ep
		t.Run(string(ep)+"_is_valid", func(t *testing.T) {
			t.Parallel()
			assert.True(t, ep.IsValid())
		})
	}

	invalid := []domain.EstadoPago{"", "moroso", "UNKNOWN"}
	for _, ep := range invalid {
		ep := ep
		t.Run(string(ep)+"_is_invalid", func(t *testing.T) {
			t.Parallel()
			assert.False(t, ep.IsValid())
		})
	}
}

func TestEstadoPagoString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ep   domain.EstadoPago
		want string
	}{
		{domain.EstadoPagoSinCredito, "SIN_CREDITO"},
		{domain.EstadoPagoLiquidado, "LIQUIDADO"},
		{domain.EstadoPagoAlCorriente, "AL_CORRIENTE"},
		{domain.EstadoPagoAtrasado, "ATRASADO"},
		{domain.EstadoPagoMoroso, "MOROSO"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.ep.String())
		})
	}
}
```

- [ ] **Step 2.2: Run tests to confirm they fail**

```bash
cd /Volumes/M2-1TB/Developer/msp-api
go test ./internal/analytics/domain/... 2>&1 | grep -E "FAIL|undefined"
```

Expected: compilation error about `domain.EstadoPago` undefined.

- [ ] **Step 2.3: Add sentinel to `errors.go`**

In `internal/analytics/domain/errors.go`, after the `ErrScoreWinbackFueraDeRango` block, add:

```go
	// ErrEstadoPagoInvalido is returned when a string cannot be parsed as an EstadoPago.
	ErrEstadoPagoInvalido = apperror.NewValidation(
		"estado_pago_invalido",
		"el estado de pago no es válido",
	)
```

- [ ] **Step 2.4: Create `estado_pago.go`**

```go
// internal/analytics/domain/estado_pago.go
package domain

// EstadoPago classifies a client's payment behaviour based on their outstanding
// balance (saldo) and last-payment date. Values are UPPERCASE to match the
// Microsip-style convention used throughout the analytics module.
//
// Computed at read time in the app layer via estadoPagoFor; never stored directly.
type EstadoPago string

const (
	// EstadoPagoSinCredito denotes a contado-only client: saldo == 0 and no
	// payment history (fechaUltimoPago is zero).
	EstadoPagoSinCredito EstadoPago = "SIN_CREDITO"

	// EstadoPagoLiquidado denotes a client who had credit and is fully paid
	// (saldo == 0 but has at least one historical payment).
	EstadoPagoLiquidado EstadoPago = "LIQUIDADO"

	// EstadoPagoAlCorriente denotes a client with outstanding balance who paid
	// recently (within umbralAlCorrienteDias days).
	EstadoPagoAlCorriente EstadoPago = "AL_CORRIENTE"

	// EstadoPagoAtrasado denotes a client with outstanding balance whose last
	// payment was moderately overdue (between umbralAlCorrienteDias and
	// umbralAtrasadoDias days ago).
	EstadoPagoAtrasado EstadoPago = "ATRASADO"

	// EstadoPagoMoroso denotes a client with outstanding balance who has not
	// paid in a long time (more than umbralAtrasadoDias days, or never).
	EstadoPagoMoroso EstadoPago = "MOROSO"
)

// ParseEstadoPago parses s into an EstadoPago or returns ErrEstadoPagoInvalido.
// Input must match the exact UPPERCASE canonical form.
func ParseEstadoPago(s string) (EstadoPago, error) {
	ep := EstadoPago(s)
	if !ep.IsValid() {
		return "", ErrEstadoPagoInvalido
	}
	return ep, nil
}

// IsValid reports whether ep is a recognized EstadoPago value.
func (ep EstadoPago) IsValid() bool {
	switch ep {
	case EstadoPagoSinCredito,
		EstadoPagoLiquidado,
		EstadoPagoAlCorriente,
		EstadoPagoAtrasado,
		EstadoPagoMoroso:
		return true
	}
	return false
}

// String returns the canonical string representation.
func (ep EstadoPago) String() string { return string(ep) }
```

- [ ] **Step 2.5: Run tests — must pass**

```bash
go test ./internal/analytics/domain/... -v -run TestParseEstadoPago
go test ./internal/analytics/domain/... -v -run TestEstadoPagoIsValid
go test ./internal/analytics/domain/... -v -run TestEstadoPagoString
```

Expected: all PASS.

- [ ] **Step 2.6: Commit**

```bash
git add internal/analytics/domain/estado_pago.go \
        internal/analytics/domain/estado_pago_test.go \
        internal/analytics/domain/errors.go
git commit -m "feat(analytics): add EstadoPago VO with Parse/IsValid/String"
```

---

## Task 3: Domain entity — add `fechaUltimoPago` field

**Files:**
- Modify: `internal/analytics/domain/winback_candidato.go`
- Modify: `internal/analytics/domain/winback_candidato_test.go`

- [ ] **Step 3.1: Write the failing entity field test**

At the bottom of `internal/analytics/domain/winback_candidato_test.go`, add a new test function:

```go
func TestWinbackCandidato_FechaUltimoPago(t *testing.T) {
	t.Parallel()

	fechaPago := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)

	t.Run("Crear sets fechaUltimoPago to UTC", func(t *testing.T) {
		t.Parallel()
		p := validParams()
		p.FechaUltimoPago = fechaPago
		got, err := domain.CrearWinbackCandidato(p)
		require.NoError(t, err)
		assert.Equal(t, fechaPago.UTC(), got.FechaUltimoPago(), "fechaUltimoPago must be UTC")
	})

	t.Run("Crear with zero fechaUltimoPago remains zero", func(t *testing.T) {
		t.Parallel()
		p := validParams()
		p.FechaUltimoPago = time.Time{}
		got, err := domain.CrearWinbackCandidato(p)
		require.NoError(t, err)
		assert.True(t, got.FechaUltimoPago().IsZero(), "zero fechaUltimoPago must remain zero")
	})

	t.Run("Hydrate round-trips fechaUltimoPago", func(t *testing.T) {
		t.Parallel()
		p := domain.HydrateWinbackCandidatoParams{
			ID:              uuid.New(),
			ClienteID:       10,
			CohorteFecha:    now,
			FechaUltimoPago: fechaPago,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		got := domain.HydrateWinbackCandidato(p)
		assert.Equal(t, fechaPago, got.FechaUltimoPago())
	})

	t.Run("Hydrate with zero fechaUltimoPago is zero", func(t *testing.T) {
		t.Parallel()
		p := domain.HydrateWinbackCandidatoParams{
			ID:           uuid.New(),
			ClienteID:    10,
			CohorteFecha: now,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		got := domain.HydrateWinbackCandidato(p)
		assert.True(t, got.FechaUltimoPago().IsZero())
	})
}
```

- [ ] **Step 3.2: Run tests — must fail**

```bash
go test ./internal/analytics/domain/... -run TestWinbackCandidato_FechaUltimoPago 2>&1
```

Expected: compilation error — `FechaUltimoPago` not found in params structs or entity.

- [ ] **Step 3.3: Update `winback_candidato.go`**

In the `WinbackCandidato` struct, after the `cohorteFecha` field, add:

```go
	fechaUltimoPago   time.Time
```

In `CrearWinbackCandidatoParams`, after `EnControl bool`, add:

```go
	// FechaUltimoPago is the most recent payment date across open cargos.
	// Zero if the client has no payment history.
	FechaUltimoPago time.Time
```

In `CrearWinbackCandidato`, after the `fechaUltimaCompra` normalization block, add:

```go
	var fechaUltimoPago time.Time
	if !p.FechaUltimoPago.IsZero() {
		fechaUltimoPago = p.FechaUltimoPago.UTC()
	}
```

In the `return &WinbackCandidato{...}` inside `CrearWinbackCandidato`, after `cohorteFecha: p.CohorteFecha.UTC(),`, add:

```go
		fechaUltimoPago:   fechaUltimoPago,
```

In `HydrateWinbackCandidatoParams`, after `CohorteFecha time.Time`, add:

```go
	// FechaUltimoPago is the most recent payment date from persistence.
	// Zero when the column is NULL (no payment history).
	FechaUltimoPago time.Time
```

In `HydrateWinbackCandidato`, in the `return &WinbackCandidato{...}` literal, after `cohorteFecha: p.CohorteFecha,`, add:

```go
		fechaUltimoPago:   p.FechaUltimoPago,
```

After the `CohorteFecha()` getter, add:

```go
// FechaUltimoPago returns the UTC timestamp of the client's most recent payment
// across open cargos. Returns zero time.Time when no payment history is available.
func (w *WinbackCandidato) FechaUltimoPago() time.Time { return w.fechaUltimoPago }
```

- [ ] **Step 3.4: Run tests — must pass**

```bash
go test ./internal/analytics/domain/... -v
```

Expected: all tests including `TestWinbackCandidato_FechaUltimoPago` PASS.

- [ ] **Step 3.5: Commit**

```bash
git add internal/analytics/domain/winback_candidato.go \
        internal/analytics/domain/winback_candidato_test.go
git commit -m "feat(analytics): add fechaUltimoPago field to WinbackCandidato entity"
```

---

## Task 4: Port — add `FechaUltimoPago` to `AnclaCliente`

**Files:**
- Modify: `internal/analytics/ports/outbound/microsip.go`

- [ ] **Step 4.1: Add field to `AnclaCliente`**

In `internal/analytics/ports/outbound/microsip.go`, after the `NextBestProduct` field, add:

```go
	// FechaUltimoPago is the most recent payment date across the client's open
	// cargos in MSP_SALDOS_VENTAS (CARGO_CANCELADO='N'). Derived as
	// MAX(sv.FECHA_ULT_PAGO) per CLIENTE_ID. Zero if the client has never made
	// a payment.
	FechaUltimoPago time.Time
```

- [ ] **Step 4.2: Build to confirm it compiles**

```bash
cd /Volumes/M2-1TB/Developer/msp-api
go build ./internal/analytics/...
```

Expected: compilation error in `analyticsfb` where `assembleAncla` does not yet set `FechaUltimoPago` — this is expected. Confirm the error is only in `analyticsfb` files, not in `domain` or `ports`.

- [ ] **Step 4.3: Commit**

```bash
git add internal/analytics/ports/outbound/microsip.go
git commit -m "feat(analytics): add FechaUltimoPago to AnclaCliente port"
```

---

## Task 5: Repo — wire `FECHA_ULTIMO_PAGO` through the DB layer

**Files:**
- Modify: `internal/analytics/infra/analyticsfb/queries.go`
- Modify: `internal/analytics/infra/analyticsfb/rowmappers.go`
- Modify: `internal/analytics/infra/analyticsfb/repo.go`

### 5a: queries.go

- [ ] **Step 5a.1: Add `FECHA_ULTIMO_PAGO` to `candidatoCols`**

In `internal/analytics/infra/analyticsfb/queries.go`, replace the `candidatoCols` constant:

```go
const candidatoCols = `
	ID,
	CLIENTE_ID,
	NOMBRE,
	ZONA,
	TELEFONO,
	FECHA_ULTIMA_COMPRA,
	FRECUENCIA,
	MONETARY,
	SALDO,
	POR_LIQUIDAR_PCT,
	NEXT_BEST_PRODUCT,
	EN_CONTROL,
	COHORTE_FECHA,
	CREATED_AT,
	UPDATED_AT,
	FECHA_ULTIMO_PAGO`
```

The comment above `leerAnclasRFMClose` explains the column order. Update the column order comment in `queries.go` to reflect 11 columns now:

In the block comment above `leerAnclasRFMBase`, update the column-order doc:

```
// Column order (must match anclaRowRaw.scanFrom exactly):
//
//	1  cliente_id
//	2  nombre         (Win1252)
//	3  zona           (Win1252, may be empty)
//	4  telefono       (Win1252, may be NULL)
//	5  fecha_ultima_compra
//	6  frecuencia
//	7  monetary
//	8  saldo          (floored at 0)
//	9  por_liquidar   (NUMERIC(5,2), 0–100)
//	10 next_best_product (Win1252, may be '')
//	11 fecha_ultimo_pago (TIMESTAMP, may be NULL)
```

- [ ] **Step 5a.2: Add `MAX(sv.FECHA_ULT_PAGO)` to `saldo_cte`**

In `internal/analytics/infra/analyticsfb/queries.go`, replace the `leerAnclasRFMClose` constant:

```go
const leerAnclasRFMClose = `
  GROUP BY pv.CLIENTE_ID
),
saldo_cte AS (
  SELECT
    sv.CLIENTE_ID,
    CASE WHEN CAST(SUM(sv.SALDO) AS NUMERIC(18,2)) > 0
         THEN CAST(SUM(sv.SALDO) AS NUMERIC(18,2))
         ELSE 0
    END                                        AS SALDO,
    CAST(SUM(sv.PRECIO_TOTAL) AS NUMERIC(18,2)) AS PRECIO_TOTAL_SUM,
    MAX(sv.FECHA_ULT_PAGO)                      AS FECHA_ULTIMO_PAGO
  FROM MSP_SALDOS_VENTAS sv
  WHERE sv.CARGO_CANCELADO = 'N'
  GROUP BY sv.CLIENTE_ID
),`
```

- [ ] **Step 5a.3: Add `FECHA_ULTIMO_PAGO` to the final SELECT in `leerAnclasNBPClose`**

In `internal/analytics/infra/analyticsfb/queries.go`, replace the `leerAnclasNBPClose` constant so the final SELECT includes the new column at position 11:

```go
const leerAnclasNBPClose = `
  GROUP BY pv.CLIENTE_ID, a.NOMBRE
),
nbp_max AS (
  SELECT CLIENTE_ID, MAX(CNT) AS MAX_CNT
  FROM nbp_freq
  GROUP BY CLIENTE_ID
),
nbp AS (
  SELECT f.CLIENTE_ID, MIN(f.ARTICULO_NOMBRE) AS ARTICULO_NOMBRE
  FROM nbp_freq f
  JOIN nbp_max m ON m.CLIENTE_ID = f.CLIENTE_ID AND m.MAX_CNT = f.CNT
  GROUP BY f.CLIENTE_ID
)
SELECT
  rfm.CLIENTE_ID,
  c.NOMBRE                                                            AS NOMBRE,
  COALESCE(z.NOMBRE, '')                                             AS ZONA,
  d.TELEFONO1                                                        AS TELEFONO,
  rfm.FECHA_ULTIMA_COMPRA,
  rfm.FRECUENCIA,
  rfm.MONETARY,
  COALESCE(sc.SALDO, 0)                                              AS SALDO,
  CASE WHEN COALESCE(sc.PRECIO_TOTAL_SUM, 0) > 0
            AND COALESCE(sc.SALDO, 0) > 0
       THEN CAST(
              sc.SALDO / sc.PRECIO_TOTAL_SUM * 100
              AS NUMERIC(5,2))
       ELSE 0
  END                                                                AS POR_LIQUIDAR_PCT,
  SUBSTRING(COALESCE(nbp.ARTICULO_NOMBRE, '') FROM 1 FOR 120)        AS NEXT_BEST_PRODUCT,
  sc.FECHA_ULTIMO_PAGO                                               AS FECHA_ULTIMO_PAGO
FROM rfm
JOIN CLIENTES c ON c.CLIENTE_ID = rfm.CLIENTE_ID
LEFT JOIN ZONAS_CLIENTES z   ON z.ZONA_CLIENTE_ID = c.ZONA_CLIENTE_ID
LEFT JOIN DIRS_CLIENTES d    ON d.CLIENTE_ID = c.CLIENTE_ID AND d.ES_DIR_PPAL = 'S'
LEFT JOIN saldo_cte sc       ON sc.CLIENTE_ID = rfm.CLIENTE_ID
LEFT JOIN nbp                ON nbp.CLIENTE_ID = rfm.CLIENTE_ID`
```

### 5b: rowmappers.go

- [ ] **Step 5b.1: Add `fechaUltimoPagoRaw` to `candidatoRowRaw`**

In `internal/analytics/infra/analyticsfb/rowmappers.go`, inside the `candidatoRowRaw` struct, after `updatedAtRaw any // TIMESTAMP NOT NULL`, add:

```go
	fechaUltimoPagoRaw any   // TIMESTAMP nullable
```

In the `candidatoRowRaw.scanFrom` method, after `&r.updatedAtRaw,`, add:

```go
		&r.fechaUltimoPagoRaw,
```

In `assembleCandidato`, after the `updatedAt` scan block, add:

```go
	fechaUltimoPago, err := scanNullableTime(r.fechaUltimoPagoRaw)
	if err != nil {
		return nil, err
	}
```

In `assembleCandidato`, in the `domain.HydrateWinbackCandidato(domain.HydrateWinbackCandidatoParams{...})` call, after `CohorteFecha: cohorteFecha,`, add:

```go
		FechaUltimoPago:   fechaUltimoPago,
```

- [ ] **Step 5b.2: Add `fechaUltimoPagoRaw` to `anclaRowRaw`**

In `internal/analytics/infra/analyticsfb/rowmappers.go`, in the `anclaRowRaw` struct, after `nextBestProduct firebird.Win1252`, add:

```go
	fechaUltimoPagoRaw any // TIMESTAMP nullable: MAX(sv.FECHA_ULT_PAGO)
```

In `anclaRowRaw.scanFrom`, after `&r.nextBestProduct,`, add:

```go
		&r.fechaUltimoPagoRaw,
```

In `assembleAncla`, after `fechaUltimaCompra, err := scanNullableTime(r.fechaUltimaCompra)` and its error check, add:

```go
	fechaUltimoPago, err := scanNullableTime(r.fechaUltimoPagoRaw)
	if err != nil {
		return outbound.AnclaCliente{}, err
	}
```

In the `return outbound.AnclaCliente{...}` in `assembleAncla`, after `NextBestProduct: string(r.nextBestProduct),`, add:

```go
		FechaUltimoPago:   fechaUltimoPago,
```

### 5c: repo.go — upsert block

- [ ] **Step 5c.1: Add `FECHA_ULTIMO_PAGO` to `buildUpsertBlock`**

The upsert block currently has 15 params per row. Add 1 more (`_fup` = fechaUltimoPago), making it 16 params per row.

Update the comment on `buildUpsertBlock` to say `16 per row` and update `upsertChunkSize` comment:

In `internal/analytics/infra/analyticsfb/repo.go`, change:

```go
// upsertChunkSize is the number of candidatos sent per EXECUTE BLOCK call.
// 20 rows × 15 params = 300 positional params per block. Each row references
// MSP_AN_WINBACK_CANDIDATOS twice (UPDATE + conditional INSERT), so 20 rows =
// 40 Relation contexts — safely below Firebird's 256-context-per-statement limit.
// Empirically the optimal chunk size for this workload against Firebird 5 is
// 10–20: below 10 the round-trip overhead dominates; above 30 Firebird's
// per-statement parse overhead grows faster than the round-trip savings.
// Each chunk is one DB round-trip instead of up to 2 per row.
const upsertChunkSize = 20
```

to:

```go
// upsertChunkSize is the number of candidatos sent per EXECUTE BLOCK call.
// 20 rows × 16 params = 320 positional params per block. Each row references
// MSP_AN_WINBACK_CANDIDATOS twice (UPDATE + conditional INSERT), so 20 rows =
// 40 Relation contexts — safely below Firebird's 256-context-per-statement limit.
// Empirically the optimal chunk size for this workload against Firebird 5 is
// 10–20: below 10 the round-trip overhead dominates; above 30 Firebird's
// per-statement parse overhead grows faster than the round-trip savings.
// Each chunk is one DB round-trip instead of up to 2 per row.
const upsertChunkSize = 20
```

In `buildUpsertBlock`, update the comment and the block:

```go
func buildUpsertBlock(chunk []*domain.WinbackCandidato) (string, []any) {
	n := len(chunk)
	args := make([]any, 0, n*16)

	var header strings.Builder
	var body strings.Builder

	_, _ = header.WriteString("EXECUTE BLOCK (\n")
	_, _ = body.WriteString("AS\nBEGIN\n")

	for i, c := range chunk {
		p := fmt.Sprintf("p%d", i)
		if i > 0 {
			_, _ = header.WriteString(",\n")
		}
		// Declare 16 typed input params per row.
		_, _ = fmt.Fprintf(
			&header,
			"  %s_id  VARCHAR(36)    = ?,\n"+
				"  %s_cid INTEGER        = ?,\n"+
				"  %s_nom VARCHAR(200)   = ?,\n"+
				"  %s_zon VARCHAR(100)   = ?,\n"+
				"  %s_tel VARCHAR(50)    = ?,\n"+
				"  %s_fuc TIMESTAMP      = ?,\n"+
				"  %s_frq INTEGER        = ?,\n"+
				"  %s_mon NUMERIC(18,2)  = ?,\n"+
				"  %s_sal NUMERIC(18,2)  = ?,\n"+
				"  %s_plp NUMERIC(5,2)   = ?,\n"+
				"  %s_nbp VARCHAR(120)   = ?,\n"+
				"  %s_enc SMALLINT       = ?,\n"+
				"  %s_coh TIMESTAMP      = ?,\n"+
				"  %s_cat TIMESTAMP      = ?,\n"+
				"  %s_upd TIMESTAMP      = ?,\n"+
				"  %s_fup TIMESTAMP      = ?",
			p, p, p, p, p,
			p, p, p, p, p,
			p, p, p, p, p,
			p,
		)

		// Body: UPDATE mutable fields (EN_CONTROL/COHORTE_FECHA excluded;
		// FECHA_ULTIMO_PAGO IS mutable — update it on each refresh).
		// Then INSERT the full row when no existing row was matched.
		_, _ = fmt.Fprintf(
			&body,
			"  UPDATE MSP_AN_WINBACK_CANDIDATOS SET\n"+
				"    NOMBRE=:%s_nom, ZONA=:%s_zon, TELEFONO=:%s_tel,\n"+
				"    FECHA_ULTIMA_COMPRA=:%s_fuc, FRECUENCIA=:%s_frq,\n"+
				"    MONETARY=:%s_mon, SALDO=:%s_sal,\n"+
				"    POR_LIQUIDAR_PCT=:%s_plp, NEXT_BEST_PRODUCT=:%s_nbp,\n"+
				"    FECHA_ULTIMO_PAGO=:%s_fup,\n"+
				"    UPDATED_AT=:%s_upd\n"+
				"  WHERE CLIENTE_ID=:%s_cid;\n"+
				"  IF (ROW_COUNT=0) THEN\n"+
				"    INSERT INTO MSP_AN_WINBACK_CANDIDATOS\n"+
				"      (ID,CLIENTE_ID,NOMBRE,ZONA,TELEFONO,FECHA_ULTIMA_COMPRA,\n"+
				"       FRECUENCIA,MONETARY,SALDO,POR_LIQUIDAR_PCT,NEXT_BEST_PRODUCT,\n"+
				"       EN_CONTROL,COHORTE_FECHA,CREATED_AT,UPDATED_AT,FECHA_ULTIMO_PAGO)\n"+
				"    VALUES(:%s_id,:%s_cid,:%s_nom,:%s_zon,:%s_tel,:%s_fuc,\n"+
				"           :%s_frq,:%s_mon,:%s_sal,:%s_plp,:%s_nbp,\n"+
				"           :%s_enc,:%s_coh,:%s_cat,:%s_upd,:%s_fup);\n",
			p, p, p,
			p, p,
			p, p,
			p, p,
			p,
			p,
			p,
			p, p, p, p, p, p,
			p, p, p, p, p,
			p, p, p, p, p,
		)

		// Bind args in param-declaration order (16 per row).
		enControl := 0
		if c.EnControl() {
			enControl = 1
		}
		args = append(
			args,
			c.ID().String(), // _id
			c.ClienteID(),   // _cid
			c.Nombre(),      // _nom
			c.Zona(),        // _zon
			c.Telefono(),    // _tel
			nullableWallClockArg(wallClockPtrFromTime(c.FechaUltimaCompra())), // _fuc
			c.Frecuencia(),                         // _frq
			c.Monetary(),                           // _mon
			c.Saldo(),                              // _sal
			c.PorLiquidarPct(),                     // _plp
			c.NextBestProduct(),                    // _nbp
			enControl,                              // _enc
			firebird.ToWallClock(c.CohorteFecha()), // _coh
			firebird.ToWallClock(c.CreatedAt()),    // _cat
			firebird.ToWallClock(c.UpdatedAt()),    // _upd
			nullableWallClockArg(wallClockPtrFromTime(c.FechaUltimoPago())), // _fup
		)
	}

	_, _ = header.WriteString("\n)")
	_, _ = body.WriteString("END")

	return header.String() + "\n" + body.String(), args
}
```

- [ ] **Step 5c.2: Build**

```bash
go build ./internal/analytics/...
```

Expected: clean build.

- [ ] **Step 5c.3: Run existing unit tests**

```bash
go test ./internal/analytics/domain/... ./internal/analytics/app/... -v
```

Expected: all existing tests still pass.

- [ ] **Step 5c.4: Commit**

```bash
git add internal/analytics/infra/analyticsfb/queries.go \
        internal/analytics/infra/analyticsfb/rowmappers.go \
        internal/analytics/infra/analyticsfb/repo.go
git commit -m "feat(analytics): wire FECHA_ULTIMO_PAGO through repo (upsert + scan)"
```

---

## Task 6: Integration tests for the repo

**Files:**
- Modify: `internal/analytics/infra/analyticsfb/repo_test.go`

- [ ] **Step 6.1: Update `makeCandidato` to include `FechaUltimoPago`**

The existing `makeCandidato` helper in `repo_test.go` doesn't pass `FechaUltimoPago`. Update it so it passes a non-zero value (reuse `fixedFechaUltima` which already exists):

In `repo_test.go`, in `makeCandidato`, add `FechaUltimoPago: fixedFechaUltima,` to the `domain.CrearWinbackCandidatoParams{...}` struct literal. Also add a fixed date for the pago anchor:

After the existing `var fixedFechaUltima = ...` declaration, add:

```go
// fixedFechaPago is a deterministic last-payment date (different from last-purchase).
var fixedFechaPago = time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC)
```

Update `makeCandidato` to add `FechaUltimoPago: fixedFechaPago,` to the params.

- [ ] **Step 6.2: Add FECHA_ULTIMO_PAGO round-trip assertion to existing round-trip test**

In `TestRepo_UpsertAndList_RoundTrip`, after the existing `assert.WithinDuration(t, fixedFechaUltima, got.FechaUltimaCompra(), time.Second)` assertion, add:

```go
		assert.WithinDuration(t, fixedFechaPago, got.FechaUltimoPago(), time.Second,
			"FECHA_ULTIMO_PAGO must round-trip correctly")
```

- [ ] **Step 6.3: Add new integration test for NULL FECHA_ULTIMO_PAGO**

Add after `TestRepo_ExistingControlFlags_ReturnsCorrectMap`:

```go
// TestRepo_UpsertAndList_NullFechaUltimoPago verifies that when FechaUltimoPago
// is zero (no payment history), the column round-trips as zero time.Time.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestRepo_UpsertAndList_NullFechaUltimoPago(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		const clienteID = -10030
		c, err := domain.CrearWinbackCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:       clienteID,
			Nombre:          "Sin Pago",
			Zona:            "R/TEST",
			Frecuencia:      1,
			Monetary:        decimal.RequireFromString("5000.00"),
			Saldo:           decimal.Zero,
			PorLiquidarPct:  decimal.Zero,
			FechaUltimoPago: time.Time{}, // explicit zero
			CohorteFecha:    fixedCohorte,
			Now:             fixedNow,
		})
		require.NoError(t, err)

		err = repo.UpsertCandidatos(ctx, []*domain.WinbackCandidato{c})
		if err != nil {
			t.Skipf("UpsertCandidatos failed — migration 000036 may not be applied: %v", err)
		}

		page, err := repo.ListCandidatos(ctx, outbound.ListWinbackParams{})
		require.NoError(t, err)

		var got *domain.WinbackCandidato
		for _, item := range page.Items {
			if item.ClienteID() == clienteID {
				got = item
				break
			}
		}
		require.NotNil(t, got)
		assert.True(t, got.FechaUltimoPago().IsZero(),
			"NULL FECHA_ULTIMO_PAGO must scan as zero time.Time, got %v", got.FechaUltimoPago())
	})
}
```

- [ ] **Step 6.4: Run integration tests**

```bash
FB_DATABASE=$(grep FB_DATABASE /Volumes/M2-1TB/Developer/msp-api/.env | cut -d= -f2) \
  go test ./internal/analytics/infra/analyticsfb/... -v -run "TestRepo_Upsert|TestRepo_List|TestRepo_LeerAnclasDesde" 2>&1
```

Expected: all pass. The `LeerAnclasDesde_Regression` test now also verifies `FechaUltimoPago` is populated on the returned ancla (no assertion needed — the scan itself would error if column count mismatched).

- [ ] **Step 6.5: Verify timing of `LeerAnclasDesde(nil)` has not regressed**

```bash
FB_DATABASE=$(grep FB_DATABASE /Volumes/M2-1TB/Developer/msp-api/.env | cut -d= -f2) \
  go test ./internal/analytics/infra/analyticsfb/... -v -run "TestRepo_LeerAnclasDesde_Regression" -timeout 300s 2>&1 | grep "elapsed="
```

Expected: elapsed time similar to pre-change (MAX(FECHA_ULT_PAGO) is just another aggregate in an already-grouped saldo_cte — no extra table scan).

- [ ] **Step 6.6: Commit**

```bash
git add internal/analytics/infra/analyticsfb/repo_test.go
git commit -m "test(analytics): update repo integration tests for FECHA_ULTIMO_PAGO round-trip"
```

---

## Task 7: App layer — `estadoPagoFor` pure function + `WinbackListItem`

**Files:**
- Modify: `internal/analytics/app/scoring.go`
- Modify: `internal/analytics/app/scoring_test.go` (or new file if preferred)
- Modify: `internal/analytics/app/winback_query.go`
- Modify: `internal/analytics/app/winback_query_test.go`
- Modify: `internal/analytics/app/refrescar_command.go`

### 7a: scoring.go — add thresholds and `estadoPagoFor`

- [ ] **Step 7a.1: Write the failing test for `estadoPagoFor`**

In `internal/analytics/app/scoring_test.go`, add (the file already has `computeSegmentoScore` tests; find the pattern):

```go
// TestEstadoPagoFor covers all branches of the estadoPagoFor pure function.
// All inputs are deterministic UTC times to guarantee no TZ sensitivity.
func TestEstadoPagoFor(t *testing.T) {
	t.Parallel()

	baseNow := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	zero := time.Time{}
	recent := baseNow.AddDate(0, 0, -15)    // 15 days ago — within 30d threshold
	mid := baseNow.AddDate(0, 0, -60)       // 60 days ago — between 30 and 90
	old := baseNow.AddDate(0, 0, -120)      // 120 days ago — beyond 90d threshold

	tests := []struct {
		name            string
		saldo           decimal.Decimal
		fechaUltimoPago time.Time
		want            domain.EstadoPago
	}{
		// saldo == 0 branch
		{
			name:            "saldo zero and no payment date → SIN_CREDITO",
			saldo:           decimal.Zero,
			fechaUltimoPago: zero,
			want:            domain.EstadoPagoSinCredito,
		},
		{
			name:            "saldo zero and has payment date → LIQUIDADO",
			saldo:           decimal.Zero,
			fechaUltimoPago: recent,
			want:            domain.EstadoPagoLiquidado,
		},
		// saldo > 0 branch
		{
			name:            "saldo positive, paid 15d ago → AL_CORRIENTE",
			saldo:           decimal.NewFromInt(500),
			fechaUltimoPago: recent,
			want:            domain.EstadoPagoAlCorriente,
		},
		{
			name:            "saldo positive, paid exactly at umbralAlCorrienteDias → AL_CORRIENTE",
			saldo:           decimal.NewFromInt(500),
			fechaUltimoPago: baseNow.AddDate(0, 0, -30), // exactly 30 days
			want:            domain.EstadoPagoAlCorriente,
		},
		{
			name:            "saldo positive, paid 31d ago → ATRASADO",
			saldo:           decimal.NewFromInt(500),
			fechaUltimoPago: baseNow.AddDate(0, 0, -31),
			want:            domain.EstadoPagoAtrasado,
		},
		{
			name:            "saldo positive, paid 60d ago → ATRASADO",
			saldo:           decimal.NewFromInt(500),
			fechaUltimoPago: mid,
			want:            domain.EstadoPagoAtrasado,
		},
		{
			name:            "saldo positive, paid exactly at umbralAtrasadoDias → ATRASADO",
			saldo:           decimal.NewFromInt(500),
			fechaUltimoPago: baseNow.AddDate(0, 0, -90), // exactly 90 days
			want:            domain.EstadoPagoAtrasado,
		},
		{
			name:            "saldo positive, paid 91d ago → MOROSO",
			saldo:           decimal.NewFromInt(500),
			fechaUltimoPago: baseNow.AddDate(0, 0, -91),
			want:            domain.EstadoPagoMoroso,
		},
		{
			name:            "saldo positive, paid 120d ago → MOROSO",
			saldo:           decimal.NewFromInt(500),
			fechaUltimoPago: old,
			want:            domain.EstadoPagoMoroso,
		},
		{
			name:            "saldo positive, no payment date → MOROSO",
			saldo:           decimal.NewFromInt(500),
			fechaUltimoPago: zero,
			want:            domain.EstadoPagoMoroso,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := app.EstadoPagoForExport(tc.saldo, tc.fechaUltimoPago, baseNow)
			assert.Equal(t, tc.want, got, "saldo=%s fechaUltimoPago=%v", tc.saldo, tc.fechaUltimoPago)
		})
	}
}
```

Note: `EstadoPagoForExport` is exported in `export_test.go` (follow the existing pattern for testing unexported functions — check `internal/analytics/app/export_test.go`).

- [ ] **Step 7a.2: Check export_test.go for the existing pattern**

Read `internal/analytics/app/export_test.go`:

```bash
cat /Volumes/M2-1TB/Developer/msp-api/internal/analytics/app/export_test.go
```

Then add the export for `estadoPagoFor` following the same pattern. The file likely contains lines like:

```go
var ComputeSegmentoScore = computeSegmentoScore
```

Add:

```go
var EstadoPagoForExport = estadoPagoFor
```

- [ ] **Step 7a.3: Run test — must fail (estadoPagoFor not defined yet)**

```bash
go test ./internal/analytics/app/... -run TestEstadoPagoFor 2>&1
```

Expected: compilation error — `estadoPagoFor` undefined.

- [ ] **Step 7a.4: Add thresholds and `estadoPagoFor` to `scoring.go`**

In `internal/analytics/app/scoring.go`, in the constants block (R1 tunables), add:

```go
	// umbralAlCorrienteDias is the maximum days since last payment for a client
	// with outstanding saldo to still be considered AL_CORRIENTE (on time).
	// R1 heuristic: 30 days maps to a monthly payment cycle typical of furniture
	// credit (planes de 12–36 mensualidades).
	umbralAlCorrienteDias = 30

	// umbralAtrasadoDias is the maximum days since last payment before a client
	// is classified as MOROSO instead of ATRASADO.
	// R1 heuristic: 90 days = 3 missed monthly payments.
	umbralAtrasadoDias = 90
```

After `clamp01`, add the new pure function:

```go
// estadoPagoFor computes the EstadoPago solvency signal from saldo and the
// client's most recent payment date.
//
// Thresholds (R1 heuristics — tune via umbralAlCorrienteDias / umbralAtrasadoDias):
//
//	saldo == 0:
//	  fechaUltimoPago zero → SIN_CREDITO (contado-only, never had open balance)
//	  fechaUltimoPago non-zero → LIQUIDADO (had credit, now fully paid)
//	saldo > 0:
//	  diasSinPagar <= umbralAlCorrienteDias  → AL_CORRIENTE
//	  diasSinPagar <= umbralAtrasadoDias     → ATRASADO
//	  else (including fechaUltimoPago zero)  → MOROSO
//
// now must be UTC; zero fechaUltimoPago is treated as extremely old (MOROSO
// when saldo > 0, SIN_CREDITO when saldo == 0).
func estadoPagoFor(saldo decimal.Decimal, fechaUltimoPago time.Time, now time.Time) domain.EstadoPago {
	if !saldo.IsPositive() {
		// Client has no outstanding balance.
		if fechaUltimoPago.IsZero() {
			return domain.EstadoPagoSinCredito
		}
		return domain.EstadoPagoLiquidado
	}
	// saldo > 0: classify by how long since last payment.
	if fechaUltimoPago.IsZero() {
		// Never paid — treat as extremely delinquent.
		return domain.EstadoPagoMoroso
	}
	diasSinPagar := int(now.Sub(fechaUltimoPago).Hours() / 24)
	if diasSinPagar < 0 {
		diasSinPagar = 0
	}
	switch {
	case diasSinPagar <= umbralAlCorrienteDias:
		return domain.EstadoPagoAlCorriente
	case diasSinPagar <= umbralAtrasadoDias:
		return domain.EstadoPagoAtrasado
	default:
		return domain.EstadoPagoMoroso
	}
}
```

- [ ] **Step 7a.5: Run tests — must pass**

```bash
go test ./internal/analytics/app/... -v -run TestEstadoPagoFor
```

Expected: all 10 table cases pass.

### 7b: winback_query.go — add `EstadoPago` to `WinbackListItem`

- [ ] **Step 7b.1: Add `EstadoPago` to `WinbackListItem`**

In `internal/analytics/app/winback_query.go`, in the `WinbackListItem` struct, after `RecenciaDias int`, add:

```go
	// EstadoPago is the payment-solvency classification computed at read time
	// from the candidato's saldo and FechaUltimoPago.
	EstadoPago domain.EstadoPago
```

- [ ] **Step 7b.2: Compute EstadoPago in the `ListarWinback` loop**

In the scoring loop inside `ListarWinback`, replace:

```go
		items = append(items, WinbackListItem{
			Candidato:    c,
			Segmento:     seg,
			Score:        score,
			RecenciaDias: recencia,
		})
```

with:

```go
		ep := estadoPagoFor(c.Saldo(), c.FechaUltimoPago(), now)
		items = append(items, WinbackListItem{
			Candidato:    c,
			Segmento:     seg,
			Score:        score,
			RecenciaDias: recencia,
			EstadoPago:   ep,
		})
```

- [ ] **Step 7b.3: Add EstadoPago assertion to existing list tests**

In `internal/analytics/app/winback_query_test.go`, in `TestListarWinback_Ordering`, after checking the last item's score, add a check that all items have a non-empty EstadoPago:

```go
	for i, item := range items {
		assert.NotEmpty(t, item.EstadoPago.String(),
			"items[%d]: EstadoPago must be populated", i)
		assert.True(t, item.EstadoPago.IsValid(),
			"items[%d]: EstadoPago must be a valid value, got %q", i, item.EstadoPago)
	}
```

Also add a specific test for `EstadoPago` classification:

```go
func TestListarWinback_EstadoPago_Populated(t *testing.T) {
	t.Parallel()

	// cPaid: saldo=0, has a payment date → LIQUIDADO
	cPaid := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 10, FechaUltimaCompra: testNow.AddDate(0, 0, -200),
		Frecuencia: 2, Monetary: decimal.NewFromInt(10_000),
		Saldo: decimal.Zero,
		CohorteFecha: testNow.AddDate(-1, 0, 0), Now: testNow,
		// Note: FechaUltimoPago intentionally left as zero (no explicit payment date)
		// → SIN_CREDITO when saldo == 0.
	})
	// cMoroso: saldo>0, no payment date → MOROSO
	cMoroso := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 11, FechaUltimaCompra: testNow.AddDate(0, 0, -400),
		Frecuencia: 4, Monetary: decimal.NewFromInt(30_000),
		Saldo: decimal.NewFromInt(8_000),
		CohorteFecha: testNow.AddDate(-1, 0, 0), Now: testNow,
		// FechaUltimoPago zero + saldo > 0 → MOROSO
	})

	repo := newFakeWinbackRepo()
	repo.candidates = []*domain.WinbackCandidato{cPaid, cMoroso}
	svc := app.NewService(repo, nil, fixedClock{testNow}, nil)

	items, err := svc.ListarWinback(context.Background(), app.ListarWinbackParams{
		IncluirControl: true,
	})
	require.NoError(t, err)
	require.Len(t, items, 2)

	byID := make(map[int]app.WinbackListItem)
	for _, it := range items {
		byID[it.Candidato.ClienteID()] = it
	}

	assert.Equal(t, domain.EstadoPagoSinCredito, byID[10].EstadoPago,
		"saldo=0 + no fechaUltimoPago → SIN_CREDITO")
	assert.Equal(t, domain.EstadoPagoMoroso, byID[11].EstadoPago,
		"saldo>0 + zero fechaUltimoPago → MOROSO")
}
```

### 7c: refrescar_command.go

- [ ] **Step 7c.1: Pass `FechaUltimoPago` in `buildCandidatos`**

In `internal/analytics/app/refrescar_command.go`, in `buildCandidatos`, in the `domain.CrearWinbackCandidato(domain.CrearWinbackCandidatoParams{...})` call, after `NextBestProduct: a.NextBestProduct,`, add:

```go
			FechaUltimoPago:   a.FechaUltimoPago,
```

- [ ] **Step 7c.2: Build and run all app tests**

```bash
go test ./internal/analytics/app/... -v
```

Expected: all tests pass, including new `TestListarWinback_EstadoPago_Populated` and `TestEstadoPagoFor`.

- [ ] **Step 7c.3: Commit**

```bash
git add internal/analytics/app/scoring.go \
        internal/analytics/app/scoring_test.go \
        internal/analytics/app/export_test.go \
        internal/analytics/app/winback_query.go \
        internal/analytics/app/winback_query_test.go \
        internal/analytics/app/refrescar_command.go
git commit -m "feat(analytics): add estadoPagoFor pure function and EstadoPago to WinbackListItem"
```

---

## Task 8: Contract and HTTP DTO

**Files:**
- Modify: `internal/analytics/analytics_contracts.go`
- Modify: `internal/analytics/analytics_contracts_mapper.go`
- Modify: `internal/analytics/infra/analyticshttp/dtos.go`
- Modify: `internal/analytics/infra/analyticshttp/dto_mapper.go`
- Modify: `internal/analytics/infra/analyticshttp/handlers_test.go`

### 8a: contracts

- [ ] **Step 8a.1: Update `WinbackCandidatoContract`**

In `internal/analytics/analytics_contracts.go`, after `EnControl bool`, add:

```go
	// FechaUltimoPago is the most recent payment date. Zero when no history.
	FechaUltimoPago time.Time
	// EstadoPago is the payment-solvency signal. Left empty by the entity-only
	// mapper; callers that need it must set it after mapping (same pattern as
	// Segmento and Score).
	EstadoPago string
```

In `internal/analytics/analytics_contracts_mapper.go`, in `ToWinbackCandidatoContract`, after `EnControl: c.EnControl(),`, add:

```go
		FechaUltimoPago: c.FechaUltimoPago(),
		// EstadoPago intentionally left empty — computed at read time by caller.
```

- [ ] **Step 8a.2: Build**

```bash
go build ./internal/analytics/...
```

Expected: clean.

### 8b: HTTP DTOs

- [ ] **Step 8b.1: Add fields to `WinbackItemDTO`**

In `internal/analytics/infra/analyticshttp/dtos.go`, in `WinbackItemDTO`, after `EnControl bool`, add:

```go
	FechaUltimoPago string `json:"fecha_ultimo_pago" format:"date-time" doc:"RFC3339 UTC de la fecha del último pago; vacío si sin historial de pagos"`
	EstadoPago      string `json:"estado_pago"       doc:"Señal de solvencia: SIN_CREDITO | LIQUIDADO | AL_CORRIENTE | ATRASADO | MOROSO"`
```

- [ ] **Step 8b.2: Map new fields in `dto_mapper.go`**

In `internal/analytics/infra/analyticshttp/dto_mapper.go`, in `toWinbackItemDTO`, after `EnControl: c.EnControl(),`, add:

```go
		FechaUltimoPago: formatTime(c.FechaUltimoPago()),
		EstadoPago:      item.EstadoPago.String(),
```

### 8c: HTTP handler test

- [ ] **Step 8c.1: Assert new fields in `TestListarWinback_HappyPath_200`**

In `internal/analytics/infra/analyticshttp/handlers_test.go`, in `TestListarWinback_HappyPath_200`, update the response struct to include the new fields:

```go
	var resp struct {
		Items []struct {
			ClienteID         int    `json:"cliente_id"`
			Nombre            string `json:"nombre"`
			Zona              string `json:"zona"`
			Telefono          string `json:"telefono"`
			FechaUltimaCompra string `json:"fecha_ultima_compra"`
			RecenciaDias      int    `json:"recencia_dias"`
			Frecuencia        int    `json:"frecuencia"`
			Monetary          string `json:"monetary"`
			Saldo             string `json:"saldo"`
			PorLiquidarPct    string `json:"por_liquidar_pct"`
			Segmento          string `json:"segmento"`
			Score             int    `json:"score"`
			EnControl         bool   `json:"en_control"`
			FechaUltimoPago   string `json:"fecha_ultimo_pago"`
			EstadoPago        string `json:"estado_pago"`
		} `json:"items"`
	}
```

After the existing assertions (e.g. `assert.False(t, item.EnControl)`), add:

```go
	// EstadoPago must be a known non-empty value.
	assert.NotEmpty(t, item.EstadoPago)
	ep, epErr := domain.ParseEstadoPago(item.EstadoPago)
	require.NoError(t, epErr, "estado_pago must be a valid EstadoPago value, got %q", item.EstadoPago)
	assert.True(t, ep.IsValid())

	// FechaUltimoPago: mustCandidato uses Saldo=500 and zero FechaUltimoPago
	// → EstadoPago must be MOROSO (saldo > 0, no payment date).
	assert.Equal(t, domain.EstadoPagoMoroso.String(), item.EstadoPago,
		"saldo=500 with zero FechaUltimoPago must be MOROSO")
	// FechaUltimoPago is empty because mustCandidato does not set it.
	assert.Empty(t, item.FechaUltimoPago,
		"fecha_ultimo_pago must be empty when FechaUltimoPago is zero")
```

Note: `mustCandidato` in `handlers_test.go` creates a candidato with `Saldo: decimal.NewFromInt(500)` and no explicit `FechaUltimoPago` (zero) — so `EstadoPago` must be `MOROSO`.

- [ ] **Step 8c.2: Run all HTTP tests**

```bash
go test ./internal/analytics/infra/analyticshttp/... -v
```

Expected: all pass including updated `TestListarWinback_HappyPath_200`.

- [ ] **Step 8c.3: Commit**

```bash
git add internal/analytics/analytics_contracts.go \
        internal/analytics/analytics_contracts_mapper.go \
        internal/analytics/analytics_contracts_mapper_test.go \
        internal/analytics/infra/analyticshttp/dtos.go \
        internal/analytics/infra/analyticshttp/dto_mapper.go \
        internal/analytics/infra/analyticshttp/handlers_test.go
git commit -m "feat(analytics): expose EstadoPago + FechaUltimoPago in contract and HTTP DTO"
```

---

## Task 9: Final verification pass

- [ ] **Step 9.1: Full build**

```bash
go build ./... 2>&1
```

Expected: no errors.

- [ ] **Step 9.2: Lint**

```bash
golangci-lint run ./internal/analytics/... 2>&1
```

Expected: clean (no new issues).

- [ ] **Step 9.3: Full unit test suite with -race**

```bash
go test -race ./internal/analytics/... 2>&1
```

Expected: all pass.

- [ ] **Step 9.4: Full integration test suite**

```bash
FB_DATABASE=$(grep FB_DATABASE /Volumes/M2-1TB/Developer/msp-api/.env | cut -d= -f2) \
  go test ./internal/analytics/infra/analyticsfb/... -v -timeout 300s 2>&1
```

Expected: all tests pass, including:
- `TestRepo_UpsertAndList_RoundTrip` — FECHA_ULTIMO_PAGO round-trips
- `TestRepo_UpsertAndList_NullFechaUltimoPago` — NULL scans as zero
- `TestRepo_LeerAnclasDesde_Regression` — timing not regressed
- `TestRepo_LeerAnclasDesde_Smoke` — returns valid anclas with FechaUltimoPago

Note the elapsed time printed by `TestRepo_LeerAnclasDesde_Regression` — paste it in the report.

- [ ] **Step 9.5: Final commit**

```bash
git add -u
git commit -m "feat(analytics): add payment-solvency signal (EstadoPago + FECHA_ULTIMO_PAGO anchor)"
```

---

## Self-Review: Spec Coverage Checklist

| Spec Requirement | Task |
|-----------------|------|
| Migration 000036 `.up.sql` and `.down.sql` | Task 1 |
| `EstadoPago` VO with Parse/IsValid/String + `ErrEstadoPagoInvalido` | Task 2 |
| `fechaUltimoPago` field in `WinbackCandidato` (Crear + Hydrate + getter) | Task 3 |
| `FechaUltimoPago` in `AnclaCliente` port | Task 4 |
| `MAX(sv.FECHA_ULT_PAGO)` in `saldo_cte` query | Task 5 (5a) |
| `FECHA_ULTIMO_PAGO` column in `candidatoCols` (ListCandidatos SELECT) | Task 5 (5a) |
| `FECHA_ULTIMO_PAGO` in upsert (UPDATE mutable branch + INSERT) | Task 5 (5c) |
| `anclaRowRaw` scan for new column | Task 5 (5b) |
| `candidatoRowRaw` scan for new column | Task 5 (5b) |
| `estadoPagoFor` pure function with named constants | Task 7a |
| `EstadoPago` field on `WinbackListItem` | Task 7b |
| `FechaUltimoPago` passed in `buildCandidatos` | Task 7c |
| `WinbackCandidatoContract` fields | Task 8a |
| `WinbackItemDTO` wire fields + mapper | Task 8b |
| Domain unit tests: VO + entity field | Tasks 2, 3 |
| App unit tests: `estadoPagoFor` table + list enrichment | Task 7 |
| Repo integration tests: round-trip + NULL | Task 6 |
| HTTP handler test: new fields in 200 response | Task 8c |
| Migration applied locally | Task 1 step 3 |
| Integration tests run against Firebird | Task 6 step 4, Task 9 step 4 |
| LeerAnclasDesde timing verified | Task 9 step 4 |
| Lint clean | Task 9 step 2 |

---

## EstadoPago Thresholds Chosen (R1 Heuristics)

| Constant | Value | Rationale |
|----------|-------|-----------|
| `umbralAlCorrienteDias` | 30 | Monthly payment cycle for furniture credit (planes 12–36 mensualidades) |
| `umbralAtrasadoDias` | 90 | Three missed monthly payments = serious delinquency signal |

These live in `app/scoring.go` alongside the other R1 tunables (`umbralActivoDias`, `umbralPerdidoDias`, etc.) so they can be recalibrated without touching any business logic.
