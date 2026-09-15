package domain

import "fmt"

// Folio is the human-readable warranty folio number in the canonical
// GA-XXXXXX form. The integer comes from a FolioGenerator (ID generation
// lives in Go, CLAUDE.md §1); this value object only formats and validates.
type Folio string

const (
	minFolioNumber = 1
	maxFolioNumber = 999999
)

// NewFolio formats the integer issued by a FolioGenerator into the
// canonical GA-%06d form. It returns ErrFolioInvalido when n is out of the
// representable range.
func NewFolio(n int) (Folio, error) {
	if n < minFolioNumber || n > maxFolioNumber {
		return "", ErrFolioInvalido
	}
	return Folio(fmt.Sprintf("GA-%06d", n)), nil
}

// ParseFolio validates a folio already in GA-XXXXXX form.
func ParseFolio(s string) (Folio, error) {
	f := Folio(s)
	if !f.IsValid() {
		return "", ErrFolioInvalido
	}
	return f, nil
}

// IsValid reports whether f follows the exact GA-XXXXXX format.
func (f Folio) IsValid() bool {
	s := string(f)
	if len(s) != 9 || s[:3] != "GA-" {
		return false
	}
	for i := 3; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func (f Folio) String() string { return string(f) }
