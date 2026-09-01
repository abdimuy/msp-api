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

// formaCobroFueraDeRango is the failure lever. See the long comment on
// TestE2E_PagoWriter_Aplicar_FalloDeInsertNoDejaRastro for why the value —
// and not a foreign key — is what breaks the third INSERT.
//
// FORMAS_COBRO_DOCTOS.FORMA_COBRO_ID is INTEGER (RDB$FIELD_TYPE 8, length 4),
// so 2^31 does not fit and Firebird rejects the statement.
const formaCobroFueraDeRango = math.MaxInt32 + 1

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
		for _, sentencia := range []string{
			`DELETE FROM FORMAS_COBRO_DOCTOS
			   WHERE FORMA_COBRO_DOC_ID > ? AND NOM_TABLA_DOCTOS = 'DOCTOS_CC'
			     AND DOCTO_ID IN (SELECT DOCTO_CC_ID FROM DOCTOS_CC WHERE CLIENTE_ID = ?)`,
			`DELETE FROM IMPORTES_DOCTOS_CC
			   WHERE IMPTE_DOCTO_CC_ID > ?
			     AND DOCTO_CC_ID IN (SELECT DOCTO_CC_ID FROM DOCTOS_CC WHERE CLIENTE_ID = ?)`,
			// The mirror caches a Microsip trigger fills in from the abono.
			// Deleting DOCTOS_CC does not touch them (ADR-0006).
			`DELETE FROM MSP_PAGOS_VENTAS
			   WHERE DOCTO_CC_ID > ?
			     AND DOCTO_CC_ID IN (SELECT DOCTO_CC_ID FROM DOCTOS_CC WHERE CLIENTE_ID = ?)`,
			`DELETE FROM MSP_SALDOS_VENTAS
			   WHERE DOCTO_CC_ID > ?
			     AND DOCTO_CC_ID IN (SELECT DOCTO_CC_ID FROM DOCTOS_CC WHERE CLIENTE_ID = ?)`,
			`DELETE FROM DOCTOS_CC WHERE DOCTO_CC_ID > ? AND CLIENTE_ID = ?`,
		} {
			if _, err := q.ExecContext(context.Background(), sentencia, mark, clienteID); err != nil {
				t.Errorf("limpieza de las filas filtradas por el rollback (%s): %v", sentencia, err)
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
// FORMA_COBRO_ID value. It is INTEGER, so 2^31 overflows the column and the
// engine rejects the INSERT. That is a real engine rejection of the real
// statement, not a simulated one.
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
// The failure is also pinned to the RIGHT statement: insideTx below asserts
// 1/1/0 while the transaction is still open.
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
//   - Anything about failures inside the second INSERT
//     (IMPORTES_DOCTOS_CC). That table DOES have real foreign keys
//     (CARGO_AFECTADO_CC on DOCTO_CC_ACR_ID and DOCTO_PADRE_CC on
//     DOCTO_CC_ID, both to DOCTOS_CC), but breaking there would only prove
//     that INSERT #1 rolls back — strictly less than what this test measures.
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

	// ── The measurement: the third INSERT is rejected by the engine and the
	// two rows that already existed inside the transaction disappear.
	t.Run("el tercer INSERT rechazado no deja ninguna de las tres filas", func(t *testing.T) {
		cargo := seedCargo(t, ctx, h.pool, h.txMgr, clienteID, decimal.NewFromInt(1000))
		mark := peekIDDoctos(t, ctx, q)
		registerLeakCleanup(t, h.pool, clienteID, mark)

		// insideTx is measured with the transaction's own querier right after
		// Aplicar returns, BEFORE the rollback. It is what pins the failure to
		// the third INSERT: statements 1 and 2 must already have written their
		// rows, and only FORMAS_COBRO_DOCTOS must be missing. Without it, a
		// writer that failed on the FIRST INSERT would also produce 0/0/0 after
		// the rollback and the test would pass for the wrong reason.
		var insideTx writerRowCount
		var result outbound.MicrosipPagoResult
		err := h.txMgr.RunInTx(ctx, func(txCtx context.Context) error {
			var e error
			result, e = h.writer.Aplicar(txCtx, buildInput(cargo.doctoCCID, formaCobroFueraDeRango))
			if e != nil {
				insideTx = countRowsAboveMark(t, txCtx, firebird.GetQuerier(txCtx, h.pool.DB), mark)
			}
			return e
		})
		require.Error(t, err, "an out-of-range FORMA_COBRO_ID must make Aplicar fail")
		appErr, ok := apperror.As(err)
		require.True(t, ok, "expected a typed apperror, got %T: %v", err, err)
		t.Logf("Aplicar rejected with code=%s message=%s raw=%v", appErr.Code, appErr.Message, err)
		assert.Zero(t, result.DoctoCCID, "a failed Aplicar must not report a DOCTO_CC_ID")

		assert.Equal(t, 1, insideTx.doctosCC, "INSERT #1 must have written the DOCTOS_CC header before the failure")
		assert.Equal(t, 1, insideTx.importes, "INSERT #2 must have written the IMPORTES_DOCTOS_CC line before the failure")
		assert.Equal(t, 0, insideTx.formasCobro, "INSERT #3 is the one that must have been rejected")

		got := countRowsAboveMark(t, ctx, q, mark)
		assert.Equal(t, 0, got.doctosCC, "the DOCTOS_CC header inserted before the failure must be rolled back")
		assert.Equal(t, 0, got.importes, "the IMPORTES_DOCTOS_CC line inserted before the failure must be rolled back")
		assert.Equal(t, 0, got.formasCobro, "the rejected FORMAS_COBRO_DOCTOS row must not exist")
	})
}
