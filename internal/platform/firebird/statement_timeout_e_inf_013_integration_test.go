package firebird_test

// E-INF-013 (docs/evidencia/infra.md): no statement issued without
// WithStatementTimeout runs under the narrow ceiling. These tests are the
// measurements behind that claim, against the dev Firebird. Read-only: the
// only "write" is a USER_TRANSACTION context variable, which lives and dies
// with a transaction that is rolled back.

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// streamingQuery is cheap to start and takes minutes to drain.
const streamingQuery = `SELECT a.RDB$TYPE FROM RDB$TYPES a, RDB$TYPES b, RDB$TYPES c`

// noopExec runs through ExecContext without fetching anything.
const noopExec = `EXECUTE BLOCK AS BEGIN END`

func newE013Pool(t *testing.T) (*firebird.Pool, int64) {
	t.Helper()
	cfg := fbtestutil.TestFirebirdConfig(t) // skips when FB_DATABASE is unset
	cfg.PoolSize = 4
	pool, err := firebird.New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Stop(context.Background()) })
	require.NoError(t, pool.Start(context.Background()))
	return pool, firebirdSeconds(firebird.RegisteredDefaultStatementTimeout()) * 1000
}

// timeoutOfAttachment reads another attachment's session timeout from the
// pool (a different conn), in a transaction of its own so the MON$ snapshot
// is fresh.
func timeoutOfAttachment(ctx context.Context, t *testing.T, pool *firebird.Pool, id int64) int64 {
	t.Helper()
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Commit()) }()
	var ms int64
	require.NoError(t, tx.QueryRowContext(ctx,
		`SELECT MON$STATEMENT_TIMEOUT FROM MON$ATTACHMENTS WHERE MON$ATTACHMENT_ID = ?`, id).Scan(&ms))
	return ms
}

// fetchFor reads rows, pausing between them, until the cursor fails, ends or
// d elapses. It returns the error (nil if none) and how long it read.
func fetchFor(rows *sql.Rows, d time.Duration) (time.Duration, error) {
	start := time.Now()
	for time.Since(start) < d {
		if !rows.Next() {
			return time.Since(start), rows.Err()
		}
		time.Sleep(5 * time.Millisecond)
	}
	return time.Since(start), nil
}

func TestE_INF_013_ExecWithoutOptionRestoresSeenFromAnotherConnection(t *testing.T) {
	t.Parallel()
	pool, defMs := newE013Pool(t)
	bg := context.Background()
	c1, err := pool.Conn(bg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c1.Close() })

	var id int64
	require.NoError(t, c1.QueryRowContext(bg, `SELECT CURRENT_CONNECTION FROM RDB$DATABASE`).Scan(&id))

	var one int
	require.NoError(t, c1.QueryRowContext(firebird.WithStatementTimeout(bg, 2*time.Second),
		`SELECT 1 FROM RDB$DATABASE`).Scan(&one))
	// Positive control: the other connection DOES see the narrow ceiling.
	require.Equal(t, int64(2000), timeoutOfAttachment(bg, t, pool, id),
		"control positivo: desde otra conexión se ve el tope acotado")

	_, err = c1.ExecContext(bg, noopExec)
	require.NoError(t, err)
	t.Logf("attachment %d tras ExecContext sin opción: %d ms (proceso %d ms)", id, timeoutOfAttachment(bg, t, pool, id), defMs)
	assert.Equal(t, defMs, timeoutOfAttachment(bg, t, pool, id),
		"un ExecContext sin la opción debe correr con el tope del proceso")
}

func TestE_INF_013_RestoreDoesNotExtendAnOpenNarrowCursor(t *testing.T) {
	t.Parallel()
	pool, _ := newE013Pool(t)
	bg := context.Background()
	tx, err := pool.BeginTx(bg, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })

	start := time.Now()
	rows, err := tx.QueryContext(firebird.WithStatementTimeout(bg, 2*time.Second), streamingQuery)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	require.True(t, rows.Next())

	// Same attachment, cursor still open: the restore SET runs now.
	_, err = tx.ExecContext(bg, noopExec)
	require.NoError(t, err)

	_, ferr := fetchFor(rows, 20*time.Second)
	elapsed := time.Since(start)
	t.Logf("cursor de 2 s murió a los %s: %v", elapsed, ferr)
	require.Error(t, ferr, "el cursor abierto con 2 s debe morir aunque la sesión ya volvió a 600 s")
	assert.Less(t, elapsed, 10*time.Second)
	assert.GreaterOrEqual(t, elapsed, 1500*time.Millisecond)

	ae, ok := apperror.As(firebird.MapError(ferr))
	require.True(t, ok, "error sin mapear: %v", ferr)
	assert.Equal(t, "firebird_timeout", ae.Code, "el tope vencido a mitad del fetch se mapea igual: %v", ferr)
}

func TestE_INF_013_LaterNarrowSetDoesNotAffectAnOpenCursor(t *testing.T) {
	t.Parallel()
	pool, _ := newE013Pool(t)
	bg := context.Background()
	tx, err := pool.BeginTx(bg, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })

	rows, err := tx.QueryContext(bg, streamingQuery) // under the process default
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	require.True(t, rows.Next())

	var one int
	require.NoError(t, tx.QueryRowContext(firebird.WithStatementTimeout(bg, 2*time.Second),
		`SELECT 1 FROM RDB$DATABASE`).Scan(&one))

	// The fetch loop is the one that DID see the abort in the test above
	// (positive control); here it must run past the 2 s untouched.
	read, ferr := fetchFor(rows, 5*time.Second)
	t.Logf("cursor con el tope del proceso leyó %s tras el SET de 2 s; error=%v", read, ferr)
	require.NoError(t, ferr, "un SET posterior no debe acotar un cursor ya abierto")
	assert.GreaterOrEqual(t, read, 4*time.Second)
}

func TestE_INF_013_SetInsideWriteTransactionKeepsTheTransaction(t *testing.T) {
	t.Parallel()
	pool, _ := newE013Pool(t)
	bg := context.Background()

	probe := func(ctx context.Context, tx *sql.Tx) (int64, sql.NullString) {
		t.Helper()
		var id int64
		var v sql.NullString
		require.NoError(t, tx.QueryRowContext(ctx,
			`SELECT CURRENT_TRANSACTION, RDB$GET_CONTEXT('USER_TRANSACTION', 'e_inf_013') FROM RDB$DATABASE`).Scan(&id, &v))
		return id, v
	}

	tx, err := pool.BeginTx(bg, nil) // read-write
	require.NoError(t, err)
	var set int
	require.NoError(t, tx.QueryRowContext(bg,
		`SELECT RDB$SET_CONTEXT('USER_TRANSACTION', 'e_inf_013', 'marca') FROM RDB$DATABASE`).Scan(&set))
	before, v := probe(bg, tx)
	require.Equal(t, "marca", v.String)

	narrow, v := probe(firebird.WithStatementTimeout(bg, 2*time.Second), tx) // SET 2 inside the tx
	assert.Equal(t, before, narrow, "el SET no debe cambiar la transacción")
	assert.Equal(t, "marca", v.String)
	restored, v := probe(bg, tx) // SET 600 inside the tx
	assert.Equal(t, before, restored)
	assert.Equal(t, "marca", v.String, "ni hacer commit de lo que la transacción lleva")
	require.NoError(t, tx.Rollback())
	t.Logf("CURRENT_TRANSACTION antes/acotado/restaurado = %d/%d/%d", before, narrow, restored)

	// Positive control: the probe does notice a different transaction.
	tx2, err := pool.BeginTx(bg, nil)
	require.NoError(t, err)
	other, v := probe(bg, tx2)
	require.NoError(t, tx2.Rollback())
	assert.NotEqual(t, before, other, "control positivo: otra transacción sí se distingue")
	assert.False(t, v.Valid)
}
