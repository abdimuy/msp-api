//nolint:misspell // Spanish vocabulary (pago, cobranza, recibido) by project convention.
package ventfb_test

// Integration tests for the pago_recibido_id enrichment added to
// selectPagoColsP in pagos_repo.go.
//
// Goal: expose MSP_PAGOS_RECIBIDOS.ID (the client-generated pago UUID) on the
// sync/by-ids projection so the Android app can exact-match a numeric synced
// pago to its local UUID row. Resolved via a correlated SCALAR SUBQUERY
// (MIN(pr.ID) WHERE pr.IMPTE_DOCTO_CC_ID = p.IMPTE_DOCTO_CC_ID) rather than a
// JOIN, so that pagoFromClause itself never changes — both the sync/by-ids
// projection and the digest/ListIDs queries in digest_query.go keep sharing
// the exact same FROM clause, and a duplicate MSP_PAGOS_RECIBIDOS row can
// never fan out a MSP_PAGOS_VENTAS row into duplicates (see
// TestE2E_PagosRepo_SyncPorZona_PagoRecibidoID_NoFanOutOnDuplicateRecibidos).

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// insertPagoRecibidoLinked inserts a minimal MSP_PAGOS_RECIBIDOS row already
// "aplicada" (ESTADO='A') and linked to impteDoctoCCID, mirroring the state
// AplicarPago leaves once MarcarAplicada persists the Microsip-assigned
// IMPTE_DOCTO_CC_ID back onto the row (see pagos_recibidos_repo.go
// updatePagoRecibidoSQL). Only the columns this test cares about are set; the
// rest tolerate NULL per migration 000016.
func insertPagoRecibidoLinked(t *testing.T, q firebird.Querier, id uuid.UUID, impteDoctoCCID int) {
	t.Helper()
	now := time.Now()
	_, err := q.ExecContext(context.Background(),
		`INSERT INTO MSP_PAGOS_RECIBIDOS
		   (ID, FECHA, IMPTE_DOCTO_CC_ID, ESTADO, INTENTOS, RECEIVED_AT, UPDATED_AT)
		 VALUES (?, ?, ?, 'A', 0, ?, ?)`,
		id.String(), now, impteDoctoCCID, now, now)
	require.NoError(t, err, "insertPagoRecibidoLinked: INSERT MSP_PAGOS_RECIBIDOS")
}

// TestE2E_PagosRepo_SyncPorZona_PagoRecibidoID_Matches verifies that when a
// MSP_PAGOS_RECIBIDOS row exists with IMPTE_DOCTO_CC_ID matching the synced
// pago, SyncPorZona returns Pago.PagoRecibidoID() == that row's UUID.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRepo_SyncPorZona_PagoRecibidoID_Matches(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)

		clienteID, zonaID := seedZonedCliente(t, q)

		importe := decimal.RequireFromString("1000.00")
		cargoID := insertCargoDoctosCC(t, q, clienteID, "PAG-REC-1", importe)
		// Partial pago: saldo queda > 0, así que el sync normal (sin desde) lo
		// recoge sin necesitar la ventana `desde`.
		pagoImporte := decimal.RequireFromString("300.00")
		impteID := insertPagoImporte(t, q, cargoID, pagoImporte)
		afterID := impteID - 1

		// Same TX_ID-forcing dance as TestE2E_PagosRepo_SyncPorZona_SaldadaConDesde:
		// rows written inside this rollback-only tx carry the tx's own TX_ID,
		// which the watermark treats as "in flight" unless forced below it.
		forcePagoTxID(t, q, impteID, 1)
		forceSaldoTxID(t, q, cargoID, 1)

		recibidoID := uuid.New()
		insertPagoRecibidoLinked(t, q, recibidoID, impteID)

		// Wait out the clock-skew margin (syncClockSkewSeconds = 1s), same as
		// TestE2E_PagosRepo_SyncPorZona_SaldadaConDesde — otherwise the upper
		// bound (server_now minus skew) can fall before UPDATED_AT.
		time.Sleep(2 * time.Second)

		repo := cobranzaventfb.NewPagosRepo(pool)
		page, err := repo.SyncPorZona(ctx, zonaID, time.Time{}, afterID, 5000, time.Time{})
		require.NoError(t, err)

		p := findPagoByCargo(page.Items, cargoID)
		require.NotNil(t, p, "pago must appear in sync (saldo > 0, no desde needed)")
		require.NotNil(t, p.PagoRecibidoID(), "pago_recibido_id must be populated when a matching MSP_PAGOS_RECIBIDOS row exists")
		assert.Equal(t, recibidoID.String(), *p.PagoRecibidoID())
	})
}

// TestE2E_PagosRepo_SyncPorZona_PagoRecibidoID_NilWhenLegacy verifies that a
// pago with NO matching MSP_PAGOS_RECIBIDOS row (legacy pago, e.g. captured
// via the old Node app or direct Microsip entry) syncs with
// PagoRecibidoID() == nil rather than being excluded or erroring.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRepo_SyncPorZona_PagoRecibidoID_NilWhenLegacy(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)

		clienteID, zonaID := seedZonedCliente(t, q)

		importe := decimal.RequireFromString("1000.00")
		cargoID := insertCargoDoctosCC(t, q, clienteID, "PAG-REC-2", importe)
		pagoImporte := decimal.RequireFromString("300.00")
		impteID := insertPagoImporte(t, q, cargoID, pagoImporte)
		afterID := impteID - 1

		forcePagoTxID(t, q, impteID, 1)
		forceSaldoTxID(t, q, cargoID, 1)

		// Deliberately no insertPagoRecibidoLinked call — this pago has no
		// MSP_PAGOS_RECIBIDOS row.

		// Wait out the clock-skew margin (syncClockSkewSeconds = 1s).
		time.Sleep(2 * time.Second)

		repo := cobranzaventfb.NewPagosRepo(pool)
		page, err := repo.SyncPorZona(ctx, zonaID, time.Time{}, afterID, 5000, time.Time{})
		require.NoError(t, err)

		p := findPagoByCargo(page.Items, cargoID)
		require.NotNil(t, p, "pago must appear in sync (saldo > 0, no desde needed)")
		assert.Nil(t, p.PagoRecibidoID(), "pago_recibido_id must be nil for a legacy pago with no MSP_PAGOS_RECIBIDOS row")
	})
}

// TestE2E_PagosRepo_ByIDs_PagoRecibidoID_Matches verifies ByIDs enriches the
// same way as SyncPorZona (both share selectPagoColsP + pagoFromClause).
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRepo_ByIDs_PagoRecibidoID_Matches(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)

		clienteID, zonaID := seedZonedCliente(t, q)

		importe := decimal.RequireFromString("1000.00")
		cargoID := insertCargoDoctosCC(t, q, clienteID, "PAG-REC-3", importe)
		pagoImporte := decimal.RequireFromString("300.00")
		impteID := insertPagoImporte(t, q, cargoID, pagoImporte)

		recibidoID := uuid.New()
		insertPagoRecibidoLinked(t, q, recibidoID, impteID)

		repo := cobranzaventfb.NewPagosRepo(pool)
		rows, err := repo.ByIDs(ctx, zonaID, []int{impteID}, time.Time{})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.NotNil(t, rows[0].PagoRecibidoID())
		assert.Equal(t, recibidoID.String(), *rows[0].PagoRecibidoID())
	})
}

// TestE2E_PagosRepo_SyncPorZona_PagoRecibidoID_NoFanOutOnDuplicateRecibidos
// is the regression test for the hardening in pagos_repo.go: pago_recibido_id
// is resolved via a correlated scalar subquery (MIN(pr.ID) ... WHERE
// pr.IMPTE_DOCTO_CC_ID = p.IMPTE_DOCTO_CC_ID), NOT a JOIN, precisely so a
// duplicate MSP_PAGOS_RECIBIDOS row can never fan out the sync result.
//
// Seeds TWO MSP_PAGOS_RECIBIDOS rows sharing the SAME non-null
// IMPTE_DOCTO_CC_ID (both "pointing" at one MSP_PAGOS_VENTAS row — a state
// that should not occur in practice since AplicarPago assigns
// IMPTE_DOCTO_CC_ID to exactly one row, but is not prevented by any DB
// constraint) and asserts SyncPorZona still returns EXACTLY ONE pago row for
// that IMPTE, with pago_recibido_id equal to the lexicographically smaller
// (MIN) of the two UUIDs.
//
// Without the MIN-subquery — i.e. with a plain
// `LEFT JOIN MSP_PAGOS_RECIBIDOS pr ON pr.IMPTE_DOCTO_CC_ID = p.IMPTE_DOCTO_CC_ID`
// — this exact fixture would return TWO rows for the same IMPTE (one per
// matching MSP_PAGOS_RECIBIDOS row), which is the regression this test
// guards against.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRepo_SyncPorZona_PagoRecibidoID_NoFanOutOnDuplicateRecibidos(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)

		clienteID, zonaID := seedZonedCliente(t, q)

		importe := decimal.RequireFromString("1000.00")
		cargoID := insertCargoDoctosCC(t, q, clienteID, "PAG-REC-4", importe)
		pagoImporte := decimal.RequireFromString("300.00")
		impteID := insertPagoImporte(t, q, cargoID, pagoImporte)
		afterID := impteID - 1

		forcePagoTxID(t, q, impteID, 1)
		forceSaldoTxID(t, q, cargoID, 1)

		// Two MSP_PAGOS_RECIBIDOS rows sharing the same IMPTE_DOCTO_CC_ID.
		recibidoA := uuid.New()
		recibidoB := uuid.New()
		insertPagoRecibidoLinked(t, q, recibidoA, impteID)
		insertPagoRecibidoLinked(t, q, recibidoB, impteID)

		wantMin := recibidoA.String()
		if recibidoB.String() < wantMin {
			wantMin = recibidoB.String()
		}

		time.Sleep(2 * time.Second)

		repo := cobranzaventfb.NewPagosRepo(pool)
		page, err := repo.SyncPorZona(ctx, zonaID, time.Time{}, afterID, 5000, time.Time{})
		require.NoError(t, err)

		var matches []int
		for i := range page.Items {
			if page.Items[i].ImpteDoctoCCID() == impteID {
				matches = append(matches, i)
			}
		}
		require.Len(t, matches, 1,
			"a duplicate MSP_PAGOS_RECIBIDOS row must NOT fan out the pago row — expected exactly 1 match for impte=%d, got %d", impteID, len(matches))

		p := page.Items[matches[0]]
		require.NotNil(t, p.PagoRecibidoID())
		assert.Equal(t, wantMin, *p.PagoRecibidoID(), "pago_recibido_id must be MIN(pr.ID) of the two duplicate rows")
	})
}

// TestE2E_PagosDigest_UnaffectedByPagoRecibidoJoin proves the digest/ListIDs
// path is provably unchanged by the pago_recibido_id feature: Digest and
// ListIDs are computed BEFORE and AFTER inserting a linked MSP_PAGOS_RECIBIDOS
// row for the same pago, and results must be bit-for-bit identical. This
// guards digest_query.go, which shares pagoFromClause with the sync/by-ids
// projection — pago_recibido_id is resolved by selectPagoColsP's scalar
// subquery, not by any change to the shared FROM clause, so digest/ListIDs
// (which don't use selectPagoColsP at all) cannot be affected even in
// principle.
//
// Uses the committed-fixture pattern from digest_query_integration_test.go
// (Digest/ListIDs run under snapshot isolation and only see committed rows).
//
//nolint:paralleltest
func TestE2E_PagosDigest_UnaffectedByPagoRecibidoJoin(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)
		requireMigration000019(t, q)
	})

	clienteID, zonaID := seedZonedClienteFromPool(t, pool)

	folio := fmt.Sprintf("PRJ%X", time.Now().UnixNano()&0xFFFFFF)
	cargoID, _ := insertCommittedCargo(t, pool, clienteID, folio, decimal.RequireFromString("2000.00"))
	impteID := insertCommittedPago(t, pool, cargoID, decimal.RequireFromString("900.00"))
	requirePagosVentasCacheRow(t, pool, impteID)

	repo := cobranzaventfb.NewPagosRepo(pool)
	ctx := context.Background()

	digestBefore, err := repo.Digest(ctx, zonaID, time.Time{})
	require.NoError(t, err)
	idsBefore, hasMoreBefore, err := repo.ListIDs(ctx, zonaID, 0, 100000, time.Time{})
	require.NoError(t, err)

	// Insert the MSP_PAGOS_RECIBIDOS row committed (auto-commit via pool),
	// mirroring the digest fixtures' pattern, so there is a real row for the
	// pago_recibido_id subquery to (not) affect the digest/ListIDs path with.
	recibidoID := uuid.New()
	now := time.Now()
	_, err = pool.ExecContext(ctx,
		`INSERT INTO MSP_PAGOS_RECIBIDOS
		   (ID, FECHA, IMPTE_DOCTO_CC_ID, ESTADO, INTENTOS, RECEIVED_AT, UPDATED_AT)
		 VALUES (?, ?, ?, 'A', 0, ?, ?)`,
		recibidoID.String(), now, impteID, now, now)
	require.NoError(t, err, "insert committed MSP_PAGOS_RECIBIDOS row")
	t.Cleanup(func() {
		_, _ = pool.ExecContext(context.Background(),
			`DELETE FROM MSP_PAGOS_RECIBIDOS WHERE ID = ?`, recibidoID.String())
	})

	digestAfter, err := repo.Digest(ctx, zonaID, time.Time{})
	require.NoError(t, err)
	idsAfter, hasMoreAfter, err := repo.ListIDs(ctx, zonaID, 0, 100000, time.Time{})
	require.NoError(t, err)

	assert.Equal(t, digestBefore.CountActivos, digestAfter.CountActivos, "Digest.CountActivos must be unchanged by the join")
	assert.Equal(t, digestBefore.IDsXor, digestAfter.IDsXor, "Digest.IDsXor must be unchanged by the join")
	assert.Equal(t, digestBefore.IDsSum, digestAfter.IDsSum, "Digest.IDsSum must be unchanged by the join")
	assert.True(t, digestBefore.MaxUpdatedAt.Equal(digestAfter.MaxUpdatedAt), "Digest.MaxUpdatedAt must be unchanged by the join")
	assert.Equal(t, hasMoreBefore, hasMoreAfter, "ListIDs hasMore must be unchanged by the join")
	assert.Equal(t, idsBefore, idsAfter, "ListIDs id set must be unchanged by the join")

	t.Logf("digest unaffected: before=(%d,%d,%d) after=(%d,%d,%d) len(idsBefore)=%d len(idsAfter)=%d",
		digestBefore.CountActivos, digestBefore.IDsXor, digestBefore.IDsSum,
		digestAfter.CountActivos, digestAfter.IDsXor, digestAfter.IDsSum,
		len(idsBefore), len(idsAfter))
}
