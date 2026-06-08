//nolint:misspell // inventario vocabulary is Spanish (traspaso, almacén, artículo, etc.) per project convention.
package invhttp

import (
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

// cantidadScale is the number of decimal places for Cantidad fields. Mirrors
// Microsip's NUMERIC(10,4) for article quantities.
const cantidadScale int32 = 4

// toTraspasoResponse projects a domain.Traspaso into its HTTP DTO.
func toTraspasoResponse(t *domain.Traspaso) TraspasoResponse {
	a := t.Audit()
	resp := TraspasoResponse{
		ID:             t.ID().String(),
		Folio:          t.Folio().Value(),
		DoctoInID:      t.DoctoInID(),
		AlmacenOrigen:  t.AlmacenOrigen(),
		AlmacenDestino: t.AlmacenDestino(),
		Fecha:          formatTime(t.Fecha()),
		Descripcion:    t.Descripcion(),
		TipoReverso:    t.TipoReverso(),
		Detalles:       toDetallesResponse(t),
		CreatedAt:      formatTime(a.CreatedAt()),
		CreatedBy:      a.CreatedBy().String(),
	}
	if vid := t.VentaID(); vid != nil {
		s := vid.String()
		resp.VentaID = &s
	}
	return resp
}

// toDetallesResponse projects every detalle child of t.
func toDetallesResponse(t *domain.Traspaso) []TraspasoDetalleResponse {
	out := make([]TraspasoDetalleResponse, 0)
	for d := range t.Detalles() {
		out = append(out, TraspasoDetalleResponse{
			ID:         d.ID().String(),
			ArticuloID: d.ArticuloID(),
			Cantidad:   d.Cantidad().Value().StringFixed(cantidadScale),
		})
	}
	return out
}

// toAlmacenResponse projects a domain.Almacen into its HTTP DTO.
func toAlmacenResponse(a domain.Almacen) AlmacenResponse {
	return AlmacenResponse{ID: a.ID, Nombre: a.Nombre}
}

// toStockResponse builds a StockResponse from raw lookup results.
func toStockResponse(articuloID, almacenID int, cantidad decimal.Decimal) StockResponse {
	return StockResponse{
		ArticuloID: articuloID,
		AlmacenID:  almacenID,
		Cantidad:   cantidad.StringFixed(cantidadScale),
	}
}

// formatTime renders a timestamp as RFC3339Nano in UTC. Zero values map to
// the empty string so optional fields stay omitted by the JSON encoder.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
