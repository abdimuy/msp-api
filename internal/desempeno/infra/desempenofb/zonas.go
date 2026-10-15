//nolint:misspell // vocabulario español por convención del proyecto.
package desempenofb

import (
	"sort"
	"strings"
	"time"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// toleranciaGrupo: zonas cuyo desde más antiguo cae dentro de este margen del
// más antiguo del grupo se leen juntas desde el más antiguo del grupo (a lo
// más un día de más por zona, que el reparto en Go descarta).
const toleranciaGrupo = 24 * time.Hour

// grupoDesde son zonas que se leen con un mismo rango de fechas.
type grupoDesde struct {
	desde time.Time
	zonas []int
}

// agruparZonas recibe, por zona, el desde más antiguo que se le pide y arma
// grupos de zonas con desdes cercanos (toleranciaGrupo). El desde de cada
// grupo es el más antiguo de sus zonas. Orden determinista.
//
// Por qué grupos y no un solo rango: un solo usuario con la semana de hace 30
// días haría que las 44 zonas se leyeran 30 días. Y por qué no un OR por zona
// dentro del WHERE: medido (E-INF-014), con un usuario de 30 días los
// aplicados pasaron de 0.8 s a 15.9 s y el cobro de rutas a 29 s; con una rama
// UNION ALL por grupo, cada una con su rango limpio, 0.9 s.
func agruparZonas(desdePorZona map[int]time.Time) []grupoDesde {
	zonas := make([]int, 0, len(desdePorZona))
	for z := range desdePorZona {
		zonas = append(zonas, z)
	}
	sort.Slice(zonas, func(i, j int) bool {
		di, dj := desdePorZona[zonas[i]], desdePorZona[zonas[j]]
		if !di.Equal(dj) {
			return di.Before(dj)
		}
		return zonas[i] < zonas[j]
	})
	var out []grupoDesde
	for _, z := range zonas {
		d := desdePorZona[z]
		if n := len(out); n > 0 && d.Sub(out[n-1].desde) <= toleranciaGrupo {
			out[n-1].zonas = append(out[n-1].zonas, z)
			continue
		}
		out = append(out, grupoDesde{desde: d, zonas: []int{z}})
	}
	for i := range out {
		sort.Ints(out[i].zonas)
	}
	return out
}

// ramaGrupo es el filtro de una rama UNION ALL: las zonas del grupo y el
// rango de fechas [desde del grupo, hasta] sobre la columna del WHERE.
type ramaGrupo struct {
	filtro string
	args   []any
}

// ramasPorGrupo arma, por grupo,
//
//	colZona IN (?, ...) AND colFecha >= ? AND colFecha <= ?
func ramasPorGrupo(colZona, colFecha string, grupos []grupoDesde, hasta time.Time) []ramaGrupo {
	out := make([]ramaGrupo, 0, len(grupos))
	for _, g := range grupos {
		args := make([]any, 0, len(g.zonas)+2)
		for _, z := range g.zonas {
			args = append(args, z)
		}
		args = append(args, firebird.ToWallClock(g.desde), firebird.ToWallClock(hasta))
		out = append(out, ramaGrupo{
			filtro: colZona + " IN (" + marcadores(len(g.zonas)) + ")\n  AND " + colFecha + " >= ?\n  AND " + colFecha + " <= ?",
			args:   args,
		})
	}
	return out
}

// unirRamas junta una consulta por rama con UNION ALL y concatena sus
// argumentos en el mismo orden.
func unirRamas(ramas []ramaGrupo, consulta func(filtro string) string) (string, []any) {
	partes := make([]string, 0, len(ramas))
	var args []any
	for _, r := range ramas {
		partes = append(partes, consulta(r.filtro))
		args = append(args, r.args...)
	}
	return strings.Join(partes, "\nUNION ALL\n"), args
}

// limiteContextosFirebird es el máximo de contextos (cada aparición de una
// tabla, vista o procedimiento, también dentro de subconsultas) que Firebird
// admite en UNA sentencia; pasarlo da "Too many Contexts of
// Relation/Procedure/Views. Maximum allowed is 256" (medido en dev, FB 4.0.2:
// con 16 grupos los aplicados ya fallaban). Cada rama UNION ALL repite todos
// los contextos de la consulta, así que un lote se parte en sentencias de a lo
// más N ramas, con N calculado por consulta para quedar en la mitad del
// límite (margen para cambios futuros de la consulta).
const limiteContextosFirebird = 256

// ramasPorSentencia: cuántas ramas de una consulta caben en una sentencia
// dejando la mitad del límite de margen. contextosPorRama se cuenta a mano
// en cada consulta (tablas del FROM y los JOIN más las de cada subconsulta).
func ramasPorSentencia(contextosPorRama int) int {
	return max(1, limiteContextosFirebird/2/contextosPorRama)
}

// partir divide xs en tramos consecutivos de a lo más n elementos.
func partir[T any](xs []T, n int) [][]T {
	var out [][]T
	for len(xs) > n {
		out = append(out, xs[:n])
		xs = xs[n:]
	}
	if len(xs) > 0 {
		out = append(out, xs)
	}
	return out
}

func marcadores(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
