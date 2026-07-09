//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ventasdomain "github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// fakeEstatusReader is a minimal outbound.ClienteEstatusReader for handler
// tests. It returns Estatus for every client when NotFound is false.
type fakeEstatusReader struct {
	Estatus  string
	NotFound bool
}

func (f *fakeEstatusReader) EstatusDeCliente(_ context.Context, _ int) (string, error) {
	if f.NotFound {
		return "", ventasdomain.ErrClienteNotFoundInMicrosip
	}
	return f.Estatus, nil
}

// buildHydratedVentaSinCliente constructs a domain.Venta via HydrateVenta
// with a nil ClienteID (no Microsip cliente link) and stores it in the
// fakeRepo. Returns the venta ID string. Mirrors
// buildHydratedVentaWithCliente but omits the cliente link entirely.
func buildHydratedVentaSinCliente(t *testing.T, repo *fakeRepo) string {
	t.Helper()
	id := uuid.New()
	by := uuid.New()
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)

	precio := decimal.NewFromInt(1000)
	montos := ventasdomain.HydrateMontoSnapshot(precio, precio, precio)
	clienteSnap := ventasdomain.HydrateClienteSnapshot(ventasdomain.NewClienteSnapshotParams{
		Nombre: ventasdomain.HydrateNombreCliente("Cliente Test"),
	})
	dir := ventasdomain.HydrateDireccion(ventasdomain.NewDireccionParams{
		Calle:     "Av. Reforma 100",
		Colonia:   "Centro",
		Poblacion: "Merida",
		Ciudad:    "Merida",
	})

	productoID := uuid.New()
	vendedorID := uuid.New()
	prod := ventasdomain.HydrateProducto(ventasdomain.HydrateProductoParams{
		ID:             productoID,
		ArticuloID:     42,
		Articulo:       "Refrigerador",
		Cantidad:       decimal.NewFromInt(1),
		Precios:        ventasdomain.HydrateMontoSnapshot(precio, precio, precio),
		AlmacenOrigen:  intPtr(1),
		AlmacenDestino: intPtr(2),
		CreatedAt:      now,
		UpdatedAt:      now,
		CreatedBy:      by,
		UpdatedBy:      by,
	})
	snap := ventasdomain.HydrateVendedorSnapshot(ventasdomain.NewVendedorSnapshotParams{
		UsuarioID: by,
		Email:     "vendedor@muebleriamsp.mx",
		Nombre:    "Vendedor Uno",
	})
	vendedor := ventasdomain.HydrateVendedor(ventasdomain.HydrateVendedorParams{
		ID:        vendedorID,
		Snapshot:  snap,
		CreatedAt: now,
		UpdatedAt: now,
		CreatedBy: by,
		UpdatedBy: by,
	})

	v := ventasdomain.HydrateVenta(ventasdomain.HydrateVentaParams{
		ID:             id,
		ClienteID:      nil,
		Cliente:        clienteSnap,
		Direccion:      dir,
		GPS:            ventasdomain.HydrateGPSCoords(0, 0),
		FechaVenta:     now,
		TipoVenta:      ventasdomain.TipoVentaContado,
		Montos:         montos,
		Estado:         ventasdomain.EstadoActive,
		Situacion:      ventasdomain.SituacionBorrador,
		Sincronizacion: ventasdomain.SincronizacionPendiente,
		Productos:      []*ventasdomain.Producto{prod},
		Vendedores:     []*ventasdomain.Vendedor{vendedor},
		CreatedAt:      now,
		UpdatedAt:      now,
		CreatedBy:      by,
		UpdatedBy:      by,
	})
	require.NoError(t, repo.Save(context.Background(), v))
	return id.String()
}

// TestObtenerVenta_EstatusClienteMicrosip verifies that GET /v2/ventas/{id}
// returns estatus_cliente_microsip populated correctly.
func TestObtenerVenta_EstatusClienteMicrosip(t *testing.T) {
	t.Parallel()

	t.Run("estatus_present_when_reader_returns_value", func(t *testing.T) {
		t.Parallel()
		svc, repo, _ := testService()
		svc = svc.WithEstatusReader(&fakeEstatusReader{Estatus: "V"})
		r := buildRouter(t, svc, fullPerms(uuid.New()))
		id := buildHydratedVentaWithCliente(t, repo, 47913, intPtr(21563))

		req := httptest.NewRequest(http.MethodGet, "/ventas/"+id, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.NotNil(t, got.EstatusClienteMicrosip)
		assert.Equal(t, "V", *got.EstatusClienteMicrosip)
	})

	t.Run("absent_when_no_estatusReader", func(t *testing.T) {
		t.Parallel()
		svc, repo, _ := testService()
		// no WithEstatusReader
		r := buildRouter(t, svc, fullPerms(uuid.New()))
		id := buildHydratedVentaWithCliente(t, repo, 47913, intPtr(21563))

		req := httptest.NewRequest(http.MethodGet, "/ventas/"+id, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Nil(t, got.EstatusClienteMicrosip)
		assert.NotContains(t, rec.Body.String(), "estatus_cliente_microsip")
	})

	t.Run("absent_when_cliente_not_found", func(t *testing.T) {
		t.Parallel()
		svc, repo, _ := testService()
		svc = svc.WithEstatusReader(&fakeEstatusReader{NotFound: true})
		r := buildRouter(t, svc, fullPerms(uuid.New()))
		id := buildHydratedVentaWithCliente(t, repo, 47913, intPtr(21563))

		req := httptest.NewRequest(http.MethodGet, "/ventas/"+id, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Nil(t, got.EstatusClienteMicrosip)
	})

	t.Run("absent_when_venta_has_no_cliente_id", func(t *testing.T) {
		t.Parallel()
		svc, repo, _ := testService()
		svc = svc.WithEstatusReader(&fakeEstatusReader{Estatus: "A"})
		r := buildRouter(t, svc, fullPerms(uuid.New()))
		id := buildHydratedVentaSinCliente(t, repo)

		req := httptest.NewRequest(http.MethodGet, "/ventas/"+id, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Nil(t, got.EstatusClienteMicrosip)
		assert.NotContains(t, rec.Body.String(), "estatus_cliente_microsip")
	})
}
