//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import (
	"strconv"
	"time"
)

// FormatearDuracion devuelve "45 min", "1 h", "1 h 12 min" (texto de la pantalla).
func FormatearDuracion(minutos int) string {
	if minutos < 60 {
		return strconv.Itoa(minutos) + " min"
	}
	h, m := minutos/60, minutos%60
	if m == 0 {
		return strconv.Itoa(h) + " h"
	}
	return strconv.Itoa(h) + " h " + strconv.Itoa(m) + " min"
}

// horaLocal da "HH:MM" del instante en loc.
func horaLocal(t time.Time, loc *time.Location) string { return t.In(loc).Format("15:04") }

// minutosEntre cuenta los minutos completos (piso) desde a hasta b.
func minutosEntre(a, b time.Time) int { return int(b.Sub(a) / time.Minute) }
