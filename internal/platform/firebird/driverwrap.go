package firebird

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"time"
)

// baseDriverName is the sql driver name registered by the firebirdsql package
// in its init(). We never hand it to a Pool directly — everything goes through
// the cancellation-proof wrapper registered under cancelProofDriverName.
const baseDriverName = "firebirdsql"

// cancelProofDriverName is the sql driver name our wrapper registers under.
const cancelProofDriverName = "firebirdsql-nocancel"

// errNoBaseDriver is returned when the firebirdsql driver cannot be reached.
var errNoBaseDriver = errors.New("firebird: base driver unavailable")

// Registration state for the wrapper driver. sql.Register panics on a repeated
// name, so registration happens exactly once per process and the resulting
// name is reused by every Pool.
var (
	cancelProofOnce      sync.Once
	errCancelProofDriver error
	// registeredOptions are the options the one registration captured.
	registeredOptions driverOptions
)

// registerCancelProofDriver registers (once) a driver.Driver that shields the
// firebirdsql driver from context cancellation and applies a server-side
// statement timeout to every new connection. It returns the name to hand to
// sql.Open / otelsql.Register.
//
// opts is captured on the first call: the process has a single Firebird
// configuration, so later calls with different values are ignored.
func registerCancelProofDriver(opts driverOptions) (string, error) {
	cancelProofOnce.Do(func() {
		base, err := baseFirebirdDriver()
		if err != nil {
			errCancelProofDriver = err
			return
		}
		registeredOptions = opts
		sql.Register(cancelProofDriverName, &cancelProofDriver{
			base:               base,
			statementTimeout:   opts.statementTimeout,
			slowQueryThreshold: opts.slowQueryThreshold,
		})
	})
	if errCancelProofDriver != nil {
		return "", errCancelProofDriver
	}
	return cancelProofDriverName, nil
}

// baseFirebirdDriver returns the driver.Driver instance the firebirdsql
// package registered in its init(). The package exports no accessor for it,
// so we go through a throwaway *sql.DB — sql.Open is lazy and never dials.
func baseFirebirdDriver() (driver.Driver, error) {
	db, err := sql.Open(baseDriverName, "")
	if err != nil {
		return nil, fmt.Errorf("firebird: open base driver: %w", err)
	}
	d := db.Driver()
	if closeErr := db.Close(); closeErr != nil {
		return nil, fmt.Errorf("firebird: close base driver probe: %w", closeErr)
	}
	if d == nil {
		return nil, errNoBaseDriver
	}
	return d, nil
}

// driverOptions are the process-wide knobs of the wrapper driver.
type driverOptions struct {
	// statementTimeout is the default server-side ceiling (FB_STATEMENT_TIMEOUT).
	statementTimeout time.Duration
	// slowQueryThreshold logs statements slower than this (0 disables).
	slowQueryThreshold time.Duration
}

// cancelProofDriver wraps the firebirdsql driver with three responsibilities.
//
// First, it strips cancellation from every context before it reaches the
// driver. firebirdsql v0.9.19 reacts to a cancelled context by writing an
// op_cancel packet to the socket and returning without reading the reply
// (rows.go). The wire protocol is then out of sync and the next round-trip
// (Rows.Close → closeCursor) blocks forever, because the package never sets a
// read deadline. That leaks the connection out of the pool for good. With the
// cancellation removed the driver never takes that path.
//
// Second, it issues SET STATEMENT TIMEOUT once per connection so the server —
// not the client — enforces the ceiling on runaway statements. The server
// reports the expiry through the normal protocol path, which leaves the
// connection healthy and reusable. A caller can narrow that ceiling for its
// own statements with WithStatementTimeout; the conn restores the default
// before the next statement that does not ask for it.
//
// Third, it times every statement and logs the ones above slowQueryThreshold
// (see slowQueryLog).
type cancelProofDriver struct {
	base               driver.Driver
	statementTimeout   time.Duration
	slowQueryThreshold time.Duration
	logger             *slog.Logger // nil → slog.Default().
	warnOnce           sync.Once
}

// Open opens a connection through the base driver and applies the statement
// timeout to it.
func (d *cancelProofDriver) Open(dsn string) (driver.Conn, error) {
	conn, err := d.base.Open(dsn)
	if err != nil {
		return nil, err //nolint:wrapcheck // driver errors must reach database/sql verbatim (driver.ErrBadConn).
	}
	d.applyStatementTimeout(conn)
	return &cancelProofConn{
		base:           conn,
		defaultTimeout: d.statementTimeout,
		// Even when the SET above failed we record the default: the conn has
		// no override to undo, and retrying a SET the server rejects on every
		// statement would only add round-trips.
		currentTimeout: d.statementTimeout,
		warn:           d.warnStatementTimeout,
		slow:           slowQueryLog{threshold: d.slowQueryThreshold, logger: d.logger},
	}, nil
}

// applyStatementTimeout runs SET STATEMENT TIMEOUT on a freshly opened
// connection. A failure is logged once and otherwise ignored: the timeout is
// defence in depth, while the cancellation stripping above is what actually
// keeps the pool alive.
func (d *cancelProofDriver) applyStatementTimeout(conn driver.Conn) {
	if d.statementTimeout <= 0 {
		return
	}
	execer, ok := conn.(driver.ExecerContext)
	if !ok {
		d.warnStatementTimeout(errNoBaseDriver)
		return
	}
	if _, err := execer.ExecContext(context.Background(), statementTimeoutSQL(d.statementTimeout), nil); err != nil {
		d.warnStatementTimeout(err)
	}
}

// timeoutSeconds converts d to the whole seconds Firebird is told. It rounds
// up so sub-second values do not collapse into 0 (= no timeout); 0 and
// negatives map to 0.
func timeoutSeconds(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64((d + time.Second - 1) / time.Second)
}

// statementTimeoutSQL renders the session statement for d.
func statementTimeoutSQL(d time.Duration) string {
	return "SET STATEMENT TIMEOUT " + strconv.FormatInt(timeoutSeconds(d), 10) + " SECOND"
}

// statementTimeoutKey is the context key of WithStatementTimeout.
type statementTimeoutKey struct{}

// WithStatementTimeout returns a context whose Firebird statements run under a
// server-side ceiling of d instead of the process default (FB_STATEMENT_TIMEOUT).
//
// The driver ignores context cancellation by design (see cancelProofDriver),
// so a request deadline alone never stops a statement already in Firebird.
// This is the mechanism that does: before each statement issued with this
// context the connection runs SET STATEMENT TIMEOUT d. The default comes
// back when database/sql hands the conn to its next borrower (ResetSession),
// and again before any statement issued without the option (any other module,
// the phone sync). A connection that cannot restore its default is discarded
// (E-INF-013). Firebird
// counts the ceiling from execution start to the last fetch, and aborts the
// statement itself when it expires (gds 335545128, mapped to
// firebird_timeout), leaving the connection healthy.
//
// Firebird works in whole seconds: d is rounded up. d <= 0 returns ctx
// unchanged.
func WithStatementTimeout(ctx context.Context, d time.Duration) context.Context {
	if d <= 0 {
		return ctx
	}
	return context.WithValue(ctx, statementTimeoutKey{}, d)
}

// StatementTimeout returns the override WithStatementTimeout put in ctx, if any.
func StatementTimeout(ctx context.Context) (time.Duration, bool) {
	d, ok := ctx.Value(statementTimeoutKey{}).(time.Duration)
	return d, ok
}

// warnStatementTimeout emits a single WARN per process for a failed SET.
func (d *cancelProofDriver) warnStatementTimeout(err error) {
	d.warnOnce.Do(func() {
		slog.Warn(
			"firebird: no se pudo aplicar el statement timeout del servidor",
			"timeout", d.statementTimeout.String(),
			"error", err,
		)
	})
}

// cancelProofConn wraps a driver.Conn, detaching cancellation from every
// context it forwards, keeping the attachment's statement timeout in line with
// what each statement asked for, and timing statements for the slow log.
//
// database/sql never uses one driver.Conn from two goroutines at once, so the
// mutable fields need no lock.
type cancelProofConn struct {
	base driver.Conn
	// defaultTimeout is the process ceiling every statement without an
	// override must run under.
	defaultTimeout time.Duration
	// currentTimeout is what the attachment was last told.
	currentTimeout time.Duration
	// unknown marks a session whose timeout is uncertain after a failed SET:
	// the next statement or ResetSession must send its SET regardless.
	unknown bool
	// broken marks a conn that could not restore its default; it refuses
	// every statement and reports itself invalid so the pool drops it.
	broken bool
	warn   func(error) // nil → no warning (tests).
	slow   slowQueryLog
}

// syncStatementTimeout makes the attachment's statement timeout match what ctx
// asks for (its override, or the default) before a statement runs.
//
// Invariant, E-INF-013 (docs/evidencia/infra.md): no statement issued without
// the option runs under a narrow ceiling. This per-statement restore is the
// second line of defence; ResetSession restores at pool checkout first. When
// the state is unknown (a SET failed) the next statement sends its SET no
// matter what, and if that fails too the conn is condemned.
func (c *cancelProofConn) syncStatementTimeout(ctx context.Context) error {
	if c.broken {
		return driver.ErrBadConn
	}
	want, override := StatementTimeout(ctx)
	if !override {
		want = c.defaultTimeout
	}
	if !c.unknown && timeoutSeconds(want) == timeoutSeconds(c.currentTimeout) {
		return nil
	}
	err := c.setStatementTimeout(ctx, want)
	if err == nil {
		return nil
	}
	if override && !c.unknown {
		// The narrow ceiling is defence in depth: the statement still runs.
		// The server may or may not have applied the SET, so the state is
		// unknown and the next statement re-sends its own value.
		c.unknown = true
		if c.warn != nil {
			c.warn(err)
		}
		return nil
	}
	// Either the default could not be restored, or the session state was
	// already unknown and still cannot be fixed: this attachment must not
	// serve anyone again.
	c.broken = true
	return driver.ErrBadConn
}

// setStatementTimeout sends SET STATEMENT TIMEOUT d and records the result.
func (c *cancelProofConn) setStatementTimeout(ctx context.Context, d time.Duration) error {
	execer, ok := c.base.(driver.ExecerContext)
	if !ok {
		return nil // nothing to talk to; a test-only shape.
	}
	if _, err := execer.ExecContext(context.WithoutCancel(ctx), statementTimeoutSQL(d), nil); err != nil {
		return err //nolint:wrapcheck // only inspected for nil by the callers.
	}
	c.currentTimeout = d
	c.unknown = false
	return nil
}

// IsValid implements driver.Validator: database/sql drops a conn that could
// not restore its default statement timeout instead of pooling it.
func (c *cancelProofConn) IsValid() bool { return !c.broken }

// ResetSession implements driver.SessionResetter. database/sql calls it when
// it hands a pooled conn to the next borrower (otelsql forwards it; it does
// not forward IsValid). It restores the process default there, so a conn
// never serves another module with a narrow ceiling and a failed restore is
// an ErrBadConn database/sql retries on another conn — never a failed
// statement inside somebody else's transaction (E-INF-013).
func (c *cancelProofConn) ResetSession(ctx context.Context) error {
	if c.broken {
		return driver.ErrBadConn
	}
	if !c.unknown && timeoutSeconds(c.currentTimeout) == timeoutSeconds(c.defaultTimeout) {
		return nil
	}
	if err := c.setStatementTimeout(ctx, c.defaultTimeout); err != nil {
		c.broken = true
		return driver.ErrBadConn
	}
	return nil
}

// Compile-time proof that the wrapper exposes the same optional interfaces as
// the firebirdsql connection it wraps. Dropping one would silently downgrade
// database/sql to a slower (or uncancellable) code path.
var (
	_ driver.Conn               = (*cancelProofConn)(nil)
	_ driver.ConnPrepareContext = (*cancelProofConn)(nil)
	_ driver.ConnBeginTx        = (*cancelProofConn)(nil)
	_ driver.ExecerContext      = (*cancelProofConn)(nil)
	_ driver.QueryerContext     = (*cancelProofConn)(nil)
	_ driver.Pinger             = (*cancelProofConn)(nil)
	_ driver.Validator          = (*cancelProofConn)(nil)
	_ driver.SessionResetter    = (*cancelProofConn)(nil)
	_ driver.Stmt               = (*cancelProofStmt)(nil)
	_ driver.StmtExecContext    = (*cancelProofStmt)(nil)
	_ driver.StmtQueryContext   = (*cancelProofStmt)(nil)
)

// Prepare implements driver.Conn.
func (c *cancelProofConn) Prepare(query string) (driver.Stmt, error) {
	st, err := c.base.Prepare(query)
	if err != nil {
		return nil, err //nolint:wrapcheck // verbatim driver error.
	}
	return &cancelProofStmt{base: st, conn: c, query: query}, nil
}

// Close implements driver.Conn.
func (c *cancelProofConn) Close() error {
	return c.base.Close() //nolint:wrapcheck // verbatim driver error.
}

// Begin implements driver.Conn (legacy path; database/sql prefers BeginTx).
func (c *cancelProofConn) Begin() (driver.Tx, error) {
	return c.base.Begin() //nolint:staticcheck,wrapcheck // required by driver.Conn.
}

// PrepareContext implements driver.ConnPrepareContext.
func (c *cancelProofConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	preparer, ok := c.base.(driver.ConnPrepareContext)
	if !ok {
		return c.Prepare(query)
	}
	st, err := preparer.PrepareContext(context.WithoutCancel(ctx), query)
	if err != nil {
		return nil, err //nolint:wrapcheck // verbatim driver error.
	}
	return &cancelProofStmt{base: st, conn: c, query: query}, nil
}

// BeginTx implements driver.ConnBeginTx.
func (c *cancelProofConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	beginner, ok := c.base.(driver.ConnBeginTx)
	if !ok {
		return c.Begin()
	}
	return beginner.BeginTx(context.WithoutCancel(ctx), opts) //nolint:wrapcheck // verbatim driver error.
}

// ExecContext implements driver.ExecerContext. Returns driver.ErrSkip when the
// wrapped connection has no fast path, so database/sql falls back to prepare.
func (c *cancelProofConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	execer, ok := c.base.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	if err := c.syncStatementTimeout(ctx); err != nil {
		return nil, err
	}
	ctx = context.WithoutCancel(ctx)
	if !c.slow.enabled() {
		return execer.ExecContext(ctx, query, args) //nolint:wrapcheck // verbatim driver error.
	}
	start := c.slow.clock()
	res, err := execer.ExecContext(ctx, query, args)
	c.slow.observe(ctx, "exec", query, start, -1, err != nil)
	return res, err //nolint:wrapcheck // verbatim driver error.
}

// QueryContext implements driver.QueryerContext. Returns driver.ErrSkip when
// the wrapped connection has no fast path.
func (c *cancelProofConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := c.base.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	if err := c.syncStatementTimeout(ctx); err != nil {
		return nil, err
	}
	ctx = context.WithoutCancel(ctx)
	if !c.slow.enabled() {
		return queryer.QueryContext(ctx, query, args) //nolint:wrapcheck // verbatim driver error.
	}
	start := c.slow.clock()
	rows, err := queryer.QueryContext(ctx, query, args)
	if err != nil {
		c.slow.observe(ctx, "query", query, start, 0, true)
		return nil, err //nolint:wrapcheck // verbatim driver error.
	}
	return &timedRows{base: rows, log: &c.slow, ctx: ctx, query: query, start: start}, nil
}

// Ping implements driver.Pinger.
func (c *cancelProofConn) Ping(ctx context.Context) error {
	pinger, ok := c.base.(driver.Pinger)
	if !ok {
		return nil
	}
	return pinger.Ping(context.WithoutCancel(ctx)) //nolint:wrapcheck // verbatim driver error.
}

// cancelProofStmt wraps a driver.Stmt so the statement-level context paths get
// the same treatment as the connection-level ones.
type cancelProofStmt struct {
	base  driver.Stmt
	conn  *cancelProofConn
	query string
}

// Close implements driver.Stmt.
func (s *cancelProofStmt) Close() error {
	return s.base.Close() //nolint:wrapcheck // verbatim driver error.
}

// NumInput implements driver.Stmt.
func (s *cancelProofStmt) NumInput() int { return s.base.NumInput() }

// Exec implements driver.Stmt (legacy path).
func (s *cancelProofStmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.execLegacy(context.Background(), args)
}

// Query implements driver.Stmt (legacy path).
func (s *cancelProofStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.queryLegacy(context.Background(), args)
}

// execLegacy runs the context-less driver path, still honouring the
// statement timeout ctx asks for.
func (s *cancelProofStmt) execLegacy(ctx context.Context, args []driver.Value) (driver.Result, error) {
	if err := s.conn.syncStatementTimeout(ctx); err != nil {
		return nil, err
	}
	return s.base.Exec(args) //nolint:staticcheck,wrapcheck // required by driver.Stmt.
}

// queryLegacy is execLegacy's query counterpart.
func (s *cancelProofStmt) queryLegacy(ctx context.Context, args []driver.Value) (driver.Rows, error) {
	if err := s.conn.syncStatementTimeout(ctx); err != nil {
		return nil, err
	}
	return s.base.Query(args) //nolint:staticcheck,wrapcheck // required by driver.Stmt.
}

// ExecContext implements driver.StmtExecContext.
func (s *cancelProofStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	execer, ok := s.base.(driver.StmtExecContext)
	if !ok {
		values, err := namedToValues(args)
		if err != nil {
			return nil, err
		}
		return s.execLegacy(ctx, values)
	}
	if err := s.conn.syncStatementTimeout(ctx); err != nil {
		return nil, err
	}
	ctx = context.WithoutCancel(ctx)
	if !s.conn.slow.enabled() {
		return execer.ExecContext(ctx, args) //nolint:wrapcheck // verbatim driver error.
	}
	start := s.conn.slow.clock()
	res, err := execer.ExecContext(ctx, args)
	s.conn.slow.observe(ctx, "exec", s.query, start, -1, err != nil)
	return res, err //nolint:wrapcheck // verbatim driver error.
}

// QueryContext implements driver.StmtQueryContext.
func (s *cancelProofStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := s.base.(driver.StmtQueryContext)
	if !ok {
		values, err := namedToValues(args)
		if err != nil {
			return nil, err
		}
		return s.queryLegacy(ctx, values)
	}
	if err := s.conn.syncStatementTimeout(ctx); err != nil {
		return nil, err
	}
	ctx = context.WithoutCancel(ctx)
	if !s.conn.slow.enabled() {
		return queryer.QueryContext(ctx, args) //nolint:wrapcheck // verbatim driver error.
	}
	start := s.conn.slow.clock()
	rows, err := queryer.QueryContext(ctx, args)
	if err != nil {
		s.conn.slow.observe(ctx, "query", s.query, start, 0, true)
		return nil, err //nolint:wrapcheck // verbatim driver error.
	}
	return &timedRows{base: rows, log: &s.conn.slow, ctx: ctx, query: s.query, start: start}, nil
}

// errNamedArgsUnsupported mirrors database/sql's own message for drivers that
// only speak positional parameters.
var errNamedArgsUnsupported = errors.New("firebird: driver does not support named parameters")

// namedToValues flattens named values into positional ones for the legacy
// fallback paths. Only reachable with a driver that lacks the Context
// variants; firebirdsql implements both, so this is defensive.
func namedToValues(args []driver.NamedValue) ([]driver.Value, error) {
	values := make([]driver.Value, len(args))
	for i, a := range args {
		if a.Name != "" {
			return nil, errNamedArgsUnsupported
		}
		values[i] = a.Value
	}
	return values, nil
}

// isEOF reports the driver's end-of-rows signal.
func isEOF(err error) bool { return errors.Is(err, io.EOF) }
