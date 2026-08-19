//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
	ventasoutbound "github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// ventaParaFase builds a minimally hydrated venta with the given id, enough
// for toVentaDTO to project it.
func ventaParaFase(t *testing.T, id uuid.UUID) *domain.Venta {
	t.Helper()
	nombre, err := domain.NewNombreCliente("Cliente De Prueba")
	require.NoError(t, err)
	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	return domain.HydrateVenta(domain.HydrateVentaParams{
		ID:      id,
		Cliente: domain.HydrateClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: nombre}),
		Direccion: domain.HydrateDireccion(domain.NewDireccionParams{
			Calle: "AV. REFORMA", Colonia: "CENTRO", Poblacion: "CUAUHTEMOC", Ciudad: "CDMX",
		}),
		FechaVenta: now,
		TipoVenta:  domain.TipoVentaContado,
		Montos: domain.HydrateMontoSnapshot(
			decimal.NewFromInt(100), decimal.NewFromInt(90), decimal.NewFromInt(80)),
		Estado:         domain.EstadoActive,
		Situacion:      domain.SituacionBorrador,
		Sincronizacion: domain.SincronizacionPendiente,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
}

func TestToVentaDTO_FaseDesde_NilMapLeavesFieldAbsent(t *testing.T) {
	t.Parallel()

	dto := toVentaDTO(ventaParaFase(t, uuid.New()), nil, nil, nil)

	assert.Nil(t, dto.FaseDesde, "a nil fase map must leave fase_desde absent")
}

func TestToVentaDTO_FaseDesde_MissLeavesFieldAbsent(t *testing.T) {
	t.Parallel()

	fases := map[uuid.UUID]ventasoutbound.FaseDeVenta{
		uuid.New(): {Desde: time.Now(), Alcanzada: 1},
	}

	dto := toVentaDTO(ventaParaFase(t, uuid.New()), nil, nil, fases)

	assert.Nil(t, dto.FaseDesde,
		"a venta absent from the map carries no fase_desde — never an invented date")
}

func TestToVentaDTO_FaseDesde_RendersRFC3339UTC(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	// Deliberately NOT in UTC: the contract with the frontend is RFC3339 UTC.
	mexico := time.FixedZone("CST", -6*60*60)
	fases := map[uuid.UUID]ventasoutbound.FaseDeVenta{
		id: {Desde: time.Date(2026, 8, 12, 11, 30, 0, 0, mexico), Alcanzada: 1},
	}

	dto := toVentaDTO(ventaParaFase(t, id), nil, nil, fases)

	require.NotNil(t, dto.FaseDesde)
	assert.Equal(t, "2026-08-12T17:30:00Z", *dto.FaseDesde)
}

// TestPageActorIDs_CollapsesDuplicatesAcrossThePage pins the batching input
// for the listing: every distinct actor across the page, once.
func TestPageActorIDs_CollapsesDuplicatesAcrossThePage(t *testing.T) {
	t.Parallel()

	creador := uuid.New()
	otro := uuid.New()
	a := ventaParaFase(t, uuid.New())
	b := ventaParaFase(t, uuid.New())
	c := ventaParaFase(t, uuid.New())
	setActores(a, creador, creador)
	setActores(b, creador, otro)
	setActores(c, otro, otro)

	ids := pageActorIDs([]*domain.Venta{a, b, c, nil})

	assert.ElementsMatch(t, []uuid.UUID{creador, otro}, ids)
}

// setActores rehydrates v with the given audit actors. Audit fields are
// private, so the venta is rebuilt through the hydrator.
func setActores(v *domain.Venta, createdBy, updatedBy uuid.UUID) {
	a := v.Audit()
	*v = *domain.HydrateVenta(domain.HydrateVentaParams{
		ID:             v.ID(),
		Cliente:        v.Cliente(),
		Direccion:      v.Direccion(),
		FechaVenta:     v.FechaVenta(),
		TipoVenta:      v.TipoVenta(),
		Montos:         v.Montos(),
		Estado:         v.Estado(),
		Situacion:      v.Situacion(),
		Sincronizacion: v.Sincronizacion(),
		CreatedAt:      a.CreatedAt(),
		UpdatedAt:      a.UpdatedAt(),
		CreatedBy:      createdBy,
		UpdatedBy:      updatedBy,
	})
}

// TestToVentaDTO_FaseAlcanzada_AbsentWithoutData pins the omission contract:
// no map, a miss, or a venta whose recorded events prove no fase all leave
// fase_alcanzada out of the JSON.
func TestToVentaDTO_FaseAlcanzada_AbsentWithoutData(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	otra := uuid.New()

	assert.Nil(t, toVentaDTO(ventaParaFase(t, id), nil, nil, nil).FaseAlcanzada,
		"a nil fase map must leave fase_alcanzada absent")

	miss := map[uuid.UUID]ventasoutbound.FaseDeVenta{
		otra: {Desde: time.Now(), Alcanzada: 3},
	}
	assert.Nil(t, toVentaDTO(ventaParaFase(t, id), nil, nil, miss).FaseAlcanzada,
		"a venta absent from the map carries no fase_alcanzada")

	desconocida := map[uuid.UUID]ventasoutbound.FaseDeVenta{
		id: {Desde: time.Date(2026, 8, 12, 17, 30, 0, 0, time.UTC)},
	}
	dto := toVentaDTO(ventaParaFase(t, id), nil, nil, desconocida)
	assert.Nil(t, dto.FaseAlcanzada,
		"Alcanzada == 0 means unknown: the field is omitted, never sent as 0")
	require.NotNil(t, dto.FaseDesde, "fase_desde is independent and stays present")
}

// TestToVentaDTO_FaseAlcanzada_CarriesTheNumber verifies the number reaches
// the DTO as-is, next to fase_desde.
func TestToVentaDTO_FaseAlcanzada_CarriesTheNumber(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	fases := map[uuid.UUID]ventasoutbound.FaseDeVenta{
		id: {Desde: time.Date(2026, 8, 12, 17, 30, 0, 0, time.UTC), Alcanzada: 2},
	}

	dto := toVentaDTO(ventaParaFase(t, id), nil, nil, fases)

	require.NotNil(t, dto.FaseAlcanzada)
	assert.Equal(t, 2, *dto.FaseAlcanzada)
	require.NotNil(t, dto.FaseDesde)
	assert.Equal(t, "2026-08-12T17:30:00Z", *dto.FaseDesde)
}
