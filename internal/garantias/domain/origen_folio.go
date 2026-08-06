package domain

// OrigenFolio represents the origin of a warranty folio.
type OrigenFolio string

// OrigenFolio values represent the origin of a warranty folio.
const (
	OrigenFolioPiso    OrigenFolio = "piso"
	OrigenFolioCliente OrigenFolio = "cliente"
)

// ParseOrigenFolio validates and returns an OrigenFolio.
// Returns ErrOrigenFolioInvalido if s is not "piso" or "cliente".
func ParseOrigenFolio(s string) (OrigenFolio, error) {
	o := OrigenFolio(s)
	if !o.IsValid() {
		return "", ErrOrigenFolioInvalido
	}
	return o, nil
}

// IsValid reports whether o is a known OrigenFolio value.
func (o OrigenFolio) IsValid() bool {
	switch o {
	case OrigenFolioPiso, OrigenFolioCliente:
		return true
	}
	return false
}

// String returns the string representation of o.
func (o OrigenFolio) String() string { return string(o) }

// EsPiso reports whether the origin is floor stock.
func (o OrigenFolio) EsPiso() bool { return o == OrigenFolioPiso }

// EsCliente reports whether the origin is a client purchase.
func (o OrigenFolio) EsCliente() bool { return o == OrigenFolioCliente }
