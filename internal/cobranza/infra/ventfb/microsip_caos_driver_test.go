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

	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
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
// THE FLOW UNDER TEST IS THE REAL ONE. The harness calls
// app.Service.CrearPagoConImagenes — not a hand-assembled sequence. That
// matters more than it looks: the composition task 2 introduced (the Microsip
// push runs INSIDE the transaction that inserted the pago, as its last step)
// is the thing being tested, so it must be READ from production, never
// re-stated by the test. A harness that opened its own transaction and called
// repo.Insert + writer.Aplicar by hand would stay green if somebody moved the
// push back outside the commit — which is the original defect this whole plan
// removes.
//
// WHAT THESE TESTS DETECT:
//   - the push escaping the transaction: moved after the commit, or into a
//     transaction of its own. Either shows up as a second connection
//     (opens > 1), a COMMIT that should not exist, or an ordinal that no
//     longer matches caosStmtOrder;
//   - a failure at statement N leaving the transaction rolled back and never
//     committed, with statements 1..N-1 already issued;
//   - the exact statement order of the whole write path — a reordered, added
//     or dropped statement anywhere in the service, the repo or PagoWriter
//     moves the ordinals and fails the table;
//   - a Firebird lock conflict surfacing as apperror KindConflict /
//     firebird_lock_conflict, with the pooled connection returned healthy and
//     reused afterwards.
//
// WHAT THEY DO NOT DETECT:
//   - whether Firebird actually UNDOES the rows. The double records that
//     Rollback was called; it does not implement storage. Real undo is the
//     job of the real-Firebird test in internal/cobranza/infra/microsip.
//   - anything about the SQL text being valid Firebird: the double never
//     parses it, so a syntactically broken statement passes here.
//   - server-side statement timeouts and the wire-protocol desync that
//     internal/platform/firebird/driverwrap.go exists to prevent. Those live
//     below database/sql, where this double sits. In particular a caosConn is
//     never sick: "the connection came back healthy" here is a statement
//     about database/sql's bookkeeping, not about the firebirdsql driver. The
//     real thing is measured in
//     pagos_recibidos_bloqueo_integration_test.go and in
//     internal/platform/firebird/poolleak_integration_test.go;
//   - argument types Firebird would refuse. CheckNamedValue runs the same
//     driver.DefaultParameterConverter database/sql would use, so a type no
//     converter accepts is caught — but a type the converter accepts and
//     Firebird rejects (a string too long for the column, a bad date format)
//     passes here.
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

// CheckNamedValue runs the same conversion database/sql applies to a driver
// with no converter of its own. Accepting arguments verbatim instead would
// make the double MORE permissive than Firebird and let a binding a real
// driver refuses pass green — PagoWriter binds 59 columns of mixed types
// (nil, string, int, decimal-as-string) and that is exactly where a wrong
// type would hide.
func (c *caosConn) CheckNamedValue(nv *driver.NamedValue) error {
	v, err := driver.DefaultParameterConverter.ConvertValue(nv.Value)
	if err != nil {
		return err //nolint:wrapcheck // verbatim converter error, as a driver would.
	}
	nv.Value = v
	return nil
}

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

// caosCargoID is the cargo every chaos pago is applied against. It never
// reaches a database: caosSaldosRepo answers for it in memory.
const caosCargoID = 5000

// caosSaldosRepo is the ONLY double in the harness besides the driver.
// CrearPagoConImagenes validates the cargo before opening the transaction,
// and that read has nothing to do with what these tests measure — routing it
// through the fault-injecting driver would add statements to the sequence and
// shift every ordinal for no gain.
//
// The embedded port is nil ON PURPOSE: CrearPagoConImagenes must read exactly
// one thing from saldos, and any other call panics loudly instead of quietly
// returning a zero value.
type caosSaldosRepo struct {
	cobranzaoutbound.SaldosRepo
	saldo domain.Saldo
}

func newCaosSaldosRepo() *caosSaldosRepo {
	return &caosSaldosRepo{saldo: domain.HydrateSaldo(domain.HydrateSaldoParams{
		DoctoCCID:   caosCargoID,
		ClienteID:   11486,
		Folio:       "CV-CAOS",
		FechaCargo:  time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		PrecioTotal: decimal.NewFromInt(50000),
		Saldo:       decimal.NewFromInt(50000),
		UpdatedAt:   time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC),
	})}
}

func (r *caosSaldosRepo) PorCargo(context.Context, int) (*domain.Saldo, error) {
	return &r.saldo, nil
}

// caosHarness wires a *sql.DB over the fault-injecting driver into the real
// TxManager, the real PagosRecibidosRepo, the real PagoWriter and the real
// cobranza Service. Everything except the saldos read and the driver itself
// is production code.
type caosHarness struct {
	plan *caosPlan
	db   *sql.DB
	svc  *cobranzaapp.Service
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
	repo := cobranzaventfb.NewPagosRecibidosRepo(pool)
	return &caosHarness{
		plan: plan,
		db:   db,
		svc: cobranzaapp.NewService(
			newCaosSaldosRepo(),
			nil, // pagos — unused by CrearPagoConImagenes
			nil, // ventas — unused
			cobranzaoutbound.ProductionClock{},
			repo, // PagosRecibidosRepo
			repo, // PagosImagenesRepo — same struct satisfies both
			cobranzamicrosip.NewPagoWriter(pool),
			nil, // storage — no imagenes on this path
			nil, // imageProc — idem
			firebird.NewTxManager(db),
		),
	}
}

// crearPago drives the real entry point: the same method the multipart
// handler calls when the phone posts a pago with no photo, which is the path
// the phone actually takes.
func (h *caosHarness) crearPago(ctx context.Context) error {
	_, err := h.svc.CrearPagoConImagenes(ctx, buildCaosInput(), nil, uuid.New())
	return err
}

// buildCaosInput is a valid CrearPagoInput built without touching a database,
// so this file stays runnable with FB_DATABASE unset.
func buildCaosInput() cobranzaapp.CrearPagoInput {
	return cobranzaapp.CrearPagoInput{
		ID:             uuid.New(),
		CargoDoctoCCID: caosCargoID,
		ClienteID:      11486,
		CobradorID:     200,
		Cobrador:       "Ramírez García, Jorge",
		Importe:        decimal.NewFromInt(1500),
		FormaCobroID:   87327,
		FechaHoraPago:  time.Now().UTC().Add(-30 * time.Minute),
	}
}

// ─── the statement map of the combined flow ─────────────────────────────────

// caosStmtOrder is the statement sequence CrearPagoConImagenes must issue
// inside its single transaction, in order. It is the assertion, not
// documentation: the harness checks the statement recorded at each ordinal
// really targets this table.
//
//	1  INSERT MSP_PAGOS_RECIBIDOS   PagosRecibidosRepo.Insert
//	2  EXECUTE PROCEDURE            PagoWriter.execGenFolioTemp
//	3  SELECT CLAVES_CLIENTES       PagoWriter.fetchClaveCliente
//	4  INSERT DOCTOS_CC             PagoWriter.insertDoctoCC
//	5  INSERT IMPORTES_DOCTOS_CC    PagoWriter.insertImporteDoctoCC
//	6  INSERT FORMAS_COBRO_DOCTOS   PagoWriter.insertFormaCobroDocto
//	7  UPDATE MSP_PAGOS_RECIBIDOS   the MarcarAplicada write-back
//
// Statements 2-7 all belong to aplicarPagoRecienCreado, the last step of
// persistPagoTx. If that step were moved out of the transaction, statement 1
// would commit alone and the rest would arrive on a second connection — which
// is what opens > 1 and the ordinal check catch.
var caosStmtOrder = []string{
	"INSERT INTO MSP_PAGOS_RECIBIDOS",
	"GEN_FOLIO_TEMP",
	"CLAVES_CLIENTES",
	"INSERT INTO DOCTOS_CC",
	"INSERT INTO IMPORTES_DOCTOS_CC",
	"INSERT INTO FORMAS_COBRO_DOCTOS",
	"UPDATE MSP_PAGOS_RECIBIDOS",
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

// TestCaos_FlujoCompleto_SieteSentenciasYUnCommit is the control that gives
// the failure tests their meaning: with no fault injected the real service
// issues all seven statements over ONE connection and commits exactly once.
// Without it a broken harness that never reached Microsip would make every
// rollback assertion below pass for the wrong reason.
//
// It is also the assertion that the Microsip push lives inside the same
// transaction as the INSERT: one connection, one commit, seven statements.
func TestCaos_FlujoCompleto_SieteSentenciasYUnCommit(t *testing.T) {
	t.Parallel()

	h := newCaosHarness(t, 0, nil)
	require.NoError(t, h.crearPago(context.Background()),
		"with no fault injected the flow must complete")

	c := h.plan.censo()
	assertSecuencia(t, c.stmts, len(caosStmtOrder))
	assert.Equal(t, 1, c.commits, "the flow commits exactly once")
	assert.Equal(t, 0, c.rollbacks, "nothing to roll back on the happy path")
	assert.Equal(t, 1, c.opens,
		"one connection: the whole flow, push included, rides the caller's tx")
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
// transaction back, a commit issued anyway, the push escaping to its own
// connection, and any change to the statement order.
//
// Does NOT detect: whether the rows are physically gone — the double has no
// storage. That is the real-Firebird test's job.
func TestCaos_FalloAMitadDelWriter_LaTransaccionSeDeshace(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nombre string
		failAt int
	}{
		{nombre: "el encabezado DOCTOS_CC es rechazado", failAt: 4},
		{nombre: "entra DOCTOS_CC pero el importe es rechazado", failAt: 5},
		{nombre: "entran encabezado e importe pero la forma de cobro es rechazada", failAt: 6},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()

			rechazo := errors.New("microsip_rechazo_sintetico")
			h := newCaosHarness(t, c.failAt, rechazo)

			err := h.crearPago(context.Background())

			// The rejection reaches the caller untouched: MapError leaves a
			// non-FbError alone, so errors.Is still finds the cause.
			require.Error(t, err, "a rejected statement must not be swallowed")
			require.ErrorIs(t, err, rechazo)

			c2 := h.plan.censo()

			// assertSecuencia is the control for "a mitad", and it is the
			// only one needed: it requires EXACTLY failAt statements and
			// checks each ordinal against caosStmtOrder, so a break at
			// statement 1 — or at any ordinal other than the injected one —
			// fails here. An extra loop re-asserting that the earlier tables
			// were written would be a strict subset of this and could never
			// fail; it was removed rather than left as decoration.
			assertSecuencia(t, c2.stmts, c.failAt)
			assert.Equal(t, 0, c2.commits,
				"a rejected pago must never reach a COMMIT")
			assert.Equal(t, 1, c2.rollbacks,
				"the transaction that carried the half-written document must roll back")
			assert.Equal(t, 1, c2.opens,
				"one connection: the half-write and its rollback share the caller's tx")
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
// Detects: a lock conflict that stops arriving as a Conflict with its shared
// code (the HTTP layer would answer 500 instead of 409, and the failure
// record would carry an opaque message), a connection that is not returned to
// the pool, and any cancellation introduced into this path.
//
// Does NOT detect: the driver-level desync itself. That happens below
// database/sql, where this double sits — it is driverwrap's own test's job.
// This test also does not prove Firebird emits that GDS code under real
// contention. It does not, in fact: see
// pagos_recibidos_bloqueo_integration_test.go, which measures what the real
// blocked path returns.
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
	err := h.crearPago(context.Background())
	require.Error(t, err)

	// 1. It is a Conflict with the shared code, not an opaque 500.
	var ae *apperror.Error
	require.ErrorAs(t, err, &ae, "a lock conflict must arrive mapped, not raw")
	assert.Equal(t, "firebird_lock_conflict", ae.Code)
	assert.Equal(t, apperror.KindConflict, ae.Kind)

	// NOTE, so nobody adds it back: there is no firebird.IsTransient
	// assertion here. IsTransient has no caller in this repo — PagoRetryWorker
	// retries on intentos < MaxIntentos plus backoff, never on transience —
	// so asserting it would claim a consequence the code does not have. The
	// GDS→code mapping itself is already pinned by
	// internal/platform/firebird/errors_test.go; what is NOT pinned anywhere
	// else, and is what this test is for, is that the code survives the whole
	// cobranza write path and that the connection comes back.

	// 2. The transaction was undone, never committed.
	c := h.plan.censo()
	assertSecuencia(t, c.stmts, 4)
	assert.Equal(t, 0, c.commits)
	assert.Equal(t, 1, c.rollbacks)

	// 3. Nobody cancelled a context to get out of the lock — the move that
	//    poisons the firebirdsql pool.
	for i, ctxErr := range c.ctxErrs {
		require.NoErrorf(t, ctxErr,
			"statement %d ran on a cancelled context; cancelling to escape a lock desyncs the driver", i+1)
	}

	// 4. The connection came back. InUse must drain and the next transaction
	//    must reuse the SAME physical connection — opens stays at 1.
	//    Scope, stated plainly: a caosConn is never sick, so this measures
	//    database/sql's bookkeeping — that nothing in the cobranza path holds
	//    a connection hostage after a rejection. It does NOT measure the
	//    firebirdsql wire desync; that is
	//    internal/platform/firebird/poolleak_integration_test.go.
	stats := h.db.Stats()
	assert.Equal(t, 0, stats.InUse,
		"the connection must be returned to the pool after the rejection")
	assert.Equal(t, 1, stats.OpenConnections)

	h.plan.mu.Lock()
	h.plan.failAt = 0
	h.plan.mu.Unlock()
	require.NoError(t, h.crearPago(context.Background()),
		"the pool must still be usable after a lock conflict")

	despues := h.plan.censo()
	assert.Equal(t, c.opens, despues.opens,
		"a second transaction must reuse the connection, not replace a leaked one")
	assert.Equal(t, 1, despues.commits, "the retry commits")
}
