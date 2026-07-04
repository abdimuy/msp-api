//nolint:misspell // domain vocabulary is Spanish (ventas) per project convention.
package app_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// newMinimalVenta builds a hydrated venta with just enough fields set for
// VentaToSearchDoc to run without panicking. Used by ReconciliarVentas
// pagination tests that need distinct venta identities without exercising
// the full CrearVenta pipeline.
func newMinimalVenta(t *testing.T) *domain.Venta {
	t.Helper()
	nombre, err := domain.NewNombreCliente("Cliente De Prueba")
	require.NoError(t, err)
	cliente := domain.HydrateClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: nombre})
	dir := domain.HydrateDireccion(domain.NewDireccionParams{
		Calle: "Calle 1", Colonia: "Col", Poblacion: "Pob", Ciudad: "Ciudad",
	})
	montos := domain.HydrateMontoSnapshot(decimal.NewFromInt(100), decimal.NewFromInt(90), decimal.NewFromInt(80))
	now := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	return domain.HydrateVenta(domain.HydrateVentaParams{
		ID:             uuid.New(),
		Cliente:        cliente,
		Direccion:      dir,
		FechaVenta:     now,
		TipoVenta:      domain.TipoVentaContado,
		Montos:         montos,
		Estado:         domain.EstadoActive,
		Situacion:      domain.SituacionBorrador,
		Sincronizacion: domain.SincronizacionPendiente,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
}

func TestReconciliarVentas_NoIndexWired_NoOp(t *testing.T) {
	t.Parallel()
	h := newHarness(t) // searchIndex not wired
	n, err := h.svc.ReconciliarVentas(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestReconciliarVentas_IncludesCanceladas(t *testing.T) {
	t.Parallel()
	h := newBuscarHarness(t)
	h.seedVenta(t)

	n, err := h.svc.ReconciliarVentas(t.Context())
	require.NoError(t, err)
	assert.Positive(t, n)

	require.NotEmpty(t, h.index.ReconciliarCalls)
	// Assert the filters passed to List explicitly opted into canceled ventas.
	assert.True(t, h.ventas.LastListFilters.IncluirCanceladas)
}

func TestReconciliarVentas_PagesThroughAllResults(t *testing.T) {
	t.Parallel()
	h := newBuscarHarness(t)

	v1 := newMinimalVenta(t)
	v2 := newMinimalVenta(t)

	h.ventas.ListPages = []outbound.Page[*domain.Venta]{
		{Items: []*domain.Venta{v1}, NextCursor: "o1"},
		{Items: []*domain.Venta{v2}, NextCursor: ""},
	}

	n, err := h.svc.ReconciliarVentas(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, 2, h.ventas.ListCalls)

	require.Len(t, h.index.ReconciliarCalls, 2)
	assert.Equal(t, v1.ID(), h.index.ReconciliarCalls[0][0].ID)
	assert.Equal(t, v2.ID(), h.index.ReconciliarCalls[1][0].ID)
}

func TestReconciliarVentas_ListErrorPropagates(t *testing.T) {
	t.Parallel()
	h := newBuscarHarness(t)
	boom := errors.New("firebird down")
	h.ventas.ListErr = boom

	_, err := h.svc.ReconciliarVentas(t.Context())
	require.ErrorIs(t, err, boom)
}

func TestReconciliarVentas_IndexErrorPropagates(t *testing.T) {
	t.Parallel()
	h := newBuscarHarness(t)
	h.seedVenta(t)
	boom := errors.New("meili down")
	h.index.ReconciliarErr = boom
	h.index.ReconciliarErrSticky = true

	_, err := h.svc.ReconciliarVentas(t.Context())
	require.ErrorIs(t, err, boom)
}
