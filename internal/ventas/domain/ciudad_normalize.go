//nolint:misspell // Spanish vocabulary (ciudad, acentos) per project convention.
package domain

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// NormalizeCiudad folds a captured ciudad string into the deterministic key
// used to match it against Microsip's CIUDADES catalog.
//
// This is a BRIDGE, needed only while the app still sends free text. Once the
// picker ships, the venta carries a CIUDAD_ID and no matching is required.
//
// The rules are deliberately conservative — accents, case and whitespace only.
// It does NOT try to reconcile the catalog's real near-duplicates
// ("TLACHICHUCA" vs "TLACHICHUCA, PUE", "TECAMACHALCO, PUE."): guessing which
// of two rows a vendor meant is how you write a cliente into the wrong state.
// Those rows are cleaned up in Microsip by the office before the flag goes on.
func NormalizeCiudad(s string) string {
	// Decompose, drop combining marks, recompose: "Cañada" → "CANADA" needs
	// the ñ handled too, so Mn removal happens on the decomposed form.
	t := transform.Chain(
		norm.NFD,
		runes.Remove(runes.In(unicode.Mn)),
		norm.NFC,
	)
	folded, _, err := transform.String(t, s)
	if err != nil {
		// Transform only fails on malformed input; fall back to the raw string
		// rather than silently returning an empty key that matches nothing.
		folded = s
	}

	folded = strings.ToUpper(folded)
	// Collapse every run of whitespace (including the trailing spaces the
	// production catalog carries on "COYOMEAPAN " and "ESPERANZA ").
	return strings.Join(strings.Fields(folded), " ")
}
