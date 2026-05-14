//nolint:misspell // domain vocabulary is Spanish (ventas, etc.) per project convention.
package app_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/app"
)

// testHarness aggregates the Service plus the fakes it depends on so tests
// can assert side effects without rebuilding the wiring on every case.
type testHarness struct {
	svc       *app.Service
	ventas    *fakeVentaRepo
	storage   *fakeStorage
	outbox    *fakeOutbox
	imageProc *fakeImageProcessor
	clock     fixedClock
}

// newHarness builds a fresh harness with empty fakes.
func newHarness(_ *testing.T) *testHarness {
	clock := fixedClock{T: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)}
	ventas := newFakeVentaRepo()
	storage := newFakeStorage()
	outbox := &fakeOutbox{}
	imageProc := &fakeImageProcessor{}
	svc := app.NewService(ventas, nil, storage, clock, outbox, imageProc, nil)
	return &testHarness{
		svc:       svc,
		ventas:    ventas,
		storage:   storage,
		outbox:    outbox,
		imageProc: imageProc,
		clock:     clock,
	}
}

// validContadoInput returns a fully populated CrearVentaInput for a CONTADO
// venta. Tests mutate the result before invoking the service.
func validContadoInput() app.CrearVentaInput {
	productoID := uuid.New()
	vendedorID := uuid.New()
	one, two := 1, 2
	return app.CrearVentaInput{
		ID:            uuid.New(),
		ClienteNombre: "Juan Perez",
		Calle:         "Av. Reforma",
		Colonia:       "Centro",
		Poblacion:     "Cuauhtemoc",
		Ciudad:        "CDMX",
		Latitud:       19.4326,
		Longitud:      -99.1332,
		FechaVenta:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		TipoVenta:     "CONTADO",
		PrecioAnual:   decimal.NewFromInt(1200),
		PrecioCorto:   decimal.NewFromInt(1100),
		PrecioContado: decimal.NewFromInt(1000),
		Productos: []app.CrearVentaProductoInput{{
			ID:             productoID,
			ArticuloID:     42,
			Articulo:       "Refrigerador",
			Cantidad:       decimal.NewFromInt(1),
			PrecioAnual:    decimal.NewFromInt(1200),
			PrecioCorto:    decimal.NewFromInt(1100),
			PrecioContado:  decimal.NewFromInt(1000),
			AlmacenOrigen:  &one,
			AlmacenDestino: &two,
		}},
		Vendedores: []app.CrearVentaVendedorInput{{
			ID:        vendedorID,
			UsuarioID: uuid.New(),
			Email:     "vendedor@example.com",
			Nombre:    "Ana Vendedora",
		}},
	}
}

// validCreditoInput returns a fully populated CrearVentaInput for a CREDITO
// venta with a quincenal plan billed by day-of-month.
func validCreditoInput() app.CrearVentaInput {
	in := validContadoInput()
	in.TipoVenta = "CREDITO"
	in.PlanCredito = &app.CrearVentaPlanCreditoInput{
		PlazoMeses:  12,
		Enganche:    decimal.NewFromInt(200),
		Parcialidad: decimal.NewFromInt(150),
		FrecPago:    "QUINCENAL",
	}
	mes := 15
	in.DiaCobranza = &app.CrearVentaDiaCobranzaInput{Mes: &mes}
	return in
}

// seedVenta creates and persists a CONTADO venta via the service so tests
// have something to mutate later. Returns the created aggregate.
func (h *testHarness) seedVenta(t *testing.T) *uuid.UUID {
	t.Helper()
	in := validContadoInput()
	venta, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)
	// Drop the create event so the assertions in tests that focus on later
	// mutations start from a clean outbox slice.
	h.outbox.mu.Lock()
	h.outbox.calls = nil
	h.outbox.mu.Unlock()
	id := venta.ID()
	return &id
}

// TestNewService asserts the constructor returns a non-nil Service.
func TestNewService(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	require.NotNil(t, h.svc)
}
