//nolint:misspell // Spanish vocabulary by project convention.
package ventfb

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestVentaFromClause_TodoEnriquecimientoEsLeftJoin fija que ninguna tabla de
// enriquecimiento pueda descartar una fila del caché.
//
// Por qué es una prueba sobre el SQL y no de integración: hoy el conjunto que
// devuelve la consulta no cambia si CLIENTES va por INNER o por LEFT, porque
// ventaClienteFilter ya exige por EXISTS que el cliente exista y esté en 'A'
// —una condición estrictamente más fuerte que el JOIN—. No hay dato que
// sembrar que distinga las dos versiones a través del repositorio.
//
// Lo que sí se puede fijar es la forma, y es la que importa: la lápida de una
// venta borrada es la única señal con la que el teléfono la elimina, y su
// entrega no puede quedar colgando de un JOIN de adorno. El precedente ya
// costó el mismo defecto en pagos (ver el comentario de pagoFromClause).
func TestVentaFromClause_TodoEnriquecimientoEsLeftJoin(t *testing.T) {
	t.Parallel()

	for _, line := range strings.Split(ventaFromClause, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "FROM ") {
			continue
		}
		require.Truef(t, strings.HasPrefix(trimmed, "LEFT JOIN "),
			"toda tabla de enriquecimiento debe ir por LEFT JOIN o una lápida sin "+
				"cliente/zona/contrato deja de viajar y la venta se queda para siempre "+
				"en el teléfono; línea ofensora: %q", trimmed)
	}
}

// TestVentaStatusFilter_CubreLaLapidaHuerfana fija las dos mitades de la rama
// de cancelados y sus binds.
//
// La de cancelación lleva ventana (FECHA_HORA_CANCELACION >= ?) porque el
// UPDATED_AT del caché lo mueve cualquier backfill. La de borrado físico NO
// la lleva: no hay fecha que consultar cuando la fila de DOCTOS_CC ya no
// existe. Si alguien "simetriza" las dos ramas, esta prueba se pone roja
// antes de que 29 ventas fantasma vuelvan a las rutas.
func TestVentaStatusFilter_CubreLaLapidaHuerfana(t *testing.T) {
	t.Parallel()

	require.Contains(t, ventaStatusFilterConVentana, "NOT EXISTS",
		"falta la rama del borrado físico: sin fila en DOCTOS_CC el EXISTS de cancelación "+
			"nunca es cierto y la lápida no sale")
	require.Equal(t, 2, strings.Count(ventaStatusFilterConVentana, "?"),
		"el predicado toma exactamente dos binds, ambos `desde`; la rama de borrado "+
			"físico no agrega ninguno porque no lleva ventana")
}
