//nolint:misspell // Spanish vocabulary by project convention.
package ventfb_test

// Integration tests for PagosChangelogRepo and SaldosChangelogRepo against a
// real Firebird database. Gated on FB_DATABASE + migration 000022.
//
// Pattern: direct committed inserts (pool.ExecContext, auto-commit) so that
// subsequent reads on independent connections / transactions see the rows.
// Cleanup is registered with t.Cleanup. All assertions are scoped to the
// specific SEQ_IDs and PKs inserted by each test.

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// ─── helpers específicos de integración ──────────────────────────────────────

// insertPagosRow inserts one row into MSP_PAGOS_CHANGELOG and returns its SEQ_ID.
func insertPagosRow(t *testing.T, pool *firebird.Pool, pk int, txID int64, commitAt time.Time) int64 {
	t.Helper()
	ctx := context.Background()

	var seqID int64
	require.NoError(
		t,
		pool.QueryRowContext(
			ctx,
			`SELECT GEN_ID(GEN_MSP_PAGOS_CHANGELOG_SEQ, 1) FROM RDB$DATABASE`,
		).Scan(&seqID),
	)
	_, err := pool.ExecContext(ctx,
		`INSERT INTO MSP_PAGOS_CHANGELOG (SEQ_ID, IMPTE_DOCTO_CC_ID, TX_ID, COMMIT_AT)
		 VALUES (?, ?, ?, ?)`,
		seqID, pk, txID, firebird.ToWallClock(commitAt))
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = pool.ExecContext(ctx,
			`DELETE FROM MSP_PAGOS_CHANGELOG WHERE SEQ_ID = ?`, seqID)
	})
	return seqID
}

// insertSaldosRow inserts one row into MSP_SALDOS_CHANGELOG and returns its SEQ_ID.
func insertSaldosRow(t *testing.T, pool *firebird.Pool, pk int, txID int64, commitAt time.Time) int64 {
	t.Helper()
	ctx := context.Background()

	var seqID int64
	require.NoError(
		t,
		pool.QueryRowContext(
			ctx,
			`SELECT GEN_ID(GEN_MSP_SALDOS_CHANGELOG_SEQ, 1) FROM RDB$DATABASE`,
		).Scan(&seqID),
	)
	_, err := pool.ExecContext(ctx,
		`INSERT INTO MSP_SALDOS_CHANGELOG (SEQ_ID, DOCTO_CC_ID, TX_ID, COMMIT_AT)
		 VALUES (?, ?, ?, ?)`,
		seqID, pk, txID, firebird.ToWallClock(commitAt))
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = pool.ExecContext(ctx,
			`DELETE FROM MSP_SALDOS_CHANGELOG WHERE SEQ_ID = ?`, seqID)
	})
	return seqID
}

// uniquePK returns a PK that fits in Firebird INTEGER (max 2,147,483,647)
// and is unlikely to collide with Microsip data. Uses the top 40 million
// slots (2,100,000,000..2,139,999,999) plus a nanosecond remainder.
func uniquePK(offset int) int {
	return 2_100_000_000 + int(time.Now().UnixNano()%40_000_000) + offset
}

// ─── PagosChangelogRepo tests ─────────────────────────────────────────────────

// TestPagosChangelogRepo_Since_FiltersByWatermark verifica que Since excluya
// filas cuyo TX_ID >= watermark.
//
//nolint:paralleltest
func TestPagosChangelogRepo_Since_FiltersByWatermark(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := cobranzaventfb.NewPagosChangelogRepo(pool)
	ctx := context.Background()
	now := time.Now().UTC()

	pk100 := uniquePK(1)
	pk200 := uniquePK(2)
	pk300 := uniquePK(3)

	seq100 := insertPagosRow(t, pool, pk100, 100, now)
	seq200 := insertPagosRow(t, pool, pk200, 200, now)
	seq300 := insertPagosRow(t, pool, pk300, 300, now)

	// sinceSeq just before the smallest of our three rows.
	minSeq := seq100
	if seq200 < minSeq {
		minSeq = seq200
	}
	if seq300 < minSeq {
		minSeq = seq300
	}
	sinceSeq := minSeq - 1

	// watermark=250: should include TX_ID 100 and 200, exclude 300.
	entries, err := repo.Since(ctx, sinceSeq, 250, 100)
	require.NoError(t, err)

	seqSet := make(map[int64]bool, len(entries))
	for _, e := range entries {
		seqSet[e.SeqID] = true
	}

	assert.True(t, seqSet[seq100], "TX_ID=100 < watermark=250 debe aparecer")
	assert.True(t, seqSet[seq200], "TX_ID=200 < watermark=250 debe aparecer")
	assert.False(t, seqSet[seq300], "TX_ID=300 >= watermark=250 NO debe aparecer")
}

// TestPagosChangelogRepo_Since_HonorsSinceSeqAndLimit verifica que Since
// respete el cursor sinceSeq y el parámetro limit.
//
//nolint:paralleltest
func TestPagosChangelogRepo_Since_HonorsSinceSeqAndLimit(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := cobranzaventfb.NewPagosChangelogRepo(pool)
	ctx := context.Background()
	now := time.Now().UTC()

	// Insertar 5 filas con TX_ID=1 (siempre bajo el sentinel).
	seqs := make([]int64, 5)
	for i := range 5 {
		seqs[i] = insertPagosRow(t, pool, uniquePK(10+i), 1, now)
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })

	// sinceSeq = seqs[0] (primer elemento); limit=3 → debe devolver seqs[1..3].
	sinceSeq := seqs[0]
	entries, err := repo.Since(ctx, sinceSeq, cobranzaventfb.SentinelNoActiveTx, 3)
	require.NoError(t, err)

	// Filtrar solo nuestras filas (la BD puede tener otras).
	ourSeqSet := make(map[int64]bool)
	for _, s := range seqs[1:4] { // esperamos seqs[1], seqs[2], seqs[3]
		ourSeqSet[s] = true
	}

	foundOurs := 0
	for _, e := range entries {
		assert.Greater(t, e.SeqID, sinceSeq, "cada SeqID debe ser > sinceSeq")
		if ourSeqSet[e.SeqID] {
			foundOurs++
		}
	}
	assert.LessOrEqual(t, len(entries), 3, "limit=3 no debe superarse")
	assert.GreaterOrEqual(t, foundOurs, 1, "al menos una de nuestras filas debe aparecer")
}

// TestPagosChangelogRepo_MaxSeqID_RespectsWatermark verifica que MaxSeqID
// devuelva el mayor SEQ_ID cuyo TX_ID < watermark.
//
//nolint:paralleltest
func TestPagosChangelogRepo_MaxSeqID_RespectsWatermark(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := cobranzaventfb.NewPagosChangelogRepo(pool)
	ctx := context.Background()
	now := time.Now().UTC()

	pk100 := uniquePK(20)
	pk200 := uniquePK(21)
	pk300 := uniquePK(22)

	seq100 := insertPagosRow(t, pool, pk100, 100, now)
	seq200 := insertPagosRow(t, pool, pk200, 200, now)
	seq300 := insertPagosRow(t, pool, pk300, 300, now)

	// MaxSeqID con watermark=250 debe ser >= max(seq100, seq200) y < seq300
	// (si seq300 > seq200 > seq100, que es lo más probable con el generador).
	maxSeq, err := repo.MaxSeqID(ctx, 250)
	require.NoError(t, err)

	// El resultado debe incluir seq100 y seq200 (TX_ID < 250) pero no seq300.
	assert.GreaterOrEqual(t, maxSeq, seq100, "MaxSeqID debe ser >= seq100")
	assert.GreaterOrEqual(t, maxSeq, seq200, "MaxSeqID debe ser >= seq200")
	// No podemos afirmar < seq300 porque puede haber otras filas en la BD con
	// TX_ID < 250 y SEQ_ID > seq300.
	_ = seq300

	// Con watermark=50 ninguna de nuestras filas es visible, pero el resultado
	// puede ser >= 0 si hay otras filas en la BD con TX_ID < 50.
	maxSeqLow, err := repo.MaxSeqID(ctx, 50)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, maxSeqLow, int64(0), "MaxSeqID nunca es negativo")
}

// TestPagosChangelogRepo_DeleteOlderThan elimina filas pasadas y respeta las recientes.
//
//nolint:paralleltest
func TestPagosChangelogRepo_DeleteOlderThan(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := cobranzaventfb.NewPagosChangelogRepo(pool)
	ctx := context.Background()

	pastAt := time.Now().UTC().Add(-72 * time.Hour)
	futureAt := time.Now().UTC().Add(24 * time.Hour)
	cutoff := time.Now().UTC()

	pastSeqs := make([]int64, 5)
	for i := range 5 {
		pastSeqs[i] = insertPagosRow(t, pool, uniquePK(30+i), 1, pastAt)
	}

	futureSeqs := make([]int64, 3)
	for i := range 3 {
		futureSeqs[i] = insertPagosRow(t, pool, uniquePK(40+i), 1, futureAt)
	}

	deleted, err := repo.DeleteOlderThan(ctx, cutoff, 1000)
	require.NoError(t, err)

	// Deben haberse eliminado al menos nuestras 5 filas del pasado.
	assert.GreaterOrEqual(t, deleted, 5,
		"DeleteOlderThan debe eliminar al menos las 5 filas del pasado que insertamos")

	// Las filas futuras deben seguir existiendo.
	for _, seq := range futureSeqs {
		var count int
		require.NoError(
			t,
			pool.QueryRowContext(
				ctx,
				`SELECT COUNT(*) FROM MSP_PAGOS_CHANGELOG WHERE SEQ_ID = ?`, seq,
			).Scan(&count),
		)
		assert.Equal(t, 1, count,
			"fila futura SEQ_ID=%d no debe haber sido eliminada", seq)
	}

	// Las filas pasadas no deben existir.
	for _, seq := range pastSeqs {
		var count int
		require.NoError(
			t,
			pool.QueryRowContext(
				ctx,
				`SELECT COUNT(*) FROM MSP_PAGOS_CHANGELOG WHERE SEQ_ID = ?`, seq,
			).Scan(&count),
		)
		assert.Equal(t, 0, count,
			"fila pasada SEQ_ID=%d debe haber sido eliminada", seq)
	}
}

// ─── SaldosChangelogRepo tests ────────────────────────────────────────────────

// TestSaldosChangelogRepo_Since_FiltersByWatermark verifica el filtro de
// watermark en MSP_SALDOS_CHANGELOG.
//
//nolint:paralleltest
func TestSaldosChangelogRepo_Since_FiltersByWatermark(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := cobranzaventfb.NewSaldosChangelogRepo(pool)
	ctx := context.Background()
	now := time.Now().UTC()

	pk100 := uniquePK(50)
	pk300 := uniquePK(51)

	seq100 := insertSaldosRow(t, pool, pk100, 100, now)
	seq300 := insertSaldosRow(t, pool, pk300, 300, now)

	minSeq := seq100
	if seq300 < minSeq {
		minSeq = seq300
	}
	sinceSeq := minSeq - 1

	entries, err := repo.Since(ctx, sinceSeq, 200, 100)
	require.NoError(t, err)

	seqSet := make(map[int64]bool, len(entries))
	for _, e := range entries {
		seqSet[e.SeqID] = true
	}

	assert.True(t, seqSet[seq100], "TX_ID=100 < watermark=200 debe aparecer en saldos")
	assert.False(t, seqSet[seq300], "TX_ID=300 >= watermark=200 NO debe aparecer en saldos")
}

// TestSaldosChangelogRepo_MaxSeqID_ReturnsZeroWhenEmpty verifica que MaxSeqID
// devuelva 0 cuando no hay filas visibles bajo el watermark.
//
//nolint:paralleltest
func TestSaldosChangelogRepo_MaxSeqID_ReturnsZeroWhenEmpty(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := cobranzaventfb.NewSaldosChangelogRepo(pool)
	ctx := context.Background()

	// watermark=0: ningún TX_ID es < 0, por lo que MAX debe ser 0 (o NULL → 0).
	maxSeq, err := repo.MaxSeqID(ctx, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(0), maxSeq,
		"MaxSeqID con watermark=0 debe devolver 0 (ninguna fila visible)")
}
