//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import (
	"math"
	"strconv"
	"time"
)

// Umbrales de las señales "para revisar" (spec §4.2), con nombre.
const (
	MismoPuntoMinClientes = 4
	MismoPuntoRadioMetros = 25.0
	MismoPuntoVentana     = 10 * time.Minute
	radioTierraMetros     = 6371000.0
	reglaMismoPunto       = "≥ 4 clientes distintos en 25 m dentro de 10 min"
	reglaCapturaTardia    = "≥ 2 h entre la captura y la llegada al servidor"
	reglaSinGPS           = "coordenadas 0,0 o ausentes"
)

// TipoSenal es uno de los seis tipos del contrato §1.
type TipoSenal string

// Tipos de señal.
const (
	SenalPausaLarga    TipoSenal = "pausa_larga"
	SenalInicioTarde   TipoSenal = "inicio_tarde"
	SenalFinTemprano   TipoSenal = "fin_temprano"
	SenalMismoPunto    TipoSenal = "mismo_punto"
	SenalCapturaTardia TipoSenal = "captura_tardia"
	SenalSinGPS        TipoSenal = "sin_gps"
)

// Senal es algo "para revisar", nunca una sanción.
type Senal struct {
	Tipo         TipoSenal
	ActividadIDs []string
	Desde        *time.Time
	Hasta        *time.Time
	Detalle      string
	Regla        string
}

// DetectarSenales corre todas las reglas sobre las actividades del día (ya
// ordenadas) y sus pausas.
func DetectarSenales(acts []Actividad, pausas []Pausa, p ParametrosJornada, dia Fecha, loc *time.Location, ahora time.Time) []Senal {
	out := []Senal{}
	out = append(out, senalesDePausa(pausas, p, loc)...)
	out = append(out, senalesDeHorario(acts, p, dia, loc, ahora)...)
	out = append(out, senalesMismoPunto(acts)...)
	out = append(out, senalesCapturaTardia(acts)...)
	if s, ok := senalSinGPS(acts); ok {
		out = append(out, s)
	}
	return out
}

func ptr(t time.Time) *time.Time { return &t }

func idsNoVacios(ids ...string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

func senalesDePausa(pausas []Pausa, p ParametrosJornada, loc *time.Location) []Senal {
	regla := "≥ " + strconv.Itoa(p.PausaMin) + " min sin registrar"
	out := make([]Senal, 0, len(pausas))
	for _, pa := range pausas {
		detalle := "Pausa de " + FormatearDuracion(pa.Minutos) +
			" (" + horaLocal(pa.Desde, loc) + "–" + horaLocal(pa.Hasta, loc) + ")"
		if pa.EnCurso {
			detalle = "Pausa en curso de " + FormatearDuracion(pa.Minutos) + " (desde " + horaLocal(pa.Desde, loc) + ")"
		}
		out = append(out, Senal{
			Tipo: SenalPausaLarga, ActividadIDs: idsNoVacios(pa.AnteriorID, pa.SiguienteID),
			Desde: ptr(pa.Desde), Hasta: ptr(pa.Hasta), Detalle: detalle, Regla: regla,
		})
	}
	return out
}

// senalesDeHorario: solo con horario y con al menos una actividad. "Empezó
// tarde": primera − desde_hora ≥ umbral. "Terminó temprano": hasta_hora −
// última ≥ umbral, y solo cuando hasta_hora ya pasó (si no, el día sigue).
func senalesDeHorario(acts []Actividad, p ParametrosJornada, dia Fecha, loc *time.Location, ahora time.Time) []Senal {
	if p.Horario == nil || len(acts) == 0 {
		return nil
	}
	out := []Senal{}
	inicio := p.Horario.Desde.EnDia(dia, loc)
	fin := p.Horario.Hasta.EnDia(dia, loc)
	primera, ultima := acts[0], acts[len(acts)-1]
	if primera.Hora.Sub(inicio) >= p.Umbral() {
		out = append(out, Senal{
			Tipo: SenalInicioTarde, ActividadIDs: []string{primera.ID},
			Desde: ptr(inicio), Hasta: ptr(primera.Hora),
			Detalle: "Primera actividad a las " + horaLocal(primera.Hora, loc) + ", " +
				FormatearDuracion(minutosEntre(inicio, primera.Hora)) + " después de las " + p.Horario.Desde.String(),
			Regla: "≥ " + strconv.Itoa(p.PausaMin) + " min después de la hora de inicio",
		})
	}
	if !ahora.Before(fin) && fin.Sub(ultima.Hora) >= p.Umbral() {
		out = append(out, Senal{
			Tipo: SenalFinTemprano, ActividadIDs: []string{ultima.ID},
			Desde: ptr(ultima.Hora), Hasta: ptr(fin),
			Detalle: "Última actividad a las " + horaLocal(ultima.Hora, loc) + ", " +
				FormatearDuracion(minutosEntre(ultima.Hora, fin)) + " antes de las " + p.Horario.Hasta.String(),
			Regla: "≥ " + strconv.Itoa(p.PausaMin) + " min antes de la hora de fin",
		})
	}
	return out
}

// DistanciaMetros calcula con haversine la distancia entre dos coordenadas.
func DistanciaMetros(lat1, lng1, lat2, lng2 float64) float64 {
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * radioTierraMetros * math.Asin(math.Sqrt(a))
}

// senalesMismoPunto: ancla en cada actividad con GPS; junta las siguientes
// (con GPS) a ≤ 10 min y ≤ 25 m del ancla; si hay ≥ 4 clientes distintos, una
// señal y se salta lo ya incluido (las señales no se traslapan).
func senalesMismoPunto(acts []Actividad) []Senal {
	conGPS := make([]Actividad, 0, len(acts))
	for _, a := range acts {
		if a.ConGPS() {
			conGPS = append(conGPS, a)
		}
	}
	out := []Senal{}
	for i := 0; i < len(conGPS); {
		ancla := conGPS[i]
		grupo := []Actividad{ancla}
		ultimo := i
		for k := i + 1; k < len(conGPS) && conGPS[k].Hora.Sub(ancla.Hora) <= MismoPuntoVentana; k++ {
			if DistanciaMetros(*ancla.Lat, *ancla.Lng, *conGPS[k].Lat, *conGPS[k].Lng) <= MismoPuntoRadioMetros {
				grupo = append(grupo, conGPS[k])
				ultimo = k
			}
		}
		clientes := map[int]bool{}
		for _, a := range grupo {
			clientes[a.ClienteID] = true
		}
		if len(clientes) < MismoPuntoMinClientes {
			i++
			continue
		}
		ids := make([]string, 0, len(grupo))
		for _, a := range grupo {
			ids = append(ids, a.ID)
		}
		desde, hasta := grupo[0].Hora, grupo[len(grupo)-1].Hora
		out = append(out, Senal{
			Tipo: SenalMismoPunto, ActividadIDs: ids, Desde: ptr(desde), Hasta: ptr(hasta),
			Detalle: strconv.Itoa(len(clientes)) + " cuentas desde el mismo punto en " +
				FormatearDuracion(minutosEntre(desde, hasta)),
			Regla: reglaMismoPunto,
		})
		i = ultimo + 1
	}
	return out
}

func senalesCapturaTardia(acts []Actividad) []Senal {
	out := []Senal{}
	for _, a := range acts {
		if a.RecibidoAt == nil {
			continue
		}
		atraso := a.RecibidoAt.Sub(a.Hora)
		if atraso < CapturaTardiaUmbral {
			continue
		}
		out = append(out, Senal{
			Tipo: SenalCapturaTardia, ActividadIDs: []string{a.ID},
			Desde: ptr(a.Hora), Hasta: ptr(*a.RecibidoAt),
			Detalle: "Llegó al servidor " + FormatearDuracion(int(atraso/time.Minute)) + " después de capturarse",
			Regla:   reglaCapturaTardia,
		})
	}
	return out
}

func senalSinGPS(acts []Actividad) (Senal, bool) {
	var sin []Actividad
	for _, a := range acts {
		if !a.ConGPS() {
			sin = append(sin, a)
		}
	}
	if len(sin) == 0 {
		return Senal{}, false
	}
	ids := make([]string, 0, len(sin))
	for _, a := range sin {
		ids = append(ids, a.ID)
	}
	detalle := strconv.Itoa(len(sin)) + " actividades sin GPS"
	if len(sin) == 1 {
		detalle = "1 actividad sin GPS"
	}
	return Senal{
		Tipo: SenalSinGPS, ActividadIDs: ids, Desde: ptr(sin[0].Hora), Hasta: ptr(sin[len(sin)-1].Hora),
		Detalle: detalle, Regla: reglaSinGPS,
	}, true
}
