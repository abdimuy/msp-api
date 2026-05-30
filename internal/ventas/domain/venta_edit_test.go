//nolint:misspell // ventas vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

func validHeaderParams(p domain.CrearVentaParams) domain.ActualizarHeaderParams {
	return domain.ActualizarHeaderParams{
		Direccion:  p.Direccion,
		GPS:        p.GPS,
		FechaVenta: p.FechaVenta.Add(time.Hour),
		Montos:     p.Montos,
		Nota:       p.Nota,
		By:         uuid.New(),
		Now:        time.Now(),
	}
}

func TestVenta_ActualizarHeader_Happy(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	v, err := domain.CrearVenta(p)
	require.NoError(t, err)
	hp := validHeaderParams(p)
	require.NoError(t, v.ActualizarHeader(hp))
	assert.Equal(t, hp.FechaVenta, v.FechaVenta())
	evs := v.PendingEvents()
	require.Len(t, evs, 2)
	assert.Equal(t, "venta.header_actualizado", evs[1].EventType())
}

func TestVenta_ActualizarHeader_RejectsIfCancelada(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	require.NoError(t, v.Cancelar("razon", uuid.New(), time.Now()))
	err := v.ActualizarHeader(validHeaderParams(validCrearVentaParams(t)))
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "venta_no_editable", ae.Code)
}

func TestVenta_ActualizarCliente_Happy(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	newNom, _ := domain.NewNombreCliente("Cliente Nuevo")
	newSnap, _ := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: newNom})
	cid := 12345
	require.NoError(t, v.ActualizarCliente(domain.ActualizarClienteParams{
		ClienteID: &cid, Cliente: newSnap, By: uuid.New(), Now: time.Now(),
	}))
	assert.Equal(t, "Cliente Nuevo", v.Cliente().Nombre().Value())
	require.NotNil(t, v.ClienteID())
	assert.Equal(t, 12345, *v.ClienteID())
	evs := v.PendingEvents()
	require.Len(t, evs, 2)
	assert.Equal(t, "venta.cliente_actualizado", evs[1].EventType())
}

func TestVenta_ReemplazarProductos_Happy(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	montos, _ := domain.NewMontoSnapshot(decimal.NewFromInt(10), decimal.NewFromInt(8), decimal.NewFromInt(5))
	one, two := 1, 2
	require.NoError(t, v.ReemplazarProductos(domain.ReemplazarProductosParams{
		Productos: []domain.CrearVentaProductoInput{{
			ID: uuid.New(), ArticuloID: 99, Articulo: "Nuevo",
			Cantidad: decimal.NewFromInt(3), Precios: montos,
			AlmacenOrigen: &one, AlmacenDestino: &two,
		}},
		By: uuid.New(), Now: time.Now(),
	}))
	assert.Equal(t, 1, v.ProductosCount())
}

func TestVenta_ReemplazarProductos_EmptyRejected(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	err := v.ReemplazarProductos(domain.ReemplazarProductosParams{
		Productos: nil, By: uuid.New(), Now: time.Now(),
	})
	require.Error(t, err)
}

func TestVenta_ReemplazarCombos_Happy(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	montos, _ := domain.NewMontoSnapshot(decimal.NewFromInt(20), decimal.NewFromInt(18), decimal.NewFromInt(15))
	require.NoError(t, v.ReemplazarCombos(domain.ReemplazarCombosParams{
		Combos: []domain.CrearVentaComboInput{{
			ID: uuid.New(), Nombre: "C", Precios: montos,
			Cantidad: decimal.NewFromInt(1), AlmacenOrigen: 1, AlmacenDestino: 2,
		}},
		By: uuid.New(), Now: time.Now(),
	}))
	assert.Equal(t, 1, v.CombosCount())
}

func TestVenta_ReemplazarVendedores_Happy(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	require.NoError(t, v.ReemplazarVendedores(domain.ReemplazarVendedoresParams{
		Vendedores: []domain.CrearVentaVendedorInput{{
			ID: uuid.New(), UsuarioID: uuid.New(),
			Email: "x@y.com", Nombre: "X",
		}},
		By: uuid.New(), Now: time.Now(),
	}))
	assert.Equal(t, 1, v.VendedoresCount())
}

func TestVenta_Cancelar_TransitionsToStatusCancelada(t *testing.T) {
	t.Parallel()
	v := buildValidVenta(t)
	assert.Equal(t, domain.SituacionBorrador, v.Situacion())
	require.NoError(t, v.Cancelar("razon", uuid.New(), time.Now()))
	assert.Equal(t, domain.SituacionCancelada, v.Situacion())
}
