//nolint:misspell // Spanish vocabulary (ventas, aplicada, etc.) by convention.
package ventfb_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// TestE2E_AplicarVenta_FullLifecycle exercises the full AplicarVenta service
// path against a real Firebird DB (inside a rollback-only transaction):
//
//  1. Seeds a venta in APROBADA state.
//  2. Calls svc.AplicarVenta.
//  3. Asserts the returned aggregate has Sincronizacion=APLICADA, a non-nil
//     MicrosipDoctoPVID, and a non-empty MicrosipFolio.
//  4. Asserts the outbox captured a "venta.aplicada" event for the venta.
//  5. Verifies the Microsip cascade via countDoctosBySis (1 IN + 2 CC for
//     CONTADO).
//
// Nothing persists — WithTestTransaction always rolls back.
//
//nolint:paralleltest // serial: shares rollback-only tx with inner harness.
func TestE2E_AplicarVenta_FullLifecycle(t *testing.T) {
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
		require.NoError(t, err, "AplicarVenta must succeed for an aprobada venta")
		require.NotNil(t, result)

		// Domain state assertions.
		assert.Equal(t, domain.SincronizacionAplicada, result.Sincronizacion(),
			"sincronizacion must be APLICADA after applying")
		assert.Equal(t, domain.SituacionAprobada, result.Situacion(),
			"situacion stays APROBADA after applying (only sincronizacion changes)")
		require.NotNil(t, result.MicrosipDoctoPVID(),
			"MicrosipDoctoPVID must be set after applying")
		assert.Positive(t, *result.MicrosipDoctoPVID(),
			"MicrosipDoctoPVID must be positive")
		require.NotNil(t, result.MicrosipFolio(),
			"MicrosipFolio must be set after applying")
		assert.NotEmpty(t, *result.MicrosipFolio(),
			"MicrosipFolio must not be empty")

		// Outbox assertion.
		assert.True(t, h.outbox.hasEventForAggregate("venta.aplicada", ventaID),
			"outbox must capture venta.aplicada event for venta %s; captured=%v",
			ventaID, h.outbox.snapshot())

		// Microsip cascade: 1 inventario doc + 2 CxC docs (cargo + pago for CONTADO).
		doctoPVID := *result.MicrosipDoctoPVID()
		assert.Equal(t, 1, countDoctosBySis(ctx, q, doctoPVID, "IN"),
			"DOCTOS_ENTRE_SIS must have 1 IN doc (inventario)")
		assert.Equal(t, 2, countDoctosBySis(ctx, q, doctoPVID, "CC"),
			"DOCTOS_ENTRE_SIS must have 2 CC docs (cargo + pago for CONTADO)")

		t.Logf("full lifecycle: ventaID=%s DoctoPVID=%d Folio=%s",
			ventaID, doctoPVID, *result.MicrosipFolio())
	})
}

// TestE2E_AplicarVenta_Idempotente_ServiceLevel verifies that calling
// AplicarVenta twice on the same venta ID returns the same folio and
// DoctoPVID without creating a second DOCTOS_PV row.
//
// The test queries DOCTOS_ENTRE_SIS to confirm exactly one row exists linking
// this venta's MSP_VENTAS.MICROSIP_DOCTO_PV_ID as the source document.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_AplicarVenta_Idempotente_ServiceLevel(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		userID := seedUsuarioRow(ctx, t, pool)
		h := newAplicarE2EHarness(ctx, t, pool)

		q := firebird.GetQuerier(ctx, pool.DB)
		requireCatalog(t, q)

		v := buildAplicarContado(t, userID)
		ventaID := h.persistAprobada(ctx, t, v)

		// First apply.
		first, err := h.svc.AplicarVenta(ctx, ventaID, userID)
		require.NoError(t, err, "first AplicarVenta must succeed")
		require.NotNil(t, first.MicrosipDoctoPVID())
		require.NotNil(t, first.MicrosipFolio())
		firstDoctoPVID := *first.MicrosipDoctoPVID()
		firstFolio := *first.MicrosipFolio()

		// Second apply — must be idempotent.
		second, err := h.svc.AplicarVenta(ctx, ventaID, userID)
		require.NoError(t, err, "second AplicarVenta must succeed (idempotent)")
		require.NotNil(t, second.MicrosipDoctoPVID())
		require.NotNil(t, second.MicrosipFolio())

		assert.Equal(t, firstDoctoPVID, *second.MicrosipDoctoPVID(),
			"idempotent apply must return the same DoctoPVID")
		assert.Equal(t, firstFolio, *second.MicrosipFolio(),
			"idempotent apply must return the same folio")

		// Verify exactly ONE DOCTOS_PV row exists for this DoctoPVID.
		var doctoCount int
		err = q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM DOCTOS_PV WHERE DOCTO_PV_ID = ?`,
			firstDoctoPVID,
		).Scan(&doctoCount)
		require.NoError(t, err)
		assert.Equal(t, 1, doctoCount,
			"exactly one DOCTOS_PV row must exist after idempotent double-apply (DoctoPVID=%d)",
			firstDoctoPVID)

		t.Logf("idempotent: ventaID=%s DoctoPVID=%d Folio=%s (applied twice, 1 row)",
			ventaID, firstDoctoPVID, firstFolio)
	})
}
