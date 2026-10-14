package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// hl construye un instante a la hora local de CDMX del 2026-10-14.
func hl(t *testing.T, hhmm string) time.Time {
	t.Helper()
	tt, err := time.ParseInLocation("2006-01-02 15:04:05", "2026-10-14 "+hhmm, cdmx(t))
	require.NoError(t, err)
	return tt.UTC()
}

func act(id string, tipo domain.TipoActividad, h time.Time, cliente int) domain.Actividad {
	a := domain.Actividad{
		ID: id, Tipo: tipo, Hora: h, ClienteID: cliente,
		Lat: f64(18.4617), Lng: f64(-97.3928),
	}
	switch tipo {
	case domain.TipoPago:
		a.Pago = &domain.DatosPago{Importe: dinero("150.00"), FormaCobroID: 157, Aplicado: true}
	case domain.TipoCondonacion:
		a.Pago = &domain.DatosPago{Importe: dinero("50.00"), FormaCobroID: 137026, Aplicado: true}
	case domain.TipoVisita:
		a.Visita = &domain.DatosVisita{TipoVisita: domain.TipoVisitaNoSeEncontraba}
	}
	return a
}

func params(t *testing.T, pausa, desde, hasta string) domain.ParametrosJornada {
	t.Helper()
	p, err := domain.NuevosParametrosJornada(pausa, desde, hasta)
	require.NoError(t, err)
	return p
}

var dia14 = func() domain.Fecha { f, _ := domain.ParseFecha("2026-10-14"); return f }()

func TestNuevosParametrosJornada(t *testing.T) {
	t.Parallel()
	p := params(t, "", "", "")
	assert.Equal(t, 45, p.PausaMin)
	assert.Nil(t, p.Horario)
	assert.Equal(t, 45*time.Minute, p.Umbral())

	p = params(t, "5", "08:00", "18:00")
	assert.Equal(t, 5, p.PausaMin)
	require.NotNil(t, p.Horario)
	assert.Equal(t, "08:00", p.Horario.Desde.String())

	assert.Equal(t, 240, params(t, "240", "", "").PausaMin)

	malos := [][3]string{
		{"4", "", ""},
		{"241", "", ""},
		{"abc", "", ""},
		{"45.5", "", ""},
		{"", "08:00", ""},
		{"", "", "18:00"},
		{"", "18:00", "08:00"},
		{"", "08:00", "08:00"},
		{"", "8:00", "18:00"},
		{"", "08:00", "25:00"},
	}
	for _, m := range malos {
		_, err := domain.NuevosParametrosJornada(m[0], m[1], m[2])
		require.ErrorIs(t, err, domain.ErrParametroInvalido, "entrada %v", m)
	}
}

func TestCalcularJornada_UmbralExacto(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	ahora := hl(t, "23:00:00")
	p := params(t, "45", "", "")
	casos := []struct {
		nombre string
		hueco  time.Duration
		pausas int
	}{
		{"44 min no es pausa", 44 * time.Minute, 0},
		{"44 min 59 s no es pausa", 44*time.Minute + 59*time.Second, 0},
		{"45 min exactos sí es pausa", 45 * time.Minute, 1},
		{"46 min es pausa", 46 * time.Minute, 1},
	}
	for _, c := range casos {
		a1 := act("pago:1", domain.TipoPago, hl(t, "10:00:00"), 1)
		a2 := act("pago:2", domain.TipoPago, hl(t, "10:00:00").Add(c.hueco), 2)
		// el día ya terminó para la pausa en curso: se evalúa con "ahora" de otro día
		j := domain.CalcularJornada([]domain.Actividad{a2, a1}, p, dia14, loc, ahora.Add(24*time.Hour))
		require.Len(t, j.Linea.Pausas, c.pausas, c.nombre)
		if c.pausas == 1 {
			pa := j.Linea.Pausas[0]
			assert.Equal(t, "pago:1", pa.AnteriorID, c.nombre)
			assert.Equal(t, "pago:2", pa.SiguienteID, c.nombre)
			assert.Equal(t, int(c.hueco/time.Minute), pa.Minutos, c.nombre)
			assert.False(t, pa.EnCurso, c.nombre)
		}
	}
}

func TestCalcularJornada_DiaVacioYUnaSolaActividad(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	p := params(t, "", "", "")
	ayer := hl(t, "12:00:00").Add(24 * time.Hour)

	vacio := domain.CalcularJornada(nil, p, dia14, loc, ayer)
	assert.Empty(t, vacio.Actividades)
	assert.Empty(t, vacio.Linea.Pausas)
	assert.Empty(t, vacio.Linea.Tramos)
	assert.Len(t, vacio.Linea.PorHora, 13)
	assert.Nil(t, vacio.Resumen.PrimeraActividad)
	assert.Equal(t, 0, vacio.Resumen.MinutosActivos)
	assert.Empty(t, vacio.Senales)

	una := domain.CalcularJornada([]domain.Actividad{act("pago:1", domain.TipoPago, hl(t, "10:00:00"), 1)}, p, dia14, loc, ayer)
	assert.Empty(t, una.Linea.Pausas)
	require.Len(t, una.Linea.Tramos, 1)
	assert.Equal(t, 0, una.Resumen.MinutosActivos)
	assert.Equal(t, 1, una.Resumen.Pagos.N)
}

func TestCalcularJornada_MedianocheParteLosDias(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	p := params(t, "", "", "")
	noche := act("pago:1", domain.TipoPago, hl(t, "23:50:00"), 1)
	madrugada := act("pago:2", domain.TipoPago, hl(t, "23:50:00").Add(20*time.Minute), 2) // 00:10 del 15
	j := domain.CalcularJornada([]domain.Actividad{noche, madrugada}, p, dia14, loc, hl(t, "23:59:00").Add(48*time.Hour))
	require.Len(t, j.Actividades, 1, "la de las 00:10 es del día 15")
	assert.Equal(t, "pago:1", j.Actividades[0].ID)
	assert.Empty(t, j.Linea.Pausas)
}

func TestCalcularJornada_PausaEnCursoSoloHoy(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	p := params(t, "45", "", "")
	acts := []domain.Actividad{act("pago:1", domain.TipoPago, hl(t, "10:00:00"), 1)}

	j := domain.CalcularJornada(acts, p, dia14, loc, hl(t, "10:45:00"))
	require.Len(t, j.Linea.Pausas, 1)
	assert.True(t, j.Linea.Pausas[0].EnCurso)
	assert.Empty(t, j.Linea.Pausas[0].SiguienteID)
	assert.Equal(t, 45, j.Linea.Pausas[0].Minutos)
	assert.True(t, j.Resumen.Pausas.EnCurso)

	j = domain.CalcularJornada(acts, p, dia14, loc, hl(t, "10:44:00"))
	assert.Empty(t, j.Linea.Pausas, "44 min después todavía no es pausa")

	j = domain.CalcularJornada(acts, p, dia14, loc, hl(t, "10:00:00").Add(30*time.Hour))
	assert.Empty(t, j.Linea.Pausas, "un día pasado no tiene pausa en curso")
}

func TestCalcularJornada_TramosPorHoraYResumen(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	p := params(t, "45", "", "")
	acts := []domain.Actividad{
		act("pago:1", domain.TipoPago, hl(t, "06:59:00"), 1),
		act("visita:a", domain.TipoVisita, hl(t, "07:10:00"), 2),
		act("pago:2", domain.TipoPago, hl(t, "07:30:00"), 2),
		act("pago:3", domain.TipoCondonacion, hl(t, "07:40:00"), 3),
		// pausa de 80 min
		act("pago:4", domain.TipoPago, hl(t, "09:00:00"), 4),
		act("visita:b", domain.TipoVisita, hl(t, "19:59:00"), 5),
		act("pago:5", domain.TipoPago, hl(t, "20:00:00"), 6),
	}
	noAplicado := act("pago:6", domain.TipoPago, hl(t, "09:10:00"), 4)
	noAplicado.Pago.Aplicado = false
	acts = append(acts, noAplicado)

	j := domain.CalcularJornada(acts, p, dia14, loc, hl(t, "12:00:00").Add(24*time.Hour))
	require.Len(t, j.Actividades, 8)
	require.Len(t, j.Linea.Pausas, 2, "07:40→09:00 y 09:10→19:59")
	require.Len(t, j.Linea.Tramos, 3)

	t1 := j.Linea.Tramos[0]
	assert.Equal(t, hl(t, "06:59:00"), t1.Desde)
	assert.Equal(t, hl(t, "07:40:00"), t1.Hasta)
	assert.Equal(t, 2, t1.Pagos.N)
	assert.Equal(t, "300.00", t1.Pagos.Importe.StringFixed(2))
	assert.Equal(t, 1, t1.Visitas)
	assert.Equal(t, 1, t1.Condonaciones)
	assert.Equal(t, 3, t1.Clientes)

	t2 := j.Linea.Tramos[1]
	assert.Equal(t, 1, t2.Pagos.N, "el no aplicado no cuenta como dinero")
	assert.Equal(t, 1, t2.Clientes)

	require.Len(t, j.Linea.PorHora, 13)
	assert.Equal(t, 7, j.Linea.PorHora[0].Hora)
	assert.Equal(t, 19, j.Linea.PorHora[12].Hora)
	assert.Equal(t, 1, j.Linea.PorHora[0].Pagos, "06:59 queda fuera; 07:30 entra")
	assert.Equal(t, 1, j.Linea.PorHora[0].Visitas)
	assert.Equal(t, 1, j.Linea.PorHora[0].Condonaciones)
	assert.Equal(t, "150.00", j.Linea.PorHora[0].Importe.StringFixed(2))
	assert.Equal(t, 1, j.Linea.PorHora[12].Visitas, "19:59 entra en la hora 19")
	assert.Equal(t, 0, j.Linea.PorHora[12].Pagos, "20:00 queda fuera")

	r := j.Resumen
	assert.Equal(t, hl(t, "06:59:00"), *r.PrimeraActividad)
	assert.Equal(t, hl(t, "20:00:00"), *r.UltimaActividad)
	// 06:59→20:00 = 781 min, menos pausas de 80 y 649 min.
	assert.Equal(t, 781-80-649, r.MinutosActivos)
	assert.Equal(t, domain.ResumenPausas{N: 2, Minutos: 80 + 649}, r.Pausas)
	assert.Equal(t, 4, r.Pagos.N)
	assert.Equal(t, "600.00", r.Pagos.Importe.StringFixed(2))
	assert.Equal(t, 1, r.NoAplicados.N)
	assert.Equal(t, 1, r.Condonaciones.N)
	assert.Equal(t, 2, r.Visitas)
	assert.Equal(t, 6, r.Clientes)
}

func TestCalcularEstadoAhora(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	p := params(t, "45", "", "")
	acts := []domain.Actividad{
		act("pago:1", domain.TipoPago, hl(t, "08:00:00"), 1),
		act("pago:2", domain.TipoPago, hl(t, "09:30:00"), 2),
		act("pago:3", domain.TipoPago, hl(t, "09:50:00"), 3),
	}

	ahora := hl(t, "10:20:00")
	e, desde := domain.CalcularEstadoAhora(domain.CalcularJornada(acts, p, dia14, loc, ahora), p, ahora)
	assert.Equal(t, domain.EstadoAhoraActivo, e)
	assert.Equal(t, hl(t, "09:30:00"), *desde, "inicio del tramo actual")

	ahora = hl(t, "10:35:00")
	e, desde = domain.CalcularEstadoAhora(domain.CalcularJornada(acts, p, dia14, loc, ahora), p, ahora)
	assert.Equal(t, domain.EstadoAhoraSinActividad, e)
	assert.Equal(t, hl(t, "09:50:00"), *desde, "desde la última actividad")

	e, desde = domain.CalcularEstadoAhora(domain.CalcularJornada(nil, p, dia14, loc, ahora), p, ahora)
	assert.Equal(t, domain.EstadoAhoraNoHaEmpezado, e)
	assert.Nil(t, desde)
}

func TestFormatearDuracion(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "0 min", domain.FormatearDuracion(0))
	assert.Equal(t, "45 min", domain.FormatearDuracion(45))
	assert.Equal(t, "1 h", domain.FormatearDuracion(60))
	assert.Equal(t, "1 h 12 min", domain.FormatearDuracion(72))
	assert.Equal(t, "10 h 49 min", domain.FormatearDuracion(649))
}

func TestOrdenarActividades_EmpateDeHoraPorID(t *testing.T) {
	t.Parallel()
	h := hl(t, "10:00:00")
	acts := []domain.Actividad{
		act("visita:b", domain.TipoVisita, h, 1),
		act("pago:9", domain.TipoPago, h.Add(-time.Minute), 1),
		act("pago:1", domain.TipoPago, h, 1),
	}
	domain.OrdenarActividades(acts)
	assert.Equal(t, []string{"pago:9", "pago:1", "visita:b"}, []string{acts[0].ID, acts[1].ID, acts[2].ID})
	assert.Equal(t, 2026, dia14.Anio())
}

func TestCalcularJornada_PuntosLlevanElPopover(t *testing.T) {
	t.Parallel()
	pago := act("pago:1", domain.TipoPago, hl(t, "10:00:00"), 1)
	pago.ClienteNombre, pago.NumFotos = "JUANA ANDRADE LECHUGA", 2
	visita := act("visita:a", domain.TipoVisita, hl(t, "10:10:00"), 2)
	visita.ClienteNombre = "ROSARIO VILLA"
	j := domain.CalcularJornada([]domain.Actividad{visita, pago}, params(t, "", "", ""), dia14, cdmx(t), hl(t, "11:00:00"))
	require.Len(t, j.Linea.Puntos, 2)
	p, v := j.Linea.Puntos[0], j.Linea.Puntos[1]
	assert.Equal(t, "JUANA ANDRADE LECHUGA", p.ClienteNombre)
	require.NotNil(t, p.Importe)
	assert.Equal(t, "150.00", p.Importe.StringFixed(2))
	assert.Empty(t, p.Resultado)
	assert.Equal(t, 2, p.NumFotos)
	assert.Nil(t, v.Importe, "importe null en visitas")
	assert.Equal(t, "No se encontraba", v.Resultado)
}
