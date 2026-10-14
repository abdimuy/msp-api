//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import (
	"sort"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
)

// Umbral de pausa (contrato §0): entero 5–240, 45 por omisión.
const (
	PausaMinPorOmision = 45
	PausaMinMinimo     = 5
	PausaMinMaximo     = 240
)

// Horas de la vista "Por hora": una entrada por hora 7..19 ([h, h+1)).
const (
	PrimeraHoraDelDia = 7
	UltimaHoraDelDia  = 19
)

// Horario es el horario opcional de la jornada.
type Horario struct {
	Desde HoraDelDia
	Hasta HoraDelDia
}

// ParametrosJornada lleva el umbral de pausa y horario opcional. Viajan en la consulta
// y nunca se guardan (D7).
type ParametrosJornada struct {
	PausaMin int
	Horario  *Horario
}

// NuevosParametrosJornada valida los parámetros crudos de la consulta:
// pausa_min vacío = 45; desde_hora y hasta_hora ambos o ninguno y desde < hasta.
func NuevosParametrosJornada(pausaMin, desdeHora, hastaHora string) (ParametrosJornada, error) {
	p := ParametrosJornada{PausaMin: PausaMinPorOmision}
	if pausaMin != "" {
		n, err := strconv.Atoi(pausaMin)
		if err != nil || n < PausaMinMinimo || n > PausaMinMaximo {
			return ParametrosJornada{}, ErrParametroInvalido.WithField("parametro", "pausa_min")
		}
		p.PausaMin = n
	}
	if (desdeHora == "") != (hastaHora == "") {
		return ParametrosJornada{}, ErrParametroInvalido.WithField("parametro", "desde_hora/hasta_hora")
	}
	if desdeHora == "" {
		return p, nil
	}
	d, err := ParseHoraDelDia(desdeHora)
	if err != nil {
		return ParametrosJornada{}, ErrParametroInvalido.WithField("parametro", "desde_hora")
	}
	h, err := ParseHoraDelDia(hastaHora)
	if err != nil {
		return ParametrosJornada{}, ErrParametroInvalido.WithField("parametro", "hasta_hora")
	}
	if d.Minutos() >= h.Minutos() {
		return ParametrosJornada{}, ErrParametroInvalido.WithField("parametro", "desde_hora/hasta_hora")
	}
	p.Horario = &Horario{Desde: d, Hasta: h}
	return p, nil
}

// Umbral devuelve el umbral de pausa como duración.
func (p ParametrosJornada) Umbral() time.Duration { return time.Duration(p.PausaMin) * time.Minute }

// Pausa es un hueco ≥ umbral entre dos actividades consecutivas del mismo día, o
// desde la última actividad hasta ahora si el día es hoy (EnCurso).
type Pausa struct {
	Desde       time.Time
	Hasta       time.Time
	Minutos     int
	AnteriorID  string
	SiguienteID string // "" en la pausa en curso
	EnCurso     bool
}

// ConteoImporte es `{n, importe}`.
type ConteoImporte struct {
	N       int
	Importe decimal.Decimal
}

func (c *ConteoImporte) sumar(importe decimal.Decimal) {
	c.N++
	c.Importe = c.Importe.Add(importe)
}

// Tramo agrupa actividades seguidas sin pausa entre ellas (vista Bloques).
type Tramo struct {
	Desde         time.Time
	Hasta         time.Time
	Pagos         ConteoImporte
	Visitas       int
	Condonaciones int
	Clientes      int
}

// Hora cuenta de [h, h+1) en la zona de negocio (vista Por hora).
type Hora struct {
	Hora          int
	Pagos         int
	Visitas       int
	Condonaciones int
	Importe       decimal.Decimal
}

// Punto es una marca de la línea con lo que pinta su popover (contrato §1):
// Importe nil en visitas; Resultado = literal de la visita, "" en pagos.
type Punto struct {
	ID            string
	Tipo          TipoActividad
	Hora          time.Time
	ClienteNombre string
	Importe       *decimal.Decimal
	Resultado     string
	NumFotos      int
}

// LineaDelDia es lo que pinta la mini barra y la línea de actividad.
type LineaDelDia struct {
	Dia     Fecha
	Puntos  []Punto
	Tramos  []Tramo
	PorHora []Hora
	Pausas  []Pausa
}

// ResumenPausas es `{n, minutos, en_curso}`.
type ResumenPausas struct {
	N       int
	Minutos int
	EnCurso bool
}

// ResumenJornada es el `resumen` del contrato §3.
type ResumenJornada struct {
	PrimeraActividad *time.Time
	UltimaActividad  *time.Time
	MinutosActivos   int
	Pausas           ResumenPausas
	Pagos            ConteoImporte
	Condonaciones    ConteoImporte
	NoAplicados      ConteoImporte
	Visitas          int
	Clientes         int
}

// Jornada reúne las actividades de un usuario en un día calendario de la zona de
// negocio, con pausas, línea, señales y resumen.
type Jornada struct {
	Dia         Fecha
	Actividades []Actividad
	Linea       LineaDelDia
	Senales     []Senal
	Resumen     ResumenJornada
}

// OrdenarActividades ordena por hora y, en empate, por id (orden total estable).
func OrdenarActividades(acts []Actividad) {
	sort.SliceStable(acts, func(i, j int) bool {
		if !acts[i].Hora.Equal(acts[j].Hora) {
			return acts[i].Hora.Before(acts[j].Hora)
		}
		return acts[i].ID < acts[j].ID
	})
}

// CalcularJornada arma la jornada de dia. acts puede traer actividades de
// otros días: se descartan. ahora decide la pausa en curso (solo si dia es hoy)
// y si "terminó temprano" ya puede evaluarse.
func CalcularJornada(acts []Actividad, p ParametrosJornada, dia Fecha, loc *time.Location, ahora time.Time) Jornada {
	delDia := make([]Actividad, 0, len(acts))
	for _, a := range acts {
		if DiaDe(a.Hora, loc) == dia {
			delDia = append(delDia, a)
		}
	}
	OrdenarActividades(delDia)
	esHoy := DiaDe(ahora, loc) == dia

	pausas := calcularPausas(delDia, p.Umbral(), esHoy, ahora)
	j := Jornada{Dia: dia, Actividades: delDia}
	j.Linea = LineaDelDia{
		Dia:     dia,
		Puntos:  puntos(delDia),
		Tramos:  calcularTramos(delDia, pausas),
		PorHora: calcularPorHora(delDia, loc),
		Pausas:  pausas,
	}
	j.Resumen = resumir(delDia, pausas)
	j.Senales = DetectarSenales(delDia, pausas, p, dia, loc, ahora)
	return j
}

func calcularPausas(acts []Actividad, umbral time.Duration, esHoy bool, ahora time.Time) []Pausa {
	pausas := []Pausa{}
	for i := 1; i < len(acts); i++ {
		prev, next := acts[i-1], acts[i]
		if next.Hora.Sub(prev.Hora) >= umbral {
			pausas = append(pausas, Pausa{
				Desde: prev.Hora, Hasta: next.Hora, Minutos: minutosEntre(prev.Hora, next.Hora),
				AnteriorID: prev.ID, SiguienteID: next.ID,
			})
		}
	}
	if esHoy && len(acts) > 0 {
		ultima := acts[len(acts)-1]
		if ahora.Sub(ultima.Hora) >= umbral {
			pausas = append(pausas, Pausa{
				Desde: ultima.Hora, Hasta: ahora, Minutos: minutosEntre(ultima.Hora, ahora),
				AnteriorID: ultima.ID, EnCurso: true,
			})
		}
	}
	return pausas
}

func puntos(acts []Actividad) []Punto {
	out := make([]Punto, 0, len(acts))
	for _, a := range acts {
		p := Punto{ID: a.ID, Tipo: a.Tipo, Hora: a.Hora, ClienteNombre: a.ClienteNombre, NumFotos: a.NumFotos}
		if a.Pago != nil {
			imp := a.Pago.Importe
			p.Importe = &imp
		}
		if a.Visita != nil {
			p.Resultado = a.Visita.TipoVisita
		}
		out = append(out, p)
	}
	return out
}

func calcularTramos(acts []Actividad, pausas []Pausa) []Tramo {
	cortes := map[string]bool{} // id de la actividad que abre un tramo nuevo
	for _, p := range pausas {
		if !p.EnCurso {
			cortes[p.SiguienteID] = true
		}
	}
	tramos := []Tramo{}
	var actual *Tramo
	clientes := map[int]bool{}
	cerrar := func() {
		if actual != nil {
			actual.Clientes = len(clientes)
			tramos = append(tramos, *actual)
		}
	}
	for _, a := range acts {
		if actual == nil || cortes[a.ID] {
			cerrar()
			actual = &Tramo{Desde: a.Hora, Pagos: ConteoImporte{Importe: decimal.Zero}}
			clientes = map[int]bool{}
		}
		actual.Hasta = a.Hora
		clientes[a.ClienteID] = true
		switch {
		case a.CuentaComoDinero():
			actual.Pagos.sumar(a.Importe())
		case a.Tipo == TipoVisita:
			actual.Visitas++
		case a.Tipo == TipoCondonacion:
			actual.Condonaciones++
		}
	}
	cerrar()
	return tramos
}

func calcularPorHora(acts []Actividad, loc *time.Location) []Hora {
	out := make([]Hora, 0, UltimaHoraDelDia-PrimeraHoraDelDia+1)
	for h := PrimeraHoraDelDia; h <= UltimaHoraDelDia; h++ {
		out = append(out, Hora{Hora: h, Importe: decimal.Zero})
	}
	for _, a := range acts {
		h := a.Hora.In(loc).Hour()
		if h < PrimeraHoraDelDia || h > UltimaHoraDelDia {
			continue
		}
		e := &out[h-PrimeraHoraDelDia]
		switch {
		case a.CuentaComoDinero():
			e.Pagos++
			e.Importe = e.Importe.Add(a.Importe())
		case a.Tipo == TipoVisita:
			e.Visitas++
		case a.Tipo == TipoCondonacion:
			e.Condonaciones++
		}
	}
	return out
}

func resumir(acts []Actividad, pausas []Pausa) ResumenJornada {
	r := ResumenJornada{
		Pagos:         ConteoImporte{Importe: decimal.Zero},
		Condonaciones: ConteoImporte{Importe: decimal.Zero},
		NoAplicados:   ConteoImporte{Importe: decimal.Zero},
	}
	if len(acts) == 0 {
		return r
	}
	primera, ultima := acts[0].Hora, acts[len(acts)-1].Hora
	r.PrimeraActividad, r.UltimaActividad = &primera, &ultima
	cerradas := 0
	for _, p := range pausas {
		r.Pausas.N++
		r.Pausas.Minutos += p.Minutos
		if p.EnCurso {
			r.Pausas.EnCurso = true
		} else {
			cerradas += p.Minutos
		}
	}
	// Las pausas cerradas caen entre primera y última: la resta nunca es negativa.
	r.MinutosActivos = minutosEntre(primera, ultima) - cerradas
	clientes := map[int]bool{}
	for _, a := range acts {
		clientes[a.ClienteID] = true
		switch {
		case a.CuentaComoDinero():
			r.Pagos.sumar(a.Importe())
		case a.NoAplicado():
			r.NoAplicados.sumar(a.Importe())
		case a.Tipo == TipoCondonacion:
			r.Condonaciones.sumar(a.Importe())
		case a.Tipo == TipoVisita:
			r.Visitas++
		}
	}
	r.Clientes = len(clientes)
	return r
}

// EstadoAhora es el estado del usuario en el tablero (contrato §2).
type EstadoAhora string

// Estados ahora.
const (
	EstadoAhoraActivo       EstadoAhora = "activo"
	EstadoAhoraSinActividad EstadoAhora = "sin_actividad"
	EstadoAhoraNoHaEmpezado EstadoAhora = "no_ha_empezado"
)

// CalcularEstadoAhora sobre la jornada de HOY: activo si la última actividad
// es de hace menos del umbral (desde = inicio del tramo actual); sin actividad
// si ya pasó (desde = última actividad); no ha empezado si hoy no hay nada.
func CalcularEstadoAhora(hoy Jornada, p ParametrosJornada, ahora time.Time) (EstadoAhora, *time.Time) {
	acts := hoy.Actividades
	if len(acts) == 0 {
		return EstadoAhoraNoHaEmpezado, nil
	}
	ultima := acts[len(acts)-1].Hora
	if ahora.Sub(ultima) >= p.Umbral() {
		return EstadoAhoraSinActividad, &ultima
	}
	desde := acts[0].Hora
	if n := len(hoy.Linea.Tramos); n > 0 {
		desde = hoy.Linea.Tramos[n-1].Desde
	}
	return EstadoAhoraActivo, &desde
}
