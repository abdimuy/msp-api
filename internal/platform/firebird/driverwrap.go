package firebird

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
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
)

// registerCancelProofDriver registers (once) a driver.Driver that shields the
// firebirdsql driver from context cancellation and applies a server-side
// statement timeout to every new connection. It returns the name to hand to
// sql.Open / otelsql.Register.
//
// statementTimeout is captured on the first call: the process has a single
// Firebird configuration, so later calls with a different value are ignored.
func registerCancelProofDriver(statementTimeout time.Duration) (string, error) {
	cancelProofOnce.Do(func() {
		base, err := baseFirebirdDriver()
		if err != nil {
			errCancelProofDriver = err
			return
		}
		sql.Register(cancelProofDriverName, &cancelProofDriver{
			base:             base,
			statementTimeout: statementTimeout,
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

// cancelProofDriver wraps the firebirdsql driver with two responsibilities.
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
// connection healthy and reusable.
type cancelProofDriver struct {
	base             driver.Driver
	statementTimeout time.Duration
	warnOnce         sync.Once
}

// Open opens a connection through the base driver and applies the statement
// timeout to it.
func (d *cancelProofDriver) Open(dsn string) (driver.Conn, error) {
	conn, err := d.base.Open(dsn)
	if err != nil {
		return nil, err //nolint:wrapcheck // driver errors must reach database/sql verbatim (driver.ErrBadConn).
	}
	d.applyStatementTimeout(conn)
	return &cancelProofConn{base: conn}, nil
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
	// Firebird accepts whole seconds; round up so sub-second configs do not
	// collapse into "no timeout".
	seconds := int64((d.statementTimeout + time.Second - 1) / time.Second)
	stmt := "SET STATEMENT TIMEOUT " + strconv.FormatInt(seconds, 10) + " SECOND"
	if _, err := execer.ExecContext(context.Background(), stmt, nil); err != nil {
		d.warnStatementTimeout(err)
	}
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
// context it forwards.
type cancelProofConn struct {
	base driver.Conn
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
	return &cancelProofStmt{base: st}, nil
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
	return &cancelProofStmt{base: st}, nil
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
	return execer.ExecContext(context.WithoutCancel(ctx), query, args) //nolint:wrapcheck // verbatim driver error.
}

// QueryContext implements driver.QueryerContext. Returns driver.ErrSkip when
// the wrapped connection has no fast path.
func (c *cancelProofConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := c.base.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return queryer.QueryContext(context.WithoutCancel(ctx), query, args) //nolint:wrapcheck // verbatim driver error.
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
	base driver.Stmt
}

// Close implements driver.Stmt.
func (s *cancelProofStmt) Close() error {
	return s.base.Close() //nolint:wrapcheck // verbatim driver error.
}

// NumInput implements driver.Stmt.
func (s *cancelProofStmt) NumInput() int { return s.base.NumInput() }

// Exec implements driver.Stmt (legacy path).
func (s *cancelProofStmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.base.Exec(args) //nolint:staticcheck,wrapcheck // required by driver.Stmt.
}

// Query implements driver.Stmt (legacy path).
func (s *cancelProofStmt) Query(args []driver.Value) (driver.Rows, error) {
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
		return s.Exec(values)
	}
	return execer.ExecContext(context.WithoutCancel(ctx), args) //nolint:wrapcheck // verbatim driver error.
}

// QueryContext implements driver.StmtQueryContext.
func (s *cancelProofStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := s.base.(driver.StmtQueryContext)
	if !ok {
		values, err := namedToValues(args)
		if err != nil {
			return nil, err
		}
		return s.Query(values)
	}
	return queryer.QueryContext(context.WithoutCancel(ctx), args) //nolint:wrapcheck // verbatim driver error.
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
