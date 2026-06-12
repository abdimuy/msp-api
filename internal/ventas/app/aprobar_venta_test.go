//nolint:misspell // domain vocabulary is Spanish (ventas, revisada, aprobada, borrador) per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// ─── EnviarARevision ───────────────────────────────────────────────────────

// TestEnviarARevision_HappyPath verifies that a borrador venta transitions to
// revisada when EnviarARevision is called.
func TestEnviarARevision_HappyPath(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ventaID := h.seedVenta(t)
	by := uuid.New()

	v, err := h.svc.EnviarARevision(context.Background(), *ventaID, by)
	require.NoError(t, err)
	require.Equal(t, domain.SituacionRevisada, v.Situacion())
}

// TestEnviarARevision_RejectsNonBorrador verifies that calling EnviarARevision
// on a revisada venta returns domain.ErrVentaNoEnviableARevision.
func TestEnviarARevision_RejectsNonBorrador(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ventaID := h.seedRevisada(t)
	by := uuid.New()

	_, err := h.svc.EnviarARevision(context.Background(), ventaID, by)
	require.ErrorIs(t, err, domain.ErrVentaNoEnviableARevision)
}

// ─── Aprobar ───────────────────────────────────────────────────────────────

// TestAprobar_HappyPath verifies that a revisada venta transitions to aprobada
// when Aprobar is called.
func TestAprobar_HappyPath(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ventaID := h.seedRevisada(t)
	by := uuid.New()

	v, err := h.svc.Aprobar(context.Background(), ventaID, by)
	require.NoError(t, err)
	require.Equal(t, domain.SituacionAprobada, v.Situacion())
}

// TestAprobar_RejectsBorrador verifies that calling Aprobar on a borrador venta
// returns domain.ErrVentaNoAprobable.
func TestAprobar_RejectsBorrador(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ventaID := h.seedVenta(t)
	by := uuid.New()

	_, err := h.svc.Aprobar(context.Background(), *ventaID, by)
	require.ErrorIs(t, err, domain.ErrVentaNoAprobable)
}

// ─── RegresarABorrador ─────────────────────────────────────────────────────

// TestRegresarABorrador_FromRevisada_HappyPath verifies that a revisada venta
// transitions back to borrador when RegresarABorrador is called.
func TestRegresarABorrador_FromRevisada_HappyPath(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ventaID := h.seedRevisada(t)
	by := uuid.New()

	v, err := h.svc.RegresarABorrador(context.Background(), ventaID, by)
	require.NoError(t, err)
	require.Equal(t, domain.SituacionBorrador, v.Situacion())
	require.Equal(t, 1, h.ventas.UpdateCalls)
	require.True(t, h.outbox.sawEventType(domain.EventTypeVentaRegresadaABorrador))
}

// TestRegresarABorrador_FromAprobada_HappyPath verifies that an aprobada venta
// transitions back to borrador, the recorded Aprobacion is cleared, the repo
// receives the Update, and the regresada event reaches the outbox.
func TestRegresarABorrador_FromAprobada_HappyPath(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ventaID := h.seedAprobada(t)
	by := uuid.New()

	v, err := h.svc.RegresarABorrador(context.Background(), ventaID, by)
	require.NoError(t, err)
	assert.Equal(t, domain.SituacionBorrador, v.Situacion())
	assert.Nil(t, v.Aprobacion(), "aprobacion debe limpiarse")
	assert.Equal(t, 1, h.ventas.UpdateCalls)
	assert.True(t, h.outbox.sawEventType(domain.EventTypeVentaRegresadaABorrador))
}

// TestRegresarABorrador_FromAprobadaAplicada_Returns_ErrVentaNoRegresableABorrador
// is the critical guardrail test: a venta already materialized in Microsip can
// NEVER regress to borrador. The repo Update must never be called and no event
// may be enqueued.
func TestRegresarABorrador_FromAprobadaAplicada_Returns_ErrVentaNoRegresableABorrador(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ventaID := h.seedAplicada(t)
	by := uuid.New()

	_, err := h.svc.RegresarABorrador(context.Background(), ventaID, by)
	require.ErrorIs(t, err, domain.ErrVentaNoRegresableABorrador)
	assert.Equal(t, 0, h.ventas.UpdateCalls, "repo Update no debe ejecutarse")
	assert.Empty(t, h.outbox.snapshot(), "ningún evento debe enqueuearse")
}

// TestRegresarABorrador_RejectsBorrador verifies that calling RegresarABorrador
// on an already-borrador venta returns domain.ErrVentaNoRegresableABorrador.
func TestRegresarABorrador_RejectsBorrador(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ventaID := h.seedVenta(t)
	by := uuid.New()

	_, err := h.svc.RegresarABorrador(context.Background(), *ventaID, by)
	require.ErrorIs(t, err, domain.ErrVentaNoRegresableABorrador)
	assert.Equal(t, 0, h.ventas.UpdateCalls)
}

// TestRegresarABorrador_NotFound_PropagatesAndSkipsUpdate verifies that an
// unknown ventaID surfaces ErrVentaNotFound without touching Update.
func TestRegresarABorrador_NotFound_PropagatesAndSkipsUpdate(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	_, err := h.svc.RegresarABorrador(context.Background(), uuid.New(), uuid.New())
	require.ErrorIs(t, err, domain.ErrVentaNotFound)
	assert.Equal(t, 0, h.ventas.UpdateCalls)
	assert.Empty(t, h.outbox.snapshot())
}

// TestRegresarABorrador_RepoUpdateError_PropagatesAndSkipsEvent verifies that
// a Update failure aborts the operation before the event is enqueued — the
// transaction would have rolled back so we must not emit a phantom event.
func TestRegresarABorrador_RepoUpdateError_PropagatesAndSkipsEvent(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ventaID := h.seedAprobada(t)
	boom := errors.New("update failed")
	h.ventas.UpdateErr = boom

	_, err := h.svc.RegresarABorrador(context.Background(), ventaID, uuid.New())
	require.ErrorIs(t, err, boom)
	assert.Empty(t, h.outbox.snapshot(), "evento NO debe enqueuearse si Update falla")
}

// TestRegresarABorrador_FindByIDError_Propagates verifies that load errors
// surface directly without invoking Update.
func TestRegresarABorrador_FindByIDError_Propagates(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ventaID := h.seedAprobada(t)
	boom := errors.New("find failed")
	h.ventas.FindErr = boom

	_, err := h.svc.RegresarABorrador(context.Background(), ventaID, uuid.New())
	require.ErrorIs(t, err, boom)
	assert.Equal(t, 0, h.ventas.UpdateCalls)
}
