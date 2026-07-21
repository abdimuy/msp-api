//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

var memoriaNow = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

func mustResumenTurno(t *testing.T, dir domain.DireccionTurno, autor domain.Autor, cuerpo string, offset time.Duration) *domain.Turno {
	t.Helper()
	turno, err := domain.CrearTurno(domain.CrearTurnoParams{
		ClienteID: 24037,
		Direccion: dir,
		Autor:     autor,
		Cuerpo:    cuerpo,
		Now:       memoriaNow.Add(offset),
	})
	require.NoError(t, err)
	return turno
}

func TestConstruirResumen_Empty(t *testing.T) {
	t.Parallel()
	resumen := construirResumen(nil)
	assert.NotEmpty(t, resumen)
	assert.Contains(t, strings.ToLower(resumen), "sin historial")
}

func TestConstruirResumen_ShapeAndOrder(t *testing.T) {
	t.Parallel()
	turnos := []*domain.Turno{
		mustResumenTurno(t, domain.DireccionEntrante, domain.AutorCliente, "hola, tengo una pregunta", 0),
		mustResumenTurno(t, domain.DireccionSaliente, domain.AutorIA, "claro, dígame", time.Minute),
		mustResumenTurno(t, domain.DireccionSaliente, domain.AutorHumano, "yo le ayudo con eso", 2*time.Minute),
	}
	resumen := construirResumen(turnos)

	// Header mentions the turn count.
	assert.Contains(t, resumen, "3")

	// Each turno's autor label appears as its own line prefix, in chronological
	// order (cliente before ia before humano, matching the input order). The
	// "- x:" prefix avoids false substring matches (e.g. bare "ia" inside
	// "historial").
	iCliente := strings.Index(resumen, "- cliente:")
	iIA := strings.Index(resumen, "- ia:")
	iHumano := strings.Index(resumen, "- humano:")
	require.NotEqual(t, -1, iCliente)
	require.NotEqual(t, -1, iIA)
	require.NotEqual(t, -1, iHumano)
	assert.Less(t, iCliente, iIA)
	assert.Less(t, iIA, iHumano)

	// The turno bodies are present verbatim.
	assert.Contains(t, resumen, "hola, tengo una pregunta")
	assert.Contains(t, resumen, "claro, dígame")
	assert.Contains(t, resumen, "yo le ayudo con eso")
}

func TestConstruirResumen_CapsLength(t *testing.T) {
	t.Parallel()
	turnos := make([]*domain.Turno, 0, 50)
	for i := range 50 {
		cuerpo := strings.Repeat("x", 200)
		turnos = append(turnos, mustResumenTurno(t, domain.DireccionEntrante, domain.AutorCliente, cuerpo, time.Duration(i)*time.Minute))
	}
	resumen := construirResumen(turnos)
	assert.LessOrEqual(t, len([]rune(resumen)), resumenMaxRunes)
}

func TestConstruirResumen_CapsTurnCount(t *testing.T) {
	t.Parallel()
	// Build more turnos than resumenMaxTurnos with distinguishable short
	// bodies; only the LAST resumenMaxTurnos should appear (most recent
	// context matters most for the LLM).
	turnos := make([]*domain.Turno, 0, resumenMaxTurnos+3)
	for i := range resumenMaxTurnos + 3 {
		turnos = append(turnos, mustResumenTurno(t, domain.DireccionEntrante, domain.AutorCliente, "mensaje-"+string(rune('a'+i)), time.Duration(i)*time.Minute))
	}
	resumen := construirResumen(turnos)

	// The earliest turno's body must be dropped by the cap.
	assert.NotContains(t, resumen, "mensaje-a")
	// The most recent turno's body must be present.
	lastLabel := "mensaje-" + string(rune('a'+resumenMaxTurnos+2))
	assert.Contains(t, resumen, lastLabel)
}
