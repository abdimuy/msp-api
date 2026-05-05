// Package transaction lets services run a function inside a database
// transaction without exposing pgx types to the domain.
//
// The pattern: services call Manager.RunInTx(ctx, fn). Inside fn, repos
// retrieve the active pgx.Tx via getQuerier(ctx) and execute their
// statements through it. If fn returns an error, the tx is rolled back;
// otherwise it commits.
package transaction

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// txKey is the unexported context key for the active pgx.Tx.
type txKey struct{}

// Querier is the surface common to *pgxpool.Pool and pgx.Tx that repos use.
//
// Note: pgx.Tx and *pgxpool.Pool both satisfy this through their Exec/Query
// /QueryRow methods (Tx is wider). Repos should depend on Querier, never on
// the pool directly.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Manager runs functions inside a database transaction.
type Manager struct {
	pool *pgxpool.Pool
}

// NewManager wraps the given pool.
func NewManager(pool *pgxpool.Pool) *Manager {
	return &Manager{pool: pool}
}

// RunInTx executes fn inside a transaction. If fn returns an error, the tx
// is rolled back; otherwise it commits.
//
// Calls to RunInTx nest: if ctx already has an active tx, fn runs inside
// the existing one and no new commit/rollback is issued. This lets services
// compose without worrying about who started the tx.
func (m *Manager) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx) // nested — outer tx owns commit/rollback
	}

	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("transaction: begin: %w", err)
	}

	ctx = context.WithValue(ctx, txKey{}, tx)

	defer func() {
		// Best-effort rollback if commit didn't happen. Ignore the
		// "tx closed" error returned after a successful commit.
		_ = tx.Rollback(ctx)
	}()

	if err := fn(ctx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("transaction: commit: %w", err)
	}
	return nil
}

// GetQuerier returns the active tx if any, otherwise the pool. Repos call
// this to pick the right executor without caring who started the tx.
func GetQuerier(ctx context.Context, fallback Querier) Querier {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return fallback
}

// HasTx reports whether ctx already carries an active transaction.
func HasTx(ctx context.Context) bool {
	_, ok := ctx.Value(txKey{}).(pgx.Tx)
	return ok
}

// ErrNoTx is returned by RequireTx when ctx has no active transaction.
var ErrNoTx = errors.New("transaction: no active transaction in context")

// RequireTx returns the active tx or ErrNoTx if there is none.
//
// Used by repos that *must* run inside a tx (e.g. outbox writes).
func RequireTx(ctx context.Context) (pgx.Tx, error) {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	if !ok {
		return nil, ErrNoTx
	}
	return tx, nil
}
