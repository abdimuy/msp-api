package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	errTelefonoRequired      = errors.New("telefono: required")
	errTelefonoInvalidFormat = errors.New("telefono: invalid format")
)

// telefonoPattern accepts a normalized phone of 10 digits (MX local) or
// 12-13 digits with country code (E.164-ish). The constructor strips
// non-digits before validation.
var telefonoPattern = regexp.MustCompile(`^\d{10,13}$`)

// Telefono is a normalized phone number, stored as digits only.
type Telefono struct{ value string }

// NewTelefono builds a Telefono. Strips spaces, dashes, parentheses, and
// the leading '+' before validating.
func NewTelefono(s string) (Telefono, error) {
	digits := stripPhoneSeparators(s)
	if digits == "" {
		return Telefono{}, errTelefonoRequired
	}
	if !telefonoPattern.MatchString(digits) {
		return Telefono{}, fmt.Errorf("%w: %q", errTelefonoInvalidFormat, s)
	}
	return Telefono{value: digits}, nil
}

// HydrateTelefono rebuilds a Telefono from persistence without validation.
func HydrateTelefono(s string) Telefono { return Telefono{value: s} }

// Value returns the digits-only representation.
func (t Telefono) Value() string { return t.value }

// String returns the digits-only representation.
func (t Telefono) String() string { return t.value }

// Equals reports whether two phones are identical.
func (t Telefono) Equals(other Telefono) bool { return t.value == other.value }

func stripPhoneSeparators(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			_, _ = b.WriteRune(r) // strings.Builder.WriteRune never returns a real error
		}
	}
	return b.String()
}
