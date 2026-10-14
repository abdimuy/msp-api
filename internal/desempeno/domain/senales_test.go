package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func senalesDe(js domain.Jornada, tipo domain.TipoSenal) []domain.Senal {
	var out []domain.Senal
	for _, s := range js.Senales {
		if s.Tipo == tipo {
			out = append(out, s)
		}
	}
	return out
}

func TestSenales_PausaLarga(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	p := params(t, "45", "", "")
	acts := []domain.Actividad{
		act("pago:1", domain.TipoPago, hl(t, "11:05:00"), 1),
		act("pago:2", domain.TipoPago, hl(t, "12:17:00"), 2),
	}
	j := domain.CalcularJornada(acts, p, dia14, loc, hl(t, "12:30:00"))
	s := senalesDe(j, domain.SenalPausaLarga)
	require.Len(t, s, 1)
	assert.Equal(t, []string{"pago:1", "pago:2"}, s[0].ActividadIDs)
	assert.Equal(t, "Pausa de 1 h 12 min (11:05–12:17)", s[0].Detalle)
	assert.Equal(t, "≥ 45 min sin registrar", s[0].Regla)

	j = domain.CalcularJornada(acts, p, dia14, loc, hl(t, "13:10:00"))
	s = senalesDe(j, domain.SenalPausaLarga)
	require.Len(t, s, 2)
	assert.Equal(t, "Pausa en curso de 53 min (desde 12:17)", s[1].Detalle)
	assert.Equal(t, []string{"pago:2"}, s[1].ActividadIDs)
}

func TestSenales_Horario(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	p := params(t, "45", "08:00", "18:00")
	finDelDia := hl(t, "18:00:00")
	casos := []struct {
		nombre                string
		primera, ultima       string
		ahora                 time.Time
		inicioTarde, finTempr bool
	}{
		{"empezó justo al umbral", "08:45:00", "17:30:00", finDelDia, true, false},
		{"un minuto antes del umbral", "08:44:00", "17:30:00", finDelDia, false, false},
		{"terminó justo al umbral", "08:10:00", "17:15:00", finDelDia, false, true},
		{"terminó un minuto después del umbral", "08:10:00", "17:16:00", finDelDia, false, false},
		{"antes de la hora de fin no se evalúa", "08:10:00", "12:00:00", hl(t, "17:59:00"), false, false},
	}
	for _, c := range casos {
		acts := []domain.Actividad{
			act("pago:1", domain.TipoPago, hl(t, c.primera), 1),
			act("pago:2", domain.TipoPago, hl(t, c.ultima), 2),
		}
		j := domain.CalcularJornada(acts, p, dia14, loc, c.ahora)
		assert.Len(t, senalesDe(j, domain.SenalInicioTarde), map[bool]int{true: 1, false: 0}[c.inicioTarde], c.nombre)
		assert.Len(t, senalesDe(j, domain.SenalFinTemprano), map[bool]int{true: 1, false: 0}[c.finTempr], c.nombre)
	}

	acts := []domain.Actividad{act("pago:1", domain.TipoPago, hl(t, "09:20:00"), 1)}
	j := domain.CalcularJornada(acts, p, dia14, loc, hl(t, "09:30:00"))
	s := senalesDe(j, domain.SenalInicioTarde)
	require.Len(t, s, 1)
	assert.Equal(t, "Primera actividad a las 09:20, 1 h 20 min después de las 08:00", s[0].Detalle)

	sinHorario := domain.CalcularJornada(acts, params(t, "45", "", ""), dia14, loc, hl(t, "09:30:00"))
	assert.Empty(t, senalesDe(sinHorario, domain.SenalInicioTarde), "sin horario no hay señal de horario")
}

// puntoA metros al norte del ancla (1° de latitud ≈ 111,195 m).
func alNorte(a domain.Actividad, metros float64) domain.Actividad {
	lat := *a.Lat + metros/111195.0
	a.Lat = &lat
	return a
}

func TestSenales_MismoPunto(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	p := params(t, "45", "", "")
	base := hl(t, "10:00:00")
	grupo := func(minutosUltimo time.Duration, metrosUltimo float64, clienteUltimo int) []domain.Actividad {
		return []domain.Actividad{
			act("pago:1", domain.TipoPago, base, 1),
			alNorte(act("pago:2", domain.TipoPago, base.Add(2*time.Minute), 2), 10),
			alNorte(act("pago:3", domain.TipoPago, base.Add(4*time.Minute), 3), 20),
			alNorte(act("pago:4", domain.TipoPago, base.Add(minutosUltimo), clienteUltimo), metrosUltimo),
		}
	}
	casos := []struct {
		nombre string
		acts   []domain.Actividad
		senal  bool
	}{
		{"4 clientes en 24 m y 6 min", grupo(6*time.Minute, 24, 4), true},
		{"el cuarto a 10 min exactos", grupo(10*time.Minute, 5, 4), true},
		{"el cuarto a 10 min 1 s", grupo(10*time.Minute+time.Second, 5, 4), false},
		{"el cuarto a 26 m", grupo(6*time.Minute, 26, 4), false},
		{"solo 3 clientes distintos", grupo(6*time.Minute, 5, 1), false},
	}
	for _, c := range casos {
		j := domain.CalcularJornada(c.acts, p, dia14, loc, base.Add(time.Hour))
		s := senalesDe(j, domain.SenalMismoPunto)
		if !c.senal {
			assert.Empty(t, s, c.nombre)
			continue
		}
		require.Len(t, s, 1, c.nombre)
		assert.Len(t, s[0].ActividadIDs, 4, c.nombre)
		assert.Equal(t, "≥ 4 clientes distintos en 25 m dentro de 10 min", s[0].Regla)
	}
	j := domain.CalcularJornada(grupo(6*time.Minute, 24, 4), p, dia14, loc, base.Add(time.Hour))
	assert.Equal(t, "4 cuentas desde el mismo punto en 6 min", senalesDe(j, domain.SenalMismoPunto)[0].Detalle)

	sinGPS := grupo(6*time.Minute, 5, 4)
	sinGPS[3].Lat, sinGPS[3].Lng = f64(0), f64(0)
	j = domain.CalcularJornada(sinGPS, p, dia14, loc, base.Add(time.Hour))
	assert.Empty(t, senalesDe(j, domain.SenalMismoPunto), "una actividad sin GPS no entra al grupo")
}

func TestSenales_CapturaTardiaYSinGPS(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	p := params(t, "45", "", "")
	a1 := act("pago:1", domain.TipoPago, hl(t, "10:00:00"), 1)
	llegada := a1.Hora.Add(2 * time.Hour)
	a1.RecibidoAt = &llegada
	a2 := act("pago:2", domain.TipoPago, hl(t, "10:10:00"), 2)
	casi := a2.Hora.Add(2*time.Hour - time.Minute)
	a2.RecibidoAt = &casi
	a3 := act("visita:c", domain.TipoVisita, hl(t, "10:20:00"), 3)
	a3.Lat, a3.Lng = f64(0), f64(0)
	a4 := act("visita:d", domain.TipoVisita, hl(t, "10:30:00"), 4)
	a4.Lat, a4.Lng = nil, nil

	j := domain.CalcularJornada([]domain.Actividad{a1, a2, a3, a4}, p, dia14, loc, hl(t, "11:00:00"))
	ct := senalesDe(j, domain.SenalCapturaTardia)
	require.Len(t, ct, 1, "2 h exactas sí; 1 h 59 min no")
	assert.Equal(t, []string{"pago:1"}, ct[0].ActividadIDs)
	assert.Equal(t, "Llegó al servidor 2 h después de capturarse", ct[0].Detalle)

	sg := senalesDe(j, domain.SenalSinGPS)
	require.Len(t, sg, 1)
	assert.Equal(t, []string{"visita:c", "visita:d"}, sg[0].ActividadIDs)
	assert.Equal(t, "2 actividades sin GPS", sg[0].Detalle)

	j = domain.CalcularJornada([]domain.Actividad{a3}, p, dia14, loc, hl(t, "11:00:00"))
	assert.Equal(t, "1 actividad sin GPS", senalesDe(j, domain.SenalSinGPS)[0].Detalle)
}

func TestDistanciaMetros(t *testing.T) {
	t.Parallel()
	d := domain.DistanciaMetros(18.4617, -97.3928, 18.4617+25/111195.0, -97.3928)
	assert.InDelta(t, 25.0, d, 0.05)
	assert.InDelta(t, 0.0, domain.DistanciaMetros(18.46, -97.39, 18.46, -97.39), 1e-9)
}
