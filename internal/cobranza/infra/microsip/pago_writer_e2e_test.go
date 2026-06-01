// Package microsip_test — black-box E2E tests for PagoWriter against the real
// Microsip Firebird database. Tests commit real transactions and rely on
// UUID-scoped / ID-tracked cleanup to avoid polluting the shared DB.
//
// Required env vars: FB_DATABASE (plus FB_HOST / FB_PORT / FB_USER / FB_PASSWORD
// from .env). If FB_DATABASE is unset, all tests skip automatically.
//
// Cleanup contract (CRITICAL — this writes real money rows to Microsip):
//   - Every seedCargo INSERT is registered with t.Cleanup immediately after.
//   - Every PagoWriter.Aplicar INSERT is registered with t.Cleanup immediately
//     after a successful call.
//   - Cleanup order: FORMAS_COBRO_DOCTOS → IMPORTES_DOCTOS_CC → DOCTOS_CC
//     (children before parent to respect FK constraints).
//
//nolint:misspell // Microsip table/column identifiers are kept verbatim.
package microsip_test

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/infra/microsip"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// ─── skip guard ──────────────────────────────────────────────────────────────

func requireFBEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird E2E tests")
	}
}

// ─── well-known test fixtures ─────────────────────────────────────────────────

// These IDs must exist in the dev MUEBLERA.FDB database. They are used across
// all E2E tests so every test exercises the same real Microsip rows.
const (
	// testClienteID is a known CLIENTES row in MUEBLERA.FDB (used in
	// concurrency and saldo tests too — see internal/cobranza/infra/ventfb/).
	testClienteID = 11486
	// testCobradorID is the first COBRADORES row (RUTA 01).
	testCobradorID = 11294
	// testFormaCobroID is the "Efectivo" FORMAS_COBRO row (ID 67).
	testFormaCobroID = 67
	// testConceptoCCID is the cobranza-en-ruta concepto (87327 = PAGO RUTA).
	testConceptoCCID = 87327
)

// ─── cargo seeding ───────────────────────────────────────────────────────────

// seededCargo holds the IDs of a synthetic DOCTOS_CC cargo row and its
// IMPORTES_DOCTOS_CC importe line, inserted by seedCargo. Both IDs are needed
// for cleanup and for building MicrosipPagoInput.CargoDoctoCCID.
type seededCargo struct {
	doctoCCID    int
	importeRowID int
}

// seedCargo inserts a minimal DOCTOS_CC cargo (NATURALEZA_CONCEPTO='C') and
// its IMPORTES_DOCTOS_CC line (TIPO_IMPTE='C') in a committed transaction.
// PagoWriter.Aplicar opens its own separate transaction and must see the row.
//
// The FOLIO is derived from the generated DOCTO_CC_ID to guarantee uniqueness
// across concurrent test runs (DOCTOS_CC_AK1 unique on CONCEPTO_CC_ID+FOLIO+APLICADO).
//
// Cleanup is registered with t.Cleanup immediately after each INSERT so a
// panic mid-test still removes the seeded rows.
func seedCargo(
	t *testing.T,
	ctx context.Context, //nolint:revive // context-as-argument: ctx is the second param here by test convention (t first).
	pool *firebird.Pool,
	txMgr *firebird.TxManager,
	clienteID int,
	importe decimal.Decimal,
) seededCargo {
	t.Helper()

	var cargo seededCargo

	err := txMgr.RunInTx(ctx, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, pool.DB)

		// Claim a DOCTO_CC_ID from Microsip's shared generator.
		if err := q.QueryRowContext(ctx,
			`SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`,
		).Scan(&cargo.doctoCCID); err != nil {
			return err
		}

		// Register cleanup for the DOCTOS_CC row immediately — before any
		// subsequent statement that could fail.
		t.Cleanup(func() {
			cleanupQ := firebird.GetQuerier(context.Background(), pool.DB)
			_, _ = cleanupQ.ExecContext(context.Background(),
				`DELETE FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`, cargo.doctoCCID)
		})

		// DOCTOS_CC.FOLIO is VARCHAR(9). Use "S-" + the last 7 digits of the
		// generated ID (ID mod 9999999) so every seeded cargo gets a unique
		// FOLIO without exceeding the 9-char column limit — avoiding the
		// DOCTOS_CC_AK1 unique index violation (CONCEPTO_CC_ID+FOLIO+APLICADO).
		folio := "S-" + strconv.Itoa(cargo.doctoCCID%9999999)

		now := time.Now()
		_, err := q.ExecContext(ctx,
			`INSERT INTO DOCTOS_CC
			  (DOCTO_CC_ID, CONCEPTO_CC_ID, FOLIO, NATURALEZA_CONCEPTO,
			   SUCURSAL_ID, FECHA, CLIENTE_ID, CLAVE_CLIENTE,
			   TIPO_CAMBIO, DESCRIPCION,
			   SISTEMA_ORIGEN, APLICADO, ESTATUS, ESTATUS_ANT,
			   CONTABILIZADO_GYP, ES_CFD, TIENE_ANTICIPO, CFDI_CERTIFICADO, ENVIADO,
			   INTEG_BA, CONTABILIZADO_BA, CANCELADO)
			VALUES (?, 87327, ?, 'C',
			        225490, ?, ?, '0001',
			        1, 'Cargo prueba pago_writer E2E',
			        'CC', 'N', 'N', 'N',
			        'N', 'N', 'N', 'N', 'N',
			        'N', 'N', 'N')`,
			cargo.doctoCCID,
			folio,
			now,
			clienteID,
		)
		if err != nil {
			return err
		}

		// Claim an IMPTE_DOCTO_CC_ID for the cargo importe line.
		if err := q.QueryRowContext(ctx,
			`SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`,
		).Scan(&cargo.importeRowID); err != nil {
			return err
		}

		// Register cleanup for the IMPORTES_DOCTOS_CC row immediately.
		t.Cleanup(func() {
			cleanupQ := firebird.GetQuerier(context.Background(), pool.DB)
			_, _ = cleanupQ.ExecContext(context.Background(),
				`DELETE FROM IMPORTES_DOCTOS_CC WHERE IMPTE_DOCTO_CC_ID = ?`, cargo.importeRowID)
		})

		// APLICADO must be 'S' so SALDO_CARGO_CC can see this importe
		// when computing the available cargo balance. SALDO_CARGO_CC filters
		// on APLICADO='S', so a cargo importe with APLICADO='N' returns saldo
		// 0 and the pago writer's abono INSERT raises EX_SALDO_CARGO_EXCEDIDO.
		_, err = q.ExecContext(ctx,
			`INSERT INTO IMPORTES_DOCTOS_CC
			  (IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, FECHA,
			   TIPO_IMPTE, DOCTO_CC_ACR_ID,
			   IMPORTE, IMPUESTO,
			   APLICADO, ESTATUS, CANCELADO)
			VALUES (?, ?, ?,
			        'C', NULL,
			        ?, 0,
			        'S', 'N', 'N')`,
			cargo.importeRowID, cargo.doctoCCID, now, importe,
		)
		return err
	})
	require.NoError(t, err, "seedCargo: committed cargo + importe")

	return cargo
}

// ptr is a convenience helper that returns a pointer to the given string.
func ptr(s string) *string { return &s }

// ─── E2E harness ─────────────────────────────────────────────────────────────

// e2eHarness holds shared infrastructure for PagoWriter E2E tests.
type e2eHarness struct {
	pool   *firebird.Pool
	txMgr  *firebird.TxManager
	writer *microsip.PagoWriter
}

func newE2EHarness(t *testing.T) *e2eHarness {
	t.Helper()
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	return &e2eHarness{
		pool:   pool,
		txMgr:  firebird.NewTxManager(pool.DB),
		writer: microsip.NewPagoWriter(pool),
	}
}

// registerAplicarCleanup registers t.Cleanup for the 3 rows produced by a
// successful Aplicar call (FORMAS_COBRO_DOCTOS → IMPORTES_DOCTOS_CC →
// DOCTOS_CC, children first for FK constraints).
func (h *e2eHarness) registerAplicarCleanup(
	t *testing.T,
	result outbound.MicrosipPagoResult,
	cargoDoctoCCID int,
) {
	t.Helper()
	t.Cleanup(func() {
		q := firebird.GetQuerier(context.Background(), h.pool.DB)
		// 1. FORMAS_COBRO_DOCTOS (child of DOCTOS_CC abono)
		_, _ = q.ExecContext(context.Background(),
			`DELETE FROM FORMAS_COBRO_DOCTOS WHERE DOCTO_ID = ? AND NOM_TABLA_DOCTOS = 'DOCTOS_CC'`,
			result.DoctoCCID,
		)
		// 2. IMPORTES_DOCTOS_CC — the abono importe line (DOCTO_CC_ACR_ID = cargo)
		_, _ = q.ExecContext(context.Background(),
			`DELETE FROM IMPORTES_DOCTOS_CC WHERE IMPTE_DOCTO_CC_ID = ?`,
			result.ImpteDoctoCCID,
		)
		// 3. DOCTOS_CC abono header
		_, _ = q.ExecContext(context.Background(),
			`DELETE FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`,
			result.DoctoCCID,
		)
	})
}

// applyInsideTx runs writer.Aplicar inside a committed transaction so the
// rows become visible for subsequent SELECTs.
func (h *e2eHarness) applyInsideTx(
	t *testing.T,
	in outbound.MicrosipPagoInput,
) outbound.MicrosipPagoResult {
	t.Helper()
	var result outbound.MicrosipPagoResult
	err := h.txMgr.RunInTx(context.Background(), func(ctx context.Context) error {
		var e error
		result, e = h.writer.Aplicar(ctx, in)
		return e
	})
	require.NoError(t, err, "Aplicar must succeed")
	return result
}

// ─── Tests ───────────────────────────────────────────────────────────────────

// TestE2E_PagoWriter_Aplicar_HappyPath verifies the full 5-statement Microsip
// materialization: one DOCTOS_CC abono header, one IMPORTES_DOCTOS_CC line,
// and one FORMAS_COBRO_DOCTOS row are inserted with consistent IDs.
//
//nolint:paralleltest // commits real txns; cleanup uses t.Cleanup — safe but not parallel.
func TestE2E_PagoWriter_Aplicar_HappyPath(t *testing.T) {
	h := newE2EHarness(t)
	ctx := context.Background()

	cargo := seedCargo(t, ctx, h.pool, h.txMgr, 11486, decimal.NewFromInt(1000))

	in := outbound.MicrosipPagoInput{
		CargoDoctoCCID: cargo.doctoCCID,
		ClienteID:      testClienteID,
		CobradorID:     testCobradorID,
		Cobrador:       "Ramírez García, Jorge",
		Importe:        decimal.NewFromInt(500),
		FormaCobroID:   testFormaCobroID,
		ConceptoCCID:   testConceptoCCID,
		FechaHoraPago:  time.Now().UTC(),
	}

	result := h.applyInsideTx(t, in)
	h.registerAplicarCleanup(t, result, cargo.doctoCCID)

	// Basic result assertions.
	assert.Positive(t, result.DoctoCCID, "DoctoCCID must be positive")
	assert.Positive(t, result.ImpteDoctoCCID, "ImpteDoctoCCID must be positive")
	assert.Equal(t, "Z", result.Folio[:1], "Folio must start with 'Z'")

	q := firebird.GetQuerier(ctx, h.pool.DB)

	// 1. DOCTOS_CC abono header must exist.
	var doctoCCCount int
	require.NoError(t,
		q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`,
			result.DoctoCCID,
		).Scan(&doctoCCCount),
	)
	assert.Equal(t, 1, doctoCCCount, "DOCTOS_CC must have exactly 1 abono row")

	// 2. IMPORTES_DOCTOS_CC abono line — DOCTO_CC_ACR_ID must point to the cargo.
	var importeStr string
	require.NoError(t,
		q.QueryRowContext(ctx,
			`SELECT CAST(IMPORTE AS VARCHAR(30)) FROM IMPORTES_DOCTOS_CC
			 WHERE IMPTE_DOCTO_CC_ID = ? AND DOCTO_CC_ACR_ID = ? AND TIPO_IMPTE = 'R'`,
			result.ImpteDoctoCCID,
			cargo.doctoCCID,
		).Scan(&importeStr),
	)
	storedImporte, parseErr := decimal.NewFromString(importeStr)
	require.NoError(t, parseErr, "IMPORTE must parse as decimal")
	assert.True(t, decimal.NewFromInt(500).Equal(storedImporte), "IMPORTE must be 500, got %s", importeStr)

	// 3. FORMAS_COBRO_DOCTOS row links abono to forma_cobro_id.
	var formaCobro int
	require.NoError(t,
		q.QueryRowContext(ctx,
			`SELECT FORMA_COBRO_ID FROM FORMAS_COBRO_DOCTOS
			 WHERE DOCTO_ID = ? AND NOM_TABLA_DOCTOS = 'DOCTOS_CC'`,
			result.DoctoCCID,
		).Scan(&formaCobro),
	)
	assert.Equal(t, testFormaCobroID, formaCobro, "FORMA_COBRO_ID must match input")
}

// TestE2E_PagoWriter_Aplicar_DecimalPrecision asserts that an importe of
// 123.45 is stored bit-exact in IMPORTES_DOCTOS_CC.IMPORTE.
//
//nolint:paralleltest // commits real txns; cleanup uses t.Cleanup.
func TestE2E_PagoWriter_Aplicar_DecimalPrecision(t *testing.T) {
	h := newE2EHarness(t)
	ctx := context.Background()

	cargo := seedCargo(t, ctx, h.pool, h.txMgr, 11486, decimal.NewFromInt(1000))

	importe := decimal.NewFromFloat(123.45)
	in := outbound.MicrosipPagoInput{
		CargoDoctoCCID: cargo.doctoCCID,
		ClienteID:      testClienteID,
		CobradorID:     testCobradorID,
		Cobrador:       "Ramírez García, Jorge",
		Importe:        importe,
		FormaCobroID:   testFormaCobroID,
		ConceptoCCID:   testConceptoCCID,
		FechaHoraPago:  time.Now().UTC(),
	}

	result := h.applyInsideTx(t, in)
	h.registerAplicarCleanup(t, result, cargo.doctoCCID)

	q := firebird.GetQuerier(ctx, h.pool.DB)
	var storedStr string
	require.NoError(t,
		q.QueryRowContext(ctx,
			`SELECT CAST(IMPORTE AS VARCHAR(30)) FROM IMPORTES_DOCTOS_CC
			 WHERE DOCTO_CC_ACR_ID = ? AND TIPO_IMPTE = 'R'`,
			cargo.doctoCCID,
		).Scan(&storedStr),
	)
	stored, parseErr := decimal.NewFromString(storedStr)
	require.NoError(t, parseErr, "IMPORTE must parse as decimal")
	assert.True(t, importe.Equal(stored),
		"IMPORTE must be stored bit-exact: want %s got %s", importe.String(), stored.String())
}

// TestE2E_PagoWriter_Aplicar_ClaveClientePersisted verifies that
// fetchClaveCliente resolves CLAVE_CLIENTE from CLAVES_CLIENTES and the writer
// persists it to DOCTOS_CC.CLAVE_CLIENTE. In MUEBLERA.FDB every CLIENTES row
// has a corresponding CLAVES_CLIENTES row, so this test exercises the happy
// path of fetchClaveCliente (non-empty clave).
//
// The ErrNoRows → "" silent fallback in fetchClaveCliente is a unit concern
// (no external DB dependency required to test it).
//
//nolint:paralleltest // commits real txns; cleanup uses t.Cleanup.
func TestE2E_PagoWriter_Aplicar_ClaveClientePersisted(t *testing.T) {
	h := newE2EHarness(t)
	ctx := context.Background()

	cargo := seedCargo(t, ctx, h.pool, h.txMgr, testClienteID, decimal.NewFromInt(200))

	in := outbound.MicrosipPagoInput{
		CargoDoctoCCID: cargo.doctoCCID,
		ClienteID:      testClienteID,
		CobradorID:     testCobradorID,
		Cobrador:       "Ramírez García, Jorge",
		Importe:        decimal.NewFromInt(100),
		FormaCobroID:   testFormaCobroID,
		ConceptoCCID:   testConceptoCCID,
		FechaHoraPago:  time.Now().UTC(),
	}

	result := h.applyInsideTx(t, in)
	h.registerAplicarCleanup(t, result, cargo.doctoCCID)

	assert.Positive(t, result.DoctoCCID, "Aplicar must succeed")

	// Verify CLAVE_CLIENTE was persisted from CLAVES_CLIENTES — it must be a
	// non-NULL value because testClienteID has a CLAVES_CLIENTES row.
	q := firebird.GetQuerier(ctx, h.pool.DB)
	var claveCliente sql.NullString
	require.NoError(t,
		q.QueryRowContext(ctx,
			`SELECT CLAVE_CLIENTE FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`,
			result.DoctoCCID,
		).Scan(&claveCliente),
	)
	assert.True(t, claveCliente.Valid, "CLAVE_CLIENTE must be populated from CLAVES_CLIENTES")
	assert.NotEmpty(t, claveCliente.String, "CLAVE_CLIENTE must be a non-empty string")
}

// TestE2E_PagoWriter_Aplicar_NullableLatLon verifies that nil Lat/Lon are
// stored as NULL in DOCTOS_CC.LAT and DOCTOS_CC.LON.
//
//nolint:paralleltest // commits real txns; cleanup uses t.Cleanup.
func TestE2E_PagoWriter_Aplicar_NullableLatLon(t *testing.T) {
	h := newE2EHarness(t)
	ctx := context.Background()

	cargo := seedCargo(t, ctx, h.pool, h.txMgr, 11486, decimal.NewFromInt(300))

	in := outbound.MicrosipPagoInput{
		CargoDoctoCCID: cargo.doctoCCID,
		ClienteID:      testClienteID,
		CobradorID:     testCobradorID,
		Cobrador:       "Ramírez García, Jorge",
		Importe:        decimal.NewFromInt(150),
		FormaCobroID:   testFormaCobroID,
		ConceptoCCID:   testConceptoCCID,
		FechaHoraPago:  time.Now().UTC(),
		Lat:            nil,
		Lon:            nil,
	}

	result := h.applyInsideTx(t, in)
	h.registerAplicarCleanup(t, result, cargo.doctoCCID)

	q := firebird.GetQuerier(ctx, h.pool.DB)
	var lat, lon sql.NullString
	require.NoError(t,
		q.QueryRowContext(ctx,
			`SELECT LAT, LON FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`,
			result.DoctoCCID,
		).Scan(&lat, &lon),
	)
	assert.False(t, lat.Valid, "LAT must be NULL when Lat=nil")
	assert.False(t, lon.Valid, "LON must be NULL when Lon=nil")
}

// TestE2E_PagoWriter_Aplicar_LatLonProvided verifies that non-nil Lat/Lon are
// persisted exactly as supplied.
//
//nolint:paralleltest // commits real txns; cleanup uses t.Cleanup.
func TestE2E_PagoWriter_Aplicar_LatLonProvided(t *testing.T) {
	h := newE2EHarness(t)
	ctx := context.Background()

	cargo := seedCargo(t, ctx, h.pool, h.txMgr, 11486, decimal.NewFromInt(400))

	const wantLat = "23.123456"
	const wantLon = "-104.654321"

	in := outbound.MicrosipPagoInput{
		CargoDoctoCCID: cargo.doctoCCID,
		ClienteID:      testClienteID,
		CobradorID:     testCobradorID,
		Cobrador:       "Ramírez García, Jorge",
		Importe:        decimal.NewFromInt(200),
		FormaCobroID:   testFormaCobroID,
		ConceptoCCID:   testConceptoCCID,
		FechaHoraPago:  time.Now().UTC(),
		Lat:            ptr(wantLat),
		Lon:            ptr(wantLon),
	}

	result := h.applyInsideTx(t, in)
	h.registerAplicarCleanup(t, result, cargo.doctoCCID)

	q := firebird.GetQuerier(ctx, h.pool.DB)
	var lat, lon sql.NullString
	require.NoError(t,
		q.QueryRowContext(ctx,
			`SELECT LAT, LON FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`,
			result.DoctoCCID,
		).Scan(&lat, &lon),
	)
	assert.True(t, lat.Valid, "LAT must not be NULL")
	assert.True(t, lon.Valid, "LON must not be NULL")
	assert.Equal(t, wantLat, lat.String, "LAT must be persisted exactly")
	assert.Equal(t, wantLon, lon.String, "LON must be persisted exactly")
}

// TestE2E_PagoWriter_Aplicar_FolioFormat runs two Aplicars and asserts that
// both folios start with "Z" and are distinct. GEN_FOLIO_TEMP guarantees
// uniqueness on the Microsip side; this test only validates the "Z" prefix
// added by the writer.
//
//nolint:paralleltest // commits real txns; cleanup uses t.Cleanup.
func TestE2E_PagoWriter_Aplicar_FolioFormat(t *testing.T) {
	h := newE2EHarness(t)
	ctx := context.Background()

	cargo1 := seedCargo(t, ctx, h.pool, h.txMgr, 11486, decimal.NewFromInt(600))
	cargo2 := seedCargo(t, ctx, h.pool, h.txMgr, 11486, decimal.NewFromInt(600))

	makeInput := func(cargoDoctoCCID int) outbound.MicrosipPagoInput {
		return outbound.MicrosipPagoInput{
			CargoDoctoCCID: cargoDoctoCCID,
			ClienteID:      testClienteID,
			CobradorID:     testCobradorID,
			Cobrador:       "Ramírez García, Jorge",
			Importe:        decimal.NewFromInt(300),
			FormaCobroID:   testFormaCobroID,
			ConceptoCCID:   testConceptoCCID,
			FechaHoraPago:  time.Now().UTC(),
		}
	}

	result1 := h.applyInsideTx(t, makeInput(cargo1.doctoCCID))
	h.registerAplicarCleanup(t, result1, cargo1.doctoCCID)

	result2 := h.applyInsideTx(t, makeInput(cargo2.doctoCCID))
	h.registerAplicarCleanup(t, result2, cargo2.doctoCCID)

	assert.Equal(t, "Z", result1.Folio[:1], "folio 1 must start with 'Z'")
	assert.Equal(t, "Z", result2.Folio[:1], "folio 2 must start with 'Z'")
	assert.NotEqual(t, result1.Folio, result2.Folio, "GEN_FOLIO_TEMP must generate distinct folios")
}

// TestE2E_PagoWriter_Aplicar_ConceptoMapping verifies that the CONCEPTO_CC_ID
// passed in MicrosipPagoInput is persisted verbatim in DOCTOS_CC. The mapping
// from FormaCobroID → ConceptoCCID is performed by the app layer; the writer
// trusts the input. Two runs with different ConceptoCCIDs assert independence.
//
//nolint:paralleltest // commits real txns; cleanup uses t.Cleanup.
func TestE2E_PagoWriter_Aplicar_ConceptoMapping(t *testing.T) {
	h := newE2EHarness(t)
	ctx := context.Background()
	q := firebird.GetQuerier(ctx, h.pool.DB)

	cases := []struct {
		name         string
		conceptoCCID int
	}{
		{name: "ruta", conceptoCCID: 87327},
		{name: "mostrador", conceptoCCID: 27969},
	}

	for _, tc := range cases {
		cargo := seedCargo(t, ctx, h.pool, h.txMgr, 11486, decimal.NewFromInt(500))

		in := outbound.MicrosipPagoInput{
			CargoDoctoCCID: cargo.doctoCCID,
			ClienteID:      testClienteID,
			CobradorID:     testCobradorID,
			Cobrador:       "Ramírez García, Jorge",
			Importe:        decimal.NewFromInt(250),
			FormaCobroID:   testFormaCobroID,
			ConceptoCCID:   tc.conceptoCCID,
			FechaHoraPago:  time.Now().UTC(),
		}

		result := h.applyInsideTx(t, in)
		h.registerAplicarCleanup(t, result, cargo.doctoCCID)

		var storedConcepto int
		require.NoError(t,
			q.QueryRowContext(ctx,
				`SELECT CONCEPTO_CC_ID FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`,
				result.DoctoCCID,
			).Scan(&storedConcepto),
			"caso %s: SELECT CONCEPTO_CC_ID", tc.name,
		)
		assert.Equal(t, tc.conceptoCCID, storedConcepto,
			"caso %s: CONCEPTO_CC_ID must match input", tc.name)
	}
}
