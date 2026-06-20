//nolint:misspell // clientes vocabulary is Spanish per project convention.
package clienteshttp_test

import (
	"context"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics"
	clientesapp "github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/infra/clienteshttp"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
)

// ─── Minimal stubs for OpenAPI test ──────────────────────────────────────────

type stubClock struct{}

func (stubClock) Now() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

type stubRepo struct{}

func (stubRepo) ObtenerCliente(_ context.Context, _ int) (*domain.Cliente, error) {
	return nil, domain.ErrClienteNotFound
}

func (stubRepo) ObtenerResumenFicha(_ context.Context, _ int, _ outbound.RangoFechas) (outbound.ResumenFicha, error) {
	return outbound.ResumenFicha{}, nil
}

func (stubRepo) ListarVentas(_ context.Context, _ int, _ outbound.ListParams) (outbound.Page[*domain.VentaCliente], error) {
	return outbound.Page[*domain.VentaCliente]{}, nil
}

func (stubRepo) ObtenerVentaDetalle(_ context.Context, _ int) (outbound.VentaDetalle, error) {
	return outbound.VentaDetalle{}, domain.ErrVentaNotFound
}

func (stubRepo) ListarDirectorioCompleto(_ context.Context, _ outbound.FiltroDirectorio) ([]outbound.DirectorioItem, error) {
	return nil, nil
}

func (stubRepo) ObtenerRitmoPagoData(_ context.Context, _ int, _ outbound.RangoFechas) (outbound.RitmoPagoData, error) {
	return outbound.RitmoPagoData{}, nil
}

func (stubRepo) ObtenerPagoDetalle(_ context.Context, _ int) (outbound.PagoDetalle, error) {
	return outbound.PagoDetalle{}, domain.ErrPagoNotFound
}

type stubAnalytics struct{}

func (stubAnalytics) ObtenerPulso(_ context.Context, _ int) (analytics.ClientePulsoContract, bool, error) {
	return analytics.ClientePulsoContract{}, false, nil
}

func (stubAnalytics) ObtenerPulsos(_ context.Context, _ []int) (map[int]analytics.ClientePulsoContract, error) {
	return map[int]analytics.ClientePulsoContract{}, nil
}

type stubDirectoryIndex struct{}

func (stubDirectoryIndex) Buscar(_ context.Context, _ outbound.DirectorioQuery) (outbound.DirectorioResultado, error) {
	return outbound.DirectorioResultado{Items: []outbound.DirectorioDoc{}, Total: 0}, nil
}

func (stubDirectoryIndex) Reconciliar(_ context.Context, _ []outbound.DirectorioDoc) error {
	return nil
}

func buildOpenAPITestService() *clientesapp.Service {
	return clientesapp.NewService(stubRepo{}, stubAnalytics{}, stubDirectoryIndex{}, stubClock{})
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestOpenAPI_PathsRegistered verifies that MountRouter registers the expected
// paths and operationIDs in the generated OpenAPI spec.
func TestOpenAPI_PathsRegistered(t *testing.T) {
	t.Parallel()

	r := chi.NewRouter()
	api := clienteshttp.MountRouter(r, buildOpenAPITestService())
	require.NotNil(t, api, "MountRouter must return a non-nil huma.API")

	spec := api.OpenAPI()
	require.NotNil(t, spec, "OpenAPI spec must be non-nil")
	require.NotNil(t, spec.Paths, "OpenAPI paths must be non-nil")

	type want struct {
		path        string
		method      string
		operationID string
	}

	cases := []want{
		{"/clientes", "GET", "listar-clientes"},
		{"/clientes/{id}", "GET", "obtener-cliente"},
		{"/clientes/{id}/ventas", "GET", "listar-ventas-cliente"},
		{"/clientes/{id}/ventas/{doctoPvId}", "GET", "obtener-venta-detalle"},
		{"/clientes/_search/refresh", "POST", "refrescar-busqueda-clientes"},
		{"/clientes/{id}/ritmo-pago", "GET", "obtener-ritmo-pago"},
	}

	for _, tc := range cases {
		t.Run(tc.operationID, func(t *testing.T) {
			t.Parallel()

			pathItem, ok := spec.Paths[tc.path]
			require.Truef(t, ok, "path %q must be registered in OpenAPI spec", tc.path)
			require.NotNil(t, pathItem, "path item for %q must not be nil", tc.path)

			var opID string
			switch tc.method {
			case "GET":
				require.NotNil(t, pathItem.Get, "GET operation must exist at %q", tc.path)
				opID = pathItem.Get.OperationID
			case "POST":
				require.NotNil(t, pathItem.Post, "POST operation must exist at %q", tc.path)
				opID = pathItem.Post.OperationID
			}

			assert.Equal(t, tc.operationID, opID,
				"operationID at %s %s must be %q", tc.method, tc.path, tc.operationID)
		})
	}
}
