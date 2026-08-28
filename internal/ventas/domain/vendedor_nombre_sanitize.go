//nolint:misspell // ventas vocabulary is Spanish (vendedor, nombre) per project convention.
package domain

import (
	"strings"
	"unicode/utf8"
)

// SanitizarNombreVendedor returns a name that NewVendedorSnapshot is
// guaranteed to accept, or "" when nothing usable survives.
//
// It exists because a vendedor's nombre no longer comes only from the phone,
// which types it into a bounded form field. Since the server resolves a
// venta's vendedores from the fleet roster, the nombre may be whatever an
// operator wrote into a Firestore document — and the domain rejects a name
// that is blank, longer than maxVendedorNombreLength, or carries a control
// character. Handing such a value straight to NewVendedorSnapshot turns a
// typo in a roster document into a venta the phone cannot create at all,
// which is the one outcome the roster feature is not allowed to have.
//
// The transformation, in order: tab/LF/CR become a single space, every other
// control character is dropped, the result is NFC-normalized and trimmed, and
// finally truncated to maxVendedorNombreLength runes. A value that still
// fails validateSafeChars — which should be impossible — comes back empty so
// the caller falls back rather than trusting it.
func SanitizarNombreVendedor(s string) string {
	limpio := strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case r == utf8.RuneError:
			// Produced by ranging over invalid UTF-8. Dropping it keeps the
			// result valid rather than storing a replacement character.
			return -1
		case r < 0x20 || r == 0x7F:
			return -1
		default:
			return r
		}
	}, s)

	limpio = normalizeNFC(strings.TrimSpace(limpio))
	if utf8RuneLen(limpio) > maxVendedorNombreLength {
		limpio = strings.TrimSpace(string([]rune(limpio)[:maxVendedorNombreLength]))
	}
	if limpio == "" || validateSafeChars(limpio) != nil {
		return ""
	}
	return limpio
}
