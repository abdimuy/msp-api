//nolint:misspell // Spanish domain vocabulary (clientes, directorio, ficha, etc.) by project convention.
package outbound

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
)

// RangoFechas is an optional inclusive date range for ficha activity aggregations.
// Nil pointers mean "no bound on that end" (all-time).
// SaldoTotal is never range-bounded — it is the live outstanding balance.
type RangoFechas struct {
	Desde *time.Time
	Hasta *time.Time
}

// ListParams is the cursor-pagination input accepted by every List method.
// Cursor is opaque to the caller (server encodes/decodes it); PageSize is the
// desired page size, with the repo applying its own minimum/maximum if necessary.
type ListParams struct {
	Cursor   string
	PageSize int
}

// Page is the generic cursor-paginated result returned by List methods.
// NextCursor is the empty string when there are no more pages.
type Page[T any] struct {
	Items      []T
	NextCursor string
}

// FiltroDirectorio is the structured filter set accepted by ClientesRepo.ListarDirectorioCompleto.
// All pointer fields are optional: nil disables that filter.
// ClienteIDs, when non-empty, restricts to exactly those client IDs regardless
// of other filters.
type FiltroDirectorio struct {
	// ZonaClienteID restricts to clients in a specific sales zone. Nil = no filter.
	ZonaClienteID *int
	// CobradorID restricts to clients assigned to a specific cobrador. Nil = no filter.
	CobradorID *int
	// ConSaldo when true restricts to clients whose outstanding balance > 0.
	ConSaldo bool
	// ClienteIDs, when non-empty, restricts results to exactly these client IDs.
	ClienteIDs []int
}

// DirectorioItem is a single row in the paginated clientes directory. It
// combines the identity projection with the aggregated outstanding balance so
// the UI can render the directory list without a second round-trip.
type DirectorioItem struct {
	// Cliente is the identity projection hydrated from Microsip CLIENTES.
	Cliente *domain.Cliente
	// SaldoTotal is the total outstanding balance across all open sales for
	// this client, sourced from the balance cache.
	SaldoTotal decimal.Decimal
}

// PuntoMensual is a single (year, month, amount) data point used in time-series
// charts on the client ficha.
type PuntoMensual struct {
	Anio  int
	Mes   int
	Monto decimal.Decimal
}

// PuntoCompradoAbonado is a dual-series data point comparing purchased vs.
// paid amounts for a given (year, month). The abonado side is broken down into
// five category buckets (cobranza, enganche, condonacion, perdida, otro) so the
// frontend can stack them with different colors. Used in the ficha summary chart.
type PuntoCompradoAbonado struct {
	Anio        int
	Mes         int
	Comprado    decimal.Decimal
	Cobranza    decimal.Decimal
	Enganche    decimal.Decimal
	Condonacion decimal.Decimal
	Perdida     decimal.Decimal
	Otro        decimal.Decimal
}

// ResumenFicha is the pre-aggregated financial summary shown at the top of
// a client's ficha (detail screen). All monetary fields are exact decimals.
type ResumenFicha struct {
	// TotalComprado is the sum of all sale totals for this client.
	TotalComprado decimal.Decimal
	// TotalAbonado is the sum of all payments received from this client.
	TotalAbonado decimal.Decimal
	// SaldoTotal is the outstanding balance (TotalComprado - TotalAbonado).
	SaldoTotal decimal.Decimal
	// PctLiquidado is the percentage of total purchased amount already paid.
	PctLiquidado decimal.Decimal
	// NumVentas is the total number of sales on record for this client.
	NumVentas int
	// NumPagos is the total number of payments received from this client.
	NumPagos int
	// TicketPromedio is the average sale amount (TotalComprado / NumVentas).
	TicketPromedio decimal.Decimal
	// AbonosPorMes is the monthly payment time series for the trailing chart.
	AbonosPorMes []PuntoMensual
	// CompradoVsAbonado is the dual-series monthly chart data for the ficha.
	CompradoVsAbonado []PuntoCompradoAbonado
}

// ContratoCredito holds the credit-contract details for a single sale, sourced
// from Microsip DOCTOS_CC and LIBRES_CARGOS_CC. Nil when the sale is contado.
type ContratoCredito struct {
	// Parcialidad is the agreed periodic installment amount.
	Parcialidad decimal.Decimal
	// Enganche is the down payment collected at the time of sale.
	Enganche decimal.Decimal
	// PrecioDeContado is the cash price at the time of sale.
	PrecioDeContado decimal.Decimal
	// PlazoMeses is the inferred loan duration in months.
	PlazoMeses int
	// FormaDePago describes the payment frequency (e.g. "mensual", "quincenal").
	FormaDePago string
	// Vendedores lists the display names of the vendedores assigned to this sale.
	Vendedores []string
}

// VentaDetalle is the full detail bundle for a single sale, including its
// line items, credit contract (if any), and payment history.
type VentaDetalle struct {
	// Venta is the sale header projection.
	Venta *domain.VentaCliente
	// Productos is the ordered list of line items for this sale.
	Productos []*domain.ProductoVenta
	// Contrato holds the credit contract details. Nil when the sale is contado.
	Contrato *ContratoCredito
	// Pagos is the chronological list of payments applied to this sale.
	Pagos []*domain.Pago
}

// RitmoPagoData is the raw data bundle fetched from Firebird by the repository
// and passed to domain.BuildRitmoPago to construct the weekly payment-rhythm series.
type RitmoPagoData struct {
	// Pagos is the chronological list of raw payments for the client.
	Pagos []domain.PagoCrudo
	// Ventas is the chronological list of raw sale headers for the client.
	Ventas []domain.VentaCruda
	// SaldoActual is the live outstanding balance sourced from the balance cache.
	SaldoActual decimal.Decimal
}

// PagoDetalle is the rich detail bundle for a single payment document.
type PagoDetalle struct {
	DoctoCCID      int
	Fecha          time.Time
	Folio          string
	Cancelado      bool
	Aplicado       bool
	Importe        decimal.Decimal
	IVA            decimal.Decimal
	ConceptoCCID   int
	Concepto       string
	Categoria      string
	CobradorID     int
	Cobrador       string
	FormaCobroID   int
	FormaCobro     string
	Referencia     string
	AplicaACargoID int
	SaldoCargo     *decimal.Decimal
	DoctoPVID      int
	Lat            *decimal.Decimal
	Lon            *decimal.Decimal
	RecibidoAt     time.Time
	AplicadoAt     time.Time
	Origen         string // "app" | "microsip"
}

// ClientesRepo is the primary read port for the clientes hub. Each method maps
// to a distinct read concern in the Customer 360 experience.
//
//nolint:interfacebloat // read-mostly hub: one method per distinct read concern.
type ClientesRepo interface {
	// ObtenerCliente returns the identity projection for a single client.
	// Returns domain.ErrClienteNotFound when no row exists for clienteID.
	ObtenerCliente(ctx context.Context, clienteID int) (*domain.Cliente, error)

	// ListarDirectorioCompleto returns ALL clients matching the native filters
	// (ESTATUS IN ('A','B','V') plus ZonaClienteID / CobradorID / ConSaldo / ClienteIDs),
	// each with identity + SaldoTotal, with NO pagination, ordered by NOMBRE.
	//
	// It is the unbounded fetch used by the app's global sort / global pulse-filter
	// path: the caller enriches the full set with pulse, filters and sorts in-app,
	// then offset-paginates. Saldo is computed with a single grouped aggregation
	// (not a per-row correlated subquery) so it stays efficient when the native
	// filters bound the set (e.g. a zone). The unfiltered case returns the whole
	// padrón (~44k rows) and is expensive — see the impl note.
	ListarDirectorioCompleto(ctx context.Context, f FiltroDirectorio) ([]DirectorioItem, error)

	// ObtenerResumenFicha returns the pre-aggregated financial summary for
	// a client's detail screen (ficha). Returns a zero-valued ResumenFicha
	// (not an error) when the client has no records — the aggregate query
	// returns zero rows rather than ErrClienteNotFound. Callers that need
	// existence validation must call ObtenerCliente first.
	// rango is an optional inclusive date range for activity aggregations
	// (TotalComprado, TotalAbonado, AbonosPorMes, CompradoVsAbonado);
	// SaldoTotal is always the live outstanding balance, never range-bounded.
	ObtenerResumenFicha(ctx context.Context, clienteID int, rango RangoFechas) (ResumenFicha, error)

	// ListarVentas returns a cursor-paginated list of sale headers for a client,
	// ordered by sale date descending.
	ListarVentas(ctx context.Context, clienteID int, p ListParams) (Page[*domain.VentaCliente], error)

	// ObtenerVentaDetalle returns the full detail bundle for a single sale,
	// including line items, credit contract, and payment history.
	// Returns domain.ErrVentaNotFound when no row exists for doctoPVID.
	ObtenerVentaDetalle(ctx context.Context, doctoPVID int) (VentaDetalle, error)

	// ObtenerRitmoPagoData fetches the raw payment and sale data required to
	// build the weekly payment-rhythm series for a client. The caller passes the
	// result to domain.BuildRitmoPago together with rango and the current time.
	// Returns a zero-valued RitmoPagoData (not an error) when the client has no
	// records — callers that need existence validation must call ObtenerCliente first.
	ObtenerRitmoPagoData(ctx context.Context, clienteID int, rango RangoFechas) (RitmoPagoData, error)

	// ObtenerPagoDetalle returns the rich detail for a single payment document.
	// Returns domain.ErrPagoNotFound when no row exists for doctoCCID.
	ObtenerPagoDetalle(ctx context.Context, doctoCCID int) (PagoDetalle, error)
}
