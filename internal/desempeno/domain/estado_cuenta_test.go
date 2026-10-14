package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func TestEstadosCuenta_SonLosOchoCodigosDelContrato(t *testing.T) {
	t.Parallel()
	codigos := []string{
		"pago", "abono_parcial", "visite_vuelvo", "prometio_proxima",
		"se_nego", "cita_a_una_hora", "no_estaba", "sin_tocar",
	}
	got := make([]string, 0, 8)
	for _, e := range domain.EstadosCuenta() {
		got = append(got, string(e))
	}
	assert.Equal(t, codigos, got)
	for _, c := range codigos {
		e, err := domain.ParseEstadoCuenta(c)
		require.NoError(t, err)
		assert.Equal(t, c, string(e))
	}
	_, err := domain.ParseEstadoCuenta("NO_ESTABA")
	require.ErrorIs(t, err, domain.ErrParametroInvalido)
}

func TestEstadoCuenta_Alcance(t *testing.T) {
	t.Parallel()
	for _, e := range domain.EstadosCuenta() {
		esperado := domain.AlcanceVenta
		if e == domain.EstadoCitaAUnaHora || e == domain.EstadoNoEstaba {
			esperado = domain.AlcanceCliente
		}
		assert.Equal(t, esperado, e.Alcance(), "estado %s", e)
	}
}

func TestEstadoDeTipoVisita_CatalogoCompleto(t *testing.T) {
	t.Parallel()
	casos := map[string]domain.EstadoCuenta{
		"No se encontraba":                 domain.EstadoNoEstaba,
		"Casa cerrada con candado":         domain.EstadoNoEstaba,
		"Solo había menores":               domain.EstadoNoEstaba,
		"Se asomó pero no salió":           domain.EstadoVisiteVuelvo,
		"No responde aunque está":          domain.EstadoVisiteVuelvo,
		"Se escuchan ruidos pero no abre":  domain.EstadoVisiteVuelvo,
		"Pidió que regrese otro día":       domain.EstadoVisiteVuelvo,
		"No pagará esta ocasión":           domain.EstadoSeNego,
		"Tiene dinero pero no quiso pagar": domain.EstadoSeNego,
		"Fue grosero o agresivo":           domain.EstadoSeNego,
		"Pidió reagendar visita":           domain.EstadoPrometioProxima,
		"No va a dar pago":                 domain.EstadoSeNego,
		"Dijo que no va a pagar":           domain.EstadoSeNego,
		"Se esconde y no sale":             domain.EstadoVisiteVuelvo,
	}
	require.Len(t, casos, 14, "el catálogo cerrado tiene 14 literales")
	for lit, esperado := range casos {
		assert.True(t, domain.EsTipoVisitaConocido(lit), "%q es conocido", lit)
		assert.Equal(t, esperado, domain.EstadoDeTipoVisita(lit, false), "literal %q", lit)
		assert.Equal(t, domain.EstadoCitaAUnaHora, domain.EstadoDeTipoVisita(lit, true),
			"con día de cita %q es cita", lit)
	}
}

func TestEstadoDeTipoVisita_FueraDeCatalogo(t *testing.T) {
	t.Parallel()
	for _, lit := range []string{"", "no se encontraba", "Pidio reagendar visita", "texto libre"} {
		assert.False(t, domain.EsTipoVisitaConocido(lit), "%q no es conocido", lit)
		assert.Equal(t, domain.EstadoVisiteVuelvo, domain.EstadoDeTipoVisita(lit, false), "%q", lit)
		assert.Equal(t, domain.AlcanceVenta, domain.AlcanceDeTipoVisita(lit, false), "%q", lit)
	}
}

func TestCatalogoTiposVisita(t *testing.T) {
	t.Parallel()
	cat := domain.CatalogoTiposVisita()
	require.Len(t, cat, 14)
	assert.Equal(t, domain.TipoVisitaCatalogo{Literal: "No se encontraba", Estado: domain.EstadoNoEstaba, Grupo: "No estaba"}, cat[0])
	assert.Equal(t, "Se esconde y no sale", cat[13].Literal, "los retirados van al final")
	for _, c := range cat {
		assert.True(t, domain.EsTipoVisitaConocido(c.Literal), c.Literal)
		assert.NotEmpty(t, c.Grupo, c.Literal)
	}
	assert.Empty(t, domain.GrupoDeEstado(domain.EstadoPago))
	assert.Empty(t, domain.GrupoDeEstado(domain.EstadoCuenta("x")))
}
