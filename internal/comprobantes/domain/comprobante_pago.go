//nolint:misspell // domain vocabulary is Spanish (cobrador, pago, etc.) per project convention.
package domain

import (
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// ComprobantePago is the content model (Categoría 3 composite VO) that a pago
// comprobante carries. It is the data sent to the template/imprimible: the
// PDF renderer fills from it and nothing else. It has no identity, no
// lifecycle and no formatting logic — presentation lives in the renderer.
//
// Money is always decimal.Decimal, never float64. Fechas are UTC.
// SaldoRestante is an input datum: the domain never queries the database to
// compute it; whoever raises the payment event passes the sale's remaining
// balance after this pago.
type ComprobantePago struct {
	folio         string
	fecha         time.Time
	clienteNombre string
	monto         decimal.Decimal
	formaCobro    string
	ventaFolio    string
	saldoRestante decimal.Decimal
	cobrador      string
}

// NewComprobantePagoParams aggregates every field needed to build a fresh
// ComprobantePago. The zero value is never valid.
type NewComprobantePagoParams struct {
	Folio         string
	Fecha         time.Time
	ClienteNombre string
	Monto         decimal.Decimal
	FormaCobro    string
	VentaFolio    string
	SaldoRestante decimal.Decimal
	Cobrador      string
}

// NewComprobantePago validates each field and returns a ComprobantePago or an
// apperror sentinel.
func NewComprobantePago(p NewComprobantePagoParams) (ComprobantePago, error) {
	if strings.TrimSpace(p.Folio) == "" {
		return ComprobantePago{}, ErrComprobantePagoFolioRequerido
	}
	if strings.TrimSpace(p.ClienteNombre) == "" {
		return ComprobantePago{}, ErrComprobantePagoClienteRequerido
	}
	if strings.TrimSpace(p.VentaFolio) == "" {
		return ComprobantePago{}, ErrComprobantePagoVentaFolioRequerido
	}
	if p.Monto.IsNegative() {
		return ComprobantePago{}, ErrComprobantePagoMontoNegativo
	}
	if p.SaldoRestante.IsNegative() {
		return ComprobantePago{}, ErrComprobantePagoSaldoRestanteNegativo
	}
	return ComprobantePago{
		folio:         p.Folio,
		fecha:         p.Fecha,
		clienteNombre: p.ClienteNombre,
		monto:         p.Monto,
		formaCobro:    p.FormaCobro,
		ventaFolio:    p.VentaFolio,
		saldoRestante: p.SaldoRestante,
		cobrador:      p.Cobrador,
	}, nil
}

// HydrateComprobantePagoParams carries the persisted shape of a
// ComprobantePago for reconstruction.
type HydrateComprobantePagoParams struct {
	Folio         string
	Fecha         time.Time
	ClienteNombre string
	Monto         decimal.Decimal
	FormaCobro    string
	VentaFolio    string
	SaldoRestante decimal.Decimal
	Cobrador      string
}

// HydrateComprobantePago rebuilds a ComprobantePago from persistence without
// validation. Intended for repository use only.
func HydrateComprobantePago(p HydrateComprobantePagoParams) ComprobantePago {
	return ComprobantePago{
		folio:         p.Folio,
		fecha:         p.Fecha,
		clienteNombre: p.ClienteNombre,
		monto:         p.Monto,
		formaCobro:    p.FormaCobro,
		ventaFolio:    p.VentaFolio,
		saldoRestante: p.SaldoRestante,
		cobrador:      p.Cobrador,
	}
}

// Folio returns the pago folio (e.g. "1-15", "B-98").
func (c ComprobantePago) Folio() string { return c.folio }

// Fecha returns the pago timestamp (UTC).
func (c ComprobantePago) Fecha() time.Time { return c.fecha }

// ClienteNombre returns the client name.
func (c ComprobantePago) ClienteNombre() string { return c.clienteNombre }

// Monto returns the amount paid.
func (c ComprobantePago) Monto() decimal.Decimal { return c.monto }

// FormaCobro returns how the payment was collected.
func (c ComprobantePago) FormaCobro() string { return c.formaCobro }

// VentaFolio returns the folio of the venta the pago applies to.
func (c ComprobantePago) VentaFolio() string { return c.ventaFolio }

// SaldoRestante returns the sale's remaining balance after this pago.
func (c ComprobantePago) SaldoRestante() decimal.Decimal { return c.saldoRestante }

// Cobrador returns the collector name.
func (c ComprobantePago) Cobrador() string { return c.cobrador }
