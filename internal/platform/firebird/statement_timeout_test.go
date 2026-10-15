package firebird

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newWrappedWithDefault builds a wrapper conn whose attachment already carries
// the process-wide default, as cancelProofDriver.Open leaves it.
func newWrappedWithDefault(def time.Duration) (*cancelProofConn, *fakeConn, *fakeStmt) {
	conn, fc, st := newWrapped()
	conn.defaultTimeout = def
	conn.currentTimeout = def
	return conn, fc, st
}

func TestWithStatementTimeout_NonPositiveIsNoOp(t *testing.T) {
	t.Parallel()
	base := context.Background()
	for _, d := range []time.Duration{0, -time.Second} {
		ctx := WithStatementTimeout(base, d)
		_, ok := StatementTimeout(ctx)
		assert.False(t, ok, "un tope %s no debe sobrescribir el del proceso", d)
	}
	_, ok := StatementTimeout(WithStatementTimeout(base, 25*time.Second))
	assert.True(t, ok)
}

func TestStatementTimeout_OverrideAppliesAndRestoresOnNextStatement(t *testing.T) {
	t.Parallel()
	conn, fc, _ := newWrappedWithDefault(10 * time.Minute)
	acotado := WithStatementTimeout(context.Background(), 25*time.Second)

	_, err := conn.QueryContext(acotado, "SELECT 1 FROM RDB$DATABASE", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"SET STATEMENT TIMEOUT 25 SECOND"}, fc.execQueries,
		"la sentencia acotada debe correr con el tope del servidor ya puesto")
	require.Len(t, fc.queryCtxs, 1)

	// Same tope again: nothing to send.
	_, err = conn.QueryContext(acotado, "SELECT 2 FROM RDB$DATABASE", nil)
	require.NoError(t, err)
	require.Len(t, fc.execQueries, 1, "repetir el mismo tope no debe costar otro viaje")

	// The next borrower (phone sync, any other module) gets the default back
	// before its statement runs.
	_, err = conn.QueryContext(context.Background(), "SELECT 3 FROM RDB$DATABASE", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"SET STATEMENT TIMEOUT 25 SECOND",
		"SET STATEMENT TIMEOUT 600 SECOND",
	}, fc.execQueries)

	// Already restored: plain statements stay free of extra round-trips.
	_, err = conn.ExecContext(context.Background(), "UPDATE X SET Y = 1", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"SET STATEMENT TIMEOUT 25 SECOND",
		"SET STATEMENT TIMEOUT 600 SECOND",
		"UPDATE X SET Y = 1",
	}, fc.execQueries)
}

func TestStatementTimeout_PreparedStatementPathsAlsoApply(t *testing.T) {
	t.Parallel()
	conn, fc, st := newWrappedWithDefault(10 * time.Minute)
	acotado := WithStatementTimeout(context.Background(), 25*time.Second)

	stmt, err := conn.PrepareContext(acotado, "SELECT 1 FROM RDB$DATABASE")
	require.NoError(t, err)
	assert.Empty(t, fc.execQueries, "preparar no ejecuta: el tope se pone al ejecutar")

	queryer, ok := stmt.(driver.StmtQueryContext)
	require.True(t, ok)
	_, err = queryer.QueryContext(acotado, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"SET STATEMENT TIMEOUT 25 SECOND"}, fc.execQueries)
	require.Len(t, st.queryCtxs, 1)

	execer, ok := stmt.(driver.StmtExecContext)
	require.True(t, ok)
	_, err = execer.ExecContext(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"SET STATEMENT TIMEOUT 25 SECOND",
		"SET STATEMENT TIMEOUT 600 SECOND",
	}, fc.execQueries)
}

func TestStatementTimeout_FailedRestoreDiscardsTheConnection(t *testing.T) {
	t.Parallel()
	conn, fc, _ := newWrappedWithDefault(10 * time.Minute)
	_, err := conn.QueryContext(WithStatementTimeout(context.Background(), 25*time.Second), "SELECT 1", nil)
	require.NoError(t, err)

	fc.execErr = errFake
	_, err = conn.QueryContext(context.Background(), "SELECT 2", nil)
	require.ErrorIs(t, err, driver.ErrBadConn,
		"una conexión que no pudo volver al tope del proceso no debe servir a nadie más")
	assert.Len(t, fc.queryCtxs, 1, "la sentencia del siguiente usuario no debe correr con 25 s")
	assert.False(t, conn.IsValid(), "database/sql debe tirar la conexión al devolverla")
	// otelsql forwards ResetSession but not IsValid: this is the path that
	// reaches database/sql in production.
	require.ErrorIs(t, conn.ResetSession(context.Background()), driver.ErrBadConn)

	// Even if the server recovers, the conn stays condemned.
	fc.execErr = nil
	_, err = conn.ExecContext(context.Background(), "SELECT 3", nil)
	require.ErrorIs(t, err, driver.ErrBadConn)
}

func TestStatementTimeout_FailedOverrideStillRunsTheStatement(t *testing.T) {
	t.Parallel()
	conn, fc, _ := newWrappedWithDefault(10 * time.Minute)
	fc.execErr = errFake

	_, err := conn.QueryContext(WithStatementTimeout(context.Background(), 25*time.Second), "SELECT 1", nil)
	require.NoError(t, err, "el tope acotado es defensa: si el SET falla la lectura sigue con el del proceso")
	assert.Len(t, fc.queryCtxs, 1)
	assert.True(t, conn.IsValid())
	require.NoError(t, conn.ResetSession(context.Background()))
	assert.Equal(t, 10*time.Minute, conn.currentTimeout, "el estado no debe mentir sobre lo que tiene el servidor")
}

func TestStatementTimeout_DisabledDefaultRestoresToZero(t *testing.T) {
	t.Parallel()
	// FB_STATEMENT_TIMEOUT=0: the attachment never got a SET, so "restore"
	// means back to 0 (no session-level timeout).
	conn, fc, _ := newWrappedWithDefault(0)
	_, err := conn.QueryContext(WithStatementTimeout(context.Background(), 25*time.Second), "SELECT 1", nil)
	require.NoError(t, err)
	_, err = conn.QueryContext(context.Background(), "SELECT 2", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"SET STATEMENT TIMEOUT 25 SECOND",
		"SET STATEMENT TIMEOUT 0 SECOND",
	}, fc.execQueries)
}

func TestCancelProofDriver_OpenLeavesConnAtProcessDefault(t *testing.T) {
	t.Parallel()
	fc := &fakeConn{stmt: &fakeStmt{}}
	d := &cancelProofDriver{base: &fakeDriver{conn: fc}, statementTimeout: 10 * time.Minute}

	c, err := d.Open("dsn")
	require.NoError(t, err)
	conn, ok := c.(*cancelProofConn)
	require.True(t, ok)
	assert.Equal(t, 10*time.Minute, conn.defaultTimeout)
	assert.Equal(t, 10*time.Minute, conn.currentTimeout)

	_, err = conn.QueryContext(context.Background(), "SELECT 1", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"SET STATEMENT TIMEOUT 600 SECOND"}, fc.execQueries,
		"una conexión recién abierta ya trae el tope del proceso: no se repite el SET")
}
