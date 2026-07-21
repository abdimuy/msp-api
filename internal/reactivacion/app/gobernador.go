//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"math/rand"
	"time"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// PerfilEnvio selects a GobernadorConfig preset. produccion applies the real
// anti-baneo pacing; demo relaxes every knob so a full drain of the fake
// channel completes in seconds.
type PerfilEnvio string

const (
	// PerfilProduccion applies the real anti-baneo pacing (spec §9).
	PerfilProduccion PerfilEnvio = "produccion"
	// PerfilDemo relaxes every knob for a fast end-to-end demo with the fake
	// sender: no jitter, an always-open window, and a very high daily cap.
	PerfilDemo PerfilEnvio = "demo"
)

// Default GobernadorConfig knobs per perfil (spec §7 / §9).
const (
	produccionTopeDiario  = 30
	produccionJitterFloor = 90 * time.Second
	produccionJitterCeil  = 8 * time.Minute
	produccionHoraInicio  = 9
	produccionHoraFin     = 18

	demoTopeDiario = 100000
	demoHoraInicio = 0
	demoHoraFin    = 24
)

// GobernadorConfig tunes the anti-baneo governor. Zero values are only
// meaningful via PerfilConfig — building one by hand should set every field.
type GobernadorConfig struct {
	// TopeDiario is the maximum number of ENVIADO messages allowed per
	// business day.
	TopeDiario int
	// JitterMin is the minimum wait between two consecutive sends.
	JitterMin time.Duration
	// JitterMax is the maximum wait between two consecutive sends. When
	// JitterMax <= JitterMin, the jitter is fixed at JitterMin (no randomness;
	// used by the demo profile to disable pacing entirely with 0/0).
	JitterMax time.Duration
	// HoraInicio is the local hour (0-23) the sending window opens.
	HoraInicio int
	// HoraFin is the local hour (0-24) the sending window closes (exclusive).
	HoraFin int
	// Zona is the business timezone the hour window is evaluated in. Defaults
	// to firebird.BusinessTZ() (America/Mexico_City) via PerfilConfig.
	Zona *time.Location
}

// isFullDayWindow reports whether the configured window spans the entire day
// (HoraInicio<=0 and HoraFin>=24). A full-day window also lifts the Sunday
// exclusion — the demo profile's whole point is "any hour, any day" so a
// scripted demo never trips on a weekend.
func (c GobernadorConfig) isFullDayWindow() bool {
	return c.HoraInicio <= 0 && c.HoraFin >= 24
}

// PerfilConfig returns the default GobernadorConfig for p. Unrecognized
// values fall back to PerfilProduccion (the safer, more conservative pacing).
func PerfilConfig(p PerfilEnvio) GobernadorConfig {
	zona := firebird.BusinessTZ()
	if p == PerfilDemo {
		return GobernadorConfig{
			TopeDiario: demoTopeDiario,
			JitterMin:  0,
			JitterMax:  0,
			HoraInicio: demoHoraInicio,
			HoraFin:    demoHoraFin,
			Zona:       zona,
		}
	}
	return GobernadorConfig{
		TopeDiario: produccionTopeDiario,
		JitterMin:  produccionJitterFloor,
		JitterMax:  produccionJitterCeil,
		HoraInicio: produccionHoraInicio,
		HoraFin:    produccionHoraFin,
		Zona:       zona,
	}
}

// Decision is the outcome of Gobernador.PuedeEnviar.
type Decision struct {
	// Permitido is true when the next message may be sent right now.
	Permitido bool
	// Motivo explains why Permitido is false: "tope_diario" | "fuera_de_horario"
	// | "jitter". Empty when Permitido is true.
	Motivo string
	// Esperar is how long until the next attempt might succeed. Zero when
	// Permitido is true, or when Motivo is "tope_diario" (the cap resets the
	// next business day, not after a fixed wait).
	Esperar time.Duration
}

// Gobernador is the anti-baneo pacing engine: pure decision logic over an
// injected clock reading (no internal timers) and an injected random source
// (deterministic in tests, time-seeded in production).
type Gobernador struct {
	cfg GobernadorConfig
	rng *rand.Rand
}

// NewGobernador builds a Gobernador. rng must not be nil — inject a seeded
// *rand.Rand in tests for determinism, or rand.New(rand.NewSource(seed)) with
// a time-derived seed in production wiring.
func NewGobernador(cfg GobernadorConfig, rng *rand.Rand) *Gobernador {
	return &Gobernador{cfg: cfg, rng: rng}
}

// PuedeEnviar decides whether the next message may go out right now.
//
// Order of checks:
//  1. enviadosHoy >= TopeDiario → blocked, "tope_diario".
//  2. now outside the business window (hour range, and Sunday unless the
//     window is full-day) → blocked, "fuera_de_horario".
//  3. now < ultimoEnvio + jitter → blocked, "jitter". ultimoEnvio zero (no
//     previous send) always passes this check.
//  4. otherwise → allowed.
func (g *Gobernador) PuedeEnviar(enviadosHoy int, ultimoEnvio, now time.Time) Decision {
	if enviadosHoy >= g.cfg.TopeDiario {
		return Decision{Motivo: "tope_diario"}
	}

	if !g.isBusinessWindow(now) {
		return Decision{Motivo: "fuera_de_horario", Esperar: g.nextVentanaInicio(now).Sub(now)}
	}

	if !ultimoEnvio.IsZero() {
		jitter := g.jitterDuration()
		siguiente := ultimoEnvio.Add(jitter)
		if now.Before(siguiente) {
			return Decision{Motivo: "jitter", Esperar: siguiente.Sub(now)}
		}
	}

	return Decision{Permitido: true}
}

// isBusinessWindow reports whether now falls inside the configured hour
// window in Zona, honoring the Sunday exclusion unless the window is
// full-day.
func (g *Gobernador) isBusinessWindow(now time.Time) bool {
	local := now.In(g.cfg.Zona)
	if !g.cfg.isFullDayWindow() && local.Weekday() == time.Sunday {
		return false
	}
	h := local.Hour()
	return h >= g.cfg.HoraInicio && h < g.cfg.HoraFin
}

// nextVentanaInicio returns the next moment >= now at which the business
// window opens, skipping Sunday (unless the window is full-day).
func (g *Gobernador) nextVentanaInicio(now time.Time) time.Time {
	local := now.In(g.cfg.Zona)
	candidate := time.Date(local.Year(), local.Month(), local.Day(), g.cfg.HoraInicio, 0, 0, 0, g.cfg.Zona)
	if !candidate.After(local) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	if !g.cfg.isFullDayWindow() {
		for candidate.Weekday() == time.Sunday {
			candidate = candidate.AddDate(0, 0, 1)
		}
	}
	return candidate
}

// jitterDuration draws one jitter value in [JitterMin, JitterMax). When
// JitterMax <= JitterMin the jitter is fixed at JitterMin — no randomness,
// which is how the demo profile (0/0) disables pacing entirely.
func (g *Gobernador) jitterDuration() time.Duration {
	minJ, maxJ := g.cfg.JitterMin, g.cfg.JitterMax
	if maxJ <= minJ {
		return minJ
	}
	delta := int64(maxJ - minJ)
	return minJ + time.Duration(g.rng.Int63n(delta))
}
