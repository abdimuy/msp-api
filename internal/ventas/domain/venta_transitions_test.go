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

func TestVenta_RegresarABorrador_FromRevisada(t *testing.T) {
	t.Parallel()
	by := uuid.New()
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	v := newBorradorVenta(t)
	require.NoError(t, v.EnviarARevision(by, now))
	require.NoError(t, v.RegresarABorrador(by, now))
	assert.Equal(t, domain.SituacionBorrador, v.Situacion())
	assert.Nil(t, v.Aprobacion())
}

func TestVenta_RegresarABorrador_FromAprobada_HappyPath(t *testing.T) {
	t.Parallel()
	by := uuid.New()
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	v := newBorradorVenta(t)
	require.NoError(t, v.EnviarARevision(by, now))
	require.NoError(t, v.Aprobar(by, now))
	require.NotNil(t, v.Aprobacion())

	// Drain events emitted up to here so we can assert only the regresar event.
	v.ClearPendingEvents()

	regresadoBy := uuid.New()
	regresadoAt := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	require.NoError(t, v.RegresarABorrador(regresadoBy, regresadoAt))

	assert.Equal(t, domain.SituacionBorrador, v.Situacion())
	assert.Equal(t, domain.SincronizacionPendiente, v.Sincronizacion())
	assert.Nil(t, v.Aprobacion(), "aprobacion debe limpiarse al regresar a borrador")
	a := v.Audit()
	assert.Equal(t, regresadoBy, a.UpdatedBy())

	events := v.PendingEvents()
	require.Len(t, events, 1)
	_, ok := events[0].(domain.VentaRegresadaABorradorEvent)
	assert.True(t, ok, "se debe emitir VentaRegresadaABorradorEvent")
}

func TestVenta_RegresarABorrador_FromAprobada_RejectsWhenAplicada(t *testing.T) {
	t.Parallel()
	by := uuid.New()
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	v := newBorradorVenta(t)
	require.NoError(t, v.EnviarARevision(by, now))
	require.NoError(t, v.Aprobar(by, now))
	require.NoError(t, v.MarcarAplicada(123, "Y00002262", now, by))
	require.True(t, v.IsAplicada())

	aprobacionAntes := v.Aprobacion()
	v.ClearPendingEvents()

	err := v.RegresarABorrador(by, now)
	require.ErrorIs(t, err, domain.ErrVentaNoRegresableABorrador,
		"una venta ya aplicada en Microsip NUNCA puede regresar a borrador")

	// Nada debe haberse mutado.
	assert.Equal(t, domain.SituacionAprobada, v.Situacion())
	assert.Equal(t, domain.SincronizacionAplicada, v.Sincronizacion())
	assert.Equal(t, aprobacionAntes, v.Aprobacion())
	assert.Empty(t, v.PendingEvents(), "no se debe emitir ningún evento cuando la transición es rechazada")
}

func TestVenta_RegresarABorrador_RejectsBorrador(t *testing.T) {
	t.Parallel()
	by := uuid.New()
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	v := newBorradorVenta(t)
	require.ErrorIs(t, v.RegresarABorrador(by, now), domain.ErrVentaNoRegresableABorrador)
	assert.Equal(t, domain.SituacionBorrador, v.Situacion())
}

func TestVenta_RegresarABorrador_RejectsCancelada(t *testing.T) {
	t.Parallel()
	by := uuid.New()
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	v := newBorradorVenta(t)
	require.NoError(t, v.Cancelar("error de captura", by, now))
	require.ErrorIs(t, v.RegresarABorrador(by, now), domain.ErrVentaNoRegresableABorrador)
	assert.Equal(t, domain.SituacionCancelada, v.Situacion())
}

func TestVenta_RegresarABorrador_RejectsDeleted(t *testing.T) {
	t.Parallel()
	by := uuid.New()
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	// Hidratamos una venta soft-deleted que estaba en aprobada antes del delete —
	// defensa en profundidad por si llega un caso así por la persistencia.
	src := newBorradorVenta(t)
	require.NoError(t, src.EnviarARevision(by, now))
	require.NoError(t, src.Aprobar(by, now))
	srcAudit := src.Audit()

	p := domain.HydrateVentaParams{
		ID:             src.ID(),
		ClienteID:      src.ClienteID(),
		Cliente:        src.Cliente(),
		Direccion:      src.Direccion(),
		GPS:            src.GPS(),
		FechaVenta:     src.FechaVenta(),
		TipoVenta:      src.TipoVenta(),
		Montos:         src.Montos(),
		PlanCredito:    src.PlanCredito(),
		DiaCobranza:    src.DiaCobranza(),
		Nota:           src.Nota(),
		Estado:         domain.EstadoDeleted,
		Situacion:      domain.SituacionAprobada,
		Sincronizacion: domain.SincronizacionPendiente,
		Aprobacion:     src.Aprobacion(),
		CreatedAt:      srcAudit.CreatedAt(),
		UpdatedAt:      srcAudit.UpdatedAt(),
		CreatedBy:      srcAudit.CreatedBy(),
		UpdatedBy:      srcAudit.UpdatedBy(),
	}
	v := domain.HydrateVenta(p)

	require.ErrorIs(t, v.RegresarABorrador(by, now), domain.ErrVentaNoRegresableABorrador)
	assert.Equal(t, domain.EstadoDeleted, v.Estado())
	assert.Equal(t, domain.SituacionAprobada, v.Situacion())
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
