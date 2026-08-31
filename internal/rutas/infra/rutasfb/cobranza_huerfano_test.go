//nolint:misspell // vocabulario de rutas en español por convención del proyecto.
//nolint:paralleltest // serie: comparte la tx de rollback.
package rutasfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/microsipseed"
	"github.com/abdimuy/msp-api/internal/rutas/infra/rutasfb"
)

// TestCobranzaRepo_VentasPorZona_ClienteHuerfano fija el comportamiento del
// LEFT JOIN a CLIENTES en queryVentasPorZona.
//
// MSP_SALDOS_VENTAS no tiene FOREIGN KEY a CLIENTES —se comprobó en
// RDB$RELATION_CONSTRAINTS: sólo PK y NOT NULLs— y el filtro de la consulta es
// `s.ZONA_CLIENTE_ID`, que sale del propio caché, no del cliente. Una fila
// cuyo CLIENTE_ID ya no exista en CLIENTES por lo tanto SÍ entra al resultado,
// con `c.NOMBRE` en NULL. En la base de desarrollo hay 80 filas así.
//
// Si el destino de escaneo de esa columna es un `string` pelado, el driver
// devuelve "converting NULL to string is unsupported" y el endpoint completo
// de la zona se cae — no una fila, la lista entera. El destino tiene que ser
// sql.NullString, que es lo que el LEFT JOIN promete.
func TestCobranzaRepo_VentasPorZona_ClienteHuerfano(t *testing.T) { //nolint:paralleltest // serie: comparte la tx de rollback.
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		repo := rutasfb.NewCobranzaRepo(pool)

		zona := microsipseed.PrimeraZona(t, q)

		// Un CLIENTE_ID que con certeza no existe en CLIENTES.
		var maxCliente int
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(CLIENTE_ID), 0) FROM CLIENTES`).Scan(&maxCliente))
		huerfano := maxCliente + 987654

		var maxDocto int
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(DOCTO_CC_ID), 0) FROM MSP_SALDOS_VENTAS`).Scan(&maxDocto))
		doctoCC := maxDocto + 987654

		// Se escribe el caché directamente: es exactamente la forma que tiene
		// la fila huérfana en producción, y sembrarla por la cascada de ventas
		// exigiría borrar después un cliente con FKs colgando.
		ahora := firebird.ToWallClock(time.Now().UTC())
		_, err := q.ExecContext(ctx, `
INSERT INTO MSP_SALDOS_VENTAS
  (DOCTO_CC_ID, DOCTO_PV_ID, CLIENTE_ID, ZONA_CLIENTE_ID, FOLIO, FECHA_CARGO,
   PRECIO_TOTAL, TOTAL_IMPORTE, IMPTE_REST, NUM_PAGOS, SALDO, CARGO_CANCELADO,
   UPDATED_AT, TX_ID)
VALUES (?, NULL, ?, ?, 'HUERF-1', ?, 5000, 5000, 5000, 0, 5000, 'N', ?, 1)`,
			doctoCC, huerfano, zona, ahora, ahora)
		require.NoError(t, err, "sembrando la fila huérfana del caché")

		// Control positivo: la fila que acabamos de sembrar de verdad queda sin
		// cliente. Sin esto, un cambio de esquema haría pasar la prueba sin que
		// el caso llegue a ejercitarse.
		var sinCliente int
		require.NoError(t, q.QueryRowContext(ctx, `
SELECT COUNT(*) FROM MSP_SALDOS_VENTAS s
LEFT JOIN CLIENTES c ON c.CLIENTE_ID = s.CLIENTE_ID
WHERE s.DOCTO_CC_ID = ? AND c.CLIENTE_ID IS NULL`, doctoCC).Scan(&sinCliente))
		require.Equal(t, 1, sinCliente,
			"la fila sembrada debía quedar huérfana; si no, la prueba no está midiendo el NULL")

		desde := time.Now().UTC().AddDate(0, 0, -30)
		hasta := time.Now().UTC().AddDate(0, 0, 1)

		ventas, err := repo.VentasPorZona(ctx, zona, desde, hasta)
		require.NoError(t, err,
			"una fila del caché sin cliente no puede tumbar la zona entera: "+
				"c.NOMBRE sale NULL del LEFT JOIN y el destino de escaneo tiene que aceptarlo")

		var vista bool
		for _, v := range ventas {
			if v.VentaID == doctoCC {
				vista = true
				assert.Empty(t, v.ClienteNombre,
					"sin cliente el nombre debe quedar vacío, no basura")
			}
		}
		assert.True(t, vista, "la fila huérfana %d debía aparecer en el resultado", doctoCC)
	})
}
