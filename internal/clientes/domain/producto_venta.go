//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import (
	"github.com/shopspring/decimal"
)

// ProductoVenta is a read-only projection of a single sale line from the
// native Microsip tables DOCTOS_PV_DET and ARTICULOS. The module never
// mutates sale lines; Microsip owns them.
//
// Deliberately deviates from the Type A/B/C entity standards:
//   - No audit embed (read-only Microsip projection).
//   - No Crear constructor and no domain events.
//   - HydrateProductoVenta is the sole constructor; used exclusively by the repository.
type ProductoVenta struct {
	articuloID      int
	nombre          string
	unidades        decimal.Decimal // NUMERIC(18,5) — fractional quantities are common
	precioUnitario  decimal.Decimal
	precioTotalNeto decimal.Decimal
	pctjeDscto      decimal.Decimal // percentage discount applied to this line
}

// HydrateProductoVentaParams holds all fields needed to reconstruct a
// ProductoVenta from persisted Microsip rows. Used exclusively by the repository.
type HydrateProductoVentaParams struct {
	ArticuloID      int
	Nombre          string
	Unidades        decimal.Decimal
	PrecioUnitario  decimal.Decimal
	PrecioTotalNeto decimal.Decimal
	PctjeDscto      decimal.Decimal
}

// HydrateProductoVenta reconstructs a ProductoVenta from Microsip persistence
// with zero validation. Called only from the repository layer.
func HydrateProductoVenta(p HydrateProductoVentaParams) *ProductoVenta {
	return &ProductoVenta{
		articuloID:      p.ArticuloID,
		nombre:          p.Nombre,
		unidades:        p.Unidades,
		precioUnitario:  p.PrecioUnitario,
		precioTotalNeto: p.PrecioTotalNeto,
		pctjeDscto:      p.PctjeDscto,
	}
}

// ─── Getters ──────────────────────────────────────────────────────────────────

// ArticuloID returns the Microsip article (product) primary key.
func (p *ProductoVenta) ArticuloID() int { return p.articuloID }

// Nombre returns the article display name from ARTICULOS.NOMBRE.
func (p *ProductoVenta) Nombre() string { return p.nombre }

// Unidades returns the quantity sold. Stored as NUMERIC(18,5) in Microsip to
// support fractional units (e.g. meters of fabric, fractional parts).
func (p *ProductoVenta) Unidades() decimal.Decimal { return p.unidades }

// PrecioUnitario returns the unit price for this line item.
func (p *ProductoVenta) PrecioUnitario() decimal.Decimal { return p.precioUnitario }

// PrecioTotalNeto returns the net total for this line (after discount).
func (p *ProductoVenta) PrecioTotalNeto() decimal.Decimal { return p.precioTotalNeto }

// PctjeDscto returns the discount percentage applied to this line item.
func (p *ProductoVenta) PctjeDscto() decimal.Decimal { return p.pctjeDscto }
