//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/imageprocessor"
	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// buildE2EServiceWithUsuarios is buildE2EService plus the real
// Firebird-backed VendedorUsuarioExistenceChecker, used by tests that
// exercise the vendedor_usuario_no_encontrado validation path.
func buildE2EServiceWithUsuarios(pool *firebird.Pool) *ventasapp.Service {
	repo := ventfb.NewVentaRepo(pool)
	usuarios := ventfb.NewUsuarioExistenceRepo(pool)
	store := newFakeStorage()
	clock := fixedClock{T: e2eFixedTime()}
	return ventasapp.NewService(repo, nil, usuarios, store, clock, noopOutbox{}, imageprocessor.NoOpProcessor{}, nil, nil, nil, nil)
}

// TestE2E_Firebird_Rejection verifies that the HTTP layer returns the correct
// 4xx status and apperror code for each boundary/rejection scenario. All
// subtests share one rollback-only Firebird transaction seeded with a single
// usuario.
//
//nolint:paralleltest // serial — shares one tx with rollback at end
func TestE2E_Firebird_Rejection(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EServiceWithUsuarios(pool)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		post := func(t *testing.T, body venthttp.CrearVentaBody) *httptest.ResponseRecorder {
			t.Helper()
			req := crearVentaMultipartRequest(t, body)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			return rec
		}

		t.Run("monto_contado_negativo_422", func(t *testing.T) {
			body := validCreateBody()
			body.Vendedores[0].UsuarioID = usuarioID.String()
			// Header montos are derived from line items and ignored, so a
			// negative price must be rejected at the producto precio level.
			body.Productos[0].PrecioContado = "-100.00"

			rec := post(t, body)
			require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "negative contado must be 422: %s", rec.Body.String())
			assert.Contains(t, rec.Body.String(), "monto_negativo", "error code must be monto_negativo")
		})

		t.Run("producto_cantidad_zero_422", func(t *testing.T) {
			body := validCreateBody()
			body.Vendedores[0].UsuarioID = usuarioID.String()
			body.Productos[0].Cantidad = "0.0000"

			rec := post(t, body)
			require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "zero cantidad must be 422: %s", rec.Body.String())
			assert.Contains(t, rec.Body.String(), "cantidad_no_positiva", "error code must be cantidad_no_positiva")
		})

		t.Run("nota_501_chars_422", func(t *testing.T) {
			body := validCreateBody()
			body.Vendedores[0].UsuarioID = usuarioID.String()
			nota := strings.Repeat("x", 501)
			body.Nota = &nota

			rec := post(t, body)
			assert.NotEqual(t, http.StatusCreated, rec.Code, "501-char nota must not be 201: %s", rec.Body.String())
			// Huma's maxLength validation may return 422 with an "errors" shape
			// rather than our domain 422; either way the body must mention "nota".
			assert.Contains(t, rec.Body.String(), "nota", "response must identify the nota field")
		})

		t.Run("vendedor_usuario_id_inexistente_422", func(t *testing.T) {
			body := validCreateBody()
			body.Vendedores[0].UsuarioID = uuid.NewString() // random — never seeded

			rec := post(t, body)
			require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "unknown usuario_id must be 422: %s", rec.Body.String())
			assert.Contains(t, rec.Body.String(), "vendedor_usuario_no_encontrado", "error code must be vendedor_usuario_no_encontrado")
		})

		t.Run("cliente_nombre_vacio_422", func(t *testing.T) {
			body := validCreateBody()
			body.Vendedores[0].UsuarioID = usuarioID.String()
			body.Cliente.Nombre = ""

			rec := post(t, body)
			require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "empty nombre must be 422: %s", rec.Body.String())
			assert.Contains(t, rec.Body.String(), "nombre_cliente_required", "error code must be nombre_cliente_required")
		})

		t.Run("nota_500_chars_201_boundary", func(t *testing.T) {
			body := validCreateBody()
			body.Vendedores[0].UsuarioID = usuarioID.String()
			nota := strings.Repeat("y", 500)
			body.Nota = &nota

			rec := post(t, body)
			require.Equal(t, http.StatusCreated, rec.Code, "500-char nota must be accepted: %s", rec.Body.String())

			req := httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
			recGet := httptest.NewRecorder()
			r.ServeHTTP(recGet, req)
			require.Equal(t, http.StatusOK, recGet.Code)
			// Nota is folded to ALL CAPS by the domain (Microsip convention).
			assert.Contains(t, recGet.Body.String(), strings.ToUpper(nota), "500-char nota must round-trip up to the ALL-CAPS fold")
		})

		t.Run("telefono_invalido_422", func(t *testing.T) {
			body := validCreateBody()
			body.Vendedores[0].UsuarioID = usuarioID.String()
			tel := "12345" // no reduce a 10 dígitos MX → rechazado por el VO
			body.Cliente.Telefono = &tel

			rec := post(t, body)
			require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "telefono inválido debe ser 422: %s", rec.Body.String())
		})
	})
}
