//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app_test

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	reactivacionapp "github.com/abdimuy/msp-api/internal/reactivacion/app"
)

// businessTZFixture is a fixed America/Mexico_City-equivalent zone (UTC-6, no
// DST complications) used to keep the governor tests independent of the
// host's tzdata / BusinessTZ() lookup.
var businessTZFixture = time.FixedZone("MX-Test", -6*3600)

func testGobernadorConfig() reactivacionapp.GobernadorConfig {
	return reactivacionapp.GobernadorConfig{
		TopeDiario: 30,
		JitterMin:  90 * time.Second,
		JitterMax:  8 * time.Minute,
		HoraInicio: 9,
		HoraFin:    18,
		Zona:       businessTZFixture,
	}
}

// wed is a Wednesday within the fixture zone at the given local hour/minute.
func wed(hour, minute int) time.Time {
	return time.Date(2026, 7, 22, hour, minute, 0, 0, businessTZFixture) // 2026-07-22 is a Wednesday
}

// sun is a Sunday within the fixture zone at the given local hour/minute.
func sun(hour, minute int) time.Time {
	return time.Date(2026, 7, 19, hour, minute, 0, 0, businessTZFixture) // 2026-07-19 is a Sunday
}

func TestGobernador_TopeDiarioAgotado(t *testing.T) {
	t.Parallel()
	g := reactivacionapp.NewGobernador(testGobernadorConfig(), rand.New(rand.NewSource(1)))
	d := g.PuedeEnviar(30, time.Time{}, wed(10, 0))
	assert.False(t, d.Permitido)
	assert.Equal(t, "tope_diario", d.Motivo)
}

func TestGobernador_DentroDeHorario_SinEnvioPrevio_ConCupo(t *testing.T) {
	t.Parallel()
	g := reactivacionapp.NewGobernador(testGobernadorConfig(), rand.New(rand.NewSource(1)))
	d := g.PuedeEnviar(0, time.Time{}, wed(10, 0))
	assert.True(t, d.Permitido)
	assert.Empty(t, d.Motivo)
	assert.Zero(t, d.Esperar)
}

func TestGobernador_FueraDeHorario_Temprano(t *testing.T) {
	t.Parallel()
	g := reactivacionapp.NewGobernador(testGobernadorConfig(), rand.New(rand.NewSource(1)))
	d := g.PuedeEnviar(0, time.Time{}, wed(7, 0))
	assert.False(t, d.Permitido)
	assert.Equal(t, "fuera_de_horario", d.Motivo)
	assert.Positive(t, d.Esperar)
}

func TestGobernador_FueraDeHorario_Tarde(t *testing.T) {
	t.Parallel()
	g := reactivacionapp.NewGobernador(testGobernadorConfig(), rand.New(rand.NewSource(1)))
	d := g.PuedeEnviar(0, time.Time{}, wed(20, 0))
	assert.False(t, d.Permitido)
	assert.Equal(t, "fuera_de_horario", d.Motivo)
	assert.Positive(t, d.Esperar)
}

func TestGobernador_Domingo(t *testing.T) {
	t.Parallel()
	g := reactivacionapp.NewGobernador(testGobernadorConfig(), rand.New(rand.NewSource(1)))
	d := g.PuedeEnviar(0, time.Time{}, sun(10, 0))
	assert.False(t, d.Permitido)
	assert.Equal(t, "fuera_de_horario", d.Motivo)
	assert.Positive(t, d.Esperar)
}

func TestGobernador_Jitter_NoCumplido(t *testing.T) {
	t.Parallel()
	g := reactivacionapp.NewGobernador(testGobernadorConfig(), rand.New(rand.NewSource(1)))
	ultimo := wed(10, 0)
	now := ultimo.Add(10 * time.Second)
	d := g.PuedeEnviar(0, ultimo, now)
	assert.False(t, d.Permitido)
	assert.Equal(t, "jitter", d.Motivo)
	assert.Positive(t, d.Esperar)
}

func TestGobernador_Jitter_Cumplido(t *testing.T) {
	t.Parallel()
	g := reactivacionapp.NewGobernador(testGobernadorConfig(), rand.New(rand.NewSource(1)))
	ultimo := wed(10, 0)
	// Comfortably beyond the max jitter (8m) so the decision is deterministic
	// regardless of the seeded jitter draw.
	now := ultimo.Add(9 * time.Minute)
	d := g.PuedeEnviar(0, ultimo, now)
	assert.True(t, d.Permitido)
	assert.Empty(t, d.Motivo)
}

func TestGobernador_PerfilDemo_SiemprePermitido(t *testing.T) {
	t.Parallel()
	cfg := reactivacionapp.PerfilConfig(reactivacionapp.PerfilDemo)
	cfg.Zona = businessTZFixture
	g := reactivacionapp.NewGobernador(cfg, rand.New(rand.NewSource(1)))

	for _, now := range []time.Time{wed(3, 0), wed(23, 0), sun(12, 0)} {
		d := g.PuedeEnviar(0, time.Time{}, now)
		assert.True(t, d.Permitido, "expected demo profile to always allow at %v", now)
	}
}

func TestPerfilConfig_Produccion(t *testing.T) {
	t.Parallel()
	cfg := reactivacionapp.PerfilConfig(reactivacionapp.PerfilProduccion)
	assert.Equal(t, 30, cfg.TopeDiario)
	assert.Equal(t, 90*time.Second, cfg.JitterMin)
	assert.Equal(t, 8*time.Minute, cfg.JitterMax)
	assert.Equal(t, 9, cfg.HoraInicio)
	assert.Equal(t, 18, cfg.HoraFin)
	require.NotNil(t, cfg.Zona)
}

func TestPerfilConfig_Demo(t *testing.T) {
	t.Parallel()
	cfg := reactivacionapp.PerfilConfig(reactivacionapp.PerfilDemo)
	assert.Equal(t, 100000, cfg.TopeDiario)
	assert.Equal(t, time.Duration(0), cfg.JitterMin)
	assert.Equal(t, time.Duration(0), cfg.JitterMax)
	assert.Equal(t, 0, cfg.HoraInicio)
	assert.Equal(t, 24, cfg.HoraFin)
	require.NotNil(t, cfg.Zona)
}
