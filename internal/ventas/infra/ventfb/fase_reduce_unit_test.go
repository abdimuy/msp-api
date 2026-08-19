//nolint:misspell // Spanish vocabulary (venta, fase) by convention.
package ventfb

// Pure unit tests for the two fase reductions. The rules "the venta's fase
// started at its NEWEST phase-changing event" and "fase_alcanzada is the
// HIGHEST fase it ever reached" are business logic, so they live in Go and
// are pinned here without a database: the SQL WHERE only narrows the rows
// read, it is never the authority on which of them counts — nor on what they
// reduce to.

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// at is a terse constructor for deterministic UTC instants.
func at(day, hour int) time.Time {
	return time.Date(2026, 8, day, hour, 0, 0, 0, time.UTC)
}

func TestReduceFaseDesde_KeepsTheNewestPhaseEventPerVenta(t *testing.T) {
	t.Parallel()

	a, b := uuid.New(), uuid.New()
	out := reduceFaseDesde([]faseEventRow{
		{ventaID: a, eventType: domain.EventTypeVentaCreada, occurredAt: at(1, 9)},
		{ventaID: a, eventType: domain.EventTypeVentaEnviadaARevision, occurredAt: at(3, 9)},
		{ventaID: a, eventType: domain.EventTypeVentaAprobada, occurredAt: at(2, 9)},
		{ventaID: b, eventType: domain.EventTypeVentaCreada, occurredAt: at(5, 9)},
	})

	assert.Equal(t, at(3, 9), out[a], "the newest phase event wins regardless of row order")
	assert.Equal(t, at(5, 9), out[b])
	assert.Len(t, out, 2)
}

func TestReduceFaseDesde_IgnoresEditEvents(t *testing.T) {
	t.Parallel()

	v := uuid.New()
	out := reduceFaseDesde([]faseEventRow{
		{ventaID: v, eventType: domain.EventTypeVentaEnviadaARevision, occurredAt: at(1, 9)},
		{ventaID: v, eventType: domain.EventTypeVentaHeaderActualizado, occurredAt: at(9, 9)},
		{ventaID: v, eventType: domain.EventTypeVentaProductosReemplazados, occurredAt: at(9, 10)},
		{ventaID: v, eventType: domain.EventTypeImagenAdjuntada, occurredAt: at(9, 11)},
	})

	assert.Equal(t, at(1, 9), out[v],
		"an edit made later must not restart the fase clock")
}

func TestReduceFaseDesde_VentaWithoutPhaseEventsIsAbsent(t *testing.T) {
	t.Parallel()

	v := uuid.New()
	out := reduceFaseDesde([]faseEventRow{
		{ventaID: v, eventType: domain.EventTypeVentaHeaderActualizado, occurredAt: at(9, 9)},
	})

	_, present := out[v]
	assert.False(t, present, "no phase event means no fase_desde — never an invented date")
}

// TestReduceFaseDesde_RegresadaABorradorBeatsEarlierAprobada pins the
// backwards transition: a venta sent back to borrador restarts its clock, so
// the older aprobada must lose.
func TestReduceFaseDesde_RegresadaABorradorBeatsEarlierAprobada(t *testing.T) {
	t.Parallel()

	v := uuid.New()
	out := reduceFaseDesde([]faseEventRow{
		{ventaID: v, eventType: domain.EventTypeVentaAprobada, occurredAt: at(1, 9)},
		{ventaID: v, eventType: domain.EventTypeVentaRegresadaABorrador, occurredAt: at(4, 9)},
	})

	assert.Equal(t, at(4, 9), out[v])
}

func TestReduceFaseDesde_EmptyInput(t *testing.T) {
	t.Parallel()

	assert.Empty(t, reduceFaseDesde(nil))
}

// ─── fase alcanzada ─────────────────────────────────────────────────────────
//
// Same rows, a DIFFERENT question: not "when did the venta last move" but
// "how far did it ever get". The two reductions disagree on purpose — see the
// comment on reduceFaseAlcanzada.

// TestReduceFaseAlcanzada_CanceladaKeepsTheFaseAlreadyReached is the exact
// defect this field exists to close: a venta cancelled while in revisada must
// still report having reached fase 2, so the desktop draws two arcs and not
// one.
func TestReduceFaseAlcanzada_CanceladaKeepsTheFaseAlreadyReached(t *testing.T) {
	t.Parallel()

	v := uuid.New()
	rows := []faseEventRow{
		{ventaID: v, eventType: domain.EventTypeVentaCreada, occurredAt: at(1, 9)},
		{ventaID: v, eventType: domain.EventTypeVentaEnviadaARevision, occurredAt: at(2, 9)},
		{ventaID: v, eventType: domain.EventTypeVentaCancelada, occurredAt: at(3, 9)},
	}

	assert.Equal(t, 2, reduceFaseAlcanzada(rows)[v],
		"cancelar no es avanzar, pero tampoco borra el avance")
	assert.Equal(t, at(3, 9), reduceFaseDesde(rows)[v],
		"the fase clock DOES restart on cancelada")
}

// TestReduceFaseAlcanzada_RegresadaABorradorDoesNotLowerTheMaximum is the
// test that pins the two reducers APART: the backwards transition restarts
// the clock (fase_desde) yet must never lower the ceiling (fase_alcanzada).
func TestReduceFaseAlcanzada_RegresadaABorradorDoesNotLowerTheMaximum(t *testing.T) {
	t.Parallel()

	v := uuid.New()
	rows := []faseEventRow{
		{ventaID: v, eventType: domain.EventTypeVentaCreada, occurredAt: at(1, 9)},
		{ventaID: v, eventType: domain.EventTypeVentaEnviadaARevision, occurredAt: at(2, 9)},
		{ventaID: v, eventType: domain.EventTypeVentaAprobada, occurredAt: at(3, 9)},
		{ventaID: v, eventType: domain.EventTypeVentaRegresadaABorrador, occurredAt: at(4, 9)},
	}

	assert.Equal(t, 3, reduceFaseAlcanzada(rows)[v],
		"the maximum is a ceiling: going back to borrador cannot lower it")
	assert.Equal(t, at(4, 9), reduceFaseDesde(rows)[v],
		"fase_desde, by contrast, moves to the regresada_a_borrador event")
}

// TestReduceFaseAlcanzada_AplicadaThenCanceladaStaysAtFour covers a venta
// cancelled after having been applied in Microsip.
func TestReduceFaseAlcanzada_AplicadaThenCanceladaStaysAtFour(t *testing.T) {
	t.Parallel()

	v := uuid.New()
	rows := []faseEventRow{
		{ventaID: v, eventType: domain.EventTypeVentaCreada, occurredAt: at(1, 9)},
		{ventaID: v, eventType: domain.EventTypeVentaEnviadaARevision, occurredAt: at(2, 9)},
		{ventaID: v, eventType: domain.EventTypeVentaAprobada, occurredAt: at(3, 9)},
		{ventaID: v, eventType: domain.EventTypeVentaAplicada, occurredAt: at(4, 9)},
		{ventaID: v, eventType: domain.EventTypeVentaCancelada, occurredAt: at(5, 9)},
	}

	assert.Equal(t, 4, reduceFaseAlcanzada(rows)[v])
}

// TestReduceFaseAlcanzada_IgnoresEditEventsAndRowOrder verifies edits never
// contribute a fase and that the maximum does not depend on row order.
func TestReduceFaseAlcanzada_IgnoresEditEventsAndRowOrder(t *testing.T) {
	t.Parallel()

	v := uuid.New()
	out := reduceFaseAlcanzada([]faseEventRow{
		{ventaID: v, eventType: domain.EventTypeVentaAprobada, occurredAt: at(3, 9)},
		{ventaID: v, eventType: domain.EventTypeVentaCreada, occurredAt: at(1, 9)},
		{ventaID: v, eventType: domain.EventTypeVentaHeaderActualizado, occurredAt: at(9, 9)},
		{ventaID: v, eventType: domain.EventTypeImagenAdjuntada, occurredAt: at(9, 10)},
	})

	assert.Equal(t, 3, out[v])
}

// TestReduceFaseAlcanzada_OnlyEditEvents_VentaIsAbsent pins the absence
// contract: no phase event, no number — never an invented 1.
func TestReduceFaseAlcanzada_OnlyEditEvents_VentaIsAbsent(t *testing.T) {
	t.Parallel()

	v := uuid.New()
	out := reduceFaseAlcanzada([]faseEventRow{
		{ventaID: v, eventType: domain.EventTypeVentaHeaderActualizado, occurredAt: at(9, 9)},
		{ventaID: v, eventType: domain.EventTypeVentaProductosReemplazados, occurredAt: at(9, 10)},
	})

	_, present := out[v]
	assert.False(t, present)
}

// TestReduceFases_CombinesBothAnswersPerVenta verifies the single reduction
// the repo actually returns: both values, keyed by venta, from ONE row set.
func TestReduceFases_CombinesBothAnswersPerVenta(t *testing.T) {
	t.Parallel()

	cancelada, enCurso, soloEdiciones := uuid.New(), uuid.New(), uuid.New()
	out := reduceFases([]faseEventRow{
		{ventaID: cancelada, eventType: domain.EventTypeVentaCreada, occurredAt: at(1, 9)},
		{ventaID: cancelada, eventType: domain.EventTypeVentaEnviadaARevision, occurredAt: at(2, 9)},
		{ventaID: cancelada, eventType: domain.EventTypeVentaCancelada, occurredAt: at(3, 9)},
		{ventaID: enCurso, eventType: domain.EventTypeVentaCreada, occurredAt: at(1, 10)},
		{ventaID: soloEdiciones, eventType: domain.EventTypeVentaHeaderActualizado, occurredAt: at(4, 9)},
	})

	require.Len(t, out, 2)
	assert.Equal(t, at(3, 9), out[cancelada].Desde)
	assert.Equal(t, 2, out[cancelada].Alcanzada)
	assert.Equal(t, at(1, 10), out[enCurso].Desde)
	assert.Equal(t, 1, out[enCurso].Alcanzada)
	assert.NotContains(t, out, soloEdiciones,
		"a venta with only edit events carries neither field")
}

// TestReduceFases_OnlyCanceladaRecorded_LeavesAlcanzadaUnknown covers the
// pre-outbox residue: a venta whose only phase row is the cancelación has a
// fase_desde but no fase it can be proven to have reached, so Alcanzada stays
// zero and the mapper omits it.
func TestReduceFases_OnlyCanceladaRecorded_LeavesAlcanzadaUnknown(t *testing.T) {
	t.Parallel()

	v := uuid.New()
	out := reduceFases([]faseEventRow{
		{ventaID: v, eventType: domain.EventTypeVentaCancelada, occurredAt: at(6, 9)},
	})

	require.Contains(t, out, v)
	assert.Equal(t, at(6, 9), out[v].Desde)
	assert.Equal(t, 0, out[v].Alcanzada, "zero means unknown, never fase 1")
}
