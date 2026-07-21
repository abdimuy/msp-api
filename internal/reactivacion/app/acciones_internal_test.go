//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

var newestDecisionNow = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

func mustInternalDecision(t *testing.T, resultado domain.ResultadoDecision, now time.Time) *domain.Decision {
	t.Helper()
	d, err := domain.CrearDecision(domain.CrearDecisionParams{
		ClienteID: 24037,
		Accion:    domain.AccionResponder,
		Resultado: resultado,
		Now:       now,
	})
	require.NoError(t, err)
	return d
}

func TestNewestDecisionPorClienteID_Empty(t *testing.T) {
	t.Parallel()
	assert.Nil(t, newestDecisionPorClienteID(nil))
}

func TestNewestDecisionPorClienteID_StrictlyLater_Wins(t *testing.T) {
	t.Parallel()
	first := mustInternalDecision(t, domain.ResultadoPropuesto, newestDecisionNow)
	second := mustInternalDecision(t, domain.ResultadoAprobado, newestDecisionNow.Add(time.Second))

	got := newestDecisionPorClienteID([]*domain.Decision{first, second})
	assert.Same(t, second, got)
}

func TestNewestDecisionPorClienteID_TiedCreatedAt_ReturnsSecondInserted(t *testing.T) {
	t.Parallel()
	// Legacy Firebird TIMESTAMP has ~100µs resolution — a propuesto and
	// its aprobado (written moments apart in the same operator action) can
	// share an identical CreatedAt. Per ListarPorCliente's ascending-order
	// contract, "newest" must mean the LAST element of a tied group — the
	// second-inserted one here — never the first (which would resurrect an
	// already-superseded propuesto).
	propuesto := mustInternalDecision(t, domain.ResultadoPropuesto, newestDecisionNow)
	aprobado := mustInternalDecision(t, domain.ResultadoAprobado, newestDecisionNow)

	got := newestDecisionPorClienteID([]*domain.Decision{propuesto, aprobado})
	assert.Same(t, aprobado, got, "on a CreatedAt tie, the LAST (second-inserted) element must win")

	// Order sensitivity: reversing the input order still returns whichever
	// element is LAST in the slice — confirms the tie-break is purely
	// position-based (relies on the caller's ascending-order contract), not an
	// accidental artifact of which decision happens to be resultado aprobado.
	gotReversed := newestDecisionPorClienteID([]*domain.Decision{aprobado, propuesto})
	assert.Same(t, propuesto, gotReversed)
}

func TestNewestDecisionPorClienteID_ThreeWayTie_ReturnsLast(t *testing.T) {
	t.Parallel()
	a := mustInternalDecision(t, domain.ResultadoPropuesto, newestDecisionNow)
	b := mustInternalDecision(t, domain.ResultadoEditado, newestDecisionNow)
	c := mustInternalDecision(t, domain.ResultadoAprobado, newestDecisionNow)

	got := newestDecisionPorClienteID([]*domain.Decision{a, b, c})
	assert.Same(t, c, got)
}
