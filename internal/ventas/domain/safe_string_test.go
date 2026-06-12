//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// Post migration 000005 the MSP_* text columns are CHARACTER SET UTF8. The
// domain guard accepts any valid UTF-8 character set — accents, emoji, CJK,
// em-dash, smart quotes — and only rejects NUL bytes and ASCII control chars
// other than tab/CR/LF. NFC normalization is applied so canonically-equivalent
// inputs compare equal downstream.

func TestNewNombreCliente_AcceptsEmoji(t *testing.T) {
	t.Parallel()
	n, err := domain.NewNombreCliente("José 🎉")
	require.NoError(t, err)
	// Folded to ALL CAPS (Microsip convention); emoji is left untouched.
	assert.Equal(t, "JOSÉ 🎉", n.Value())
}

func TestNewNombreCliente_AcceptsEmDash(t *testing.T) {
	t.Parallel()
	n, err := domain.NewNombreCliente("Cliente — Histórico")
	require.NoError(t, err)
	// Folded to ALL CAPS (Microsip convention); em-dash is left untouched.
	assert.Equal(t, "CLIENTE — HISTÓRICO", n.Value())
}

func TestNewNombreCliente_AcceptsExtendedLatin(t *testing.T) {
	t.Parallel()
	_, err := domain.NewNombreCliente("Müller Ñoño Pérez")
	require.NoError(t, err)
}

func TestNewNombreCliente_RejectsNUL(t *testing.T) {
	t.Parallel()
	_, err := domain.NewNombreCliente("José\x00Pérez")
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "string_unsafe_chars", ae.Code)
}

func TestNewNombreCliente_RejectsAsciiControl(t *testing.T) {
	t.Parallel()
	_, err := domain.NewNombreCliente("José\x01Pérez")
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "string_unsafe_chars", ae.Code)
}

func TestNewNombreCliente_NormalizesNFC(t *testing.T) {
	t.Parallel()
	// composed (NFC): "Jos" + U+00E9 + " P" + U+00E9 + "rez"
	// decomposed (NFD): "Jos" + "e" + U+0301 + " P" + "e" + U+0301 + "rez"
	composed := "José Pérez"
	decomposed := "José Pérez"
	require.NotEqual(t, composed, decomposed, "fixtures must differ byte-wise")

	a, err := domain.NewNombreCliente(decomposed)
	require.NoError(t, err)
	b, err := domain.NewNombreCliente(composed)
	require.NoError(t, err)
	assert.Equal(t, b.Value(), a.Value(), "decomposed input must collapse to NFC")
}

func TestNewCancelacion_AcceptsEmoji(t *testing.T) {
	t.Parallel()
	_, err := domain.NewCancelacion(timeFixed(), uuid.New(), "cancel 🚨 — motivo")
	require.NoError(t, err)
}

func TestNewVendedorSnapshot_AcceptsEmoji(t *testing.T) {
	t.Parallel()
	_, err := domain.NewVendedorSnapshot(domain.NewVendedorSnapshotParams{
		UsuarioID: uuid.New(),
		Email:     "v@x.com",
		Nombre:    "Vendedor 🚀",
	})
	require.NoError(t, err)
}

func TestNewVendedorSnapshot_RejectsNULInEmail(t *testing.T) {
	t.Parallel()
	_, err := domain.NewVendedorSnapshot(domain.NewVendedorSnapshotParams{
		UsuarioID: uuid.New(),
		Email:     "v\x00@x.com",
		Nombre:    "V",
	})
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "string_unsafe_chars", ae.Code)
}

func TestNewDireccion_AcceptsEmoji(t *testing.T) {
	t.Parallel()
	p := domain.NewDireccionParams{
		Calle: "Av. 🌮", Colonia: "C", Poblacion: "P", Ciudad: "CDMX",
	}
	_, err := domain.NewDireccion(p)
	require.NoError(t, err)
}

func TestCrearVenta_AcceptsProductoArticuloEmoji(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	p.Productos[0].Articulo = "Bici 🚴"
	_, err := domain.CrearVenta(p)
	require.NoError(t, err)
}

func TestCrearVenta_AcceptsComboNombreEmoji(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	p.Combos = []domain.CrearVentaComboInput{{
		ID:             uuid.New(),
		Nombre:         "Combo 🎉",
		Precios:        p.Montos,
		Cantidad:       decimal.NewFromInt(1),
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
	}}
	_, err := domain.CrearVenta(p)
	require.NoError(t, err)
}

func TestCrearVenta_AcceptsNotaWithEmojiAndEmDash(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	nota := "nota con 🚀 — entrega importante"
	p.Nota = &nota
	_, err := domain.CrearVenta(p)
	require.NoError(t, err)
}

func TestCrearVenta_RejectsNotaWithNUL(t *testing.T) {
	t.Parallel()
	p := validCrearVentaParams(t)
	bad := "nota válida\x00con NUL"
	p.Nota = &bad
	_, err := domain.CrearVenta(p)
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "string_unsafe_chars", ae.Code)
}

// timeFixed returns a deterministic time for tests that don't care about
// the exact instant.
func timeFixed() time.Time { return time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC) }

// Compile-time check: keep strings imported even when no test references it.
var _ = strings.Builder{}
