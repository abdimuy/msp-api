//nolint:misspell // inventario adapter — Spanish vocabulary (Descripcion, Reverso) per project convention.
package inventario

// Adapter that wraps the app-layer Service so it satisfies the cross-module
// TraspasoService interface. Lives in the contract package so depguard can
// enforce the rule that consumers (ventas, cmd/api) import only
// "inventario", never "inventario/app" or "inventario/domain".

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/inventario/app"
)

// ServiceAdapter projects the app.Service onto the TraspasoService contract.
// It is the single bridge between the inventario internals (domain, app) and
// the cross-module surface.
type ServiceAdapter struct {
	inner *app.Service
}

// NewServiceAdapter wraps a built app.Service so it can be handed to
// consumers as a TraspasoService. The adapter is intentionally thin —
// nothing here implements business logic; it only re-shapes inputs and
// outputs across the contract boundary.
func NewServiceAdapter(inner *app.Service) *ServiceAdapter {
	return &ServiceAdapter{inner: inner}
}

// Compile-time check.
var _ TraspasoService = (*ServiceAdapter)(nil)

// ValidarStockParaVenta delegates to the inner service after translating
// the contract-shaped items into the app's input type.
func (a *ServiceAdapter) ValidarStockParaVenta(ctx context.Context, items []ValidarStockItem) error {
	innerItems := make([]app.ValidarStockItem, len(items))
	for i, it := range items {
		innerItems[i] = app.ValidarStockItem{
			ArticuloID:    it.ArticuloID,
			AlmacenOrigen: it.AlmacenOrigen,
			Cantidad:      it.Cantidad,
		}
	}
	return a.inner.ValidarStockParaVenta(ctx, innerItems)
}

// CrearTraspasoParaVenta delegates to the inner service and projects the
// returned domain.Traspaso onto the contract DTO.
func (a *ServiceAdapter) CrearTraspasoParaVenta(ctx context.Context, p CrearTraspasoParaVentaParams) (Traspaso, int, error) {
	innerDetalles := make([]app.CrearTraspasoDetalleInput, len(p.Detalles))
	for i, d := range p.Detalles {
		innerDetalles[i] = app.CrearTraspasoDetalleInput{
			ArticuloID: d.ArticuloID,
			Cantidad:   d.Cantidad,
		}
	}
	tr, doctoInID, err := a.inner.CrearTraspasoParaVenta(ctx, app.CrearTraspasoParaVentaParams{
		VentaID:        p.VentaID,
		AlmacenOrigen:  p.AlmacenOrigen,
		AlmacenDestino: p.AlmacenDestino,
		Fecha:          p.Fecha,
		Descripcion:    p.Descripcion,
		Detalles:       innerDetalles,
		CreatedBy:      p.CreatedBy,
	})
	if err != nil {
		return Traspaso{}, 0, err
	}
	return TraspasoFromDomain(tr), doctoInID, nil
}

// CrearTraspasoReverso delegates to the inner service and projects the
// reversed domain.Traspaso onto the contract DTO.
func (a *ServiceAdapter) CrearTraspasoReverso(ctx context.Context, ventaID, by uuid.UUID) (Traspaso, int, error) {
	tr, doctoInID, err := a.inner.CrearTraspasoReverso(ctx, ventaID, by)
	if err != nil {
		return Traspaso{}, 0, err
	}
	return TraspasoFromDomain(tr), doctoInID, nil
}
