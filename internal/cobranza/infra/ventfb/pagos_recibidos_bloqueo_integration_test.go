//nolint:misspell // Spanish vocabulary (pago, cobrador, bloqueo) by project convention.
package ventfb_test

import (
	"context"
	"database/sql"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Contención real de fila: qué le pasa a cobranza cuando otra sesión —Cxc.exe
// en la caja— tiene tomada la fila que necesita.
//
// This file exists because the shape of that outcome was argued and never
// measured. The argument ran: cobranza uses TxManager.RunInTx, which is READ
// COMMITTED with WAIT, so a lock held elsewhere never produces
// firebird_lock_conflict; the blocked statement waits until the server's
// statement timeout cuts it, and that maps to firebird_timeout instead.
//
// Both halves are now measured, and both hold.
//
// WHY THE CEILING IS SET ON ONE CONNECTION AND NOT ON A POOL. The obvious
// shape — build a private *firebird.Pool with a two-second
// cfg.StatementTimeout — DOES NOT WORK, and failing quietly is the worst part
// of it. firebird.registerOtelDriver wraps its registration in a
// sync.Once (firebird.go:28-53), so the statement timeout of the FIRST pool
// built in the process is baked into the one registered driver and every
// later pool silently inherits it, whatever its config says. Measured: run
// alone, a private 2s pool never cut the lock wait (it had really got the
// shared pool's 10 minutes); run after another test that had built a 2s pool
// first, the same code was cut at 2.6s. Worse in the other direction — a test
// that managed to register 2s first would impose it on every other pool in
// the same binary. So the ceiling here is applied with SET STATEMENT TIMEOUT
// on a single checked-out connection and restored before it goes back.

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
	// that the ceiling is really in force on this connection.
	slowReadOnlyQueryBloqueo = `SELECT COUNT(*) FROM RDB$TYPES a, RDB$TYPES b, RDB$TYPES c, RDB$TYPES d`
)

// conexionConTechoCorto checks out ONE physical connection, puts a
// two-second statement ceiling on it, and restores the pool's configured
// ceiling before handing it back. Scoped on purpose: SET STATEMENT TIMEOUT
// applies to the attachment, so a connection returned to the pool still
// carrying two seconds would cut unrelated tests.
func conexionConTechoCorto(t *testing.T, pool *firebird.Pool) *sql.Conn {
	t.Helper()
	original := fbtestutil.TestFirebirdConfig(t).StatementTimeout

	conn, err := pool.Conn(context.Background())
	require.NoError(t, err, "checking out a dedicated connection")
	t.Cleanup(func() {
		// Restore FIRST, close second. A connection handed back with the
		// short ceiling still on it would cut somebody else's query.
		_, restoreErr := conn.ExecContext(context.Background(), setStatementTimeout(original))
		assert.NoError(t, restoreErr, "restoring the connection's statement ceiling")
		assert.NoError(t, conn.Close())
	})

	_, err = conn.ExecContext(context.Background(), setStatementTimeout(bloqueoStatementTimeout))
	require.NoError(t, err, "applying the short statement ceiling")
	return conn
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
// MEDIDO, en una conexión con techo de 2 s (probado activo por el control
// positivo del paso 1, que corta una consulta lenta en la MISMA conexión):
// una PagosRecibidosRepo.LockByID bloqueada por el lock de fila de otra sesión
// SÍ es cortada por el servidor, en ~2 s, y llega como
// firebird_timeout / KindServiceUnavailable — NO como firebird_lock_conflict.
//
// CONSECUENCIA para producción, donde el techo es FB_STATEMENT_TIMEOUT: un
// pago que choca con un cargo bloqueado por Cxc.exe retiene una conexión del
// pool hasta diez minutos, y lo que devuelve entonces es un 503 sin ninguna
// pista de que hubo un bloqueo. Ni el HTTP ni el retry worker distinguen ese
// caso de una base caída.
//
// WHAT THIS DETECTS:
//   - the blocked path hanging with no ceiling at all;
//   - the cut arriving unclassified, or classified as a lock conflict — which
//     would mean somebody moved the write path to RunInTxNoWait and changed
//     the retry semantics with it;
//   - the connection not coming back after the cut. That is the one that
//     matters operationally: a stuck connection is a pool slot gone for good.
//
// WHAT IT DOES NOT DETECT:
//   - anything specific to Cxc.exe. The holder is a second Go transaction; to
//     Firebird the two are indistinguishable, but the real Cxc.exe also holds
//     SALDOS_CC and MSP_SALDOS_VENTAS, which this does not touch;
//   - production's actual wait, which is FB_STATEMENT_TIMEOUT (ten minutes),
//     not two seconds. The shape is measured here, not the duration.
//
//nolint:paralleltest // holds a real row lock; t.Parallel would race with cleanup.
func TestE2E_ContencionDeFila_LaEsperaSeCortaComoTimeoutNoComoConflicto(t *testing.T) {
	h := newConcurrencyHarness(t)
	conn := conexionConTechoCorto(t, h.pool)

	// ── 1. Control positivo: el techo está puesto en ESTA conexión ────────
	// Without this, "the statement was cut" and "the statement was never
	// going to run" are indistinguishable, and so are "it was not cut" and
	// "the ceiling was never applied".
	inicioLenta := time.Now()
	var total int
	errLenta := conn.QueryRowContext(context.Background(), slowReadOnlyQueryBloqueo).Scan(&total)
	esperaLenta := time.Since(inicioLenta)

	require.Error(t, errLenta, "the slow query must be cut by the ceiling on this connection")
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

	// ── 3. La medición: el camino real de cobranza, bloqueado ─────────────
	// The transaction is opened on the ceiling-carrying connection and
	// planted on the context with the same seam fbtestutil uses, so the code
	// under test is the production PagosRecibidosRepo.LockByID.
	tx, err := conn.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	bloqueado := make(chan error, 1)
	inicio := time.Now()
	go func() {
		bloqueado <- h.repo.LockByID(firebird.InjectTx(context.Background(), tx), pagoID)
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

	// It was cut, near the ceiling.
	require.Error(t, errBloqueado, "a row held by another session must not let LockByID through")
	var ae *apperror.Error
	require.ErrorAs(t, errBloqueado, &ae,
		"the blocked path must return a classified error, got %T: %v", errBloqueado, errBloqueado)
	t.Logf("contención medida: espera=%s code=%s kind=%v", transcurrido, ae.Code, ae.Kind)

	assert.Equal(t, "firebird_timeout", ae.Code,
		"measured: a lock wait is cut as a statement timeout. It is NOT "+
			"firebird_lock_conflict — RunInTx is the WAIT flavor and nothing in cobranza "+
			"asks for NO WAIT. If this ever flips, the write path moved to RunInTxNoWait "+
			"and the retry semantics moved with it")
	assert.Equal(t, apperror.KindServiceUnavailable, ae.Kind,
		"a blocked pago surfaces as 503, indistinguishable from a database that is down")

	// ── 4. La conexión vuelve ─────────────────────────────────────────────
	assert.NoError(t, tx.Rollback())
	var uno int
	require.NoError(t, conn.QueryRowContext(context.Background(), "SELECT 1 FROM RDB$DATABASE").Scan(&uno),
		"the connection must still serve after its statement was cut")
	assert.Equal(t, 1, uno)
}
