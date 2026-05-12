package domain

import (
	"strings"
	"unicode"
)

// Maximum lengths mirror the Firebird column widths in
// migrations-firebird/000001_create_auth_tables.up.sql so that any value the
// domain accepts can be persisted without truncation.
const (
	maxEmailLength       = 255
	maxFirebaseUIDLength = 128
	maxNombreLength      = 200
)

// ─── Email ─────────────────────────────────────────────────────────────────

// Email is a normalized, lowercased email address. The validation is
// deliberately lighter than RFC 5322 — we only check for the structural
// features that matter downstream (a single '@', at least one '.' in the
// domain part, no whitespace, bounded length). Stricter validation belongs
// in the Firebase identity provider, not in domain code.
type Email struct{ value string }

// NewEmail validates and constructs an Email. The input is trimmed and
// lowercased before validation.
func NewEmail(s string) (Email, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Email{}, ErrEmailRequerido
	}
	if len(s) > maxEmailLength {
		return Email{}, ErrEmailDemasiadoLargo
	}
	s = strings.ToLower(s)
	if !isValidEmail(s) {
		return Email{}, ErrEmailInvalido
	}
	return Email{value: s}, nil
}

// HydrateEmail rebuilds an Email from persistence without validation.
func HydrateEmail(s string) Email { return Email{value: s} }

// Value returns the canonical lowercased email.
func (e Email) Value() string { return e.value }

// String returns the canonical lowercased email.
func (e Email) String() string { return e.value }

// Equals reports whether two emails are identical.
func (e Email) Equals(other Email) bool { return e.value == other.value }

// IsZero reports whether the Email has its zero value (no email assigned).
func (e Email) IsZero() bool { return e.value == "" }

func isValidEmail(s string) bool {
	at := strings.IndexByte(s, '@')
	if at <= 0 || at != strings.LastIndexByte(s, '@') {
		return false
	}
	local, domain := s[:at], s[at+1:]
	if local == "" || domain == "" {
		return false
	}
	if !strings.Contains(domain, ".") {
		return false
	}
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}
	if strings.Contains(domain, "..") {
		return false
	}
	for _, r := range s {
		if unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// ─── FirebaseUID ───────────────────────────────────────────────────────────

// FirebaseUID is a Firebase Authentication user identifier. Per the Firebase
// docs the UID is 1–128 characters; we additionally constrain it to ASCII
// printable characters with no spaces, which matches every UID Firebase has
// been observed to emit and protects against control-character injection in
// downstream logs.
type FirebaseUID struct{ value string }

// NewFirebaseUID validates and constructs a FirebaseUID.
func NewFirebaseUID(s string) (FirebaseUID, error) {
	if s == "" {
		return FirebaseUID{}, ErrFirebaseUIDRequerido
	}
	if len(s) > maxFirebaseUIDLength {
		return FirebaseUID{}, ErrFirebaseUIDDemasiadoLargo
	}
	if !isPrintableASCIINoSpace(s) {
		return FirebaseUID{}, ErrFirebaseUIDInvalido
	}
	return FirebaseUID{value: s}, nil
}

// HydrateFirebaseUID rebuilds a FirebaseUID from persistence without
// validation.
func HydrateFirebaseUID(s string) FirebaseUID { return FirebaseUID{value: s} }

// Value returns the underlying uid string.
func (f FirebaseUID) Value() string { return f.value }

// String returns the underlying uid string.
func (f FirebaseUID) String() string { return f.value }

// Equals reports whether two uids are identical.
func (f FirebaseUID) Equals(other FirebaseUID) bool { return f.value == other.value }

// IsZero reports whether the FirebaseUID has its zero value.
func (f FirebaseUID) IsZero() bool { return f.value == "" }

func isPrintableASCIINoSpace(s string) bool {
	for i := range len(s) {
		c := s[i]
		// Printable ASCII range is 0x21..0x7E (excludes space 0x20 and DEL).
		if c < 0x21 || c > 0x7E {
			return false
		}
	}
	return true
}

// ─── Nombre ────────────────────────────────────────────────────────────────

// Nombre is a person's display name. The input is trimmed and must consist
// of letters (any unicode script), digits, spaces, and the punctuation marks
// "." "'" "-". Empty or whitespace-only input is rejected; length is bounded
// to 200 chars to match the Firebird column.
type Nombre struct{ value string }

// NewNombre validates and constructs a Nombre.
func NewNombre(s string) (Nombre, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Nombre{}, ErrNombreRequerido
	}
	if len(s) > maxNombreLength {
		return Nombre{}, ErrNombreDemasiadoLargo
	}
	if !isValidNombre(s) {
		return Nombre{}, ErrNombreInvalido
	}
	return Nombre{value: s}, nil
}

// HydrateNombre rebuilds a Nombre from persistence without validation.
func HydrateNombre(s string) Nombre { return Nombre{value: s} }

// Value returns the trimmed nombre string.
func (n Nombre) Value() string { return n.value }

// String returns the trimmed nombre string.
func (n Nombre) String() string { return n.value }

// Equals reports whether two nombres are identical.
func (n Nombre) Equals(other Nombre) bool { return n.value == other.value }

// IsZero reports whether the Nombre has its zero value.
func (n Nombre) IsZero() bool { return n.value == "" }

func isValidNombre(s string) bool {
	for _, r := range s {
		switch {
		case unicode.IsLetter(r):
		case unicode.IsDigit(r):
		case r == ' ' || r == '.' || r == '\'' || r == '-':
		default:
			return false
		}
	}
	return true
}
