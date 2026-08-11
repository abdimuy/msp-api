//nolint:misspell // domain vocabulary is Spanish (venta, producto, articulo) per project convention.
package domain

import "github.com/shopspring/decimal"

// ProductoVenta is a read-only projection of a DOCTOS_PV_DET line item for a
// sale (DOCTOS_PV). The mobile cobranza app renders these both in the sale
// detail (SaleProductsSection) and, concatenated, in the sale list. It carries
// the DOCTOS_PV_DET primary key, its parent DOCTOS_PV_ID, the cargo FOLIO, and
// the article name/quantities/prices.
//
// This mirrors the clientes module's ProductoVenta but keeps a wider
// projection (adds DOCTO_PV_DET_ID / DOCTO_PV_ID / FOLIO / POSICION) so the
// row maps 1:1 to the app's Product/ProductEntity. Per the vertical-slice rule
// (CLAUDE.md §2) cobranza must not import clientes' domain, so the type is
// duplicated here on purpose.
//
// All fields are private; callers use the getter methods.
type ProductoVenta struct {
	doctoPVDetID    int
	doctoPVID       int
	folio           string
	articuloID      int
	articulo        string
	cantidad        int
	precioUnitario  decimal.Decimal
	precioTotalNeto decimal.Decimal
	posicion        int
}

// HydrateProductoVentaParams carries the raw values read from DOCTOS_PV_DET +
// ARTICULOS + DOCTOS_PV to build a ProductoVenta.
type HydrateProductoVentaParams struct {
	DoctoPVDetID    int
	DoctoPVID       int
	Folio           string
	ArticuloID      int
	Articulo        string
	Cantidad        int
	PrecioUnitario  decimal.Decimal
	PrecioTotalNeto decimal.Decimal
	Posicion        int
}

// HydrateProductoVenta rebuilds a ProductoVenta from persisted infra values.
// Used only by the infra layer when scanning rows — never in business logic.
func HydrateProductoVenta(p HydrateProductoVentaParams) ProductoVenta {
	return ProductoVenta{
		doctoPVDetID:    p.DoctoPVDetID,
		doctoPVID:       p.DoctoPVID,
		folio:           p.Folio,
		articuloID:      p.ArticuloID,
		articulo:        p.Articulo,
		cantidad:        p.Cantidad,
		precioUnitario:  p.PrecioUnitario,
		precioTotalNeto: p.PrecioTotalNeto,
		posicion:        p.Posicion,
	}
}

// DoctoPVDetID returns the DOCTOS_PV_DET primary key of the line.
func (p ProductoVenta) DoctoPVDetID() int { return p.doctoPVDetID }

// DoctoPVID returns the parent DOCTOS_PV document ID.
func (p ProductoVenta) DoctoPVID() int { return p.doctoPVID }

// Folio returns the cargo folio the line belongs to.
func (p ProductoVenta) Folio() string { return p.folio }

// ArticuloID returns the ARTICULOS primary key.
func (p ProductoVenta) ArticuloID() int { return p.articuloID }

// Articulo returns the article display name.
func (p ProductoVenta) Articulo() string { return p.articulo }

// Cantidad returns the (truncated) unit count for the line.
func (p ProductoVenta) Cantidad() int { return p.cantidad }

// PrecioUnitario returns the unit price with tax (PRECIO_UNITARIO_IMPTO).
func (p ProductoVenta) PrecioUnitario() decimal.Decimal { return p.precioUnitario }

// PrecioTotalNeto returns the line total (unit * qty * (1 - dscto)).
func (p ProductoVenta) PrecioTotalNeto() decimal.Decimal { return p.precioTotalNeto }

// Posicion returns the 1-based ordering position within the document.
func (p ProductoVenta) Posicion() int { return p.posicion }
