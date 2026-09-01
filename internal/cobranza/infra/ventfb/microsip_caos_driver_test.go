//nolint:misspell // Microsip table identifiers and cobranza vocabulary are Spanish by project convention.
package ventfb_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nakagami/firebirdsql"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	cobranzamicrosip "github.com/abdimuy/msp-api/internal/cobranza/infra/microsip"
	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	cobranzaoutbound "github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// ─────────────────────────────────────────────────────────────────────────────
// In-process chaos harness for the write path that task 2 moved INSIDE the
// transaction: repo.Insert(pago) followed by PagoWriter.Aplicar, all under one
// firebird.RunInTx.
//
// WHY IT LIVES HERE AND NOT IN internal/cobranza/infra/microsip: that package
// owns the real-Firebird rejection test and is deliberately excluded from
// `make test-firebird-all` because it COMMITS money rows to the shared
// Microsip database. What is missing there is not another real failure but a
// failure that can be placed at an ARBITRARY statement — Firebird will not
// reject the second INSERT on demand. This file supplies exactly that, with a
// database/sql driver double, so it needs no database at all and runs in CI
// where FB_DATABASE is unset.
//
// WHY A DRIVER DOUBLE AND NOT A firebird.Querier DOUBLE: Querier is
//
//	ExecContext(...)      (sql.Result, error)
//	QueryContext(...)     (*sql.Rows, error)
//	QueryRowContext(...)  *sql.Row
//
// and *sql.Row carries its error in an UNEXPORTED field. No type outside
// database/sql can produce a *sql.Row that fails. Four of PagoWriter's five
// statements go through QueryRowContext (the two INSERTs use RETURNING), so a
// hand-written Querier could only ever fail the ONE ExecContext statement —
// FORMAS_COBRO_DOCTOS, the last one. Reproducing "DOCTOS_CC went in but the
// importe did not" is impossible that way. Registering a driver and handing
// the real *sql.DB to firebird.NewTxManager (whose doc comment invites
// exactly this: "Accepts a *sql.DB so test code can pass a stub") makes every
// statement failable and, as a bonus, exercises the REAL transaction
// lifecycle — BEGIN, the statement sequence, and ROLLBACK vs COMMIT.
//
// WHAT THESE TESTS DETECT:
//   - the writer joining the caller's transaction instead of taking its own
//     connection (asserted as opens == 1);
//   - a failure at statement N leaving the transaction rolled back and never
//     committed, with statements 1..N-1 already issued;
//   - the exact statement order of the combined flow — a reordered or added
//     statement in PagoWriter moves the ordinals and fails the table;
//   - a Firebird lock conflict surfacing as apperror KindConflict /
//     firebird_lock_conflict, classified transient, with the pooled
//     connection returned healthy and reused afterwards.
//
// WHAT THEY DO NOT DETECT:
//   - whether Firebird actually UNDOES the rows. The double records that
//     Rollback was called; it does not implement storage. Real undo is the
//     job of the real-Firebird test in internal/cobranza/infra/microsip.
//   - anything about the SQL text being valid Firebird: the double never
//     parses it, so a syntactically broken statement passes here.
//   - server-side statement timeouts and the wire-protocol desync that
//     internal/platform/firebird/driverwrap.go exists to prevent. Those live
//     below database/sql, where this double sits.
// ─────────────────────────────────────────────────────────────────────────────

// caosPlan is the shared recorder + fault injector behind one registered
// driver. Every statement the flow issues lands in stmts, in order; the
// statement whose 1-indexed ordinal equals failAt returns failErr instead of
// running.
type caosPlan struct {
	mu        sync.Mutex
	stmts     []string
	ctxErrs   []error
	opens     int
	commits   int
	rollbacks int

	failAt  int // 1-indexed ordinal that fails; 0 = never fail
	failErr error
}

// record appends the statement and reports the injected error when this is
// the ordinal the test asked to break. It also snapshots ctx.Err() so a test
// can prove the caller never tried to escape a slow statement by cancelling
// — the move that poisons the firebirdsql pool.
func (p *caosPlan) record(ctx context.Context, query string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stmts = append(p.stmts, query)
	p.ctxErrs = append(p.ctxErrs, ctx.Err())
	if p.failAt != 0 && len(p.stmts) == p.failAt {
		return p.failErr
	}
	return nil
}

// caosCenso is the table-by-table census of one run: what was issued, on how
// many connections, and how the transaction ended.
type caosCenso struct {
	stmts     []string
	ctxErrs   []error
	opens     int
	commits   int
	rollbacks int
}

func (p *caosPlan) censo() caosCenso {
	p.mu.Lock()
	defer p.mu.Unlock()
	return caosCenso{
		stmts:     append([]string(nil), p.stmts...),
		ctxErrs:   append([]error(nil), p.ctxErrs...),
		opens:     p.opens,
		commits:   p.commits,
		rollbacks: p.rollbacks,
	}
}

// ─── the driver ─────────────────────────────────────────────────────────────

type caosDriver struct{ plan *caosPlan }

func (d *caosDriver) Open(string) (driver.Conn, error) {
	d.plan.mu.Lock()
	d.plan.opens++
	d.plan.mu.Unlock()
	return &caosConn{plan: d.plan}, nil
}

type caosConn struct{ plan *caosPlan }

// Prepare is never expected to run: the conn implements ExecerContext and
// QueryerContext, so database/sql uses the fast path. Failing loudly here
// keeps a silent fallback from making the ordinals lie.
func (c *caosConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("caos driver: Prepare is not implemented; the conn answers Exec/Query directly")
}

func (c *caosConn) Close() error              { return nil }
func (c *caosConn) Begin() (driver.Tx, error) { return &caosTx{plan: c.plan}, nil }

func (c *caosConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return &caosTx{plan: c.plan}, nil
}

// CheckNamedValue accepts every argument verbatim. PagoWriter binds 59
// columns of mixed types (nil, string, int, decimal-as-string); converting
// them is Firebird's job, not this double's.
func (c *caosConn) CheckNamedValue(*driver.NamedValue) error { return nil }

func (c *caosConn) ExecContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if err := c.plan.record(ctx, query); err != nil {
		return nil, err
	}
	return driver.RowsAffected(1), nil
}

func (c *caosConn) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := c.plan.record(ctx, query); err != nil {
		return nil, err
	}
	col, val := caosAnswerFor(query)
	return &caosRows{cols: []string{col}, vals: []driver.Value{val}}, nil
}

type caosTx struct{ plan *caosPlan }

func (t *caosTx) Commit() error {
	t.plan.mu.Lock()
	t.plan.commits++
	t.plan.mu.Unlock()
	return nil
}

func (t *caosTx) Rollback() error {
	t.plan.mu.Lock()
	t.plan.rollbacks++
	t.plan.mu.Unlock()
	return nil
}

type caosRows struct {
	cols []string
	vals []driver.Value
	done bool
}

func (r *caosRows) Columns() []string { return r.cols }
func (r *caosRows) Close() error      { return nil }

func (r *caosRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	copy(dest, r.vals)
	return nil
}

// caosAnswerFor supplies the single value each of PagoWriter's SELECT-shaped
// statements scans. The generated ids are deliberately out of the range any
// real row uses so a leak into a real database would be obvious.
func caosAnswerFor(query string) (string, driver.Value) {
	switch {
	case strings.Contains(query, "GEN_FOLIO_TEMP"):
		return "FOLIO_TEMP", "9000001"
	case strings.Contains(query, "CLAVES_CLIENTES"):
		return "CLAVE_CLIENTE", "CL-CAOS"
	case strings.Contains(query, "INTO DOCTOS_CC"):
		return "DOCTO_CC_ID", int64(990001)
	case strings.Contains(query, "INTO IMPORTES_DOCTOS_CC"):
		return "IMPTE_DOCTO_CC_ID", int64(990002)
	default:
		return "UNO", int64(1)
	}
}

// ─── harness ────────────────────────────────────────────────────────────────

// caosDriverSeq keeps every registered driver name unique; sql.Register
// panics on a repeat and these tests run in parallel.
var caosDriverSeq atomic.Int64

// caosHarness wires a *sql.DB over the fault-injecting driver into the real
// TxManager, the real PagosRecibidosRepo and the real PagoWriter.
type caosHarness struct {
	plan   *caosPlan
	db     *sql.DB
	txMgr  *firebird.TxManager
	repo   *cobranzaventfb.PagosRecibidosRepo
	writer *cobranzamicrosip.PagoWriter
}

func newCaosHarness(t *testing.T, failAt int, failErr error) *caosHarness {
	t.Helper()
	plan := &caosPlan{failAt: failAt, failErr: failErr}
	name := fmt.Sprintf("cobranza-caos-%d", caosDriverSeq.Add(1))
	sql.Register(name, &caosDriver{plan: plan})
	db, err := sql.Open(name, "caos")
	require.NoError(t, err, "sql.Open over the caos driver must not fail")
	t.Cleanup(func() { _ = db.Close() })

	pool := &firebird.Pool{DB: db}
	return &caosHarness{
		plan:   plan,
		db:     db,
		txMgr:  firebird.NewTxManager(db),
		repo:   cobranzaventfb.NewPagosRecibidosRepo(pool),
		writer: cobranzamicrosip.NewPagoWriter(pool),
	}
}

// crearYAplicar reproduces the shape task 2 introduced: one transaction that
// inserts the pago row and then pushes it to Microsip as its last step.
func (h *caosHarness) crearYAplicar(ctx context.Context, pago *domain.PagoRecibido) error {
	return h.txMgr.RunInTx(ctx, func(ctx context.Context) error {
		if err := h.repo.Insert(ctx, pago); err != nil {
			return err
		}
		_, err := h.writer.Aplicar(ctx, caosInputFrom(pago))
		return err
	})
}

func caosInputFrom(p *domain.PagoRecibido) cobranzaoutbound.MicrosipPagoInput {
	return cobranzaoutbound.MicrosipPagoInput{
		CargoDoctoCCID: p.CargoDoctoCCID(),
		ClienteID:      p.ClienteID(),
		CobradorID:     p.CobradorID(),
		Cobrador:       p.Cobrador(),
		FormaCobroID:   p.FormaCobroID(),
		ConceptoCCID:   p.ConceptoCCID(),
		Importe:        p.Importe(),
		FechaHoraPago:  p.FechaHoraPago(),
		Lat:            p.Lat(),
		Lon:            p.Lon(),
	}
}

// buildCaosPago builds a valid aggregate without touching the database, so
// this file stays runnable with FB_DATABASE unset.
func buildCaosPago(t *testing.T) *domain.PagoRecibido {
	t.Helper()
	now := time.Now().UTC()
	p, err := domain.NewPagoRecibido(domain.CrearPagoRecibidoParams{
		ID:             uuid.New(),
		CargoDoctoCCID: 5000,
		ClienteID:      11486,
		CobradorID:     200,
		Cobrador:       "Ramírez García, Jorge",
		Importe:        decimal.NewFromInt(1500),
		FormaCobroID:   87327,
		FechaHoraPago:  now.Add(-30 * time.Minute),
		CreatedBy:      uuid.New(),
		Now:            now,
	})
	require.NoError(t, err, "buildCaosPago: NewPagoRecibido must not fail")
	return p
}

// ─── the statement map of the combined flow ─────────────────────────────────

// caosStmtOrder is the statement sequence the flow must issue, in order. It
// is the assertion, not documentation: a test names an ordinal, the harness
// checks the statement recorded at that ordinal really targets this table.
//
//	1  INSERT MSP_PAGOS_RECIBIDOS   PagosRecibidosRepo.Insert
//	2  EXECUTE PROCEDURE            PagoWriter.execGenFolioTemp
//	3  SELECT CLAVES_CLIENTES       PagoWriter.fetchClaveCliente
//	4  INSERT DOCTOS_CC             PagoWriter.insertDoctoCC
//	5  INSERT IMPORTES_DOCTOS_CC    PagoWriter.insertImporteDoctoCC
//	6  INSERT FORMAS_COBRO_DOCTOS   PagoWriter.insertFormaCobroDocto
var caosStmtOrder = []string{
	"MSP_PAGOS_RECIBIDOS",
	"GEN_FOLIO_TEMP",
	"CLAVES_CLIENTES",
	"INTO DOCTOS_CC",
	"INTO IMPORTES_DOCTOS_CC",
	"INTO FORMAS_COBRO_DOCTOS",
}

// assertSecuencia checks that the recorded statements are the expected
// prefix of caosStmtOrder — same length, same targets, same order.
func assertSecuencia(t *testing.T, stmts []string, hasta int) {
	t.Helper()
	require.Len(t, stmts, hasta,
		"the flow must stop at statement %d; recorded: %v", hasta, resumirStmts(stmts))
	for i := range hasta {
		assert.Contains(t, stmts[i], caosStmtOrder[i],
			"statement %d must target %s", i+1, caosStmtOrder[i])
	}
}

// resumirStmts renders the recorded statements as their table names so a
// failure message is readable instead of six screens of SQL.
func resumirStmts(stmts []string) []string {
	out := make([]string, 0, len(stmts))
	for _, s := range stmts {
		etiqueta := "?"
		for _, marca := range caosStmtOrder {
			if strings.Contains(s, marca) {
				etiqueta = marca
				break
			}
		}
		out = append(out, etiqueta)
	}
	return out
}

// ─── 1. the happy path — the positive control for the harness ───────────────

// TestCaos_FlujoCompleto_SeisSentenciasYUnCommit is the control that gives
// the failure tests their meaning: with no fault injected the flow issues all
// six statements over ONE connection and commits exactly once. Without it a
// broken harness that never reached Microsip would make every rollback
// assertion below pass for the wrong reason.
func TestCaos_FlujoCompleto_SeisSentenciasYUnCommit(t *testing.T) {
	t.Parallel()

	h := newCaosHarness(t, 0, nil)
	err := h.crearYAplicar(context.Background(), buildCaosPago(t))
	require.NoError(t, err, "with no fault injected the flow must complete")

	c := h.plan.censo()
	assertSecuencia(t, c.stmts, len(caosStmtOrder))
	assert.Equal(t, 1, c.commits, "the flow commits exactly once")
	assert.Equal(t, 0, c.rollbacks, "nothing to roll back on the happy path")
	assert.Equal(t, 1, c.opens,
		"one connection: the writer joins the caller's tx instead of taking its own")
}

// ─── 2. fallo a mitad ───────────────────────────────────────────────────────

// TestCaos_FalloAMitadDelWriter_LaTransaccionSeDeshace injects a failure at
// each statement of the Microsip half and pins what the caller is left with.
//
// The case the whole plan is about is ordinal 5: DOCTOS_CC is already in and
// the importe is refused. Before task 2 the writer ran after the commit, so
// that shape produced a DOCTOS_CC header with no IMPORTES_DOCTOS_CC line
// AND a pago row answered with 2xx. Now the same shape must end in a rollback
// with no commit anywhere.
//
// Detects: a statement failing mid-sequence that does not roll the caller's
// transaction back, a commit issued anyway, the writer opening its own
// connection, and any change to the statement order.
//
// Does NOT detect: whether the rows are physically gone — the double has no
// storage. That is the real-Firebird test's job.
func TestCaos_FalloAMitadDelWriter_LaTransaccionSeDeshace(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nombre string
		failAt int
		// yaEscrito is what the flow had already issued when it broke — the
		// positive control that the failure really happened in the MIDDLE.
		yaEscrito []string
	}{
		{
			nombre:    "el encabezado DOCTOS_CC es rechazado",
			failAt:    4,
			yaEscrito: []string{"MSP_PAGOS_RECIBIDOS"},
		},
		{
			nombre:    "entra DOCTOS_CC pero el importe es rechazado",
			failAt:    5,
			yaEscrito: []string{"MSP_PAGOS_RECIBIDOS", "INTO DOCTOS_CC"},
		},
		{
			nombre:    "entran encabezado e importe pero la forma de cobro es rechazada",
			failAt:    6,
			yaEscrito: []string{"MSP_PAGOS_RECIBIDOS", "INTO DOCTOS_CC", "INTO IMPORTES_DOCTOS_CC"},
		},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()

			// Census BEFORE: a fresh harness has issued nothing.
			rechazo := errors.New("microsip_rechazo_sintetico")
			h := newCaosHarness(t, c.failAt, rechazo)
			antes := h.plan.censo()
			require.Empty(t, antes.stmts)
			require.Equal(t, 0, antes.opens)
			require.Equal(t, 0, antes.commits)
			require.Equal(t, 0, antes.rollbacks)

			err := h.crearYAplicar(context.Background(), buildCaosPago(t))

			// The rejection reaches the caller untouched: MapError leaves a
			// non-FbError alone, so errors.Is still finds the cause.
			require.Error(t, err, "a rejected statement must not be swallowed")
			require.ErrorIs(t, err, rechazo)

			// Census AFTER.
			despues := h.plan.censo()
			assertSecuencia(t, despues.stmts, c.failAt)
			assert.Equal(t, 0, despues.commits,
				"a rejected pago must never reach a COMMIT")
			assert.Equal(t, 1, despues.rollbacks,
				"the transaction that carried the half-written document must roll back")
			assert.Equal(t, 1, despues.opens,
				"one connection: the half-write and its rollback share the caller's tx")

			// Positive control for "a mitad": everything in yaEscrito really
			// was issued before the break. Without this the test would also
			// pass if the flow had failed at statement 1.
			resumen := resumirStmts(despues.stmts)
			for _, tabla := range c.yaEscrito {
				assert.Contains(t, resumen, tabla,
					"%s must have been written BEFORE the rejection; recorded: %v", tabla, resumen)
			}
		})
	}
}

// ─── 3. conflicto de bloqueo ────────────────────────────────────────────────

// TestCaos_ConflictoDeBloqueo_SeMapeaYNoDejaLaConexionColgada covers the
// contention case with Cxc.exe, which holds SALDOS_CC and MSP_SALDOS_VENTAS
// while a cajero is posting. Firebird answers the blocked statement with GDS
// 335544345 (lock conflict on no wait transaction); the flow must surface it
// as a retryable conflict and — the part that matters operationally — must
// give the pooled connection back healthy.
//
// The connection half is why this test exists at all: the documented failure
// mode of the firebirdsql driver is that a CANCELLED context makes it write
// op_cancel and stop reading, desyncing the wire and leaking the connection
// forever. internal/platform/firebird/driverwrap.go strips cancellation at
// the driver boundary precisely to prevent that. This test pins the
// consequence one layer up: nobody in the cobranza write path cancels a
// context to escape a lock, and after the rejection the same physical
// connection is reused.
//
// Detects: a lock conflict that stops being classified transient (the retry
// worker would give up on a pago Microsip would have accepted a second
// later), a lock conflict that stops being a Conflict, a connection that is
// not returned to the pool, and any cancellation introduced into this path.
//
// Does NOT detect: the driver-level desync itself. That happens below
// database/sql, where this double sits — it is driverwrap's own test's job.
// This test also does not prove Firebird emits that GDS code; that mapping is
// pinned by internal/platform/firebird/errors_test.go.
func TestCaos_ConflictoDeBloqueo_SeMapeaYNoDejaLaConexionColgada(t *testing.T) {
	t.Parallel()

	const gdsLockNoWait = 335544345
	bloqueo := &firebirdsql.FbError{
		GDSCodes: []int{gdsLockNoWait},
		Message:  "lock conflict on no wait transaction",
	}

	// The conflict lands on DOCTOS_CC — the statement that touches the rows
	// Cxc.exe holds.
	h := newCaosHarness(t, 4, bloqueo)
	err := h.crearYAplicar(context.Background(), buildCaosPago(t))
	require.Error(t, err)

	// 1. It is a Conflict with the shared code, not an opaque 500.
	var ae *apperror.Error
	require.ErrorAs(t, err, &ae, "a lock conflict must arrive mapped, not raw")
	assert.Equal(t, "firebird_lock_conflict", ae.Code)
	assert.Equal(t, apperror.KindConflict, ae.Kind)

	// 2. It is transient, so the retry worker will come back for it.
	assert.True(t, firebird.IsTransient(err),
		"a lock conflict must stay retryable or the pago is abandoned")

	// 3. The transaction was undone, never committed.
	c := h.plan.censo()
	assertSecuencia(t, c.stmts, 4)
	assert.Equal(t, 0, c.commits)
	assert.Equal(t, 1, c.rollbacks)

	// 4. Nobody cancelled a context to get out of the lock — the move that
	//    poisons the firebirdsql pool.
	for i, ctxErr := range c.ctxErrs {
		require.NoErrorf(t, ctxErr,
			"statement %d ran on a cancelled context; cancelling to escape a lock desyncs the driver", i+1)
	}

	// 5. The connection came back. InUse must drain and the next transaction
	//    must reuse the SAME physical connection — opens stays at 1.
	stats := h.db.Stats()
	assert.Equal(t, 0, stats.InUse,
		"the connection must be returned to the pool after the rejection")
	assert.Equal(t, 1, stats.OpenConnections)

	h.plan.mu.Lock()
	h.plan.failAt = 0
	h.plan.mu.Unlock()
	require.NoError(t, h.crearYAplicar(context.Background(), buildCaosPago(t)),
		"the pool must still be usable after a lock conflict")

	despues := h.plan.censo()
	assert.Equal(t, c.opens, despues.opens,
		"a second transaction must reuse the connection, not replace a leaked one")
	assert.Equal(t, 1, despues.commits, "the retry commits")
}
