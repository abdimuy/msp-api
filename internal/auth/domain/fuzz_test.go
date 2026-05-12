package domain

import (
	"strings"
	"testing"
)

// FuzzEmailParse exercises NewEmail with arbitrary input. The contract is:
// NewEmail must never panic, and when it accepts an input it must return a
// non-empty canonical value. Stricter semantic checks are covered in
// value_objects_test.go.
func FuzzEmailParse(f *testing.F) {
	seeds := []string{
		"alice@example.com",
		"bob+tag@host.dev",
		"",
		"no-at",
		"@missing-local",
		"missing-domain@",
		"double@@at",
		"x@y..z",
		"  spaces  @ x.y",
		"a@b.c",
		"UPPER@CASE.COM",
		strings.Repeat("a", 300) + "@example.com",
		"a@" + strings.Repeat("b", 300) + ".c",
		"\x00@example.com",
		"a@b.",
		".a@b.c",
		"a@.b.c",
		"a@b..c",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		// Must never panic.
		e, err := NewEmail(in)
		if err != nil {
			// Accept any typed error — the API surface is "rejected".
			return
		}
		if e.Value() == "" {
			t.Fatalf("NewEmail(%q) accepted but produced empty Value()", in)
		}
		// Lowercased + trimmed invariant.
		if e.Value() != strings.ToLower(strings.TrimSpace(in)) {
			// Allow only the documented normalization (trim + lower).
			// Anything else is a regression in the normalization contract.
			t.Fatalf("NewEmail(%q) returned %q; expected trim+lowercase normalization", in, e.Value())
		}
	})
}

// FuzzFirebaseUIDParse exercises NewFirebaseUID. Must never panic; accepted
// inputs round-trip cleanly.
func FuzzFirebaseUIDParse(f *testing.F) {
	seeds := []string{
		"abc123",
		"",
		" ",
		"with space",
		"tab\there",
		"newline\nhere",
		"\x00null",
		"DEL\x7f",
		strings.Repeat("a", 128),
		strings.Repeat("a", 129),
		"emoji-\U0001F600",
		"control\x01",
		"!~",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		f, err := NewFirebaseUID(in)
		if err != nil {
			return
		}
		if f.Value() == "" {
			t.Fatalf("NewFirebaseUID(%q) accepted but produced empty Value()", in)
		}
		if f.Value() != in {
			t.Fatalf("NewFirebaseUID(%q) returned %q; should round-trip without mutation", in, f.Value())
		}
	})
}

// FuzzNombreParse exercises NewNombre. Must never panic; accepted inputs are
// trimmed but otherwise preserved.
func FuzzNombreParse(f *testing.F) {
	seeds := []string{
		"Alice",
		"  Padded  ",
		"",
		"   ",
		"O'Brien",
		"José Pérez",
		"123",
		"a.b-c",
		strings.Repeat("a", 200),
		strings.Repeat("a", 201),
		"name\nwith newline",
		"name\twith tab",
		"emoji \U0001F600",
		"中文",
		"<script>",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		n, err := NewNombre(in)
		if err != nil {
			return
		}
		if n.Value() == "" {
			t.Fatalf("NewNombre(%q) accepted but produced empty Value()", in)
		}
		if n.Value() != strings.TrimSpace(in) {
			t.Fatalf("NewNombre(%q) returned %q; expected trim normalization only", in, n.Value())
		}
	})
}
