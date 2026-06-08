//nolint:misspell // domain vocabulary is Spanish (folio, etc.) per project convention.
package domain

import "regexp"

// folioPattern matches the canonical traspaso folio format: two uppercase
// letters "MS" followed by one uppercase letter (the series: T, U, V, …)
// and exactly six digits, e.g. "MST000123", "MSU999999".
var folioPattern = regexp.MustCompile(`^MS[A-Z]\d{6}$`)

// Folio is a value object wrapping a validated traspaso folio string.
// The format is ^MS[A-Z]\d{6}$ — prefix "MS", one series letter, 6 digits.
type Folio struct{ value string }

// NewFolio validates and constructs a Folio. Rejects empty strings and any
// string that does not match the pattern ^MS[A-Z]\d{6}$ with ErrFolioInvalido.
func NewFolio(s string) (Folio, error) {
	if !folioPattern.MatchString(s) {
		return Folio{}, ErrFolioInvalido
	}
	return Folio{value: s}, nil
}

// HydrateFolio rebuilds a Folio from persistence without validation.
// Intended for repository use only.
func HydrateFolio(s string) Folio { return Folio{value: s} }

// Value returns the raw folio string.
func (f Folio) Value() string { return f.value }

// String returns the folio string representation.
func (f Folio) String() string { return f.value }

// Equals reports whether two Folio values are identical.
func (f Folio) Equals(other Folio) bool { return f.value == other.value }

// IsZero reports whether the Folio has its zero value (empty string).
func (f Folio) IsZero() bool { return f.value == "" }
