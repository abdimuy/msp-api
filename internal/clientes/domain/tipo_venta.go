//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

// TipoVenta enumerates the kind of sale: cash (CONTADO) or credit (CREDITO).
type TipoVenta string

// TipoVenta enum values. The string forms match the canonical UPPERCASE form
// used across the codebase (mirrors internal/ventas/domain.TipoVenta).
const (
	// TipoVentaContado denotes a cash (contado) sale.
	TipoVentaContado TipoVenta = "CONTADO"
	// TipoVentaCredito denotes a credit (crédito) sale.
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

// EsCredito reports whether the sale was made on credit.
// Used by app-layer and DTO branching to apply credit-specific logic.
func (t TipoVenta) EsCredito() bool { return t == TipoVentaCredito }
