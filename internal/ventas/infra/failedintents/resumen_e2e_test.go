//go:build !ci_skip_firebird

//nolint:misspell // Spanish vocabulary by project convention.

package failedintents_test

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	failedintentblobfs "github.com/abdimuy/msp-api/internal/platform/failedintent/blobfs"
	failedintentfb "github.com/abdimuy/msp-api/internal/platform/failedintent/firebird"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	ventasfailedintents "github.com/abdimuy/msp-api/internal/ventas/infra/failedintents"
)

// Esta prueba corre el camino COMPLETO con las piezas de producción: el
// middleware de captura de verdad, el blob en disco de verdad, el store de
// Firebird de verdad y el extractor de ventas de verdad.
//
// Vive en ventas y no en la plataforma porque es el único lado que puede
// juntarlos: `internal/platform/failedintent` tiene prohibido importar un
// módulo de negocio (regla `failedintent-no-modules` de depguard, más
// TestPlataformaNoImportaModulos). Esa prohibición es la que sostiene el
// puerto; la prueba que la cruza tiene que vivir del lado permitido.
//
// Lo que mide es exactamente el defecto que se arregló: una venta con fotos
// —o sea multipart— cuyo cuerpo NO viaja en la fila, y de la que sin embargo
// tienen que quedar el nombre del cliente y el monto.

func TestE2E_CapturaDeVentaMultipart_DejaModuloYResumenEnLaFila(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)

	blobs, err := failedintentblobfs.New(t.TempDir())
	require.NoError(t, err)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		store := failedintentfb.New(pool)

		cfg := failedintent.Config{
			Store: store,
			Blob:  blobs,
			// El mismo registro que arma cmd/api.
			Resumen: failedintent.NewRegistroExtractores().
				Registrar("/v2/ventas", "ventas", ventasfailedintents.NewResumenExtractor()),
			PathPrefixes:      []string{"/v2/ventas"},
			Methods:           []string{http.MethodPost},
			MaxMultipartBytes: 10 << 20,
		}

		req := ventaMultipartReal(t).WithContext(ctx)
		rec := httptest.NewRecorder()

		// Un handler que lee el cuerpo y contesta 422, como el real cuando
		// falta inventario.
		failedintent.CaptureMiddleware(cfg)(http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = io.WriteString(w,
					`{"code":"articulo_sin_existencia","detail":"falta 1 base Leos Venecia"}`)
			},
		)).ServeHTTP(rec, req)

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)

		// La custodia se confirmó: el teléfono puede soltar la captura.
		idCapturado := rec.Header().Get(failedintent.HeaderIntentCaptured)
		require.NotEmpty(t, idCapturado, "sin este encabezado el teléfono reintenta para siempre")

		// Y ahora lo que importa: la fila.
		page, err := store.List(ctx, failedintent.ListParams{Modulo: "ventas", PageSize: 50})
		require.NoError(t, err)

		var fila *failedintent.Intent
		for i := range page.Items {
			if page.Items[i].ID.String() == idCapturado {
				fila = &page.Items[i]
				break
			}
		}
		require.NotNil(t, fila, "la fila debe salir por el filtro de módulo, que es SQL")

		// El cuerpo sigue vacío en la fila — eso NO cambió, y es justo por lo
		// que el resumen tiene que viajar aparte.
		assert.JSONEq(t, `null`, string(fila.Body))
		assert.NotEmpty(t, fila.BodyBlobPath, "la evidencia sigue en disco")

		assert.Equal(t, "ventas", fila.Modulo)
		require.NotNil(t, fila.Resumen, "el renglón necesita quién y cuánto")
		assert.Equal(t, "Carmen López Zavaleta", fila.Resumen.Titulo)
		require.NotNil(t, fila.Resumen.Monto)
		assert.Equal(t, "10300.5", fila.Resumen.Monto.String())
		assert.Equal(t, "6f1c9f0e-1111-4222-8333-444444444444", fila.Resumen.Referencia)

		// Y no vuelve a salir en el filtro del relleno del janitor: ya se
		// extrajo, no hay por qué reabrir su blob cada hora.
		sinExtraer, err := store.List(ctx, failedintent.ListParams{SinExtraer: true, PageSize: 50})
		require.NoError(t, err)
		for _, i := range sinExtraer.Items {
			assert.NotEqual(t, fila.ID, i.ID)
		}
	})
}

// ventaMultipartReal arma el cuerpo que manda la app: un campo `datos` con el
// JSON de la venta y dos fotos.
func ventaMultipartReal(t *testing.T) *http.Request {
	t.Helper()
	const datos = `{
		"id": "6f1c9f0e-1111-4222-8333-444444444444",
		"cliente": {"nombre": "Carmen López Zavaleta", "telefono": "2381234567"},
		"tipo_venta": "CREDITO",
		"montos": {"anual": "12000.00", "corto_plazo": "10300.50", "contado": "9000.00"},
		"productos": [{"articulo": "Base de cama Leos Venecia", "cantidad": "1"}]
	}`

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("datos", datos))
	for _, nombre := range []string{"domicilio.jpg", "ine.jpg"} {
		fw, err := mw.CreateFormFile("imagen", nombre)
		require.NoError(t, err)
		_, err = fw.Write(bytes.Repeat([]byte{0xFF, 0xD8, 0xFF, 0xE0}, 8192))
		require.NoError(t, err)
	}
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Idempotency-Key", "e2e-resumen-venta-1")
	return req
}
