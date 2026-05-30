//nolint:misspell // Spanish vocabulary (inventario, saldo, enganche, etc.) by convention.
package ventfb_test

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// TestE2E_AplicarVenta_InventarioBaja verifies that applying a CONTADO venta
// (1 unit of testArticuloID from testAlmacenID) increments
// SALDOS_IN.SALIDAS_UNIDADES by exactly 1.0000 for that article/warehouse in
// the current month.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_AplicarVenta_InventarioBaja(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		userID := seedUsuarioRow(ctx, t, pool)
		h := newAplicarE2EHarness(ctx, t, pool)

		q := firebird.GetQuerier(ctx, pool.DB)
		requireCatalog(t, q)

		// Snapshot SALDOS_IN BEFORE applying.
		before := snapshotSalidasInventario(ctx, t, q, testArticuloID, testAlmacenID)

		v := buildAplicarContado(t, userID)
		ventaID := h.persistAprobada(ctx, t, v)

		_, err := h.svc.AplicarVenta(ctx, ventaID, userID)
		require.NoError(t, err)

		// The venta has 1.0000 units of testArticuloID from testAlmacenID.
		expectedDelta := decimal.RequireFromString("1.0000")
		assertSalidasInventarioDelta(ctx, t, q, testArticuloID, testAlmacenID, before, expectedDelta)

		t.Logf("inventario baja: SALDOS_IN before=%s delta=%s",
			before.StringFixed(4), expectedDelta.StringFixed(4))
	})
}

// TestE2E_AplicarVenta_SaldoVenta_ContadoNetoCero verifies that after applying
// a CONTADO venta the CxC position is zero (cargo = pago = 3500.00, saldo = 0).
//
// The test article (testArticuloID) is TASA 0%, so total = 3500.00 exactly
// (MontoSnapshot.Contado()). For CONTADO, Microsip creates:
//   - 1 DOCTOS_CC cargo of 3500.00
//   - 1 DOCTOS_CC pago of 3500.00 (immediate cash settlement)
//   - Net saldo = 0
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_AplicarVenta_SaldoVenta_ContadoNetoCero(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		userID := seedUsuarioRow(ctx, t, pool)
		h := newAplicarE2EHarness(ctx, t, pool)

		q := firebird.GetQuerier(ctx, pool.DB)
		requireCatalog(t, q)

		v := buildAplicarContado(t, userID)
		ventaID := h.persistAprobada(ctx, t, v)

		result, err := h.svc.AplicarVenta(ctx, ventaID, userID)
		require.NoError(t, err)
		require.NotNil(t, result.MicrosipDoctoPVID())

		// CONTADO unit price = MontoSnapshot.Contado() = 3500.00 (TASA 0% article).
		totalContado := decimal.RequireFromString("3500.00")
		assertSaldoVenta(t, q, *result.MicrosipDoctoPVID(), saldoEsperado{
			cargo: totalContado,
			pago:  totalContado,
			saldo: decimal.Zero,
		})

		t.Logf("saldo contado neto cero: DoctoPVID=%d cargo=pago=%s",
			*result.MicrosipDoctoPVID(), totalContado.StringFixed(2))
	})
}

// TestE2E_AplicarVenta_SaldoVenta_CreditoConEnganche verifies the financial
// position after applying a CREDITO venta with enganche.
//
// The test venta (buildAplicarCredito) has:
//   - Total (anual price) = 9100.00
//   - Enganche = 500.00
//   - Expected: cargo=9100.00, pago=500.00 (enganche), saldo=8600.00
//
// The enganche row in IMPORTES_DOCTOS_CC (TIPO='R', CONCEPTO_CC_ID=24533)
// is verified via assertDoctoCCImportes.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_AplicarVenta_SaldoVenta_CreditoConEnganche(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		userID := seedUsuarioRow(ctx, t, pool)
		h := newAplicarE2EHarness(ctx, t, pool)

		q := firebird.GetQuerier(ctx, pool.DB)
		requireCatalog(t, q)

		v := buildAplicarCredito(t, userID)
		ventaID := h.persistAprobada(ctx, t, v)

		result, err := h.svc.AplicarVenta(ctx, ventaID, userID)
		require.NoError(t, err)
		require.NotNil(t, result.MicrosipDoctoPVID())

		doctoPVID := *result.MicrosipDoctoPVID()

		// Total anual price = 9100.00; enganche = 500.00 → saldo = 8600.00.
		totalAnual := decimal.RequireFromString("9100.00")
		enganche := decimal.RequireFromString("500.00")
		saldo := decimal.RequireFromString("8600.00")

		// Verify CxC financial position.
		assertSaldoVenta(t, q, doctoPVID, saldoEsperado{
			cargo: totalAnual,
			pago:  enganche,
			saldo: saldo,
		})

		// Verify IMPORTES_DOCTOS_CC has exactly 1 cargo + 1 enganche payment row.
		// CONCEPTO_CC_ID=24533 for enganche (see insertDoctoCC in venta_writer.go).
		assertDoctoCCImportes(t, q, doctoPVID, []importeEsperado{
			{tipo: "C", concepto: 0, importe: totalAnual},   // cargo
			{tipo: "R", concepto: 24533, importe: enganche}, // enganche abono
		})

		t.Logf("credito con enganche: DoctoPVID=%d cargo=%s enganche=%s saldo=%s",
			doctoPVID, totalAnual.StringFixed(2), enganche.StringFixed(2), saldo.StringFixed(2))
	})
}

// TestE2E_AplicarVenta_ImpuestosDesglose verifies that applying a CONTADO venta
// using testArticuloID16Pct (16% IVA) populates IMPUESTOS_DOCTOS_PV_DET with
// PCTJE_IMPUESTO=16 and that the sum matches DOCTOS_PV.TOTAL_IMPUESTOS (±0.02).
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_AplicarVenta_ImpuestosDesglose(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		userID := seedUsuarioRow(ctx, t, pool)
		h := newAplicarE2EHarness(ctx, t, pool)

		q := firebird.GetQuerier(ctx, pool.DB)
		requireCatalog(t, q)

		// Use the 16% IVA article.
		v := buildAplicarContadoConArticulo(t, userID, testArticuloID16Pct, "Batería Cinsa 50 Aniv")
		ventaID := h.persistAprobada(ctx, t, v)

		result, err := h.svc.AplicarVenta(ctx, ventaID, userID)
		require.NoError(t, err)
		require.NotNil(t, result.MicrosipDoctoPVID())

		assertImpuestosDesglose(t, q, *result.MicrosipDoctoPVID(), 16)

		t.Logf("IVA desglose 16%%: DoctoPVID=%d", *result.MicrosipDoctoPVID())
	})
}

// TestE2E_AplicarVenta_MovtoEfvoCaja_Contado verifies that applying a CONTADO
// venta creates exactly one MOVTOS_EFVO_CAJA row with IMPORTE = total con IVA
// (3500.00 for the default TASA-0% test article).
//
// If the MOVTOS_EFVO_CAJA trigger is not active in the current DB snapshot or
// the join is not trivially resolvable, the assertion is skipped with a clear
// reason (assertMovtoEfvoCajaForContado handles the skip internally).
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_AplicarVenta_MovtoEfvoCaja_Contado(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		userID := seedUsuarioRow(ctx, t, pool)
		h := newAplicarE2EHarness(ctx, t, pool)

		q := firebird.GetQuerier(ctx, pool.DB)
		requireCatalog(t, q)

		v := buildAplicarContado(t, userID)
		ventaID := h.persistAprobada(ctx, t, v)

		result, err := h.svc.AplicarVenta(ctx, ventaID, userID)
		require.NoError(t, err)
		require.NotNil(t, result.MicrosipDoctoPVID())

		// TASA-0% article → total con IVA = Contado price = 3500.00.
		expectedImporte := decimal.RequireFromString("3500.00")
		assertMovtoEfvoCajaForContado(ctx, t, q, *result.MicrosipDoctoPVID(), expectedImporte)

		t.Logf("movto efvo caja contado: DoctoPVID=%d expectedImporte=%s",
			*result.MicrosipDoctoPVID(), expectedImporte.StringFixed(2))
	})
}
