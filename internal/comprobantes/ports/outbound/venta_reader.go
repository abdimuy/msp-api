//nolint:misspell // Descripcion is domain vocabulary in Spanish per project convention.
package outbound

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// DatosVenta is what a receipt needs from a sale. It is NOT domain.ComprobanteVenta:
// that model also carries the client's name and address, which come from
// ClienteReader. The application layer assembles both halves.
type DatosVenta struct {
	Folio        string
	Fecha        time.Time
	Articulos    []ArticuloVenta
	Total        decimal.Decimal
	Enganche     decimal.Decimal
	Saldo        decimal.Decimal
	Parcialidad  decimal.Decimal
	Periodicidad string // "semanal" | "quincenal" | "mensual"
	NumeroPagos  int
	Vendedor     string
}

// ArticuloVenta is one detail line of the sale.
type ArticuloVenta struct {
	Descripcion    string
	Cantidad       decimal.Decimal
	PrecioUnitario decimal.Decimal
	Importe        decimal.Decimal
}

// VentaReader reads the sale behind a receipt. The implementation lives in
// infra/clients and goes through the ventas module's contracts package —
// never through its domain or its tables.
type VentaReader interface {
	Leer(ctx context.Context, ventaID int) (DatosVenta, error)
}
