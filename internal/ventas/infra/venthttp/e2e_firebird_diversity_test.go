//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// seedE2EUsuarios seeds n independent usuario rows inside the active test tx
// and returns their IDs. Each call to seedE2EUsuario generates a fresh UUID so
// the rows are always distinct; composing multiple calls is safe.
func seedE2EUsuarios(ctx context.Context, t *testing.T, pool *firebird.Pool, n int) []uuid.UUID {
	t.Helper()
	out := make([]uuid.UUID, n)
	for i := range n {
		out[i] = seedE2EUsuario(ctx, t, pool)
	}
	return out
}

// TestE2E_Firebird_Diversity exercises the full POST → GET pipeline for a
// variety of data shapes: multi-combo/producto mixes, multi-vendedor, numeric
// boundary values, GPS extremes, and heavy-accent Unicode strings. Every
// subtest runs inside a single rollback-only transaction so no residue is left
// in the dev DB.
//
//nolint:paralleltest // serial — shares one tx with rollback at end
func TestE2E_Firebird_Diversity(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		postAndGet := func(t *testing.T, body venthttp.CrearVentaBody) venthttp.VentaDTO {
			t.Helper()
			req := crearVentaMultipartRequest(t, body)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			require.Equal(t, http.StatusCreated, rec.Code, "POST body=%s", rec.Body.String())

			req = httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
			rec = httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, "GET body=%s", rec.Body.String())

			var got venthttp.VentaDTO
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
			return got
		}

		t.Run("multi_combo_mixed_productos", func(t *testing.T) {
			body := validCreateBody()
			body.Vendedores[0].UsuarioID = usuarioID.String()

			comboA := uuid.NewString()
			comboB := uuid.NewString()
			comboC := uuid.NewString()
			body.Combos = []venthttp.ComboDTO{
				{ID: comboA, Nombre: "Combo A", PrecioAnual: "500", PrecioCorto: "450", PrecioContado: "400", Cantidad: "1", AlmacenOrigenID: 1, AlmacenDestinoID: 2},
				{ID: comboB, Nombre: "Combo B", PrecioAnual: "300", PrecioCorto: "270", PrecioContado: "240", Cantidad: "2", AlmacenOrigenID: 1, AlmacenDestinoID: 2},
				{ID: comboC, Nombre: "Combo C", PrecioAnual: "200", PrecioCorto: "180", PrecioContado: "160", Cantidad: "1", AlmacenOrigenID: 1, AlmacenDestinoID: 2},
			}
			body.Productos = []venthttp.ProductoDTO{
				{
					ID: uuid.NewString(), ArticuloID: 1, Articulo: "Stand-alone",
					Cantidad: "1", PrecioAnual: "100", PrecioCorto: "90", PrecioContado: "80",
					AlmacenOrigenID: intPtr(1), AlmacenDestinoID: intPtr(2),
				},
				{
					ID: uuid.NewString(), ArticuloID: 2, Articulo: "Parte de A",
					Cantidad: "1", PrecioAnual: "50", PrecioCorto: "45", PrecioContado: "40",
					ComboID: &comboA,
				},
				{
					ID: uuid.NewString(), ArticuloID: 3, Articulo: "Parte de B",
					Cantidad: "1", PrecioAnual: "60", PrecioCorto: "54", PrecioContado: "48",
					ComboID: &comboB,
				},
				{
					ID: uuid.NewString(), ArticuloID: 4, Articulo: "Parte de C",
					Cantidad: "1", PrecioAnual: "70", PrecioCorto: "63", PrecioContado: "56",
					ComboID: &comboC,
				},
			}

			got := postAndGet(t, body)
			assert.Len(t, got.Combos, 3, "three combos must round-trip")
			assert.Len(t, got.Productos, 4, "four productos must round-trip")

			byArticulo := map[int]venthttp.ProductoDTO{}
			for _, p := range got.Productos {
				byArticulo[p.ArticuloID] = p
			}
			assert.Nil(t, byArticulo[1].ComboID, "stand-alone producto must have nil combo_id")
			require.NotNil(t, byArticulo[2].ComboID)
			assert.Equal(t, comboA, *byArticulo[2].ComboID, "combo_id A must round-trip")
			require.NotNil(t, byArticulo[3].ComboID)
			assert.Equal(t, comboB, *byArticulo[3].ComboID, "combo_id B must round-trip")
			require.NotNil(t, byArticulo[4].ComboID)
			assert.Equal(t, comboC, *byArticulo[4].ComboID, "combo_id C must round-trip")
		})

		t.Run("multi_vendedor", func(t *testing.T) {
			userIDs := seedE2EUsuarios(ctx, t, pool, 3)

			body := validCreateBody()
			body.Vendedores = []venthttp.VendedorDTO{
				{ID: uuid.NewString(), UsuarioID: userIDs[0].String(), Email: "vend0@muebleriamsp.mx", Nombre: "Vendedor Cero"},
				{ID: uuid.NewString(), UsuarioID: userIDs[1].String(), Email: "vend1@muebleriamsp.mx", Nombre: "Vendedor Uno"},
				{ID: uuid.NewString(), UsuarioID: userIDs[2].String(), Email: "vend2@muebleriamsp.mx", Nombre: "Vendedor Dos"},
			}

			got := postAndGet(t, body)
			assert.Len(t, got.Vendedores, 3, "three vendedores must round-trip")

			byUsuario := map[string]bool{}
			for _, v := range got.Vendedores {
				byUsuario[v.UsuarioID] = true
			}
			assert.True(t, byUsuario[userIDs[0].String()], "usuario 0 must be present")
			assert.True(t, byUsuario[userIDs[1].String()], "usuario 1 must be present")
			assert.True(t, byUsuario[userIDs[2].String()], "usuario 2 must be present")
		})

		t.Run("montos_numeric_max", func(t *testing.T) {
			body := validCreateBody()
			body.Vendedores[0].UsuarioID = usuarioID.String()
			body.Montos = venthttp.MontosDTO{
				Anual:      "999999999999.99",
				CortoPlazo: "999999999998.99",
				Contado:    "999999999997.99",
			}
			body.Productos[0].PrecioAnual = "999999999999.99"
			body.Productos[0].PrecioCorto = "999999999998.99"
			body.Productos[0].PrecioContado = "999999999997.99"

			got := postAndGet(t, body)
			assert.Equal(t, "999999999999.99", got.Montos.Anual, "anual monto must round-trip")
			assert.Equal(t, "999999999998.99", got.Montos.CortoPlazo, "corto_plazo monto must round-trip")
			assert.Equal(t, "999999999997.99", got.Montos.Contado, "contado monto must round-trip")
		})

		t.Run("cantidad_numeric_max", func(t *testing.T) {
			body := validCreateBody()
			body.Vendedores[0].UsuarioID = usuarioID.String()
			body.Productos[0].Cantidad = "999999.9999"

			got := postAndGet(t, body)
			require.Len(t, got.Productos, 1)
			assert.Equal(t, "999999.9999", got.Productos[0].Cantidad, "max NUMERIC(10,4) cantidad must round-trip")
		})

		t.Run("gps_polo_norte", func(t *testing.T) {
			body := validCreateBody()
			body.Vendedores[0].UsuarioID = usuarioID.String()
			body.GPS = venthttp.GPSDTO{Latitud: 89.9999, Longitud: 0.0}

			got := postAndGet(t, body)
			assert.InDelta(t, 89.9999, got.GPS.Latitud, 0.00001, "norte latitud must round-trip")
			assert.InDelta(t, 0.0, got.GPS.Longitud, 0.00001, "zero longitud must round-trip")
		})

		t.Run("gps_sur_oeste", func(t *testing.T) {
			body := validCreateBody()
			body.Vendedores[0].UsuarioID = usuarioID.String()
			body.GPS = venthttp.GPSDTO{Latitud: -33.4489, Longitud: -70.6693}

			got := postAndGet(t, body)
			assert.InDelta(t, -33.4489, got.GPS.Latitud, 0.00001, "sur latitud must round-trip")
			assert.InDelta(t, -70.6693, got.GPS.Longitud, 0.00001, "oeste longitud must round-trip")
		})

		t.Run("gps_punto_cero", func(t *testing.T) {
			body := validCreateBody()
			body.Vendedores[0].UsuarioID = usuarioID.String()
			body.GPS = venthttp.GPSDTO{Latitud: 0.0, Longitud: 0.0}

			got := postAndGet(t, body)
			assert.InDelta(t, 0.0, got.GPS.Latitud, 0.00001, "zero latitud must round-trip")
			assert.InDelta(t, 0.0, got.GPS.Longitud, 0.00001, "zero longitud must round-trip")
		})

		t.Run("em_dash_en_nota", func(t *testing.T) {
			body := validCreateBody()
			body.Vendedores[0].UsuarioID = usuarioID.String()
			nota := "Cliente preferente — entrega matutina."
			body.Nota = &nota

			got := postAndGet(t, body)
			require.NotNil(t, got.Nota, "nota must not be nil")
			assert.Equal(t, nota, *got.Nota, "em-dash nota must round-trip without character substitution")
		})

		t.Run("acentos_pesados", func(t *testing.T) {
			body := validCreateBody()
			body.Vendedores[0].UsuarioID = usuarioID.String()
			body.Cliente = venthttp.ClienteSnapshotDTO{
				Nombre: "María Núñez Ñañez",
				Aval:   strPtr("Ángel Jiménez Ávila"),
			}
			body.Direccion = venthttp.DireccionDTO{
				Calle:     "Calle del Cañón",
				Colonia:   "Colonia Ñoños",
				Poblacion: "Población Güera",
				Ciudad:    "Tláhuac",
			}

			got := postAndGet(t, body)
			assert.Equal(t, "María Núñez Ñañez", got.Cliente.Nombre, "heavy-accent nombre must round-trip")
			require.NotNil(t, got.Cliente.Aval)
			assert.Equal(t, "Ángel Jiménez Ávila", *got.Cliente.Aval, "aval heavy-accent must round-trip")
			assert.Equal(t, "Calle del Cañón", got.Direccion.Calle, "calle with heavy accents must round-trip")
			assert.Equal(t, "Colonia Ñoños", got.Direccion.Colonia, "colonia with ñ must round-trip")
			assert.Equal(t, "Población Güera", got.Direccion.Poblacion, "poblacion with heavy accents must round-trip")
			assert.Equal(t, "Tláhuac", got.Direccion.Ciudad, "ciudad with accent must round-trip")
		})
	})
}
