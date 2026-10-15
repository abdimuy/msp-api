package firebird_test

// Proves WithStatementTimeout stops a statement IN FIREBIRD, not just in Go:
// the driver ignores context cancellation by design, so before this mechanism
// a request the HTTP layer had already answered kept its statement running on
// the server (measured: a 78 s 503 from the desempeño dashboard in production).
//
// Needs a live Firebird (FB_DATABASE). Every statement is read-only against
// system tables (RDB$TYPES, MON$*), present in any Firebird 4+ database; there
// is nothing to roll back, so the test uses dedicated conns instead of
// fbtestutil.WithTestTransaction (which would pin the conn under test).

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// endlessCount is server-side work only (no fetch until the very end) that
// takes hours: RDB$TYPES has hundreds of rows, four-way cartesian product.
// The marker lets MON$STATEMENTS find exactly this statement.
func endlessCount(marker string) string {
	return `SELECT COUNT(*) /* ` + marker + ` */ FROM RDB$TYPES a, RDB$TYPES b, RDB$TYPES c, RDB$TYPES d`
}

// MON$ tables are a snapshot taken on first access and kept until the
// transaction ENDS. The driver's implicit transaction only commits retaining,
// so a plain pool query can read a minutes-old snapshot (it did: the first
// version of this test saw the aborted statement "still running"). Every MON$
// read therefore runs in a transaction of its own.

// activeStatements counts statements with the marker that Firebird is still
// executing (MON$STATE = 1), seen from a different attachment. The LIKE
// excludes this monitoring query itself, whose text also contains the marker.
func activeStatements(ctx context.Context, t *testing.T, pool *firebird.Pool, marker string) int {
	t.Helper()
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Commit()) }()
	var n int
	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM MON$STATEMENTS
		WHERE MON$STATE = 1
		  AND MON$ATTACHMENT_ID <> CURRENT_CONNECTION
		  AND MON$SQL_TEXT LIKE ?`, "SELECT COUNT(*) /* "+marker+"%").Scan(&n)
	require.NoError(t, err)
	return n
}

// attachmentTimeoutMs reads the session-level statement timeout the server
// holds for conn's own attachment.
func attachmentTimeoutMs(ctx context.Context, t *testing.T, conn *sql.Conn) int64 {
	t.Helper()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Commit()) }()
	var ms int64
	err = tx.QueryRowContext(ctx, `
		SELECT MON$STATEMENT_TIMEOUT FROM MON$ATTACHMENTS
		WHERE MON$ATTACHMENT_ID = CURRENT_CONNECTION`).Scan(&ms)
	require.NoError(t, err)
	return ms
}

func TestWithStatementTimeout_FirebirdAbortsTheStatementOnTheServer(t *testing.T) {
	t.Parallel()
	cfg := fbtestutil.TestFirebirdConfig(t) // skips when FB_DATABASE is unset
	cfg.PoolSize = 4
	pool, err := firebird.New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Stop(context.Background()) })
	require.NoError(t, pool.Start(context.Background()))

	bg := context.Background()
	marker := "prueba-tope-" + uuid.NewString()

	conn, err := pool.Conn(bg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	// Positive control: with a ceiling long enough to observe it, the
	// monitoring query DOES see the statement running. Without this an empty
	// MON$STATEMENTS below would prove nothing.
	controlDone := make(chan error, 1)
	go func() {
		var n int64
		controlDone <- conn.QueryRowContext(firebird.WithStatementTimeout(bg, 6*time.Second), endlessCount(marker)).Scan(&n)
	}()
	require.Eventually(t, func() bool { return activeStatements(bg, t, pool, marker) == 1 },
		4*time.Second, 200*time.Millisecond, "control positivo: la consulta de MON$STATEMENTS debe ver la sentencia en curso")
	controlStart := time.Now()
	select {
	case err = <-controlDone:
	case <-time.After(30 * time.Second):
		t.Fatal("la sentencia de control no terminó: Firebird no aplicó el tope de 6 s")
	}
	require.Error(t, err)
	assert.Less(t, time.Since(controlStart), 10*time.Second)

	// The real check: a 2 s ceiling, with NO Go deadline at all. Only the
	// server can stop this statement.
	start := time.Now()
	var n int64
	err = conn.QueryRowContext(firebird.WithStatementTimeout(bg, 2*time.Second), endlessCount(marker)).Scan(&n)
	elapsed := time.Since(start)
	require.Error(t, err, "una sentencia de horas debe abortarse")
	assert.GreaterOrEqual(t, elapsed, 1500*time.Millisecond, "no debe abortarse antes del tope")
	assert.Less(t, elapsed, 8*time.Second, "el servidor debe abortarla cerca de los 2 s")

	var ae *apperror.Error
	require.ErrorAs(t, firebird.MapError(err), &ae, "error sin mapear: %v", err)
	assert.Equal(t, "firebird_timeout", ae.Code, "Firebird reporta el tope vencido (gds 335545128): %v", err)

	assert.Zero(t, activeStatements(bg, t, pool, marker), "la sentencia ya no debe correr en Firebird")

	// Same attachment, still healthy, and the next statement without an
	// override runs under the process default again.
	acotado := attachmentTimeoutMs(firebird.WithStatementTimeout(bg, 3*time.Second), t, conn)
	assert.Equal(t, int64(3000), acotado)
	def := attachmentTimeoutMs(bg, t, conn)
	assert.Equal(t, firebirdSeconds(firebird.RegisteredDefaultStatementTimeout())*1000, def,
		"la conexión debe volver al tope del proceso (FB_STATEMENT_TIMEOUT) antes de servir a otro")
}

// firebirdSeconds mirrors the wrapper's rounding (whole seconds, rounded up).
func firebirdSeconds(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64((d + time.Second - 1) / time.Second)
}
