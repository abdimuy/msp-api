package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// newBorradorVenta builds a fresh active/borrador/pendiente venta.
func newBorradorVenta(t *testing.T) *domain.Venta {
	t.Helper()
	v, err := domain.CrearVenta(validCrearVentaParams(t))
	require.NoError(t, err)
	return v
}

func TestVenta_InitialState(t *testing.T) {
	t.Parallel()
	v := newBorradorVenta(t)
	assert.Equal(t, domain.EstadoActive, v.Estado())
	assert.Equal(t, domain.SituacionBorrador, v.Situacion())
	assert.Equal(t, domain.SincronizacionPendiente, v.Sincronizacion())
	assert.False(t, v.IsAplicada())
	assert.Nil(t, v.MicrosipDoctoPVID())
	assert.Nil(t, v.MicrosipFolio())
	assert.Nil(t, v.MicrosipAplicadaAt())
}

func TestVenta_EnviarARevision(t *testing.T) {
	t.Parallel()
	by := uuid.New()
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	v := newBorradorVenta(t)
	require.NoError(t, v.EnviarARevision(by, now))
	assert.Equal(t, domain.SituacionRevisada, v.Situacion())

	// Not allowed twice (revisada → revisada).
	require.ErrorIs(t, v.EnviarARevision(by, now), domain.ErrVentaNoEnviableARevision)
}

func TestVenta_AprobarOnlyFromRevisada(t *testing.T) {
	t.Parallel()
	by := uuid.New()
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	v := newBorradorVenta(t)
	// Cannot approve straight from borrador.
	require.ErrorIs(t, v.Aprobar(by, now), domain.ErrVentaNoAprobable)

	require.NoError(t, v.EnviarARevision(by, now))
	require.NoError(t, v.Aprobar(by, now))
	assert.Equal(t, domain.SituacionAprobada, v.Situacion())
	require.NotNil(t, v.Aprobacion())
	assert.Equal(t, by, v.Aprobacion().By())
}

func TestVenta_RegresarABorrador(t *testing.T) {
	t.Parallel()
	by := uuid.New()
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	v := newBorradorVenta(t)
	// Not from borrador.
	require.ErrorIs(t, v.RegresarABorrador(by, now), domain.ErrVentaNoRegresableABorrador)

	require.NoError(t, v.EnviarARevision(by, now))
	require.NoError(t, v.RegresarABorrador(by, now))
	assert.Equal(t, domain.SituacionBorrador, v.Situacion())
	assert.Nil(t, v.Aprobacion())
}

func TestVenta_MarcarAplicada(t *testing.T) {
	t.Parallel()
	by := uuid.New()
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	at := time.Date(2026, 5, 2, 11, 0, 0, 0, time.UTC)

	v := newBorradorVenta(t)
	// Cannot apply before approval.
	require.ErrorIs(t, v.MarcarAplicada(123, "Y00002262", at, by), domain.ErrVentaNoAplicable)

	require.NoError(t, v.EnviarARevision(by, now))
	require.NoError(t, v.Aprobar(by, now))

	// Empty artifacts rejected.
	require.ErrorIs(t, v.MarcarAplicada(0, "Y00002262", at, by), domain.ErrMicrosipArtefactosRequeridos)
	require.ErrorIs(t, v.MarcarAplicada(123, "   ", at, by), domain.ErrMicrosipArtefactosRequeridos)

	require.NoError(t, v.MarcarAplicada(123, "Y00002262", at, by))
	assert.True(t, v.IsAplicada())
	assert.Equal(t, domain.SincronizacionAplicada, v.Sincronizacion())
	require.NotNil(t, v.MicrosipDoctoPVID())
	assert.Equal(t, 123, *v.MicrosipDoctoPVID())
	require.NotNil(t, v.MicrosipFolio())
	assert.Equal(t, "Y00002262", *v.MicrosipFolio())
	require.NotNil(t, v.MicrosipAplicadaAt())
	assert.Equal(t, at, *v.MicrosipAplicadaAt())

	// Idempotency guard at the domain level: cannot re-apply.
	require.ErrorIs(t, v.MarcarAplicada(999, "Y00009999", at, by), domain.ErrVentaNoAplicable)
}

func TestVenta_CancelarFromAnyNonCanceled(t *testing.T) {
	t.Parallel()
	by := uuid.New()
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	// From borrador.
	v := newBorradorVenta(t)
	require.NoError(t, v.Cancelar("error de captura", by, now))
	assert.Equal(t, domain.SituacionCancelada, v.Situacion())
	require.ErrorIs(t, v.Cancelar("otra", by, now), domain.ErrVentaYaCancelada)

	// From revisada.
	v2 := newBorradorVenta(t)
	require.NoError(t, v2.EnviarARevision(by, now))
	require.NoError(t, v2.Cancelar("ya no", by, now))
	assert.Equal(t, domain.SituacionCancelada, v2.Situacion())

	// From aprobada.
	v3 := newBorradorVenta(t)
	require.NoError(t, v3.EnviarARevision(by, now))
	require.NoError(t, v3.Aprobar(by, now))
	require.NoError(t, v3.Cancelar("cambio de opinión", by, now))
	assert.Equal(t, domain.SituacionCancelada, v3.Situacion())
}

func TestVenta_CancelarRejectedWhenAplicada(t *testing.T) {
	t.Parallel()
	by := uuid.New()
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	v := newBorradorVenta(t)
	require.NoError(t, v.EnviarARevision(by, now))
	require.NoError(t, v.Aprobar(by, now))
	require.NoError(t, v.MarcarAplicada(123, "Y00002262", now, by))

	require.ErrorIs(t, v.Cancelar("no se puede", by, now), domain.ErrVentaYaAplicada)
	assert.Equal(t, domain.SituacionAprobada, v.Situacion())
}

func TestVenta_NotEditableAfterRevision(t *testing.T) {
	t.Parallel()
	by := uuid.New()
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	v := newBorradorVenta(t)
	require.NoError(t, v.EnviarARevision(by, now))

	// Editing a non-borrador venta is rejected.
	err := v.ActualizarCliente(domain.ActualizarClienteParams{
		Cliente: v.Cliente(), By: by, Now: now,
	})
	require.ErrorIs(t, err, domain.ErrVentaNoEditable)
}
