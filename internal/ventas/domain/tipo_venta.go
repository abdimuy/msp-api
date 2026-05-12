package domain

// TipoVenta enumerates the kind of sale: cash (CONTADO) or credit (CREDITO).
type TipoVenta string

// TipoVenta enum values. The string forms match the persisted column values
// in MSP_VENTAS.TIPO_VENTA.
const (
	// TipoVentaContado denotes a cash sale (no credit plan).
	TipoVentaContado TipoVenta = "CONTADO"
	// TipoVentaCredito denotes a credit sale (requires plan + cobranza day).
	TipoVentaCredito TipoVenta = "CREDITO"
)

// ParseTipoVenta parses a string into a TipoVenta or returns
// ErrTipoVentaInvalido.
func ParseTipoVenta(s string) (TipoVenta, error) {
	t := TipoVenta(s)
	if !t.IsValid() {
		return "", ErrTipoVentaInvalido
	}
	return t, nil
}

// IsValid reports whether t is a recognized TipoVenta.
func (t TipoVenta) IsValid() bool {
	switch t {
	case TipoVentaContado, TipoVentaCredito:
		return true
	}
	return false
}

// String returns the canonical string representation.
func (t TipoVenta) String() string { return string(t) }
