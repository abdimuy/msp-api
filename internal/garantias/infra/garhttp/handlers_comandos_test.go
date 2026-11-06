//nolint:misspell // vocabulario español (comando, Comandos) por convención del proyecto.
package garhttp_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

// garantiaJSONCompleto is the exact response body every command answers with.
// It mirrors fixtureHTTPGarantia(); `$schema` is checked and dropped by
// assertCuerpoGarantia so the assertion stays about the folio.
const garantiaJSONCompleto = `{
	"id": "11111111-1111-1111-1111-111111111111",
	"folio": "GA-000123",
	"origen": "cliente",
	"cliente_id": 25,
	"venta_id": 80,
	"estado_cuenta": "liquidada",
	"estado": "en_proceso",
	"description": "Garantía de sala",
	"vigencia_hasta": "2027-01-31",
	"calle": "Reforma",
	"numero_exterior": "123",
	"colonia": "Centro",
	"localidad": "Centro",
	"ciudad": "Ciudad de México",
	"codigo_postal": "06000",
	"gps_lat": 19.4326,
	"gps_lon": -99.1332,
	"abierto_por": "ruben",
	"created_at": "2026-10-13T15:00:00Z",
	"updated_at": "2026-10-13T15:30:00Z",
	"articulos": [
		{
			"id": "22222222-2222-2222-2222-222222222222",
			"garantia_id": "11111111-1111-1111-1111-111111111111",
			"rol": "original",
			"articulo_id": 150,
			"clave": "SALA-001",
			"description": "Respaldo dañado",
			"ruta": "taller",
			"etapa": "en_taller",
			"ubicacion": "taller",
			"created_at": "2026-10-13T15:00:00Z",
			"updated_at": "2026-10-13T15:30:00Z"
		}
	]
}`

// enumInvalidoPorRuta names the enum field of each body that carries one. The
// routes without a body enum are absent on purpose.
var enumInvalidoPorRuta = map[string]string{
	"abrir-garantia":          "origen",
	"avanzar-articulo":        "etapa_destino",
	"registrar-diagnostico":   "ruta",
	"registrar-dictamen":      "dictamen",
	"mover-articulo":          "ubicacion",
	"autorizar-cambio-fisico": "rol_decisor",
	"marcar-no-devuelto":      "rol_decisor",
	"registrar-desenlace":     "desenlace",
}

func operadorAutenticado() *fakeComandoIdentity {
	return &fakeComandoIdentity{
		usuario:   outbound.Usuario{ID: "usuario-1", Nombre: "Ruben"},
		permitido: true,
	}
}

// nuevoRouter devuelve el router y las dependencias que la prueba necesita
// inspeccionar después de la petición.
func nuevoRouter(op string) (http.Handler, *fakeComandoGarantiaRepo, *fakeComandoEventoRepo) {
	fakes := nuevoRouterCompleto(op)

	return fakes.router, fakes.repositorio, fakes.eventos
}

// fixtureRespuestaComando is the folio every route answers with.
func fixtureRespuestaComando() *domain.Garantia {
	garantia, _ := fixtureHTTPGarantia()
	return garantia
}

// enviarEjemplo sends one example, optionally with an idempotency key. An empty
// clave means "do not send the header".
func enviarEjemplo(
	router http.Handler,
	ejemplo ejemploOperacion,
	clave string,
) *httptest.ResponseRecorder {
	req := httptest.NewRequest(
		ejemplo.Method,
		ejemplo.Path,
		bytes.NewReader(ejemplo.Body),
	)
	req.Header.Set("Content-Type", ejemplo.ContentType)

	if clave != "" {
		req.Header.Set("Idempotency-Key", clave)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	return rec
}

// assertCuerpoGarantia compares the whole folio body, ignoring only the
// `$schema` Huma adds.
func assertCuerpoGarantia(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got), rec.Body.String())
	require.Contains(t, got, "$schema", rec.Body.String())
	delete(got, "$schema")

	var esperado map[string]any
	require.NoError(t, json.Unmarshal([]byte(garantiaJSONCompleto), &esperado))

	require.Equal(t, esperado, got)
}

// cuerpoConEnumInvalido replaces one enum field of the JSON body with a value
// no enum accepts.
func cuerpoConEnumInvalido(t *testing.T, cuerpo []byte, campo string) []byte {
	t.Helper()

	var objeto map[string]any
	require.NoError(t, json.Unmarshal(cuerpo, &objeto))
	objeto[campo] = "no_existe"

	out, err := json.Marshal(objeto)
	require.NoError(t, err)

	return out
}

func TestEjemplosComandos_CatorceOperacionesUnicas(t *testing.T) {
	t.Parallel()

	ejemplos := ejemplosComandos()
	require.Len(t, ejemplos, 14)

	vistas := make(map[string]bool, len(ejemplos))

	for _, ejemplo := range ejemplos {
		require.False(t, vistas[ejemplo.OperationID], ejemplo.OperationID)
		vistas[ejemplo.OperationID] = true
		require.Equal(t, http.MethodPost, ejemplo.Method)
		require.NotEmpty(t, ejemplo.Path)
		require.NotEmpty(t, ejemplo.ContentType)
		require.NotEmpty(t, ejemplo.Body)
	}

	require.Len(t, ejemplosJSONComandos(), 13)
}

func TestComandos_CaminoFeliz_JSONCompleto(t *testing.T) {
	t.Parallel()

	for _, ejemplo := range ejemplosJSONComandos() {
		t.Run(ejemplo.OperationID, func(t *testing.T) {
			t.Parallel()

			router, repositorio, eventos := nuevoRouter(ejemplo.OperationID)

			rec := enviarEjemplo(router, ejemplo, claveEjemplo)

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			assertCuerpoGarantia(t, rec)

			require.Equal(t, claveEjemplo, eventos.ultimaClave)

			if ejemplo.OperationID == "abrir-garantia" {
				require.Len(t, repositorio.creados, 1)
				require.Zero(t, repositorio.guardados)
				return
			}

			require.Empty(t, repositorio.creados)
			require.Equal(t, 1, repositorio.guardados)
		})
	}
}

func TestComandos_SinClaveIdempotencia_422(t *testing.T) {
	t.Parallel()

	for _, ejemplo := range ejemplosJSONComandos() {
		t.Run(ejemplo.OperationID, func(t *testing.T) {
			t.Parallel()

			router, repositorio, eventos := nuevoRouter(ejemplo.OperationID)

			rec := enviarEjemplo(router, ejemplo, "")

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
			require.Zero(t, repositorio.guardados)
			require.Empty(t, repositorio.creados)
			require.Zero(t, eventos.busquedas)
		})
	}
}

func TestComandos_ClaveNoUUID_422(t *testing.T) {
	t.Parallel()

	for _, ejemplo := range ejemplosJSONComandos() {
		t.Run(ejemplo.OperationID, func(t *testing.T) {
			t.Parallel()

			router, repositorio, _ := nuevoRouter(ejemplo.OperationID)

			rec := enviarEjemplo(router, ejemplo, "no-es-un-uuid")

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
			require.Zero(t, repositorio.guardados)
			require.Empty(t, repositorio.creados)
		})
	}
}

func TestComandos_UUIDDeRutaInvalido_422(t *testing.T) {
	t.Parallel()

	for _, ejemplo := range ejemplosJSONComandos() {
		if !strings.Contains(ejemplo.Path, idGarantiaEjemplo) {
			continue
		}

		t.Run(ejemplo.OperationID, func(t *testing.T) {
			t.Parallel()

			invalido := ejemplo
			invalido.Path = strings.ReplaceAll(ejemplo.Path, idGarantiaEjemplo, "no-es-un-uuid")

			router, repositorio, _ := nuevoRouter(ejemplo.OperationID)

			rec := enviarEjemplo(router, invalido, claveEjemplo)

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
			require.Zero(t, repositorio.guardados)
		})
	}
}

func TestComandos_EnumInvalido_422(t *testing.T) {
	t.Parallel()

	for _, ejemplo := range ejemplosJSONComandos() {
		campo := enumInvalidoPorRuta[ejemplo.OperationID]
		if campo == "" {
			continue
		}

		t.Run(ejemplo.OperationID, func(t *testing.T) {
			t.Parallel()

			invalido := ejemplo
			invalido.Body = cuerpoConEnumInvalido(t, ejemplo.Body, campo)

			router, repositorio, _ := nuevoRouter(ejemplo.OperationID)

			rec := enviarEjemplo(router, invalido, claveEjemplo)

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
			require.Zero(t, repositorio.guardados)
		})
	}
}

func TestComandos_ErrorDeDominio_SuStatus(t *testing.T) {
	t.Parallel()

	for _, ejemplo := range ejemplosJSONComandos() {
		t.Run(ejemplo.OperationID, func(t *testing.T) {
			t.Parallel()

			router, repositorio, _ := nuevoRouter(ejemplo.OperationID)

			if ejemplo.OperationID == "abrir-garantia" {
				repositorio.errCrear = domain.ErrGarantiaNoEncontrada
			} else {
				repositorio.errParaActualizar = domain.ErrGarantiaNoEncontrada
			}

			rec := enviarEjemplo(router, ejemplo, claveEjemplo)

			require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), domain.ErrGarantiaNoEncontrada.Code)
		})
	}
}

func TestComandos_ErrorInterno_500SinFiltrar(t *testing.T) {
	t.Parallel()

	// An internal apperror (and any non-apperror) is logged and replaced by the
	// generic 500 body: the client never sees the repository message.
	for nombre, err := range map[string]error{
		"apperror_interno": apperror.NewInternal("boom", "detalle interno"),
		"error_ajeno":      errors.New("detalle interno"),
	} {
		t.Run(nombre, func(t *testing.T) {
			t.Parallel()

			ejemplo := ejemploDe("iniciar-proceso")
			repositorio := &fakeComandoGarantiaRepo{
				paraActualizar:    folioPara(ejemplo.OperationID),
				errParaActualizar: err,
			}
			router := buildCommandRouter(operadorAutenticado(), repositorio, &fakeComandoEventoRepo{})

			rec := enviarEjemplo(router, ejemplo, claveEjemplo)

			require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
			require.NotContains(t, rec.Body.String(), "detalle interno")
			require.Zero(t, repositorio.guardados)
		})
	}
}

func TestComandos_ClaveDeOtroFolio_409(t *testing.T) {
	t.Parallel()

	ejemplo := ejemploDe("iniciar-proceso")
	router, repositorio, eventos := nuevoRouter(ejemplo.OperationID)
	eventos.errBuscar = domain.ErrClaveIdempotenciaDeOtroFolio

	rec := enviarEjemplo(router, ejemplo, claveEjemplo)

	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), domain.ErrClaveIdempotenciaDeOtroFolio.Code)
	require.Zero(t, repositorio.guardados)
}

func TestComandos_401_SinUsuario(t *testing.T) {
	t.Parallel()

	for _, ejemplo := range ejemplosComandos() {
		t.Run(ejemplo.OperationID, func(t *testing.T) {
			t.Parallel()

			identity := &fakeComandoIdentity{
				usuarioErr: domain.ErrUsuarioNoAutenticado,
			}
			repositorio := &fakeComandoGarantiaRepo{paraActualizar: folioPara(ejemplo.OperationID)}
			router := buildCommandRouter(identity, repositorio, &fakeComandoEventoRepo{})

			rec := enviarEjemplo(router, ejemplo, claveEjemplo)

			require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
			require.Zero(t, repositorio.guardados)
			require.Empty(t, repositorio.creados)
		})
	}
}

func TestComandos_403_SinPermiso(t *testing.T) {
	t.Parallel()

	for _, ejemplo := range ejemplosComandos() {
		t.Run(ejemplo.OperationID, func(t *testing.T) {
			t.Parallel()

			identity := &fakeComandoIdentity{
				usuario:   outbound.Usuario{ID: "usuario-1", Nombre: "Ruben"},
				permitido: false,
			}
			repositorio := &fakeComandoGarantiaRepo{paraActualizar: folioPara(ejemplo.OperationID)}
			router := buildCommandRouter(identity, repositorio, &fakeComandoEventoRepo{})

			rec := enviarEjemplo(router, ejemplo, claveEjemplo)

			require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
			require.Zero(t, repositorio.guardados)
			require.Empty(t, repositorio.creados)
		})
	}
}

// ejemploDe returns the example of one operation.
func ejemploDe(operationID string) ejemploOperacion {
	for _, ejemplo := range ejemplosComandos() {
		if ejemplo.OperationID == operationID {
			return ejemplo
		}
	}

	panic("operación sin ejemplo: " + operationID)
}
