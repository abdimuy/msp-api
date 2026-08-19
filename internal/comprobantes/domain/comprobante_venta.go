//nolint:misspell // domain vocabulary is Spanish (artículo, comprobante, etc.) per project convention.
package domain

import (
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// ComprobanteVenta is the content model (Categoría 3 composite VO) that a
// venta comprobante carries. It is the data sent to the template/imprimible:
// the PDF renderer fills from it and nothing else. It has no identity, no
// lifecycle and no formatting logic — presentation lives in the renderer.
//
// Money is always decimal.Decimal, never float64. Fechas are UTC.
type ComprobanteVenta struct {
	folio            string
	fecha            time.Time
	clienteNombre    string
	clienteDomicilio string
	articulos        []ArticuloComprobante
	total            decimal.Decimal
	enganche         decimal.Decimal
	saldo            decimal.Decimal
	planPago         string
	vendedor         string
}

// NewComprobanteVentaParams aggregates every field needed to build a fresh
// ComprobanteVenta. The zero value is never valid.
type NewComprobanteVentaParams struct {
	Folio            string
	Fecha            time.Time
	ClienteNombre    string
	ClienteDomicilio string
	Articulos        []ArticuloComprobante
	Total            decimal.Decimal
	Enganche         decimal.Decimal
	Saldo            decimal.Decimal
	PlanPago         string
	Vendedor         string
}

// NewComprobanteVenta validates each field and returns a ComprobanteVenta or
// an apperror sentinel. Articles are re-validated so a New-built comprobante
// can never carry an invalid detail line even if it was hydrated elsewhere.
func NewComprobanteVenta(p NewComprobanteVentaParams) (ComprobanteVenta, error) {
	if strings.TrimSpace(p.Folio) == "" {
		return ComprobanteVenta{}, ErrComprobanteVentaFolioRequerido
	}
	if strings.TrimSpace(p.ClienteNombre) == "" {
		return ComprobanteVenta{}, ErrComprobanteVentaClienteRequerido
	}
	if p.Total.IsNegative() {
		return ComprobanteVenta{}, ErrComprobanteVentaTotalNegativo
	}
	if p.Enganche.IsNegative() {
		return ComprobanteVenta{}, ErrComprobanteVentaEngancheNegativo
	}
	if p.Saldo.IsNegative() {
		return ComprobanteVenta{}, ErrComprobanteVentaSaldoNegativo
	}
	if len(p.Articulos) == 0 {
		return ComprobanteVenta{}, ErrComprobanteVentaSinArticulos
	}
	articulos := make([]ArticuloComprobante, len(p.Articulos))
	for i, a := range p.Articulos {
		if err := a.validar(); err != nil {
			return ComprobanteVenta{}, err
		}
		articulos[i] = a
	}
	return ComprobanteVenta{
		folio:            p.Folio,
		fecha:            p.Fecha,
		clienteNombre:    p.ClienteNombre,
		clienteDomicilio: p.ClienteDomicilio,
		articulos:        articulos,
		total:            p.Total,
		enganche:         p.Enganche,
		saldo:            p.Saldo,
		planPago:         p.PlanPago,
		vendedor:         p.Vendedor,
	}, nil
}

// HydrateComprobanteVentaParams carries the persisted shape of a
// ComprobanteVenta for reconstruction.
type HydrateComprobanteVentaParams struct {
	Folio            string
	Fecha            time.Time
	ClienteNombre    string
	ClienteDomicilio string
	Articulos        []ArticuloComprobante
	Total            decimal.Decimal
	Enganche         decimal.Decimal
	Saldo            decimal.Decimal
	PlanPago         string
	Vendedor         string
}

// HydrateComprobanteVenta rebuilds a ComprobanteVenta from persistence without
// validation. Intended for repository use only.
func HydrateComprobanteVenta(p HydrateComprobanteVentaParams) ComprobanteVenta {
	return ComprobanteVenta{
		folio:            p.Folio,
		fecha:            p.Fecha,
		clienteNombre:    p.ClienteNombre,
		clienteDomicilio: p.ClienteDomicilio,
		articulos:        p.Articulos,
		total:            p.Total,
		enganche:         p.Enganche,
		saldo:            p.Saldo,
		planPago:         p.PlanPago,
		vendedor:         p.Vendedor,
	}
}

// Folio returns the sale folio.
func (c ComprobanteVenta) Folio() string { return c.folio }

// Fecha returns the sale timestamp (UTC).
func (c ComprobanteVenta) Fecha() time.Time { return c.fecha }

// ClienteNombre returns the client name.
func (c ComprobanteVenta) ClienteNombre() string { return c.clienteNombre }

// ClienteDomicilio returns the client address as a single line.
func (c ComprobanteVenta) ClienteDomicilio() string { return c.clienteDomicilio }

// Articulos returns a defensive copy of the detail lines.
func (c ComprobanteVenta) Articulos() []ArticuloComprobante {
	return append([]ArticuloComprobante(nil), c.articulos...)
}

// Total returns the sale total.
func (c ComprobanteVenta) Total() decimal.Decimal { return c.total }

// Enganche returns the down payment.
func (c ComprobanteVenta) Enganche() decimal.Decimal { return c.enganche }

// Saldo returns the outstanding balance.
func (c ComprobanteVenta) Saldo() decimal.Decimal { return c.saldo }

// PlanPago returns the payment plan in words (e.g. "12 meses sin intereses").
func (c ComprobanteVenta) PlanPago() string { return c.planPago }

// Vendedor returns the seller name.
func (c ComprobanteVenta) Vendedor() string { return c.vendedor }

// ArticuloComprobante is one detail line of a venta comprobante: the article
// sold, its quantity, unit price and line importe. It is a Categoría 3
// composite VO and is always validated as part of its parent
// ComprobanteVenta.
type ArticuloComprobante struct {
	descripcion    string
	cantidad       decimal.Decimal
	precioUnitario decimal.Decimal
	importe        decimal.Decimal
}

// NewArticuloComprobanteParams aggregates every field needed to build a fresh
// ArticuloComprobante. The zero value is never valid.
type NewArticuloComprobanteParams struct {
	Descripcion    string
	Cantidad       decimal.Decimal
	PrecioUnitario decimal.Decimal
	Importe        decimal.Decimal
}

// NewArticuloComprobante validates each field and returns an
// ArticuloComprobante or an apperror sentinel.
func NewArticuloComprobante(p NewArticuloComprobanteParams) (ArticuloComprobante, error) {
	a := ArticuloComprobante{
		descripcion:    p.Descripcion,
		cantidad:       p.Cantidad,
		precioUnitario: p.PrecioUnitario,
		importe:        p.Importe,
	}
	if err := a.validar(); err != nil {
		return ArticuloComprobante{}, err
	}
	return a, nil
}

// HydrateArticuloComprobanteParams carries the persisted shape of an
// ArticuloComprobante for reconstruction.
type HydrateArticuloComprobanteParams struct {
	Descripcion    string
	Cantidad       decimal.Decimal
	PrecioUnitario decimal.Decimal
	Importe        decimal.Decimal
}

// HydrateArticuloComprobante rebuilds an ArticuloComprobante from persistence
// without validation. Intended for repository use only.
func HydrateArticuloComprobante(p HydrateArticuloComprobanteParams) ArticuloComprobante {
	return ArticuloComprobante{
		descripcion:    p.Descripcion,
		cantidad:       p.Cantidad,
		precioUnitario: p.PrecioUnitario,
		importe:        p.Importe,
	}
}

// Descripcion returns the article description.
func (a ArticuloComprobante) Descripcion() string { return a.descripcion }

// Cantidad returns the quantity sold.
func (a ArticuloComprobante) Cantidad() decimal.Decimal { return a.cantidad }

// PrecioUnitario returns the unit price.
func (a ArticuloComprobante) PrecioUnitario() decimal.Decimal { return a.precioUnitario }

// Importe returns the line total.
func (a ArticuloComprobante) Importe() decimal.Decimal { return a.importe }

// validar enforces the article invariants: description required and
// cantidad/precioUnitario/importe non-negative.
func (a ArticuloComprobante) validar() error {
	if strings.TrimSpace(a.descripcion) == "" {
		return ErrArticuloDescripcionRequerida
	}
	if a.cantidad.IsNegative() {
		return ErrArticuloCantidadNegativa
	}
	if a.precioUnitario.IsNegative() {
		return ErrArticuloPrecioUnitarioNegativo
	}
	if a.importe.IsNegative() {
		return ErrArticuloImporteNegativo
	}
	return nil
}
