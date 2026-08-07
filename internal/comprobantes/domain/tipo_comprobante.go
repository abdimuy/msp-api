package domain

// TipoComprobante enumerates which event produced the receipt.
type TipoComprobante string

// TipoComprobante enum values. The string forms match the persisted column
// values in MSP_CM_ENVIO.TIPO.
const (
	// TipoVenta identifies a receipt for a sale registered in Microsip.
	TipoVenta TipoComprobante = "venta"
	// TipoPago identifies a receipt for a payment applied in Microsip.
	TipoPago TipoComprobante = "pago"
)

// ParseTipoComprobante parses a string into a TipoComprobante or returns
// ErrTipoComprobanteInvalido.
func ParseTipoComprobante(s string) (TipoComprobante, error) {
	t := TipoComprobante(s)
	if !t.IsValid() {
		return "", ErrTipoComprobanteInvalido
	}
	return t, nil
}

// IsValid reports whether t is a recognized TipoComprobante.
func (t TipoComprobante) IsValid() bool {
	switch t {
	case TipoVenta, TipoPago:
		return true
	}
	return false
}

// String returns the canonical string representation.
func (t TipoComprobante) String() string { return string(t) }

// EsVenta reports whether this is a receipt for a sale.
func (t TipoComprobante) EsVenta() bool { return t == TipoVenta }

// EsPago reports whether this is a receipt for a payment.
func (t TipoComprobante) EsPago() bool { return t == TipoPago }
