// Rollback E2E test for PagoWriter against the real Microsip Firebird DB.
// Everything in this file obeys the cleanup contract documented at the top of
// pago_writer_e2e_test.go — read it before touching anything here.
//
//nolint:misspell // Microsip table/column identifiers are kept verbatim.
package microsip_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// formaCobroOutOfRange is the failure lever for the third INSERT. See the long
// comment on TestE2E_PagoWriter_Aplicar_FalloDeInsertNoDejaRastro for why the
// value — and not a foreign key — is what breaks that statement.
//
// FORMAS_COBRO_DOCTOS.FORMA_COBRO_ID is INTEGER (RDB$FIELD_TYPE 8, length 4),
// so 2^31 does not fit. Firebird rejects the PARAMETER, not the row: the DSQL
// layer raises -303 while narrowing the bound int64 down to INTEGER, before the
// INSERT body runs. Measured: the same -303 comes back from
// `SELECT ... WHERE 1 = 0 AND FORMA_COBRO_ID = ?`, where the parameter can
// never be evaluated. It is still the engine and not the driver that rejects it
// (nakagami/firebirdsql implements neither CheckNamedValue nor ColumnConverter,
// so database/sql widens the int and the value travels as 64-bit BLR), but the
// BEFORE INSERT trigger never fires and no GEN_ID is consumed.
const formaCobroOutOfRange = math.MaxInt32 + 1

// nonexistentCargoOffset is the failure lever for the second INSERT. Added to
// the ID_DOCTOS watermark it yields a DOCTO_CC_ID far above anything the
// generator has handed out, so IMPORTES_DOCTOS_CC.DOCTO_CC_ACR_ID points at a
// cargo that does not exist. The test asserts that absence rather than
// assuming it.
//
// IMPORTES_DOCTOS_CC.DOCTO_CC_ACR_ID does carry a real, enforced foreign key
// (CARGO_AFECTADO_CC → DOCTOS_CC, read from RDB$REF_CONSTRAINTS), but MEASURED
// against the live schema that key never gets its turn: the rejection comes
// from the BEFORE INSERT trigger IMPTES_DOCTOS_CC_BEFINS_0, which calls
// REGISTRA_IMPORTE_CC and raises EX_SALDO_CARGO_EXCEDIDO ("el importe del
// crédito es mayor al saldo del cargo") because a cargo that does not exist has
// no saldo to credit. Firebird runs BEFORE triggers ahead of constraint checks
// (this schema depends on that ordering: the -1 → GEN_ID idiom only works if
// the PK is verified afterwards), so on this path CARGO_AFECTADO_CC is
// unreachable. Precisely: unreachable while the importe is > 0, which is the
// only shape PagoWriter ever sends. REGISTRA_IMPORTE_CC guards its raise with
// a comparison against the cargo saldo, so an importe of 0 would not trip it
// and the foreign key would get its turn after all.
//
// What matters for this test is unchanged, and is the point the -303 lever
// cannot reach: the failure is raised from INSIDE the statement body, by
// row-level logic that already ran.
//
// The generator-assigning trigger IMPTES_DOCTOS_CC_BEFINS sits at
// RDB$TRIGGER_SEQUENCE 1, behind the one that raises at sequence 0, so the
// rejected importe row never claims an id. Measured, not inferred: the t.Logf
// below reported 1 ID_DOCTOS value burned per failed Aplicar here (the
// DOCTOS_CC header alone) against 2 for the formaCobroOutOfRange subtest,
// where both earlier INSERTs completed.
//
// Those two numbers are an observation of one run, not an invariant: ID_DOCTOS
// is shared database-wide and any other writer moving it inflates the delta.
// That is why the counter only ever reaches t.Logf. Read a departure from 1
// and 2 as "someone else was writing", not as a regression — and note the
// counter has no positive control either: the happy path would burn 3 and
// nothing records it.
const nonexistentCargoOffset = 1_000_000

// writerRowCount holds how many rows each of the three tables PagoWriter
// inserts into gained above a generator watermark.
type writerRowCount struct {
	doctosCC    int
	importes    int
	formasCobro int
}

// peekIDDoctos reads the shared ID_DOCTOS generator WITHOUT consuming a value
// (GEN_ID with increment 0). All three tables the writer inserts into draw
// their primary keys from this one generator: DOCTOS_CC and IMPORTES_DOCTOS_CC
// through their BEFORE INSERT triggers, and FORMAS_COBRO_DOCTOS through
// FORMAS_COBRO_DOCTOS_BEFINS. That makes the current value a usable watermark:
// any row created after this call has an id strictly greater than it.
func peekIDDoctos(t *testing.T, ctx context.Context, q firebird.Querier) int { //nolint:revive // context-as-argument: t comes first by test convention.
	t.Helper()
	var mark int
	require.NoError(t,
		q.QueryRowContext(ctx, `SELECT GEN_ID(ID_DOCTOS, 0) FROM RDB$DATABASE`).Scan(&mark),
		"peek ID_DOCTOS watermark",
	)
	require.Positive(t, mark, "ID_DOCTOS watermark must be positive")
	return mark
}

// countRowsAboveMark counts, in each of the three writer tables, the rows whose
// primary key is above the watermark. Each table is counted on its OWN primary
// key — deliberately not by joining back to DOCTOS_CC, because a join through
// the parent would hide a leaked child row whenever the parent did roll back.
func countRowsAboveMark(t *testing.T, ctx context.Context, q firebird.Querier, mark int) writerRowCount { //nolint:revive // context-as-argument: t comes first by test convention.
	t.Helper()
	var got writerRowCount
	for _, probe := range []struct {
		sql   string
		into  *int
		label string
	}{
		{`SELECT COUNT(*) FROM DOCTOS_CC WHERE DOCTO_CC_ID > ?`, &got.doctosCC, "DOCTOS_CC"},
		{`SELECT COUNT(*) FROM IMPORTES_DOCTOS_CC WHERE IMPTE_DOCTO_CC_ID > ?`, &got.importes, "IMPORTES_DOCTOS_CC"},
		{`SELECT COUNT(*) FROM FORMAS_COBRO_DOCTOS WHERE FORMA_COBRO_DOC_ID > ?`, &got.formasCobro, "FORMAS_COBRO_DOCTOS"},
	} {
		require.NoError(t, q.QueryRowContext(ctx, probe.sql, mark).Scan(probe.into),
			"count rows above watermark in %s", probe.label)
	}
	return got
}

// registerLeakCleanup registers the safety net for the failing half of the
// test. If the rollback holds there is nothing to delete; if it does NOT hold
// (which is exactly the regression this test exists to catch) the abono rows
// are real money rows in the shared Microsip DB and must not survive the run.
//
// Deletes are scoped by BOTH the watermark and the synthetic client, so the
// seeded cargo — whose id is below the watermark — is never touched, and
// nothing belonging to any other client can be hit. Order is children before
// parents, and no delete discards its error.
func registerLeakCleanup(t *testing.T, pool *firebird.Pool, clienteID, mark int) {
	t.Helper()
	t.Cleanup(func() {
		q := firebird.GetQuerier(context.Background(), pool.DB)
		for _, stmt := range []string{
			`DELETE FROM FORMAS_COBRO_DOCTOS
			   WHERE FORMA_COBRO_DOC_ID > ? AND NOM_TABLA_DOCTOS = 'DOCTOS_CC'
			     AND DOCTO_ID IN (SELECT DOCTO_CC_ID FROM DOCTOS_CC WHERE CLIENTE_ID = ?)`,
			`DELETE FROM IMPORTES_DOCTOS_CC
			   WHERE IMPTE_DOCTO_CC_ID > ?
			     AND DOCTO_CC_ID IN (SELECT DOCTO_CC_ID FROM DOCTOS_CC WHERE CLIENTE_ID = ?)`,
			// MSP_PAGOS_VENTAS is the mirror cache a Microsip trigger fills in
			// from the abono header, and deleting DOCTOS_CC does not touch it
			// (ADR-0006). MSP_SALDOS_VENTAS is deliberately NOT here: it is
			// keyed by the CARGO (PK_MSP_SALDOS_VENTAS on DOCTO_CC_ID, see
			// migrations-firebird/000010) and both trigger paths recompute it
			// with the cargo's id, which seedCargo claimed BEFORE the watermark
			// was taken. A `DOCTO_CC_ID > mark` filter could therefore never
			// match it. That row is removed by seedCargo's own t.Cleanup.
			`DELETE FROM MSP_PAGOS_VENTAS
			   WHERE DOCTO_CC_ID > ?
			     AND DOCTO_CC_ID IN (SELECT DOCTO_CC_ID FROM DOCTOS_CC WHERE CLIENTE_ID = ?)`,
			`DELETE FROM DOCTOS_CC WHERE DOCTO_CC_ID > ? AND CLIENTE_ID = ?`,
		} {
			if _, err := q.ExecContext(context.Background(), stmt, mark, clienteID); err != nil {
				t.Errorf("limpieza de las filas filtradas por el rollback (%s): %v", stmt, err)
			}
		}
	})
}

// TestE2E_PagoWriter_Aplicar_FalloDeInsertNoDejaRastro is the atomicity test
// for the Microsip writer against a real Firebird: when the LAST of the three
// INSERTs blows up, the two that already ran must leave nothing behind.
//
// ─── Why the failure is a value and not a foreign key ────────────────────────
//
// The plan for this task assumed the third INSERT could be broken "with an
// invalid FK". Measured against the live schema, that is false:
// FORMAS_COBRO_DOCTOS has NO foreign key at all — its only constraints are one
// PRIMARY KEY (FORMAS_COBRO_DOCTOS_PK on FORMA_COBRO_DOC_ID) and five NOT NULL
// checks (INTEG_1156..INTEG_1160). Query used, against RDB$RELATION_CONSTRAINTS
// joined to RDB$REF_CONSTRAINTS: zero FOREIGN KEY rows for that table. So a
// nonexistent FORMA_COBRO_ID inserts happily — which also explains the
// documented production data where FORMAS_COBRO_DOCTOS.FORMA_COBRO_ID matches
// no FORMAS_COBRO row (docs/module-standards/ENCODING_HANDLING.md).
//
// The four NOT NULL columns the writer fills are all hardcoded constants in
// pago_writer.go, so the only lever a caller has on that statement is the
// FORMA_COBRO_ID value. It is INTEGER, so 2^31 does not fit and the engine
// rejects the PARAMETER of that statement (DSQL -303) before executing the
// INSERT body — see the comment on formaCobroOutOfRange for the measurement
// that separates "rejected parameter" from "rejected row". The rejection is
// real and comes from the server, but the row insert never begins.
//
// ─── What this test catches ──────────────────────────────────────────────────
//
// A writer (or a caller) that does not keep the three INSERTs in one
// transaction. Verified twice over:
//
//   - The positive control subtest below runs the SAME watermark probes on a
//     successful Aplicar and sees 1/1/1, so a green 0/0/0 in the failing half
//     is a measurement, not a query that looks nowhere.
//   - Verified by breaking the atomicity on purpose: running the failing half
//     WITHOUT the surrounding RunInTx (each statement autocommitting on the
//     pool, which is what a writer outside a transaction would do) turns the
//     post-rollback counts into 1/1/0 and both header assertions fire, with
//     the engine rejection identical. So the green run measures the rollback,
//     not just the rejection.
//
// The failure is also pinned to the RIGHT statement: each failing subtest
// measures insideTx while the transaction is still open (1/1/0 when the third
// INSERT is the one rejected, 1/0/0 when it is the second).
//
// Two different rejection points are covered on purpose, because they are
// different axes and not a subset of one another:
//
//   - Third INSERT, rejected PARAMETER (formaCobroOutOfRange). Widest reach —
//     two prior INSERTs must be undone — but the row insert never begins.
//   - Second INSERT, rejected ROW (nonexistentCargoOffset). Narrower reach —
//     one prior INSERT — but the failure is raised from INSIDE the statement
//     body, by a BEFORE INSERT trigger that already ran. This recovers the
//     class of failure the plan asked for with its (nonexistent) FK, and it is
//     the only one of that class this path offers.
//
// ─── What this test does NOT catch ───────────────────────────────────────────
//
//   - A failure of the first two statements (EXECUTE PROCEDURE GEN_FOLIO_TEMP
//     and the CLAVES_CLIENTES SELECT). Neither writes, so there is nothing for
//     a rollback to undo; GEN_FOLIO_TEMP in particular burns a folio number
//     that the rollback does NOT give back — a documented, accepted cosmetic
//     gap, not a defect this test should fail on.
//   - Whether the CALLER rolls back. This test drives the rollback itself
//     through TxManager.RunInTx; it proves the writer composes with an
//     external transaction, not that AplicarPago wires one up correctly.
//     That belongs to the app-level tests.
//   - A failure raised after a BEFORE INSERT trigger has already CONSUMED a
//     GEN_ID(ID_DOCTOS,1) for the row being rejected. No subtest reaches that
//     state, and the two get close in different, incomplete ways. On the third
//     INSERT the parameter is rejected before the statement body runs at all,
//     so no trigger fires; the schema offers no constraint there to do better
//     (FORMAS_COBRO_DOCTOS has no FK and no CHECK). On the second INSERT a
//     trigger does fire and does raise — that half of the class IS covered —
//     but it is the one at RDB$TRIGGER_SEQUENCE 0, ahead of the trigger that
//     assigns the id, so the row still never claims one. The counters measure
//     exactly this: 1 id burned there against 2 on the third-INSERT subtest,
//     in both cases only for rows that fully succeeded.
//   - The CARGO_AFECTADO_CC foreign key itself. It exists and is enforced, but
//     it is unreachable from this path — see nonexistentCargoOffset. No test
//     here proves that key works.
//   - Whether the generator gap left by a rolled-back INSERT is reclaimed. It
//     is not, by design: every failing run burns the ID_DOCTOS value that the
//     DOCTOS_CC header took, plus a GEN_FOLIO_TEMP folio, and the rollback
//     gives neither back — the same accepted cosmetic gap the plan documents.
//     The watermark probes count rows, never generator values, precisely so
//     this does not turn into a false failure.
//
//nolint:paralleltest // commits real txns; cleanup uses t.Cleanup — safe but not parallel.
func TestE2E_PagoWriter_Aplicar_FalloDeInsertNoDejaRastro(t *testing.T) {
	h := newE2EHarness(t)
	ctx := context.Background()
	q := firebird.GetQuerier(ctx, h.pool.DB)

	clienteID := seedCliente(t, ctx, h.pool, h.txMgr)

	buildInput := func(cargoDoctoCCID, formaCobroID int) outbound.MicrosipPagoInput {
		return outbound.MicrosipPagoInput{
			CargoDoctoCCID: cargoDoctoCCID,
			ClienteID:      clienteID,
			CobradorID:     testCobradorID,
			Cobrador:       "Ramírez García, Jorge",
			Importe:        decimal.NewFromInt(100),
			FormaCobroID:   formaCobroID,
			ConceptoCCID:   testConceptoCCID,
			FechaHoraPago:  time.Now().UTC(),
		}
	}

	// ── Positive control: the same probes must SEE the rows when Aplicar
	// succeeds. Without this, a 0/0/0 in the failing half would be
	// indistinguishable from a query that can never find anything.
	t.Run("control positivo: el camino feliz sí deja las tres filas", func(t *testing.T) {
		cargo := seedCargo(t, ctx, h.pool, h.txMgr, clienteID, decimal.NewFromInt(1000))
		mark := peekIDDoctos(t, ctx, q)

		result := h.applyInsideTx(t, buildInput(cargo.doctoCCID, testFormaCobroID))
		h.registerAplicarCleanup(t, result, cargo.doctoCCID)

		got := countRowsAboveMark(t, ctx, q, mark)
		assert.Equal(t, 1, got.doctosCC, "happy path must leave exactly 1 DOCTOS_CC abono above the watermark")
		assert.Equal(t, 1, got.importes, "happy path must leave exactly 1 IMPORTES_DOCTOS_CC line above the watermark")
		assert.Equal(t, 1, got.formasCobro, "happy path must leave exactly 1 FORMAS_COBRO_DOCTOS row above the watermark")
	})

	// runFailingAplicar drives one rejected Aplicar inside a transaction and
	// asserts on both readings of the three probes: what they see while the
	// transaction is still open (before the rollback), which must equal
	// wantInsideTx and is what names the statement expected to fail, and what
	// they see after it, which must be zero across the board. It returns
	// nothing; every check is an assertion.
	runFailingAplicar := func(t *testing.T, in outbound.MicrosipPagoInput, wantInsideTx writerRowCount) {
		t.Helper()
		mark := peekIDDoctos(t, ctx, q)
		registerLeakCleanup(t, h.pool, clienteID, mark)

		// insideTx is measured with the transaction's OWN querier right after
		// Aplicar returns, BEFORE the rollback. It is what pins the failure to
		// the intended statement: without it, a writer that blew up on the
		// FIRST INSERT would also produce 0/0/0 afterwards and the test would
		// pass for the wrong reason.
		var insideTx writerRowCount
		var result outbound.MicrosipPagoResult
		err := h.txMgr.RunInTx(ctx, func(txCtx context.Context) error {
			var e error
			result, e = h.writer.Aplicar(txCtx, in)
			if e != nil {
				insideTx = countRowsAboveMark(t, txCtx, firebird.GetQuerier(txCtx, h.pool.DB), mark)
			}
			return e
		})
		require.Error(t, err, "Aplicar must fail")
		appErr, ok := apperror.As(err)
		require.True(t, ok, "expected a typed apperror, got %T: %v", err, err)
		t.Logf("Aplicar rejected with code=%s message=%s raw=%v", appErr.Code, appErr.Message, err)
		assert.Zero(t, result.DoctoCCID, "a failed Aplicar must not report a DOCTO_CC_ID")

		assert.Equal(t, wantInsideTx.doctosCC, insideTx.doctosCC,
			"DOCTOS_CC rows visible inside the transaction, before the rollback")
		assert.Equal(t, wantInsideTx.importes, insideTx.importes,
			"IMPORTES_DOCTOS_CC rows visible inside the transaction, before the rollback")
		assert.Equal(t, wantInsideTx.formasCobro, insideTx.formasCobro,
			"FORMAS_COBRO_DOCTOS rows visible inside the transaction, before the rollback")

		got := countRowsAboveMark(t, ctx, q, mark)
		assert.Equal(t, 0, got.doctosCC, "no DOCTOS_CC row may survive the rollback")
		assert.Equal(t, 0, got.importes, "no IMPORTES_DOCTOS_CC row may survive the rollback")
		assert.Equal(t, 0, got.formasCobro, "no FORMAS_COBRO_DOCTOS row may survive the rollback")

		// Logged, never asserted: the generator is shared and other writers can
		// move it. It records which BEFORE INSERT triggers got far enough to
		// claim an id — the rollback never gives those back.
		t.Logf("ID_DOCTOS burned by this failed Aplicar: %d", peekIDDoctos(t, ctx, q)-mark)
	}

	// ── Widest reach: the third INSERT is rejected and the TWO rows that
	// already existed inside the transaction disappear.
	t.Run("el tercer INSERT rechazado no deja ninguna de las tres filas", func(t *testing.T) {
		cargo := seedCargo(t, ctx, h.pool, h.txMgr, clienteID, decimal.NewFromInt(1000))
		runFailingAplicar(t,
			buildInput(cargo.doctoCCID, formaCobroOutOfRange),
			writerRowCount{doctosCC: 1, importes: 1, formasCobro: 0},
		)
	})

	// ── The other axis: the second INSERT is rejected from inside the statement
	// body, by a BEFORE INSERT trigger that already ran. Narrower reach — only
	// INSERT #1 to undo — but it is the failure class the parameter rejection
	// above cannot reach.
	t.Run("el INSERT rechazado por el cargo inexistente tampoco deja rastro", func(t *testing.T) {
		mark := peekIDDoctos(t, ctx, q)
		nonexistentCargo := mark + nonexistentCargoOffset

		// Do not assume the cargo is missing — measure it. Otherwise a value
		// that happened to exist would turn this into a happy path that the
		// following assertions would misread.
		var existingRows int
		require.NoError(t,
			q.QueryRowContext(ctx, `SELECT COUNT(*) FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`, nonexistentCargo).Scan(&existingRows),
		)
		require.Equal(t, 0, existingRows, "the cargo whose absence triggers the rejection must not exist")

		runFailingAplicar(t,
			buildInput(nonexistentCargo, testFormaCobroID),
			writerRowCount{doctosCC: 1, importes: 0, formasCobro: 0},
		)
	})
}
