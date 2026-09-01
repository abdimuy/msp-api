//nolint:misspell // Spanish vocabulary (pago, cobrador, bloqueo) by project convention.
package ventfb_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	cobranzaoutbound "github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Contención real de fila: qué le pasa a cobranza cuando otra sesión —Cxc.exe
// en la caja— tiene tomada la fila que necesita.
//
// This file exists because the shape of that outcome was argued and never
// measured. The argument ran: cobranza uses TxManager.RunInTx, which is READ
// COMMITTED with WAIT, so a lock held elsewhere does not produce
// firebird_lock_conflict; the blocked statement waits until the server's
// statement timeout cuts it, and that maps to firebird_timeout.
//
// EL LADO BLOQUEADO LO ABRE PRODUCCIÓN, NO LA PRUEBA — Y POR EL SERVICIO.
// This has been walked back twice, so the reasoning is written down.
//
// First shortcut, wrong: opening the blocked transaction with
// conn.BeginTx(&sql.TxOptions{Isolation: sql.LevelReadCommitted}) RE-STATES
// the isolation flavor firebird.RunInTx picks (transaction.go:67) instead of
// READING it. Measured: with that shortcut, patching firebird.RunInTx to
// LevelReadCommittedNoWait left the test passing while the real path would
// have returned firebird_lock_conflict immediately.
//
// Second shortcut, also wrong, one level down: calling
// firebird.TxManager.RunInTx directly reads that function's isolation, but
// NOT the call site. Somebody could move a service method onto a no-wait
// runner with firebird.RunInTx untouched and the test would stay green — and
// that is not hypothetical, since §4.3 of this plan's report recommends
// considering exactly that change.
//
// Third shortcut, wrong for the same reason one level down again: reading
// SOME of the call sites. Service.runInTx (service.go:194) has THREE
// production callers, counted with grep rather than from memory — the count
// was wrong twice in review before somebody measured it:
//
//	aplicar_pago.go:92              AplicarPago            → step 3
//	aplicar_pago.go:193             registrarFalloAparte   → step 6
//	crear_pago_con_imagenes.go:188  persistPagoTx          → step 5
//
// Each was measured to be independently missable: deriving any ONE of them
// onto a no-wait runner, with the other two and firebird.RunInTx untouched,
// left every earlier version of this test green.
//
// So the blocked side is measured THREE times, once per call site. All three
// read the isolation flavor AND the call site from production; none of them
// opens a transaction of the test's own.
//
// WHY THE CEILING GOES ON A PRIVATE ONE-CONNECTION POOL. Setting
// cfg.StatementTimeout does NOT work: firebird.registerOtelDriver wraps its
// registration in a sync.Once (firebird.go:28-51) and
// registerCancelProofDriver captures the timeout there
// (driverwrap.go:41-56), so the ceiling of the FIRST pool built in the
// process is baked into the single registered driver and every later pool
// silently inherits it, whatever its config says. Measured both ways: run
// alone, a private 2s pool never cut the wait (it had really got the shared
// pool's ten minutes); run after another test that registered 2s first, the
// same code was cut at 2.6s — and in that direction it would have imposed two
// seconds on every other pool in the binary.
//
// So the ceiling is applied with SET STATEMENT TIMEOUT on a pool of its own
// limited to ONE connection, which is therefore the connection every
// production call on that pool receives. The pool is closed at cleanup, so
// nothing survives the test — in particular the shared pool is never touched.
//
// The one-connection trick holds for a reason stronger than pool arithmetic:
// cancelProofConn implements neither driver.SessionResetter nor
// driver.Validator (driverwrap.go:150-156 lists Conn, ConnPrepareContext,
// ConnBeginTx, ExecerContext, QueryerContext and Pinger, and nothing else),
// so database/sql has no proactive way to reset or discard that connection
// between uses. The attachment — and with it the ceiling — survives.
//
// What CAN still replace it is driver.ErrBadConn, which firebirdsql returns
// from three places (wireprotocol.go:227, connection.go:83,
// driver_go18.go:95). A replacement connection is opened by
// cancelProofDriver.Open (driverwrap.go:99-106), which applies the
// PROCESS-WIDE ceiling via applyStatementTimeout (driverwrap.go:112-128) —
// ten minutes, since .env sets no
// FB_STATEMENT_TIMEOUT — not this test's two seconds. That is why every
// blocking call below runs under conPresupuesto: without it the test would
// hang past `go test`'s own ten-minute default, die by panic, run no
// t.Cleanup at all, and strand committed rows in the shared DB. That exact
// incident already happened once during this plan.

const (
	// bloqueoStatementTimeout is the server-side ceiling for the blocked
	// session. Short enough for a test, long enough that a slow machine
	// cannot mistake normal lock acquisition for the timeout.
	//
	// IT MUST ALSO STAY BELOW app.falloPersistTimeout (5s), which is the Go
	// deadline registrarFalloAparte puts on its own transaction. That
	// deadline is a SECOND producer of firebird_timeout — MapError sends
	// context.DeadlineExceeded to the same code — so a ceiling at or above it
	// would let step 6 pass for the wrong reason. Measured: raising this to
	// 4s already moves step 6's wait to 4.46s, which is the ceiling still
	// winning; at 5s the two would collide.
	bloqueoStatementTimeout = 2 * time.Second
	// bloqueoPresupuesto is how long an outcome is waited for before calling
	// it a hang. Five times the ceiling.
	bloqueoPresupuesto = 10 * time.Second
	// slowReadOnlyQueryBloqueo is a cartesian product over a system table:
	// cheap to start, slow to drain, writes nothing. The positive control
	// that the ceiling is really in force.
	slowReadOnlyQueryBloqueo = `SELECT COUNT(*) FROM RDB$TYPES a, RDB$TYPES b, RDB$TYPES c, RDB$TYPES d`
)

// poolDeUnaConexionConTechoCorto builds a private pool holding exactly one
// connection and puts a two-second statement ceiling on it. Because the pool
// can never open a second connection, every production call routed through it
// lands on that one attachment and inherits the ceiling.
func poolDeUnaConexionConTechoCorto(t *testing.T) *firebird.Pool {
	t.Helper()
	cfg := fbtestutil.TestFirebirdConfig(t) // skips when FB_DATABASE is unset
	cfg.PoolSize = 1
	pool, err := firebird.New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, pool.Stop(context.Background())) })
	require.NoError(t, pool.Start(context.Background()))

	// One connection, never recycled for the life of the test: the ceiling
	// lives on the attachment, so a replaced connection would lose it.
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	pool.SetConnMaxIdleTime(0)
	pool.SetConnMaxLifetime(0)

	conn, err := pool.Conn(context.Background())
	require.NoError(t, err, "checking out the pool's only connection")
	_, err = conn.ExecContext(context.Background(), setStatementTimeout(bloqueoStatementTimeout))
	require.NoError(t, err, "applying the short statement ceiling")
	require.NoError(t, conn.Close())
	return pool
}

// setStatementTimeout renders the statement Firebird accepts, in whole
// seconds — the same form internal/platform/firebird/driverwrap.go uses.
func setStatementTimeout(d time.Duration) string {
	seconds := int64((d + time.Second - 1) / time.Second)
	return "SET STATEMENT TIMEOUT " + strconv.FormatInt(seconds, 10) + " SECOND"
}

// conPresupuesto runs fn on its own goroutine and fails the test if it has
// not returned within presupuesto.
//
// MUST BE CALLED FROM THE TEST GOROUTINE. The t.Fatal on expiry only stops
// the test when it runs on the goroutine that owns t; from anywhere else it
// marks the test failed and the body keeps going. All uses below comply.
//
// Every blocking call in this file goes through it, not just the one being
// measured. The reason is in the header: a connection replaced via
// driver.ErrBadConn comes back with the process-wide ten-minute ceiling
// instead of this test's two seconds, and an unguarded call would then
// outlive `go test`'s own default timeout — which kills the process without
// running a single t.Cleanup and leaves committed rows in the shared DB.
//
// On expiry the goroutine running fn is LEAKED, on purpose: it is blocked in
// the driver and there is no safe way to interrupt it (cancelling its context
// is the move that desyncs the firebirdsql wire). The channel is buffered so
// it can never wedge on send. What it can still do, once the lock holder is
// released, is finish its statement against a pool that is closing. The
// t.Cleanup order makes that harmless: cleanups run LIFO, so the holder is
// released BEFORE pool.Stop, and pool.Stop waits on database/sql's own
// bookkeeping rather than yanking the socket.
func conPresupuesto(t *testing.T, presupuesto time.Duration, mensaje string, fn func() error) error {
	t.Helper()
	hecho := make(chan error, 1)
	go func() { hecho <- fn() }()
	return recibirConPresupuesto(t, presupuesto, mensaje, hecho)
}

// recibirConPresupuesto is conPresupuesto for a goroutine that was started
// earlier, which step 6 needs because its call has to be in flight before the
// lock holder can even try to take the lock. Same contract: test goroutine
// only, and the channel must be buffered.
func recibirConPresupuesto[T any](t *testing.T, presupuesto time.Duration, mensaje string, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(presupuesto):
		t.Fatal(mensaje)
		var cero T
		return cero
	}
}

// TestE2E_ContencionDeFila_LaEsperaSeCortaComoTimeoutNoComoConflicto is the
// measurement this plan was missing.
//
// MEDIDO: with a two-second ceiling in force — proven in force by the control
// in step 1, which runs through the SAME production entry point on the SAME
// pool — a PagosRecibidosRepo.LockByID blocked by another session's row lock
// is cut by the server in ~2s and arrives as firebird_timeout /
// KindServiceUnavailable. NOT as firebird_lock_conflict.
//
// CONSECUENCIA para producción, donde el techo es FB_STATEMENT_TIMEOUT: un
// pago que choca con un cargo bloqueado por Cxc.exe retiene una conexión del
// pool hasta diez minutos, y lo que devuelve entonces es un 503 sin ninguna
// pista de que hubo un bloqueo. Ni el HTTP ni el retry worker distinguen ese
// caso de una base caída.
//
// WHAT THIS DETECTS:
//   - the blocked path hanging with no ceiling at all;
//   - the cut arriving unclassified;
//   - the cobranza write path losing its WAIT semantics, by any of the four
//     routes: firebird.RunInTx's isolation changed underneath it, or any one
//     of its three callers moved onto a no-wait runner — AplicarPago,
//     persistPagoTx, or registrarFalloAparte. Each makes the corresponding
//     blocked call return early and fails step 3, 5 or 6. All READ from
//     production: none of the three BLOCKED sides opens a transaction of the
//     test's own. The scaffolding around them does — the control query, the
//     three lock holders, the re-take probe, the seed INSERT and the INTENTOS
//     read are all the test's, and none of them is what is being measured;
//   - the connection not coming back after the cut. That is the one that
//     matters operationally: a stuck connection is a pool slot gone for good.
//
// WHAT IT DOES NOT DETECT:
//   - the OTHER contention outcome. This measures one scenario: the holder is
//     still holding when the ceiling expires. If the holder COMMITS first,
//     Firebird can answer the waiter with an update conflict / deadlock
//     (GDS 335544336) instead, which MapError does collapse into
//     firebird_lock_conflict. So "cobranza never sees firebird_lock_conflict"
//     is NOT what is proven here, and the assertion in step 3 is scoped to
//     this scenario on purpose;
//   - anything specific to Cxc.exe. The holder is a second Go transaction; to
//     Firebird the two are indistinguishable, but the real Cxc.exe also holds
//     SALDOS_CC and MSP_SALDOS_VENTAS, which this does not touch;
//   - production's actual wait, which is FB_STATEMENT_TIMEOUT (ten minutes),
//     not two seconds. The shape is measured here, not the duration.
//
//nolint:paralleltest // holds a real row lock; t.Parallel would race with cleanup.
func TestE2E_ContencionDeFila_LaEsperaSeCortaComoTimeoutNoComoConflicto(t *testing.T) {
	h := newConcurrencyHarness(t)
	pool := poolDeUnaConexionConTechoCorto(t)
	repoBloqueado := cobranzaventfb.NewPagosRecibidosRepo(pool)

	// The blocked side is the real cobranza Service on the ceiling-carrying
	// pool. AplicarPago's first act inside its transaction is LockByID, so
	// the block happens there — and the writer is never reached, which the
	// assertion at the end proves.
	writer := &recordingFakeWriter{}
	svcBloqueado := cobranzaapp.NewService(
		// saldos answers the cargo check CrearPagoConImagenes makes BEFORE
		// opening its transaction; routing it through the ceiling-carrying
		// pool would add a statement to the measurement for no gain.
		// AplicarPago never touches it.
		newCaosSaldosRepo(),
		nil, nil, // pagos/ventas — neither entry point needs them
		cobranzaoutbound.ProductionClock{},
		repoBloqueado, // PagosRecibidosRepo
		repoBloqueado, // PagosImagenesRepo — same struct satisfies both
		writer,
		nil, nil, // storage, imageProc — unused
		firebird.NewTxManager(pool.DB),
	)

	// ── 1. Control positivo, por el mismo camino de producción ───────────
	// Without this, "the statement was cut" and "the ceiling was never
	// applied" are indistinguishable — the mistake that made the first
	// attempt at this test report the opposite conclusion. It runs through
	// firebird.RunInReadTx on the same pool, so it also proves the ceiling is
	// on the connection production code will be handed.
	inicioLenta := time.Now()
	var total int
	errLenta := conPresupuesto(t, bloqueoPresupuesto,
		"el control positivo no devolvió a tiempo: la conexión no lleva el techo corto "+
			"(probablemente fue reemplazada vía driver.ErrBadConn y trae el del proceso), "+
			"así que nada de lo que sigue se puede interpretar",
		func() error {
			return firebird.RunInReadTx(context.Background(), pool.DB, func(ctx context.Context) error {
				return firebird.GetQuerier(ctx, pool.DB).
					QueryRowContext(ctx, slowReadOnlyQueryBloqueo).Scan(&total)
			})
		})
	esperaLenta := time.Since(inicioLenta)

	require.Error(t, errLenta, "the slow query must be cut by the ceiling")
	var aeLenta *apperror.Error
	require.ErrorAs(t, firebird.MapError(errLenta), &aeLenta)
	require.Equal(t, "firebird_timeout", aeLenta.Code,
		"the control must be a server cut, not something else")
	require.Less(t, esperaLenta, bloqueoPresupuesto,
		"ceiling is %s; the control took %s", bloqueoStatementTimeout, esperaLenta)
	t.Logf("control positivo: consulta lenta cortada en %s (code=%s)", esperaLenta, aeLenta.Code)

	// ── 2. Una fila comprometida, y otra sesión que la bloquea ────────────
	pagoID := uuid.New()
	h.trackID(pagoID)
	require.NoError(t, h.txMgr.RunInTx(context.Background(), func(ctx context.Context) error {
		return h.repo.Insert(ctx, buildValidPagoRecibidoWithID(t, pagoID))
	}))

	tomado := make(chan struct{})
	soltar := make(chan struct{})
	// Release exactly once and ALWAYS: if an assertion below fails, the
	// holder must still let go, or the harness cleanup would block trying to
	// delete a row somebody has locked.
	liberar := sync.OnceFunc(func() { close(soltar) })
	t.Cleanup(liberar)

	holderErr := make(chan error, 1)
	go func() {
		holderErr <- h.txMgr.RunInTx(context.Background(), func(ctx context.Context) error {
			if e := h.repo.LockByID(ctx, pagoID); e != nil {
				return e
			}
			close(tomado)
			<-soltar
			return nil
		})
	}()
	recibirConPresupuesto(t, bloqueoPresupuesto,
		"el bloqueador del paso 2 nunca tomó el lock", tomado)

	// ── 3. La medición: el camino real de cobranza, bloqueado ────────────
	// Service.AplicarPago, not a transaction the test opened: the isolation
	// flavor AND the call site are production's choice.
	inicio := time.Now()
	errBloqueado := conPresupuesto(t, bloqueoPresupuesto,
		fmt.Sprintf("AplicarPago stayed blocked for %s with a %s ceiling in force: the "+
			"server is NOT cutting lock waits, so a pago blocked by Cxc.exe would hold "+
			"its pool connection with no bound at all. Rewrite this test's conclusion "+
			"with that measurement", bloqueoPresupuesto, bloqueoStatementTimeout),
		func() error {
			_, err := svcBloqueado.AplicarPago(context.Background(), pagoID, uuid.Nil)
			return err
		})
	transcurrido := time.Since(inicio)
	liberar()
	require.NoError(t, recibirConPresupuesto(t, bloqueoPresupuesto,
		"el bloqueador del paso 2 no terminó tras soltarlo", holderErr),
		"the lock holder must be able to commit")

	require.Error(t, errBloqueado, "a row held by another session must not let LockByID through")
	var ae *apperror.Error
	require.ErrorAs(t, errBloqueado, &ae,
		"the blocked path must return a classified error, got %T: %v", errBloqueado, errBloqueado)
	t.Logf("contención medida: espera=%s code=%s kind=%v", transcurrido, ae.Code, ae.Kind)

	assert.Equal(t, "firebird_timeout", ae.Code,
		"in THIS scenario — the holder is still holding when the ceiling expires — the "+
			"wait is cut as a statement timeout. firebird_lock_conflict here would mean "+
			"the transaction is no longer the WAIT flavor, i.e. somebody moved the write "+
			"path to RunInTxNoWait or changed firebird.RunInTx underneath it, and the "+
			"retry semantics moved with it. (A DIFFERENT scenario — the holder commits "+
			"first — can legitimately yield GDS 335544336, which MapError also collapses "+
			"into firebird_lock_conflict; that path is not measured here.)")
	assert.Equal(t, apperror.KindServiceUnavailable, ae.Kind,
		"a blocked pago surfaces as 503, indistinguishable from a database that is down")

	// ── 4. La ranura del pool no se perdió ───────────────────────────────
	// Stated precisely, because the previous wording claimed more than this
	// measures: sql.DBStats exposes no counter of physical opens, so this is
	// NOT proof that the same connection was reused. What it proves is that
	// the cut statement gave its slot back — and with MaxOpenConns(1) that is
	// still worth proving, since a lost slot leaves the pool with none and
	// shows up as the budget below expiring rather than as a failed
	// assertion.
	assert.Equal(t, 0, pool.Stats().InUse,
		"the cut statement must return its pool slot: %+v", pool.Stats())

	require.NoError(t, conPresupuesto(t, bloqueoPresupuesto,
		"the pool never served again after the cut: its only slot was not given back",
		func() error {
			return firebird.RunInTx(context.Background(), pool.DB, func(ctx context.Context) error {
				return repoBloqueado.LockByID(ctx, pagoID)
			})
		}), "with the holder gone the same row must lock without trouble")

	// The writer was never reached: the block happened at the lock, which is
	// what makes this a contention measurement and not a Microsip one.
	writer.mu.Lock()
	llamadasWriter := writer.callCount
	writer.mu.Unlock()
	assert.Equal(t, 0, llamadasWriter,
		"AplicarPago must have died at LockByID, before touching Microsip")

	// ── 5. El OTRO call site: CrearPagoConImagenes ───────────────────────
	// The second of Service.runInTx's three callers. It is the one that
	// matters most: the phone's
	// push to Microsip goes through persistPagoTx, so it is where somebody
	// would most plausibly apply the "make the push no-wait" recommendation.
	//
	// The block is produced differently here — CrearPagoConImagenes takes no
	// row lock; its transaction opens with the pago INSERT. So the holder
	// INSERTS the same UUID and does not commit: in WAIT mode the second
	// INSERT waits on the uncommitted unique-index entry, in NO WAIT it is
	// refused at once. The holder then ROLLS BACK, so unlike step 2 this step
	// commits nothing at all.
	pagoNuevoID := uuid.New()
	h.trackID(pagoNuevoID) // defensivo: si algo commiteara, se limpia igual

	insertTomado := make(chan struct{})
	insertSoltar := make(chan struct{})
	liberarInsert := sync.OnceFunc(func() { close(insertSoltar) })
	t.Cleanup(liberarInsert)

	// Built here and not inside the goroutine: buildValidPagoRecibidoWithID
	// asserts, and testifylint's go-require rule is right that an assertion
	// off the test goroutine does not stop the test.
	pagoBloqueador := buildValidPagoRecibidoWithID(t, pagoNuevoID)

	errRollbackDeliberado := errors.New("el bloqueador revierte a propósito")
	insertHolder := make(chan error, 1)
	go func() {
		insertHolder <- h.txMgr.RunInTx(context.Background(), func(ctx context.Context) error {
			if e := h.repo.Insert(ctx, pagoBloqueador); e != nil {
				return e
			}
			close(insertTomado)
			<-insertSoltar
			return errRollbackDeliberado
		})
	}()
	recibirConPresupuesto(t, bloqueoPresupuesto,
		"el bloqueador del paso 5 nunca dejó su INSERT en vuelo", insertTomado)

	inicioCrear := time.Now()
	errCrear := conPresupuesto(t, bloqueoPresupuesto,
		fmt.Sprintf("CrearPagoConImagenes stayed blocked for %s with a %s ceiling in "+
			"force: same conclusion as step 3 — the server is not cutting lock waits",
			bloqueoPresupuesto, bloqueoStatementTimeout),
		func() error {
			_, err := svcBloqueado.CrearPagoConImagenes(
				context.Background(), inputBloqueoConID(pagoNuevoID), nil, uuid.Nil)
			return err
		})
	transcurridoCrear := time.Since(inicioCrear)

	liberarInsert()
	require.ErrorIs(t, recibirConPresupuesto(t, bloqueoPresupuesto,
		"el bloqueador del paso 5 no terminó tras soltarlo", insertHolder),
		errRollbackDeliberado,
		"the blocking INSERT must roll back so this step commits nothing")

	require.Error(t, errCrear, "a UUID another transaction is inserting must not go through")
	var aeCrear *apperror.Error
	require.ErrorAs(t, errCrear, &aeCrear,
		"the blocked create must return a classified error, got %T: %v", errCrear, errCrear)
	t.Logf("contención medida (CrearPagoConImagenes): espera=%s code=%s kind=%v",
		transcurridoCrear, aeCrear.Code, aeCrear.Kind)

	// Anything other than a timeout here means persistPagoTx — the
	// transaction that carries the phone's push to Microsip — is no longer
	// the WAIT flavor. The symptom is NOT the firebird_lock_conflict step 3
	// produces, which is worth writing down because it is worse: measured
	// with persistPagoTx alone on a no-wait runner, Firebird reports the
	// duplicate key against the uncommitted row as a UNIQUE VIOLATION, the
	// repo maps that to ErrPagoYaExiste, persistPagoTx takes its idempotent
	// replay branch, FindByID finds nothing (the twin was never committed),
	// and the phone gets pago_no_encontrado — a 404 for a pago that was never
	// created, in 17ms.
	//
	// Three things that sentence must not be read as claiming:
	//
	//  1. That unique-violation BEATS lock-conflict is a MEASUREMENT, not an
	//     invariant. MapError walks fbErr.GDSCodes and returns at the
	//     first recognized code (errors.go:54-86), with unique violation
	//     (335544665 / 335544349) and lock conflict (335544345) in different
	//     branches of the same loop. Which one wins depends on the order of
	//     the status vector the driver hands back.
	//  2. With the code as it stands (WAIT) that 404 DOES NOT EXIST. If the
	//     twin commits, FindByID finds it and the caller gets an idempotent
	//     200; if it rolls back, the INSERT goes through. The 404 is a hazard
	//     CONDITIONAL on adopting the "make the push no-wait" recommendation,
	//     not a live defect.
	//  3. The step is not resting on the blocker having blocked. If the
	//     INSERT went through instead, recordingFakeWriter returns zeroes and
	//     MarcarAplicada refuses them with ErrPagoDoctoCCIDInvalido
	//     (pago_recibido.go:348-350), so the transaction unwinds anyway and
	//     the require.Error below still holds.
	assert.Equal(t, "firebird_timeout", aeCrear.Code,
		"same scoping as step 3, on the other call site: a non-timeout here means "+
			"persistPagoTx lost its WAIT semantics")

	// ── 6. El TERCER call site: registrarFalloAparte ─────────────────────
	// This is the caller nobody had measured, and the worst one to leave
	// unread. registrarFalloAparte is the transaction that RE-TAKES the row
	// lock after Microsip rejected a pago, to record INTENTOS and
	// ULTIMO_ERROR. It is reachable from the retry worker
	// (pago_retry_worker.go:176) and from the admin endpoint, and it is the
	// most natural place for somebody to say "this one need not wait ten
	// minutes, it is only the failure record".
	//
	// If they do, a lock held by Cxc.exe means the attempt is NOT COUNTED:
	// INTENTOS stays at 0, the worker hammers the pago every tick with no
	// backoff, and the only trace is the pago.fallo_no_persistido line that
	// AplicarPago itself calls "the one degradation this flow can still
	// suffer in silence".
	//
	// Reaching it under contention needs the lock to be taken in the window
	// between the apply transaction rolling back and the failure record
	// starting — a window the test cannot hook from outside. The writer
	// double is the hook: it runs INSIDE the apply transaction, while that
	// transaction holds the row lock. So the holder is launched from there
	// and blocks on the lock the apply tx is holding; when the writer returns
	// its rejection and the apply tx rolls back, the holder is next in
	// Firebird's queue and takes it, and registrarFalloAparte lands behind it.
	pago6ID := uuid.New()
	h.trackID(pago6ID)
	pago6 := buildValidPagoRecibidoWithID(t, pago6ID)
	require.NoError(t, h.txMgr.RunInTx(context.Background(), func(ctx context.Context) error {
		return h.repo.Insert(ctx, pago6)
	}))

	rechazoMicrosip := errors.New("microsip_rechazo_para_medir_el_registro_del_fallo")
	writer6 := &writerQueSostieneElLock{
		err:        rechazoMicrosip,
		enElWriter: make(chan struct{}),
		seguir:     make(chan struct{}),
	}
	svcFallo := cobranzaapp.NewService(
		newCaosSaldosRepo(), nil, nil,
		cobranzaoutbound.ProductionClock{},
		repoBloqueado, repoBloqueado, writer6, nil, nil,
		firebird.NewTxManager(pool.DB),
	)

	aplicarErr := make(chan error, 1)
	go func() {
		_, err := svcFallo.AplicarPago(context.Background(), pago6ID, uuid.Nil)
		aplicarErr <- err
	}()

	// The apply transaction is now inside the writer, holding the row lock.
	recibirConPresupuesto(t, bloqueoPresupuesto,
		"AplicarPago nunca llegó al writer: sin eso no hay ventana donde medir "+
			"registrarFalloAparte", writer6.enElWriter)

	soltar6 := make(chan struct{})
	liberar6 := sync.OnceFunc(func() { close(soltar6) })
	t.Cleanup(liberar6)
	errRollback6 := errors.New("el bloqueador del paso 6 revierte a propósito")
	holder6 := make(chan error, 1)
	tomado6 := make(chan struct{})
	go func() {
		holder6 <- h.txMgr.RunInTx(context.Background(), func(ctx context.Context) error {
			if e := h.repo.LockByID(ctx, pago6ID); e != nil {
				return e
			}
			close(tomado6)
			<-soltar6
			return errRollback6
		})
	}()

	// A barrier, not a guess about speed: the holder's LockByID has to have
	// reached the server and joined the lock queue before the apply tx lets
	// go, or the apply tx's own next transaction would win the lock instead.
	// No channel can signal "my statement is now blocked in the driver", so
	// this is a sleep — and it is safe to get wrong in one direction only: if
	// it is too short the holder loses the race, registrarFalloAparte
	// SUCCEEDS, AplicarPago returns the bare writer error with no apperror
	// inside it, and the ErrorAs below fails. The test cannot pass vacuously.
	time.Sleep(500 * time.Millisecond)
	close(writer6.seguir)
	// Guarded like every other wait in this file, and NOT as a formality:
	// measured, with firebird.RunInTx globally switched to no-wait this very
	// receive hung for the whole `go test` budget — the holder's own LockByID
	// was refused instead of queuing, so tomado6 was never closed. An
	// unguarded receive there turns a clean failure into a hang, and a hang
	// runs no t.Cleanup and strands committed rows.
	recibirConPresupuesto(t, bloqueoPresupuesto,
		"el bloqueador del paso 6 nunca tomó el lock; sin él registrarFalloAparte no "+
			"encuentra contención y no hay nada que medir", tomado6)

	inicioFallo := time.Now()
	errAplicar := recibirConPresupuesto(t, bloqueoPresupuesto,
		fmt.Sprintf("AplicarPago never returned within %s: registrarFalloAparte is blocked "+
			"on the row lock and was not cut by the %s ceiling",
			bloqueoPresupuesto, bloqueoStatementTimeout),
		aplicarErr)
	transcurridoFallo := time.Since(inicioFallo)

	liberar6()
	require.ErrorIs(t, recibirConPresupuesto(t, bloqueoPresupuesto,
		"el bloqueador del paso 6 no terminó tras soltarlo", holder6),
		errRollback6, "the step-6 blocker must roll back")

	// Both errors come back joined: the rejection says what Microsip
	// answered, the second says why nobody will see it recorded.
	require.Error(t, errAplicar)
	require.ErrorIs(t, errAplicar, rechazoMicrosip,
		"the Microsip rejection must survive the join")
	var aeFallo *apperror.Error
	require.ErrorAs(t, errAplicar, &aeFallo,
		"registrarFalloAparte must have failed too and joined its error; got %v", errAplicar)
	t.Logf("contención medida (registrarFalloAparte): espera=%s code=%s kind=%v",
		transcurridoFallo, aeFallo.Code, aeFallo.Kind)

	// Measured detail, so the narrative does not drift: when this call site is
	// put on a no-wait runner the server answers with GDS 335544336
	// (deadlock / update conflicts with concurrent update), NOT 335544345
	// (lock conflict on no wait transaction). MapError collapses both into
	// firebird_lock_conflict, so the assertion below catches it either way —
	// but the origin is the concurrent-update branch, not the no-wait one.
	assert.Equal(t, "firebird_timeout", aeFallo.Code,
		"same scoping as steps 3 and 5, on the third call site: a non-timeout here means "+
			"registrarFalloAparte lost its WAIT semantics")

	// The damage, recorded but NOT the discriminator — it is the same whether
	// the failure record was cut by a timeout or refused by a no-wait
	// conflict. It is here because it is the reason the call site matters.
	var intentos int
	require.NoError(t, firebird.RunInReadTx(context.Background(), h.pool.DB, func(ctx context.Context) error {
		return firebird.GetQuerier(ctx, h.pool.DB).
			QueryRowContext(ctx, "SELECT INTENTOS FROM MSP_PAGOS_RECIBIDOS WHERE ID = ?",
				pago6ID.String()).Scan(&intentos)
	}))
	assert.Equal(t, 0, intentos,
		"the blocked failure record leaves INTENTOS at 0: the worker will retry this pago "+
			"every tick with no backoff and nobody can see what Microsip said")
}

// writerQueSostieneElLock is the hook that lets step 6 reach
// registrarFalloAparte under contention. It runs inside AplicarPago's
// transaction — which is holding the row lock — so the test can launch the
// lock holder from there and have it queue behind that very lock.
//
// The rejection it returns is a PLAIN error on purpose: AplicarPago joins it
// with registrarFalloAparte's error, and errors.As walks the join in order,
// so an apperror here would shadow the one the assertions are about.
type writerQueSostieneElLock struct {
	mu         sync.Mutex
	calls      int
	err        error
	enElWriter chan struct{}
	seguir     chan struct{}
}

func (w *writerQueSostieneElLock) Aplicar(
	context.Context, cobranzaoutbound.MicrosipPagoInput,
) (cobranzaoutbound.MicrosipPagoResult, error) {
	w.mu.Lock()
	w.calls++
	w.mu.Unlock()
	close(w.enElWriter)
	<-w.seguir
	return cobranzaoutbound.MicrosipPagoResult{}, w.err
}

var _ cobranzaoutbound.MicrosipPagoWriter = (*writerQueSostieneElLock)(nil)

// inputBloqueoConID builds the CrearPagoInput for step 5. The cargo is the
// one caosSaldosRepo answers for, and the importe fits inside its saldo, so
// the pre-transaction validation passes without touching any database.
func inputBloqueoConID(id uuid.UUID) cobranzaapp.CrearPagoInput {
	return cobranzaapp.CrearPagoInput{
		ID:             id,
		CargoDoctoCCID: caosCargoID,
		ClienteID:      11486,
		CobradorID:     200,
		Cobrador:       "Ramírez García, Jorge",
		Importe:        decimal.NewFromInt(1500),
		FormaCobroID:   87327,
		FechaHoraPago:  time.Now().UTC().Add(-30 * time.Minute),
	}
}
