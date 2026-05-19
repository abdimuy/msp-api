//nolint:misspell // domain vocabulary is Spanish (caracteres, etc.) per project convention.
package domain

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// validateSafeChars rejects strings that would corrupt persistence:
//
//  1. NUL byte (U+0000) — string terminators in many drivers and external
//     systems; safer to forbid outright than reason about every interop layer.
//  2. ASCII control characters U+0001..U+001F except tab (U+0009), line feed
//     (U+000A), and carriage return (U+000D), plus U+007F (DEL). They have no
//     legitimate place in user-facing form fields and break diff/grep tooling.
//  3. Invalid UTF-8 byte sequences — Go strings can technically hold these;
//     reject defensively.
//
// Everything else — accents, em-dash, smart quotes, emoji, kanji, hebreo —
// is allowed. The MSP_* text columns are CHARACTER SET UTF8 (migration 000005)
// so anything Unicode-valid round-trips byte-equal.
//
// The check sits behind requireBounded / trimOptionalBounded so every
// string-typed field in the ventas aggregate runs through it.
func validateSafeChars(s string) error {
	if !utf8.ValidString(s) {
		return ErrStringUnsafeChars
	}
	if strings.ContainsRune(s, 0) {
		return ErrStringUnsafeChars
	}
	for _, r := range s {
		// Tab/LF/CR are the only control chars users legitimately type.
		if r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		if r < 0x20 || r == 0x7F {
			return ErrStringUnsafeChars
		}
	}
	return nil
}

// normalizeNFC returns s in Unicode Normalization Form C — the canonical
// composed form. Without normalization, the same visual character (e.g. "é")
// can be encoded two ways (single codepoint U+00E9 vs "e" + combining acute
// U+0301), producing strings that LOOK identical but compare !=. Calling NFC
// at the domain boundary kills that class of silent equality bugs.
func normalizeNFC(s string) string { return norm.NFC.String(s) }
