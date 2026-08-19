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
	// usuarioResolver is optional. Tests omit it; production wires it via
	// WithUsuarioResolver. When nil, EventosDeVenta leaves event ActorNombre
	// empty — actor labels are best-effort and their absence must not break
	// the timeline.
	usuarioResolver outbound.UsuarioNombreResolver
	// almacenResolver is optional. Tests omit it; production wires it via
	// WithAlmacenResolver. When nil, EventosDeVenta leaves traspaso events'
	// almacén names unresolved — the route labels are best-effort and their
	// absence must not break the timeline.
	almacenResolver outbound.AlmacenNombreResolver
	// zonaNombreResolver is optional. Tests omit it; production wires it via
	// WithZonaNombreResolver. When nil, NombresDeZonas returns an empty map
	// and the venta DTO simply carries no zona name — the label is
	// best-effort and its absence must not break the read.
	zonaNombreResolver outbound.ZonaNombreResolver
	// faseResolver is optional. Tests omit it; production wires it via
	// WithFaseResolver. When nil, Fases returns an empty map and the venta
	// DTO simply carries neither fase_desde nor fase_alcanzada — the
	// desktop's "Fase" column degrades to showing no elapsed time and its
	// progress ring to showing no arcs, which must never cost the read
	// itself.
	faseResolver outbound.FaseResolver
	// juegoResolver is optional. Tests omit it; production wires it via
	// WithJuegos. When nil or juegosEnabled is false, AplicarVenta skips the
	// combo→juego resolution step and passes an empty JuegosPorCombo map to
	// the writer (pre-juego behavior is preserved).
	juegoResolver         outbound.MicrosipJuegoResolver
	juegosEnabled         bool
	juegosLineaArticuloID int
	// ciudadCatalogo resolves the captured ciudad against Microsip's CIUDADES
	// catalog. Optional: when nil or ciudadCatalogoEnabled is false, the
	// cliente auto-create keeps writing the fixed Tehuacán/Puebla defaults.
	// Set via WithCiudadCatalogo.
	ciudadCatalogo        outbound.CiudadCatalogo
	ciudadCatalogoEnabled bool
	// zonaObligatoria requires a zona on every venta and resolves caja/cajero
	// from MSP_CFG_ZONA_CAJA for all venta types. When false (the default),
	// CONTADO ventas fall back to the fixed mostrador caja so the backlog of
	// ventas captured without a zona can still drain. Set via
	// WithZonaObligatoria.
	zonaObligatoria bool
	// zonaReader is optional. Tests omit it; production wires it via
	// WithZonaReader. When nil, the zona mismatch check is skipped — used for
	// tests that do not exercise the pre-existing cliente branch.
	zonaReader outbound.ClienteZonaReader
	// estatusReader is optional. Tests omit it; production wires it via
	// WithEstatusReader. When nil, EstatusMicrosipDeCliente returns nil — used
	// for tests that do not exercise the cliente estatus hint.
	estatusReader outbound.ClienteEstatusReader
	// searchIndex is optional. Tests omit it; production wires it via
	// WithSearchIndex. When nil, BuscarVentas falls back to the Firebird
	// keyset listing (ListarVentas) — the pre-Meilisearch behavior is
	// preserved exactly until the index is wired at the composition root.
	searchIndex outbound.VentaSearchIndex
	// reactivarClienteEnabled gates the cliente-reactivation step inside
	// AplicarVenta (MICROSIP_REACTIVAR_CLIENTE_ENABLED, default false). When
	// false, AplicarVenta never calls microsipCliente.ReactivarSiEnBaja —
	// pre-feature behavior is preserved exactly. Wired via
	// WithReactivarCliente.
	reactivarClienteEnabled bool
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

// WithUsuarioResolver attaches a UsuarioNombreResolver so EventosDeVenta can
// label each event with the usuario who triggered it. Returns s for fluent
// wiring at the composition root.
func (s *Service) WithUsuarioResolver(r outbound.UsuarioNombreResolver) *Service {
	s.usuarioResolver = r
	return s
}

// WithAlmacenResolver attaches an AlmacenNombreResolver so EventosDeVenta can
// resolve traspaso events' almacén ids to names (origen → destino route).
// Returns s for fluent wiring at the composition root.
func (s *Service) WithAlmacenResolver(r outbound.AlmacenNombreResolver) *Service {
	s.almacenResolver = r
	return s
}

// WithZonaNombreResolver attaches a ZonaNombreResolver so the venta read
// paths can label each direccion with its zona name. Returns s for fluent
// wiring at the composition root.
func (s *Service) WithZonaNombreResolver(r outbound.ZonaNombreResolver) *Service {
	s.zonaNombreResolver = r
	return s
}

// WithFaseResolver attaches a FaseResolver so the venta read paths can report
// WHEN each venta entered its current fase and HOW FAR it ever got. Returns s
// for fluent wiring at the composition root.
func (s *Service) WithFaseResolver(r outbound.FaseResolver) *Service {
	s.faseResolver = r
	return s
}

// WithZonaReader attaches a ClienteZonaReader so AplicarVenta can verify the
// venta's zona matches the pre-existing cliente's zona in Microsip.
func (s *Service) WithZonaReader(r outbound.ClienteZonaReader) *Service {
	s.zonaReader = r
	return s
}

// WithEstatusReader attaches a ClienteEstatusReader so the venta detail read
// can surface the cliente's current ESTATUS in Microsip.
func (s *Service) WithEstatusReader(r outbound.ClienteEstatusReader) *Service {
	s.estatusReader = r
	return s
}

// WithSearchIndex attaches a VentaSearchIndex so BuscarVentas queries
// Meilisearch instead of falling back to the Firebird keyset listing.
// Returns s for fluent wiring at the composition root.
func (s *Service) WithSearchIndex(idx outbound.VentaSearchIndex) *Service {
	s.searchIndex = idx
	return s
}

// WithJuegos attaches the MicrosipJuegoResolver and enables the combo→juego
// resolution step inside AplicarVenta. When enabled is false the resolver is
// stored but never called — the feature can be toggled without rewiring.
// When r is nil the feature is always off regardless of enabled.
// Returns s for fluent wiring at the composition root.
func (s *Service) WithJuegos(r outbound.MicrosipJuegoResolver, enabled bool, lineaArticuloID int) *Service {
	s.juegoResolver = r
	s.juegosEnabled = enabled
	s.juegosLineaArticuloID = lineaArticuloID
	return s
}

// WithZonaObligatoria toggles the zona requirement inside AplicarVenta (see
// zonaObligatoria). Returns s for fluent wiring at the composition root.
func (s *Service) WithZonaObligatoria(enabled bool) *Service {
	s.zonaObligatoria = enabled
	return s
}

// WithCiudadCatalogo attaches the CIUDADES resolver and enables resolving the
// captured ciudad instead of writing the fixed defaults. When c is nil the
// feature is off regardless of enabled. Returns s for fluent wiring at the
// composition root.
func (s *Service) WithCiudadCatalogo(c outbound.CiudadCatalogo, enabled bool) *Service {
	s.ciudadCatalogo = c
	s.ciudadCatalogoEnabled = enabled && c != nil
	return s
}

// WithReactivarCliente toggles the cliente-reactivation step inside
// AplicarVenta (see reactivarClienteEnabled). Returns s for fluent wiring at
// the composition root.
func (s *Service) WithReactivarCliente(enabled bool) *Service {
	s.reactivarClienteEnabled = enabled
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

// NombresDeUsuarios resolves usuario display names for the given ids,
// best-effort. A nil resolver, an empty input, or a lookup error all yield an
// empty map rather than failing — these names decorate the venta-detail audit
// panel (created_by / updated_by / aprobada_by) and their absence must not
// break the read. Ids without a matching MSP_USUARIOS row are simply absent
// from the returned map.
func (s *Service) NombresDeUsuarios(ctx context.Context, ids []uuid.UUID) map[uuid.UUID]string {
	if s.usuarioResolver == nil || len(ids) == 0 {
		return map[uuid.UUID]string{}
	}
	nombres, err := s.usuarioResolver.NombresPorID(ctx, ids)
	if err != nil {
		return map[uuid.UUID]string{}
	}
	return nombres
}

// NombresDeZonas resolves the display name of every zona referenced by the
// given ventas, best-effort and in a SINGLE resolver call per page. A nil
// resolver, an empty page, a page whose ventas carry no zona, or a lookup
// error all yield an empty map rather than failing — the zona name decorates
// the venta's direccion and its absence must not break the read. Ids without
// a matching ZONAS_CLIENTES row are simply absent from the returned map.
func (s *Service) NombresDeZonas(ctx context.Context, ventas []*domain.Venta) map[int]string {
	if s.zonaNombreResolver == nil || len(ventas) == 0 {
		return map[int]string{}
	}
	ids := make([]int, 0, len(ventas))
	seen := make(map[int]struct{}, len(ventas))
	for _, v := range ventas {
		if v == nil {
			continue
		}
		z := v.Direccion().ZonaClienteID()
		if z == nil {
			continue
		}
		if _, dup := seen[*z]; dup {
			continue
		}
		seen[*z] = struct{}{}
		ids = append(ids, *z)
	}
	if len(ids) == 0 {
		return map[int]string{}
	}
	nombres, err := s.zonaNombreResolver.NombresPorID(ctx, ids)
	if err != nil {
		return map[int]string{}
	}
	return nombres
}

// Fases resolves, for the given ventas, WHEN each entered its current fase
// and the HIGHEST fase each ever reached — best-effort and in a SINGLE
// resolver call per page. A nil resolver, an empty page, or a lookup error
// all yield an empty map rather than failing: both fields are decoration and
// their absence must not break the read. Ventas with no phase event recorded
// (captured before the event timeline existed) are simply absent from the
// returned map, so the caller omits the fields instead of inventing a date or
// a fase.
func (s *Service) Fases(ctx context.Context, ventas []*domain.Venta) map[uuid.UUID]outbound.FaseDeVenta {
	if s.faseResolver == nil || len(ventas) == 0 {
		return map[uuid.UUID]outbound.FaseDeVenta{}
	}
	ids := make([]uuid.UUID, 0, len(ventas))
	for _, v := range ventas {
		if v == nil {
			continue
		}
		ids = append(ids, v.ID())
	}
	if len(ids) == 0 {
		return map[uuid.UUID]outbound.FaseDeVenta{}
	}
	fases, err := s.faseResolver.FasesPorVenta(ctx, ids)
	if err != nil {
		return map[uuid.UUID]outbound.FaseDeVenta{}
	}
	return fases
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
