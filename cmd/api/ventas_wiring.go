//nolint:misspell // ventas vocabulary is Spanish (clientes) per project convention.
package main

import (
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/imageprocessor"
	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/infra/microsip"
	"github.com/abdimuy/msp-api/internal/ventas/infra/storage"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventoutbox"
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

// provideVentasService assembles the ventas application service. Multi-step
// writes are coordinated through the supplied Firebird transaction manager.
// The inventario adapter is attached via WithInventario so CrearVenta /
// CancelarVenta exercise stock validation + automatic traspaso.
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
) *ventasapp.Service {
	return ventasapp.NewService(repo, clientes, usuarios, store, clock, outbox, imageProc, fbTxMgr, aplicarCfg, microsipWriter, microsipCliente).
		WithInventario(inv).
		WithEventReader(eventReader).
		WithUsuarioResolver(usuarioResolver).
		WithAlmacenResolver(almacenResolver)
}
