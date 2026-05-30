//nolint:misspell // domain vocabulary is Spanish (ventas, revisada, aprobada, borrador) per project convention.
package app_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
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

// TestRegresarABorrador_HappyPath verifies that a revisada venta transitions
// back to borrador when RegresarABorrador is called.
func TestRegresarABorrador_HappyPath(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ventaID := h.seedRevisada(t)
	by := uuid.New()

	v, err := h.svc.RegresarABorrador(context.Background(), ventaID, by)
	require.NoError(t, err)
	require.Equal(t, domain.SituacionBorrador, v.Situacion())
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
}
