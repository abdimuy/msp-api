//nolint:misspell // Spanish vocabulary by project convention.

// Package failedintents implementa, del lado de ventas, el puerto que la
// plataforma declara para conciliar intentos fallidos.
//
// El puerto va invertido a propósito: `internal/platform/failedintent` no
// puede —ni debe— saber qué es una venta. La captura es un mecanismo genérico
// que también sirve para los pagos; el día que aprenda de ventas deja de
// servir para lo demás. El que sí sabe dónde mirar es este módulo.
package failedintents

import (
	"context"
	"fmt"
	"strings"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// pathVentas es la única ruta por la que este checker responde. Una clave de
// idempotencia sólo es única dentro de su recurso, así que contestar por la
// ruta de otro módulo sería afirmar algo que no se sabe.
const pathVentas = "/v2/ventas"

// maxClavesPorConsulta acota el IN (...) de cada lote. El janitor puede
// entregar cientos de claves de una vez y Firebird tiene tope de parámetros
// por sentencia; sin trocear, el lote grande falla ENTERO y ninguna venta que
// ya aterrizó se cierra.
const maxClavesPorConsulta = 200

// ResolutionChecker contesta cuáles de las claves entregadas corresponden a
// ventas que ya existen.
//
// Por qué funciona sin una columna nueva: la app manda como
// `Idempotency-Key` el **id de la venta** que ella misma generó, y ese id es
// el que termina en MSP_VENTAS.ID cuando la venta entra. Verificado contra la
// base de desarrollo — de 12 intentos capturados en /v2/ventas, 4 tienen fila
// en MSP_VENTAS y 8 no: la consulta distingue, no responde que sí a todo.
type ResolutionChecker struct {
	pool *firebird.Pool
}

// Compile-time assertion: satisface el puerto de la plataforma.
var _ failedintent.ResolutionChecker = (*ResolutionChecker)(nil)

// NewResolutionChecker construye el checker sobre el pool dado.
func NewResolutionChecker(pool *firebird.Pool) *ResolutionChecker {
	return &ResolutionChecker{pool: pool}
}

// ResueltasEntre devuelve el subconjunto de claves que ya tienen venta.
//
// Rutas ajenas devuelven nil sin consultar: no es un error, es que la
// pregunta no es para este módulo.
func (c *ResolutionChecker) ResueltasEntre(
	ctx context.Context, path string, claves []string,
) ([]string, error) {
	if path != pathVentas || len(claves) == 0 {
		return nil, nil
	}

	resueltas := make([]string, 0, len(claves))
	err := firebird.RunInReadTx(ctx, c.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, c.pool.DB)
		for i := 0; i < len(claves); i += maxClavesPorConsulta {
			fin := min(i+maxClavesPorConsulta, len(claves))
			lote := claves[i:fin]

			marcadores := make([]string, len(lote))
			args := make([]any, len(lote))
			for n, clave := range lote {
				marcadores[n] = "?"
				args[n] = clave
			}
			//nolint:gosec // los marcadores son "?" generados aquí; los valores van por parámetro.
			query := `SELECT ID FROM MSP_VENTAS WHERE ID IN (` + strings.Join(marcadores, ",") + `)`

			rows, qerr := q.QueryContext(ctx, query, args...)
			if qerr != nil {
				return firebird.MapError(qerr)
			}
			for rows.Next() {
				var id string
				if scanErr := rows.Scan(&id); scanErr != nil {
					_ = rows.Close()
					return scanErr
				}
				// MSP_VENTAS.ID es CHAR(36) y rellena con espacios a la
				// derecha. Sin recortar, la clave que se devuelve no coincide
				// con la que el janitor va a buscar y el cierre no alcanza a
				// nadie — un fallo silencioso, sin error y sin efecto.
				resueltas = append(resueltas, strings.TrimSpace(id))
			}
			if rowsErr := rows.Err(); rowsErr != nil {
				_ = rows.Close()
				return rowsErr
			}
			_ = rows.Close()
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("ventas.failedintents: conciliar %d claves: %w", len(claves), err)
	}
	return resueltas, nil
}
