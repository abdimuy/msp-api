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
// string-typed field in the cobranza aggregate runs through it.
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

// requireBounded trims s, normalizes to Unicode NFC, rejects empty, rejects
// strings longer than max (in runes, not bytes), and rejects strings carrying
// unsafe characters (NUL or other ASCII control chars).
func requireBounded(s string, maxLen int, errRequired, errTooLong error) (string, error) {
	s = normalizeNFC(strings.TrimSpace(s))
	if s == "" {
		return "", errRequired
	}
	if utf8RuneLen(s) > maxLen {
		return "", errTooLong
	}
	if err := validateSafeChars(s); err != nil {
		return "", err
	}
	return s, nil
}

// trimOptionalBounded trims an optional pointer string, normalizes to NFC,
// and applies the same length+safety checks as requireBounded. A nil input or
// pointer to a blank string both yield nil output.
func trimOptionalBounded(p *string, maxLen int, errTooLong error) (*string, error) {
	if p == nil {
		return nil, nil //nolint:nilnil // optional pointer pattern: nil ptr + nil err means "not provided".
	}
	trimmed := normalizeNFC(strings.TrimSpace(*p))
	if trimmed == "" {
		return nil, nil //nolint:nilnil // optional pointer pattern: blank input normalizes to "not provided".
	}
	if utf8RuneLen(trimmed) > maxLen {
		return nil, errTooLong
	}
	if err := validateSafeChars(trimmed); err != nil {
		return nil, err
	}
	return &trimmed, nil
}

// utf8RuneLen returns the number of Unicode codepoints in s. Used instead of
// len(s) (which counts bytes) for max-length checks against column widths
// declared in CHARACTER SET UTF8 — those are in characters, not bytes.
func utf8RuneLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}
