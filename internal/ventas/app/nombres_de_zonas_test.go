//nolint:misspell // domain vocabulary is Spanish (ventas, zonas) per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// fakeZonaResolver maps a fixed set of zona ids to names, recording every
// call so tests can assert the batching behavior (one call per page).
type fakeZonaResolver struct {
	nombres  map[int]string
	err      error
	calls    int
	lastIDs  []int
	totalIDs int
}

func (f *fakeZonaResolver) NombresPorID(_ context.Context, ids []int) (map[int]string, error) {
	f.calls++
	f.lastIDs = append([]int(nil), ids...)
	f.totalIDs += len(ids)
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[int]string, len(ids))
	for _, id := range ids {
		if n, ok := f.nombres[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}

// ventaConZona builds a hydrated venta whose direccion carries zonaID (nil
// for a venta captured without zona).
func ventaConZona(t *testing.T, zonaID *int) *domain.Venta {
	t.Helper()
	nombre, err := domain.NewNombreCliente("Cliente De Prueba")
	require.NoError(t, err)
	cliente := domain.HydrateClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: nombre})
	dir := domain.HydrateDireccion(domain.NewDireccionParams{
		Calle: "Calle 1", Colonia: "Col", Poblacion: "Pob", Ciudad: "Ciudad",
		ZonaClienteID: zonaID,
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

func TestNombresDeZonas_BatchesUniqueIDsInOneCall(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	zonas := &fakeZonaResolver{nombres: map[int]string{7: "TEHUACAN NORTE", 9: "TEHUACAN SUR"}}
	h.svc.WithZonaNombreResolver(zonas)

	sieteA, sieteB, nueve := 7, 7, 9
	ventas := []*domain.Venta{
		ventaConZona(t, &sieteA),
		ventaConZona(t, &sieteB),
		ventaConZona(t, &nueve),
		ventaConZona(t, nil), // no zona captured: contributes no id
	}

	out := h.svc.NombresDeZonas(t.Context(), ventas)

	assert.Equal(t, 1, zonas.calls, "the resolver must be called exactly once per page")
	assert.Equal(t, 2, zonas.totalIDs, "duplicate zona ids must be collapsed before the call")
	assert.ElementsMatch(t, []int{7, 9}, zonas.lastIDs)
	assert.Equal(t, map[int]string{7: "TEHUACAN NORTE", 9: "TEHUACAN SUR"}, out)
}

func TestNombresDeZonas_NoResolverWired_ReturnsEmptyMap(t *testing.T) {
	t.Parallel()
	h := newHarness(t) // zona resolver deliberately not wired
	zona := 7

	out := h.svc.NombresDeZonas(t.Context(), []*domain.Venta{ventaConZona(t, &zona)})

	assert.Empty(t, out)
}

func TestNombresDeZonas_ResolverError_ReturnsEmptyMap(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	zonas := &fakeZonaResolver{err: errors.New("firebird down")}
	h.svc.WithZonaNombreResolver(zonas)
	zona := 7

	out := h.svc.NombresDeZonas(t.Context(), []*domain.Venta{ventaConZona(t, &zona)})

	assert.Empty(t, out, "a lookup failure is best-effort: no names, no error")
}

func TestNombresDeZonas_NoZonaIDs_SkipsTheResolverEntirely(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	zonas := &fakeZonaResolver{nombres: map[int]string{7: "TEHUACAN NORTE"}}
	h.svc.WithZonaNombreResolver(zonas)

	out := h.svc.NombresDeZonas(t.Context(), []*domain.Venta{ventaConZona(t, nil)})

	assert.Empty(t, out)
	assert.Equal(t, 0, zonas.calls, "no zona ids means no query")
}

func TestNombresDeZonas_EmptyInput_SkipsTheResolverEntirely(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	zonas := &fakeZonaResolver{nombres: map[int]string{7: "TEHUACAN NORTE"}}
	h.svc.WithZonaNombreResolver(zonas)

	out := h.svc.NombresDeZonas(t.Context(), nil)

	assert.Empty(t, out)
	assert.Equal(t, 0, zonas.calls)
}
