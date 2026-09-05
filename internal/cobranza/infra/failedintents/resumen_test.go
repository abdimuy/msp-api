//nolint:misspell // Spanish vocabulary by project convention.

package failedintents_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/infra/failedintents"
)

func TestResumenExtractor_FormaValida(t *testing.T) {
	t.Parallel()

	cuerpo := []byte(`{
		"id": "9a2b1c3d-0000-4000-8000-000000000000",
		"cargo_docto_cc_id": 887766,
		"cliente_id": 12345,
		"cobrador_id": 7,
		"cobrador": "Gabriel Roque",
		"importe": "350.00",
		"forma_cobro_id": 71,
		"fecha_hora_pago": "2026-08-25T14:03:00Z"
	}`)

	got := failedintents.NewResumenExtractor().Extraer("/v2/cobranza/pagos", cuerpo, "application/json")

	require.NotNil(t, got)
	require.Equal(t, "Gabriel Roque", got.Titulo)
	require.Equal(t, "12345", got.Referencia, "el ancla es el cliente")
	require.NotNil(t, got.Monto)
	require.Equal(t, "350", got.Monto.String())
	require.Empty(t, got.Modulo, "el módulo lo estampa el registro, no el extractor")
}

// Sin cliente, el ancla es el cargo. Sirve igual para encontrar la venta.
func TestResumenExtractor_SinClienteAnclaAlCargo(t *testing.T) {
	t.Parallel()

	got := failedintents.NewResumenExtractor().Extraer(
		"/v2/cobranza/pagos",
		[]byte(`{"cargo_docto_cc_id":887766,"cobrador":"Ana","importe":"100"}`), "",
	)
	require.Equal(t, "887766", got.Referencia)
}

// Un cero no es un id: es el campo ausente. Poner "0" en el renglón mandaría
// a buscar un cliente que no existe.
func TestResumenExtractor_IDsEnCeroNoSonAncla(t *testing.T) {
	t.Parallel()

	got := failedintents.NewResumenExtractor().Extraer(
		"/v2/cobranza/pagos",
		[]byte(`{"cliente_id":0,"cargo_docto_cc_id":0,"cobrador":"Ana","importe":"100"}`), "",
	)
	require.NotNil(t, got)
	require.Empty(t, got.Referencia)
	require.Equal(t, "Ana", got.Titulo)
}

func TestResumenExtractor_AceptaImporteComoNumero(t *testing.T) {
	t.Parallel()

	got := failedintents.NewResumenExtractor().Extraer(
		"/v2/cobranza/pagos", []byte(`{"cliente_id":1,"importe":350.75}`), "",
	)
	require.NotNil(t, got.Monto)
	require.Equal(t, "350.75", got.Monto.String())
}

// Un pago de cero no existe. Mostrar "$0.00" se leería como un dato cuando es
// un hueco.
func TestResumenExtractor_ImporteNoPositivoSeDescarta(t *testing.T) {
	t.Parallel()

	casos := []string{
		`{"cliente_id":1,"importe":"0"}`,
		`{"cliente_id":1,"importe":"0.00"}`,
		`{"cliente_id":1,"importe":"-50"}`,
		`{"cliente_id":1,"importe":0}`,
		`{"cliente_id":1,"importe":"no soy un número"}`,
		`{"cliente_id":1,"importe":true}`,
		`{"cliente_id":1,"importe":null}`,
		`{"cliente_id":1,"importe":{"raro":1}}`,
		`{"cliente_id":1}`,
	}
	for _, c := range casos {
		t.Run(c, func(t *testing.T) {
			t.Parallel()
			got := failedintents.NewResumenExtractor().Extraer("/v2/cobranza/pagos", []byte(c), "")
			require.NotNil(t, got, "el ancla del cliente basta para reconocerlo")
			require.Nil(t, got.Monto)
			require.Equal(t, "1", got.Referencia)
		})
	}
}

func TestResumenExtractor_FormaAjenaDevuelveNil(t *testing.T) {
	t.Parallel()

	ajenos := []string{
		`{}`,
		`{"otra":"forma"}`,
		`[]`,
		`null`,
		`123`,
		`"una cadena"`,
		`no es json`,
		``,
		`{"cobrador":"   "}`,
		`{"cobrador":123}`,
	}
	for _, c := range ajenos {
		t.Run(c, func(t *testing.T) {
			t.Parallel()
			require.Nil(t, failedintents.NewResumenExtractor().Extraer("/v2/cobranza/pagos", []byte(c), ""))
		})
	}
}

func TestResumenExtractor_RecortaLosEspacios(t *testing.T) {
	t.Parallel()

	got := failedintents.NewResumenExtractor().Extraer(
		"/v2/cobranza/pagos", []byte(`{"cliente_id":9,"cobrador":"  Ana Pérez \t"}`), "",
	)
	require.Equal(t, "Ana Pérez", got.Titulo)
}

// FuzzResumenExtractor: ningún cuerpo arbitrario lo hace entrar en pánico ni
// producir un título de la nada.
func FuzzResumenExtractor(f *testing.F) {
	f.Add(`{"cliente_id":1,"cobrador":"Ana","importe":"350"}`)
	f.Add(`{"cargo_docto_cc_id":5}`)
	f.Add(`{}`)
	f.Add(`[1,2,3]`)
	f.Add(`{"importe":1e309}`)
	f.Add("\x00\xff")

	ex := failedintents.NewResumenExtractor()
	f.Fuzz(func(t *testing.T, cuerpo string) {
		got := ex.Extraer("/v2/cobranza/pagos", []byte(cuerpo), "application/json")
		if got == nil {
			return
		}
		require.Empty(t, got.Modulo)
		require.False(t, got.Vacio(), "un resumen sin datos debe ser nil, no vacío")

		// Un título sólo puede salir de una llave `cobrador` del cuerpo. La
		// comparación es insensible a mayúsculas porque encoding/json lo es.
		if got.Titulo != "" {
			var sonda map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(cuerpo), &sonda))
			tiene := false
			for k := range sonda {
				if strings.EqualFold(k, "cobrador") {
					tiene = true
					break
				}
			}
			require.True(t, tiene, "título sin llave cobrador: %q", cuerpo)
		}
		// Y el monto, cuando existe, es estrictamente positivo.
		if got.Monto != nil {
			require.True(t, got.Monto.IsPositive(), "monto no positivo: %s", got.Monto)
		}
	})
}

// ─── el cliente_id explícito ────────────────────────────────────────────────
//
// El listado resuelve el nombre del cliente contra CLIENTES usando ESTE campo,
// no la Referencia. La diferencia importa: la Referencia cae al id del CARGO
// cuando no hay cliente, y las dos son indistinguibles una vez guardadas, así
// que buscar con ella mostraría de vez en cuando el nombre de OTRO cliente.

// TestResumenExtractor_GuardaElClienteIDAparteDeLaReferencia es lo que hace
// que el nombre del cliente pueda resolverse. Sin ella, anular el campo en el
// extractor dejaba toda la suite en verde — medido con mutación dirigida.
func TestResumenExtractor_GuardaElClienteIDAparteDeLaReferencia(t *testing.T) {
	t.Parallel()

	got := failedintents.NewResumenExtractor().Extraer(
		"/v2/cobranza/pagos",
		[]byte(`{"cargo_docto_cc_id":13458179,"cliente_id":2344886,`+
			`"cobrador":"RUTA 27 - ALEJANDRO CHAVARRIA","importe":"200.00"}`), "",
	)

	require.NotNil(t, got)
	require.NotNil(t, got.ClienteID, "sin este campo el listado no puede resolver el nombre")
	require.Equal(t, 2344886, *got.ClienteID)
	require.Equal(t, "2344886", got.Referencia, "aquí coinciden, y por eso hace falta la prueba de abajo")
}

// TestResumenExtractor_SinClienteElIDQuedaNiloAunqueLaReferenciaNoLoEste es la
// mitad que separa los dos campos. La referencia cae al cargo; el cliente_id
// NO puede caer con ella, o el listado buscaría el cargo 887766 en CLIENTES.
func TestResumenExtractor_SinClienteElIDQuedaNiloAunqueLaReferenciaNoLoEste(t *testing.T) {
	t.Parallel()

	got := failedintents.NewResumenExtractor().Extraer(
		"/v2/cobranza/pagos",
		[]byte(`{"cargo_docto_cc_id":887766,"cobrador":"Ana","importe":"100"}`), "",
	)

	require.NotNil(t, got)
	require.Equal(t, "887766", got.Referencia, "la referencia sí ancla al cargo")
	require.Nil(t, got.ClienteID, "pero el cargo NO es un cliente")
}

// Un cero tampoco es un cliente.
func TestResumenExtractor_ClienteIDEnCeroEsAusente(t *testing.T) {
	t.Parallel()

	got := failedintents.NewResumenExtractor().Extraer(
		"/v2/cobranza/pagos",
		[]byte(`{"cliente_id":0,"cargo_docto_cc_id":887766,"cobrador":"Ana","importe":"100"}`), "",
	)

	require.NotNil(t, got)
	require.Nil(t, got.ClienteID)
}
