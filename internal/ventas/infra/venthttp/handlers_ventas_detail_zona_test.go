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

// fakeZonaReader is a minimal outbound.ClienteZonaReader for handler tests.
// It returns ZonaID for every client when NotFound is false and ZonaNil is false.
type fakeZonaReader struct {
	ZonaID   int
	ZonaNil  bool
	NotFound bool
}

func (f *fakeZonaReader) ZonaDeCliente(_ context.Context, _ int) (*int, error) {
	if f.NotFound {
		return nil, ventasdomain.ErrClienteNotFoundInMicrosip
	}
	if f.ZonaNil {
		return nil, nil
	}
	z := f.ZonaID
	return &z, nil
}

// buildHydratedVentaWithCliente constructs a domain.Venta via HydrateVenta
// with a given clienteID and zonaClienteID, then stores it in the fakeRepo.
// Returns the venta ID string.
func buildHydratedVentaWithCliente(t *testing.T, repo *fakeRepo, clienteID int, zonaClienteID *int) string {
	t.Helper()
	id := uuid.New()
	by := uuid.New()
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	cid := clienteID

	precio := decimal.NewFromInt(1000)
	montos := ventasdomain.HydrateMontoSnapshot(precio, precio, precio)
	clienteSnap := ventasdomain.HydrateClienteSnapshot(ventasdomain.NewClienteSnapshotParams{
		Nombre: ventasdomain.HydrateNombreCliente("Cliente Test"),
	})
	dir := ventasdomain.HydrateDireccion(ventasdomain.NewDireccionParams{
		Calle:         "Av. Reforma 100",
		Colonia:       "Centro",
		Poblacion:     "Merida",
		Ciudad:        "Merida",
		ZonaClienteID: zonaClienteID,
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
		ClienteID:      &cid,
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

// TestObtenerVenta_ZonaMismatch verifies that GET /v2/ventas/{id} returns
// zona_mismatch and zona_cliente_microsip_id populated correctly.
func TestObtenerVenta_ZonaMismatch(t *testing.T) {
	t.Parallel()

	const ventaZona = 21563
	const microsipZona = 99999

	ventaZonaPtr := intPtr(ventaZona)

	t.Run("mismatch_true_when_zonas_differ", func(t *testing.T) {
		t.Parallel()
		svc, repo, _ := testService()
		svc = svc.WithZonaReader(&fakeZonaReader{ZonaID: microsipZona})
		r := buildRouter(t, svc, fullPerms(uuid.New()))
		id := buildHydratedVentaWithCliente(t, repo, 47913, ventaZonaPtr)

		req := httptest.NewRequest(http.MethodGet, "/ventas/"+id, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.True(t, got.ZonaMismatch, "zona_mismatch must be true when zonas differ")
		require.NotNil(t, got.ZonaClienteMicrosipID)
		assert.Equal(t, microsipZona, *got.ZonaClienteMicrosipID)
	})

	t.Run("mismatch_false_when_zonas_match", func(t *testing.T) {
		t.Parallel()
		svc, repo, _ := testService()
		svc = svc.WithZonaReader(&fakeZonaReader{ZonaID: ventaZona})
		r := buildRouter(t, svc, fullPerms(uuid.New()))
		id := buildHydratedVentaWithCliente(t, repo, 47913, ventaZonaPtr)

		req := httptest.NewRequest(http.MethodGet, "/ventas/"+id, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.False(t, got.ZonaMismatch, "zona_mismatch must be false when zonas match")
		require.NotNil(t, got.ZonaClienteMicrosipID)
		assert.Equal(t, ventaZona, *got.ZonaClienteMicrosipID)
	})

	t.Run("mismatch_false_when_no_zonaReader", func(t *testing.T) {
		t.Parallel()
		svc, repo, _ := testService()
		// no WithZonaReader
		r := buildRouter(t, svc, fullPerms(uuid.New()))
		id := buildHydratedVentaWithCliente(t, repo, 47913, ventaZonaPtr)

		req := httptest.NewRequest(http.MethodGet, "/ventas/"+id, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.False(t, got.ZonaMismatch)
		assert.Nil(t, got.ZonaClienteMicrosipID)
		assert.NotContains(t, rec.Body.String(), "zona_cliente_microsip_id")
	})

	t.Run("mismatch_false_when_cliente_not_found", func(t *testing.T) {
		t.Parallel()
		svc, repo, _ := testService()
		svc = svc.WithZonaReader(&fakeZonaReader{NotFound: true})
		r := buildRouter(t, svc, fullPerms(uuid.New()))
		id := buildHydratedVentaWithCliente(t, repo, 47913, ventaZonaPtr)

		req := httptest.NewRequest(http.MethodGet, "/ventas/"+id, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.False(t, got.ZonaMismatch)
		assert.Nil(t, got.ZonaClienteMicrosipID)
	})
}
