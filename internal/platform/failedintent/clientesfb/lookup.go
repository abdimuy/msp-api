// Package clientesfb resuelve nombres de cliente para el listado de intentos
// fallidos, leyendo CLIENTES de Microsip.
//
// Vive aquí, junto a failedintent, y no en internal/clientes, porque lo que
// necesita es una sola columna de una tabla legacy: no hay entidad de dominio
// que construir ni contrato de módulo que atravesar. Implementa el puerto
// failedintent/http.ClienteLookup.
package clientesfb

import (
	"context"
	"strings"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// maxIDsPorConsulta acota el IN (...) de cada viaje.
//
// El listado pagina, así que en la práctica una página trae bastante menos.
// El troceo está igualmente porque el tope de parámetros de un IN es la clase
// de límite que no avisa: no degrada, revienta la consulta entera.
const maxIDsPorConsulta = 200

// Lookup lee nombres de CLIENTES por id.
type Lookup struct {
	pool *firebird.Pool
}

// New construye el Lookup.
func New(pool *firebird.Pool) *Lookup { return &Lookup{pool: pool} }

// NombresPorID devuelve el nombre de cada id que exista. Los ids que no
// existen no aparecen en el mapa: no encontrarlos no es un error.
//
// NOMBRE es una columna legacy, así que se lee como cadena plana — la regla
// del repo para tablas de Microsip.
func (l *Lookup) NombresPorID(ctx context.Context, ids []int) (map[int]string, error) {
	out := make(map[int]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	q := firebird.GetQuerier(ctx, l.pool.DB)
	for inicio := 0; inicio < len(ids); inicio += maxIDsPorConsulta {
		fin := min(inicio+maxIDsPorConsulta, len(ids))
		trozo := ids[inicio:fin]

		marcadores := make([]string, len(trozo))
		args := make([]any, len(trozo))
		for i, id := range trozo {
			marcadores[i] = "?"
			args[i] = id
		}
		sql := "SELECT CLIENTE_ID, NOMBRE FROM CLIENTES WHERE CLIENTE_ID IN (" +
			strings.Join(marcadores, ",") + ")"

		rows, err := q.QueryContext(ctx, sql, args...)
		if err != nil {
			return nil, firebird.MapError(err)
		}
		for rows.Next() {
			var id int
			var nombre string
			if err := rows.Scan(&id, &nombre); err != nil {
				_ = rows.Close()
				return nil, firebird.MapError(err)
			}
			if n := strings.TrimSpace(nombre); n != "" {
				out[id] = n
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, firebird.MapError(err)
		}
		if err := rows.Close(); err != nil {
			return nil, firebird.MapError(err)
		}
	}
	return out, nil
}
