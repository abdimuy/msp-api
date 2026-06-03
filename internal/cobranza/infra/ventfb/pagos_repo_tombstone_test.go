// Tests for the tombstone semantics introduced by migration 000019.
//
// Migration 000019 changed MSP_RECOMPUTE_PAGO so that cancelled pagos write a
// tombstone row (CANCELADO='S', IMPORTE=0, IMPUESTO=0) instead of performing
// a DELETE. The tests here verify:
//
//  1. A cancellation creates a tombstone row — not a DELETE.
//  2. Human-read methods (PorVenta, PorCliente, EnRutaPorZona) filter out
//     tombstones.
//  3. DeleteTombstonesOlderThan respects the cutoff and only removes stale
//     tombstones.
//
//nolint:misspell // Spanish vocabulary by project convention.
package ventfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// requireMigration000019 skips the test when the CANCELADO column does not
// exist on MSP_PAGOS_VENTAS (migration 000019 not applied).
func requireMigration000019(t *testing.T, q firebird.Querier) {
	t.Helper()
	var n int
	err := q.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM RDB$RELATION_FIELDS
		  WHERE RDB$RELATION_NAME = 'MSP_PAGOS_VENTAS'
		    AND RDB$FIELD_NAME    = 'CANCELADO'`,
	).Scan(&n)
	if err != nil || n == 0 {
		t.Skipf("migration 000019 not applied; skipping — run 'make fb-migrate-up'")
	}
}

// preseedSaldosCC adds `importe` to both SALDOS_CC.CARGOS_CXC and
// SALDOS_CC.CREDITOS_CXC for the current month so that cancel triggers can
// subtract without violating the >= 0 CHECK constraints. Cancelling a cargo
// (TIPO_IMPTE='C') decrements CARGOS_CXC; cancelling a pago (TIPO_IMPTE='R')
// decrements CREDITOS_CXC. Seeding both keeps the helper safe for either
// case. The UPDATE is inside the rollback-only tx and is reverted at the
// end of the test. If no SALDOS_CC row exists for the cliente this month,
// the test is skipped.
func preseedSaldosCC(t *testing.T, q firebird.Querier, clienteID int, importe decimal.Decimal) {
	t.Helper()
	ano := time.Now().Year()
	mes := int(time.Now().Month())
	res, err := q.ExecContext(context.Background(),
		`UPDATE SALDOS_CC SET CARGOS_CXC   = CARGOS_CXC   + ?,
		                       CREDITOS_CXC = CREDITOS_CXC + ?
		  WHERE CLIENTE_ID = ? AND ANO = ? AND MES = ?`,
		importe, importe, clienteID, ano, mes)
	require.NoError(t, err, "preseedSaldosCC")
	if n, _ := res.RowsAffected(); n == 0 {
		t.Skipf("no SALDOS_CC row for cliente %d ano=%d mes=%d — re-run after Microsip creates this month's row",
			clienteID, ano, mes)
	}
}

// TestE2E_Cobranza_Pagos_CancelacionTombstone verifies that cancelling an
// IMPORTES_DOCTOS_CC pago row writes a tombstone into MSP_PAGOS_VENTAS
// (CANCELADO='S', IMPORTE=0, IMPUESTO=0) rather than deleting the cache row.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Cobranza_Pagos_CancelacionTombstone(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)
		requireMigration000019(t, q)

		clienteID, _ := seedClienteID(t, q)

		cargoImporte := decimal.RequireFromString("2000.00")
		pagoImporte := decimal.RequireFromString("500.00")

		// Pre-seed SALDOS_CC so the cancel trigger's CHECK constraint is satisfied
		// for both the cargo and the pago subtraction.
		buffer := decimal.RequireFromString("3000.00")
		preseedSaldosCC(t, q, clienteID, buffer)

		cargoID := insertCargoDoctosCC(t, q, clienteID, "PAG-TMB-1", cargoImporte)
		impteID := insertPagoImporte(t, q, cargoID, pagoImporte)

		// Verify the pago cache row exists before cancellation.
		var existsBefore int
		err := q.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`,
			impteID).Scan(&existsBefore)
		require.NoError(t, err)
		if existsBefore == 0 {
			t.Skipf("trigger did not create cache row for pago %d — verify migration 000019", impteID)
		}

		// Cancel the importe — the trigger should write a tombstone, not DELETE.
		_, err = q.ExecContext(context.Background(),
			`UPDATE IMPORTES_DOCTOS_CC SET CANCELADO = 'S' WHERE IMPTE_DOCTO_CC_ID = ?`,
			impteID)
		require.NoError(t, err, "cancelar importe")

		// Verify the cache row STILL exists (not deleted) and carries tombstone values.
		var (
			cancelado   string
			importeRaw  any
			impuestoRaw any
		)
		err = q.QueryRowContext(context.Background(),
			`SELECT CANCELADO, IMPORTE, IMPUESTO FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`,
			impteID).Scan(&cancelado, &importeRaw, &impuestoRaw)
		require.NoError(t, err, "tombstone row must exist after cancellation")

		assert.Equal(t, "S", cancelado, "tombstone must have CANCELADO='S'")

		imp, err := firebird.ScanDecimal(importeRaw, 2)
		require.NoError(t, err)
		assert.True(t, imp.IsZero(), "tombstone IMPORTE must be zero, got %s", imp)

		impuesto, err := firebird.ScanDecimal(impuestoRaw, 2)
		require.NoError(t, err)
		assert.True(t, impuesto.IsZero(), "tombstone IMPUESTO must be zero, got %s", impuesto)

		t.Logf("pago %d: tombstone created after CANCELADO='S' (cancelado=%s, importe=%s)",
			impteID, cancelado, imp)
	})
}

// TestE2E_Cobranza_Pagos_HumanReadsFilterCanceled verifies that PorVenta,
// PorCliente, and EnRutaPorZona exclude tombstone rows (CANCELADO='S') from
// their results.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Cobranza_Pagos_HumanReadsFilterCanceled(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)
		requireMigration000019(t, q)

		clienteID, zonaPtr := seedClienteID(t, q)

		cargoImporte := decimal.RequireFromString("2000.00")
		pagoImporte := decimal.RequireFromString("500.00")

		// Pre-seed SALDOS_CC with enough buffer for two pagos.
		buffer := decimal.RequireFromString("5000.00")
		preseedSaldosCC(t, q, clienteID, buffer)

		cargoID := insertCargoDoctosCC(t, q, clienteID, "PAG-HRM-1", cargoImporte)

		// Active pago — must appear in human reads.
		impteActive := insertPagoImporte(t, q, cargoID, pagoImporte)

		// Second pago — will be cancelled to become a tombstone.
		impteCanceled := insertPagoImporte(t, q, cargoID, pagoImporte)

		// Skip if neither pago cache row was created (migration guard).
		var cacheCount int
		err := q.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM MSP_PAGOS_VENTAS WHERE DOCTO_CC_ACR_ID = ?`,
			cargoID).Scan(&cacheCount)
		require.NoError(t, err)
		if cacheCount == 0 {
			t.Skipf("trigger did not create cache rows for cargo %d — verify migration 000019", cargoID)
		}

		// Cancel the second pago to produce a tombstone.
		_, err = q.ExecContext(context.Background(),
			`UPDATE IMPORTES_DOCTOS_CC SET CANCELADO = 'S' WHERE IMPTE_DOCTO_CC_ID = ?`,
			impteCanceled)
		require.NoError(t, err, "cancelar segundo importe")

		repo := cobranzaventfb.NewPagosRepo(pool)

		// PorVenta: should include active pago but exclude the tombstone.
		porVenta, err := repo.PorVenta(ctx, cargoID)
		require.NoError(t, err)
		activeFound := false
		for _, p := range porVenta {
			assert.NotEqual(t, impteCanceled, p.ImpteDoctoCCID(),
				"PorVenta must not return tombstone pago %d", impteCanceled)
			if p.ImpteDoctoCCID() == impteActive {
				activeFound = true
			}
		}
		assert.True(t, activeFound, "PorVenta must include active pago %d", impteActive)

		// PorCliente: tombstone must be absent.
		porCliente, err := repo.PorCliente(ctx, clienteID)
		require.NoError(t, err)
		for _, p := range porCliente {
			assert.NotEqual(t, impteCanceled, p.ImpteDoctoCCID(),
				"PorCliente must not return tombstone pago %d", impteCanceled)
		}

		// EnRutaPorZona: tombstone must be absent.
		if zonaPtr == nil {
			t.Skip("cliente has no zona — skipping EnRutaPorZona assertion")
		}
		porZona, err := repo.EnRutaPorZona(ctx, *zonaPtr, time.Time{})
		require.NoError(t, err)
		for _, p := range porZona {
			assert.NotEqual(t, impteCanceled, p.ImpteDoctoCCID(),
				"EnRutaPorZona must not return tombstone pago %d", impteCanceled)
		}
	})
}

// TestPagosRepo_DeleteTombstonesOlderThan verifies that the cleaner removes
// tombstones whose UPDATED_AT is older than the cutoff and leaves fresh
// tombstones intact.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestPagosRepo_DeleteTombstonesOlderThan(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)
		requireMigration000019(t, q)

		clienteID, _ := seedClienteID(t, q)

		cargoImporte := decimal.RequireFromString("2000.00")
		pagoImporte := decimal.RequireFromString("500.00")

		// Pre-seed SALDOS_CC for the cancellation.
		buffer := decimal.RequireFromString("3000.00")
		preseedSaldosCC(t, q, clienteID, buffer)

		cargoID := insertCargoDoctosCC(t, q, clienteID, "PAG-CLN-1", cargoImporte)
		impteID := insertPagoImporte(t, q, cargoID, pagoImporte)

		// Skip if the cache row was not created.
		var existsBefore int
		err := q.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`,
			impteID).Scan(&existsBefore)
		require.NoError(t, err)
		if existsBefore == 0 {
			t.Skipf("trigger did not create cache row for pago %d — verify migration 000019", impteID)
		}

		// Cancel the pago to create a tombstone.
		_, err = q.ExecContext(context.Background(),
			`UPDATE IMPORTES_DOCTOS_CC SET CANCELADO = 'S' WHERE IMPTE_DOCTO_CC_ID = ?`,
			impteID)
		require.NoError(t, err, "cancelar importe")

		// Verify tombstone was created.
		var cancelado string
		err = q.QueryRowContext(context.Background(),
			`SELECT CANCELADO FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`,
			impteID).Scan(&cancelado)
		require.NoError(t, err, "tombstone must exist after cancellation")
		require.Equal(t, "S", cancelado, "row must be a tombstone")

		// Shift the tombstone's UPDATED_AT into the past (40 days ago) so the
		// cleaner's 30-day cutoff covers it.
		past := time.Now().Add(-40 * 24 * time.Hour)
		_, err = q.ExecContext(context.Background(),
			`UPDATE MSP_PAGOS_VENTAS SET UPDATED_AT = ? WHERE IMPTE_DOCTO_CC_ID = ?`,
			firebird.ToWallClock(past), impteID)
		require.NoError(t, err, "shift UPDATED_AT into past")

		// Run the cleaner with a 30-day cutoff.
		repo := cobranzaventfb.NewPagosRepo(pool)
		cutoff := time.Now().Add(-30 * 24 * time.Hour)
		deleted, err := repo.DeleteTombstonesOlderThan(ctx, cutoff)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, deleted, 1,
			"cleaner must delete at least the stale tombstone we created")

		// Verify the tombstone is gone.
		var afterCount int
		err = q.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`,
			impteID).Scan(&afterCount)
		require.NoError(t, err)
		assert.Equal(t, 0, afterCount,
			"stale tombstone must be physically deleted after cleaner run")

		t.Logf("cleaner deleted %d tombstone(s) older than %s", deleted, cutoff.Format(time.RFC3339))
	})
}

// requireMigration000020 skips the test when migration 000020 has not been
// applied. Detection: query MSP_MIGRATIONS for ID=20.
func requireMigration000020(t *testing.T, q firebird.Querier) {
	t.Helper()
	var n int
	err := q.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM MSP_MIGRATIONS WHERE ID = 20`,
	).Scan(&n)
	if err != nil || n == 0 {
		t.Skipf("migration 000020 not applied; skipping — run 'make fb-migrate-up'")
	}
}

// TestE2E_Pagos_DeleteImporte_GeneraTombstone verifies that physically deleting
// an IMPORTES_DOCTOS_CC pago row (TIPO_IMPTE='R') causes MSP_PAGOS_VENTAS to
// receive a tombstone (CANCELADO='S', IMPORTE=0, IMPUESTO=0) rather than
// disappearing from the cache.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Pagos_DeleteImporte_GeneraTombstone(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)
		requireMigration000019(t, q)
		requireMigration000020(t, q)

		clienteID, _ := seedClienteID(t, q)

		cargoImporte := decimal.RequireFromString("2000.00")
		pagoImporte := decimal.RequireFromString("500.00")

		buffer := decimal.RequireFromString("3000.00")
		preseedSaldosCC(t, q, clienteID, buffer)

		cargoID := insertCargoDoctosCC(t, q, clienteID, "DEL-TMB-1", cargoImporte)
		impteID := insertPagoImporte(t, q, cargoID, pagoImporte)

		// Verify the cache row exists before delete.
		var existsBefore int
		err := q.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`,
			impteID).Scan(&existsBefore)
		require.NoError(t, err)
		if existsBefore == 0 {
			t.Skipf("trigger did not create cache row for pago %d — verify migrations 000019", impteID)
		}

		// Physically delete the importe row — mig 20 trigger must write tombstone.
		_, err = q.ExecContext(context.Background(),
			`DELETE FROM IMPORTES_DOCTOS_CC WHERE IMPTE_DOCTO_CC_ID = ?`,
			impteID)
		require.NoError(t, err, "DELETE IMPORTES_DOCTOS_CC")

		// Assert cache row still exists with tombstone values.
		var (
			cancelado   string
			importeRaw  any
			impuestoRaw any
		)
		err = q.QueryRowContext(context.Background(),
			`SELECT CANCELADO, IMPORTE, IMPUESTO FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`,
			impteID).Scan(&cancelado, &importeRaw, &impuestoRaw)
		require.NoError(t, err, "tombstone row must exist after DELETE")

		assert.Equal(t, "S", cancelado, "tombstone must have CANCELADO='S'")

		imp, err := firebird.ScanDecimal(importeRaw, 2)
		require.NoError(t, err)
		assert.True(t, imp.IsZero(), "tombstone IMPORTE must be zero, got %s", imp)

		impuesto, err := firebird.ScanDecimal(impuestoRaw, 2)
		require.NoError(t, err)
		assert.True(t, impuesto.IsZero(), "tombstone IMPUESTO must be zero, got %s", impuesto)

		t.Logf("pago %d: tombstone created after DELETE (cancelado=%s, importe=%s)",
			impteID, cancelado, imp)
	})
}

// TestE2E_Pagos_DeleteImporte_NoCacheRow_NoOp verifies that deleting an
// IMPORTES_DOCTOS_CC row that was never inserted into MSP_PAGOS_VENTAS (because
// TIPO_IMPTE != 'R') is a no-op: no tombstone phantom row is created and no
// error is raised.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Pagos_DeleteImporte_NoCacheRow_NoOp(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)
		requireMigration000020(t, q)

		clienteID, _ := seedClienteID(t, q)
		cargoImporte := decimal.RequireFromString("1000.00")

		buffer := decimal.RequireFromString("2000.00")
		preseedSaldosCC(t, q, clienteID, buffer)

		cargoID := insertCargoDoctosCC(t, q, clienteID, "DEL-NOP-1", cargoImporte)

		// Insert an importe with TIPO_IMPTE='C' (cargo type) — the trigger
		// MSP_RECOMPUTE_PAGO skips non-'R' importes, so no cache row is created.
		cargImpteID := nextMicrosipID(t, q)
		now := time.Now()
		_, err := q.ExecContext(context.Background(),
			`INSERT INTO IMPORTES_DOCTOS_CC
			  (IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, FECHA,
			   TIPO_IMPTE, DOCTO_CC_ACR_ID,
			   IMPORTE, IMPUESTO,
			   APLICADO, ESTATUS, CANCELADO)
			VALUES (?, ?, ?,
			        'C', NULL,
			        ?, 0,
			        'N', 'N', 'N')`,
			cargImpteID, cargoID, now, cargoImporte)
		require.NoError(t, err, "INSERT IMPORTES_DOCTOS_CC tipo C")

		// Confirm there is no cache row for this importe.
		var cacheCount int
		err = q.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`,
			cargImpteID).Scan(&cacheCount)
		require.NoError(t, err)
		require.Equal(t, 0, cacheCount, "TIPO_IMPTE='C' must not create a cache row")

		// Delete the importe — the trigger UPDATE should be a no-op (0 rows).
		_, err = q.ExecContext(context.Background(),
			`DELETE FROM IMPORTES_DOCTOS_CC WHERE IMPTE_DOCTO_CC_ID = ?`,
			cargImpteID)
		require.NoError(t, err, "DELETE IMPORTES_DOCTOS_CC tipo C")

		// Confirm no phantom tombstone was created.
		var afterCount int
		err = q.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`,
			cargImpteID).Scan(&afterCount)
		require.NoError(t, err)
		assert.Equal(t, 0, afterCount, "no phantom tombstone must exist for non-R importe")

		t.Logf("DELETE of tipo-C importe %d: no phantom tombstone created (no-op)", cargImpteID)
	})
}

// TestE2E_Pagos_DeleteImporte_OnTombstone_Idempotent verifies that deleting a
// row that was already tombstoned via CANCELADO='S' produces an idempotent
// result: CANCELADO stays 'S', UPDATED_AT is refreshed, and no error occurs.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Pagos_DeleteImporte_OnTombstone_Idempotent(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)
		requireMigration000019(t, q)
		requireMigration000020(t, q)

		clienteID, _ := seedClienteID(t, q)

		cargoImporte := decimal.RequireFromString("2000.00")
		pagoImporte := decimal.RequireFromString("500.00")

		buffer := decimal.RequireFromString("3000.00")
		preseedSaldosCC(t, q, clienteID, buffer)

		cargoID := insertCargoDoctosCC(t, q, clienteID, "DEL-IMP-1", cargoImporte)
		impteID := insertPagoImporte(t, q, cargoID, pagoImporte)

		// Verify cache row exists.
		var existsBefore int
		err := q.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`,
			impteID).Scan(&existsBefore)
		require.NoError(t, err)
		if existsBefore == 0 {
			t.Skipf("trigger did not create cache row for pago %d — verify migration 000019", impteID)
		}

		// First: cancel to produce a tombstone via CANCELADO='S' (mig 19 path).
		_, err = q.ExecContext(context.Background(),
			`UPDATE IMPORTES_DOCTOS_CC SET CANCELADO = 'S' WHERE IMPTE_DOCTO_CC_ID = ?`,
			impteID)
		require.NoError(t, err, "cancel importe first")

		// Read the UPDATED_AT of the existing tombstone.
		var updatedAtBefore any
		err = q.QueryRowContext(context.Background(),
			`SELECT UPDATED_AT FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`,
			impteID).Scan(&updatedAtBefore)
		require.NoError(t, err, "tombstone must exist after cancellation")

		// Shift the tombstone's UPDATED_AT back 1 second so we can detect a refresh.
		past := time.Now().Add(-2 * time.Second)
		_, err = q.ExecContext(context.Background(),
			`UPDATE MSP_PAGOS_VENTAS SET UPDATED_AT = ? WHERE IMPTE_DOCTO_CC_ID = ?`,
			firebird.ToWallClock(past), impteID)
		require.NoError(t, err, "shift UPDATED_AT back")

		// Now DELETE the importe from IMPORTES_DOCTOS_CC (the mig 20 trigger path).
		_, err = q.ExecContext(context.Background(),
			`DELETE FROM IMPORTES_DOCTOS_CC WHERE IMPTE_DOCTO_CC_ID = ?`,
			impteID)
		require.NoError(t, err, "DELETE already-tombstoned importe")

		// Verify: tombstone still exists with CANCELADO='S'.
		var (
			cancelado    string
			updatedAtRaw any
		)
		err = q.QueryRowContext(context.Background(),
			`SELECT CANCELADO, UPDATED_AT FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`,
			impteID).Scan(&cancelado, &updatedAtRaw)
		require.NoError(t, err, "tombstone must still exist after second DELETE")
		assert.Equal(t, "S", cancelado, "CANCELADO must remain 'S' after idempotent DELETE")

		// UPDATED_AT should have been refreshed by the mig-20 trigger UPDATE.
		updatedAt, err := firebird.ScanUTCTime(updatedAtRaw)
		require.NoError(t, err)
		assert.True(t, updatedAt.After(past),
			"UPDATED_AT must advance after idempotent DELETE (got %s, shifted past %s)",
			updatedAt.Format(time.RFC3339), past.Format(time.RFC3339))

		t.Logf("pago %d: idempotent tombstone preserved after DELETE on already-tombstoned row", impteID)
	})
}
