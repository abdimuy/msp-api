//nolint:misspell // clientes vocabulary is Spanish per project convention.
package clienteshttp_test

import (
	"net/http"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	clientesapp "github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
)

// ─── TestReporte_HappyPath_200 ────────────────────────────────────────────────

// TestReporte_HappyPath_200 verifies that a well-formed request returns 200,
// the Content-Type is application/pdf, the Content-Disposition names the file
// correctly, and the body starts with the PDF magic bytes %PDF.
func TestReporte_HappyPath_200(t *testing.T) {
	t.Parallel()

	c := newCliente(42)
	v := newVenta(100, 42)
	repo := &fakeRepo{
		cliente: c,
		resumen: outbound.ResumenFicha{
			TotalComprado: decimal.NewFromInt(10000),
			NumVentas:     1,
		},
		ventasPage: outbound.Page[*domain.VentaCliente]{
			Items:      []*domain.VentaCliente{v},
			NextCursor: "",
		},
		detalleByID: map[int]outbound.VentaDetalle{
			100: {Venta: v, Pagos: nil},
		},
	}

	svc := clientesapp.NewService(repo, &fakeAnalytics{}, noopDirectoryIndex{}, testClock{})
	cu := userWith(auth.PermClientesLeer)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/clientes/42/reporte", nil)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	assert.Equal(t, "application/pdf", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "reporte-cliente-42.pdf")
	require.GreaterOrEqual(t, rec.Body.Len(), 4)
	assert.Equal(t, "%PDF", rec.Body.String()[:4])
}

// ─── TestReporte_NotFound_404 ─────────────────────────────────────────────────

// TestReporte_NotFound_404 verifies that a request for a non-existent client
// returns HTTP 404.
func TestReporte_NotFound_404(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{clienteErr: domain.ErrClienteNotFound}
	svc := clientesapp.NewService(repo, &fakeAnalytics{}, noopDirectoryIndex{}, testClock{})
	cu := userWith(auth.PermClientesLeer)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/clientes/9999/reporte", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// ─── TestReporte_InvalidVenta_400 ────────────────────────────────────────────

// TestReporte_InvalidVenta_400 verifies that a non-numeric ?venta= parameter
// returns HTTP 400 before any repo calls are made.
func TestReporte_InvalidVenta_400(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{cliente: newCliente(42)}
	svc := clientesapp.NewService(repo, &fakeAnalytics{}, noopDirectoryIndex{}, testClock{})
	cu := userWith(auth.PermClientesLeer)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/clientes/42/reporte?venta=notnumeric", nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
