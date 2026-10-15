//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutasfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/rutas/infra/rutasfb"
	"github.com/abdimuy/msp-api/internal/rutas/ports/outbound"
)

// Con 48 grupos de inicio (uno por zona) una sola sentencia UNION ALL pasa
// del máximo de 256 contextos de Firebird. El lote se parte en varias
// sentencias y da lo mismo que VentasPorZona consulta por consulta.
func TestCobranzaRepo_VentasPorZonas_CuarentaYOchoGrupos(t *testing.T) { //nolint:paralleltest // base compartida.
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()
	repo := rutasfb.NewCobranzaRepo(pool)
	zonas := todasLasZonas(t, pool)
	require.GreaterOrEqual(t, len(zonas), 30)
	desfases := []time.Duration{
		0, 5 * time.Hour, 23*time.Hour + 59*time.Minute, 24 * time.Hour, 24*time.Hour + 150*time.Microsecond,
		30 * time.Hour, 47 * time.Hour, 49 * time.Hour, 3 * 24 * time.Hour, 5*24*time.Hour + 7*time.Hour,
	}
	base := loteAhora.Add(-6 * 24 * time.Hour)
	var consultas []outbound.ConsultaVentasZona
	for i, z := range zonas {
		consultas = append(consultas,
			outbound.ConsultaVentasZona{ZonaID: z, Desde: base.Add(-time.Duration(i) * 25 * time.Hour)},
			outbound.ConsultaVentasZona{ZonaID: z, Desde: base.Add(desfases[i%len(desfases)])})
	}
	lote, err := repo.VentasPorZonas(ctx, consultas, loteAhora)
	require.NoError(t, err)
	require.Len(t, lote, len(consultas))
	filas := 0
	for i, c := range consultas {
		una, err := repo.VentasPorZona(ctx, c.ZonaID, c.Desde, loteAhora)
		require.NoError(t, err)
		require.Equal(t, describirVentas(una), describirVentas(lote[i]), "zona %d desde %s", c.ZonaID, c.Desde)
		filas += len(una)
	}
	t.Logf("%d zonas, %d consultas, %d filas idénticas", len(zonas), len(consultas), filas)
}
