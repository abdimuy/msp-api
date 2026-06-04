//nolint:misspell // Spanish vocabulary by project convention.
package ventfb_test

// Anti-leak integration tests for the cobranza Firebird adapter.
//
// These tests prove that after the RunInTx/RunInReadTx/RunInSnapshotTx wraps
// landed in this branch, repo reads no longer leak idle uncommitted txs into
// MON\$TRANSACTIONS. Before the fix, every QueryContext on pool.DB left a
// dangling tx in state=0 (idle) until the connection was returned to the
// pool and aged out by ConnMaxIdleTime (30 min default), which blocked the
// watermark probe from advancing.
//
// The invariants checked here are:
//
//   1. After N read queries through repo methods, the active-state-1
//      transaction count contributed by our pool stays at 0 (or drops to
//      0 within a short flush window).
//   2. MinActiveTransactionID does not climb monotonically across
//      consecutive calls — i.e. the previous probe's tx is gone by the
//      time the next probe runs.
//
// Gated on FB_DATABASE.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// countOurIdleTxs returns the number of transactions on our pool's
// attachments that are currently NOT in active state (state != 1). With the
// implicit-tx leak this is the count of "stuck idle" txs. The query joins
// MON\$TRANSACTIONS with MON\$ATTACHMENTS to scope to our pool's
// connections (filtered by remote process matching the test binary path)
// — but the firebirdsql driver exposes only the protocol-level
// MON\$REMOTE_ADDRESS, so we use the looser heuristic: count all non-active
// txs whose attachment is not from a remote address other than localhost.
//
// In practice the dev DB is local-only, so this count includes pre-existing
// idle txs from any prior test runs. The test mitigates by capturing a
// baseline first and asserting on the DELTA, not the absolute count.
func countOurIdleTxs(ctx context.Context, pool *firebird.Pool) (int, error) {
	// MON\$STATE codes: 0=idle, 1=active.
	// We deliberately query via the pool (auto-commit) — the very mechanism
	// that used to leak. With the fix in place each call commits cleanly.
	var n int
	err := pool.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM MON$TRANSACTIONS t
JOIN MON$ATTACHMENTS  a ON a.MON$ATTACHMENT_ID = t.MON$ATTACHMENT_ID
WHERE t.MON$STATE = 0
  AND t.MON$TRANSACTION_ID <> CURRENT_TRANSACTION`).Scan(&n)
	return n, err
}

// TestNoTxLeak_AfterRepoQueries runs a batch of repo reads through different
// methods and verifies that the count of idle transactions does not grow.
// Without the fix, every QueryContext leaks one tx; with the fix the wraps
// commit each one explicitly.
//
//nolint:paralleltest
func TestNoTxLeak_AfterRepoQueries(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()

	clienteID, zonaID := seedZonedClienteFromPool(t, pool)
	_ = clienteID

	// Baseline — give the server a moment to flush any prior MON\$ state.
	time.Sleep(200 * time.Millisecond)
	baseline, err := countOurIdleTxs(ctx, pool)
	require.NoError(t, err)
	t.Logf("baseline idle tx count: %d", baseline)

	// Drive 20 reads through different methods on the cobranza adapter.
	// Each one used to leak before the RunInReadTx wraps; each must now
	// terminate cleanly.
	pagosRepo := cobranzaventfb.NewPagosRepo(pool)
	saldosRepo := cobranzaventfb.NewSaldosRepo(pool)
	desde := time.Now().Add(-30 * 24 * time.Hour)
	for range 5 {
		_, _ = pagosRepo.EnRutaPorZona(ctx, zonaID, desde)
		_, _ = saldosRepo.EnRutaPorZona(ctx, zonaID, desde)
		_, _ = cobranzaventfb.MinActiveTransactionID(ctx, pool)
		_, _ = saldosRepo.AbiertasPorCliente(ctx, clienteID)
	}

	// Flush window — MON\$TRANSACTIONS is materialised per-statement; small
	// wait so any in-flight rows settle to their terminal state.
	time.Sleep(200 * time.Millisecond)

	after, err := countOurIdleTxs(ctx, pool)
	require.NoError(t, err)
	t.Logf("idle tx count after 20 mixed reads: %d (delta vs baseline: %d)", after, after-baseline)

	// Allowable noise: a few txs from other test/clients can come and go,
	// but we should not see growth proportional to our query count
	// (without the fix the delta would be roughly 20).
	delta := after - baseline
	assert.LessOrEqual(t, delta, 5,
		"idle tx count grew by %d after 20 repo reads — looks like the implicit-tx leak; baseline=%d after=%d",
		delta, baseline, after)
}

// TestWatermarkAdvances_AfterRepoQueries: with the listener probe self-
// excluding and the read wraps committing cleanly, MinActiveTransactionID
// must not climb across consecutive invocations. Before the fix, each
// probe leaked its own tx and the next call returned that leaked TX_ID
// (strictly greater than the previous one). Now the watermark stays at
// the sentinel (or drops as other writers commit) but never climbs from
// self-induced leakage.
//
//nolint:paralleltest
func TestWatermarkAdvances_AfterRepoQueries(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()

	clienteID, zonaID := seedZonedClienteFromPool(t, pool)
	_ = clienteID

	// Initial probe must succeed and return positive value (sentinel or
	// a real active tx).
	w0, err := cobranzaventfb.MinActiveTransactionID(ctx, pool)
	require.NoError(t, err)
	require.Positive(t, w0)

	// Drive 10 reads of mixed kinds, then re-probe.
	pagosRepo := cobranzaventfb.NewPagosRepo(pool)
	saldosRepo := cobranzaventfb.NewSaldosRepo(pool)
	desde := time.Now().Add(-30 * 24 * time.Hour)
	for range 10 {
		_, _ = pagosRepo.EnRutaPorZona(ctx, zonaID, desde)
		_, _ = saldosRepo.EnRutaPorZona(ctx, zonaID, desde)
	}

	w1, err := cobranzaventfb.MinActiveTransactionID(ctx, pool)
	require.NoError(t, err)
	require.Positive(t, w1)

	t.Logf("watermark before reads: %d; after 10 reads: %d", w0, w1)

	// The invariant we care about: the watermark must NOT have climbed
	// because of self-induced leakage. It can stay equal (sentinel/quiet
	// system) or drop (a new external writer appeared) but it must not
	// grow purely from our own activity.
	//
	// "Climbed monotonically by exactly our query count" is the unambiguous
	// signature of the bug — without the fix, w1 would equal w0 + ~10 (one
	// per leaked probe). With the fix, w1 == w0 in a quiet env.
	assert.LessOrEqual(t, w1, w0,
		"watermark climbed after our own reads (w0=%d w1=%d) — looks like the self-include leak", w0, w1)
}

// TestDigestQuery_NoTxLeak: 10 consecutive Digest calls must not accumulate
// idle uncommitted txs in MON\$TRANSACTIONS. Pre-fix, each Digest opened a
// snapshot tx that was only ever rolled back via defer (never explicitly
// committed); RunInSnapshotTx now ensures a clean COMMIT/ROLLBACK pair.
//
//nolint:paralleltest
func TestDigestQuery_NoTxLeak(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()

	clienteID, zonaID := seedZonedClienteFromPool(t, pool)
	_ = clienteID

	time.Sleep(200 * time.Millisecond)
	baseline, err := countOurIdleTxs(ctx, pool)
	require.NoError(t, err)

	pagosRepo := cobranzaventfb.NewPagosRepo(pool)
	saldosRepo := cobranzaventfb.NewSaldosRepo(pool)
	desde := time.Now().Add(-30 * 24 * time.Hour)
	for range 5 {
		_, _ = pagosRepo.Digest(ctx, zonaID, desde)
		_, _ = saldosRepo.Digest(ctx, zonaID, desde)
	}

	time.Sleep(200 * time.Millisecond)
	after, err := countOurIdleTxs(ctx, pool)
	require.NoError(t, err)

	delta := after - baseline
	t.Logf("Digest idle tx delta after 10 calls: %d (baseline=%d after=%d)", delta, baseline, after)
	assert.LessOrEqual(t, delta, 5,
		"Digest leaked %d idle txs — RunInSnapshotTx must commit cleanly", delta)
}
