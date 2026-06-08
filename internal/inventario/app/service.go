// Package app contains the inventario module's command and query services. It
// depends only on the inventario domain, the module's outbound ports, and a
// small set of platform helpers. Wiring (database pool, http handlers) lives
// in infra; cross-module surfaces live in the inventario root package.
//
//nolint:misspell // inventario vocabulary is Spanish (traspaso, almacén, etc.) per project convention.
package app

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
	"github.com/abdimuy/msp-api/internal/inventario/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Outbox aggregate constant. Kept here so the string is not free-floating
// across the package; event type strings are pulled from the domain events
// themselves via Event.EventType().
const outboxAggregateTraspaso = "traspaso"

// Service is the inventario module's command/query surface. Handlers depend
// on *Service; everything Service depends on goes through the outbound ports.
type Service struct {
	traspasos   outbound.TraspasoRepo
	existencia  outbound.ExistenciaQuery
	folioMinter outbound.FolioMinter
	almacenes   outbound.AlmacenRepo
	clock       outbound.Clock
	outbox      outbound.OutboxEnqueuer
	txMgr       *firebird.TxManager
}

// NewService builds a Service wired against the given ports. The
// *firebird.TxManager is required so multi-step writes (e.g. CrearTraspasoParaVenta)
// run inside a single transaction; pass nil only in tests that exercise
// in-memory fakes which do not need transactional semantics.
//
// folioMinter allocates folio numbers from Microsip's GEN_MST_FOLIO sequence.
// Pass a counter-backed fake in tests.
//
// almacenes provides catalog reads for the ALMACENES table. Pass nil only in
// tests that do not exercise almacén validation.
func NewService(
	traspasos outbound.TraspasoRepo,
	existencia outbound.ExistenciaQuery,
	folioMinter outbound.FolioMinter,
	almacenes outbound.AlmacenRepo,
	clock outbound.Clock,
	outbox outbound.OutboxEnqueuer,
	txMgr *firebird.TxManager,
) *Service {
	return &Service{
		traspasos:   traspasos,
		existencia:  existencia,
		folioMinter: folioMinter,
		almacenes:   almacenes,
		clock:       clock,
		outbox:      outbox,
		txMgr:       txMgr,
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

// runInTxNoWait delegates to TxManager.RunInTxNoWait when one is wired,
// otherwise invokes fn directly. Used for stock validation to fail fast on
// lock contention rather than blocking.
func (s *Service) runInTxNoWait(ctx context.Context, fn func(context.Context) error) error {
	if s.txMgr == nil {
		return fn(ctx)
	}
	return s.txMgr.RunInTxNoWait(ctx, fn)
}

// enqueueEvent best-effort enqueues an outbox event. Failures are logged with
// the payload but never block the business write — consistent with the
// platform/outbox contract.
func (s *Service) enqueueEvent(ctx context.Context, aggregate string, aggregateID uuid.UUID, eventType string, payload any) {
	if s.outbox == nil {
		return
	}
	if err := s.outbox.Enqueue(ctx, aggregate, aggregateID, eventType, payload); err != nil {
		slog.WarnContext(
			ctx, "inventario.outbox_enqueue_failed",
			"aggregate", aggregate,
			"aggregate_id", aggregateID,
			"event_type", eventType,
			"error", err,
		)
	}
}

// drainEvents forwards each pending event on t to the outbox and clears the
// aggregate's buffer. Best-effort — see enqueueEvent.
func (s *Service) drainEvents(ctx context.Context, t *domain.Traspaso) {
	for _, ev := range t.PendingEvents() {
		s.enqueueEvent(ctx, outboxAggregateTraspaso, ev.AggregateID(), ev.EventType(), ev.Payload())
	}
	t.ClearPendingEvents()
}
