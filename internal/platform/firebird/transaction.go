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
func (m *TxManager) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return m.runInTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, fn)
}

// RunInTxNoWait runs fn at READ COMMITTED NO WAIT — the Firebird flavor where
// a lock conflict returns immediately instead of blocking. Use for hot paths
// where the caller would rather retry than wait (traspasos contention, etc.).
func (m *TxManager) RunInTxNoWait(ctx context.Context, fn func(ctx context.Context) error) error {
	return m.runInTx(ctx, &sql.TxOptions{Isolation: sql.IsolationLevel(firebirdsql.LevelReadCommittedNoWait)}, fn)
}

func (m *TxManager) runInTx(ctx context.Context, opts *sql.TxOptions, fn func(context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return fn(ctx) // nested — outer owns commit/rollback
	}
	tx, err := m.db.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("firebird: begin: %w", err)
	}
	ctx = context.WithValue(ctx, txKey{}, tx)

	defer func() {
		_ = tx.Rollback() // best-effort; commit already returned the tx
	}()

	if err := fn(ctx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("firebird: commit: %w", err)
	}
	return nil
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
