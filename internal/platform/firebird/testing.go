package firebird

import (
	"context"
	"database/sql"
)

// InjectTx exposes the unexported tx context key to the fbtestutil package.
//
// Production code never calls this — it's a test seam used by
// fbtestutil.WithTestTransaction so a test can plant a pre-started tx in the
// context that downstream repos read via GetQuerier.
func InjectTx(ctx context.Context, tx *sql.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}
