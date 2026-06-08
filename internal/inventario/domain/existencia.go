//nolint:misspell // domain vocabulary is Spanish (existencia, artículo, etc.) per project convention.
package domain

import "github.com/shopspring/decimal"

// Existencia is a read-model value object representing current stock for one
// article in one warehouse. It is a data carrier used for projections only.
//
// Cantidad is stored as decimal.Decimal (not the Cantidad VO) because stock
// can be zero or negative when items are oversold — the Cantidad VO's > 0
// invariant would be incorrect for stock readings.
type Existencia struct {
	ArticuloID int
	AlmacenID  int
	Cantidad   decimal.Decimal
}
