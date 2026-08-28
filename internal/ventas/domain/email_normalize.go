//nolint:misspell // ventas vocabulary is Spanish (vendedores, correos) per project convention.
package domain

import "strings"

// NormalizarEmail returns the canonical form of an email address: trimmed of
// surrounding whitespace and lowercased.
//
// It exists because the SAME address reaches the ventas module from two
// independent sources that used to disagree about its shape:
//
//   - the auth module, whose Email VO trims + lowercases every address before
//     it ever reaches MSP_USUARIOS, so every stored address is canonical;
//   - the Firestore `users` roster, which is typed by hand from the desktop
//     app and therefore holds whatever the operator wrote — including
//     capitals.
//
// Comparing one against the other verbatim is what made a vendedor with a
// capitalized address vanish from a venta without a log and without an error:
// the lookup key was canonical and the probe was raw, so the map simply
// missed. Both sides now run through this function, so the comparison is
// between two values of the same shape.
//
// The result is NOT validated — an empty or malformed input comes back
// normalized, not rejected. Validation is the caller's job (the domain VO for
// the venta's own snapshot; MSP_USUARIOS's UNIQUE constraint for identity).
func NormalizarEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
