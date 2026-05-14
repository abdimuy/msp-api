//nolint:misspell // domain vocabulary is Spanish (caracteres, etc.) per project convention.
package domain

import (
	"strings"

	"golang.org/x/text/encoding/charmap"
)

// validateSafeChars rejects strings that cannot be persisted safely through
// the Firebird WIN1252 connection. Two failure modes:
//
//  1. NUL byte (U+0000) — the driver may silently truncate at the first NUL
//     or pass through corrupt bytes; we reject upfront so user input never
//     reaches the column in that shape.
//  2. Any rune outside WIN1252's codable set (e.g., emoji, CJK, supplementary
//     planes). The driver lossy-encodes such runes to '?' or worse — we
//     reject upfront so the persisted name matches the submitted one.
//
// This check sits behind requireBounded / trimOptionalBounded so every
// string-typed field in the ventas aggregate benefits without per-VO code
// duplication.
func validateSafeChars(s string) error {
	if strings.ContainsRune(s, 0) {
		return ErrStringUnsafeChars
	}
	enc := charmap.Windows1252.NewEncoder()
	if _, err := enc.String(s); err != nil {
		return ErrStringUnsafeChars
	}
	return nil
}
