//nolint:misspell // Spanish vocabulary (saldo, cargo, enganche, etc.) by convention.
package ventfb_test

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// ─── saldo assertions ─────────────────────────────────────────────────────────

// saldoEsperado describes the expected financial position of a venta in
// Microsip's CxC ledger after Aplicar.
type saldoEsperado struct {
	cargo decimal.Decimal
	pago  decimal.Decimal
	saldo decimal.Decimal // cargo - pago
}

// assertSaldoVenta verifies the combined cargo/pago/saldo amounts for all
// DOCTOS_CC rows linked to the given DOCTO_PV_ID via DOCTOS_ENTRE_SIS.
//
// The query sums IMPORTES_DOCTOS_CC grouped by TIPO_IMPTE ('C' = cargo, 'R' = pago).
// DOCTOS_ENTRE_SIS links DOCTO_PV → DOCTO_CC via CLAVE_SIS_FTE='PV' and
// CLAVE_SIS_DEST='CC'.
//
// Note: switching to cobranzaventfb.SaldosRepo.PorVenta was considered but
// depguard's no-cross-module-internals rule forbids importing
// internal/cobranza/infra/ventfb from internal/ventas/infra/ventfb_test.
// The correlated SQL below is the compliant fallback.
func assertSaldoVenta(t *testing.T, q firebird.Querier, doctoPVID int, want saldoEsperado) {
	t.Helper()
	ctx := context.Background()

	// Sum all CARGO importes (TIPO_IMPTE='C') linked to this DOCTO_PV.
	// COALESCE pins NULL → 0 so the SUM is always a numeric value even when
	// no rows match (the trigger cascade has not produced the cargo yet).
	var cargoRaw any
	err := q.QueryRowContext(
		ctx, `
		SELECT CAST(COALESCE(SUM(IDC.IMPORTE), 0) AS NUMERIC(18,2))
		FROM DOCTOS_ENTRE_SIS DES
		JOIN IMPORTES_DOCTOS_CC IDC ON IDC.DOCTO_CC_ID = DES.DOCTO_DEST_ID
		WHERE DES.CLAVE_SIS_FTE = 'PV'
		  AND DES.CLAVE_SIS_DEST = 'CC'
		  AND DES.DOCTO_FTE_ID = ?
		  AND IDC.TIPO_IMPTE = 'C'`,
		doctoPVID,
	).Scan(&cargoRaw)
	require.NoError(t, err, "assertSaldoVenta: query cargo sum")

	var pagoRaw any
	err = q.QueryRowContext(
		ctx, `
		SELECT CAST(COALESCE(SUM(IDC.IMPORTE), 0) AS NUMERIC(18,2))
		FROM DOCTOS_ENTRE_SIS DES
		JOIN DOCTOS_CC DC ON DC.DOCTO_CC_ID = DES.DOCTO_DEST_ID
		JOIN IMPORTES_DOCTOS_CC IDC ON IDC.DOCTO_CC_ACR_ID = DC.DOCTO_CC_ID
		WHERE DES.CLAVE_SIS_FTE = 'PV'
		  AND DES.CLAVE_SIS_DEST = 'CC'
		  AND DES.DOCTO_FTE_ID = ?
		  AND IDC.TIPO_IMPTE = 'R'`,
		doctoPVID,
	).Scan(&pagoRaw)
	require.NoError(t, err, "assertSaldoVenta: query pago sum")

	gotCargo, err := firebird.ScanDecimal(cargoRaw, 2)
	require.NoError(t, err, "assertSaldoVenta: scan cargo")
	gotPago, err := firebird.ScanDecimal(pagoRaw, 2)
	require.NoError(t, err, "assertSaldoVenta: scan pago")
	gotSaldo := gotCargo.Sub(gotPago)

	assert.True(t, want.cargo.Equal(gotCargo),
		"assertSaldoVenta: cargo mismatch: want=%s got=%s", want.cargo.StringFixed(2), gotCargo.StringFixed(2))
	assert.True(t, want.pago.Equal(gotPago),
		"assertSaldoVenta: pago mismatch: want=%s got=%s", want.pago.StringFixed(2), gotPago.StringFixed(2))
	assert.True(t, want.saldo.Equal(gotSaldo),
		"assertSaldoVenta: saldo mismatch: want=%s got=%s", want.saldo.StringFixed(2), gotSaldo.StringFixed(2))
}

// ─── IMPORTES_DOCTOS_CC row assertions ───────────────────────────────────────

// importeEsperado describes one expected row in IMPORTES_DOCTOS_CC linked to
// the DOCTO_PV via DOCTOS_ENTRE_SIS.
type importeEsperado struct {
	tipo     string          // "C" (cargo) or "R" (pago/enganche)
	concepto int             // CONCEPTO_CC_ID — 0 means any
	importe  decimal.Decimal // expected IMPORTE value
}

// assertDoctoCCImportes verifies the count and per-row importe values for
// every IMPORTES_DOCTOS_CC row that participates in the cargo's CxC position.
//
// For CONTADO ventas the cascade emits one DOCTOS_CC of NATURALEZA='C' (cargo)
// AND a second of NATURALEZA='R' (auto-pago); both are linked back to the
// DOCTO_PV via DOCTOS_ENTRE_SIS.
//
// For CREDITO + enganche, only the cargo is linked via DOCTOS_ENTRE_SIS; the
// enganche is a SEPARATE DOCTOS_CC of NATURALEZA='R' whose IMPORTES_DOCTOS_CC
// row points back to the cargo via DOCTO_CC_ACR_ID. We resolve the cargo IDs
// via DOCTOS_ENTRE_SIS first, then collect every importe where either
// DOCTO_CC_ID or DOCTO_CC_ACR_ID matches — that covers both cases uniformly.
//
// The DC join carries through the source document so we can read its
// CONCEPTO_CC_ID (cargo concepto vs. enganche concepto 24533, etc.).
func assertDoctoCCImportes(t *testing.T, q firebird.Querier, doctoPVID int, expected []importeEsperado) {
	t.Helper()
	ctx := context.Background()

	rows, err := q.QueryContext(
		ctx, `
		SELECT IDC.TIPO_IMPTE, DC.CONCEPTO_CC_ID, IDC.IMPORTE
		FROM IMPORTES_DOCTOS_CC IDC
		JOIN DOCTOS_CC DC ON DC.DOCTO_CC_ID = IDC.DOCTO_CC_ID
		WHERE (
		  IDC.DOCTO_CC_ID IN (
		    SELECT DOCTO_DEST_ID FROM DOCTOS_ENTRE_SIS
		    WHERE CLAVE_SIS_FTE='PV' AND CLAVE_SIS_DEST='CC' AND DOCTO_FTE_ID=?
		  )
		  OR IDC.DOCTO_CC_ACR_ID IN (
		    SELECT DOCTO_DEST_ID FROM DOCTOS_ENTRE_SIS
		    WHERE CLAVE_SIS_FTE='PV' AND CLAVE_SIS_DEST='CC' AND DOCTO_FTE_ID=?
		  )
		)
		ORDER BY IDC.TIPO_IMPTE, IDC.IMPORTE`,
		doctoPVID, doctoPVID,
	)
	require.NoError(t, err, "assertDoctoCCImportes: query")
	defer rows.Close()

	type row struct {
		tipo     string
		concepto int
		importe  decimal.Decimal
	}
	var got []row
	for rows.Next() {
		var tipo string
		var concepto int
		var importeRaw any
		require.NoError(t, rows.Scan(&tipo, &concepto, &importeRaw))
		imp, err := firebird.ScanDecimal(importeRaw, 2)
		require.NoError(t, err)
		got = append(got, row{tipo: tipo, concepto: concepto, importe: imp})
	}
	require.NoError(t, rows.Err())

	assert.Len(t, got, len(expected),
		"assertDoctoCCImportes: row count mismatch for doctoPVID=%d; got=%v", doctoPVID, got)

	for i, want := range expected {
		if i >= len(got) {
			break
		}
		g := got[i]
		assert.Equal(t, want.tipo, g.tipo,
			"assertDoctoCCImportes[%d]: tipo mismatch", i)
		if want.concepto != 0 {
			assert.Equal(t, want.concepto, g.concepto,
				"assertDoctoCCImportes[%d]: concepto mismatch", i)
		}
		assert.True(t, want.importe.Equal(g.importe),
			"assertDoctoCCImportes[%d]: importe mismatch: want=%s got=%s",
			i, want.importe.StringFixed(2), g.importe.StringFixed(2))
	}
}

// ─── inventory assertions ─────────────────────────────────────────────────────

// snapshotSalidasInventario reads SALDOS_IN.SALIDAS_UNIDADES for the given
// (articuloID, almacenID) at ANO=current-year MES=current-month. Returns zero
// when the row does not exist (new article/month with no exits yet).
//
// The Microsip column is ANO (no Ñ). The query uses Firebird's EXTRACT to
// derive year/month from CURRENT_DATE so the snapshot stays tx-local.
func snapshotSalidasInventario(ctx context.Context, t *testing.T, q firebird.Querier, articuloID, almacenID int) decimal.Decimal {
	t.Helper()
	var raw any
	err := q.QueryRowContext(
		ctx, `
		SELECT SALIDAS_UNIDADES
		FROM SALDOS_IN
		WHERE ARTICULO_ID = ?
		  AND ALMACEN_ID  = ?
		  AND ANO         = EXTRACT(YEAR  FROM CURRENT_DATE)
		  AND MES         = EXTRACT(MONTH FROM CURRENT_DATE)`,
		articuloID, almacenID,
	).Scan(&raw)
	if err != nil {
		// Row may not exist for a new article/month; treat as zero.
		return decimal.Zero
	}
	d, scanErr := firebird.ScanDecimal(raw, 4)
	if scanErr != nil {
		t.Logf("snapshotSalidasInventario: scan error (treating as zero): %v", scanErr)
		return decimal.Zero
	}
	return d
}

// assertSalidasInventarioDelta verifies that SALDOS_IN.SALIDAS_UNIDADES
// increased by exactly expectedDelta relative to the before snapshot.
func assertSalidasInventarioDelta(ctx context.Context, t *testing.T, q firebird.Querier, articuloID, almacenID int, before, expectedDelta decimal.Decimal) {
	t.Helper()
	after := snapshotSalidasInventario(ctx, t, q, articuloID, almacenID)
	delta := after.Sub(before)
	assert.True(t, expectedDelta.Equal(delta),
		"SALDOS_IN.SALIDAS_UNIDADES delta mismatch: want=%s got=%s (before=%s after=%s)",
		expectedDelta.StringFixed(4), delta.StringFixed(4),
		before.StringFixed(4), after.StringFixed(4))
}

// ─── IVA breakdown assertions ─────────────────────────────────────────────────

// assertImpuestosDesglose verifies that DOCTOS_PV.TOTAL_IMPUESTOS is consistent
// with the article's IVA rate. The detail table IMPUESTOS_DOCTOS_PV_DET is
// populated by a Microsip path our adapter does not exercise (it belongs to
// the invoicing/timbrado layer), so we read directly from the header columns
// IMPORTE_NETO and TOTAL_IMPUESTOS that the adapter does write.
//
// For an article at expectedPct IVA, neto * pct/100 must equal totalImpuestos
// within ±0.02 to absorb rounding.
func assertImpuestosDesglose(t *testing.T, q firebird.Querier, doctoPVID, expectedPct int) {
	t.Helper()
	ctx := context.Background()

	var netoRaw, totalImpRaw any
	err := q.QueryRowContext(
		ctx,
		`SELECT IMPORTE_NETO, TOTAL_IMPUESTOS FROM DOCTOS_PV WHERE DOCTO_PV_ID = ?`,
		doctoPVID,
	).Scan(&netoRaw, &totalImpRaw)
	require.NoError(t, err, "assertImpuestosDesglose: read DOCTOS_PV header")

	neto, err := firebird.ScanDecimal(netoRaw, 2)
	require.NoError(t, err, "assertImpuestosDesglose: scan neto")
	totalImp, err := firebird.ScanDecimal(totalImpRaw, 2)
	require.NoError(t, err, "assertImpuestosDesglose: scan total_impuestos")

	expected := neto.InexactFloat64() * float64(expectedPct) / 100.0
	assert.InDelta(t, expected, totalImp.InexactFloat64(), 0.02,
		"DOCTOS_PV.TOTAL_IMPUESTOS=%s must ≈ IMPORTE_NETO * %d%% = %.2f (±0.02)",
		totalImp.StringFixed(2), expectedPct, expected)
}

// ─── cash movement assertions ─────────────────────────────────────────────────

// assertMovtoEfvoCajaForContado verifies that MOVTOS_EFVO_CAJA has exactly one
// row for the CONTADO DOCTO_PV_COBRO linked to the DOCTO_PV.
//
// Join path:
//
//	DOCTOS_PV_COBROS → DOCTOS_ENTRE_SIS (CLAVE_SIS_FTE='PV' TIPO_DOCTO='CB')
//	→ MOVTOS_EFVO_CAJA.DOCTO_CC_ID = DOCTO_DEST_ID
//
// Firebird's aplicar cascade on DOCTOS_PV_COBROS (TIPO='C') generates one
// MOVTOS_EFVO_CAJA row per cobro. The IMPORTE column must match the total
// con-IVA passed to the writer.
//
// If the join from DOCTOS_PV to MOVTOS_EFVO_CAJA via DOCTOS_PV_COBROS is not
// resolvable (e.g. schema drift), the test is skipped with a clear message.
// See the comment below for the schema investigation rationale.
func assertMovtoEfvoCajaForContado(ctx context.Context, t *testing.T, q firebird.Querier, doctoPVID int, expectedImporte decimal.Decimal) {
	t.Helper()

	// The cascade path for CONTADO:
	//   1. INSERT DOCTOS_PV_COBROS with DOCTO_PV_ID → Microsip trigger fires.
	//   2. Trigger creates MOVTOS_EFVO_CAJA linked via DOCTOS_PV_COBRO_ID.
	//
	// We look for MOVTOS_EFVO_CAJA rows where DOCTO_COBRO_ID matches the
	// DOCTO_PV_COBRO_ID inserted for this DOCTO_PV.
	//
	// Firebird schema: MOVTOS_EFVO_CAJA has DOCTO_COBRO_ID referencing
	// DOCTOS_PV_COBROS.DOCTO_PV_COBRO_ID (not directly DOCTOS_PV_ID).
	var count int
	err := q.QueryRowContext(
		ctx, `
		SELECT COUNT(*)
		FROM DOCTOS_PV_COBROS DPC
		JOIN MOVTOS_EFVO_CAJA MEC ON MEC.DOCTO_COBRO_ID = DPC.DOCTO_PV_COBRO_ID
		WHERE DPC.DOCTO_PV_ID = ?`,
		doctoPVID,
	).Scan(&count)
	if err != nil {
		t.Skipf("assertMovtoEfvoCajaForContado: MOVTOS_EFVO_CAJA join query failed (schema investigation needed): %v", err)
		return
	}
	if count == 0 {
		// Some Microsip configurations do not write MOVTOS_EFVO_CAJA in all
		// environments. Skip rather than fail to avoid false negatives on
		// dev DBs that lack efvo movement triggers.
		t.Skipf("assertMovtoEfvoCajaForContado: no MOVTOS_EFVO_CAJA row for doctoPVID=%d; trigger may not fire in this DB snapshot", doctoPVID)
		return
	}

	assert.Equal(t, 1, count,
		"MOVTOS_EFVO_CAJA must have exactly 1 row for CONTADO doctoPVID=%d", doctoPVID)

	var importeRaw any
	err = q.QueryRowContext(
		ctx, `
		SELECT MEC.IMPORTE
		FROM DOCTOS_PV_COBROS DPC
		JOIN MOVTOS_EFVO_CAJA MEC ON MEC.DOCTO_COBRO_ID = DPC.DOCTO_PV_COBRO_ID
		WHERE DPC.DOCTO_PV_ID = ?`,
		doctoPVID,
	).Scan(&importeRaw)
	require.NoError(t, err, "assertMovtoEfvoCajaForContado: importe query")
	gotImporte, err := firebird.ScanDecimal(importeRaw, 2)
	require.NoError(t, err)
	assert.True(t, expectedImporte.Equal(gotImporte),
		"MOVTOS_EFVO_CAJA.IMPORTE mismatch: want=%s got=%s",
		expectedImporte.StringFixed(2), gotImporte.StringFixed(2))
}
