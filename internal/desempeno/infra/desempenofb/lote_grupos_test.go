//nolint:misspell // vocabulario español por convención del proyecto.
package desempenofb_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/infra/desempenofb"
	"github.com/abdimuy/msp-api/internal/desempeno/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// desfasesEscalonados son los inicios del revisor de la compuerta 2: dentro y
// fuera de la tolerancia de agrupación (24 h), incluida una frontera de 150 µs.
var desfasesEscalonados = []time.Duration{
	0, 5 * time.Hour, 23*time.Hour + 59*time.Minute, 24 * time.Hour, 24*time.Hour + 150*time.Microsecond,
	30 * time.Hour, 47 * time.Hour, 49 * time.Hour, 3 * 24 * time.Hour, 5*24*time.Hour + 7*time.Hour,
}

// consultasEscalonadas: por zona, un inicio propio a 25 h del de la zona
// anterior (un grupo por zona: 48 grupos con todas las zonas de dev) y un
// segundo inicio escalonado más reciente.
func consultasEscalonadas(zonas []int, hasta time.Time) ([]outbound.ConsultaActividades, []outbound.ConsultaUniverso) {
	base := hasta.Add(-6 * 24 * time.Hour)
	var acts []outbound.ConsultaActividades
	var uni []outbound.ConsultaUniverso
	for i, z := range zonas {
		for _, d := range []time.Time{
			base.Add(-time.Duration(i) * 25 * time.Hour),
			base.Add(desfasesEscalonados[i%len(desfasesEscalonados)]),
		} {
			acts = append(acts, outbound.ConsultaActividades{ZonaID: z, Desde: d, Hasta: hasta})
			uni = append(uni, outbound.ConsultaUniverso{ZonaID: z, InicioSemana: d})
		}
	}
	return acts, uni
}

func zonasDeDev(t *testing.T, pool *firebird.Pool) []int {
	t.Helper()
	rows, err := pool.QueryContext(context.Background(),
		`SELECT DISTINCT ZONA_CLIENTE_ID FROM MSP_SALDOS_VENTAS WHERE ZONA_CLIENTE_ID IS NOT NULL ORDER BY 1`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var zs []int
	for rows.Next() {
		var z int
		require.NoError(t, rows.Scan(&z))
		zs = append(zs, z)
	}
	require.NoError(t, rows.Err())
	return zs
}

func comoJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// Con 48 grupos de inicio (uno por zona) una sola sentencia UNION ALL pasa
// del máximo de 256 contextos de Firebird ("Too many Contexts of
// Relation/Procedure/Views") y el tablero entero daría 500. El lote se parte
// en varias sentencias y el resultado es el mismo que consulta por consulta.
func TestActividadesYUniverso_CuarentaYOchoGruposEquivalenAConsultaPorConsulta(t *testing.T) { //nolint:paralleltest // base compartida.
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()
	zonas := zonasDeDev(t, pool)
	require.GreaterOrEqual(t, len(zonas), 30, "hacen falta muchas zonas para pasar del límite de contextos")
	consAct, consUni := consultasEscalonadas(zonas, equivAhora)

	ar := desempenofb.NewActividadesRepo(pool)
	lote, err := ar.ActividadesZonas(ctx, consAct)
	require.NoError(t, err)
	require.Len(t, lote, len(consAct))
	filas := 0
	for i, c := range consAct {
		una, err := ar.ActividadesZona(ctx, c.ZonaID, c.Desde, c.Hasta)
		require.NoError(t, err)
		require.Equal(t, comoJSON(t, una), comoJSON(t, lote[i]), "actividades zona %d desde %s", c.ZonaID, c.Desde)
		filas += len(una)
	}

	cr := desempenofb.NewCuentasRepo(pool)
	loteU, err := cr.UniversoZonas(ctx, consUni)
	require.NoError(t, err)
	require.Len(t, loteU, len(consUni))
	cuentas := 0
	for i, c := range consUni {
		una, err := cr.UniversoSemana(ctx, c.ZonaID, c.InicioSemana)
		require.NoError(t, err)
		require.Equal(t, comoJSON(t, una), comoJSON(t, loteU[i]), "universo zona %d inicio %s", c.ZonaID, c.InicioSemana)
		cuentas += len(una)
	}
	t.Logf("%d zonas, %d consultas: %d actividades y %d cuentas idénticas", len(zonas), len(consAct), filas, cuentas)
	require.Positive(t, filas)
	require.Positive(t, cuentas)
}
