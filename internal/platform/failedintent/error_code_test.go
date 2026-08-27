package failedintent_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	"github.com/abdimuy/msp-api/internal/platform/response"
)

// ---------------------------------------------------------------------------
// ERROR_CODE: las DOS formas de cuerpo de error que el API emite de verdad
//
// El defecto que originó este archivo: ERROR_CODE quedaba vacía en todas las
// filas de los módulos servidos por Huma (ventas entre ellos), porque el
// parser sólo leía el miembro `code` de primer nivel que escribe
// `platform/response`. Huma no tiene ese miembro: `mapAppError` mete el código
// dentro de `errors[].message` con el prefijo `code=`.
//
// Nadie lo detectó porque la prueba anterior usaba un cuerpo INVENTADO —uno
// con `code` plano que ventas nunca produce—. Por eso todos los cuerpos de
// este archivo son REALES: o los genera el mismo escritor que corre en
// producción (`response.Error`, `huma.NewError`), o son copia literal de lo
// que emitió una ronda completa chi+humachi con `huma.DefaultConfig`, la misma
// composición de `internal/ventas/infra/venthttp/routes.go:29-52`.
//
// El miembro `$schema` de los cuerpos de Huma NO es decorado: lo añade el
// SchemaLinkTransformer que trae `huma.DefaultConfig`, y por eso aparece en el
// cable pero no en el modelo que devuelve `huma.NewError`.
// ---------------------------------------------------------------------------

// --- Cuerpos reales de Huma (copia literal de la ronda chi+humachi) ---------

// cuerpoHumaAppError es la respuesta de ventas a un apperror tipado: 422 con
// el código dentro de errors[0].message. Es el cuerpo exacto que dejó
// ERROR_CODE vacía en producción.
const cuerpoHumaAppError = `{"$schema":"https://example.com/schemas/ErrorModel.json",` +
	`"title":"Unprocessable Entity","status":422,` +
	`"detail":"el combo referenciado por el producto no existe en la venta",` +
	`"errors":[{"message":"code=producto_combo_referencia_invalida"}]}`

// cuerpoHumaValidacion es la validación de request de Huma con DOS campos
// inválidos: errors[] trae varios elementos y NINGUNO lleva prefijo. Es la
// trampa que un `strings.Contains` habría convertido en un código inventado.
const cuerpoHumaValidacion = `{"$schema":"https://example.com/schemas/ErrorModel.json",` +
	`"title":"Unprocessable Entity","status":422,"detail":"validation failed",` +
	`"errors":[{"message":"expected length >= 3","location":"body.folio","value":"a"},` +
	`{"message":"expected array length >= 1","location":"body.productos","value":[]}]}`

// cuerpoHuma500 es la rama no-apperror de mapAppError: el detalle es el
// err.Error() crudo, sin prefijo. No hay código que extraer y no debe
// inventarse ninguno.
const cuerpoHuma500 = `{"$schema":"https://example.com/schemas/ErrorModel.json",` +
	`"title":"Internal Server Error","status":500,"detail":"ocurrió un error interno",` +
	`"errors":[{"message":"conexión perdida"}]}`

// cuerpoHumaCodigoLargo lleva un código de 95 caracteres — más ancho que
// ERROR_CODE VARCHAR(80). Generado igual que los demás, con un apperror cuyo
// código es strings.Repeat("x", 95).
var cuerpoHumaCodigoLargo = `{"$schema":"https://example.com/schemas/ErrorModel.json",` +
	`"title":"Unprocessable Entity","status":422,"detail":"código absurdamente largo",` +
	`"errors":[{"message":"code=` + strings.Repeat("x", 95) + `"}]}`

// --- Cuerpos reales de platform/response (chi: auth + middlewares) ---------

// cuerpoPlanoError es lo que escribe response.Error, el camino de siempre.
// Se genera abajo en el arranque de las pruebas, no se teclea.
var (
	cuerpoPlanoError      = generarCuerpoPlanoError()
	cuerpoPlanoValidacion = generarCuerpoPlanoValidacion()
)

// generarCuerpoPlanoError ejercita el escritor real de los middlewares.
func generarCuerpoPlanoError() string {
	rw := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v2/pagos", http.NoBody)
	response.Error(rw, req, apperror.NewConflict(
		"idempotency_key_mismatch", "la llave de idempotencia no coincide"))
	return rw.Body.String()
}

// generarCuerpoPlanoValidacion ejercita response.ValidationError, que es el
// único escritor REAL que emite `code` plano Y `errors[]` a la vez. Sus
// elementos son FieldError (field/code/message) con mensaje en prosa: no
// llevan el prefijo y no deben interferir.
func generarCuerpoPlanoValidacion() string {
	rw := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v2/pagos", http.NoBody)
	response.ValidationError(rw, req, []response.FieldError{
		{Field: "monto", Code: "monto_invalido", Message: "el monto debe ser mayor a cero"},
		{Field: "fecha", Code: "fecha_invalida", Message: "la fecha no es válida"},
	})
	return rw.Body.String()
}

// --- Cuerpos compuestos con el propio huma.NewError ------------------------

// cuerpoHuma arma un cuerpo con el mismo constructor que usa mapAppError en
// los diez módulos servidos por Huma. Le falta el `$schema` que añade el
// transformer al escribir; para el parser es irrelevante y las pruebas que
// necesitan el cuerpo de cable usan las constantes de arriba.
func cuerpoHuma(t *testing.T, status int, detalle string, mensajes ...string) string {
	t.Helper()
	detalles := make([]error, 0, len(mensajes))
	for _, m := range mensajes {
		detalles = append(detalles, &huma.ErrorDetail{Message: m})
	}
	crudo, err := json.Marshal(huma.NewError(status, detalle, detalles...))
	require.NoError(t, err)
	return string(crudo)
}

// ---------------------------------------------------------------------------
// Fidelidad: el cuerpo literal de arriba ES el que produce huma hoy
//
// Sin esto las constantes serían otra vez "una forma que alguien recuerda".
// Si huma cambiara el modelo de error, esta prueba cae y las demás dejan de
// mentir.
// ---------------------------------------------------------------------------

func TestCuerpoHumaLiteral_CoincideConHumaNewError(t *testing.T) {
	t.Parallel()

	// Reproducción línea por línea de mapAppError
	// (internal/ventas/infra/venthttp/auth.go:56-63) para un apperror de
	// validación: status = Kind.HTTPStatus(), detail = ae.Message,
	// errors[0].message = "code=" + ae.Code.
	ae := apperror.NewValidation(
		"producto_combo_referencia_invalida",
		"el combo referenciado por el producto no existe en la venta")
	generado := cuerpoHuma(t, ae.Kind.HTTPStatus(), ae.Message, "code="+ae.Code)

	require.Equal(t, http.StatusUnprocessableEntity, ae.Kind.HTTPStatus(),
		"el Kind de validación debe seguir mapeando a 422")
	assert.JSONEq(t, sinSchema(t, cuerpoHumaAppError), generado,
		"el cuerpo literal ya no es el que emite huma.NewError")
}

// sinSchema quita el miembro `$schema`, que el transformer añade al escribir y
// no forma parte del modelo que devuelve huma.NewError.
func sinSchema(t *testing.T, cuerpo string) string {
	t.Helper()
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(cuerpo), &m))
	require.Contains(t, m, "$schema", "el cuerpo de cable debe traer $schema")
	delete(m, "$schema")
	crudo, err := json.Marshal(m)
	require.NoError(t, err)
	return string(crudo)
}

// ---------------------------------------------------------------------------
// El caso del defecto, aislado y con nombre propio
// ---------------------------------------------------------------------------

func TestCaptura_ExtraeCodigoDeCuerpoHumaDeVentas(t *testing.T) {
	t.Parallel()

	got := capturar(t, http.StatusUnprocessableEntity, cuerpoHumaAppError)

	assert.Equal(t, "producto_combo_referencia_invalida", got.ErrorCode,
		"ERROR_CODE vacía aquí es exactamente el defecto: el filtro por código "+
			"devuelve cero para ventas y se lee como 'no hay nada'")
	assert.Equal(t, "el combo referenciado por el producto no existe en la venta",
		got.ErrorMessage)
}

// ---------------------------------------------------------------------------
// Tabla: las dos formas, sus mezclas y sus bordes
// ---------------------------------------------------------------------------

func TestCaptura_ErrorCodeEnAmbasFormas(t *testing.T) {
	t.Parallel()

	// anchoErrorCode es el ancho de MSP_FAILED_INTENTS.ERROR_CODE
	// (migración 000031, VARCHAR(80) CHARACTER SET ASCII).
	const anchoErrorCode = 80
	codigoLargo := strings.Repeat("x", 95)

	casos := []struct {
		nombre   string
		status   int
		cuerpo   string
		wantCode string
		wantMsg  string
		porQue   string
	}{
		{
			nombre:   "plana_response_error_sigue_funcionando",
			status:   http.StatusConflict,
			cuerpo:   cuerpoPlanoError,
			wantCode: "idempotency_key_mismatch",
			wantMsg:  "la llave de idempotencia no coincide",
			porQue:   "auth y los middlewares no pueden regresar",
		},
		{
			nombre:   "plana_con_errors_de_validacion",
			status:   http.StatusUnprocessableEntity,
			cuerpo:   cuerpoPlanoValidacion,
			wantCode: "validation_failed",
			wantMsg:  "uno o más campos no son válidos",
			porQue:   "los FieldError no llevan prefijo y no deben estorbar",
		},
		{
			nombre:   "huma_apperror_extrae_el_codigo",
			status:   http.StatusUnprocessableEntity,
			cuerpo:   cuerpoHumaAppError,
			wantCode: "producto_combo_referencia_invalida",
			wantMsg:  "el combo referenciado por el producto no existe en la venta",
			porQue:   "es el cuerpo que dejó la columna vacía en producción",
		},
		{
			nombre:   "huma_validacion_varios_sin_prefijo",
			status:   http.StatusUnprocessableEntity,
			cuerpo:   cuerpoHumaValidacion,
			wantCode: "",
			wantMsg:  "validation failed",
			porQue:   "'expected array length >= 1' no es un código",
		},
		{
			nombre:   "huma_500_no_apperror",
			status:   http.StatusInternalServerError,
			cuerpo:   cuerpoHuma500,
			wantCode: "",
			wantMsg:  "ocurrió un error interno",
			porQue:   "la rama no tipada no lleva código; inventar uno sería peor",
		},
		{
			nombre:   "codigo_mas_largo_que_la_columna_se_recorta",
			status:   http.StatusUnprocessableEntity,
			cuerpo:   cuerpoHumaCodigoLargo,
			wantCode: codigoLargo[:anchoErrorCode],
			wantMsg:  "código absurdamente largo",
			porQue:   "ERROR_CODE es VARCHAR(80): sin recorte, Firebird rechaza el INSERT",
		},
		{
			nombre:   "sin_ninguna_de_las_dos_formas",
			status:   http.StatusUnprocessableEntity,
			cuerpo:   `{"status":422}`,
			wantCode: "",
			wantMsg:  "",
			porQue:   "un cuerpo sin forma de Problem no debe reventar la captura",
		},
		{
			nombre:   "json_malformado",
			status:   http.StatusUnprocessableEntity,
			cuerpo:   `{"title":"Unprocessable Entity","errors":[{"message":"code=venta_rota"`,
			wantCode: "",
			wantMsg:  "",
			porQue:   "un blob truncado no es JSON y no se parsea a medias",
		},
		{
			nombre:   "texto_plano",
			status:   http.StatusBadGateway,
			cuerpo:   "502 Bad Gateway",
			wantCode: "",
			wantMsg:  "",
			porQue:   "un proxy puede meter texto plano en la respuesta",
		},
	}

	for _, tc := range casos {
		t.Run(tc.nombre, func(t *testing.T) {
			t.Parallel()

			got := capturar(t, tc.status, tc.cuerpo)

			assert.Equal(t, tc.wantCode, got.ErrorCode, tc.porQue)
			assert.Equal(t, tc.wantMsg, got.ErrorMessage, tc.porQue)
		})
	}
}

// ---------------------------------------------------------------------------
// Mezclas: quién gana cuando conviven las dos formas
// ---------------------------------------------------------------------------

func TestCaptura_PrecedenciaEntreFormas(t *testing.T) {
	t.Parallel()

	t.Run("las_dos_a_la_vez_gana_la_plana", func(t *testing.T) {
		t.Parallel()

		// Sintético a propósito: ningún escritor del API emite las dos formas
		// con códigos distintos. Fija la regla por si alguien añade el miembro
		// plano a los módulos de Huma sin quitar el prefijo — el plano manda.
		cuerpo := `{"title":"Unprocessable Entity","status":422,` +
			`"detail":"el combo no existe","code":"codigo_plano",` +
			`"errors":[{"message":"code=codigo_de_huma"}]}`

		got := capturar(t, http.StatusUnprocessableEntity, cuerpo)

		assert.Equal(t, "codigo_plano", got.ErrorCode)
	})

	t.Run("mezcla_prefijo_entre_ruido_de_validacion", func(t *testing.T) {
		t.Parallel()

		// Cuerpo construido con huma.NewError, el mismo constructor de
		// mapAppError, con un detalle de validación ANTES del que lleva
		// prefijo: el código se encuentra aunque no venga primero.
		cuerpo := cuerpoHuma(t, http.StatusUnprocessableEntity, "validación mixta",
			"expected array length >= 1", "code=venta_zona_obligatoria")

		got := capturar(t, http.StatusUnprocessableEntity, cuerpo)

		assert.Equal(t, "venta_zona_obligatoria", got.ErrorCode)
		assert.Equal(t, "validación mixta", got.ErrorMessage)
	})

	t.Run("varios_con_prefijo_gana_el_primero", func(t *testing.T) {
		t.Parallel()

		cuerpo := cuerpoHuma(t, http.StatusUnprocessableEntity, "dos códigos",
			"code=primero_gana", "code=segundo_pierde")

		got := capturar(t, http.StatusUnprocessableEntity, cuerpo)

		assert.Equal(t, "primero_gana", got.ErrorCode,
			"con varios prefijos el resultado debe ser determinista, no el último visto")
	})

	t.Run("prefijo_vacio_no_es_codigo", func(t *testing.T) {
		t.Parallel()

		cuerpo := cuerpoHuma(t, http.StatusUnprocessableEntity, "sin código", "code=")

		got := capturar(t, http.StatusUnprocessableEntity, cuerpo)

		assert.Empty(t, got.ErrorCode)
	})

	t.Run("prosa_que_empieza_con_el_prefijo_no_es_codigo", func(t *testing.T) {
		t.Parallel()

		// La rama no-apperror de mapAppError vuelca err.Error() tal cual. Si
		// ese texto empezara con el prefijo, guardar la frase entera
		// contaminaría el agrupamiento — y un byte no-ASCII haría que Firebird
		// rechazara el INSERT sobre una columna CHARACTER SET ASCII.
		cuerpo := cuerpoHuma(t, http.StatusInternalServerError, "ocurrió un error interno",
			"code= no soy un código, soy una frase")

		got := capturar(t, http.StatusInternalServerError, cuerpo)

		assert.Empty(t, got.ErrorCode)
		assert.Equal(t, "ocurrió un error interno", got.ErrorMessage)
	})
}

// ---------------------------------------------------------------------------
// Ayudante
// ---------------------------------------------------------------------------

// capturar hace pasar una respuesta con el status y cuerpo dados por
// CaptureMiddleware y devuelve el Intent que se guardó. parseProblemJSON es
// privada: ésta es la única vía honesta de ejercitarla, y de paso mide lo que
// de verdad acaba en la fila.
func capturar(t *testing.T, status int, cuerpo string) failedintent.Intent {
	t.Helper()

	store := &fakeStore{}
	mw := failedintent.CaptureMiddleware(newTestConfig(store, 4096))

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, cuerpo)
	})

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	mw(inner).ServeHTTP(httptest.NewRecorder(), req)

	require.Equal(t, 1, store.count(), "la captura debe haber guardado exactamente un intento")
	return store.first()
}
