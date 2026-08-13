//nolint:misspell // Spanish domain vocabulary (venta, producto, articulo) by project convention.
package cobranzahttp

// Unit tests for the productos-attachment mapping added to dto_ventas.go:
// toProductoDTO / toVentaDTO / toSyncVentasBody. White-box (package
// cobranzahttp, not cobranzahttp_test) because these mapping helpers are
// unexported.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// testVenta builds a minimal but valid domain.Venta with the given
// DoctoCCID/DoctoPVID, for mapping tests. All other fields are filled with
// deterministic, realistic-looking values.
func testVenta(doctoCCID int, doctoPVID *int) domain.Venta {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	return domain.HydrateVenta(domain.HydrateVentaParams{
		DoctoCCID:      doctoCCID,
		DoctoPVID:      doctoPVID,
		ClienteID:      24037,
		ZonaClienteID:  intPtr(3),
		Folio:          "F-0001",
		FechaCargo:     now,
		PrecioTotal:    decimal.RequireFromString("5000.00"),
		TotalImporte:   decimal.RequireFromString("2000.00"),
		ImpteRest:      decimal.RequireFromString("0.00"),
		Saldo:          decimal.RequireFromString("3000.00"),
		NumPagos:       4,
		CargoCancelado: false,
		UpdatedAt:      now,
		ClienteNombre:  "Minerva Lopez",
		ClienteNotas:   "",
		ZonaNombre:     "Zona Centro",
	})
}

func intPtr(i int) *int { return &i }

func testProductoVenta(doctoPVID, posicion int, precioUnit, precioTotal string) domain.ProductoVenta {
	return domain.HydrateProductoVenta(domain.HydrateProductoVentaParams{
		DoctoPVDetID:    1000 + posicion,
		DoctoPVID:       doctoPVID,
		Folio:           "F-0001",
		ArticuloID:      500 + posicion,
		Articulo:        "Recamara Matrimonial",
		Cantidad:        1,
		PrecioUnitario:  decimal.RequireFromString(precioUnit),
		PrecioTotalNeto: decimal.RequireFromString(precioTotal),
		Posicion:        posicion,
	})
}

// ─── toVentaDTO ───────────────────────────────────────────────────────────────

func TestToVentaDTO_AttachesProductosWhenPresent(t *testing.T) {
	t.Parallel()
	v := testVenta(101, intPtr(4464))
	productos := []domain.ProductoVenta{
		testProductoVenta(4464, 0, "1500.50", "1500.50"),
		testProductoVenta(4464, 1, "2999.99", "2999.99"),
	}

	dto := toVentaDTO(v, productos)

	require.Len(t, dto.Productos, 2)

	p0 := dto.Productos[0]
	assert.Equal(t, 1000, p0.DoctoPVDetID)
	assert.Equal(t, 4464, p0.DoctoPVID)
	assert.Equal(t, "F-0001", p0.Folio)
	assert.Equal(t, 500, p0.ArticuloID)
	assert.Equal(t, "Recamara Matrimonial", p0.Articulo)
	assert.Equal(t, 1, p0.Cantidad)
	assert.InDelta(t, 1500.50, p0.PrecioUnitarioImpto, 0.0001)
	assert.InDelta(t, 1500.50, p0.PrecioTotalNeto, 0.0001)
	assert.Equal(t, 0, p0.Posicion)

	p1 := dto.Productos[1]
	assert.Equal(t, 1, p1.Posicion)
	assert.InDelta(t, 2999.99, p1.PrecioUnitarioImpto, 0.0001)
}

func TestToVentaDTO_ProductosNeverNull(t *testing.T) {
	t.Parallel()
	v := testVenta(102, intPtr(9999))

	// nil slice.
	dtoNil := toVentaDTO(v, nil)
	require.NotNil(t, dtoNil.Productos)
	assert.Empty(t, dtoNil.Productos)

	jsonNil, err := json.Marshal(dtoNil)
	require.NoError(t, err)
	assert.Contains(t, string(jsonNil), `"productos":[]`)
	assert.NotContains(t, string(jsonNil), `"productos":null`)

	// empty (non-nil) slice.
	dtoEmpty := toVentaDTO(v, []domain.ProductoVenta{})
	require.NotNil(t, dtoEmpty.Productos)
	assert.Empty(t, dtoEmpty.Productos)

	jsonEmpty, err := json.Marshal(dtoEmpty)
	require.NoError(t, err)
	assert.Contains(t, string(jsonEmpty), `"productos":[]`)
}

// ─── toSyncVentasBody ─────────────────────────────────────────────────────────

func TestToSyncVentasBody_MapsProductosByDoctoPVID(t *testing.T) {
	t.Parallel()
	ventaA := testVenta(201, intPtr(4464))
	ventaB := testVenta(202, intPtr(7777))
	ventaC := testVenta(203, nil) // no DOCTO_PV_ID

	page := outbound.SyncPage[domain.Venta]{
		Items:        []domain.Venta{ventaA, ventaB, ventaC},
		MaxUpdatedAt: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
		ServerNow:    time.Date(2026, 8, 10, 12, 0, 1, 0, time.UTC),
		HasMore:      false,
	}

	productos := map[int][]domain.ProductoVenta{
		4464: {testProductoVenta(4464, 0, "100.00", "100.00")},
		// 7777 intentionally absent from the map — must map to empty.
	}

	body := toSyncVentasBody(page, productos, 0)
	require.Len(t, body.Items, 3)

	byDoctoCC := make(map[int]VentaDTO, len(body.Items))
	for _, item := range body.Items {
		byDoctoCC[item.DoctoCCID] = item
	}

	require.Len(t, byDoctoCC[201].Productos, 1, "venta A (docto_pv_id=4464) must get its line from the map")
	assert.Equal(t, 4464, byDoctoCC[201].Productos[0].DoctoPVID)

	require.NotNil(t, byDoctoCC[202].Productos)
	assert.Empty(t, byDoctoCC[202].Productos, "venta B (docto_pv_id=7777, no map entry) must get an empty slice")

	require.NotNil(t, byDoctoCC[203].Productos)
	assert.Empty(t, byDoctoCC[203].Productos, "venta C (nil docto_pv_id) must get an empty slice")
}
