//nolint:misspell // ventas vocabulary is Spanish (venta, fase) per project convention.
package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// TestEsEventoDeCambioDeFase_AcceptsEveryPhaseTransition pins the six event
// types that move a venta from one fase to another. The desktop's "Fase"
// column measures elapsed time from the newest of these, so a missing member
// would freeze the clock on a venta that actually moved.
func TestEsEventoDeCambioDeFase_AcceptsEveryPhaseTransition(t *testing.T) {
	t.Parallel()

	for _, et := range []string{
		domain.EventTypeVentaCreada,
		domain.EventTypeVentaEnviadaARevision,
		domain.EventTypeVentaAprobada,
		domain.EventTypeVentaRegresadaABorrador,
		domain.EventTypeVentaAplicada,
		domain.EventTypeVentaCancelada,
	} {
		assert.True(t, domain.EsEventoDeCambioDeFase(et), "%s must count as a fase change", et)
	}
}

// TestEsEventoDeCambioDeFase_RejectsEditEvents pins the complement: editing a
// venta does NOT restart its fase clock. Counting them would hide a venta
// stuck in revisión behind a cosmetic edit made yesterday — the exact defect
// the fase_desde field exists to expose.
func TestEsEventoDeCambioDeFase_RejectsEditEvents(t *testing.T) {
	t.Parallel()

	for _, et := range []string{
		domain.EventTypeVentaHeaderActualizado,
		domain.EventTypeVentaClienteActualizado,
		domain.EventTypeVentaProductosReemplazados,
		domain.EventTypeVentaCombosReemplazados,
		domain.EventTypeVentaVendedoresReemplazados,
		domain.EventTypeImagenAdjuntada,
		domain.EventTypeImagenEliminada,
		"traspaso.creado",
		"",
	} {
		assert.False(t, domain.EsEventoDeCambioDeFase(et), "%q must NOT count as a fase change", et)
	}
}

// TestEventTypesCambioDeFase_ListsExactlyThePhaseEvents verifies the exported
// slice (used to build the SQL IN list) matches the predicate, and that
// mutating the returned slice cannot corrupt the canonical set.
func TestEventTypesCambioDeFase_ListsExactlyThePhaseEvents(t *testing.T) {
	t.Parallel()

	got := domain.EventTypesCambioDeFase()
	assert.ElementsMatch(t, []string{
		domain.EventTypeVentaCreada,
		domain.EventTypeVentaEnviadaARevision,
		domain.EventTypeVentaAprobada,
		domain.EventTypeVentaRegresadaABorrador,
		domain.EventTypeVentaAplicada,
		domain.EventTypeVentaCancelada,
	}, got)

	got[0] = "mutated"
	assert.NotContains(t, domain.EventTypesCambioDeFase(), "mutated")
	assert.True(t, domain.EsEventoDeCambioDeFase(domain.EventTypeVentaCreada))
}

// TestFaseDelEvento_NumbersEveryAdvancingEvent pins the event → fase number
// map that feeds "fase_alcanzada": the desktop draws one arc per fase the
// venta reached, so a wrong number draws a wrong ring.
func TestFaseDelEvento_NumbersEveryAdvancingEvent(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		eventType string
		fase      int
	}{
		{domain.EventTypeVentaCreada, 1},
		{domain.EventTypeVentaRegresadaABorrador, 1},
		{domain.EventTypeVentaEnviadaARevision, 2},
		{domain.EventTypeVentaAprobada, 3},
		{domain.EventTypeVentaAplicada, 4},
	} {
		fase, ok := domain.FaseDelEvento(tc.eventType)
		assert.True(t, ok, "%s must carry a fase", tc.eventType)
		assert.Equal(t, tc.fase, fase, "fase number for %s", tc.eventType)
	}
}

// TestFaseDelEvento_CanceladaCarriesNoFase pins the exception that motivates
// the whole field: cancelling is not advancing. The venta keeps whatever fase
// it had reached, so venta.cancelada must contribute nothing to the maximum.
func TestFaseDelEvento_CanceladaCarriesNoFase(t *testing.T) {
	t.Parallel()

	fase, ok := domain.FaseDelEvento(domain.EventTypeVentaCancelada)

	assert.False(t, ok, "cancelar no es avanzar")
	assert.Equal(t, 0, fase)
}

// TestFaseDelEvento_RejectsEditEventsAndNoise pins the complement: an edit
// never places the venta in a fase.
func TestFaseDelEvento_RejectsEditEventsAndNoise(t *testing.T) {
	t.Parallel()

	for _, et := range []string{
		domain.EventTypeVentaHeaderActualizado,
		domain.EventTypeVentaClienteActualizado,
		domain.EventTypeVentaProductosReemplazados,
		domain.EventTypeVentaCombosReemplazados,
		domain.EventTypeVentaVendedoresReemplazados,
		domain.EventTypeImagenAdjuntada,
		domain.EventTypeImagenEliminada,
		"traspaso.creado",
		"",
	} {
		fase, ok := domain.FaseDelEvento(et)
		assert.False(t, ok, "%q must carry no fase", et)
		assert.Equal(t, 0, fase)
	}
}

// TestFaseDelEvento_EveryFaseEventIsEitherNumberedOrCancelada guards the two
// sets against drift: every phase-changing event must be numbered, except the
// single documented exception.
func TestFaseDelEvento_EveryFaseEventIsEitherNumberedOrCancelada(t *testing.T) {
	t.Parallel()

	for _, et := range domain.EventTypesCambioDeFase() {
		fase, ok := domain.FaseDelEvento(et)
		if et == domain.EventTypeVentaCancelada {
			assert.False(t, ok, "cancelada is the only phase event without a fase number")
			continue
		}
		assert.True(t, ok, "%s changes fase, so it must have a fase number", et)
		assert.GreaterOrEqual(t, fase, 1)
		assert.LessOrEqual(t, fase, 4)
	}
}
