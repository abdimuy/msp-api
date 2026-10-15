//nolint:misspell // vocabulario español por convención del proyecto.
package desempenofb

import (
	"time"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// precisionTimestamp es la precisión con que el driver manda un parámetro
// TIMESTAMP: firebirdsql v0.9.19 (utils.go _convert_time) codifica
// nanosegundos/100000, o sea que TRUNCA a 100 µs.
const precisionTimestamp = 100 * time.Microsecond

// pared toma los campos de calendario y reloj de t (su hora de pared, en su
// propia zona) y los fija en UTC, para comparar horas de pared como números.
func pared(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
}

// parametroPared es lo que Firebird compara cuando recibe t como parámetro:
// firebird.ToWallClock(t) (hora de pared del negocio) truncada a 100 µs.
func parametroPared(t time.Time) time.Time {
	return pared(firebird.ToWallClock(t)).Truncate(precisionTimestamp)
}

// ventanaPared repite en Go el `COLUMNA >= ? AND COLUMNA <= ?` de una lectura
// por zona: compara la columna tal como la entrega el driver (hora de pared,
// sin convertir a UTC) contra los parámetros tal como los recibe Firebird.
// Así el reparto por lote deja dentro o fuera exactamente las mismas filas,
// también en una frontera con fracciones de 100 µs o en una hora ambigua de
// un cambio de horario.
type ventanaPared struct {
	desde, hasta time.Time
}

func nuevaVentanaPared(desde, hasta time.Time) ventanaPared {
	return ventanaPared{desde: parametroPared(desde), hasta: parametroPared(hasta)}
}

func (v ventanaPared) contiene(columna time.Time) bool {
	c := pared(columna)
	return !c.Before(v.desde) && !c.After(v.hasta)
}

// desdePared repite `COLUMNA >= ?`.
func desdePared(columna, desde time.Time) bool {
	return !pared(columna).Before(parametroPared(desde))
}
