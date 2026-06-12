package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	errTelefonoRequired      = errors.New("telefono: required")
	errTelefonoInvalidFormat = errors.New("telefono: invalid format, expected 10-digit MX number")
)

// nonDigit matches every character that is not an ASCII digit. NewTelefono
// strips these (spaces, dashes, parentheses, dots, the '+' sign) before
// validating, so callers may pass numbers in any human format.
var nonDigit = regexp.MustCompile(`\D`)

// mxCountryCode is the Mexican country code stripped from inputs that carry it
// (e.g. "+52..." or "52..."). We store the national 10-digit number only.
const mxCountryCode = "52"

// telefonoLength is the canonical national length for Mexican phone numbers.
const telefonoLength = 10

// Telefono is a Mexican phone number stored in canonical 10-digit national
// form (e.g. "4491234567"). The +52 country code and any separators are
// removed at construction time. This is the form Microsip stores in its
// address phone column and the form our own MSP_* snapshots carry.
type Telefono struct{ value string }

// NewTelefono canonicalizes a raw phone string to 10-digit MX national form.
// It accepts input with an optional "+52"/"52" country-code prefix or 10 bare
// digits, in any separator style (spaces, dashes, parentheses, dots). All
// non-digit characters are stripped; an optional leading "52" country code is
// removed; the result must be exactly 10 digits. Inputs that do not reduce to
// 10 national digits are rejected.
func NewTelefono(s string) (Telefono, error) {
	if s == "" {
		return Telefono{}, errTelefonoRequired
	}
	digits := nonDigit.ReplaceAllString(s, "")
	// Strip the +52 country code when present (12 digits = 52 + 10 national).
	if len(digits) == len(mxCountryCode)+telefonoLength && strings.HasPrefix(digits, mxCountryCode) {
		digits = digits[len(mxCountryCode):]
	}
	if len(digits) != telefonoLength {
		return Telefono{}, fmt.Errorf("%w: %q", errTelefonoInvalidFormat, s)
	}
	return Telefono{value: digits}, nil
}

// HydrateTelefono rebuilds a Telefono from persistence without validation.
func HydrateTelefono(s string) Telefono { return Telefono{value: s} }

// Value returns the canonical 10-digit MX national representation.
func (t Telefono) Value() string { return t.value }

// String returns the canonical 10-digit MX national representation.
func (t Telefono) String() string { return t.value }

// Equals reports whether two phones are identical.
func (t Telefono) Equals(other Telefono) bool { return t.value == other.value }
