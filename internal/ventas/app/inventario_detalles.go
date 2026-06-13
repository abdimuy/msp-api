//nolint:misspell // domain vocabulary is Spanish (productos, almacenes, combos) per project convention.
package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// buildTraspasoDetallesFromVenta derives the InventarioTraspasoDetalle slice
// and the single shared AlmacenOrigen from the venta's current productos + combos.
//
// Combo-child productos (ComboID != nil) inherit their origin from the parent
// combo. Stand-alone productos (ComboID == nil) carry their own AlmacenOrigen.
// All items must share the same origin; a mismatch returns
// "productos_multiples_almacenes_origen".
//
// Returns (nil, 0, nil) when the venta has no productos — callers must not
// attempt to create a traspaso in that case.
func buildTraspasoDetallesFromVenta(v *domain.Venta) ([]outbound.InventarioTraspasoDetalle, int, error) {
	// Build a lookup map: combo ID → AlmacenOrigen.
	combosByID := make(map[uuid.UUID]int)
	for c := range v.Combos() {
		combosByID[c.ID()] = c.AlmacenOrigen()
	}

	almacenOrigen := 0
	var detalles []outbound.InventarioTraspasoDetalle

	for p := range v.Productos() {
		var origen int
		if p.ComboID() == nil {
			// Stand-alone producto: AlmacenOrigen is never nil for non-combo productos.
			origen = *p.AlmacenOrigen()
		} else {
			comboOrigen, ok := combosByID[*p.ComboID()]
			if !ok {
				return nil, 0, fmt.Errorf("producto %v referencia combo %v que no existe en la venta", p.ID(), *p.ComboID())
			}
			origen = comboOrigen
		}

		if almacenOrigen == 0 {
			almacenOrigen = origen
		} else if almacenOrigen != origen {
			return nil, 0, apperror.NewValidation(
				"productos_multiples_almacenes_origen",
				"los productos de la venta tienen distintos almacenes de origen; no se puede generar un traspaso único",
			)
		}

		detalles = append(detalles, outbound.InventarioTraspasoDetalle{
			ArticuloID: p.ArticuloID(),
			Cantidad:   p.Cantidad(),
		})
	}

	if len(detalles) == 0 {
		return nil, 0, nil
	}
	return detalles, almacenOrigen, nil
}

// validateStockParaDetalles maps each InventarioTraspasoDetalle to an
// InventarioStockItem (stamping the shared almacenOrigen) and delegates to the
// configured InventarioService. Returns nil when s.inventario is nil or the
// detalles slice is empty.
func (s *Service) validateStockParaDetalles(ctx context.Context, detalles []outbound.InventarioTraspasoDetalle, almacenOrigen int) error {
	if s.inventario == nil || len(detalles) == 0 {
		return nil
	}
	items := make([]outbound.InventarioStockItem, 0, len(detalles))
	for _, d := range detalles {
		items = append(items, outbound.InventarioStockItem{
			ArticuloID:    d.ArticuloID,
			AlmacenOrigen: almacenOrigen,
			Cantidad:      d.Cantidad,
		})
	}
	return s.inventario.ValidarStockParaVenta(ctx, items)
}
