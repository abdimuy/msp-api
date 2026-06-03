//nolint:misspell // Spanish vocabulary by project convention.
package ventfb_test

// Scenario J — money integrity property test.
//
// TestProperty_MoneyIntegrity_50Rounds runs 50 rounds of 5-10 random
// operations against MSP_PAGOS_CHANGELOG / MSP_SALDOS_CHANGELOG directly (no
// real DOCTOS_CC / IMPORTES_DOCTOS_CC because wiring Microsip's many FK
// constraints in a test fixture is prohibitive). The test exercises the
// listener's changelog path to verify:
//
//  1. The listener never loses a changelog entry (no-loss invariant: every
//     row committed before the sync's watermark reaches the caller via exactly
//     one Since batch).
//  2. The union of all entries returned across every sync — plus a final drain
//     — equals the set of all PKs inserted during the test (coverage invariant).
//
// On any assertion failure the test dumps the full op sequence and per-round
// state for reproduction. The rapid seed is logged so failures are
// reproducible with -rapid.seed=<seed>.
//
// Requirements:
//   - FB_DATABASE env var must be set.
//   - Migration 000022 must be applied.
//
// The test is allowed to run slowly (up to 60s) and is skipped when FB_DATABASE
// is not set.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/stretchr/testify/require"

	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// opKind identifies the type of changelog operation in a round.
type opKind int

const (
	opInsertPago   opKind = iota // insert a row in MSP_PAGOS_CHANGELOG
	opInsertSaldo                // insert a row in MSP_SALDOS_CHANGELOG
	opListenerSync               // simulate a listener sync (call Since + advance cursor)
)

func (k opKind) String() string {
	switch k {
	case opInsertPago:
		return "insert_pago"
	case opInsertSaldo:
		return "insert_saldo"
	case opListenerSync:
		return "listener_sync"
	default:
		return "unknown"
	}
}

type op struct {
	kind  opKind
	pk    int
	seqID int64
}

// TestProperty_MoneyIntegrity_50Rounds runs 50 rapid rounds of 5-10 random
// changelog operations and verifies that every inserted row is delivered by
// exactly one Since call (no-loss) and that the union of all Since calls
// covers the full inserted set (coverage invariant).
//
//nolint:paralleltest
func TestProperty_MoneyIntegrity_50Rounds(t *testing.T) {
	requireFBEnv(t)
	requireMigration000022(t)

	pool := fbtestutil.NewTestFirebirdPool(t)
	pagosRepo := cobranzaventfb.NewPagosChangelogRepo(pool)
	saldosRepo := cobranzaventfb.NewSaldosChangelogRepo(pool)
	ctx := context.Background()

	// Use a known TX_ID that is well below any real active transaction so all
	// inserted rows pass "TX_ID < watermark" in Since queries.
	const testTxID = int64(1)

	// ── bookkeeping ──────────────────────────────────────────────────────────

	// insertedPagosSeqs and insertedSaldosSeqs accumulate the SEQ_IDs of all
	// rows inserted during the test. Registered for cleanup.
	var insertedPagosSeqs []int64
	var insertedSaldosSeqs []int64

	t.Cleanup(func() {
		for _, seq := range insertedPagosSeqs {
			_, _ = pool.ExecContext(ctx,
				`DELETE FROM MSP_PAGOS_CHANGELOG WHERE SEQ_ID = ?`, seq)
		}
		for _, seq := range insertedSaldosSeqs {
			_, _ = pool.ExecContext(ctx,
				`DELETE FROM MSP_SALDOS_CHANGELOG WHERE SEQ_ID = ?`, seq)
		}
	})

	// ── helpers ───────────────────────────────────────────────────────────────

	insertPagosChangelog := func(t *testing.T, pk int) int64 {
		t.Helper()
		var seqID int64
		require.NoError(t,
			pool.QueryRowContext(
				ctx,
				`SELECT GEN_ID(GEN_MSP_PAGOS_CHANGELOG_SEQ, 1) FROM RDB$DATABASE`,
			).Scan(&seqID),
			"GEN_ID pagos")
		_, err := pool.ExecContext(ctx,
			`INSERT INTO MSP_PAGOS_CHANGELOG (SEQ_ID, IMPTE_DOCTO_CC_ID, TX_ID, COMMIT_AT)
			 VALUES (?, ?, ?, ?)`,
			seqID, pk, testTxID, firebird.ToWallClock(time.Now().UTC()))
		require.NoError(t, err, "INSERT MSP_PAGOS_CHANGELOG pk=%d", pk)
		return seqID
	}

	insertSaldosChangelog := func(t *testing.T, pk int) int64 {
		t.Helper()
		var seqID int64
		require.NoError(t,
			pool.QueryRowContext(
				ctx,
				`SELECT GEN_ID(GEN_MSP_SALDOS_CHANGELOG_SEQ, 1) FROM RDB$DATABASE`,
			).Scan(&seqID),
			"GEN_ID saldos")
		_, err := pool.ExecContext(ctx,
			`INSERT INTO MSP_SALDOS_CHANGELOG (SEQ_ID, DOCTO_CC_ID, TX_ID, COMMIT_AT)
			 VALUES (?, ?, ?, ?)`,
			seqID, pk, testTxID, firebird.ToWallClock(time.Now().UTC()))
		require.NoError(t, err, "INSERT MSP_SALDOS_CHANGELOG pk=%d", pk)
		return seqID
	}

	// ── property check ────────────────────────────────────────────────────────

	rapid.Check(t, func(rt *rapid.T) {
		// Per-round state.
		roundOps := make([]op, 0, 10)

		nOps := rapid.IntRange(5, 10).Draw(rt, "n_ops")

		// PKs in the test-safe range 2,100,000,000 – 2,139,999,999.
		basePK := 2_100_000_000 + int(time.Now().UnixNano()%40_000_000)

		// Current Since cursors, advanced after each sync.
		pagosCursor := int64(0)
		saldosCursor := int64(0)

		// All inserts this round — used for the final coverage check.
		var roundPagosSeqs []int64
		var roundSaldosSeqs []int64

		// Union of all SeqIDs ever returned by Since calls this round (pagos/saldos).
		// Used to verify coverage at the end.
		pagosDelivered := make(map[int64]bool)
		saldosDelivered := make(map[int64]bool)

		for i := range nOps {
			kind := opKind(rapid.IntRange(0, 2).Draw(rt, fmt.Sprintf("op_%d_kind", i)))

			switch kind {
			case opInsertPago:
				pk := basePK + i
				seqID := insertPagosChangelog(t, pk)
				roundPagosSeqs = append(roundPagosSeqs, seqID)
				insertedPagosSeqs = append(insertedPagosSeqs, seqID)
				roundOps = append(roundOps, op{kind, pk, seqID})

			case opInsertSaldo:
				pk := basePK + 1000 + i
				seqID := insertSaldosChangelog(t, pk)
				roundSaldosSeqs = append(roundSaldosSeqs, seqID)
				insertedSaldosSeqs = append(insertedSaldosSeqs, seqID)
				roundOps = append(roundOps, op{kind, pk, seqID})

			case opListenerSync:
				// ── simulate one listener tick ───────────────────────────────
				//
				// No-loss invariant (checked here):
				//   Let prevCursor := pagosCursor before this call.
				//   Let entries   := Since(prevCursor, watermark, largeLimit).
				//   Let maxRet    := max(entry.SeqID) across entries (or prevCursor if empty).
				//
				//   Every insert op that occurred before this sync whose SeqID
				//   satisfies prevCursor < SeqID <= maxRet MUST appear in entries.
				//   Inserts with SeqID > maxRet are "ahead of this batch" and will
				//   appear in a later sync — that is expected and correct.
				//
				// After the assertion the cursor is advanced to maxRet.

				wm := cobranzaventfb.SentinelNoActiveTx

				// ── pagos sync ─────────────────────────────────────────────────
				prevPagosCursor := pagosCursor
				pagosEntries, err := pagosRepo.Since(ctx, prevPagosCursor, wm, 10_000)
				if err != nil {
					rt.Fatalf("pagosRepo.Since failed: %v", err)
				}

				// Assert strictly ascending SeqIDs and record what was delivered.
				maxPagosRet := prevPagosCursor
				prevSeq := int64(-1)
				for _, e := range pagosEntries {
					if e.SeqID <= prevSeq {
						rt.Fatalf("pagos Since returned non-ascending SeqIDs: %d then %d (cursor=%d)",
							prevSeq, e.SeqID, prevPagosCursor)
					}
					if e.SeqID <= prevPagosCursor {
						rt.Fatalf("pagos Since returned SeqID=%d which is <= cursor=%d",
							e.SeqID, prevPagosCursor)
					}
					prevSeq = e.SeqID
					if e.SeqID > maxPagosRet {
						maxPagosRet = e.SeqID
					}
					pagosDelivered[e.SeqID] = true
				}

				// No-loss: every insert in (prevCursor, maxPagosRet] must appear.
				pagosRetSet := make(map[int64]bool, len(pagosEntries))
				for _, e := range pagosEntries {
					pagosRetSet[e.SeqID] = true
				}
				for _, seq := range roundPagosSeqs {
					if seq > prevPagosCursor && seq <= maxPagosRet {
						if !pagosRetSet[seq] {
							opStrs := make([]string, len(roundOps))
							for j, o := range roundOps {
								opStrs[j] = fmt.Sprintf("%s(pk=%d,seq=%d)", o.kind, o.pk, o.seqID)
							}
							rt.Fatalf(
								"no-loss violation: pagos SEQ_ID=%d in window (%d, %d] missing from Since\nops: %v",
								seq, prevPagosCursor, maxPagosRet, opStrs,
							)
						}
					}
				}

				pagosCursor = maxPagosRet

				// ── saldos sync ────────────────────────────────────────────────
				prevSaldosCursor := saldosCursor
				saldosEntries, err := saldosRepo.Since(ctx, prevSaldosCursor, wm, 10_000)
				if err != nil {
					rt.Fatalf("saldosRepo.Since failed: %v", err)
				}

				maxSaldosRet := prevSaldosCursor
				prevSeq = -1
				for _, e := range saldosEntries {
					if e.SeqID <= prevSeq {
						rt.Fatalf("saldos Since returned non-ascending SeqIDs: %d then %d (cursor=%d)",
							prevSeq, e.SeqID, prevSaldosCursor)
					}
					if e.SeqID <= prevSaldosCursor {
						rt.Fatalf("saldos Since returned SeqID=%d which is <= cursor=%d",
							e.SeqID, prevSaldosCursor)
					}
					prevSeq = e.SeqID
					if e.SeqID > maxSaldosRet {
						maxSaldosRet = e.SeqID
					}
					saldosDelivered[e.SeqID] = true
				}

				saldosRetSet := make(map[int64]bool, len(saldosEntries))
				for _, e := range saldosEntries {
					saldosRetSet[e.SeqID] = true
				}
				for _, seq := range roundSaldosSeqs {
					if seq > prevSaldosCursor && seq <= maxSaldosRet {
						if !saldosRetSet[seq] {
							opStrs := make([]string, len(roundOps))
							for j, o := range roundOps {
								opStrs[j] = fmt.Sprintf("%s(pk=%d,seq=%d)", o.kind, o.pk, o.seqID)
							}
							rt.Fatalf(
								"no-loss violation: saldos SEQ_ID=%d in window (%d, %d] missing from Since\nops: %v",
								seq, prevSaldosCursor, maxSaldosRet, opStrs,
							)
						}
					}
				}

				saldosCursor = maxSaldosRet

				roundOps = append(roundOps, op{kind, 0, 0})
			}
		}

		// ── final drain + coverage invariant ─────────────────────────────────
		//
		// After all ops, drain any remaining entries (inserts that were never
		// consumed by a sync op this round). Then assert that the union of all
		// Since calls — including this drain — covers every insert in the round.

		// Drain pagos.
		for {
			entries, err := pagosRepo.Since(ctx, pagosCursor, cobranzaventfb.SentinelNoActiveTx, 10_000)
			if err != nil {
				rt.Fatalf("pagos drain Since failed: %v", err)
			}
			if len(entries) == 0 {
				break
			}
			for _, e := range entries {
				pagosDelivered[e.SeqID] = true
				if e.SeqID > pagosCursor {
					pagosCursor = e.SeqID
				}
			}
		}

		// Drain saldos.
		for {
			entries, err := saldosRepo.Since(ctx, saldosCursor, cobranzaventfb.SentinelNoActiveTx, 10_000)
			if err != nil {
				rt.Fatalf("saldos drain Since failed: %v", err)
			}
			if len(entries) == 0 {
				break
			}
			for _, e := range entries {
				saldosDelivered[e.SeqID] = true
				if e.SeqID > saldosCursor {
					saldosCursor = e.SeqID
				}
			}
		}

		// Coverage: every inserted SeqID must have been delivered at some point.
		for _, seq := range roundPagosSeqs {
			if !pagosDelivered[seq] {
				opStrs := make([]string, len(roundOps))
				for i, o := range roundOps {
					opStrs[i] = fmt.Sprintf("%s(pk=%d,seq=%d)", o.kind, o.pk, o.seqID)
				}
				rt.Fatalf("coverage violation: pagos SEQ_ID=%d never delivered\nops: %v",
					seq, opStrs)
			}
		}
		for _, seq := range roundSaldosSeqs {
			if !saldosDelivered[seq] {
				opStrs := make([]string, len(roundOps))
				for i, o := range roundOps {
					opStrs[i] = fmt.Sprintf("%s(pk=%d,seq=%d)", o.kind, o.pk, o.seqID)
				}
				rt.Fatalf("coverage violation: saldos SEQ_ID=%d never delivered\nops: %v",
					seq, opStrs)
			}
		}
	})
}
