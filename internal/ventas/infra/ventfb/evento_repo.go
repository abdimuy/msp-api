//nolint:misspell // Spanish vocabulary (ventas, eventos) by convention.
package ventfb

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/outboxfb"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// EventoRepo implements outbound.VentaEventReader by reading the platform
// outbox (MSP_OUTBOX_EVENTS) for the venta aggregate. It projects each
// outbox row into the ventas-owned VentaEvento, dropping the dispatcher
// delivery internals the operator does not need to see.
type EventoRepo struct {
	pool *firebird.Pool
}

// NewEventoRepo builds an EventoRepo wired to the given pool.
func NewEventoRepo(pool *firebird.Pool) *EventoRepo {
	return &EventoRepo{pool: pool}
}

// Compile-time check: EventoRepo satisfies the outbound port.
var _ outbound.VentaEventReader = (*EventoRepo)(nil)

// EventosDeVenta returns the venta's events oldest-first. The read runs
// inside an explicit READ COMMITTED transaction so the firebirdsql driver
// commits cleanly instead of leaking an idle implicit tx.
func (r *EventoRepo) EventosDeVenta(
	ctx context.Context, ventaID uuid.UUID,
) ([]outbound.VentaEvento, error) {
	var out []outbound.VentaEvento
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		events, readErr := outboxfb.ReadByAggregateID(ctx, r.pool.DB, ventaID)
		if readErr != nil {
			return readErr
		}
		out = make([]outbound.VentaEvento, 0, len(events))
		for _, e := range events {
			out = append(out, outbound.VentaEvento{
				ID:         e.ID,
				EventType:  e.EventType,
				Payload:    e.Payload,
				OccurredAt: e.CreatedAt,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
