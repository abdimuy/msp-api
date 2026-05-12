// Package app contains the ventas module's command and query services. It
// depends only on the ventas domain, the module's outbound ports, and a small
// set of platform helpers. Wiring (database pool, http handlers) lives in
// infra; cross-module surfaces live in the ventas root package.
package app

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// Outbox aggregate constant. Kept here so the string is not free-floating
// across the package; the linter and grep agree on the canonical spelling.
// Event type strings are pulled from the domain events themselves via
// Event.EventType() so the canonical names live in one place.
const outboxAggregateVenta = "venta"

// Service is the ventas module's command/query surface. Handlers depend on
// *Service; everything Service depends on goes through the outbound ports.
type Service struct {
	ventas    outbound.VentaRepo
	storage   outbound.StorageProvider
	clock     outbound.Clock
	outbox    outbound.OutboxEnqueuer
	imageProc outbound.ImageProcessor
	txMgr     *firebird.TxManager
}

// NewService builds a Service wired against the given ports. The
// *firebird.TxManager is required so multi-step writes (e.g. CrearVenta)
// run inside a single transaction; pass nil only in tests that exercise
// in-memory fakes which do not need transactional semantics.
//
// imageProc transforms image uploads (resize + recompress) before they
// reach the storage provider. Pass the NoOp impl for a passthrough.
func NewService(
	ventas outbound.VentaRepo,
	storage outbound.StorageProvider,
	clock outbound.Clock,
	outbox outbound.OutboxEnqueuer,
	imageProc outbound.ImageProcessor,
	txMgr *firebird.TxManager,
) *Service {
	return &Service{
		ventas:    ventas,
		storage:   storage,
		clock:     clock,
		outbox:    outbox,
		imageProc: imageProc,
		txMgr:     txMgr,
	}
}

// runInTx delegates to the configured TxManager when one is wired, otherwise
// invokes fn directly so in-memory tests can omit a TxManager.
func (s *Service) runInTx(ctx context.Context, fn func(context.Context) error) error {
	if s.txMgr == nil {
		return fn(ctx)
	}
	return s.txMgr.RunInTx(ctx, fn)
}

// enqueueEvent best-effort enqueues an outbox event. Failures are logged
// with the payload but never block the business write — consistent with the
// platform/outbox contract.
func (s *Service) enqueueEvent(ctx context.Context, aggregate string, aggregateID uuid.UUID, eventType string, payload any) {
	if s.outbox == nil {
		return
	}
	if err := s.outbox.Enqueue(ctx, aggregate, aggregateID, eventType, payload); err != nil {
		slog.WarnContext(
			ctx, "ventas.outbox_enqueue_failed",
			"aggregate", aggregate,
			"aggregate_id", aggregateID,
			"event_type", eventType,
			"error", err,
		)
	}
}

// drainEvents forwards each pending event on v to the outbox and clears the
// aggregate's buffer. Best-effort — see enqueueEvent.
func (s *Service) drainEvents(ctx context.Context, v *domain.Venta) {
	for _, ev := range v.PendingEvents() {
		s.enqueueEvent(ctx, outboxAggregateVenta, ev.AggregateID(), ev.EventType(), ev.Payload())
	}
	v.ClearPendingEvents()
}
