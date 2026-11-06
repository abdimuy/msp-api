package garhttp_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// claveAbrirGPS is a stable idempotency key for the GPS tests of abrir.
const claveAbrirGPS = "6f1d2c3b-4a59-4e6f-8a7b-9c0d1e2f3a4b"

// abrirConCuerpo sends POST /garantias with the given body.
func abrirConCuerpo(t *testing.T, fakes *comandoFakes, cuerpo string) int {
	t.Helper()

	rec := enviarEjemplo(fakes.router, ejemploOperacion{
		OperationID: "abrir-garantia",
		Method:      http.MethodPost,
		Path:        "/garantias",
		ContentType: "application/json",
		Body:        []byte(cuerpo),
	}, claveAbrirGPS)
	require.NotEmpty(t, rec.Body.String())

	return rec.Code
}

// The agent's gps_lat/gps_lon is not the client's home: a piso folio has no
// address, so the agent's fix must not be read as one (that answered 422
// warranty_address_not_allowed to every piso folio sent with GPS).
func TestAbrirGarantia_PisoConGPSDelAgente_200(t *testing.T) {
	t.Parallel()

	fakes := nuevoRouterCompleto("abrir-garantia")

	code := abrirConCuerpo(t, fakes, `{
		"origen": "piso",
		"description": "Sala de exhibición rayada",
		"gps_lat": 19.4326,
		"gps_lon": -99.1332,
		"device_created_at": "`+deviceCreatedAtEjemplo+`"
	}`)

	require.Equal(t, http.StatusOK, code)
	require.Len(t, fakes.repositorio.creados, 1)

	creada := fakes.repositorio.creados[0]
	require.Equal(t, domain.OrigenFolioPiso, creada.Origen())
	require.Nil(t, creada.GPSLat())
	require.Nil(t, creada.GPSLon())

	assertGPSDelAgenteEnApertura(t, fakes)
}

// On a cliente folio the agent's fix stays on the opening event and the
// address keeps no GPS, since the body carries none for the home.
func TestAbrirGarantia_ClienteConGPSDelAgente_DomicilioSinGPS(t *testing.T) {
	t.Parallel()

	fakes := nuevoRouterCompleto("abrir-garantia")

	code := abrirConCuerpo(t, fakes, string(ejemploDe("abrir-garantia").Body))

	require.Equal(t, http.StatusOK, code)
	require.Len(t, fakes.repositorio.creados, 1)

	creada := fakes.repositorio.creados[0]
	require.Equal(t, domain.OrigenFolioCliente, creada.Origen())
	require.Equal(t, "Reforma", creada.Calle())
	require.Nil(t, creada.GPSLat(), "el GPS del agente no es el del domicilio")
	require.Nil(t, creada.GPSLon(), "el GPS del agente no es el del domicilio")

	assertGPSDelAgenteEnApertura(t, fakes)
}

// assertGPSDelAgenteEnApertura checks the agent's fix landed on folio_abierto.
func assertGPSDelAgenteEnApertura(t *testing.T, fakes *comandoFakes) {
	t.Helper()

	eventos := fakes.eventos.eventosConClave(claveAbrirGPS)
	require.Len(t, eventos, 1)
	require.Equal(t, domain.TipoEventoFolioAbierto, eventos[0].Tipo())
	require.NotNil(t, eventos[0].GPSLat())
	require.NotNil(t, eventos[0].GPSLon())
	require.InDelta(t, 19.4326, *eventos[0].GPSLat(), 1e-9)
	require.InDelta(t, -99.1332, *eventos[0].GPSLon(), 1e-9)
}
