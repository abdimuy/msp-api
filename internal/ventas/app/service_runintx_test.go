//nolint:misspell // ventas vocabulary is Spanish per project convention.
package app_test

// Tests for Service.runInTx error propagation. Two contracts:
//   1. nil TxManager → fn runs directly (already implicit in newHarness;
//      pinned here for regression detection).
//   2. non-nil TxManager whose underlying *sql.DB rejects BeginTx →
//      CrearVenta surfaces the error. We construct a *sql.DB on top of an
//      always-failing driver.Connector so the test does not need a real
//      Firebird container.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/app"
)

// errConnector is a driver.Connector whose Connect always returns the
// configured error. Used to back a *sql.DB so any operation requiring a
// connection (BeginTx in particular) fails deterministically.
type errConnector struct{ err error }

func (c errConnector) Connect(_ context.Context) (driver.Conn, error) { return nil, c.err }
func (c errConnector) Driver() driver.Driver                          { return panicDriver{} }

// panicDriver is the unused fallback Driver — never invoked because the
// Connector path takes precedence for sql.OpenDB.
type panicDriver struct{}

func (panicDriver) Open(string) (driver.Conn, error) { panic("unreachable") }

// TestService_runInTx_PropagatesError verifies that when a real TxManager is
// wired and BeginTx errors, the failure surfaces unchanged through CrearVenta
// — the runInTx wrapper does not swallow or rewrite TxManager errors.
func TestService_runInTx_PropagatesError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("simulated connection refused")
	db := sql.OpenDB(errConnector{err: sentinel})
	t.Cleanup(func() { _ = db.Close() })
	txMgr := firebird.NewTxManager(db)

	h := newHarness(t)
	// Replace the service with one that has a real (failing) TxManager.
	h.svc = app.NewService(h.ventas, nil, nil, h.storage, h.clock, h.outbox, h.imageProc, txMgr, nil, nil, nil)

	_, err := h.svc.CrearVenta(t.Context(), validContadoInput(), uuid.New())
	require.ErrorIs(t, err, sentinel,
		"runInTx must propagate the underlying BeginTx error verbatim")
	assert.Zero(t, h.ventas.SaveCalls,
		"Save must not run when the tx never began")
}

// TestService_runInTx_NoTxMgrFallsThrough pins the nil-TxManager branch:
// fn runs directly, side effects happen, no panic. Implicit in every other
// test, but explicit here so a future "txMgr now required" refactor is a
// loud failure rather than a silent regression.
func TestService_runInTx_NoTxMgrFallsThrough(t *testing.T) {
	t.Parallel()
	h := newHarness(t) // newHarness wires nil TxManager.
	_, err := h.svc.CrearVenta(t.Context(), validContadoInput(), uuid.New())
	require.NoError(t, err)
	assert.Equal(t, 1, h.ventas.SaveCalls)
}
