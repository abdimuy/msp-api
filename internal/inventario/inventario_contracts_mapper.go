//nolint:misspell // Spanish vocabulary (Descripcion, Traspaso) per project convention.
package inventario

import (
	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

// TraspasoFromDomain projects a domain.Traspaso into its cross-module DTO.
// Called by the inventario app layer when returning aggregates to consumers
// (the ventas module + HTTP handlers).
func TraspasoFromDomain(t *domain.Traspaso) Traspaso {
	if t == nil {
		return Traspaso{}
	}
	detalles := make([]TraspasoDetalle, 0, len(t.DetallesForRepo()))
	for _, d := range t.DetallesForRepo() {
		detalles = append(detalles, TraspasoDetalle{
			ID:         d.ID(),
			ArticuloID: d.ArticuloID(),
			Cantidad:   d.Cantidad().Value(),
		})
	}
	audit := t.Audit()
	return Traspaso{
		ID:             t.ID(),
		Folio:          t.Folio().Value(),
		AlmacenOrigen:  t.AlmacenOrigen(),
		AlmacenDestino: t.AlmacenDestino(),
		Fecha:          t.Fecha(),
		Descripcion:    t.Descripcion(),
		VentaID:        t.VentaID(),
		TipoReverso:    t.TipoReverso(),
		DoctoInID:      t.DoctoInID(),
		Detalles:       detalles,
		CreatedAt:      audit.CreatedAt(),
		CreatedBy:      audit.CreatedBy(),
	}
}

// AlmacenFromDomain projects a domain.Almacen into its cross-module DTO.
func AlmacenFromDomain(a domain.Almacen) Almacen {
	return Almacen{
		ID:     a.ID,
		Nombre: a.Nombre,
	}
}

// ExistenciaFromDomain projects a domain.Existencia into its cross-module DTO.
func ExistenciaFromDomain(e domain.Existencia) Existencia {
	return Existencia{
		ArticuloID: e.ArticuloID,
		AlmacenID:  e.AlmacenID,
		Cantidad:   e.Cantidad,
	}
}
