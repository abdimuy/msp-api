package domain

import (
	"errors"
	"fmt"
	"regexp"
)

var (
	errTelefonoRequired      = errors.New("telefono: required")
	errTelefonoInvalidFormat = errors.New("telefono: invalid format, expected E.164")
)

// e164Pattern is the strict E.164 format used by Twilio, AWS SNS, and the
// ITU-T E.164 spec: a leading '+', a country-code digit 1-9, and 1-14 more
// digits. Total length 2-15 digits after the '+'. No spaces, separators, or
// leading zero on the country code.
//
//	valid:   +15551234567, +524491234567, +447911123456
//	invalid: 5551234567, +0123, +1-555-123-4567, (555) 123-4567
var e164Pattern = regexp.MustCompile(`^\+[1-9]\d{1,14}$`)

// Telefono is a phone number stored in canonical E.164 form (e.g. +524491234567).
type Telefono struct{ value string }

// NewTelefono builds a Telefono from a raw E.164 string. The constructor
// does no normalization — callers must provide the number already formatted
// per the ITU-T E.164 spec (leading '+', country code, subscriber digits,
// nothing else). Inputs containing spaces, dashes, parentheses, or missing
// the leading '+' are rejected.
func NewTelefono(s string) (Telefono, error) {
	if s == "" {
		return Telefono{}, errTelefonoRequired
	}
	if !e164Pattern.MatchString(s) {
		return Telefono{}, fmt.Errorf("%w: %q", errTelefonoInvalidFormat, s)
	}
	return Telefono{value: s}, nil
}

// HydrateTelefono rebuilds a Telefono from persistence without validation.
func HydrateTelefono(s string) Telefono { return Telefono{value: s} }

// Value returns the canonical E.164 representation.
func (t Telefono) Value() string { return t.value }

// String returns the canonical E.164 representation.
func (t Telefono) String() string { return t.value }

// Equals reports whether two phones are identical.
func (t Telefono) Equals(other Telefono) bool { return t.value == other.value }
