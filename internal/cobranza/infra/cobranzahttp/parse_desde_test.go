//nolint:misspell // Spanish vocabulary by project convention.
package cobranzahttp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// `desde` acota una ventana de instantes, así que sólo se acepta un instante.
//
// Antes también se admitía `YYYY-MM-DD` y se interpretaba como medianoche
// **UTC**, que en la zona del negocio son las 18:00 del día anterior — medio
// turno corrido. Eso contradecía al propio estándar del proyecto
// (docs/module-standards/DATETIME_HANDLING.md), que fija que el día 13 anclado
// a CDMX empieza en `13T06:00:00Z`: dos respuestas distintas a la misma
// pregunta dentro de la misma base de código.
//
// No estaba rompiendo nada —la app manda `Instant.toString()`, que es RFC3339,
// y `desde` es siempre cota inferior, así que un valor temprano sólo ensancha—
// pero estaba **publicado en el OpenAPI**, esperando al primer cliente que lo
// usara.
//
// Se resuelve quitando la interpretación en vez de definiéndola, que es lo
// mismo que ya hacían los endpoints de reconcile (`parseReconcileDesde`,
// "porque la ventana debe ser determinista entre llamadas"). Quien tenga un
// día de calendario lo convierte a instante **donde sabe en qué zona está**,
// que es el cliente.
func TestParseDesde_RechazaFechaSinHora(t *testing.T) {
	t.Parallel()

	casos := []string{
		"2026-08-20",
		"2026-08-20 13:00:00", // sin T ni zona
		"20/08/2026",
		"2026-08-20T13:00:00", // sin zona: ambiguo, que es justo el problema
	}

	for _, raw := range casos {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			_, err := parseDesde(raw)
			require.Error(t, err, "%q no fija un instante; aceptarlo es adivinar la zona", raw)
			assert.ErrorIs(t, err, domain.ErrDesdeInvalido,
				"debe ser el error de dominio, para que el handler lo mapee a 422")
		})
	}
}

func TestParseDesde_AceptaRFC3339(t *testing.T) {
	t.Parallel()

	casos := map[string]string{
		"medianoche UTC":          "2026-08-20T00:00:00Z",
		"medianoche de negocio":   "2026-08-20T06:00:00Z",
		"con offset explicito":    "2026-08-20T00:00:00-06:00",
		"con fraccion de segundo": "2026-08-20T06:00:00.123456789Z",
	}

	for nombre, raw := range casos {
		t.Run(nombre, func(t *testing.T) {
			t.Parallel()
			got, err := parseDesde(raw)
			require.NoError(t, err)
			assert.Equal(t, time.UTC, got.Location(), "siempre se normaliza a UTC")
		})
	}
}

// El offset explícito no se pierde: `2026-08-20T00:00:00-06:00` es el MISMO
// instante que `2026-08-20T06:00:00Z`. Es la forma en que un cliente dice
// "medianoche en CDMX" sin que el servidor tenga que suponer nada.
func TestParseDesde_ElOffsetDelClienteSeRespeta(t *testing.T) {
	t.Parallel()

	conOffset, err := parseDesde("2026-08-20T00:00:00-06:00")
	require.NoError(t, err)
	enUTC, err := parseDesde("2026-08-20T06:00:00Z")
	require.NoError(t, err)

	assert.True(t, conOffset.Equal(enUTC),
		"medianoche en CDMX y 06:00Z son el mismo instante; got %s vs %s", conOffset, enUTC)
}

func TestParseDesde_VacioNoEsError(t *testing.T) {
	t.Parallel()

	// Vacío significa "no lo mandé": el servicio resuelve su ventana por
	// defecto. No es lo mismo que una fecha inválida.
	got, err := parseDesde("")
	require.NoError(t, err)
	assert.True(t, got.IsZero())
}
