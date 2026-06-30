//nolint:misspell // Spanish domain vocabulary (reporte, venta, pago, etc.) by project convention.
package outbound

import (
	"time"

	"github.com/shopspring/decimal"
)

// ReporteCliente is the read-model for the client PDF report. It bundles
// identity data, the aggregated financial summary, and per-sale payment details.
type ReporteCliente struct {
	Cliente ReporteClienteDatos
	Resumen ResumenFicha
	Ventas  []ReporteVenta
	// TotalVentas is the total number of sales on record for the client.
	// Ventas may be a printed subset (when the caller filters by venta IDs).
	TotalVentas int
	// VentasLiquidadas is the count of sales with a zero outstanding balance
	// (fully paid), computed over all client ventas (not just the printed subset).
	VentasLiquidadas int
	// VentasActivas is the count of sales with a positive outstanding balance,
	// computed over all client ventas (not just the printed subset).
	VentasActivas int
}

// ReporteClienteDatos holds the identity fields shown in the client block of
// the report. Direccion is a pre-formatted single-line address.
type ReporteClienteDatos struct {
	ID        int
	Nombre    string
	Direccion string
	Telefono  string
	Zona      string
	Cobrador  string
	// Notas is the client's free-form note (Microsip NOTAS); may be long. Empty
	// when there is no note.
	Notas string
}

// ReporteVenta is a single sale record in the report, with its line items,
// credit terms (when on credit), and payment history.
type ReporteVenta struct {
	DoctoPvID int
	Folio     string
	Fecha     time.Time
	Almacen   string
	Total     decimal.Decimal
	Saldo     decimal.Decimal
	Liquidada bool
	Productos []ReporteProducto
	// Credito holds the credit terms; nil for contado sales.
	Credito *ReporteCredito
	Pagos   []ReportePago
}

// ReporteProducto is one line item of a sale.
type ReporteProducto struct {
	Nombre         string
	Cantidad       decimal.Decimal
	PrecioUnitario decimal.Decimal
	Importe        decimal.Decimal
	PctDescuento   decimal.Decimal
}

// ReporteCredito holds the credit-contract terms of a sale.
type ReporteCredito struct {
	Parcialidad     decimal.Decimal
	FormaPago       string
	PlazoMeses      int
	Enganche        decimal.Decimal
	PrecioContado   decimal.Decimal
	MontoCortoPlazo decimal.Decimal
	Vendedores      []string
}

// ReportePago is a single payment entry within a sale's payment history.
type ReportePago struct {
	Fecha    time.Time
	Concepto string
	Cobrador string
	Importe  decimal.Decimal
	// EsIngreso is false for condonación and pérdida movements (forgiven debt /
	// write-offs), shown apart from real collected money in the report.
	EsIngreso bool
	// Categoria is the economic role: "pago"|"enganche"|"condonacion"|"perdida"|"otro".
	Categoria string
}
