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
}

// ReporteVenta is a single sale record in the report, with its payment history.
type ReporteVenta struct {
	DoctoPvID int
	Folio     string
	Fecha     time.Time
	Almacen   string
	Total     decimal.Decimal
	Saldo     decimal.Decimal
	Liquidada bool
	Pagos     []ReportePago
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
