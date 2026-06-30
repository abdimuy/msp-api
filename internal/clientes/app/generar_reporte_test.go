//nolint:misspell // Spanish domain vocabulary per project convention.
package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clientesapp "github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// ─── multiPageRepo ────────────────────────────────────────────────────────────

// multiPageRepo wraps fakeClientesRepo and overrides ListarVentas to serve a
// sequence of pages, so tests can verify that GenerarReporteCliente iterates
// until NextCursor is empty.
type multiPageRepo struct {
	fakeClientesRepo
	pages []outbound.Page[*domain.VentaCliente]
	idx   int
}

func (m *multiPageRepo) ListarVentas(_ context.Context, _ int, _ outbound.ListParams) (outbound.Page[*domain.VentaCliente], error) {
	if m.idx >= len(m.pages) {
		return outbound.Page[*domain.VentaCliente]{}, nil
	}
	p := m.pages[m.idx]
	m.idx++
	return p, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func newReportePago(importe string) *domain.Pago {
	v, _ := decimal.NewFromString(importe)
	return domain.HydratePago(domain.HydratePagoParams{
		Fecha:    time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		Importe:  v,
		Concepto: "Cobranza en ruta",
		Cobrador: "Cobrador Test",
	})
}

// ─── TestGenerarReporteCliente_EstructuraCorrecta ─────────────────────────────

// TestGenerarReporteCliente_EstructuraCorrecta verifies that the returned
// ReporteCliente has correct identity, resumen, and per-venta payment data.
func TestGenerarReporteCliente_EstructuraCorrecta(t *testing.T) {
	t.Parallel()

	v := newVentaCliente(101, 42)
	pago := newReportePago("1500.00")

	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{
			42: domain.HydrateCliente(domain.HydrateClienteParams{
				ClienteID:      42,
				Nombre:         "García López Ramón",
				ZonaNombre:     "NORTE",
				CobradorNombre: "Martínez Test",
				Telefono:       "3312345678",
				Direccion: domain.HydrateDireccion(domain.HydrateDireccionParams{
					Calle:     "Av. Juárez 123",
					Colonia:   "Centro",
					Poblacion: "Guadalajara",
				}),
			}),
		},
		resumen: outbound.ResumenFicha{
			TotalComprado: decimal.NewFromInt(10000),
			NumVentas:     1,
		},
		ventasPage: outbound.Page[*domain.VentaCliente]{
			Items:      []*domain.VentaCliente{v},
			NextCursor: "",
		},
		detalleByID: map[int]outbound.VentaDetalle{
			101: {
				Venta: v,
				Pagos: []*domain.Pago{pago},
			},
		},
	}

	svc := clientesapp.NewService(repo, nil, nil, nil)
	rep, err := svc.GenerarReporteCliente(context.Background(), 42, nil)
	require.NoError(t, err)

	assert.Equal(t, 42, rep.Cliente.ID)
	assert.Equal(t, "García López Ramón", rep.Cliente.Nombre)
	assert.Equal(t, "NORTE", rep.Cliente.Zona)
	assert.Equal(t, "Martínez Test", rep.Cliente.Cobrador)
	assert.Equal(t, "3312345678", rep.Cliente.Telefono)
	assert.Equal(t, "Av. Juárez 123, Centro, Guadalajara", rep.Cliente.Direccion)
	assert.Equal(t, int64(10000), rep.Resumen.TotalComprado.IntPart())
	require.Len(t, rep.Ventas, 1)
	assert.Equal(t, 101, rep.Ventas[0].DoctoPvID)
	require.Len(t, rep.Ventas[0].Pagos, 1)
	assert.Equal(t, "Cobranza en ruta", rep.Ventas[0].Pagos[0].Concepto)
}

// ─── TestGenerarReporteCliente_FiltroPorVentaIDs ─────────────────────────────

// TestGenerarReporteCliente_FiltroPorVentaIDs verifies the ventaIDs filter:
// when non-empty, only the requested ventas are included, in order.
func TestGenerarReporteCliente_FiltroPorVentaIDs(t *testing.T) {
	t.Parallel()

	v1 := newVentaCliente(101, 42)
	v2 := newVentaCliente(102, 42)

	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{
			42: newCliente(42, "Test Cliente"),
		},
		ventasPage: outbound.Page[*domain.VentaCliente]{
			Items:      []*domain.VentaCliente{v1, v2},
			NextCursor: "",
		},
		detalleByID: map[int]outbound.VentaDetalle{
			101: {Venta: v1, Pagos: nil},
			102: {Venta: v2, Pagos: nil},
		},
	}

	svc := clientesapp.NewService(repo, nil, nil, nil)

	// Filter to only venta 101.
	rep, err := svc.GenerarReporteCliente(context.Background(), 42, []int{101})
	require.NoError(t, err)
	require.Len(t, rep.Ventas, 1)
	assert.Equal(t, 101, rep.Ventas[0].DoctoPvID)

	// No filter → all ventas.
	repo.ventasPage = outbound.Page[*domain.VentaCliente]{
		Items:      []*domain.VentaCliente{v1, v2},
		NextCursor: "",
	}
	rep2, err := svc.GenerarReporteCliente(context.Background(), 42, nil)
	require.NoError(t, err)
	assert.Len(t, rep2.Ventas, 2)
}

// ─── TestGenerarReporteCliente_Liquidada ─────────────────────────────────────

// TestGenerarReporteCliente_Liquidada verifies that Liquidada is true when
// SaldoVenta is zero, and false when it is positive.
func TestGenerarReporteCliente_Liquidada(t *testing.T) {
	t.Parallel()

	vLiq := domain.HydrateVentaCliente(domain.HydrateVentaClienteParams{
		DoctoPVID:  201,
		ClienteID:  42,
		Fecha:      fixedTime,
		SaldoVenta: decimal.Zero, // liquidada
	})
	vDebe := domain.HydrateVentaCliente(domain.HydrateVentaClienteParams{
		DoctoPVID:  202,
		ClienteID:  42,
		Fecha:      fixedTime,
		SaldoVenta: decimal.NewFromInt(5000), // debe
	})

	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{
			42: newCliente(42, "Test"),
		},
		ventasPage: outbound.Page[*domain.VentaCliente]{
			Items:      []*domain.VentaCliente{vLiq, vDebe},
			NextCursor: "",
		},
		detalleByID: map[int]outbound.VentaDetalle{
			201: {Venta: vLiq},
			202: {Venta: vDebe},
		},
	}

	svc := clientesapp.NewService(repo, nil, nil, nil)
	rep, err := svc.GenerarReporteCliente(context.Background(), 42, nil)
	require.NoError(t, err)
	require.Len(t, rep.Ventas, 2)

	assert.True(t, rep.Ventas[0].Liquidada, "saldo=0 debe ser Liquidada=true")
	assert.False(t, rep.Ventas[1].Liquidada, "saldo>0 debe ser Liquidada=false")

	assert.Equal(t, 2, rep.TotalVentas, "TotalVentas debe contar todas las ventas")
	assert.Equal(t, 1, rep.VentasLiquidadas, "VentasLiquidadas: solo la de saldo=0")
	assert.Equal(t, 1, rep.VentasActivas, "VentasActivas: solo la de saldo>0")
}

// ─── TestGenerarReporteCliente_ClienteNoEncontrado ───────────────────────────

// TestGenerarReporteCliente_ClienteNoEncontrado verifies that
// ErrClienteNotFound propagates as a KindNotFound apperror.
func TestGenerarReporteCliente_ClienteNoEncontrado(t *testing.T) {
	t.Parallel()

	// Empty map → ObtenerCliente returns domain.ErrClienteNotFound.
	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{},
	}

	svc := clientesapp.NewService(repo, nil, nil, nil)
	_, err := svc.GenerarReporteCliente(context.Background(), 9999, nil)
	require.Error(t, err)

	ae, ok := apperror.As(err)
	require.True(t, ok, "error must be an *apperror.Error")
	assert.Equal(t, apperror.KindNotFound, ae.Kind)
}

// ─── TestGenerarReporteCliente_Paginacion ────────────────────────────────────

// TestGenerarReporteCliente_Paginacion verifies that ventas are accumulated
// across multiple pages by iterating until NextCursor is empty.
func TestGenerarReporteCliente_Paginacion(t *testing.T) {
	t.Parallel()

	v1 := newVentaCliente(301, 42)
	v2 := newVentaCliente(302, 42)

	base := fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{
			42: newCliente(42, "Test"),
		},
		detalleByID: map[int]outbound.VentaDetalle{
			301: {Venta: v1},
			302: {Venta: v2},
		},
	}

	// multiPageRepo returns two pages: first with v1+cursor, second with v2+empty.
	repo := &multiPageRepo{
		fakeClientesRepo: base,
		pages: []outbound.Page[*domain.VentaCliente]{
			{Items: []*domain.VentaCliente{v1}, NextCursor: "page2"},
			{Items: []*domain.VentaCliente{v2}, NextCursor: ""},
		},
	}

	svc := clientesapp.NewService(repo, nil, nil, nil)
	rep, err := svc.GenerarReporteCliente(context.Background(), 42, nil)
	require.NoError(t, err)
	assert.Len(t, rep.Ventas, 2, "debe acumular ventas de ambas páginas")
	assert.Equal(t, 301, rep.Ventas[0].DoctoPvID)
	assert.Equal(t, 302, rep.Ventas[1].DoctoPvID)
}
