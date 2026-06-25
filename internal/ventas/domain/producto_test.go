package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

func TestHydrateProducto(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	creator := uuid.New()
	comboID := uuid.New()
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	montos, err := domain.NewMontoSnapshot(decimal.NewFromInt(100), decimal.NewFromInt(80), decimal.NewFromInt(50))
	require.NoError(t, err)

	p := domain.HydrateProducto(domain.HydrateProductoParams{
		ID:         id,
		ArticuloID: 999,
		Articulo:   "Articulo",
		Cantidad:   decimal.NewFromInt(2),
		Precios:    montos,
		ComboID:    &comboID,
		CreatedAt:  now,
		UpdatedAt:  now,
		CreatedBy:  creator,
		UpdatedBy:  creator,
	})

	assert.Equal(t, id, p.ID())
	assert.Equal(t, 999, p.ArticuloID())
	assert.Equal(t, "Articulo", p.Articulo())
	assert.True(t, p.Cantidad().Equal(decimal.NewFromInt(2)))
	require.NotNil(t, p.ComboID())
	assert.Equal(t, comboID, *p.ComboID())
	assert.True(t, p.Precios().Equals(montos))
	a := p.Audit()
	assert.Equal(t, creator, a.CreatedBy())
}

func TestProducto_ViaCrearVenta_Validation(t *testing.T) {
	t.Parallel()
	one, two := 1, 2
	mk := func(p domain.CrearVentaProductoInput) error {
		params := validCrearVentaParams(t)
		params.Productos = []domain.CrearVentaProductoInput{p}
		_, err := domain.CrearVenta(params)
		return err
	}
	montos, _ := domain.NewMontoSnapshot(decimal.NewFromInt(100), decimal.NewFromInt(80), decimal.NewFromInt(50))

	// Valid producto.
	require.NoError(t, mk(domain.CrearVentaProductoInput{
		ID: uuid.New(), ArticuloID: 1, Articulo: "X", Cantidad: decimal.NewFromInt(1), Precios: montos,
		AlmacenOrigen: &one, AlmacenDestino: &two,
	}))
	// Empty articulo.
	require.Error(t, mk(domain.CrearVentaProductoInput{
		ID: uuid.New(), ArticuloID: 1, Articulo: "   ", Cantidad: decimal.NewFromInt(1), Precios: montos,
		AlmacenOrigen: &one, AlmacenDestino: &two,
	}))
	// Articulo too long.
	require.Error(t, mk(domain.CrearVentaProductoInput{
		ID: uuid.New(), ArticuloID: 1, Articulo: strings.Repeat("a", 201),
		Cantidad: decimal.NewFromInt(1), Precios: montos,
		AlmacenOrigen: &one, AlmacenDestino: &two,
	}))
	// Cantidad zero.
	require.Error(t, mk(domain.CrearVentaProductoInput{
		ID: uuid.New(), ArticuloID: 1, Articulo: "X", Cantidad: decimal.Zero, Precios: montos,
		AlmacenOrigen: &one, AlmacenDestino: &two,
	}))
	// Cantidad negative.
	require.Error(t, mk(domain.CrearVentaProductoInput{
		ID: uuid.New(), ArticuloID: 1, Articulo: "X", Cantidad: decimal.NewFromInt(-1), Precios: montos,
		AlmacenOrigen: &one, AlmacenDestino: &two,
	}))
	// Standalone producto missing almacen.
	require.Error(t, mk(domain.CrearVentaProductoInput{
		ID: uuid.New(), ArticuloID: 1, Articulo: "X", Cantidad: decimal.NewFromInt(1), Precios: montos,
	}))
}

func TestProducto_EnCombo_HeredaAlmacenes(t *testing.T) {
	t.Parallel()
	one, two := 1, 2
	montos, _ := domain.NewMontoSnapshot(decimal.NewFromInt(10), decimal.NewFromInt(8), decimal.NewFromInt(5))
	comboID := uuid.New()

	params := validCrearVentaParams(t)
	params.Combos = []domain.CrearVentaComboInput{{
		ID: comboID, Nombre: "Bundle", Precios: montos,
		Cantidad: decimal.NewFromInt(2), AlmacenOrigen: 1, AlmacenDestino: 2,
	}}
	params.Productos = []domain.CrearVentaProductoInput{{
		ID: uuid.New(), ArticuloID: 1, Articulo: "Item", Cantidad: decimal.NewFromInt(1), Precios: montos,
		ComboID: &comboID,
	}}
	v, err := domain.CrearVenta(params)
	require.NoError(t, err)
	assert.Equal(t, 1, v.ProductosCount())

	// Producto inside a combo with its own almacenes → rejected.
	bad := validCrearVentaParams(t)
	bad.Combos = []domain.CrearVentaComboInput{{
		ID: comboID, Nombre: "Bundle", Precios: montos,
		Cantidad: decimal.NewFromInt(1), AlmacenOrigen: 1, AlmacenDestino: 2,
	}}
	bad.Productos = []domain.CrearVentaProductoInput{{
		ID: uuid.New(), ArticuloID: 1, Articulo: "Item", Cantidad: decimal.NewFromInt(1), Precios: montos,
		ComboID: &comboID, AlmacenOrigen: &one, AlmacenDestino: &two,
	}}
	_, err = domain.CrearVenta(bad)
	require.Error(t, err)
}

// TestProducto_AlmacenesIgualesEsValido verifica que un producto con
// almacen_origen == almacen_destino YA NO se rechaza: el destino del cliente es
// vestigial (el destino real del traspaso es la config de exhibición). Reproduce
// el caso de la app que manda ambos = la camioneta del cobrador (p.ej. 19).
func TestProducto_AlmacenesIgualesEsValido(t *testing.T) {
	t.Parallel()
	mismo := 19
	params := validCrearVentaParams(t)
	params.Productos[0].AlmacenOrigen = &mismo
	params.Productos[0].AlmacenDestino = &mismo
	_, err := domain.CrearVenta(params)
	require.NoError(t, err)
}

func TestProducto_ReferenciaComboInvalida(t *testing.T) {
	t.Parallel()
	montos, _ := domain.NewMontoSnapshot(decimal.NewFromInt(10), decimal.NewFromInt(8), decimal.NewFromInt(5))
	ghost := uuid.New()

	params := validCrearVentaParams(t)
	params.Productos = []domain.CrearVentaProductoInput{{
		ID: uuid.New(), ArticuloID: 1, Articulo: "Item", Cantidad: decimal.NewFromInt(1), Precios: montos,
		ComboID: &ghost,
	}}
	_, err := domain.CrearVenta(params)
	require.Error(t, err)
}
