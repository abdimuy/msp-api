package transaction_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/transaction"
)

func TestHasTx_FalseOnPlainContext(t *testing.T) {
	t.Parallel()
	assert.False(t, transaction.HasTx(context.Background()))
}

func TestRequireTx_ErrNoTxOnPlainContext(t *testing.T) {
	t.Parallel()
	tx, err := transaction.RequireTx(context.Background())
	require.ErrorIs(t, err, transaction.ErrNoTx)
	assert.Nil(t, tx)
}

// stubQuerier is a no-op Querier used to verify GetQuerier returns the
// fallback when context has no tx attached.
type stubQuerier struct{ name string }

func (s stubQuerier) Exec(_ context.Context, _ string, _ ...any) (pgconnTagStub, error) {
	return pgconnTagStub{}, nil
}

func (s stubQuerier) Query(_ context.Context, _ string, _ ...any) (rowsStub, error) {
	return rowsStub{}, nil
}

func (s stubQuerier) QueryRow(_ context.Context, _ string, _ ...any) rowStub {
	return rowStub{}
}

type (
	pgconnTagStub struct{}
	rowsStub      struct{}
	rowStub       struct{}
)

// Compile-time check: this stub does NOT satisfy transaction.Querier
// (return types differ). The point of this test is just to assert the
// helper function returns the *fallback* when no tx is in context — we
// don't need a real Querier for that.
func TestGetQuerier_ReturnsFallbackWhenNoTx(t *testing.T) {
	t.Parallel()
	// Use a typed nil that satisfies transaction.Querier (any interface).
	var fallback transaction.Querier // nil interface value
	got := transaction.GetQuerier(context.Background(), fallback)
	assert.Equal(t, fallback, got)
}
