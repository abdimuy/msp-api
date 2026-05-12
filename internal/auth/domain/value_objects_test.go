package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// ─── Email ─────────────────────────────────────────────────────────────────

func TestNewEmail_AcceptsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"foo@bar.com", "foo@bar.com"},
		{" Foo@Bar.Com ", "foo@bar.com"},
		{"a.b+tag@sub.example.com", "a.b+tag@sub.example.com"},
		{"USER@DOMAIN.MX", "user@domain.mx"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			e, err := domain.NewEmail(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, e.Value())
			assert.Equal(t, tc.want, e.String())
			assert.False(t, e.IsZero())
		})
	}
}

func TestNewEmail_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		code string
	}{
		{"", "email_required"},
		{"   ", "email_required"},
		{"plainaddress", "email_invalid"},
		{"@nodomain.com", "email_invalid"},
		{"nolocal@", "email_invalid"},
		{"two@@signs.com", "email_invalid"},
		{"no spaces@allowed.com", "email_invalid"},
		{"trailing.dot@example.", "email_invalid"},
		{"leading.dot@.example.com", "email_invalid"},
		{"dot@example..com", "email_invalid"},
		{"no.dot.in.domain@example", "email_invalid"},
		{strings.Repeat("a", 250) + "@b.com", "email_too_long"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewEmail(tc.in)
			require.Error(t, err)
			ae, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, tc.code, ae.Code)
		})
	}
}

func TestEmail_EqualsAndHydrate(t *testing.T) {
	t.Parallel()
	a, err := domain.NewEmail("Foo@Bar.Com")
	require.NoError(t, err)
	b, err := domain.NewEmail("foo@bar.com")
	require.NoError(t, err)
	c, err := domain.NewEmail("zzz@bar.com")
	require.NoError(t, err)

	assert.True(t, a.Equals(b))
	assert.False(t, a.Equals(c))

	h := domain.HydrateEmail("anything")
	assert.Equal(t, "anything", h.Value())
}

// emailAlphabet keeps property-test local parts to characters the structural
// validator already accepts.
const emailAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._+-"

func TestEmail_RoundTrip_Property(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		local := rapid.StringMatching(`[a-z0-9]([a-z0-9._+-]{0,30}[a-z0-9])?`).Draw(t, "local")
		dom := rapid.StringMatching(`[a-z0-9]{1,20}\.[a-z]{2,10}`).Draw(t, "domain")
		raw := local + "@" + dom

		e, err := domain.NewEmail(raw)
		if err != nil {
			t.Fatalf("expected valid email %q, got: %v", raw, err)
		}
		// Lower-cased canonical form should re-parse to the same value.
		e2, err := domain.NewEmail(e.Value())
		if err != nil {
			t.Fatalf("round-trip parse failed: %v", err)
		}
		if !e.Equals(e2) {
			t.Fatalf("round-trip mismatch: %q -> %q -> %q", raw, e.Value(), e2.Value())
		}
	})
}

func TestEmail_RejectsRandomInvalid_Property(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Strings containing a space are always invalid. Allow ASCII
		// printable + space, then require at least one space.
		s := rapid.StringMatching(`[a-zA-Z0-9@.]{0,5} [a-zA-Z0-9@.]{0,5}`).Draw(t, "with_space")
		if _, err := domain.NewEmail(s); err == nil {
			t.Fatalf("expected error for %q (contains whitespace)", s)
		}
	})
}

// ─── FirebaseUID ───────────────────────────────────────────────────────────

func TestNewFirebaseUID_AcceptsValid(t *testing.T) {
	t.Parallel()
	cases := []string{
		"a",
		"abc123",
		"AbCdEf-_.~/!?",
		strings.Repeat("x", 128),
	}
	for _, tc := range cases {
		t.Run(tc, func(t *testing.T) {
			t.Parallel()
			f, err := domain.NewFirebaseUID(tc)
			require.NoError(t, err)
			assert.Equal(t, tc, f.Value())
			assert.Equal(t, tc, f.String())
			assert.False(t, f.IsZero())
		})
	}
}

func TestNewFirebaseUID_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, in, code string
	}{
		{"empty", "", "firebase_uid_required"},
		{"too_long", strings.Repeat("x", 129), "firebase_uid_too_long"},
		{"space", "abc def", "firebase_uid_invalid"},
		{"tab", "abc\tdef", "firebase_uid_invalid"},
		{"newline", "abc\ndef", "firebase_uid_invalid"},
		{"non_ascii", "ábc", "firebase_uid_invalid"},
		{"control", "abc\x01", "firebase_uid_invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewFirebaseUID(tc.in)
			require.Error(t, err)
			ae, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, tc.code, ae.Code)
		})
	}
}

func TestFirebaseUID_EqualsAndHydrate(t *testing.T) {
	t.Parallel()
	a, err := domain.NewFirebaseUID("abc")
	require.NoError(t, err)
	b, err := domain.NewFirebaseUID("abc")
	require.NoError(t, err)
	c, err := domain.NewFirebaseUID("xyz")
	require.NoError(t, err)
	assert.True(t, a.Equals(b))
	assert.False(t, a.Equals(c))

	h := domain.HydrateFirebaseUID(" anything ")
	assert.Equal(t, " anything ", h.Value())
}

func TestFirebaseUID_RoundTrip_Property(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// ASCII printable, no space — 0x21..0x7E.
		uid := rapid.StringMatching(`[!-~]{1,128}`).Draw(t, "uid")
		f, err := domain.NewFirebaseUID(uid)
		if err != nil {
			t.Fatalf("expected valid uid %q: %v", uid, err)
		}
		if f.Value() != uid {
			t.Fatalf("value mismatch: %q != %q", f.Value(), uid)
		}
	})
}

func TestFirebaseUID_RejectsOverLong_Property(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		uid := rapid.StringMatching(`[!-~]{129,200}`).Draw(t, "uid")
		_, err := domain.NewFirebaseUID(uid)
		if err == nil {
			t.Fatalf("expected too-long error for length %d", len(uid))
		}
	})
}

// ─── Nombre ────────────────────────────────────────────────────────────────

func TestNewNombre_AcceptsValid(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"Aldrich", "Aldrich"},
		{"  Aldrich Cortero  ", "Aldrich Cortero"},
		{"María José O'Connor-Núñez", "María José O'Connor-Núñez"},
		{"Dr. Foo 2nd", "Dr. Foo 2nd"},
		{"José.María-Foo", "José.María-Foo"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			n, err := domain.NewNombre(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, n.Value())
			assert.Equal(t, tc.want, n.String())
			assert.False(t, n.IsZero())
		})
	}
}

func TestNewNombre_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, in, code string
	}{
		{"empty", "", "nombre_required"},
		{"whitespace_only", "   ", "nombre_required"},
		{"too_long", strings.Repeat("a", 201), "nombre_too_long"},
		{"forbidden_char", "Aldrich@home", "nombre_invalid"},
		{"forbidden_underscore", "Aldrich_C", "nombre_invalid"},
		{"forbidden_paren", "Aldrich (C)", "nombre_invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewNombre(tc.in)
			require.Error(t, err)
			ae, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, tc.code, ae.Code)
		})
	}
}

func TestNombre_EqualsAndHydrate(t *testing.T) {
	t.Parallel()
	a, err := domain.NewNombre("Foo")
	require.NoError(t, err)
	b, err := domain.NewNombre("Foo")
	require.NoError(t, err)
	c, err := domain.NewNombre("Bar")
	require.NoError(t, err)
	assert.True(t, a.Equals(b))
	assert.False(t, a.Equals(c))

	h := domain.HydrateNombre("anything@goes")
	assert.Equal(t, "anything@goes", h.Value())
}

func TestNombre_RoundTrip_Property(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.StringMatching(`[A-Za-z][A-Za-z0-9 .'-]{0,50}[A-Za-z]`).Draw(t, "nombre")
		n, err := domain.NewNombre(s)
		if err != nil {
			t.Fatalf("expected valid nombre %q: %v", s, err)
		}
		if n.Value() != strings.TrimSpace(s) {
			t.Fatalf("trim mismatch: in=%q out=%q", s, n.Value())
		}
	})
}
