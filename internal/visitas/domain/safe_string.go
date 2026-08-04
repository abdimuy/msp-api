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
//     legitimate place in a cobrador's free-text fields and break diff/grep
//     tooling.
//  3. Invalid UTF-8 byte sequences — Go strings can technically hold these;
//     reject defensively.
//
// Everything else — accents, em-dash, smart quotes, emoji — is allowed.
// MSP_VISITAS' text columns hold plain UTF-8 bytes (see migration 000047's
// header comment); anything Unicode-valid round-trips byte-equal.
//
// Mirrors internal/cobranza/domain/safe_string.go — duplicated here rather
// than imported because domain packages are unexported-symbol boundaries and
// each module is a self-contained vertical slice.
func validateSafeChars(s string) error {
	if !utf8.ValidString(s) {
		return ErrVisitaStringCaracteresInvalidos
	}
	if strings.ContainsRune(s, 0) {
		return ErrVisitaStringCaracteresInvalidos
	}
	for _, r := range s {
		if r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		if r < 0x20 || r == 0x7F {
			return ErrVisitaStringCaracteresInvalidos
		}
	}
	return nil
}

// normalizeNFC returns s in Unicode Normalization Form C — the canonical
// composed form. Without normalization, the same visual character (e.g. "é")
// can be encoded two ways, producing strings that LOOK identical but compare
// !=. Calling NFC at the domain boundary kills that class of silent equality
// bugs (docs/module-standards/ENCODING_HANDLING.md).
func normalizeNFC(s string) string { return norm.NFC.String(s) }

// utf8RuneLen returns the number of Unicode codepoints in s. Used instead of
// len(s) (byte count) for max-length checks against column widths declared
// in CHARACTER SET UTF8 — those are in characters, not bytes.
func utf8RuneLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// requireBounded trims s, normalizes to NFC, rejects empty, rejects strings
// longer than max (in runes), and rejects unsafe characters.
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

// trimOptionalBounded trims an optional string, normalizes to NFC, and
// applies the same length+safety checks as requireBounded. A blank input
// (empty or whitespace-only) normalizes to "" — the domain's convention for
// "not provided" on the nullable NOTA field (see Visita.nota doc comment).
func trimOptionalBounded(s string, maxLen int, errTooLong error) (string, error) {
	trimmed := normalizeNFC(strings.TrimSpace(s))
	if trimmed == "" {
		return "", nil
	}
	if utf8RuneLen(trimmed) > maxLen {
		return "", errTooLong
	}
	if err := validateSafeChars(trimmed); err != nil {
		return "", err
	}
	return trimmed, nil
}
