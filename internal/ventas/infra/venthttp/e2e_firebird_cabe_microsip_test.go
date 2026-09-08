//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// These tests exist because the four unit tests of checkParcialidadFits /
// checkNotaFits / checkAvalFits prove the FUNCTIONS work and nothing else:
// replacing the call to them with a no-op left the whole ventas suite green.
// What has to be pinned is that the aplicar path actually invokes them, with
// the real writer, over HTTP.
//
// And that it rejects BEFORE writing: the point of validating up front is that
// phase 6 has not yet flipped APLICADO='S' nor burned any generator.

// ventaCabeOpts are the levers each subtest moves. Everything else — zona,
// artículo, almacenes — is fixed so the only difference between subtests is
// the value under test.
type ventaCabeOpts struct {
	// Contado switches the venta to CONTADO, which never reaches
	// LIBRES_CARGOS_CC and therefore must not be measured against its widths.
	Contado bool
	// Parcialidad is ignored when Contado is set.
	Parcialidad string
	Nota        *string
	Aval        *string
}

// aplicarVentaCabe drives a venta through the full lifecycle — create →
// revisar → aprobar → aplicar — and returns the response of the last step.
func aplicarVentaCabe(
	t *testing.T, r http.Handler, usuarioID uuid.UUID, opts ventaCabeOpts,
) (*httptest.ResponseRecorder, string) {
	t.Helper()

	const (
		cabeZonaID     = 21563
		cabeArticuloID = 378
		cabeAlmacenID  = 11058
		cabeAlmDestID  = 11059
	)
	tel := "+522381234567"
	numExt := "100"

	body := validCreateBody()
	body.Vendedores[0].UsuarioID = usuarioID.String()
	if opts.Contado {
		body.TipoVenta = "CONTADO"
	} else {
		body.TipoVenta = "CREDITO"
		body.PlanCredito = &venthttp.PlanCreditoDTO{
			PlazoMeses: 12, Enganche: "100.00", Parcialidad: opts.Parcialidad, FrecPago: "SEMANAL",
		}
		semana := "LUNES"
		body.DiaCobranza = &venthttp.DiaCobranzaDTO{Semana: &semana}
	}
	body.Nota = opts.Nota
	// Un nombre distinto por subtest: los que sí aplican auto-crean el
	// cliente en Microsip, y dos clientes con el mismo nombre chocan contra la
	// unicidad del padrón dentro de la misma transacción de prueba.
	body.Cliente = venthttp.ClienteSnapshotDTO{
		Nombre:   "MARISOL AGUILAR " + strings.ToUpper(uuid.NewString()[:8]),
		Telefono: &tel,
		Aval:     opts.Aval,
	}
	body.Direccion = venthttp.DireccionDTO{
		ZonaClienteID:  intPtr(cabeZonaID),
		Calle:          "AV CUAUHTÉMOC",
		NumeroExterior: &numExt,
		Colonia:        "CENTRO TEST",
		Poblacion:      "TEHUACAN",
		Ciudad:         "TEHUACAN",
	}
	body.Productos[0].ArticuloID = cabeArticuloID
	body.Productos[0].AlmacenOrigenID = intPtr(cabeAlmacenID)
	body.Productos[0].AlmacenDestinoID = intPtr(cabeAlmDestID)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, crearVentaMultipartRequest(t, body))
	require.Equal(t, http.StatusCreated, rec.Code, "crear venta: %s", rec.Body.String())

	var created venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	for _, paso := range []string{"revisar", "aprobar"} {
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, jsonRequest(t, http.MethodPost, "/ventas/"+created.ID+"/"+paso, struct{}{}))
		require.Equalf(t, http.StatusOK, rec.Code, "%s: %s", paso, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, jsonRequest(t, http.MethodPost, "/ventas/"+created.ID+"/aplicar", struct{}{}))
	return rec, created.ID
}

// requireSigueSinAplicar asserts the rejection left the venta untouched: still
// aprobada, still pendiente, with no Microsip artifacts. That is the half a
// unit test of the guard function cannot say.
func requireSigueSinAplicar(t *testing.T, r http.Handler, ventaID string) {
	t.Helper()

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, jsonRequest(t, http.MethodGet, "/ventas/"+ventaID, nil))
	require.Equal(t, http.StatusOK, rec.Code, "consultar venta: %s", rec.Body.String())

	var got venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "aprobada", got.Situacion, "a rejected aplicar must not move the situación")
	assert.Equal(t, "pendiente", got.Sincronizacion, "nor the sincronización")
	assert.Nil(t, got.MicrosipDoctoPVID, "and it must not have reached DOCTOS_PV")
	assert.Nil(t, got.MicrosipFolio)
}

//nolint:paralleltest // shared rollback tx, like every Firebird E2E here.
func TestE2E_AplicarVenta_RechazaLoQueNoCabeEnLibresCargosCC(t *testing.T) {
	pool := e2eTestPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EAutoCrearClienteService(pool)

		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eAllPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		t.Run("la parcialidad que el SMALLINT no aguanta", func(t *testing.T) {
			rec, ventaID := aplicarVentaCabe(t, r, usuarioID, ventaCabeOpts{Parcialidad: "33000.00"})

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code,
				"aplicar debe rechazar, no reventar en el INSERT: %s", rec.Body.String())
			assert.Contains(t, rec.Body.String(), "parcialidad_exceeds_microsip_max")
			assert.Contains(t, rec.Body.String(), "32767", "el mensaje lleva el tope")
			requireSigueSinAplicar(t, r, ventaID)
		})

		// LA NOTA YA NO RECHAZA: SE RECORTA. Rechazar la venta entera por un
		// comentario que sobra es desproporcionado, y no hay nada que perder:
		// la nota completa vive en MSP_VENTAS.NOTA, y lo de LIBRES_CARGOS_CC
		// es la copia que Microsip enseña en su pantalla.
		//
		// Que el 200 sea la prueba del recorte no es indirecto: Firebird NO
		// trunca el sobrante, RECHAZA la sentencia con SQLSTATE 22001. Si la
		// nota viajara entera, la fase 7 reventaría y esto no sería 200.
		t.Run("la nota mas larga que OBSERVACIONES se recorta y la venta aplica", func(t *testing.T) {
			// 230 caracteres: el largo exacto de la nota que ya existía en una
			// venta CREDITO en borrador de la base de desarrollo.
			nota := strings.Repeat("A", 230)
			rec, _ := aplicarVentaCabe(t, r, usuarioID,
				ventaCabeOpts{Parcialidad: "150.00", Nota: &nota})

			require.Equal(t, http.StatusOK, rec.Code,
				"una nota larga ya no bloquea la venta: se recorta al escribir. "+
					"Un 422 aquí significa que ValidarCabe volvió a rechazarla; "+
					"un error de base, que el recorte no se aplicó: %s", rec.Body.String())
			assert.NotContains(t, rec.Body.String(), "nota_exceeds_microsip_max")
		})

		t.Run("el aval mas largo que AVAL_O_RESPONSABLE", func(t *testing.T) {
			// 60 caracteres: por debajo de los 200 que admite el dominio
			// (maxAvalLength) y por encima de los 50 de la columna, que es
			// justo el hueco que nadie cerraba.
			aval := strings.Repeat("B", 60)
			rec, ventaID := aplicarVentaCabe(t, r, usuarioID,
				ventaCabeOpts{Parcialidad: "150.00", Aval: &aval})

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code,
				"aplicar debe rechazar, no reventar en el INSERT: %s", rec.Body.String())
			assert.Contains(t, rec.Body.String(), "aval_exceeds_microsip_max")
			assert.Contains(t, rec.Body.String(), "60", "el mensaje lleva el tamaño recibido")
			requireSigueSinAplicar(t, r, ventaID)
		})

		// TestE2E… /la venta de contado no se mide contra LIBRES_CARGOS_CC
		// fija la EXENCIÓN, no la guarda. Sin este caso, quitar el
		// `if v.TipoVenta() != domain.TipoVentaCredito` de ValidarCabe deja
		// todo en verde, y una venta de contado empezaría a rechazarse por el
		// ancho de una columna a la que su nota jamás llega: CONTADO no
		// escribe LIBRES_CARGOS_CC.
		t.Run("la venta de contado no se mide contra LIBRES_CARGOS_CC", func(t *testing.T) {
			nota := strings.Repeat("A", 230)
			rec, _ := aplicarVentaCabe(t, r, usuarioID,
				ventaCabeOpts{Contado: true, Nota: &nota})

			require.Equal(t, http.StatusOK, rec.Code,
				"una venta de contado con nota de 230 debe aplicar: su nota no "+
					"llega a OBSERVACIONES: %s", rec.Body.String())
		})

		t.Run("control positivo: la misma venta con valores que caben si aplica", func(t *testing.T) {
			nota := strings.Repeat("A", 99)
			rec, _ := aplicarVentaCabe(t, r, usuarioID,
				ventaCabeOpts{Parcialidad: "150.00", Nota: &nota})

			require.Equal(t, http.StatusOK, rec.Code,
				"sin este caso las dos pruebas de arriba pasarían aunque aplicar "+
					"estuviera roto por cualquier otra razón: %s", rec.Body.String())
		})
	})
}
