//nolint:misspell // Spanish vocabulary by project convention.

package failedintents_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/infra/failedintents"
)

func TestResumenExtractor_FormaValida(t *testing.T) {
	t.Parallel()

	cuerpo := []byte(`{
		"id": "6f1c9f0e-1111-4222-8333-444444444444",
		"cliente": {"nombre": "María Guadalupe Hernández"},
		"tipo_venta": "CREDITO",
		"montos": {"anual": "18000.00", "corto_plazo": "15500.75", "contado": "12000.00"}
	}`)

	got := failedintents.NewResumenExtractor().Extraer("/v2/ventas", cuerpo, "application/json")

	require.NotNil(t, got)
	require.Equal(t, "María Guadalupe Hernández", got.Titulo)
	require.Equal(t, "6f1c9f0e-1111-4222-8333-444444444444", got.Referencia)
	require.NotNil(t, got.Monto)
	require.Equal(t, "15500.75", got.Monto.String(), "a crédito manda el corto plazo")
	require.Empty(t, got.Modulo, "el módulo lo estampa el registro, no el extractor")
}

func TestResumenExtractor_ContadoTomaElPrecioDeContado(t *testing.T) {
	t.Parallel()

	cuerpo := []byte(`{
		"cliente": {"nombre": "Ana"},
		"tipo_venta": "CONTADO",
		"montos": {"anual": "18000.00", "corto_plazo": "15500.75", "contado": "12000.00"}
	}`)

	got := failedintents.NewResumenExtractor().Extraer("/v2/ventas", cuerpo, "application/json")
	require.NotNil(t, got.Monto)
	require.Equal(t, "12000", got.Monto.String())
}

// La precedencia salta los ceros: un contrato que manda "0.00" en corto plazo
// no debe convertir el renglón en "$0".
func TestResumenExtractor_SaltaLosMontosEnCero(t *testing.T) {
	t.Parallel()

	casos := []struct {
		nombre string
		cuerpo string
		quiero string
	}{
		{
			"crédito con corto plazo en cero cae al anual",
			`{"cliente":{"nombre":"A"},"tipo_venta":"CREDITO","montos":{"anual":"9000","corto_plazo":"0.00","contado":"7000"}}`,
			"9000",
		},
		{
			"crédito con los dos en cero cae al contado",
			`{"cliente":{"nombre":"A"},"tipo_venta":"CREDITO","montos":{"anual":"0","corto_plazo":"0","contado":"7000"}}`,
			"7000",
		},
		{
			"contado con contado en cero cae al corto plazo",
			`{"cliente":{"nombre":"A"},"tipo_venta":"CONTADO","montos":{"anual":"9000","corto_plazo":"8000","contado":"0"}}`,
			"8000",
		},
		{
			"negativo no cuenta como monto",
			`{"cliente":{"nombre":"A"},"tipo_venta":"CREDITO","montos":{"corto_plazo":"-5","anual":"9000"}}`,
			"9000",
		},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()
			got := failedintents.NewResumenExtractor().Extraer("/v2/ventas", []byte(c.cuerpo), "")
			require.NotNil(t, got.Monto)
			require.Equal(t, c.quiero, got.Monto.String())
		})
	}
}

// Los montos son cadenas por contrato, pero se han visto capturas con números
// desnudos. Perder el monto por una comilla sería un renglón peor por nada.
func TestResumenExtractor_AceptaMontoComoNumero(t *testing.T) {
	t.Parallel()

	cuerpo := []byte(`{"cliente":{"nombre":"Ana"},"tipo_venta":"CREDITO","montos":{"corto_plazo":15500.75}}`)
	got := failedintents.NewResumenExtractor().Extraer("/v2/ventas", cuerpo, "")
	require.NotNil(t, got.Monto)
	require.Equal(t, "15500.75", got.Monto.String())
}

func TestResumenExtractor_MontoIlegibleNoRompeElNombre(t *testing.T) {
	t.Parallel()

	casos := []string{
		`{"cliente":{"nombre":"Ana"},"montos":{"corto_plazo":"no soy un número"}}`,
		`{"cliente":{"nombre":"Ana"},"montos":{"corto_plazo":true}}`,
		`{"cliente":{"nombre":"Ana"},"montos":{"corto_plazo":null}}`,
		`{"cliente":{"nombre":"Ana"},"montos":{"corto_plazo":{"raro":1}}}`,
		`{"cliente":{"nombre":"Ana"},"montos":{"corto_plazo":"   "}}`,
		`{"cliente":{"nombre":"Ana"}}`,
	}
	for _, c := range casos {
		t.Run(c, func(t *testing.T) {
			t.Parallel()
			got := failedintents.NewResumenExtractor().Extraer("/v2/ventas", []byte(c), "")
			require.NotNil(t, got)
			require.Equal(t, "Ana", got.Titulo)
			require.Nil(t, got.Monto)
		})
	}
}

// Una forma ajena devuelve nil. Es la diferencia entre "no lo sé" y un nombre
// inventado, y en esta pantalla esa diferencia es la que decide si alguien
// vuelve a cobrarle a un cliente.
func TestResumenExtractor_FormaAjenaDevuelveNil(t *testing.T) {
	t.Parallel()

	ajenos := []string{
		`{}`,
		`{"otra":"forma"}`,
		`{"cliente":"no soy un objeto"}`,
		`{"cliente":{"telefono":"5551234567"}}`,
		`[]`,
		`null`,
		`123`,
		`"una cadena"`,
		`no es json`,
		``,
		`{"cliente":{"nombre":""}}`,
		`{"cliente":{"nombre":"   "}}`,
		`{"cliente":{"nombre":123}}`,
	}
	for _, c := range ajenos {
		t.Run(c, func(t *testing.T) {
			t.Parallel()
			require.Nil(t, failedintents.NewResumenExtractor().Extraer("/v2/ventas", []byte(c), ""))
		})
	}
}

// Un cuerpo sin nombre pero con id sigue siendo una venta reconocible: el
// renglón se degrada a mostrar la referencia en vez de desaparecer.
func TestResumenExtractor_SinNombrePeroConID(t *testing.T) {
	t.Parallel()

	got := failedintents.NewResumenExtractor().Extraer(
		"/v2/ventas", []byte(`{"id":"abc-123","montos":{"corto_plazo":"100"}}`), "",
	)
	require.NotNil(t, got)
	require.Empty(t, got.Titulo)
	require.Equal(t, "abc-123", got.Referencia)
	require.Equal(t, "100", got.Monto.String())
}

func TestResumenExtractor_RecortaLosEspacios(t *testing.T) {
	t.Parallel()

	got := failedintents.NewResumenExtractor().Extraer(
		"/v2/ventas", []byte(`{"id":"  x  ","cliente":{"nombre":"  Ana Pérez \n"}}`), "",
	)
	require.Equal(t, "Ana Pérez", got.Titulo)
	require.Equal(t, "x", got.Referencia)
}

// FuzzResumenExtractor: ningún cuerpo arbitrario puede hacerlo entrar en
// pánico ni producir un título de la nada. Corre dentro de la petición del
// vendedor; un pánico aquí es una venta perdida.
func FuzzResumenExtractor(f *testing.F) {
	f.Add(`{"cliente":{"nombre":"Ana"},"montos":{"corto_plazo":"1"}}`)
	f.Add(`{"id":"x"}`)
	f.Add(`{}`)
	f.Add(`[1,2,3]`)
	f.Add(`{"montos":{"anual":1e309}}`)
	f.Add("\x00\xff")

	ex := failedintents.NewResumenExtractor()
	f.Fuzz(func(t *testing.T, cuerpo string) {
		got := ex.Extraer("/v2/ventas", []byte(cuerpo), "application/json")
		if got == nil {
			return
		}
		// Contrato duro: si devolvió algo, ese algo salió del cuerpo. Nunca
		// estampa el módulo (eso es del registro) y nunca devuelve un resumen
		// completamente vacío disfrazado de dato.
		require.Empty(t, got.Modulo)
		require.False(t, got.Vacio(), "un resumen sin datos debe ser nil, no vacío")

		// Y el título, cuando existe, salió de una llave `cliente` del cuerpo.
		// La comparación es insensible a mayúsculas porque encoding/json lo
		// es: `{"Cliente":{"NOMBRE":"x"}}` también casa. Es tolerancia del
		// stdlib, no nuestra, y conviene — un cliente que cambie el casing no
		// debería apagar el nombre de todos los renglones.
		if got.Titulo != "" {
			var sonda map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(cuerpo), &sonda),
				"un título sólo puede salir de un objeto JSON válido")
			tieneCliente := false
			for k := range sonda {
				if strings.EqualFold(k, "cliente") {
					tieneCliente = true
					break
				}
			}
			require.True(t, tieneCliente, "título sin llave cliente: %q", cuerpo)
		}
	})
}
