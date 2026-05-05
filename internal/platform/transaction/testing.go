package transaction

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// InjectTx exposes the unexported tx context key to the testutil package.
//
// Production code never calls this — it's a test seam used by
// testutil.WithTestTransaction so a test can plant a pre-started tx in the
// context that downstream repos read via GetQuerier.
func InjectTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}
