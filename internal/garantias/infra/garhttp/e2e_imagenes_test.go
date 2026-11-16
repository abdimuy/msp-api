package garhttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/infra/garfb"
	"github.com/abdimuy/msp-api/internal/garantias/infra/garhttp"
	"github.com/abdimuy/msp-api/internal/garantias/infra/storage"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

//nolint:paralleltest // Usa una transaccion Firebird compartida durante el escenario E2E.
func TestE2EImagenes_SubirDescargarYReintentar(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		ent := nuevoEntornoE2EHTTP(t, pool)

		imagenes := garfb.NewImagenRepo(pool)
		almacen, err := storage.NewFilesystemProvider(ent.storage)
		require.NoError(t, err)

		identity := &fakeHTTPIdentity{
			usuario: outbound.Usuario{
				ID:     "usuario-1",
				Nombre: "Ruben",
			},
			permitido: true,
		}

		consultas := app.NewConsultas(app.ConsultasDeps{
			Garantias: ent.garantias,
			Bandeja:   garfb.NewBandejaRepo(pool),
			Eventos:   ent.eventos,
			Imagenes:  imagenes,
			Storage:   almacen,
			Identity:  identity,
		})

		servicio := app.NewService(app.Deps{
			GarantiaRepo:   ent.garantias,
			EventoRepo:     ent.eventos,
			ImagenRepo:     imagenes,
			FolioGenerator: garfb.NewFolioGenerator(pool),
			TxRunner:       txRunnerHTTP{pool: pool},
			Identity:       identity,
			Clock:          ent.clk,
			Storage:        almacen,
		})

		r := chi.NewRouter()
		garhttp.MountRouter(r, garhttp.Deps{
			Consultas: consultas,
			Comandos:  servicio,
		})
		ent.router = r

		// La garantía y su evento se crean dentro de la transacción.
		abierta := ent.jsonOK(
			ctx, t,
			"/garantias",
			uuid.NewString(),
			ent.cuerpo(map[string]any{
				"origen":      "piso",
				"description": "Garantía de prueba de imágenes",
			}),
		)

		garantiaID := uuid.MustParse(abierta.ID)
		eventos, err := ent.eventos.ListarPorGarantia(ctx, garantiaID)
		require.NoError(t, err)
		require.NotEmpty(t, eventos)

		eventoID := eventos[0].ID()
		clave := uuid.NewString()
		contenido := adjuntoHTTPPNG()

		ruta := "/garantias/" + garantiaID.String() +
			"/eventos/" + eventoID.String() + "/imagenes"

		subir := func() (int, map[string]any) {
			t.Helper()

			body, contentType := adjuntoHTTPMultipart(
				t, contenido, "foto E2E",
			)
			req := httptest.NewRequest(
				http.MethodPost, ruta, body,
			).WithContext(ctx)
			req.Header.Set("Content-Type", contentType)
			req.Header.Set("Idempotency-Key", clave)

			rec := httptest.NewRecorder()
			ent.router.ServeHTTP(rec, req)

			var respuesta map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &respuesta),
				rec.Body.String())
			return rec.Code, respuesta
		}

		// Primera subida.
		status1, primera := subir()
		require.Equal(t, http.StatusOK, status1, primera)
		require.Equal(t, clave, primera["id"])
		require.Equal(t, "evidencia", primera["tipo"])
		require.NotEmpty(t, primera["created_at"])

		// Reintento idéntico: debe conservar la misma imagen.
		status2, segunda := subir()
		require.Equal(t, http.StatusOK, status2, segunda)
		require.Equal(t, primera["id"], segunda["id"])

		// Verificar directamente las filas de Firebird.
		var total int
		err = firebird.GetQuerier(ctx, pool.DB).QueryRowContext(
			ctx,
			"SELECT COUNT(*) FROM MSP_GA_IMAGEN WHERE EVENTO_ID = ?",
			eventoID.String(),
		).Scan(&total)
		require.NoError(t, err)
		require.Equal(t, 1, total, "el reintento no debe duplicar imágenes")

		// Descargar la imagen guardada.
		url, ok := primera["url"].(string)
		require.True(t, ok)
		require.True(t, strings.HasPrefix(url, ruta+"/"))

		req := httptest.NewRequest(
			http.MethodGet, url, nil,
		).WithContext(ctx)
		rec := httptest.NewRecorder()
		ent.router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, "image/png", rec.Header().Get("Content-Type"))
		require.Equal(t, contenido, rec.Body.Bytes())

		// Confirmar que también aparece en el detalle del folio.
		detalleReq := httptest.NewRequest(
			http.MethodGet,
			"/garantias/"+garantiaID.String(),
			nil,
		).WithContext(ctx)
		detalleRec := httptest.NewRecorder()
		ent.router.ServeHTTP(detalleRec, detalleReq)

		require.Equal(t, http.StatusOK, detalleRec.Code,
			detalleRec.Body.String())
		require.Contains(t, detalleRec.Body.String(), clave)
	})
}
