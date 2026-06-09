// Package app contains the ventas module's command and query services. It
// depends only on the ventas domain, the module's outbound ports, and a small
// set of platform helpers. Wiring (database pool, http handlers) lives in
// infra; cross-module surfaces live in the ventas root package.
//
//nolint:misspell // ventas vocabulary is Spanish (clientes, productos, etc.) per project convention.
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
	ventas          outbound.VentaRepo
	clientes        outbound.ClienteExistenceChecker
	usuarios        outbound.VendedorUsuarioExistenceChecker
	storage         outbound.StorageProvider
	clock           outbound.Clock
	outbox          outbound.OutboxEnqueuer
	imageProc       outbound.ImageProcessor
	txMgr           *firebird.TxManager
	aplicarCfg      outbound.AplicarConfig
	microsipWriter  outbound.MicrosipVentaWriter
	microsipCliente outbound.MicrosipClienteWriter
	// inventario is optional. Tests omit it; production wires it via
	// WithInventario. When nil, the venta lifecycle skips the stock-validation
	// + automatic-traspaso steps — the legacy behavior before the inventario
	// module existed.
	inventario outbound.InventarioService
	// eventReader is optional. Tests omit it; production wires it via
	// WithEventReader. When nil, EventosDeVenta returns an empty timeline
	// rather than failing — the read is purely informational.
	eventReader outbound.VentaEventReader
}

// WithInventario attaches an InventarioService so CrearVenta validates stock
// + emits the automatic traspaso, and CancelarVenta reverses it. Returns
// s for fluent wiring at the composition root.
func (s *Service) WithInventario(inv outbound.InventarioService) *Service {
	s.inventario = inv
	return s
}

// WithEventReader attaches a VentaEventReader so EventosDeVenta can surface
// the venta's outbox event timeline. Returns s for fluent wiring at the
// composition root.
func (s *Service) WithEventReader(r outbound.VentaEventReader) *Service {
	s.eventReader = r
	return s
}

// NewService builds a Service wired against the given ports. The
// *firebird.TxManager is required so multi-step writes (e.g. CrearVenta)
// run inside a single transaction; pass nil only in tests that exercise
// in-memory fakes which do not need transactional semantics.
//
// clientes is consulted to validate the optional cliente_id on a venta —
// pass nil only in tests that do not exercise the cliente link.
//
// usuarios is consulted to validate that every vendedor on a CrearVenta
// request has a row in MSP_USUARIOS — pass nil only in tests that do not
// exercise vendedor validation.
//
// imageProc transforms image uploads (resize + recompress) before they
// reach the storage provider. Pass the NoOp impl for a passthrough.
//
// aplicarCfg resolves the MSP_CFG_* mappings needed by AplicarVenta.
// Pass nil only in tests that do not exercise that command.
//
// microsipWriter materializes ventas into Microsip's DOCTOS_PV family.
// Pass nil only in tests that do not exercise AplicarVenta.
//
// microsipCliente auto-creates a Microsip cliente when AplicarVenta runs on a
// venta whose ClienteID is nil — pass nil only in tests that do not exercise
// the auto-create branch.
func NewService(
	ventas outbound.VentaRepo,
	clientes outbound.ClienteExistenceChecker,
	usuarios outbound.VendedorUsuarioExistenceChecker,
	storage outbound.StorageProvider,
	clock outbound.Clock,
	outbox outbound.OutboxEnqueuer,
	imageProc outbound.ImageProcessor,
	txMgr *firebird.TxManager,
	aplicarCfg outbound.AplicarConfig,
	microsipWriter outbound.MicrosipVentaWriter,
	microsipCliente outbound.MicrosipClienteWriter,
) *Service {
	return &Service{
		ventas:          ventas,
		clientes:        clientes,
		usuarios:        usuarios,
		storage:         storage,
		clock:           clock,
		outbox:          outbox,
		imageProc:       imageProc,
		txMgr:           txMgr,
		aplicarCfg:      aplicarCfg,
		microsipWriter:  microsipWriter,
		microsipCliente: microsipCliente,
	}
}

// validateClienteID consults the configured checker to ensure clienteID
// (when non-nil) points to a real row in Microsip's CLIENTES. Nil pointer
// or nil checker short-circuits to (nil) — the cliente link is optional.
func (s *Service) validateClienteID(ctx context.Context, clienteID *int) error {
	if clienteID == nil || s.clientes == nil {
		return nil
	}
	ok, err := s.clientes.Exists(ctx, *clienteID)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrClienteIDInvalido
	}
	return nil
}

// validateVendedorUsuarios consults the configured checker to ensure every
// usuario_id in the supplied vendedores has a matching row in MSP_USUARIOS.
// Nil checker or empty input short-circuits to (nil). When at least one id
// is missing, returns domain.ErrVendedorUsuarioNoEncontrado with the
// missing ids attached as details so the HTTP layer can name the offender.
func (s *Service) validateVendedorUsuarios(ctx context.Context, vendedores []CrearVentaVendedorInput) error {
	if s.usuarios == nil || len(vendedores) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(vendedores))
	for i, v := range vendedores {
		ids[i] = v.UsuarioID
	}
	missing, err := s.usuarios.MissingIDs(ctx, ids)
	if err != nil {
		return err
	}
	if len(missing) == 0 {
		return nil
	}
	missingStrs := make([]string, len(missing))
	for i, id := range missing {
		missingStrs[i] = id.String()
	}
	return domain.ErrVendedorUsuarioNoEncontrado.WithField("usuario_ids", missingStrs)
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
