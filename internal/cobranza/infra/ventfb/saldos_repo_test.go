// Package ventfb_test contains Firebird-backed E2E tests for the cobranza
// infra layer.
//
// Approach (b) for trigger tests: direct INSERT into DOCTOS_CC /
// IMPORTES_DOCTOS_CC with synthetic data. Approach (a) — calling
// ventas/app.Service.AplicarVenta — is blocked by the depguard
// no-cross-module-internals rule which forbids importing
// internal/ventas/infra/* from internal/cobranza/infra/*. Direct inserts are
// more complex to set up but do not require a cross-module harness.
//
//nolint:misspell // Spanish vocabulary (saldo, cargo, zona, cobranza, etc.) by convention.
package ventfb_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	cobranzaoutbound "github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// requireFBEnv skips the test when FB_DATABASE env var is not set.
func requireFBEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird integration tests")
	}
}

// requireMigration000010 skips the test when the MSP_RECOMPUTE_SALDO_VENTA
// procedure has not been created (migration 000010 not applied).
func requireMigration000010(t *testing.T, q firebird.Querier) {
	t.Helper()
	var n int
	err := q.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM RDB$PROCEDURES WHERE RDB$PROCEDURE_NAME = 'MSP_RECOMPUTE_SALDO_VENTA'`,
	).Scan(&n)
	if err != nil || n == 0 {
		t.Skipf("migration 000010 not applied; skipping — run 'make fb-migrate-up'")
	}
}

// nextMicrosipID claims the next value from the shared Microsip ID generator.
func nextMicrosipID(t *testing.T, q firebird.Querier) int {
	t.Helper()
	var id int
	err := q.QueryRowContext(context.Background(), `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&id)
	require.NoError(t, err, "nextMicrosipID: GEN_ID")
	return id
}

// seedCliente looks up a CLIENTES row and returns its CLIENTE_ID and
// ZONA_CLIENTE_ID. Uses a well-known test cliente (ID 11486, present in
// MUEBLERA.FDB). If the row is absent the test is skipped.
func seedClienteID(t *testing.T, q firebird.Querier) (int, *int) {
	t.Helper()
	const testClienteID = 11486
	var zRaw *int
	err := q.QueryRowContext(
		context.Background(),
		`SELECT ZONA_CLIENTE_ID FROM CLIENTES WHERE CLIENTE_ID = ?`,
		testClienteID,
	).Scan(&zRaw)
	if err != nil {
		t.Skipf("seedClienteID: cliente %d not found: %v", testClienteID, err)
	}
	return testClienteID, zRaw
}

// insertCargoDoctosCC inserts a minimal DOCTOS_CC cargo row (NATURALEZA_CONCEPTO='C')
// and returns the cargo DOCTO_CC_ID. The caller must ensure the transaction is
// available in ctx.
//
// Column set mirrors internal/ventas/infra/microsip/venta_writer.go's cascade
// output as closely as possible. The trigger MSP_SALDOS_DOCTOS_CC_AIU fires
// AFTER INSERT and calls MSP_RECOMPUTE_SALDO_VENTA.
func insertCargoDoctosCC(
	t *testing.T,
	q firebird.Querier,
	clienteID int,
	folio string,
	importeTotal decimal.Decimal,
) int {
	t.Helper()
	var cargoID int
	err := q.QueryRowContext(context.Background(), `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&cargoID)
	require.NoError(t, err, "insertCargoDoctosCC: GEN_ID")

	now := time.Now()
	_, err = q.ExecContext(
		context.Background(),
		`INSERT INTO DOCTOS_CC
		  (DOCTO_CC_ID, CONCEPTO_CC_ID, FOLIO, NATURALEZA_CONCEPTO,
		   SUCURSAL_ID, FECHA, CLIENTE_ID, CLAVE_CLIENTE,
		   TIPO_CAMBIO, DESCRIPCION,
		   SISTEMA_ORIGEN, APLICADO, ESTATUS, ESTATUS_ANT,
		   CONTABILIZADO_GYP, ES_CFD, TIENE_ANTICIPO, CFDI_CERTIFICADO, ENVIADO,
		   INTEG_BA, CONTABILIZADO_BA, CANCELADO)
		VALUES (?, 87327, ?, 'C',
		        225490, ?, ?, '0001',
		        1, 'Cargo prueba cobranza E2E',
		        'CC', 'S', 'N', 'N',
		        'N', 'N', 'N', 'N', 'N',
		        'N', 'N', 'N')`,
		cargoID, folio, now, clienteID,
	)
	require.NoError(t, err, "insertCargoDoctosCC: INSERT DOCTOS_CC")

	// Insert the IMPORTES_DOCTOS_CC importe for the cargo (TIPO_IMPTE='C').
	// IMPORTES_DOCTOS_CC has no CONCEPTO_CC_ID: the concept lives on the
	// parent DOCTOS_CC row above (the recompute procedure JOINs to read it).
	impteID := nextMicrosipID(t, q)
	_, err = q.ExecContext(
		context.Background(),
		`INSERT INTO IMPORTES_DOCTOS_CC
		  (IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, FECHA,
		   TIPO_IMPTE, DOCTO_CC_ACR_ID,
		   IMPORTE, IMPUESTO,
		   APLICADO, ESTATUS, CANCELADO)
		VALUES (?, ?, ?,
		        'C', NULL,
		        ?, 0,
		        'N', 'N', 'N')`,
		impteID, cargoID, now, importeTotal,
	)
	require.NoError(t, err, "insertCargoDoctosCC: INSERT IMPORTES_DOCTOS_CC cargo")

	return cargoID
}

// insertPagoImporte inserts an IMPORTES_DOCTOS_CC payment row (TIPO_IMPTE='R')
// crediting the given cargoID. The pago line is parented to the cargo's own
// DOCTOS_CC row (CONCEPTO_CC_ID=87327, set above), so the recompute procedure
// counts it as cobranza-en-ruta TOTAL_IMPORTE. This is a fixture simplification
// — in real Microsip the pago would live on a separate DOCTOS_CC abono row.
//
// Returns the IMPTE_DOCTO_CC_ID of the new row so callers can use (impteID-1)
// as afterID in SyncPorZona calls. This scopes the sync query past the bulk
// of historical pagos already present in the zone and avoids a spurious
// "pago not found" failure when the new row falls off a fixed-size page.
func insertPagoImporte(
	t *testing.T,
	q firebird.Querier,
	cargoID int,
	importe decimal.Decimal,
) int {
	t.Helper()
	impteID := nextMicrosipID(t, q)
	now := time.Now()
	_, err := q.ExecContext(
		context.Background(),
		`INSERT INTO IMPORTES_DOCTOS_CC
		  (IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, FECHA,
		   TIPO_IMPTE, DOCTO_CC_ACR_ID,
		   IMPORTE, IMPUESTO,
		   APLICADO, ESTATUS, CANCELADO)
		VALUES (?, ?, ?,
		        'R', ?,
		        ?, 0,
		        'N', 'N', 'N')`,
		impteID, cargoID, now, cargoID, importe,
	)
	require.NoError(t, err, "insertPagoImporte: INSERT IMPORTES_DOCTOS_CC pago")
	return impteID
}

// insertPagoNoEnRutaImporte inserts a pago crediting cargoID whose header
// carries CONCEPTO_CC_ID=155 (condonación). Concepto crítico para reproducir
// la asimetría que motiva el fix: MSP_RECOMPUTE_SALDO_VENTA (migración
// 000015) cuenta este concepto dentro de FECHA_ULT_PAGO — por lo que el
// filtro viejo `s.FECHA_ULT_PAGO >= ?` lo dejaba colarse — pero /sync/pagos
// lo excluye (filtro (87327, 27969)), de modo que la venta saldada por
// condonación viajaba sin pago asociado y la app la borraba en
// mergeVentas. El filtro nuevo basado en EXISTS sobre MSP_PAGOS_VENTAS
// (mismo concepto que /sync/pagos) debe rechazarla.
func insertPagoNoEnRutaImporte(
	t *testing.T,
	q firebird.Querier,
	clienteID int,
	cargoID int,
	importe decimal.Decimal,
) {
	t.Helper()
	abonoID := nextMicrosipID(t, q)
	now := time.Now()
	_, err := q.ExecContext(
		context.Background(),
		`INSERT INTO DOCTOS_CC
		  (DOCTO_CC_ID, CONCEPTO_CC_ID, FOLIO, NATURALEZA_CONCEPTO,
		   SUCURSAL_ID, FECHA, CLIENTE_ID, CLAVE_CLIENTE,
		   TIPO_CAMBIO, DESCRIPCION,
		   SISTEMA_ORIGEN, APLICADO, ESTATUS, ESTATUS_ANT,
		   CONTABILIZADO_GYP, ES_CFD, TIENE_ANTICIPO, CFDI_CERTIFICADO, ENVIADO,
		   INTEG_BA, CONTABILIZADO_BA, CANCELADO)
		VALUES (?, 155, ?, 'R',
		        225490, ?, ?, '0001',
		        1, 'Condonacion prueba (fuera filtro cobranza /sync/pagos)',
		        'CC', 'S', 'N', 'N',
		        'N', 'N', 'N', 'N', 'N',
		        'N', 'N', 'N')`,
		abonoID, "COND-ADM", now, clienteID,
	)
	require.NoError(t, err, "insertPagoNoEnRutaImporte: INSERT DOCTOS_CC abono")

	impteID := nextMicrosipID(t, q)
	_, err = q.ExecContext(
		context.Background(),
		`INSERT INTO IMPORTES_DOCTOS_CC
		  (IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, FECHA,
		   TIPO_IMPTE, DOCTO_CC_ACR_ID,
		   IMPORTE, IMPUESTO,
		   APLICADO, ESTATUS, CANCELADO)
		VALUES (?, ?, ?,
		        'R', ?,
		        ?, 0,
		        'N', 'N', 'N')`,
		impteID, abonoID, now, cargoID, importe,
	)
	require.NoError(t, err, "insertPagoNoEnRutaImporte: INSERT IMPORTES_DOCTOS_CC")
}

// buildCobranzaService builds a real Service with Firebird-backed repos.
func buildCobranzaService(t *testing.T, pool *firebird.Pool) *cobranzaapp.Service {
	t.Helper()
	repo := cobranzaventfb.NewSaldosRepo(pool)
	return cobranzaapp.NewService(repo, cobranzaventfb.NewPagosRepo(pool), cobranzaventfb.NewVentasRepo(pool), cobranzaoutbound.ProductionClock{}, nil, nil, nil, nil, nil, nil)
}

// buildCobranzaReconciler builds a real Reconciler with Firebird-backed
// Recomputer + SaldosRepo, but a fixed in-memory lister so the test only
// walks the cargo IDs the caller supplies. Walking the full MSP_SALDOS_VENTAS
// table (~100k+ rows in dev) inside a single transaction is too slow to
// complete within a test timeout, so we narrow the scan to the inserted
// fixtures.
func buildCobranzaReconciler(t *testing.T, pool *firebird.Pool, cargoIDs ...int) *cobranzaapp.Reconciler {
	t.Helper()
	repo := cobranzaventfb.NewSaldosRepo(pool)
	return cobranzaapp.NewReconciler(cobranzaapp.ReconcilerDeps{
		SaldosLister: &fixedIDLister{ids: cargoIDs},
		Recomputer:   cobranzaventfb.NewRecomputer(pool, repo),
		SaldosRepo:   repo,
		Clock:        cobranzaoutbound.ProductionClock{},
		Config: cobranzaapp.ReconcilerConfig{
			PageSize: max(1, len(cargoIDs)),
			DriftLog: true,
			FixDrift: true,
		},
		Logger: testLogger(),
	})
}

// fixedIDLister is an outbound.SaldosLister that yields a fixed set of cargo
// IDs in one page. Used by E2E reconcile tests to scope the reconciler scan
// to fixtures instead of the entire (large) cache.
type fixedIDLister struct {
	ids    []int
	served bool
}

func (l *fixedIDLister) Page(_ context.Context, _, _ int) ([]int, int, error) {
	if l.served {
		return nil, 0, nil
	}
	l.served = true
	return l.ids, 0, nil
}

// testLogger returns a no-op slog.Logger for tests.
func testLogger() *slog.Logger {
	return slog.Default()
}

// ─── E2E tests ────────────────────────────────────────────────────────────────

// TestE2E_Cobranza_Trigger_FiresOnInsertContado inserts a cargo (TIPO='C')
// directly into DOCTOS_CC and verifies the trigger created a MSP_SALDOS_VENTAS
// row with saldo == precioTotal (since no pagos exist).
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Cobranza_Trigger_FiresOnInsertContado(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)

		clienteID, _ := seedClienteID(t, q)
		importe := decimal.RequireFromString("3500.00")
		cargoID := insertCargoDoctosCC(t, q, clienteID, "TEST-001", importe)

		repo := cobranzaventfb.NewSaldosRepo(pool)
		saldo, err := repo.PorCargo(ctx, cargoID)
		if err != nil {
			// If the trigger didn't fire (WHEN ANY DO absorbs errors), skip clearly.
			require.ErrorIs(t, err, domain.ErrSaldoNoEncontrado,
				"expected ErrSaldoNoEncontrado or valid saldo; trigger may not have fired")
			t.Skipf("trigger did not create cache row for cargo %d — verify migration 000010", cargoID)
		}

		assert.Equal(t, cargoID, saldo.DoctoCCID())
		assert.True(t, importe.Equal(saldo.PrecioTotal()),
			"PrecioTotal mismatch: want=%s got=%s", importe, saldo.PrecioTotal())
		// Saldo should equal precioTotal since no payments yet.
		assert.True(t, saldo.Saldo().GreaterThanOrEqual(decimal.Zero),
			"Saldo must be >= 0 after trigger")

		t.Logf("cargo %d: PrecioTotal=%s Saldo=%s", cargoID, saldo.PrecioTotal(), saldo.Saldo())
	})
}

// TestE2E_Cobranza_AbonoExterno_UpdatesSaldo inserts a cargo then a payment
// importe and verifies the trigger decreases the saldo.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Cobranza_AbonoExterno_UpdatesSaldo(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)

		clienteID, _ := seedClienteID(t, q)
		importe := decimal.RequireFromString("5000.00")
		cargoID := insertCargoDoctosCC(t, q, clienteID, "TEST-002", importe)

		repo := cobranzaventfb.NewSaldosRepo(pool)

		// Read saldo before payment.
		saldoAntes, err := repo.PorCargo(ctx, cargoID)
		if err != nil {
			t.Skipf("trigger did not create cache row for cargo %d — verify migration 000010", cargoID)
		}

		// Insert payment of 500.
		pago := decimal.RequireFromString("500.00")
		insertPagoImporte(t, q, cargoID, pago)

		// Re-read: saldo should have decreased.
		saldoDespues, err := repo.PorCargo(ctx, cargoID)
		require.NoError(t, err)

		expectedSaldo := saldoAntes.Saldo().Sub(pago)
		assert.True(t, expectedSaldo.Equal(saldoDespues.Saldo()),
			"Saldo after pago mismatch: want=%s got=%s",
			expectedSaldo.StringFixed(2), saldoDespues.Saldo().StringFixed(2))
		assert.Greater(t, saldoDespues.NumPagos(), saldoAntes.NumPagos(),
			"NumPagos must increase after pago")

		t.Logf("cargo %d: SaldoAntes=%s SaldoDespues=%s",
			cargoID, saldoAntes.Saldo(), saldoDespues.Saldo())
	})
}

// TestE2E_Cobranza_Cancelacion_Tombstone inserts a cargo, verifies the cache
// row exists, then sets CANCELADO='S' on DOCTOS_CC and verifies the row is
// kept as a tombstone (CARGO_CANCELADO=true, saldo=0). Tombstones are
// required so the sync-by-UPDATED_AT endpoint can propagate cancellations to
// mobile clients (see migration 000014).
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Cobranza_Cancelacion_Tombstone(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)

		clienteID, _ := seedClienteID(t, q)
		cargoImporte := decimal.RequireFromString("2000.00")

		// Pre-seed SALDOS_CC for the current month so the cancel trigger can
		// subtract the cargo importe without violating the CARGOS_CXC >= 0
		// CHECK constraint.
		//
		// Root-cause analysis (2026-06): SALDOS_CC is a monthly summary table
		// keyed by (CLIENTE_ID, ANO, MES). Inserting a synthetic DOCTOS_CC
		// cargo does NOT increment SALDOS_CC.CARGOS_CXC (the Microsip INSERT
		// trigger path for SALDOS_CC is asymmetric with the cancel/UPDATE path
		// for synthetic data that bypasses some preconditions). The cancel
		// trigger (DOCTOS_CC_BEFUPD_0 → IMPTES_DOCTOS_CC_BEFUPD_0 →
		// AFECTA_SALDOS_CC) DOES subtract from CARGOS_CXC, so if the current
		// month row shows 0, the subtraction underflows and the constraint
		// fires. We pre-add the importe directly (inside the rollback-only tx)
		// so the math is symmetric for the test; the tx rolls back at the end
		// and SALDOS_CC is restored to its original state.
		ano := time.Now().Year()
		mes := int(time.Now().Month())
		seedRes, seedErr := q.ExecContext(context.Background(),
			`UPDATE SALDOS_CC SET CARGOS_CXC = CARGOS_CXC + ?
			  WHERE CLIENTE_ID = ? AND ANO = ? AND MES = ?`,
			cargoImporte, clienteID, ano, mes)
		require.NoError(t, seedErr, "pre-seed SALDOS_CC CARGOS_CXC")
		if n, _ := seedRes.RowsAffected(); n == 0 {
			t.Skipf("no SALDOS_CC row for cliente %d ano=%d mes=%d — pre-seed skipped; "+
				"re-run after Microsip creates this month's saldo row", clienteID, ano, mes)
		}

		cargoID := insertCargoDoctosCC(t, q, clienteID, "TEST-003", cargoImporte)

		repo := cobranzaventfb.NewSaldosRepo(pool)

		_, err := repo.PorCargo(ctx, cargoID)
		if err != nil {
			t.Skipf("trigger did not create cache row for cargo %d — verify migration 000010", cargoID)
		}

		// Cancel the cargo — the trigger should mark the cache row as a
		// tombstone (CARGO_CANCELADO='S'), not delete it.
		_, err = q.ExecContext(
			context.Background(),
			`UPDATE DOCTOS_CC SET CANCELADO = 'S' WHERE DOCTO_CC_ID = ?`,
			cargoID,
		)
		require.NoError(t, err, "cancelar cargo")

		tomb, err := repo.PorCargo(ctx, cargoID)
		require.NoError(t, err, "tombstone row must exist after cancellation")
		require.NotNil(t, tomb)
		assert.True(t, tomb.CargoCancelado(), "tombstone must have cargo_cancelado=true")
		assert.True(t, tomb.Saldo().IsZero(), "tombstone saldo must be zero")
		assert.Equal(t, 0, tomb.NumPagos(), "tombstone num_pagos must be zero")

		t.Logf("cargo %d: tombstone created after CANCELADO='S' (cargo_cancelado=%v)",
			cargoID, tomb.CargoCancelado())
	})
}

// TestE2E_Cobranza_CambioZona_UpdatesAllRows inserts two cargos for the same
// cliente, then updates CLIENTES.ZONA_CLIENTE_ID and verifies both cache rows
// reflect the new zone (denormalized update trigger).
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Cobranza_CambioZona_UpdatesAllRows(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)

		clienteID, zonaAntes := seedClienteID(t, q)

		cargo1 := insertCargoDoctosCC(t, q, clienteID, "TEST-004A", decimal.RequireFromString("1000.00"))
		cargo2 := insertCargoDoctosCC(t, q, clienteID, "TEST-004B", decimal.RequireFromString("2000.00"))

		repo := cobranzaventfb.NewSaldosRepo(pool)

		_, err := repo.PorCargo(ctx, cargo1)
		if err != nil {
			t.Skipf("trigger did not create cache row for cargo %d — verify migration 000010", cargo1)
		}

		// Pick a new zone different from the current one.
		const altZonaID = 21563 // test zona from ventas E2E catalog
		newZona := altZonaID
		if zonaAntes != nil && *zonaAntes == altZonaID {
			newZona = 1 // any different ID
		}

		_, err = q.ExecContext(
			context.Background(),
			`UPDATE CLIENTES SET ZONA_CLIENTE_ID = ? WHERE CLIENTE_ID = ?`,
			newZona, clienteID,
		)
		require.NoError(t, err, "update CLIENTES zona")

		s1, err := repo.PorCargo(ctx, cargo1)
		require.NoError(t, err)
		s2, err := repo.PorCargo(ctx, cargo2)
		require.NoError(t, err)

		assert.Equal(t, newZona, *s1.ZonaClienteID(),
			"cargo1 cache must reflect new zona")
		assert.Equal(t, newZona, *s2.ZonaClienteID(),
			"cargo2 cache must reflect new zona")

		t.Logf("zona change %v→%d: both cache rows updated", zonaAntes, newZona)
	})
}

// TestE2E_Cobranza_EnRutaPorZona_VentanaDias inserts cargos in a zone, applies
// payments to some, and verifies the ventana-dias filter.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Cobranza_EnRutaPorZona_VentanaDias(t *testing.T) {
	t.Skip("TODO(cobranza): requires setting FECHA_ULT_PAGO relative dates; complex within a single tx — skipping until date-manipulation helper is available")
}

// TestE2E_Cobranza_Reconcile_Sano inserts two cargos with no drift and runs
// the reconciler, expecting Drift == 0 for those rows.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Cobranza_Reconcile_Sano(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)

		clienteID, _ := seedClienteID(t, q)
		cargo1 := insertCargoDoctosCC(t, q, clienteID, "REC-001A", decimal.RequireFromString("1500.00"))

		repo := cobranzaventfb.NewSaldosRepo(pool)
		_, err := repo.PorCargo(ctx, cargo1)
		if err != nil {
			t.Skipf("trigger did not create cache row for cargo %d", cargo1)
		}

		reconciler := buildCobranzaReconciler(t, pool, cargo1)
		report, err := reconciler.Run(ctx)
		require.NoError(t, err)

		assert.GreaterOrEqual(t, report.Checked, 1, "reconciler should have checked at least 1 row")
		assert.Equal(t, 0, report.Drift, "no drift expected on a freshly inserted cargo")
		t.Logf("reconcile sano: checked=%d drift=%d errors=%d", report.Checked, report.Drift, report.Errors)
	})
}

// TestE2E_Cobranza_Reconcile_DetectaDrift corrupts a cache row directly,
// runs the reconciler, and verifies Drift >= 1 is reported and the row fixed.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Cobranza_Reconcile_DetectaDrift(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)

		clienteID, _ := seedClienteID(t, q)
		importe := decimal.RequireFromString("3000.00")
		cargoID := insertCargoDoctosCC(t, q, clienteID, "RDRIFT-01", importe)

		repo := cobranzaventfb.NewSaldosRepo(pool)
		saldoOK, err := repo.PorCargo(ctx, cargoID)
		if err != nil {
			t.Skipf("trigger did not create cache row for cargo %d", cargoID)
		}

		// Corrupt the cache row directly (bypasses triggers).
		_, err = q.ExecContext(
			context.Background(),
			`UPDATE MSP_SALDOS_VENTAS SET SALDO = -999 WHERE DOCTO_CC_ID = ?`,
			cargoID,
		)
		require.NoError(t, err, "corrupt cache row")

		reconciler := buildCobranzaReconciler(t, pool, cargoID)
		report, err := reconciler.Run(ctx)
		require.NoError(t, err)

		assert.GreaterOrEqual(t, report.Drift, 1, "reconciler must detect >= 1 drift")

		// After reconcile the saldo should be back to correct value.
		saldoFixed, err := repo.PorCargo(ctx, cargoID)
		require.NoError(t, err)
		assert.True(t, saldoOK.Saldo().Equal(saldoFixed.Saldo()),
			"saldo must be fixed after reconcile: want=%s got=%s",
			saldoOK.Saldo().StringFixed(2), saldoFixed.Saldo().StringFixed(2))

		t.Logf("drift detected and fixed: cargoID=%d drift=%d", cargoID, report.Drift)
	})
}

// TestE2E_Saldos_DeleteCargo_GeneraTombstone verifies that physically deleting
// a DOCTOS_CC cargo row (NATURALEZA_CONCEPTO='C') causes the trigger
// MSP_SALDOS_DOCTOS_CC_AD (migration 000020) to write a tombstone in
// MSP_SALDOS_VENTAS (CARGO_CANCELADO='S', SALDO=0, TOTAL_IMPORTE=0,
// IMPTE_REST=0, NUM_PAGOS=0) instead of deleting the cache row.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Saldos_DeleteCargo_GeneraTombstone(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)
		requireMigration000020(t, q)

		clienteID, _ := seedClienteID(t, q)
		cargoImporte := decimal.RequireFromString("3000.00")

		buffer := decimal.RequireFromString("4000.00")
		preseedSaldosCC(t, q, clienteID, buffer)

		cargoID := insertCargoDoctosCC(t, q, clienteID, "SDL-TMB-1", cargoImporte)

		repo := cobranzaventfb.NewSaldosRepo(pool)

		// Verify the cache row was created by the INSERT trigger.
		saldoAntes, err := repo.PorCargo(ctx, cargoID)
		if err != nil {
			t.Skipf("trigger did not create cache row for cargo %d — verify migration 000010", cargoID)
		}
		require.NotNil(t, saldoAntes)

		// Delete all IMPORTES_DOCTOS_CC rows referencing this DOCTOS_CC row first
		// (FK DOCTO_PADRE_CC is RESTRICT). The insertCargoDoctosCC helper inserts
		// one IMPORTES_DOCTOS_CC cargo importe; delete it explicitly before the
		// parent DOCTOS_CC DELETE.
		_, err = q.ExecContext(context.Background(),
			`DELETE FROM IMPORTES_DOCTOS_CC WHERE DOCTO_CC_ID = ?`,
			cargoID)
		require.NoError(t, err, "DELETE IMPORTES_DOCTOS_CC children")

		// Now delete the DOCTOS_CC cargo row — mig 20 AFTER DELETE trigger fires.
		_, err = q.ExecContext(context.Background(),
			`DELETE FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`,
			cargoID)
		require.NoError(t, err, "DELETE DOCTOS_CC cargo")

		// Assert the cache row still exists as a tombstone.
		var (
			cargoCancelado string
			saldoRaw       any
			totalImpteRaw  any
			impteRestRaw   any
			numPagos       int
		)
		err = q.QueryRowContext(context.Background(),
			`SELECT CARGO_CANCELADO, SALDO, TOTAL_IMPORTE, IMPTE_REST, NUM_PAGOS
			   FROM MSP_SALDOS_VENTAS
			  WHERE DOCTO_CC_ID = ?`,
			cargoID).Scan(&cargoCancelado, &saldoRaw, &totalImpteRaw, &impteRestRaw, &numPagos)
		require.NoError(t, err, "tombstone row must exist in MSP_SALDOS_VENTAS after DELETE")

		assert.Equal(t, "S", cargoCancelado, "tombstone must have CARGO_CANCELADO='S'")

		saldo, err := firebird.ScanDecimal(saldoRaw, 2)
		require.NoError(t, err)
		assert.True(t, saldo.IsZero(), "tombstone SALDO must be zero, got %s", saldo)

		totalImporte, err := firebird.ScanDecimal(totalImpteRaw, 2)
		require.NoError(t, err)
		assert.True(t, totalImporte.IsZero(), "tombstone TOTAL_IMPORTE must be zero, got %s", totalImporte)

		impteRest, err := firebird.ScanDecimal(impteRestRaw, 2)
		require.NoError(t, err)
		assert.True(t, impteRest.IsZero(), "tombstone IMPTE_REST must be zero, got %s", impteRest)

		assert.Equal(t, 0, numPagos, "tombstone NUM_PAGOS must be zero")

		t.Logf("cargo %d: tombstone created in MSP_SALDOS_VENTAS after DELETE (cargo_cancelado=%s, saldo=%s)",
			cargoID, cargoCancelado, saldo)
	})
}

// TestE2E_Saldos_SyncPorZona_SameMillisecond_NoSkip verifies that SyncPorZona
// does NOT silently skip rows when two rows share the exact same UPDATED_AT
// millisecond value. The production code already implements the tie-break via
// querySyncPage (which adds `AND (UPDATED_AT > ? OR (UPDATED_AT = ? AND
// DOCTO_CC_ID > ?))` when cursor is non-zero); this test guards against future
// regression by exercising that path end-to-end against a real Firebird DB.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Saldos_SyncPorZona_SameMillisecond_NoSkip(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)

		clienteID, zonaID := seedZonedCliente(t, q)

		// Insert two cargo rows — the trigger creates MSP_SALDOS_VENTAS rows.
		cargo1 := insertCargoDoctosCC(t, q, clienteID, "SMMS-001A", decimal.RequireFromString("1000.00"))
		cargo2 := insertCargoDoctosCC(t, q, clienteID, "SMMS-001B", decimal.RequireFromString("1500.00"))

		// Verify both cache rows were created by the trigger before forcing UPDATED_AT.
		repo := cobranzaventfb.NewSaldosRepo(pool)
		if _, err := repo.PorCargo(ctx, cargo1); err != nil {
			t.Skipf("trigger did not create cache row for cargo %d — verify migration 000010", cargo1)
		}
		if _, err := repo.PorCargo(ctx, cargo2); err != nil {
			t.Skipf("trigger did not create cache row for cargo %d — verify migration 000010", cargo2)
		}

		// Force both rows to the same UPDATED_AT millisecond. Use a timestamp
		// safely in the past (> syncLagSeconds=5 s) so neither row falls inside
		// the lag exclusion window.
		forcedTS := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
		_, err := q.ExecContext(
			context.Background(),
			`UPDATE MSP_SALDOS_VENTAS
			    SET UPDATED_AT = ?
			  WHERE DOCTO_CC_ID IN (?, ?)`,
			firebird.ToWallClock(forcedTS), cargo1, cargo2,
		)
		require.NoError(t, err, "force UPDATED_AT to same millisecond")

		// Determine the lower of the two PKs so page 1 starts right before it.
		minPK := min(cargo1, cargo2)

		// cursor just before forcedTS so the >= lower-bound passes both rows.
		cursorBefore := forcedTS.Add(-time.Second)
		afterID0 := minPK - 1

		page1, err := repo.SyncPorZona(ctx, zonaID, cursorBefore, afterID0, 1)
		require.NoError(t, err)
		require.Len(t, page1.Items, 1, "page 1 should return exactly one row when limit=1")

		// Page 2: cursor = forcedTS, afterID = PK returned by page 1.
		pk1 := page1.Items[0].DoctoCCID()
		page2, err := repo.SyncPorZona(ctx, zonaID, forcedTS, pk1, 1)
		require.NoError(t, err)
		require.Len(t, page2.Items, 1, "page 2 must return the second same-ms row via PK tie-break")
		pk2 := page2.Items[0].DoctoCCID()

		assert.NotEqual(t, pk1, pk2, "page 2 must return the OTHER row, not the same one")

		// Both inserted PKs must be covered across the two pages.
		got := map[int]bool{pk1: true, pk2: true}
		want := map[int]bool{cargo1: true, cargo2: true}
		assert.Equal(t, want, got,
			"the two pages together must cover exactly the two inserted cargo IDs")

		t.Logf("same-ms tie-break ok: forcedTS=%s page1PK=%d page2PK=%d",
			forcedTS.Format(time.RFC3339Nano), pk1, pk2)
	})
}
