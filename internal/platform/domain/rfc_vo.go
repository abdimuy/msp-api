package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	errRFCRequired      = errors.New("rfc: required")
	errRFCInvalidFormat = errors.New("rfc: invalid format")
)

// rfcPattern matches a Mexican RFC: 12 chars (moral) or 13 chars (física).
//
//	[A-ZÑ&]{3,4}  — 3 letters (moral) or 4 letters (física)
//	\d{6}          — birth/incorporation date (YYMMDD)
//	[A-Z\d]{3}     — homoclave (2 chars + check digit)
var rfcPattern = regexp.MustCompile(`^[A-ZÑ&]{3,4}\d{6}[A-Z\d]{3}$`)

// RFC is a Mexican tax identifier (Registro Federal de Contribuyentes).
type RFC struct{ value string }

// NewRFC validates and constructs an RFC. The input is upper-cased and trimmed.
func NewRFC(s string) (RFC, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return RFC{}, errRFCRequired
	}
	if !rfcPattern.MatchString(s) {
		return RFC{}, fmt.Errorf("%w: %q", errRFCInvalidFormat, s)
	}
	return RFC{value: s}, nil
}

// HydrateRFC rebuilds an RFC from persistence without validation.
func HydrateRFC(s string) RFC { return RFC{value: s} }

// Value returns the canonical RFC string.
func (r RFC) Value() string { return r.value }

// String returns the canonical RFC string.
func (r RFC) String() string { return r.value }

// Equals reports whether two RFCs are identical.
func (r RFC) Equals(other RFC) bool { return r.value == other.value }

// IsMoral reports whether this RFC belongs to a legal entity (12 chars).
func (r RFC) IsMoral() bool { return len(r.value) == 12 }

// IsFisica reports whether this RFC belongs to a natural person (13 chars).
func (r RFC) IsFisica() bool { return len(r.value) == 13 }
