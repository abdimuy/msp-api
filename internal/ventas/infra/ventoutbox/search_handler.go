//nolint:misspell // ventas vocabulary is Spanish per project convention.
package ventoutbox

import (
	"context"
	"errors"

	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
	"github.com/abdimuy/msp-api/internal/platform/outboxfb"
	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// ventaEventTypes lists every venta domain event that must trigger a
// reindex. Kept in one slice so NewVentaReindexHandlers registers exactly
// one handler per type — a new event type added to domain/events.go later
// needs a matching entry here, otherwise the outbox never claims rows of
// that type and the ventas mutated by it only refresh on the next full
// VentasReconcileWorker tick.
var ventaEventTypes = []string{
	domain.EventTypeVentaCreada,
	domain.EventTypeVentaCancelada,
	domain.EventTypeImagenAdjuntada,
	domain.EventTypeImagenEliminada,
	domain.EventTypeVentaHeaderActualizado,
	domain.EventTypeVentaClienteActualizado,
	domain.EventTypeVentaProductosReemplazados,
	domain.EventTypeVentaCombosReemplazados,
	domain.EventTypeVentaVendedoresReemplazados,
	domain.EventTypeVentaEnviadaARevision,
	domain.EventTypeVentaAprobada,
	domain.EventTypeVentaRegresadaABorrador,
	domain.EventTypeVentaAplicada,
}

// reindexHandler routes a single venta domain event type to
// Service.ReindexVenta. One instance is registered per event type (13
// total, see ventaEventTypes) so the outbox dispatcher's
// EVENT_TYPE IN (...) filter claims every venta mutation.
type reindexHandler struct {
	eventType string
	svc       *ventasapp.Service
}

// Compile-time check: reindexHandler satisfies outboxfb.Handler.
var _ outboxfb.Handler = (*reindexHandler)(nil)

// EventType returns the routing key for the outbox dispatcher.
func (h *reindexHandler) EventType() string { return h.eventType }

// Handle reindexes the venta named by the event's aggregate id. Idempotent
// — the dispatcher may invoke Handle more than once for the same event.
// A transient Meilisearch failure (network blip, 5xx, timeout) is
// translated to outboxfb.ErrTransient so the dispatcher retries with
// backoff instead of marking the event permanently failed.
func (h *reindexHandler) Handle(ctx context.Context, e outboxfb.Event) error {
	if err := h.svc.ReindexVenta(ctx, e.AggregateID); err != nil {
		if errors.Is(err, platformmeili.ErrMeilisearchTransient) {
			return outboxfb.ErrTransient
		}
		return err
	}
	return nil
}

// NewVentaReindexHandlers builds one reindexHandler per venta domain event
// type, all delegating to svc.ReindexVenta. Register every handler on the
// shared outboxfb.HandlerRegistry (see cmd/api's
// registerVentasOutboxHandlers) before the outbox dispatcher starts.
func NewVentaReindexHandlers(svc *ventasapp.Service) []outboxfb.Handler {
	handlers := make([]outboxfb.Handler, 0, len(ventaEventTypes))
	for _, t := range ventaEventTypes {
		handlers = append(handlers, &reindexHandler{eventType: t, svc: svc})
	}
	return handlers
}
