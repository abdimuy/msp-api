//nolint:misspell // ventas vocabulary is Spanish (clientes) per project convention.
package main

import (
	"log/slog"

	"go.uber.org/fx"

	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/imageprocessor"
	"github.com/abdimuy/msp-api/internal/platform/lifecycle"
	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
	"github.com/abdimuy/msp-api/internal/platform/outboxfb"
	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/infra/microsip"
	"github.com/abdimuy/msp-api/internal/ventas/infra/storage"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventoutbox"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventsearch"
	ventasoutbound "github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// provideVentasRepo builds the Firebird-backed VentaRepo.
func provideVentasRepo(p *firebird.Pool) ventasoutbound.VentaRepo {
	return ventfb.NewVentaRepo(p)
}

// provideVentasClienteChecker builds the Firebird-backed implementation of
// ClienteExistenceChecker that validates cliente_id references against
// Microsip's CLIENTES table.
func provideVentasClienteChecker(p *firebird.Pool) ventasoutbound.ClienteExistenceChecker {
	return ventfb.NewClienteRepo(p)
}

// provideVentasUsuarioChecker builds the Firebird-backed implementation of
// VendedorUsuarioExistenceChecker that validates each vendedor's usuario_id
// against MSP_USUARIOS before the venta INSERT — so unknown ids surface as
// a 422 vendedor_usuario_no_encontrado instead of a 409 firebird_fk_violation.
func provideVentasUsuarioChecker(p *firebird.Pool) ventasoutbound.VendedorUsuarioExistenceChecker {
	return ventfb.NewUsuarioExistenceRepo(p)
}

// provideVentasStorage selects the StorageProvider implementation from
// config.Storage. The factory returns the Filesystem provider in v1; an
// R2 stub stands in for the future Cloudflare R2 adapter (see ADR-0003).
func provideVentasStorage(cfg *config.Config) (ventasoutbound.StorageProvider, error) {
	return storage.New(cfg.Storage)
}

// provideVentasClock returns the production clock used by every ventas service.
func provideVentasClock() ventasoutbound.Clock { return ventasoutbound.ProductionClock{} }

// provideVentasOutboxEnqueuer builds the ventas-module wrapper around the
// platform outbox. Backed by Firebird per ADR-0008: the event row is
// INSERTed into MSP_OUTBOX_EVENTS inside the same firebird tx as the
// business write, so a tx rollback takes the event with it atomically.
func provideVentasOutboxEnqueuer(p *firebird.Pool) ventasoutbound.OutboxEnqueuer {
	return ventoutbox.NewEnqueuer(p)
}

// provideVentasEventReader builds the read side of the outbox for the venta
// detail timeline. It reads MSP_OUTBOX_EVENTS by aggregate_id and projects
// each row into a ventas-owned VentaEvento.
func provideVentasEventReader(p *firebird.Pool) ventasoutbound.VentaEventReader {
	return ventfb.NewEventoRepo(p)
}

// provideVentasUsuarioResolver builds the usuario name resolver used to label
// each timeline event with the usuario who triggered it.
func provideVentasUsuarioResolver(p *firebird.Pool) ventasoutbound.UsuarioNombreResolver {
	return ventfb.NewUsuarioNombreRepo(p)
}

// provideVentasAlmacenResolver builds the almacén name resolver used to label
// traspaso timeline events with the stock route (origen → destino) instead of
// opaque ALMACEN_IDs. ALMACENES is a Microsip table readable from the ventas
// fb adapter, so this needs no cross-module dependency.
func provideVentasAlmacenResolver(p *firebird.Pool) ventasoutbound.AlmacenNombreResolver {
	return ventfb.NewAlmacenNombreRepo(p)
}

// provideVentasZonaNombreResolver builds the zona name resolver used to label
// each venta's direccion with the zona NAME instead of the opaque
// ZONA_CLIENTE_ID, so the desktop listing does not need a local catalog.
// ZONAS_CLIENTES is a Microsip table readable from the ventas fb adapter, so
// this needs no cross-module dependency.
func provideVentasZonaNombreResolver(p *firebird.Pool) ventasoutbound.ZonaNombreResolver {
	return ventfb.NewZonaNombreRepo(p)
}

// provideVentasFaseResolver builds the resolver that answers WHEN each venta
// entered its current fase and HOW FAR it ever got, reading the
// phase-changing events from MSP_OUTBOX_EVENTS. It exists because neither
// UPDATED_AT (any edit bumps it) nor a revisada_at column (there is none) can
// tell the desktop how long a venta has been sitting where it is, and because
// a cancelación overwrites the situacion that says how far it had advanced.
func provideVentasFaseResolver(p *firebird.Pool) ventasoutbound.FaseResolver {
	return ventfb.NewFaseRepo(p)
}

// provideVentasImageProcessor selects the image-processing implementation
// for the ventas module. When IMAGEPROCESSOR_ENABLED=false the factory
// returns the NoOp passthrough so uploads land verbatim on disk.
func provideVentasImageProcessor(cfg *config.Config) (ventasoutbound.ImageProcessor, error) {
	return imageprocessor.New(cfg.ImageProcessor)
}

// provideVentasAplicarConfig builds the Firebird-backed AplicarConfig that
// resolves MSP_CFG_* mappings (zona → caja, frecuencia → forma_pago, etc.).
func provideVentasAplicarConfig(p *firebird.Pool) ventasoutbound.AplicarConfig {
	return ventfb.NewAplicarConfigRepo(p)
}

// provideVentasMicrosipWriter builds the Firebird-backed MicrosipVentaWriter
// that materializes approved ventas into Microsip's DOCTOS_PV family. When
// the inventario module is wired (the typical production case), the writer
// is parameterized with AlmacenDestinoVentasID so DOCTOS_PV references the
// reserved-stock pool the inventario traspaso has already populated.
func provideVentasMicrosipWriter(p *firebird.Pool, cfg *config.Config) ventasoutbound.MicrosipVentaWriter {
	return microsip.NewVentaWriter(p).
		WithAlmacenDestinoVentas(cfg.Inventario.AlmacenDestinoVentasID).
		WithTiempoCortoPlazoMeses(cfg.MicrosipVenta.TiempoCortoPlazoMeses).
		WithFormaCobroEnganche(cfg.MicrosipVenta.FormaCobroEnganche)
}

// provideVentasMicrosipClienteWriter builds the Firebird-backed
// MicrosipClienteWriter that auto-creates a Microsip cliente when AplicarVenta
// runs on a venta whose ClienteID is nil.
func provideVentasMicrosipClienteWriter(p *firebird.Pool, cfg *config.Config) ventasoutbound.MicrosipClienteWriter {
	return microsip.NewClienteWriter(p).WithLimiteCredito(cfg.MicrosipVenta.ClienteLimiteCredito)
}

// provideVentasMicrosipJuegoResolver builds the Firebird-backed
// MicrosipJuegoResolver that matches or creates a Microsip juego (kit) for
// each combo inside AplicarVenta. The resolver is always constructed but only
// invoked when MICROSIP_VENTA_JUEGOS_ENABLED=true and a non-nil resolver is
// wired into the Service via WithJuegos (see provideVentasService).
func provideVentasMicrosipJuegoResolver(p *firebird.Pool) ventasoutbound.MicrosipJuegoResolver {
	return microsip.NewJuegoResolver(p)
}

// provideVentasService assembles the ventas application service. Multi-step
// writes are coordinated through the supplied Firebird transaction manager.
// The inventario adapter is attached via WithInventario so CrearVenta /
// CancelarVenta exercise stock validation + automatic traspaso.
// p and cfg are used to wire the optional JuegoResolver (combo→juego feature).
//
// searchIndex is applied via WithSearchIndex ONLY when non-nil — see
// provideVentasSearchIndex: when Meilisearch is not configured it returns a
// literal nil interface (not a typed-nil pointer) so this guard actually
// works and BuscarVentas takes the Firebird-fallback branch.
func provideVentasService(
	repo ventasoutbound.VentaRepo,
	clientes ventasoutbound.ClienteExistenceChecker,
	usuarios ventasoutbound.VendedorUsuarioExistenceChecker,
	store ventasoutbound.StorageProvider,
	clock ventasoutbound.Clock,
	outbox ventasoutbound.OutboxEnqueuer,
	imageProc ventasoutbound.ImageProcessor,
	fbTxMgr *firebird.TxManager,
	aplicarCfg ventasoutbound.AplicarConfig,
	microsipWriter ventasoutbound.MicrosipVentaWriter,
	microsipCliente ventasoutbound.MicrosipClienteWriter,
	inv ventasoutbound.InventarioService,
	eventReader ventasoutbound.VentaEventReader,
	usuarioResolver ventasoutbound.UsuarioNombreResolver,
	almacenResolver ventasoutbound.AlmacenNombreResolver,
	zonaNombreResolver ventasoutbound.ZonaNombreResolver,
	faseResolver ventasoutbound.FaseResolver,
	searchIndex ventasoutbound.VentaSearchIndex,
	p *firebird.Pool,
	cfg *config.Config,
) *ventasapp.Service {
	svc := ventasapp.NewService(repo, clientes, usuarios, store, clock, outbox, imageProc, fbTxMgr, aplicarCfg, microsipWriter, microsipCliente).
		WithInventario(inv).
		WithEventReader(eventReader).
		WithUsuarioResolver(usuarioResolver).
		WithAlmacenResolver(almacenResolver).
		WithZonaNombreResolver(zonaNombreResolver).
		WithFaseResolver(faseResolver).
		WithJuegos(provideVentasMicrosipJuegoResolver(p), cfg.MicrosipVenta.JuegosEnabled, cfg.MicrosipVenta.JuegosLineaArticuloID).
		WithZonaReader(ventfb.NewClienteRepo(p)).
		WithEstatusReader(ventfb.NewClienteRepo(p)).
		WithReactivarCliente(cfg.MicrosipVenta.ReactivarClienteEnabled).
		WithZonaObligatoria(cfg.MicrosipVenta.ZonaObligatoria).
		WithCiudadCatalogo(ventfb.NewCiudadCatalogoRepo(p), cfg.MicrosipVenta.CiudadCatalogo)
	if searchIndex != nil {
		svc = svc.WithSearchIndex(searchIndex)
	}
	return svc
}

// provideVentasSearchIndex builds the Meilisearch-backed ventas search index.
// When Meilisearch is not configured (cfg.Meilisearch.URL == "") this
// returns a LITERAL nil interface — not a typed-nil *MeilisearchVentaSearchIndex
// — so provideVentasService's `searchIndex != nil` guard actually triggers
// and Service.BuscarVentas falls back to the Firebird keyset listing. A
// typed-nil pointer stored in an interface variable is never == nil, which
// would silently break the fallback in dev/test environments without
// MEILISEARCH_URL set.
func provideVentasSearchIndex(
	client platformmeili.Client,
	cfg *config.Config,
) ventasoutbound.VentaSearchIndex {
	if cfg.Meilisearch.URL == "" {
		return nil
	}
	return ventsearch.NewMeilisearchVentaSearchIndex(client, cfg.Meilisearch.VentasIndexName)
}

// provideVentasReindexHandlers builds one outbox handler per venta domain
// event type, all delegating to Service.ReindexVenta — the incremental
// half of ventas search freshness (the periodic VentasReconcileWorker is
// the drift-recovery half).
func provideVentasReindexHandlers(svc *ventasapp.Service) []outboxfb.Handler {
	return ventoutbox.NewVentaReindexHandlers(svc)
}

// registerVentasOutboxHandlers registers every ventas-module outbox handler
// on the shared registry. Must run before registerOutboxLifecycle so the
// dispatcher sees the handlers when it starts (mirrors
// registerAuthOutboxHandlers).
func registerVentasOutboxHandlers(reg *outboxfb.HandlerRegistry, hs []outboxfb.Handler) {
	for _, h := range hs {
		reg.Register(h)
	}
}

// provideVentasReconcileWorker builds the background worker that
// periodically materializes the Meilisearch ventas index from Firebird
// (warm-up + drift recovery). The interval is taken from
// cfg.Meilisearch.SyncInterval (default 5m) — mirrors
// provideClientesDirectoryReconcileWorker.
func provideVentasReconcileWorker(
	svc *ventasapp.Service,
	cfg *config.Config,
	logger *slog.Logger,
) *ventasapp.VentasReconcileWorker {
	return ventasapp.NewVentasReconcileWorker(
		svc,
		ventasapp.VentasReconcileWorkerConfig{Interval: cfg.Meilisearch.SyncInterval},
		logger,
	)
}

// registerVentasReconcileWorkerLifecycle hooks the ventas reconcile worker
// into the fx lifecycle.
func registerVentasReconcileWorkerLifecycle(
	lc fx.Lifecycle,
	w *ventasapp.VentasReconcileWorker,
) {
	lifecycle.Append(lc, "ventas-reconcile-worker", w)
}
