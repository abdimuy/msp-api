//nolint:misspell // Spanish vocabulary (pago, cobrador, bloqueo) by project convention.
package ventfb_test

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
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
// EL LADO BLOQUEADO LO ABRE PRODUCCIÓN, NO LA PRUEBA. That is the whole point
// of the harness below and it is worth stating because the obvious shortcut
// is wrong: opening the blocked transaction with
// conn.BeginTx(&sql.TxOptions{Isolation: sql.LevelReadCommitted}) would
// RE-STATE the isolation flavor firebird.RunInTx picks
// (transaction.go:68) instead of READING it. Measured: with that shortcut in
// place, patching firebird.RunInTx to LevelReadCommittedNoWait — the exact
// change this file's conclusion depends on NOT having happened — left the
// test passing while the real path would have returned
// firebird_lock_conflict immediately. So the blocked side runs on
// firebird.TxManager.RunInTx, the same call CrearPagoConImagenes and
// AplicarPago make.
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

const (
	// bloqueoStatementTimeout is the server-side ceiling for the blocked
	// session. Short enough for a test, long enough that a slow machine
	// cannot mistake normal lock acquisition for the timeout.
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
//   - the write path being moved to RunInTxNoWait (or firebird.RunInTx's
//     isolation being changed underneath it): the blocked call would return
//     firebird_lock_conflict at once and step 3 would fail. This is READ from
//     production — the transaction is opened by firebird.TxManager.RunInTx,
//     never by the test;
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
	txBloqueado := firebird.NewTxManager(pool.DB)

	// ── 1. Control positivo, por el mismo camino de producción ───────────
	// Without this, "the statement was cut" and "the ceiling was never
	// applied" are indistinguishable — the mistake that made the first
	// attempt at this test report the opposite conclusion. It runs through
	// firebird.RunInReadTx on the same pool, so it also proves the ceiling is
	// on the connection production code will be handed.
	inicioLenta := time.Now()
	var total int
	errLenta := firebird.RunInReadTx(context.Background(), pool.DB, func(ctx context.Context) error {
		return firebird.GetQuerier(ctx, pool.DB).
			QueryRowContext(ctx, slowReadOnlyQueryBloqueo).Scan(&total)
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
	<-tomado

	// ── 3. La medición: el camino real de cobranza, bloqueado ────────────
	// firebird.TxManager.RunInTx is the same call CrearPagoConImagenes and
	// AplicarPago make, so the isolation flavor under test is production's
	// choice, not a re-statement of it by the test.
	bloqueado := make(chan error, 1)
	inicio := time.Now()
	go func() {
		bloqueado <- txBloqueado.RunInTx(context.Background(), func(ctx context.Context) error {
			return repoBloqueado.LockByID(ctx, pagoID)
		})
	}()

	var errBloqueado error
	select {
	case errBloqueado = <-bloqueado:
	case <-time.After(bloqueoPresupuesto):
		t.Fatalf("LockByID stayed blocked for %s with a %s ceiling in force: the server is "+
			"NOT cutting lock waits, so a pago blocked by Cxc.exe would hold its pool "+
			"connection with no bound at all. Rewrite this test's conclusion with that "+
			"measurement", bloqueoPresupuesto, bloqueoStatementTimeout)
	}
	transcurrido := time.Since(inicio)
	liberar()
	require.NoError(t, <-holderErr, "the lock holder must be able to commit")

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

	// ── 4. La conexión vuelve ────────────────────────────────────────────
	assert.Equal(t, 0, pool.Stats().InUse,
		"the cut statement must return its connection: %+v", pool.Stats())

	require.NoError(t, txBloqueado.RunInTx(context.Background(), func(ctx context.Context) error {
		return repoBloqueado.LockByID(ctx, pagoID)
	}), "with the holder gone the same row must lock without trouble on the same connection")
}
