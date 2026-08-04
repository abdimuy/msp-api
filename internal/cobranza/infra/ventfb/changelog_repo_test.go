//nolint:misspell // Spanish vocabulary by project convention.
package ventfb_test

// Property tests for PagosChangelogRepo and SaldosChangelogRepo using
// pgregory.net/rapid. These tests drive the real Firebird database
// (integration-flavored) so they are gated on FB_DATABASE like all other
// Firebird tests in this package.
//
// Isolation strategy: each helper inserts rows directly into the changelog
// tables with PKs derived from time.Now().UnixNano() to avoid collision with
// production data. All assertions are scoped to the SEQ_IDs inserted by that
// run. Cleanup is registered via t.Cleanup so it runs even when rapid.Check
// shrinks a failure.

import (
	"context"
	"sort"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// ─── helpers de fixtures ──────────────────────────────────────────────────────

// insertPagosChangelogRows inserts n rows directly into MSP_PAGOS_CHANGELOG
// with distinct PKs derived from time.Now().UnixNano() and the given txID /
// commitAt. Returns the inserted SEQ_IDs sorted ascending. Registers cleanup.
func insertPagosChangelogRows(tb testing.TB, pool *firebird.Pool, n int, txID int64, commitAt time.Time) []int64 {
	tb.Helper()
	ctx := context.Background()

	seqIDs := make([]int64, 0, n)
	pks := make([]int, 0, n)

	// Base PK fits in Firebird INTEGER (max 2,147,483,647). We use the top
	// 40 million slots (2,100,000,000..2,139,999,999) as a test-safe range
	// that is unlikely to collide with Microsip data.
	base := int(time.Now().UnixNano()%40_000_000) + 1

	for i := range n {
		pk := 2_100_000_000 + base + i

		var seqID int64
		require.NoError(
			tb,
			pool.QueryRowContext(
				ctx,
				`SELECT GEN_ID(GEN_MSP_PAGOS_CHANGELOG_SEQ, 1) FROM RDB$DATABASE`,
			).Scan(&seqID),
			"obtener SEQ_ID del generador pagos",
		)

		_, err := pool.ExecContext(ctx,
			`INSERT INTO MSP_PAGOS_CHANGELOG (SEQ_ID, IMPTE_DOCTO_CC_ID, TX_ID, COMMIT_AT)
			 VALUES (?, ?, ?, ?)`,
			seqID, pk, txID, firebird.ToWallClock(commitAt))
		require.NoError(tb, err, "INSERT MSP_PAGOS_CHANGELOG fila %d", i)

		seqIDs = append(seqIDs, seqID)
		pks = append(pks, pk)
	}

	sort.Slice(seqIDs, func(i, j int) bool { return seqIDs[i] < seqIDs[j] })

	pksCopy := make([]int, len(pks))
	copy(pksCopy, pks)
	tb.Cleanup(func() {
		for _, pk := range pksCopy {
			_, _ = pool.ExecContext(ctx,
				`DELETE FROM MSP_PAGOS_CHANGELOG WHERE IMPTE_DOCTO_CC_ID = ?`, pk)
		}
	})

	return seqIDs
}

// ─── Property A: Since returns rows strictly ordered ascending by SEQ_ID ─────

// TestPropA_Since_OrderedAscending verifica que los resultados de Since estén
// estrictamente ordenados por SEQ_ID ascendente para cualquier sinceSeq y limit.
//
//nolint:paralleltest
func TestPropA_Since_OrderedAscending(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := cobranzaventfb.NewPagosChangelogRepo(pool)
	ctx := context.Background()

	// Insertar un conjunto base de filas con TX_ID=1 (siempre visible bajo SentinelNoActiveTx).
	baseSeqs := insertPagosChangelogRows(t, pool, 20, 1, time.Now())

	rapid.Check(t, func(rt *rapid.T) {
		limit := rapid.IntRange(1, 50).Draw(rt, "limit")
		sinceSeq := rapid.Int64Range(0, baseSeqs[len(baseSeqs)-1]).Draw(rt, "sinceSeq")

		entries, err := repo.Since(ctx, sinceSeq, cobranzaventfb.SentinelNoActiveTx, limit)
		if err != nil {
			rt.Fatalf("Since error: %v", err)
		}

		for i := 1; i < len(entries); i++ {
			if entries[i].SeqID <= entries[i-1].SeqID {
				rt.Fatalf("orden ascendente violado en posición %d: SeqID[%d]=%d <= SeqID[%d]=%d",
					i, i, entries[i].SeqID, i-1, entries[i-1].SeqID)
			}
		}
	})
}

// ─── Property B: Since filters strictly ──────────────────────────────────────

// TestPropB_Since_FiltersStrictly verifica que cada entrada devuelta cumpla
// SeqID > sinceSeq y TxID < watermark.
//
//nolint:paralleltest
func TestPropB_Since_FiltersStrictly(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := cobranzaventfb.NewPagosChangelogRepo(pool)
	ctx := context.Background()

	// Insertar filas con TX_IDs conocidos: 100, 200, 300.
	seqs100 := insertPagosChangelogRows(t, pool, 3, 100, time.Now())
	seqs200 := insertPagosChangelogRows(t, pool, 3, 200, time.Now())
	seqs300 := insertPagosChangelogRows(t, pool, 3, 300, time.Now())

	allSeqs := append(append(seqs100, seqs200...), seqs300...)
	sort.Slice(allSeqs, func(i, j int) bool { return allSeqs[i] < allSeqs[j] })
	minSeq := allSeqs[0]

	rapid.Check(t, func(rt *rapid.T) {
		sinceSeq := rapid.Int64Range(0, minSeq).Draw(rt, "sinceSeq")
		watermark := rapid.Int64Range(50, 350).Draw(rt, "watermark")
		limit := rapid.IntRange(1, 100).Draw(rt, "limit")

		entries, err := repo.Since(ctx, sinceSeq, watermark, limit)
		if err != nil {
			rt.Fatalf("Since error: %v", err)
		}

		for _, e := range entries {
			if e.SeqID <= sinceSeq {
				rt.Fatalf("filtro SEQ_ID > sinceSeq violado: SeqID=%d, sinceSeq=%d",
					e.SeqID, sinceSeq)
			}
			if e.TxID >= watermark {
				rt.Fatalf("filtro TX_ID < watermark violado: TxID=%d, watermark=%d",
					e.TxID, watermark)
			}
		}
	})
}

// ─── Property C: Since honors limit ──────────────────────────────────────────

// TestPropC_Since_HonorsLimit verifica que len(result) <= limit siempre.
//
//nolint:paralleltest
func TestPropC_Since_HonorsLimit(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := cobranzaventfb.NewPagosChangelogRepo(pool)
	ctx := context.Background()

	insertPagosChangelogRows(t, pool, 30, 1, time.Now())

	rapid.Check(t, func(rt *rapid.T) {
		limit := rapid.IntRange(1, 25).Draw(rt, "limit")

		entries, err := repo.Since(ctx, 0, cobranzaventfb.SentinelNoActiveTx, limit)
		if err != nil {
			rt.Fatalf("Since error: %v", err)
		}

		if len(entries) > limit {
			rt.Fatalf("limit violado: got %d entries, limit=%d", len(entries), limit)
		}
	})
}

// ─── Property D: MaxSeqID >= max of what Since returns ───────────────────────

// TestPropD_MaxSeqID_GteMaxSince verifica que MaxSeqID(watermark) sea mayor o
// igual al mayor SeqID devuelto por Since(0, watermark, largeLimit). La BD tiene
// otras filas además de las nuestras, así que afirmamos >= en vez de ==.
//
//nolint:paralleltest
func TestPropD_MaxSeqID_GteMaxSince(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := cobranzaventfb.NewPagosChangelogRepo(pool)
	ctx := context.Background()

	// Insertar filas con TX_IDs bajos para que sean visibles con distintos watermarks.
	insertPagosChangelogRows(t, pool, 10, 1, time.Now())
	insertPagosChangelogRows(t, pool, 5, 50, time.Now())

	rapid.Check(t, func(rt *rapid.T) {
		watermark := rapid.Int64Range(1, 200).Draw(rt, "watermark")

		maxSeq, err := repo.MaxSeqID(ctx, watermark)
		if err != nil {
			rt.Fatalf("MaxSeqID error: %v", err)
		}

		entries, err := repo.Since(ctx, 0, watermark, 100_000)
		if err != nil {
			rt.Fatalf("Since error: %v", err)
		}

		if len(entries) == 0 {
			// No hay filas visibles bajo este watermark: MaxSeqID debe ser >= 0.
			if maxSeq < 0 {
				rt.Fatalf("MaxSeqID negativo inesperado: %d", maxSeq)
			}
			return
		}

		// MaxSeqID global debe ser >= máximo devuelto por Since.
		var maxFromSince int64
		for _, e := range entries {
			if e.SeqID > maxFromSince {
				maxFromSince = e.SeqID
			}
		}
		if maxSeq < maxFromSince {
			rt.Fatalf("MaxSeqID=%d < max(Since)=%d — invariante violada",
				maxSeq, maxFromSince)
		}
	})
}

// ─── DeleteOlderThan never deletes rows with COMMIT_AT >= cutoff ─────────────

// TestDeleteOlderThan_RespectsCommitAt verifica que DeleteOlderThan solo
// elimine filas con COMMIT_AT < cutoff y deje intactas las filas recientes.
//
// Antes se llamaba TestPropE_* y corría dentro de rapid.Check, pero el cuerpo
// no invocaba un solo generador: los offsets de tiempo y los conteos de filas
// eran constantes. Las 100 iteraciones por omisión de rapid ejecutaban el
// escenario idéntico cien veces —unas 1300 idas y vueltas a Firebird— y
// tardaban ~264 s contra el límite de 240 s del target de integración, así que
// el paquete completo expiraba y bloqueaba todo push con código Go.
//
// Correr una vez verifica exactamente lo mismo en ~2.5 s. Si algún día se
// quiere una propiedad de verdad, hay que generar los conteos y los offsets
// (incluido el caso de borde: una fila con COMMIT_AT exactamente en el cutoff,
// que este escenario no cubre).
//
//nolint:paralleltest
func TestDeleteOlderThan_RespectsCommitAt(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := cobranzaventfb.NewPagosChangelogRepo(pool)
	ctx := context.Background()

	pastAt := time.Now().UTC().Add(-48 * time.Hour)
	futureAt := time.Now().UTC().Add(24 * time.Hour)

	// Insertar filas pasadas (deben eliminarse) y futuras (deben sobrevivir).
	pastSeqs := insertPagosChangelogRows(t, pool, 3, 1, pastAt)
	futureSeqs := insertPagosChangelogRows(t, pool, 3, 1, futureAt)

	cutoff := time.Now().UTC()

	_, err := repo.DeleteOlderThan(ctx, cutoff, 1000)
	require.NoError(t, err, "DeleteOlderThan")

	// Filas futuras deben seguir existiendo.
	for _, seq := range futureSeqs {
		var count int
		require.NoError(t,
			pool.QueryRowContext(
				ctx,
				`SELECT COUNT(*) FROM MSP_PAGOS_CHANGELOG WHERE SEQ_ID = ?`, seq,
			).Scan(&count),
			"verificar fila futura SEQ_ID=%d", seq,
		)
		assert.NotZero(t, count,
			"DeleteOlderThan borró fila futura SEQ_ID=%d (COMMIT_AT > cutoff)", seq)
	}

	// Filas pasadas deben haber sido eliminadas.
	for _, seq := range pastSeqs {
		var count int
		require.NoError(t,
			pool.QueryRowContext(
				ctx,
				`SELECT COUNT(*) FROM MSP_PAGOS_CHANGELOG WHERE SEQ_ID = ?`, seq,
			).Scan(&count),
			"verificar fila pasada SEQ_ID=%d", seq,
		)
		assert.Zero(t, count,
			"DeleteOlderThan dejó fila pasada SEQ_ID=%d (COMMIT_AT < cutoff)", seq)
	}
}
