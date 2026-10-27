//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import (
	"fmt"
	"time"
)

// Fecha es un día del calendario civil, sin hora ni zona. Se usa para el
// `fecha` de la jornada, para el `dia` de la línea y para las fechas de cita y
// promesa del juego de casos. Nunca se compara contra un time.Time sin pasar
// por DiaDe/InicioDelDia con la zona de negocio explícita.
type Fecha struct {
	anio int
	mes  time.Month
	dia  int
}

// NuevaFecha construye una Fecha y rechaza días que no existen (30 de febrero).
func NuevaFecha(anio int, mes time.Month, dia int) (Fecha, error) {
	t := time.Date(anio, mes, dia, 0, 0, 0, 0, time.UTC)
	if t.Year() != anio || t.Month() != mes || t.Day() != dia {
		return Fecha{}, ErrParametroInvalido.WithField("fecha", fmt.Sprintf("%04d-%02d-%02d", anio, mes, dia))
	}
	return Fecha{anio: anio, mes: mes, dia: dia}, nil
}

// ParseFecha lee "YYYY-MM-DD" (contrato §0). Cualquier otra forma es
// ErrParametroInvalido.
func ParseFecha(s string) (Fecha, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return Fecha{}, ErrParametroInvalido.WithField("fecha", s).WithError(err)
	}
	return Fecha{anio: t.Year(), mes: t.Month(), dia: t.Day()}, nil
}

// DiaDe devuelve el día del calendario en loc al que pertenece el instante t.
func DiaDe(t time.Time, loc *time.Location) Fecha {
	l := t.In(loc)
	return Fecha{anio: l.Year(), mes: l.Month(), dia: l.Day()}
}

// InicioDelDia es el instante (UTC) de las 00:00 de f en loc.
func (f Fecha) InicioDelDia(loc *time.Location) time.Time {
	return time.Date(f.anio, f.mes, f.dia, 0, 0, 0, 0, loc).UTC()
}

// Siguiente es el día calendario posterior.
func (f Fecha) Siguiente() Fecha {
	t := time.Date(f.anio, f.mes, f.dia+1, 0, 0, 0, 0, time.UTC)
	return Fecha{anio: t.Year(), mes: t.Month(), dia: t.Day()}
}

// Anio del día.
func (f Fecha) Anio() int { return f.anio }

// Mes del día.
func (f Fecha) Mes() time.Month { return f.mes }

// Dia del mes.
func (f Fecha) Dia() int { return f.dia }

// EsCero dice si es el valor cero (sin fecha).
func (f Fecha) EsCero() bool { return f.anio == 0 }

// String devuelve "YYYY-MM-DD".
func (f Fecha) String() string { return fmt.Sprintf("%04d-%02d-%02d", f.anio, f.mes, f.dia) }

func (f Fecha) clave() int { return f.anio*10000 + int(f.mes)*100 + f.dia }

// Antes dice si f es un día anterior a o.
func (f Fecha) Antes(o Fecha) bool { return f.clave() < o.clave() }

// Despues dice si f es un día posterior a o.
func (f Fecha) Despues(o Fecha) bool { return f.clave() > o.clave() }

// HoraDelDia es una hora de reloj "HH:MM" sin día ni zona: el horario opcional
// de la jornada (`desde_hora`, `hasta_hora`) y la hora de una cita.
type HoraDelDia struct {
	hora   int
	minuto int
}

// ParseHoraDelDia lee exactamente "HH:MM" con 00 ≤ HH ≤ 23 y 00 ≤ MM ≤ 59.
func ParseHoraDelDia(s string) (HoraDelDia, error) {
	if len(s) != len("15:04") {
		return HoraDelDia{}, ErrParametroInvalido.WithField("hora", s)
	}
	t, err := time.Parse("15:04", s)
	if err != nil {
		return HoraDelDia{}, ErrParametroInvalido.WithField("hora", s).WithError(err)
	}
	return HoraDelDia{hora: t.Hour(), minuto: t.Minute()}, nil
}

// Minutos desde la medianoche.
func (h HoraDelDia) Minutos() int { return h.hora*60 + h.minuto }

// EnDia es el instante (UTC) de h en el día f de loc.
func (h HoraDelDia) EnDia(f Fecha, loc *time.Location) time.Time {
	return time.Date(f.anio, f.mes, f.dia, h.hora, h.minuto, 0, 0, loc).UTC()
}

// String devuelve "HH:MM".
func (h HoraDelDia) String() string { return fmt.Sprintf("%02d:%02d", h.hora, h.minuto) }
