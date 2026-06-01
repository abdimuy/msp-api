//nolint:misspell // Spanish vocabulary by convention.
package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// validateSafeChars and its siblings are package-private. We exercise them via
// NewPagoRecibido (which calls requireBounded for Cobrador) and AdjuntarImagen
// (which calls trimOptionalBounded for Descripcion).

// ─── validateSafeChars via Cobrador field ─────────────────────────────────────

func TestSafeChars_RejectsNULByte(t *testing.T) {
	t.Parallel()

	p := validParams(t)
	p.Cobrador = "foo\x00bar"

	_, err := domain.NewPagoRecibido(p)

	require.ErrorIs(t, err, domain.ErrStringUnsafeChars)
}

func TestSafeChars_RejectsControlChars(t *testing.T) {
	t.Parallel()

	controls := []struct {
		name string
		r    rune
	}{
		{"SOH_0x01", 0x01},
		{"BEL_0x07", 0x07},
		{"ESC_0x1B", 0x1B},
		{"US_0x1F", 0x1F},
		{"DEL_0x7F", 0x7F},
	}

	for _, tc := range controls {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := validParams(t)
			p.Cobrador = "foo" + string(tc.r) + "bar"

			_, err := domain.NewPagoRecibido(p)

			require.ErrorIs(t, err, domain.ErrStringUnsafeChars)
		})
	}
}

// Tab (0x09), line feed (0x0A), and carriage return (0x0D) are the only
// control characters users may legitimately include in free-text fields.
func TestSafeChars_AllowsTabLFCR(t *testing.T) {
	t.Parallel()

	p := validParams(t)
	p.Cobrador = "foo\tbar\nbaz\rqux"

	_, err := domain.NewPagoRecibido(p)

	require.NoError(t, err)
}

func TestSafeChars_RejectsInvalidUTF8(t *testing.T) {
	t.Parallel()

	p := validParams(t)
	p.Cobrador = "\xff\xfe"

	_, err := domain.NewPagoRecibido(p)

	require.ErrorIs(t, err, domain.ErrStringUnsafeChars)
}

func TestSafeChars_AllowsUnicode(t *testing.T) {
	t.Parallel()

	p := validParams(t)
	// Emoji, Hebrew, and accented Latin — all valid Unicode.
	p.Cobrador = "Jorge 👨 שלום García"

	pago, err := domain.NewPagoRecibido(p)

	require.NoError(t, err)
	require.NotNil(t, pago)
}

// ─── normalizeNFC via Cobrador field ──────────────────────────────────────────

// "é" can be encoded as U+00E9 (composed, NFC) or as "e" + U+0301 (decomposed, NFD).
// requireBounded must normalize to NFC so the stored form is always composed.
func TestNFC_Normalization(t *testing.T) {
	t.Parallel()

	// Decomposed NFD form: ASCII 'e' (0x65) + combining acute U+0301 (0xCC 0x81) = 3 bytes.
	decomposed := "é"
	// NFC composed form: é (U+00E9) = 2 bytes.
	composed := "é"

	p := validParams(t)
	p.Cobrador = decomposed

	pago, err := domain.NewPagoRecibido(p)

	require.NoError(t, err)
	// Stored value must be the NFC composed form.
	assert.Equal(t, composed, pago.Cobrador())
	// Byte-level verification: composed é is 2 bytes, not 3.
	assert.Len(t, []byte(pago.Cobrador()), 2)
}

// ─── requireBounded boundary cases ────────────────────────────────────────────

func TestRequireBounded_TrimsAndRejectsEmpty(t *testing.T) {
	t.Parallel()

	p := validParams(t)
	p.Cobrador = "   "

	_, err := domain.NewPagoRecibido(p)

	require.ErrorIs(t, err, domain.ErrPagoCobradorRequerido)
}

// maxCobradorLength is 100 runes (see pago_recibido.go). A Cobrador with 101
// "ñ" runes (202 bytes) must exceed the rune limit and be rejected.
func TestRequireBounded_RuneCountedLength(t *testing.T) {
	t.Parallel()

	p := validParams(t)
	p.Cobrador = strings.Repeat("ñ", 101)

	_, err := domain.NewPagoRecibido(p)

	require.ErrorIs(t, err, domain.ErrPagoCobradorDemasiadoLargo)
}

// Exactly 100 "ñ" runes (200 bytes) must be accepted.
func TestRequireBounded_AcceptsAtMaxRuneLength(t *testing.T) {
	t.Parallel()

	p := validParams(t)
	p.Cobrador = strings.Repeat("ñ", 100)

	pago, err := domain.NewPagoRecibido(p)

	require.NoError(t, err)
	require.NotNil(t, pago)
}

// ─── trimOptionalBounded via Descripcion field ────────────────────────────────

func TestTrimOptionalBounded_NilPointerOK(t *testing.T) {
	t.Parallel()

	pago := newValidPago(t)
	params := adjuntarParams(t)
	params.Descripcion = nil

	img, err := pago.AdjuntarImagen(params)

	require.NoError(t, err)
	assert.Nil(t, img.Descripcion())
}

func TestTrimOptionalBounded_BlankPointerYieldsNil(t *testing.T) {
	t.Parallel()

	blank := "   "
	pago := newValidPago(t)
	params := adjuntarParams(t)
	params.Descripcion = &blank

	img, err := pago.AdjuntarImagen(params)

	require.NoError(t, err)
	// Blank input normalizes to "not provided".
	assert.Nil(t, img.Descripcion())
}

func TestTrimOptionalBounded_TrimsWhitespace(t *testing.T) {
	t.Parallel()

	padded := "  comprobante de pago  "
	pago := newValidPago(t)
	params := adjuntarParams(t)
	params.Descripcion = &padded

	img, err := pago.AdjuntarImagen(params)

	require.NoError(t, err)
	require.NotNil(t, img.Descripcion())
	assert.Equal(t, "comprobante de pago", *img.Descripcion())
}
