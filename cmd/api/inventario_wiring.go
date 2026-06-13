//nolint:misspell // inventario vocabulary is Spanish per project convention.
package main

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/inventario"
	inventarioapp "github.com/abdimuy/msp-api/internal/inventario/app"
	"github.com/abdimuy/msp-api/internal/inventario/infra/invfb"
	"github.com/abdimuy/msp-api/internal/inventario/infra/invoutbox"
	inventariooutbound "github.com/abdimuy/msp-api/internal/inventario/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	ventasoutbound "github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// provideInventarioTraspasoRepo builds the Firebird-backed TraspasoRepo.
func provideInventarioTraspasoRepo(p *firebird.Pool, cfg *config.Config) inventariooutbound.TraspasoRepo {
	return invfb.NewTraspasoRepo(cfg.Inventario, p)
}

// provideInventarioExistenciaQuery builds the Firebird-backed existencia
// reader against Microsip's SALDOS_IN table.
func provideInventarioExistenciaQuery(p *firebird.Pool) inventariooutbound.ExistenciaQuery {
	return invfb.NewExistenciaQuerier(p)
}

// provideInventarioFolioMinter builds the Firebird-backed atomic folio
// generator backed by Microsip's GEN_MST_FOLIO sequence.
func provideInventarioFolioMinter(p *firebird.Pool) inventariooutbound.FolioMinter {
	return invfb.NewFolioMinter(p)
}

// provideInventarioAlmacenRepo builds the Firebird-backed ALMACENES reader.
func provideInventarioAlmacenRepo(p *firebird.Pool) inventariooutbound.AlmacenRepo {
	return invfb.NewAlmacenRepo(p)
}

// provideInventarioClock returns the production clock used by inventario.
func provideInventarioClock() inventariooutbound.Clock {
	return inventariooutbound.ProductionClock{}
}

// provideInventarioOutboxEnqueuer builds the inventario-module wrapper
// around the platform outbox. Backed by Firebird per ADR-0008: the event
// row is INSERTed into MSP_OUTBOX_EVENTS inside the same firebird tx as
// the business write, so a tx rollback takes the traspaso.creado event
// with it atomically — the exact gap that motivated this ADR.
func provideInventarioOutboxEnqueuer(p *firebird.Pool) inventariooutbound.OutboxEnqueuer {
	return invoutbox.NewEnqueuer(p)
}

// provideInventarioService assembles the inventario application service.
func provideInventarioService(
	traspasos inventariooutbound.TraspasoRepo,
	existencia inventariooutbound.ExistenciaQuery,
	folioMinter inventariooutbound.FolioMinter,
	almacenes inventariooutbound.AlmacenRepo,
	clock inventariooutbound.Clock,
	outbox inventariooutbound.OutboxEnqueuer,
	fbTxMgr *firebird.TxManager,
) *inventarioapp.Service {
	return inventarioapp.NewService(traspasos, existencia, folioMinter, almacenes, clock, outbox, fbTxMgr)
}

// provideInventarioServiceAdapter wraps the inventario app.Service in the
// public contract adapter exposed under the inventario module root. Other
// modules that consume inventario.TraspasoService receive this value.
func provideInventarioServiceAdapter(svc *inventarioapp.Service) inventario.TraspasoService {
	return inventario.NewServiceAdapter(svc)
}

// ventasInventarioAdapter bridges the ventas module's outbound.InventarioService
// port to the inventario.TraspasoService contract, stamping the configured
// AlmacenDestinoVentasID onto every CrearTraspasoParaVenta call. The
// destino lives in config (not in the ventas request) so the magic ID never
// leaks across module boundaries.
type ventasInventarioAdapter struct {
	inv            inventario.TraspasoService
	almacenDestino int
}

func (a *ventasInventarioAdapter) ValidarStockParaVenta(ctx context.Context, items []ventasoutbound.InventarioStockItem) error {
	dst := make([]inventario.ValidarStockItem, len(items))
	for i, it := range items {
		dst[i] = inventario.ValidarStockItem{
			ArticuloID:    it.ArticuloID,
			AlmacenOrigen: it.AlmacenOrigen,
			Cantidad:      it.Cantidad,
		}
	}
	return a.inv.ValidarStockParaVenta(ctx, dst)
}

func (a *ventasInventarioAdapter) CrearTraspasoParaVenta(ctx context.Context, p ventasoutbound.InventarioCrearTraspasoParams) (int, error) {
	detalles := make([]inventario.CrearTraspasoDetalleInput, len(p.Detalles))
	for i, d := range p.Detalles {
		detalles[i] = inventario.CrearTraspasoDetalleInput{
			ArticuloID: d.ArticuloID,
			Cantidad:   d.Cantidad,
		}
	}
	_, doctoInID, err := a.inv.CrearTraspasoParaVenta(ctx, inventario.CrearTraspasoParaVentaParams{
		VentaID:        p.VentaID,
		AlmacenOrigen:  p.AlmacenOrigen,
		AlmacenDestino: a.almacenDestino,
		Fecha:          p.Fecha,
		Descripcion:    p.Descripcion,
		Detalles:       detalles,
		CreatedBy:      p.CreatedBy,
	})
	return doctoInID, err
}

func (a *ventasInventarioAdapter) CrearTraspasoReverso(ctx context.Context, ventaID, by uuid.UUID) (int, error) {
	_, doctoInID, err := a.inv.CrearTraspasoReverso(ctx, ventaID, by)
	return doctoInID, err
}

func (a *ventasInventarioAdapter) ResincronizarTraspasoParaVenta(ctx context.Context, p ventasoutbound.InventarioCrearTraspasoParams) (int, error) {
	detalles := make([]inventario.CrearTraspasoDetalleInput, len(p.Detalles))
	for i, d := range p.Detalles {
		detalles[i] = inventario.CrearTraspasoDetalleInput{
			ArticuloID: d.ArticuloID,
			Cantidad:   d.Cantidad,
		}
	}
	_, doctoInID, err := a.inv.ResincronizarTraspasoParaVenta(ctx, inventario.CrearTraspasoParaVentaParams{
		VentaID:        p.VentaID,
		AlmacenOrigen:  p.AlmacenOrigen,
		AlmacenDestino: a.almacenDestino,
		Fecha:          p.Fecha,
		Descripcion:    p.Descripcion,
		Detalles:       detalles,
		CreatedBy:      p.CreatedBy,
	})
	return doctoInID, err
}

// provideVentasInventarioAdapter builds the ventas-side adapter that fans
// out to the inventario module while injecting the configured destino.
func provideVentasInventarioAdapter(inv inventario.TraspasoService, cfg *config.Config) ventasoutbound.InventarioService {
	return &ventasInventarioAdapter{
		inv:            inv,
		almacenDestino: cfg.Inventario.AlmacenDestinoVentasID,
	}
}
