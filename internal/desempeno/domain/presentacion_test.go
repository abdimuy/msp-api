package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func fecha(t *testing.T, s string) *domain.Fecha {
	t.Helper()
	f, err := domain.ParseFecha(s)
	require.NoError(t, err)
	return &f
}

func hora(t *testing.T, s string) *domain.HoraDelDia {
	t.Helper()
	h, err := domain.ParseHoraDelDia(s)
	require.NoError(t, err)
	return &h
}

// Cadenas copiadas de EstadoCuentaUi.kt y de EstadoCuentaUiTest.kt
// (LEÍDO 2026-10-14). Si el teléfono cambia una, esta tabla se pone roja.
// Única divergencia: el segmento del abono parcial (E-INF-016).
func TestPresentacion_TratoEtiquetaDetalleSegmento(t *testing.T) {
	t.Parallel()
	hoy := *fecha(t, "2026-09-20")
	casos := []struct {
		nombre            string
		r                 domain.ResultadoCuenta
		trato             domain.Trato
		etiqueta, detalle string
		segmento          domain.Segmento
		haySegmento       bool
	}{
		{
			"pago",
			domain.ResultadoCuenta{Estado: domain.EstadoPago},
			domain.TratoPagado, "Pagó esta semana", "No hace falta volver", domain.SegmentoPagados, true,
		},
		{
			// E-INF-016: divergencia deliberada con el teléfono (que lo manda a
			// "volver"); la etiqueta y el trato siguen siendo los suyos.
			"abono parcial",
			domain.ResultadoCuenta{Estado: domain.EstadoAbonoParcial},
			domain.TratoParcial, "Abonó parcial", "Falta por confirmar", domain.SegmentoPagados, true,
		},
		{
			"visité vuelvo",
			domain.ResultadoCuenta{Estado: domain.EstadoVisiteVuelvo},
			domain.TratoRegresas, "Visité, vuelvo", "Regresas esta semana", domain.SegmentoVolver, true,
		},
		{
			"promesa sin fecha",
			domain.ResultadoCuenta{Estado: domain.EstadoPrometioProxima},
			domain.TratoRegresas, "Prometió sin fecha", "Sin fecha, regresas", domain.SegmentoVolver, true,
		},
		{
			"promesa a futuro",
			domain.ResultadoCuenta{Estado: domain.EstadoPrometioProxima, FechaPromesa: fecha(t, "2026-09-25")},
			domain.TratoDiferido, "Prometió el 25 sept", "No cae esta semana", domain.SegmentoDespues, true,
		},
		{
			"promesa de hoy",
			domain.ResultadoCuenta{Estado: domain.EstadoPrometioProxima, FechaPromesa: fecha(t, "2026-09-20")},
			domain.TratoDiferido, "Prometió el 20 sept", "No cae esta semana", domain.SegmentoVolver, true,
		},
		{
			"promesa vencida",
			domain.ResultadoCuenta{Estado: domain.EstadoPrometioProxima, FechaPromesa: fecha(t, "2026-09-02")},
			domain.TratoDiferido, "Prometió el 2 sept", "No cae esta semana", domain.SegmentoVolver, true,
		},
		{
			"se negó",
			domain.ResultadoCuenta{Estado: domain.EstadoSeNego},
			domain.TratoEscalar, "Se negó", "Escalar esta cuenta", domain.SegmentoDespues, true,
		},
		{
			"cita sin hora",
			domain.ResultadoCuenta{Estado: domain.EstadoCitaAUnaHora, FechaCita: fecha(t, "2026-09-24")},
			domain.TratoRegresas, "Cita sin hora", "Sin hora, regresas", domain.SegmentoVolver, true,
		},
		{
			"cita con día y hora a futuro",
			domain.ResultadoCuenta{Estado: domain.EstadoCitaAUnaHora, FechaCita: fecha(t, "2026-09-24"), HoraCita: hora(t, "16:30")},
			domain.TratoCita, "Cita 24 sept 16:30", "Quedaron de verse", domain.SegmentoDespues, true,
		},
		{
			"cita con hora sin día no cae en ningún chip",
			domain.ResultadoCuenta{Estado: domain.EstadoCitaAUnaHora, HoraCita: hora(t, "16:30")},
			domain.TratoCita, "Cita 16:30", "Quedaron de verse", "", false,
		},
		{
			"no estaba",
			domain.ResultadoCuenta{Estado: domain.EstadoNoEstaba},
			domain.TratoNadie, "No estaba", "Regresas esta semana", domain.SegmentoVolver, true,
		},
		{
			"sin tocar",
			domain.ResultadoCuenta{Estado: domain.EstadoSinTocar},
			domain.TratoSinTrabajar, "Falta pasar", "Todavía no vas", domain.SegmentoSinVisitar, true,
		},
	}
	for _, c := range casos {
		assert.Equal(t, c.trato, domain.TratoDe(c.r), c.nombre)
		assert.Equal(t, c.etiqueta, domain.EtiquetaDe(c.r), c.nombre)
		assert.Equal(t, c.detalle, domain.DetalleDe(c.r), c.nombre)
		seg, ok := domain.SegmentoDe(c.r, hoy)
		assert.Equal(t, c.haySegmento, ok, c.nombre)
		assert.Equal(t, c.segmento, seg, c.nombre)
	}
}

func TestPresentacion_MesesCortos(t *testing.T) {
	t.Parallel()
	esperado := []string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sept", "oct", "nov", "dic"}
	for i, mes := range esperado {
		f := fecha(t, "2026-"+[]string{"01", "02", "03", "04", "05", "06", "07", "08", "09", "10", "11", "12"}[i]+"-03")
		r := domain.ResultadoCuenta{Estado: domain.EstadoPrometioProxima, FechaPromesa: f}
		assert.Equal(t, "Prometió el 3 "+mes, domain.EtiquetaDe(r))
	}
}

func TestPresentacion_EstadoDesconocidoEsSinTrabajar(t *testing.T) {
	t.Parallel()
	r := domain.ResultadoCuenta{Estado: domain.EstadoCuenta("inventado")}
	assert.Equal(t, domain.TratoSinTrabajar, domain.TratoDe(r))
	assert.Equal(t, "Falta pasar", domain.EtiquetaDe(r))
	assert.Equal(t, "Todavía no vas", domain.DetalleDe(r))
	seg, ok := domain.SegmentoDe(r, *fecha(t, "2026-10-14"))
	assert.True(t, ok)
	assert.Equal(t, domain.SegmentoSinVisitar, seg)
}
