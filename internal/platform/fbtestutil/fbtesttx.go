package fbtestutil

import (
	"context"
	"testing"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// WithTestTransaction runs fn inside a transaction that always rolls back.
//
// The active *sql.Tx is injected into the context via firebird.InjectTx so
// repos calling firebird.GetQuerier(ctx, fallback) automatically execute their
// statements through the test transaction. This is the ONLY safe pattern for
// writing tests against the real Microsip DB — anything that escapes a
// rollback would pollute the dev database.
//
// There is intentionally no WithTestCommit counterpart. If you ever need to
// test true commit semantics, do it in an isolated branch DB you can throw
// away, not in the shared Microsip instance.
func WithTestTransaction(tb testing.TB, pool *firebird.Pool, fn func(ctx context.Context)) {
	tb.Helper()
	ctx := context.Background()

	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		tb.Fatalf("fbtestutil: begin tx: %v", err)
	}
	defer func() {
		// Rollback is always safe here — Commit is never called.
		_ = tx.Rollback()
	}()

	fn(firebird.InjectTx(ctx, tx))
}
