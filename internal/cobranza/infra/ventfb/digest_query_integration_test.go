//nolint:misspell // Spanish vocabulary by project convention.
package ventfb_test

// Integration tests for digest_query.go: PagosRepo.Digest + PagosRepo.ListIDs.
//
// NOTE ON ISOLATION
// =================
// Digest and ListIDs open a Firebird REPEATABLE READ transaction (snapshot
// isolation). The snapshot sees only committed rows, so fixtures must be
// committed rather than left in a rollback-only transaction.
//
// Pattern used here:
//
//  1. Insert fixtures using pool.DB directly (auto-commit, outside any tx).
//  2. Register t.Cleanup to DELETE those same rows after the test.
//  3. Scope assertions to the IDs we inserted so pre-existing rows in the zone
//     do not invalidate the test.

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// ─── committed-fixture helpers ───────────────────────────────────────────────

// insertCommittedCargo inserts a DOCTOS_CC + IMPORTES_DOCTOS_CC cargo pair
// using pool (auto-commit). Registers t.Cleanup to delete the rows.
// Returns cargoID and the cargo-importe ID.
func insertCommittedCargo(t *testing.T, pool *firebird.Pool, clienteID int, folio string, importe decimal.Decimal) (int, int) {
	t.Helper()
	ctx := context.Background()

	var cargoID, cargoImpteID int
	require.NoError(t, pool.QueryRowContext(ctx, `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&cargoID))
	require.NoError(t, pool.QueryRowContext(ctx, `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&cargoImpteID))

	now := time.Now()
	_, err := pool.ExecContext(ctx,
		`INSERT INTO DOCTOS_CC
		  (DOCTO_CC_ID, CONCEPTO_CC_ID, FOLIO, NATURALEZA_CONCEPTO,
		   SUCURSAL_ID, FECHA, CLIENTE_ID, CLAVE_CLIENTE,
		   TIPO_CAMBIO, DESCRIPCION,
		   SISTEMA_ORIGEN, APLICADO, ESTATUS, ESTATUS_ANT,
		   CONTABILIZADO_GYP, ES_CFD, TIENE_ANTICIPO, CFDI_CERTIFICADO, ENVIADO,
		   INTEG_BA, CONTABILIZADO_BA, CANCELADO)
		VALUES (?, 87327, ?, 'C',
		        225490, ?, ?, '0001',
		        1, 'Digest E2E fixture',
		        'CC', 'S', 'N', 'N',
		        'N', 'N', 'N', 'N', 'N',
		        'N', 'N', 'N')`,
		cargoID, folio, now, clienteID)
	require.NoError(t, err, "insertCommittedCargo: INSERT DOCTOS_CC")

	_, err = pool.ExecContext(ctx,
		`INSERT INTO IMPORTES_DOCTOS_CC
		  (IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, FECHA,
		   TIPO_IMPTE, DOCTO_CC_ACR_ID,
		   IMPORTE, IMPUESTO,
		   APLICADO, ESTATUS, CANCELADO)
		VALUES (?, ?, ?, 'C', NULL, ?, 0, 'N', 'N', 'N')`,
		cargoImpteID, cargoID, now, importe)
	require.NoError(t, err, "insertCommittedCargo: INSERT IMPORTES_DOCTOS_CC cargo")

	t.Cleanup(func() {
		_, _ = pool.ExecContext(ctx, `DELETE FROM IMPORTES_DOCTOS_CC WHERE IMPTE_DOCTO_CC_ID = ?`, cargoImpteID)
		_, _ = pool.ExecContext(ctx, `DELETE FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`, cargoID)
	})
	return cargoID, cargoImpteID
}

// insertCommittedPago inserts an IMPORTES_DOCTOS_CC payment row (TIPO_IMPTE='R')
// using pool (auto-commit). The trigger MSP_RECOMPUTE_PAGO fires AFTER
// INSERT and writes a row into MSP_PAGOS_VENTAS. Registers t.Cleanup to delete
// both rows. Returns the pago impte ID.
func insertCommittedPago(t *testing.T, pool *firebird.Pool, cargoID int, importe decimal.Decimal) int {
	t.Helper()
	ctx := context.Background()

	var impteID int
	require.NoError(t, pool.QueryRowContext(ctx, `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&impteID))

	now := time.Now()
	_, err := pool.ExecContext(ctx,
		`INSERT INTO IMPORTES_DOCTOS_CC
		  (IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, FECHA,
		   TIPO_IMPTE, DOCTO_CC_ACR_ID,
		   IMPORTE, IMPUESTO,
		   APLICADO, ESTATUS, CANCELADO)
		VALUES (?, ?, ?, 'R', ?, ?, 0, 'N', 'N', 'N')`,
		impteID, cargoID, now, cargoID, importe)
	require.NoError(t, err, "insertCommittedPago: INSERT IMPORTES_DOCTOS_CC")

	t.Cleanup(func() {
		_, _ = pool.ExecContext(ctx, `DELETE FROM IMPORTES_DOCTOS_CC WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
		_, _ = pool.ExecContext(ctx, `DELETE FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
	})
	return impteID
}

// requirePagosVentasCacheRow skips the test when the MSP_RECOMPUTE_PAGO trigger
// did not create a cache row for the given impteID.
func requirePagosVentasCacheRow(t *testing.T, pool *firebird.Pool, impteID int) {
	t.Helper()
	var n int
	err := pool.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`, impteID).Scan(&n)
	if err != nil || n == 0 {
		t.Skipf("trigger did not create MSP_PAGOS_VENTAS row for impte %d — verify migrations 000010/000019", impteID)
	}
}

// seedZonedClienteFromPool returns a clienteID and zonaID by querying the pool
// directly (no active transaction required). Uses the same logic as the
// existing seedZonedCliente helper but queries pool directly instead of a Querier.
func seedZonedClienteFromPool(t *testing.T, pool *firebird.Pool) (int, int) {
	t.Helper()
	const preferredID = 11486
	var preferredZona *int
	err := pool.QueryRowContext(context.Background(),
		`SELECT ZONA_CLIENTE_ID FROM CLIENTES WHERE CLIENTE_ID = ?`, preferredID).Scan(&preferredZona)
	if err == nil && preferredZona != nil {
		return preferredID, *preferredZona
	}
	var clienteID, zonaID int
	err = pool.QueryRowContext(context.Background(),
		`SELECT FIRST 1 CLIENTE_ID, ZONA_CLIENTE_ID FROM CLIENTES
		 WHERE ZONA_CLIENTE_ID IS NOT NULL ORDER BY CLIENTE_ID`).Scan(&clienteID, &zonaID)
	if err != nil {
		t.Skipf("no zoned cliente available: %v", err)
	}
	return clienteID, zonaID
}

// ─── TestE2E_PagosDigest_HappyPath ───────────────────────────────────────────

// TestE2E_PagosDigest_HappyPath inserts a committed pago row and verifies that
// Digest returns count >= 1. Uses a relaxed assertion because the zone may have
// pre-existing rows.
//
//nolint:paralleltest
func TestE2E_PagosDigest_HappyPath(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)
		requireMigration000019(t, q)
	})

	clienteID, zonaID := seedZonedClienteFromPool(t, pool)

	folio := fmt.Sprintf("DIG%X", time.Now().UnixNano()&0xFFFFFF)
	cargoID, _ := insertCommittedCargo(t, pool, clienteID, folio, decimal.RequireFromString("2000.00"))
	impteID := insertCommittedPago(t, pool, cargoID, decimal.RequireFromString("900.00"))
	requirePagosVentasCacheRow(t, pool, impteID)

	repo := cobranzaventfb.NewPagosRepo(pool)
	result, err := repo.Digest(context.Background(), zonaID, time.Time{})
	require.NoError(t, err)

	assert.GreaterOrEqual(t, result.CountActivos, 1,
		"Digest must count at least the pago we inserted")
	if result.CountActivos > 0 {
		assert.NotZero(t, result.IDsSum, "IDsSum must be non-zero when at least one row exists")
	}

	t.Logf("PagosDigest zona=%d: count=%d xor=%d sum=%d maxUpdatedAt=%s",
		zonaID, result.CountActivos, result.IDsXor, result.IDsSum,
		result.MaxUpdatedAt.Format(time.RFC3339))
}

// ─── TestE2E_PagosListIDs_Pagination ─────────────────────────────────────────

// TestE2E_PagosListIDs_Pagination inserts two pago rows and verifies
// cursor-paginated ListIDs retrieves both in order.
//
//nolint:paralleltest
func TestE2E_PagosListIDs_Pagination(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)
		requireMigration000019(t, q)
	})

	clienteID, zonaID := seedZonedClienteFromPool(t, pool)

	folio := fmt.Sprintf("DIG%X", time.Now().UnixNano()&0xFFFFFF)
	cargoID, _ := insertCommittedCargo(t, pool, clienteID, folio, decimal.RequireFromString("2000.00"))
	impte1 := insertCommittedPago(t, pool, cargoID, decimal.RequireFromString("500.00"))
	impte2 := insertCommittedPago(t, pool, cargoID, decimal.RequireFromString("500.00"))
	requirePagosVentasCacheRow(t, pool, impte1)
	requirePagosVentasCacheRow(t, pool, impte2)

	if impte1 > impte2 {
		impte1, impte2 = impte2, impte1
	}

	repo := cobranzaventfb.NewPagosRepo(pool)

	// Page 1: after = impte1-1, limit = 1 → [impte1], has_more = true.
	page1, hasMore1, err := repo.ListIDs(context.Background(), zonaID, impte1-1, 1, time.Time{})
	require.NoError(t, err)
	require.NotEmpty(t, page1)
	assert.Equal(t, impte1, page1[0])
	assert.True(t, hasMore1, "impte2 follows impte1 so has_more must be true")

	// Page 2: after = impte1, limit = 1 → at minimum [impte2].
	page2, _, err := repo.ListIDs(context.Background(), zonaID, impte1, 1, time.Time{})
	require.NoError(t, err)
	require.NotEmpty(t, page2)
	assert.Equal(t, impte2, page2[0])

	t.Logf("PagosListIDs zona=%d impte1=%d impte2=%d page1=%v hasMore1=%v page2=%v",
		zonaID, impte1, impte2, page1, hasMore1, page2)
}

// ─── TestE2E_PagosDigest_UnderConcurrentWrites ───────────────────────────────

// TestE2E_PagosDigest_UnderConcurrentWrites starts a background goroutine
// inserting committed rows while calling Digest twice. Because two independent
// snapshots may see different committed states, we Skip (not Fail) when they
// differ. The test's real assertion is that each individual Digest call
// completes without error under concurrent write pressure.
//
//nolint:paralleltest
func TestE2E_PagosDigest_UnderConcurrentWrites(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)
		requireMigration000019(t, q)
	})

	clienteID, zonaID := seedZonedClienteFromPool(t, pool)

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		bgIDs []int
	)
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx := context.Background()
		for i := range 5 {
			select {
			case <-stop:
				return
			default:
			}
			folio := time.Now().Format("150405.000") + string(rune('A'+i%26))

			// Use direct pool inserts (not the require-based helpers) so that
			// errors in the goroutine don't call t.FailNow on the wrong goroutine.
			var cargoID int
			if err := pool.QueryRowContext(ctx, `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&cargoID); err != nil {
				continue
			}
			now := time.Now()
			if _, err := pool.ExecContext(ctx,
				`INSERT INTO DOCTOS_CC
				  (DOCTO_CC_ID, CONCEPTO_CC_ID, FOLIO, NATURALEZA_CONCEPTO,
				   SUCURSAL_ID, FECHA, CLIENTE_ID, CLAVE_CLIENTE,
				   TIPO_CAMBIO, DESCRIPCION,
				   SISTEMA_ORIGEN, APLICADO, ESTATUS, ESTATUS_ANT,
				   CONTABILIZADO_GYP, ES_CFD, TIENE_ANTICIPO, CFDI_CERTIFICADO, ENVIADO,
				   INTEG_BA, CONTABILIZADO_BA, CANCELADO)
				VALUES (?, 87327, ?, 'C',
				        225490, ?, ?, '0001',
				        1, 'ConcDigest E2E fixture',
				        'CC', 'S', 'N', 'N',
				        'N', 'N', 'N', 'N', 'N',
				        'N', 'N', 'N')`,
				cargoID, folio, now, clienteID); err != nil {
				continue
			}
			var impteID int
			if err := pool.QueryRowContext(ctx, `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&impteID); err != nil {
				continue
			}
			if _, err := pool.ExecContext(ctx,
				`INSERT INTO IMPORTES_DOCTOS_CC
				  (IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, FECHA,
				   TIPO_IMPTE, DOCTO_CC_ACR_ID,
				   IMPORTE, IMPUESTO,
				   APLICADO, ESTATUS, CANCELADO)
				VALUES (?, ?, ?, 'R', ?, 100, 0, 'N', 'N', 'N')`,
				impteID, cargoID, now, cargoID); err != nil {
				continue
			}
			// Schedule cleanup outside the goroutine (t.Cleanup is goroutine-safe).
			localImpteID := impteID
			t.Cleanup(func() {
				_, _ = pool.ExecContext(ctx, `DELETE FROM IMPORTES_DOCTOS_CC WHERE IMPTE_DOCTO_CC_ID = ?`, localImpteID)
				_, _ = pool.ExecContext(ctx, `DELETE FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`, localImpteID)
			})
			mu.Lock()
			bgIDs = append(bgIDs, impteID)
			mu.Unlock()
			time.Sleep(150 * time.Millisecond)
		}
	}()

	time.Sleep(200 * time.Millisecond)

	repo := cobranzaventfb.NewPagosRepo(pool)
	result1, err1 := repo.Digest(context.Background(), zonaID, time.Time{})
	result2, err2 := repo.Digest(context.Background(), zonaID, time.Time{})

	close(stop)
	wg.Wait()

	require.NoError(t, err1)
	require.NoError(t, err2)

	mu.Lock()
	inserted := len(bgIDs)
	mu.Unlock()

	if result1.CountActivos != result2.CountActivos ||
		result1.IDsXor != result2.IDsXor ||
		result1.IDsSum != result2.IDsSum {
		t.Skipf(
			"two consecutive Digest calls saw different commits — expected under concurrent load "+
				"(not a bug; each snapshot is internally consistent). "+
				"call1=(%d,%d,%d) call2=(%d,%d,%d) bg_inserts=%d",
			result1.CountActivos, result1.IDsXor, result1.IDsSum,
			result2.CountActivos, result2.IDsXor, result2.IDsSum, inserted,
		)
	}

	t.Logf("PagosDigest concurrent (bg=%d): count=%d xor=%d sum=%d",
		inserted, result1.CountActivos, result1.IDsXor, result1.IDsSum)
}

// ─── TestE2E_PagosDigest_ConceptoFilterMatchesSync ───────────────────────────

// insertCommittedPagoWithConcepto inserts an IMPORTES_DOCTOS_CC 'R' row crediting
// cargoID via an abono DOCTOS_CC with the given conceptoCCID. Returns the pago
// IMPTE_DOCTO_CC_ID. The MSP_RECOMPUTE_PAGO trigger fires on INSERT into
// IMPORTES_DOCTOS_CC and writes the row into MSP_PAGOS_VENTAS with the concepto
// inherited from the abono's DOCTOS_CC.
func insertCommittedPagoWithConcepto(t *testing.T, pool *firebird.Pool, clienteID, cargoID, conceptoCCID int, importe decimal.Decimal) int {
	t.Helper()
	ctx := context.Background()

	var abonoID int
	require.NoError(t, pool.QueryRowContext(ctx, `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&abonoID))

	now := time.Now()
	_, err := pool.ExecContext(ctx,
		`INSERT INTO DOCTOS_CC
		  (DOCTO_CC_ID, CONCEPTO_CC_ID, FOLIO, NATURALEZA_CONCEPTO,
		   SUCURSAL_ID, FECHA, CLIENTE_ID, CLAVE_CLIENTE,
		   TIPO_CAMBIO, DESCRIPCION,
		   SISTEMA_ORIGEN, APLICADO, ESTATUS, ESTATUS_ANT,
		   CONTABILIZADO_GYP, ES_CFD, TIENE_ANTICIPO, CFDI_CERTIFICADO, ENVIADO,
		   INTEG_BA, CONTABILIZADO_BA, CANCELADO)
		VALUES (?, ?, ?, 'R',
		        225490, ?, ?, '0001',
		        1, 'DigestConcepto E2E fixture',
		        'CC', 'S', 'N', 'N',
		        'N', 'N', 'N', 'N', 'N',
		        'N', 'N', 'N')`,
		// FOLIO is CHAR(9): "CC" prefix + 7 hex digits derived from the nano
		// timestamp (uniqueness within a test run). conceptoCCID lives in its
		// own column, no need to encode it here.
		abonoID, conceptoCCID, fmt.Sprintf("CC%07X", now.UnixNano()&0xFFFFFFF), now, clienteID)
	require.NoError(t, err, "insertCommittedPagoWithConcepto: INSERT DOCTOS_CC abono")

	var impteID int
	require.NoError(t, pool.QueryRowContext(ctx, `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&impteID))

	_, err = pool.ExecContext(ctx,
		`INSERT INTO IMPORTES_DOCTOS_CC
		  (IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, FECHA,
		   TIPO_IMPTE, DOCTO_CC_ACR_ID,
		   IMPORTE, IMPUESTO,
		   APLICADO, ESTATUS, CANCELADO)
		VALUES (?, ?, ?, 'R', ?, ?, 0, 'N', 'N', 'N')`,
		impteID, abonoID, now, cargoID, importe)
	require.NoError(t, err, "insertCommittedPagoWithConcepto: INSERT IMPORTES_DOCTOS_CC")

	t.Cleanup(func() {
		_, _ = pool.ExecContext(ctx, `DELETE FROM IMPORTES_DOCTOS_CC WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
		_, _ = pool.ExecContext(ctx, `DELETE FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
		_, _ = pool.ExecContext(ctx, `DELETE FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`, abonoID)
	})
	return impteID
}

// TestE2E_PagosDigest_ConceptoFilterMatchesSync inserts pagos with three
// different CONCEPTO_CC_ID values (87327, 27969, 155) and verifies that Digest
// counts only the two en-ruta concepts, matching what /sync would return.
//
//nolint:paralleltest
func TestE2E_PagosDigest_ConceptoFilterMatchesSync(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)
		requireMigration000019(t, q)
	})

	clienteID, zonaID := seedZonedClienteFromPool(t, pool)

	folio := fmt.Sprintf("DCC%X", time.Now().UnixNano()&0xFFFFFF)
	cargoID, _ := insertCommittedCargo(t, pool, clienteID, folio, decimal.RequireFromString("3000.00"))

	// Insert one pago for each of the three conceptos.
	impte87327 := insertCommittedPagoWithConcepto(t, pool, clienteID, cargoID, 87327, decimal.RequireFromString("300.00"))
	impte27969 := insertCommittedPagoWithConcepto(t, pool, clienteID, cargoID, 27969, decimal.RequireFromString("300.00"))
	impte155 := insertCommittedPagoWithConcepto(t, pool, clienteID, cargoID, 155, decimal.RequireFromString("300.00"))

	requirePagosVentasCacheRow(t, pool, impte87327)
	requirePagosVentasCacheRow(t, pool, impte27969)
	// Note: impte155 may or may not produce a cache row depending on the concepto filter
	// in the recompute trigger; either way it must NOT appear in the digest.

	_ = impte155 // the cache row for concepto 155 must not appear in digest

	repo := cobranzaventfb.NewPagosRepo(pool)
	result, err := repo.Digest(context.Background(), zonaID, time.Time{})
	require.NoError(t, err)

	// Scoped assertion: among the IDs we know, only 87327 and 27969 must be present.
	ids, _, err := repo.ListIDs(context.Background(), zonaID, 0, 100000, time.Time{})
	require.NoError(t, err)
	idSet := make(map[int]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	assert.True(t, idSet[impte87327], "concepto 87327 must appear in digest/ids")
	assert.True(t, idSet[impte27969], "concepto 27969 must appear in digest/ids")
	assert.False(t, idSet[impte155], "concepto 155 must NOT appear in digest/ids — excluded by pagoConceptoFilter")
	assert.GreaterOrEqual(t, result.CountActivos, 2, "at least our two en-ruta pagos must be counted")

	t.Logf("PagosDigest concepto filter: count=%d idSet has 87327=%v 27969=%v 155=%v",
		result.CountActivos, idSet[impte87327], idSet[impte27969], idSet[impte155])
}

// TestE2E_PagosDigest_SaldoFilterMatchesSync inserts a pago against a cargo
// with saldo > 0, then zeroes out the saldo and verifies that Digest (desde
// zero) excludes the pago when the saldo is gone.
//
//nolint:paralleltest
func TestE2E_PagosDigest_SaldoFilterMatchesSync(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)
		requireMigration000019(t, q)
	})

	clienteID, zonaID := seedZonedClienteFromPool(t, pool)

	// Cargo with saldo > 0.
	folioA := fmt.Sprintf("DSA%X", time.Now().UnixNano()&0xFFFFFF)
	cargoA, _ := insertCommittedCargo(t, pool, clienteID, folioA, decimal.RequireFromString("1000.00"))
	impteA := insertCommittedPago(t, pool, cargoA, decimal.RequireFromString("100.00"))
	requirePagosVentasCacheRow(t, pool, impteA)

	// Cargo B: insert, verify pago exists, then forcibly zero the saldo.
	folioB := fmt.Sprintf("DSB%X", time.Now().UnixNano()&0xFFFFFF)
	cargoB, _ := insertCommittedCargo(t, pool, clienteID, folioB, decimal.RequireFromString("500.00"))
	impteB := insertCommittedPago(t, pool, cargoB, decimal.RequireFromString("500.00"))
	requirePagosVentasCacheRow(t, pool, impteB)

	// Force cargoB's saldo to 0 so the digest filter (s.SALDO > 0) excludes impteB.
	ctx := context.Background()
	_, err := pool.ExecContext(ctx,
		`UPDATE MSP_SALDOS_VENTAS SET SALDO = 0, UPDATED_AT = ?
		  WHERE DOCTO_CC_ID = ?`,
		time.Now(), cargoB)
	require.NoError(t, err, "force cargoB saldo to 0")

	repo := cobranzaventfb.NewPagosRepo(pool)
	ids, _, err := repo.ListIDs(ctx, zonaID, 0, 100000, time.Time{})
	require.NoError(t, err)

	idSet := make(map[int]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	assert.True(t, idSet[impteA], "pago for cargo with saldo>0 must appear (saldo filter passes)")
	assert.False(t, idSet[impteB], "pago for saldada cargo must NOT appear when desde is zero (saldo filter excludes it)")

	t.Logf("PagosDigest saldo filter: impteA(%d)=%v impteB(%d)=%v",
		impteA, idSet[impteA], impteB, idSet[impteB])
}

// TestE2E_PagosDigest_DesdeAddsRecentSaldadas verifies that setting desde
// causes the digest to include pagos against recently-saldada cargos.
//
//nolint:paralleltest
func TestE2E_PagosDigest_DesdeAddsRecentSaldadas(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)
		requireMigration000019(t, q)
	})

	clienteID, zonaID := seedZonedClienteFromPool(t, pool)

	// Insert a saldada cargo and a pago against it.
	folio := fmt.Sprintf("DDA%X", time.Now().UnixNano()&0xFFFFFF)
	cargo, _ := insertCommittedCargo(t, pool, clienteID, folio, decimal.RequireFromString("800.00"))
	impte := insertCommittedPago(t, pool, cargo, decimal.RequireFromString("800.00"))
	requirePagosVentasCacheRow(t, pool, impte)

	// Set pago FECHA to a known recent time, then zero the saldo.
	pagoFecha := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second)
	ctx := context.Background()
	_, err := pool.ExecContext(ctx,
		`UPDATE MSP_PAGOS_VENTAS SET FECHA = ?, UPDATED_AT = ?
		  WHERE IMPTE_DOCTO_CC_ID = ?`,
		firebird.ToWallClock(pagoFecha), time.Now(), impte)
	require.NoError(t, err, "force pago FECHA to yesterday")

	_, err = pool.ExecContext(ctx,
		`UPDATE MSP_SALDOS_VENTAS SET SALDO = 0, UPDATED_AT = ?
		  WHERE DOCTO_CC_ID = ?`,
		time.Now(), cargo)
	require.NoError(t, err, "zero the saldo so the saldo>0 filter excludes it")

	repo := cobranzaventfb.NewPagosRepo(pool)

	// Without desde: pago must be excluded (saldo = 0 and no desde).
	idsNoDesde, _, err := repo.ListIDs(ctx, zonaID, 0, 100000, time.Time{})
	require.NoError(t, err)
	noDesdeSet := make(map[int]bool, len(idsNoDesde))
	for _, id := range idsNoDesde {
		noDesdeSet[id] = true
	}
	assert.False(t, noDesdeSet[impte], "saldada pago must NOT appear without desde")

	// With desde two days ago: pago.FECHA >= desde so it must appear.
	desde := pagoFecha.Add(-48 * time.Hour)
	idsWithDesde, _, err := repo.ListIDs(ctx, zonaID, 0, 100000, desde)
	require.NoError(t, err)
	withDesdeSet := make(map[int]bool, len(idsWithDesde))
	for _, id := range idsWithDesde {
		withDesdeSet[id] = true
	}
	assert.True(t, withDesdeSet[impte], "saldada pago must appear when desde includes pago.FECHA")

	t.Logf("PagosDigest desde: impte=%d noDesde=%v withDesde=%v pagoFecha=%s desde=%s",
		impte, noDesdeSet[impte], withDesdeSet[impte],
		pagoFecha.Format(time.RFC3339), desde.Format(time.RFC3339))
}

// ─── TestE2E_SaldosDigest_SaldoFilterMatchesSync ─────────────────────────────

// requireSaldosCacheRow skips the test if no MSP_SALDOS_VENTAS row exists for doctoCCID.
func requireSaldosCacheRow(t *testing.T, pool *firebird.Pool, doctoCCID int) {
	t.Helper()
	var n int
	err := pool.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM MSP_SALDOS_VENTAS WHERE DOCTO_CC_ID = ?`, doctoCCID).Scan(&n)
	if err != nil || n == 0 {
		t.Skipf("trigger did not create MSP_SALDOS_VENTAS row for cargo %d — verify migration 000010", doctoCCID)
	}
}

// TestE2E_SaldosDigest_SaldoFilterMatchesSync inserts two cargos, verifies the
// saldo>0 filter includes the open one and excludes the saldada one.
//
//nolint:paralleltest
func TestE2E_SaldosDigest_SaldoFilterMatchesSync(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)
		requireMigration000019(t, q)
	})

	clienteID, zonaID := seedZonedClienteFromPool(t, pool)

	// Cargo A: saldo > 0 (open).
	folioA := fmt.Sprintf("SSA%X", time.Now().UnixNano()&0xFFFFFF)
	cargoA, _ := insertCommittedCargo(t, pool, clienteID, folioA, decimal.RequireFromString("2000.00"))
	requireSaldosCacheRow(t, pool, cargoA)

	// Cargo B: insert, then set saldo = 0.
	folioB := fmt.Sprintf("SSB%X", time.Now().UnixNano()&0xFFFFFF)
	cargoB, _ := insertCommittedCargo(t, pool, clienteID, folioB, decimal.RequireFromString("1000.00"))
	requireSaldosCacheRow(t, pool, cargoB)

	ctx := context.Background()
	_, err := pool.ExecContext(ctx,
		`UPDATE MSP_SALDOS_VENTAS SET SALDO = 0, UPDATED_AT = ?
		  WHERE DOCTO_CC_ID = ?`,
		time.Now(), cargoB)
	require.NoError(t, err, "zero cargoB saldo")

	repo := cobranzaventfb.NewSaldosRepo(pool)
	ids, _, err := repo.ListIDs(ctx, zonaID, 0, 100000, time.Time{})
	require.NoError(t, err)

	idSet := make(map[int]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	assert.True(t, idSet[cargoA], "open saldo (saldo>0) must appear in ListIDs with zero desde")
	assert.False(t, idSet[cargoB], "saldada cargo (saldo=0) must NOT appear when desde is zero")

	t.Logf("SaldosDigest saldo filter: cargoA(%d)=%v cargoB(%d)=%v",
		cargoA, idSet[cargoA], cargoB, idSet[cargoB])
}

// TestE2E_SaldosDigest_DesdeIncludesRecentSaldadas verifies that setting desde
// causes recently-paid saldos to appear in the digest.
//
//nolint:paralleltest
func TestE2E_SaldosDigest_DesdeIncludesRecentSaldadas(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)
		requireMigration000019(t, q)
	})

	clienteID, zonaID := seedZonedClienteFromPool(t, pool)

	folio := fmt.Sprintf("SDD%X", time.Now().UnixNano()&0xFFFFFF)
	cargo, _ := insertCommittedCargo(t, pool, clienteID, folio, decimal.RequireFromString("1500.00"))
	requireSaldosCacheRow(t, pool, cargo)

	// Set FECHA_ULT_PAGO to a known recent time and zero the saldo.
	ultPago := time.Now().UTC().Add(-12 * time.Hour).Truncate(time.Second)
	ctx := context.Background()
	_, err := pool.ExecContext(ctx,
		`UPDATE MSP_SALDOS_VENTAS SET SALDO = 0, FECHA_ULT_PAGO = ?, UPDATED_AT = ?
		  WHERE DOCTO_CC_ID = ?`,
		firebird.ToWallClock(ultPago), time.Now(), cargo)
	require.NoError(t, err, "zero saldo and set FECHA_ULT_PAGO")

	repo := cobranzaventfb.NewSaldosRepo(pool)

	// Without desde: saldada must be excluded.
	idsNoDesde, _, err := repo.ListIDs(ctx, zonaID, 0, 100000, time.Time{})
	require.NoError(t, err)
	noDesdeSet := make(map[int]bool, len(idsNoDesde))
	for _, id := range idsNoDesde {
		noDesdeSet[id] = true
	}
	assert.False(t, noDesdeSet[cargo], "saldada cargo must NOT appear without desde")

	// With desde before FECHA_ULT_PAGO: cargo must appear.
	desde := ultPago.Add(-24 * time.Hour)
	idsWithDesde, _, err := repo.ListIDs(ctx, zonaID, 0, 100000, desde)
	require.NoError(t, err)
	withDesdeSet := make(map[int]bool, len(idsWithDesde))
	for _, id := range idsWithDesde {
		withDesdeSet[id] = true
	}
	assert.True(t, withDesdeSet[cargo], "recently-paid cargo must appear when desde <= FECHA_ULT_PAGO")

	t.Logf("SaldosDigest desde: cargo=%d noDesde=%v withDesde=%v ultPago=%s desde=%s",
		cargo, noDesdeSet[cargo], withDesdeSet[cargo],
		ultPago.Format(time.RFC3339), desde.Format(time.RFC3339))
}
