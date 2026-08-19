package firebird

// White-box package so the wrapper can be built directly over a fake
// driver.Driver without going through sql.Register (which is process-global
// and one-shot).

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Fakes ────────────────────────────────────────────────────────────

// errFake is the sentinel a fake returns when asked to fail.
var errFake = errors.New("fake driver failure")

// fakeDriver records the DSN it was opened with and hands back fakeConn.
type fakeDriver struct {
	conn    *fakeConn
	openErr error
}

func (d *fakeDriver) Open(_ string) (driver.Conn, error) {
	if d.openErr != nil {
		return nil, d.openErr
	}
	return d.conn, nil
}

// fakeConn records every context it receives so tests can assert that
// cancellation and deadlines were stripped before reaching the driver.
type fakeConn struct {
	execQueries []string
	execCtxs    []context.Context //nolint:containedctx // recorded for assertions.
	queryCtxs   []context.Context //nolint:containedctx // recorded for assertions.
	prepareCtxs []context.Context //nolint:containedctx // recorded for assertions.
	beginCtxs   []context.Context //nolint:containedctx // recorded for assertions.
	pingCtxs    []context.Context //nolint:containedctx // recorded for assertions.
	execErr     error
	stmt        *fakeStmt
}

func (c *fakeConn) Prepare(_ string) (driver.Stmt, error) { return c.stmt, nil }
func (c *fakeConn) Close() error                          { return nil }
func (c *fakeConn) Begin() (driver.Tx, error)             { return fakeTx{}, nil }

func (c *fakeConn) PrepareContext(ctx context.Context, _ string) (driver.Stmt, error) {
	c.prepareCtxs = append(c.prepareCtxs, ctx)
	return c.stmt, nil
}

func (c *fakeConn) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	c.beginCtxs = append(c.beginCtxs, ctx)
	return fakeTx{}, nil
}

func (c *fakeConn) ExecContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.execCtxs = append(c.execCtxs, ctx)
	c.execQueries = append(c.execQueries, query)
	if c.execErr != nil {
		return nil, c.execErr
	}
	return driver.RowsAffected(0), nil
}

func (c *fakeConn) QueryContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	c.queryCtxs = append(c.queryCtxs, ctx)
	return &fakeRows{}, nil
}

func (c *fakeConn) Ping(ctx context.Context) error {
	c.pingCtxs = append(c.pingCtxs, ctx)
	return nil
}

// fakeStmt records the contexts the statement-level paths receive.
type fakeStmt struct {
	execCtxs  []context.Context //nolint:containedctx // recorded for assertions.
	queryCtxs []context.Context //nolint:containedctx // recorded for assertions.
}

func (s *fakeStmt) Close() error  { return nil }
func (s *fakeStmt) NumInput() int { return -1 }

func (s *fakeStmt) Exec(_ []driver.Value) (driver.Result, error) {
	return driver.RowsAffected(0), nil
}

func (s *fakeStmt) Query(_ []driver.Value) (driver.Rows, error) { return &fakeRows{}, nil }

func (s *fakeStmt) ExecContext(ctx context.Context, _ []driver.NamedValue) (driver.Result, error) {
	s.execCtxs = append(s.execCtxs, ctx)
	return driver.RowsAffected(0), nil
}

func (s *fakeStmt) QueryContext(ctx context.Context, _ []driver.NamedValue) (driver.Rows, error) {
	s.queryCtxs = append(s.queryCtxs, ctx)
	return &fakeRows{}, nil
}

type fakeTx struct{}

func (fakeTx) Commit() error   { return nil }
func (fakeTx) Rollback() error { return nil }

type fakeRows struct{}

func (*fakeRows) Columns() []string           { return nil }
func (*fakeRows) Close() error                { return nil }
func (*fakeRows) Next(_ []driver.Value) error { return io.EOF }

// bareConn implements only driver.Conn: no context-aware optional interfaces.
// It exercises the wrapper's fallback branches.
type bareConn struct{ stmt driver.Stmt }

func (c *bareConn) Prepare(_ string) (driver.Stmt, error) { return c.stmt, nil }
func (c *bareConn) Close() error                          { return nil }
func (c *bareConn) Begin() (driver.Tx, error)             { return fakeTx{}, nil }

// bareStmt implements only driver.Stmt.
type bareStmt struct{ values []driver.Value }

func (s *bareStmt) Close() error  { return nil }
func (s *bareStmt) NumInput() int { return -1 }

func (s *bareStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.values = args
	return driver.RowsAffected(0), nil
}

func (s *bareStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.values = args
	return &fakeRows{}, nil
}

// ── Helpers ──────────────────────────────────────────────────────────

// deadCtx returns a context that is both cancelled and past its deadline —
// the exact shape that used to corrupt the Firebird wire protocol.
func deadCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	t.Cleanup(cancel)
	<-ctx.Done()
	return ctx
}

// assertDetached fails when ctx still carries a deadline or cancellation.
func assertDetached(ctx context.Context, t *testing.T, label string) {
	t.Helper()
	require.NotNil(t, ctx, "%s: nil context", label)
	_, hasDeadline := ctx.Deadline()
	assert.False(t, hasDeadline, "%s: context must not carry a deadline", label)
	assert.Nil(t, ctx.Done(), "%s: context must not be cancellable", label)
	assert.NoError(t, ctx.Err(), "%s: context must not be cancelled", label)
}

// newWrapped builds a wrapper conn over a fresh fakeConn.
func newWrapped() (*cancelProofConn, *fakeConn, *fakeStmt) {
	st := &fakeStmt{}
	fc := &fakeConn{stmt: st}
	return &cancelProofConn{base: fc}, fc, st
}

// ── Tests: cancellation stripping ────────────────────────────────────

func TestCancelProofConn_StripsCancellationFromEveryPath(t *testing.T) {
	t.Parallel()
	conn, fc, _ := newWrapped()
	ctx := deadCtx(t)
	require.Error(t, ctx.Err(), "precondition: the context must already be cancelled")

	_, err := conn.ExecContext(ctx, "SELECT 1", nil)
	require.NoError(t, err)
	_, err = conn.QueryContext(ctx, "SELECT 1", nil)
	require.NoError(t, err)
	_, err = conn.PrepareContext(ctx, "SELECT 1")
	require.NoError(t, err)
	_, err = conn.BeginTx(ctx, driver.TxOptions{})
	require.NoError(t, err)
	require.NoError(t, conn.Ping(ctx))

	require.Len(t, fc.execCtxs, 1)
	assertDetached(fc.execCtxs[0], t, "ExecContext")
	require.Len(t, fc.queryCtxs, 1)
	assertDetached(fc.queryCtxs[0], t, "QueryContext")
	require.Len(t, fc.prepareCtxs, 1)
	assertDetached(fc.prepareCtxs[0], t, "PrepareContext")
	require.Len(t, fc.beginCtxs, 1)
	assertDetached(fc.beginCtxs[0], t, "BeginTx")
	require.Len(t, fc.pingCtxs, 1)
	assertDetached(fc.pingCtxs[0], t, "Ping")
}

func TestCancelProofStmt_StripsCancellation(t *testing.T) {
	t.Parallel()
	conn, _, st := newWrapped()
	ctx := deadCtx(t)

	stmt, err := conn.PrepareContext(ctx, "SELECT 1")
	require.NoError(t, err)

	execer, ok := stmt.(driver.StmtExecContext)
	require.True(t, ok, "wrapped stmt must expose StmtExecContext")
	_, err = execer.ExecContext(ctx, nil)
	require.NoError(t, err)

	queryer, ok := stmt.(driver.StmtQueryContext)
	require.True(t, ok, "wrapped stmt must expose StmtQueryContext")
	_, err = queryer.QueryContext(ctx, nil)
	require.NoError(t, err)

	require.Len(t, st.execCtxs, 1)
	assertDetached(st.execCtxs[0], t, "stmt.ExecContext")
	require.Len(t, st.queryCtxs, 1)
	assertDetached(st.queryCtxs[0], t, "stmt.QueryContext")
}

func TestCancelProofConn_PreservesContextValues(t *testing.T) {
	t.Parallel()
	conn, fc, _ := newWrapped()
	type key struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "trace-1"))
	cancel()

	_, err := conn.QueryContext(ctx, "SELECT 1", nil)
	require.NoError(t, err)
	require.Len(t, fc.queryCtxs, 1)
	assert.Equal(t, "trace-1", fc.queryCtxs[0].Value(key{}),
		"values (trace/log correlation) must survive WithoutCancel")
}

func TestCancelProofDriver_OpenWrapsConn(t *testing.T) {
	t.Parallel()
	fc := &fakeConn{stmt: &fakeStmt{}}
	d := &cancelProofDriver{base: &fakeDriver{conn: fc}}

	conn, err := d.Open("dsn")
	require.NoError(t, err)
	assert.IsType(t, &cancelProofConn{}, conn)
	assert.Empty(t, fc.execQueries, "no statement timeout configured: nothing must be sent")
	require.NoError(t, conn.Close())
}

func TestCancelProofDriver_OpenPropagatesError(t *testing.T) {
	t.Parallel()
	d := &cancelProofDriver{base: &fakeDriver{openErr: errFake}}
	_, err := d.Open("dsn")
	require.ErrorIs(t, err, errFake)
}

// ── Tests: statement timeout ─────────────────────────────────────────

func TestCancelProofDriver_SetsStatementTimeoutOnOpen(t *testing.T) {
	t.Parallel()
	fc := &fakeConn{stmt: &fakeStmt{}}
	d := &cancelProofDriver{base: &fakeDriver{conn: fc}, statementTimeout: 10 * time.Minute}

	conn, err := d.Open("dsn")
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	require.Equal(t, []string{"SET STATEMENT TIMEOUT 600 SECOND"}, fc.execQueries)
	require.Len(t, fc.execCtxs, 1)
	assertDetached(fc.execCtxs[0], t, "SET STATEMENT TIMEOUT")
}

func TestCancelProofDriver_StatementTimeoutRoundsUpToOneSecond(t *testing.T) {
	t.Parallel()
	fc := &fakeConn{stmt: &fakeStmt{}}
	d := &cancelProofDriver{base: &fakeDriver{conn: fc}, statementTimeout: 250 * time.Millisecond}

	conn, err := d.Open("dsn")
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	assert.Equal(t, []string{"SET STATEMENT TIMEOUT 1 SECOND"}, fc.execQueries,
		"sub-second configs must not collapse into 0 (= no timeout)")
}

func TestCancelProofDriver_StatementTimeoutFailureDoesNotAbortOpen(t *testing.T) {
	t.Parallel()
	fc := &fakeConn{stmt: &fakeStmt{}, execErr: errFake}
	d := &cancelProofDriver{base: &fakeDriver{conn: fc}, statementTimeout: time.Minute}

	conn, err := d.Open("dsn")
	require.NoError(t, err, "a rejected SET must not take the connection down")
	require.NotNil(t, conn)
	require.NoError(t, conn.Close())
	assert.Len(t, fc.execQueries, 1)
}

// ── Tests: fallbacks for drivers without the Context interfaces ──────

func TestCancelProofConn_FallsBackWhenBaseLacksContextInterfaces(t *testing.T) {
	t.Parallel()
	bs := &bareStmt{}
	conn := &cancelProofConn{base: &bareConn{stmt: bs}}

	ctx := deadCtx(t)

	_, err := conn.ExecContext(ctx, "SELECT 1", nil)
	require.ErrorIs(t, err, driver.ErrSkip, "database/sql must fall back to the prepare path")
	_, err = conn.QueryContext(ctx, "SELECT 1", nil)
	require.ErrorIs(t, err, driver.ErrSkip)

	stmt, err := conn.PrepareContext(ctx, "SELECT 1")
	require.NoError(t, err)
	_, err = conn.BeginTx(ctx, driver.TxOptions{})
	require.NoError(t, err)
	require.NoError(t, conn.Ping(ctx), "a base without Pinger must not fail the ping")

	execer, ok := stmt.(driver.StmtExecContext)
	require.True(t, ok)
	_, err = execer.ExecContext(ctx, []driver.NamedValue{{Ordinal: 1, Value: int64(7)}})
	require.NoError(t, err)
	assert.Equal(t, []driver.Value{int64(7)}, bs.values)
}

func TestCancelProofStmt_RejectsNamedArgsOnLegacyFallback(t *testing.T) {
	t.Parallel()
	conn := &cancelProofConn{base: &bareConn{stmt: &bareStmt{}}}
	stmt, err := conn.PrepareContext(context.Background(), "SELECT 1")
	require.NoError(t, err)

	execer, ok := stmt.(driver.StmtExecContext)
	require.True(t, ok)
	_, err = execer.ExecContext(context.Background(), []driver.NamedValue{{Name: "p", Value: 1}})
	require.ErrorIs(t, err, errNamedArgsUnsupported)

	queryer, ok := stmt.(driver.StmtQueryContext)
	require.True(t, ok)
	_, err = queryer.QueryContext(context.Background(), []driver.NamedValue{{Name: "p", Value: 1}})
	require.ErrorIs(t, err, errNamedArgsUnsupported)
}

// ── Tests: registration ──────────────────────────────────────────────

func TestRegisterCancelProofDriver_IsIdempotent(t *testing.T) {
	t.Parallel()
	name, err := registerCancelProofDriver(10 * time.Minute)
	require.NoError(t, err)
	assert.Equal(t, cancelProofDriverName, name)

	// A second call must not panic on a duplicate sql.Register.
	again, err := registerCancelProofDriver(time.Minute)
	require.NoError(t, err)
	assert.Equal(t, name, again)
}

func TestBaseFirebirdDriver_IsReachable(t *testing.T) {
	t.Parallel()
	d, err := baseFirebirdDriver()
	require.NoError(t, err)
	require.NotNil(t, d, "the firebirdsql package must have registered its driver in init()")
}
