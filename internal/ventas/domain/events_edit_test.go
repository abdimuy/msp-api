package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// TestEditEvents_Constructors_AndAccessors covers every edit-event type's
// EventType / AggregateID / OccurredAt / Payload surface in one sweep. The
// aggregate-driven happy-path tests already verify Append-then-emit; this
// fills the remaining branches on Payload/Occured/Aggregate accessors and on
// the items-count field.
func TestEditEvents_Constructors_AndAccessors(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	by := uuid.New()
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

	t.Run("header", func(t *testing.T) {
		t.Parallel()
		e := domain.NewVentaHeaderActualizadoEvent(id, by, now)
		assert.Equal(t, "venta.header_actualizado", e.EventType())
		assert.Equal(t, id, e.AggregateID())
		assert.Equal(t, now, e.OccurredAt())
		p := e.Payload()
		assert.Equal(t, id.String(), p["venta_id"])
		assert.Equal(t, by.String(), p["updated_by"])
	})

	t.Run("cliente", func(t *testing.T) {
		t.Parallel()
		e := domain.NewVentaClienteActualizadoEvent(id, by, now)
		assert.Equal(t, "venta.cliente_actualizado", e.EventType())
		assert.Equal(t, id, e.AggregateID())
		assert.Equal(t, now, e.OccurredAt())
		p := e.Payload()
		assert.Equal(t, id.String(), p["venta_id"])
	})

	t.Run("productos", func(t *testing.T) {
		t.Parallel()
		e := domain.NewVentaProductosReemplazadosEvent(id, 3, by, now)
		assert.Equal(t, "venta.productos_reemplazados", e.EventType())
		assert.Equal(t, id, e.AggregateID())
		assert.Equal(t, now, e.OccurredAt())
		p := e.Payload()
		assert.Equal(t, id.String(), p["venta_id"])
		assert.Equal(t, 3, p["productos_count"])
	})

	t.Run("combos", func(t *testing.T) {
		t.Parallel()
		e := domain.NewVentaCombosReemplazadosEvent(id, 2, by, now)
		assert.Equal(t, "venta.combos_reemplazados", e.EventType())
		assert.Equal(t, id, e.AggregateID())
		assert.Equal(t, now, e.OccurredAt())
		assert.Equal(t, 2, e.Payload()["combos_count"])
	})

	t.Run("vendedores", func(t *testing.T) {
		t.Parallel()
		e := domain.NewVentaVendedoresReemplazadosEvent(id, 1, by, now)
		assert.Equal(t, "venta.vendedores_reemplazados", e.EventType())
		assert.Equal(t, id, e.AggregateID())
		assert.Equal(t, now, e.OccurredAt())
		assert.Equal(t, 1, e.Payload()["vendedores_count"])
	})
}

// TestEditEvents_ImplementEventInterface verifies every edit event satisfies
// domain.Event at compile time AND at runtime through a uniform slice.
func TestEditEvents_ImplementEventInterface(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	by := uuid.New()
	now := time.Now()
	events := []domain.Event{
		domain.NewVentaHeaderActualizadoEvent(id, by, now),
		domain.NewVentaClienteActualizadoEvent(id, by, now),
		domain.NewVentaProductosReemplazadosEvent(id, 1, by, now),
		domain.NewVentaCombosReemplazadosEvent(id, 1, by, now),
		domain.NewVentaVendedoresReemplazadosEvent(id, 1, by, now),
	}
	for _, e := range events {
		require.NotEmpty(t, e.EventType())
		assert.Equal(t, id, e.AggregateID())
		assert.NotNil(t, e.Payload())
		assert.False(t, e.OccurredAt().IsZero())
	}
}
