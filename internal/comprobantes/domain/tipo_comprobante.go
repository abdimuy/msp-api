package domain

// TipoVenta identifies a receipt for a sale registered in Microsip.
const TipoVenta = "venta"

// TipoPago identifies a receipt for a payment applied in Microsip.
const TipoPago = "pago"

// TipoComprobante is a value object wrapping which event produced the
// receipt. Only "venta" and "pago" are valid.
type TipoComprobante struct{ value string }

// NewTipoComprobante validates and constructs a TipoComprobante. Rejects
// anything else with ErrTipoComprobanteInvalido.
func NewTipoComprobante(s string) (TipoComprobante, error) {
	if s != TipoVenta && s != TipoPago {
		return TipoComprobante{}, ErrTipoComprobanteInvalido
	}
	return TipoComprobante{value: s}, nil
}

// HydrateTipoComprobante rebuilds one from persistence without validation.
// Intended for repository use only.
func HydrateTipoComprobante(s string) TipoComprobante { return TipoComprobante{value: s} }

// Value returns the raw receipt type string ("venta" or "pago").
func (t TipoComprobante) Value() string { return t.value }

// String returns the receipt type string representation.
func (t TipoComprobante) String() string { return t.value }

// Equals reports whether two TipoComprobante values are identical.
func (t TipoComprobante) Equals(other TipoComprobante) bool { return t.value == other.value }

// IsZero reports whether the TipoComprobante has its zero value (empty string).
func (t TipoComprobante) IsZero() bool { return t.value == "" }

// EsVenta reports whether this is a receipt for a sale.
func (t TipoComprobante) EsVenta() bool { return t.value == TipoVenta }

// EsPago reports whether this is a receipt for a payment.
func (t TipoComprobante) EsPago() bool { return t.value == TipoPago }
