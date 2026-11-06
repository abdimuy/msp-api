package domain

import "golang.org/x/text/unicode/norm"

// normalizarNFC returns s in Unicode NFC (canonical composition), the single
// form every free-text field of the domain is stored in (ENCODING_HANDLING.md).
// Without it, "Juárez" typed with a combining accent and "Juárez" typed
// precomposed are two different strings for any byte comparison or lookup, and
// the difference is invisible on screen.
//
// It is applied in the constructor or mutator that first receives the text
// (AbrirGarantia, newArticulo, newEvento), so everything downstream can assume
// NFC. Hydration does not normalize: those rows already came from here.
func normalizarNFC(s string) string { return norm.NFC.String(s) }
