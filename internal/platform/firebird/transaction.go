package firebird

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/nakagami/firebirdsql"
)

// txKey is the unexported context key for the active *sql.Tx.
type txKey struct{}

// Querier is the surface common to *sql.DB and *sql.Tx that repos use.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// TxManager runs functions inside a Firebird transaction without exposing
// *sql.Tx to the domain.
type TxManager struct {
	db *sql.DB
}

// NewTxManager wraps the given pool. Accepts a *sql.DB so test code can pass
// a stub; production callers pass pool.DB.
func NewTxManager(db *sql.DB) *TxManager {
	return &TxManager{db: db}
}

// RunInTx executes fn inside a transaction at READ COMMITTED isolation. If fn
// returns an error, the tx is rolled back; otherwise it commits. Re-entrant:
// if ctx already carries an active tx, fn runs inside the existing one.
//
// Delegates to the free-function RunInTx so callers without a *TxManager
// (repos that only hold a *sql.DB) can share the exact same lifecycle.
func (m *TxManager) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return RunInTx(ctx, m.db, fn)
}

// RunInTxNoWait runs fn at READ COMMITTED NO WAIT — the Firebird flavor where
// a lock conflict returns immediately instead of blocking. Use for hot paths
// where the caller would rather retry than wait (traspasos contention, etc.).
func (m *TxManager) RunInTxNoWait(ctx context.Context, fn func(ctx context.Context) error) error {
	return runInTx(ctx, m.db, &sql.TxOptions{Isolation: sql.IsolationLevel(firebirdsql.LevelReadCommittedNoWait)}, "no-wait", fn)
}

// RunInTx is the free-function equivalent of TxManager.RunInTx: it executes
// fn inside a READ COMMITTED transaction on db, committing on success and
// rolling back on error. Re-entrant via the same context key as the manager —
// if ctx already carries an active tx, fn runs inside the existing one and
// no BEGIN/COMMIT is issued.
//
// Use this form in repos that only hold a *firebird.Pool / *sql.DB and do
// not want to inject a *TxManager just to obtain transactional semantics.
func RunInTx(ctx context.Context, db *sql.DB, fn func(ctx context.Context) error) error {
	return runInTx(ctx, db, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, "", fn)
}

// RunInReadTx runs fn inside a READ COMMITTED transaction explicitly
// committed at the end, even for read-only workloads. The purpose is to
// prevent the nakagami/firebirdsql implicit-transaction leak: every
// QueryContext / QueryRowContext on a bare *sql.DB opens an implicit tx that
// the driver never commits, leaving an idle uncommitted tx in MON$TRANSACTIONS
// that pins MON$OLDEST_ACTIVE and blocks the watermark probe.
//
// Wrapping reads in this helper forces an explicit BeginTx + Commit cycle:
// the tx state transitions cleanly to committed (MON$STATE = 3) instead of
// lingering as idle (MON$STATE = 0).
//
// Re-entrant: if ctx already carries an active tx, fn runs inside the
// existing one with no BEGIN/COMMIT. This is the correct behavior even
// though the outer tx may be a write tx — the inner read sees the outer's
// uncommitted writes, which is what callers expect when chaining
// read-after-write logic inside a single service call.
//
// Note: the firebirdsql driver does not honor sql.TxOptions.ReadOnly via
// database/sql — there is no read-only TPB path exposed. We use READ
// COMMITTED isolation for parity with TxManager.RunInTx; the "read-only-ness"
// is enforced by the caller never issuing DML inside fn.
func RunInReadTx(ctx context.Context, db *sql.DB, fn func(ctx context.Context) error) error {
	return runInTx(ctx, db, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, "read", fn)
}

// RunInSnapshotTx runs fn inside a REPEATABLE READ transaction — Firebird's
// isc_tpb_concurrency, a true snapshot that freezes the visible row set at
// BEGIN. Use for digest / list pairs that must see one consistent
// point-in-time view across multiple queries.
//
// Like RunInReadTx, this helper exists to prevent the implicit-tx leak: it
// guarantees an explicit BeginTx + Commit pair so the tx ends in MON$STATE=3
// (committed) rather than lingering as idle/uncommitted.
//
// Re-entrant via the same context key. If ctx already carries an active tx,
// fn runs inside it (no new BEGIN). Note that nesting a snapshot request
// inside a READ COMMITTED outer tx silently downgrades isolation to the
// outer's level — callers depending strictly on snapshot semantics must not
// be invoked from inside another RunInTx.
func RunInSnapshotTx(ctx context.Context, db *sql.DB, fn func(ctx context.Context) error) error {
	return runInTx(ctx, db, &sql.TxOptions{Isolation: sql.LevelRepeatableRead}, "snapshot", fn)
}

func runInTx(ctx context.Context, db *sql.DB, opts *sql.TxOptions, label string, fn func(context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return fn(ctx) // nested — outer owns commit/rollback
	}
	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("firebird: begin%s: %w", labelSuffix(label), err)
	}
	ctx = context.WithValue(ctx, txKey{}, tx)

	defer func() {
		_ = tx.Rollback() // best-effort; commit already returned the tx
	}()

	if err := fn(ctx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("firebird: commit%s: %w", labelSuffix(label), err)
	}
	return nil
}

func labelSuffix(label string) string {
	if label == "" {
		return ""
	}
	return " " + label + " tx"
}

// GetQuerier returns the active tx if any, otherwise the fallback. Repos call
// this so the same method body works inside and outside a tx.
func GetQuerier(ctx context.Context, fallback Querier) Querier {
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return tx
	}
	return fallback
}

// HasTx reports whether ctx already carries an active transaction.
func HasTx(ctx context.Context) bool {
	_, ok := ctx.Value(txKey{}).(*sql.Tx)
	return ok
}

// ErrNoTx is returned by RequireTx when ctx has no active transaction.
var ErrNoTx = errors.New("firebird: no active transaction in context")

// RequireTx returns the active tx or ErrNoTx.
func RequireTx(ctx context.Context) (*sql.Tx, error) {
	tx, ok := ctx.Value(txKey{}).(*sql.Tx)
	if !ok {
		return nil, ErrNoTx
	}
	return tx, nil
}
