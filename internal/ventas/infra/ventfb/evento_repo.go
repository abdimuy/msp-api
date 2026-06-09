//nolint:misspell // Spanish vocabulary (ventas, eventos) by convention.
package ventfb

import (
	"context"
	"sort"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/outboxfb"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// outboxAggregateTraspaso is the AGGREGATE value the inventario module stamps
// on its outbox events. Traspaso events are keyed by their own traspaso id, so
// the ones created for a venta carry the venta id only inside their payload —
// the timeline reader pulls them in via that payload link. Duplicated here as
// a literal (not imported) to keep the ventas slice free of an inventario
// dependency.
const outboxAggregateTraspaso = "traspaso"

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
//
// Besides the events keyed directly to the venta aggregate, the timeline also
// folds in traspaso events created for this venta — those are keyed by their
// own traspaso id and reference the venta only inside their payload, so they
// are pulled in via that payload link and merged into the chronological order.
func (r *EventoRepo) EventosDeVenta(
	ctx context.Context, ventaID uuid.UUID,
) ([]outbound.VentaEvento, error) {
	var out []outbound.VentaEvento
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		events, readErr := outboxfb.ReadByAggregateID(ctx, r.pool.DB, ventaID)
		if readErr != nil {
			return readErr
		}

		// venta_id is serialized by the enqueuer's json.Marshal with sorted
		// keys and no spaces, so this exact fragment reliably matches a
		// traspaso payload that carries this venta.
		needle := `"venta_id":"` + ventaID.String() + `"`
		traspasos, readErr := outboxfb.ReadByAggregateAndPayloadContaining(
			ctx, r.pool.DB, outboxAggregateTraspaso, needle,
		)
		if readErr != nil {
			return readErr
		}

		merged := make([]outboxfb.Event, 0, len(events)+len(traspasos))
		merged = append(merged, events...)
		merged = append(merged, traspasos...)
		// Stable sort keeps the per-source oldest-first order for events that
		// share a timestamp while interleaving the two sources chronologically.
		sort.SliceStable(merged, func(i, j int) bool {
			return merged[i].CreatedAt.Before(merged[j].CreatedAt)
		})

		out = make([]outbound.VentaEvento, 0, len(merged))
		for _, e := range merged {
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
