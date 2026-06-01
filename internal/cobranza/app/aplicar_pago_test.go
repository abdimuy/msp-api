//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/app"
)

// TestAplicarPago_MissingDeps verifies that AplicarPago returns an internal
// error (not a nil-deref panic) when the Service is constructed without the
// write-side dependencies. This is the defensive wiring-bug guard that runs
// before any Firebird interaction.
func TestAplicarPago_MissingDeps(t *testing.T) {
	t.Parallel()

	// txMgr is nil → runInTx returns errWriteDepsMissing.
	svc := app.NewService(
		newFakeSaldosRepo(),
		newFakePagosRepo(),
		nil,
		fixedClock{T: time.Now()},
		newFakePagosRecibidosRepo(),
		nil,
		&fakeMicrosipPagoWriter{},
		nil, nil,
		nil, // txMgr nil
	)

	_, err := svc.AplicarPago(context.Background(), uuid.New(), uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cobranza_write_deps_missing",
		"error should surface as a wiring error")
}
