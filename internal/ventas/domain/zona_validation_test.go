package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// newVentaConZona builds a CONTADO venta whose dirección has the given
// zona_cliente_id. Pass nil to produce a venta without a zona.
func newVentaConZona(t *testing.T, zona *int) *domain.Venta {
	t.Helper()
	nom, err := domain.NewNombreCliente("Ana García")
	require.NoError(t, err)
	cliente, err := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: nom})
	require.NoError(t, err)
	dir, err := domain.NewDireccion(domain.NewDireccionParams{
		Calle:         "Hidalgo",
		Colonia:       "Centro",
		Poblacion:     "Tehuacán",
		Ciudad:        "Puebla",
		ZonaClienteID: zona,
	})
	require.NoError(t, err)
	montos, err := domain.NewMontoSnapshot(decimal.NewFromInt(100), decimal.NewFromInt(80), decimal.NewFromInt(50))
	require.NoError(t, err)
	one, two := 1, 2
	v, err := domain.CrearVenta(domain.CrearVentaParams{
		ID:         uuid.New(),
		Cliente:    cliente,
		Direccion:  dir,
		FechaVenta: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		TipoVenta:  domain.TipoVentaContado,
		Productos: []domain.CrearVentaProductoInput{{
			ID:             uuid.New(),
			ArticuloID:     1,
			Articulo:       "Mesa",
			Cantidad:       decimal.NewFromInt(1),
			Precios:        montos,
			AlmacenOrigen:  &one,
			AlmacenDestino: &two,
		}},
		Vendedores: []domain.CrearVentaVendedorInput{{
			ID:        uuid.New(),
			UsuarioID: uuid.New(),
			Email:     "v@muebleriamsp.mx",
			Nombre:    "Vendedor Test",
		}},
		CreatedBy: uuid.New(),
		Now:       time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	return v
}

func TestVenta_ValidarZonaCoincideMicrosip(t *testing.T) {
	t.Parallel()

	t.Run("match — returns nil", func(t *testing.T) {
		t.Parallel()
		zona := 21563
		v := newVentaConZona(t, &zona)
		err := v.ValidarZonaCoincideMicrosip(21563)
		require.NoError(t, err)
	})

	t.Run("mismatch — returns ErrVentaZonaNoCoincideCliente", func(t *testing.T) {
		t.Parallel()
		zona := 21563
		v := newVentaConZona(t, &zona)
		err := v.ValidarZonaCoincideMicrosip(99999)
		require.ErrorIs(t, err, domain.ErrVentaZonaNoCoincideCliente)
	})

	t.Run("nil zona on venta — returns nil (defensive)", func(t *testing.T) {
		t.Parallel()
		v := newVentaConZona(t, nil)
		err := v.ValidarZonaCoincideMicrosip(99999)
		assert.NoError(t, err, "when venta has no zona the check must be a no-op")
	})
}
