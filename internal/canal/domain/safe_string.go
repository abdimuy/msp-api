package domain

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// requireBounded trims s, normalizes to Unicode NFC, rejects empty, rejects
// strings longer than maxLen (measured in codepoints, not bytes), and
// rejects strings carrying unsafe characters (NUL or other ASCII control
// chars). Mirrors the pipeline documented in
// docs/module-standards/02-value-objects-errors.md and implemented in
// internal/ventas/domain/direccion.go — canal keeps its own copy because it
// is a sealed module (ADR-0009) and may not import another module's domain.
func requireBounded(s string, maxLen int, errRequired, errTooLong error) (string, error) {
	s = normalizeNFC(strings.TrimSpace(s))
	if s == "" {
		return "", errRequired
	}
	if codepointCount(s) > maxLen {
		return "", errTooLong
	}
	if err := validateSafeChars(s); err != nil {
		return "", err
	}
	return s, nil
}

// optionalBounded is requireBounded without the non-empty requirement: an
// empty (or all-whitespace) input normalizes to "", not an error. Used for
// fields the mailbox must still accept and store even when Meta sends
// nothing usable in them — the module's job is to relay whatever arrived,
// not to reject a message because one field is blank.
func optionalBounded(s string, maxLen int, errTooLong error) (string, error) {
	s = normalizeNFC(strings.TrimSpace(s))
	if s == "" {
		return "", nil
	}
	if codepointCount(s) > maxLen {
		return "", errTooLong
	}
	if err := validateSafeChars(s); err != nil {
		return "", err
	}
	return s, nil
}

// validateSafeChars rejects strings that would corrupt persistence: invalid
// UTF-8, the NUL byte, and ASCII control characters other than tab/LF/CR.
// See internal/ventas/domain/safe_string.go for the exact rationale this
// mirrors.
func validateSafeChars(s string) error {
	if !utf8.ValidString(s) {
		return ErrMensajeEntranteCaracteresInvalidos
	}
	if strings.ContainsRune(s, 0) {
		return ErrMensajeEntranteCaracteresInvalidos
	}
	for _, r := range s {
		if r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		if r < 0x20 || r == 0x7F {
			return ErrMensajeEntranteCaracteresInvalidos
		}
	}
	return nil
}

// normalizeNFC returns s in Unicode Normalization Form C — the canonical
// composed form — so visually identical strings encoded differently compare
// equal (e.g. wamid/text bytes Meta may have decomposed).
func normalizeNFC(s string) string { return norm.NFC.String(s) }

// codepointCount returns the number of Unicode codepoints in s. Used instead
// of len(s) (which counts bytes) for max-length checks — text columns are
// bounded in characters, not bytes.
func codepointCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}
