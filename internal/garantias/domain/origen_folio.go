//nolint:dupl // all value objects follow the same pattern; duplication is intentional and unavoidable.
package domain

// OrigenFolioPiso represents a warranty folio opened for a floor-stock item
// (no associated cliente or venta).
const OrigenFolioPiso = "piso"

// OrigenFolioCliente represents a warranty folio opened against a client's
// existing purchase.
const OrigenFolioCliente = "cliente"

// OrigenFolio is a value object wrapping the origin of a warranty folio.
// Only "piso" and "cliente" are valid.
type OrigenFolio struct{ value string }

// NewOrigenFolio validates and constructs an OrigenFolio. Accepts only
// "piso" or "cliente"; rejects anything else with ErrOrigenFolioInvalido.
func NewOrigenFolio(s string) (OrigenFolio, error) {
	if s != OrigenFolioPiso && s != OrigenFolioCliente {
		return OrigenFolio{}, ErrOrigenFolioInvalido
	}
	return OrigenFolio{value: s}, nil
}

// HydrateOrigenFolio rebuilds an OrigenFolio from persistence without
// validation. Intended for repository use only.
func HydrateOrigenFolio(s string) OrigenFolio { return OrigenFolio{value: s} }

// Value returns the raw origin string ("piso" or "cliente").
func (o OrigenFolio) Value() string { return o.value }

// String returns the origin string representation.
func (o OrigenFolio) String() string { return o.value }

// Equals reports whether two OrigenFolio values are identical.
func (o OrigenFolio) Equals(other OrigenFolio) bool { return o.value == other.value }

// IsZero reports whether the OrigenFolio has its zero value (empty string).
func (o OrigenFolio) IsZero() bool { return o.value == "" }

// EsPiso reports whether this folio originates from floor stock.
func (o OrigenFolio) EsPiso() bool { return o.value == OrigenFolioPiso }

// EsCliente reports whether this folio originates from a client purchase.
func (o OrigenFolio) EsCliente() bool { return o.value == OrigenFolioCliente }
