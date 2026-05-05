package testutil

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/abdimuy/msp-api/internal/platform/transaction"
)

// WithTestTransaction runs fn inside a transaction that always rolls back.
//
// The active tx is injected into ctx so repos using
// transaction.GetQuerier(ctx, fallback) execute their statements through the
// tx. Use this for the fast path — the vast majority of integration tests.
func WithTestTransaction(tb testing.TB, pool *pgxpool.Pool, fn func(ctx context.Context)) {
	tb.Helper()
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		tb.Fatalf("testutil: begin tx: %v", err)
	}
	defer func() {
		// Rollback is always safe — Commit was never called.
		_ = tx.Rollback(ctx)
	}()

	fn(transaction.InjectTx(ctx, tx))
}

// WithTestCommit runs fn with a plain context — no auto-rollback. Committed
// rows persist in the calling package's isolated DB until process exit.
//
// Use only when the test needs real commits: optimistic locking, unique
// constraint violations at COMMIT, outbox dispatcher SELECT FOR UPDATE
// SKIP LOCKED, transaction-boundary tests.
func WithTestCommit(tb testing.TB, _ *pgxpool.Pool, fn func(ctx context.Context)) {
	tb.Helper()
	fn(context.Background())
}
