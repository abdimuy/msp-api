package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// La lista de rutas de creación NO puede ser una segunda lista.
//
// El listado de intentos fallidos acota por defecto a la etapa 1: la petición
// que no dejó fila en ninguna parte. Saber cuáles son esas rutas es
// exactamente saber qué prefijo captura cada módulo, y eso ya se declara una
// vez aquí. Si alguien registra un capturador nuevo y la lista del listado se
// arma aparte, el módulo nuevo se captura pero nunca se lista — y nada avisa.
func TestFailedIntentRutasRaiz_SonLosPrefijosDeLosCapturadores(t *testing.T) {
	t.Parallel()

	capturas := provideFailedIntentCapturas(nil, nil, nil, &config.Config{})

	esperadas := map[string]bool{}
	for _, c := range capturas.todas() {
		require.NotEmpty(t, c.PathPrefixes,
			"un capturador sin prefijo explícito deja su ruta raíz fuera del listado")
		for _, p := range c.PathPrefixes {
			esperadas[p] = true
		}
	}

	obtenidas := map[string]bool{}
	for _, r := range capturas.RutasRaiz() {
		obtenidas[r] = true
	}
	assert.Equal(t, esperadas, obtenidas)
}

// El ancla de regresión: hoy son estas tres. Si el día de mañana se captura un
// módulo más y esta prueba no se actualiza, es que la línea del capturador se
// escribió sin pensar en la pantalla.
func TestFailedIntentRutasRaiz_SonLasTresConocidas(t *testing.T) {
	t.Parallel()

	capturas := provideFailedIntentCapturas(nil, nil, nil, &config.Config{})

	assert.ElementsMatch(t,
		[]string{"/v2/ventas", "/v2/cobranza/pagos", "/v2/visitas"},
		capturas.RutasRaiz(),
	)
}

// El capturador de ventas debe declarar su prefijo, no heredarlo del default
// de la plataforma: un prefijo implícito no aparece en la unión y la pantalla
// se quedaría sin la etapa 1 del módulo más importante.
func TestFailedIntentCapturas_VentasDeclaraSuPrefijo(t *testing.T) {
	t.Parallel()

	capturas := provideFailedIntentCapturas(nil, nil, nil, &config.Config{})
	assert.Equal(t, []string{"/v2/ventas"}, capturas.Ventas.PathPrefixes)
}

// El Service del listado recibe esa misma unión.
func TestFailedIntentHTTPService_RecibeLasRutasRaiz(t *testing.T) {
	t.Parallel()

	capturas := provideFailedIntentCapturas(nil, nil, nil, &config.Config{})
	var store failedintent.Store
	svc := provideFailedIntentHTTPService(store, nil, nil, nil, capturas)
	require.NotNil(t, svc)
}
