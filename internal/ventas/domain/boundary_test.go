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

// ─── A1: String length boundary tests ──────────────────────────────────────
//
// All length checks in the domain use len(s) (byte width), not utf8.RuneCount.
// These tests pin that contract: ASCII strings at the exact byte limit are
// accepted, one extra byte is rejected.

func TestNombreCliente_BoundaryLengths(t *testing.T) {
	t.Parallel()

	_, err := domain.NewNombreCliente(strings.Repeat("a", 200))
	require.NoError(t, err, "200 ASCII bytes must be accepted")

	_, err = domain.NewNombreCliente(strings.Repeat("a", 201))
	require.Error(t, err, "201 ASCII bytes must be rejected")
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "nombre_cliente_too_long", ae.Code)

	// Post migration 000005 (UTF8 columns), the limit is measured in Unicode
	// codepoints, not bytes — matching CHARACTER SET UTF8(N) column semantics.
	// "é" is 1 codepoint regardless of UTF-8 byte length, so 200 é's must be
	// accepted and 201 must be rejected.
	_, err = domain.NewNombreCliente(strings.Repeat("é", 200))
	require.NoError(t, err, "200 codepoints must be accepted regardless of byte width")

	_, err = domain.NewNombreCliente(strings.Repeat("é", 201))
	require.Error(t, err, "201 codepoints must be rejected")
	ae, ok = apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "nombre_cliente_too_long", ae.Code)
}

func TestProductoArticulo_BoundaryLengths(t *testing.T) {
	t.Parallel()
	mk := func(t *testing.T, articulo string) error {
		t.Helper()
		p := validCrearVentaParams(t)
		p.Productos[0].Articulo = articulo
		_, err := domain.CrearVenta(p)
		return err
	}

	require.NoError(t, mk(t, strings.Repeat("a", 200)), "200 bytes must be accepted")

	err := mk(t, strings.Repeat("a", 201))
	require.Error(t, err, "201 bytes must be rejected")
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "producto_articulo_too_long", ae.Code)
}

func TestNota_BoundaryLengths(t *testing.T) {
	t.Parallel()
	mk := func(t *testing.T, nota string) error {
		t.Helper()
		p := validCrearVentaParams(t)
		s := nota
		p.Nota = &s
		_, err := domain.CrearVenta(p)
		return err
	}

	require.NoError(t, mk(t, strings.Repeat("a", 500)), "500 bytes must be accepted (exact boundary)")

	err := mk(t, strings.Repeat("a", 501))
	require.Error(t, err, "501 bytes must be rejected")
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "nota_too_long", ae.Code)
}

func TestComboNombre_BoundaryLengths(t *testing.T) {
	t.Parallel()
	mk := func(t *testing.T, nombre string) error {
		t.Helper()
		p := validCrearVentaParams(t)
		p.Combos = []domain.CrearVentaComboInput{{
			ID:             uuid.New(),
			Nombre:         nombre,
			Precios:        p.Montos,
			Cantidad:       decimal.NewFromInt(1),
			AlmacenOrigen:  1,
			AlmacenDestino: 2,
		}}
		_, err := domain.CrearVenta(p)
		return err
	}

	require.NoError(t, mk(t, strings.Repeat("a", 200)), "200 bytes must be accepted")

	err := mk(t, strings.Repeat("a", 201))
	require.Error(t, err, "201 bytes must be rejected")
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "combo_nombre_too_long", ae.Code)
}

func TestVendedorEmail_BoundaryLengths(t *testing.T) {
	t.Parallel()
	// 255 bytes accepted, 256 rejected. Build the email so it still looks
	// structurally like an email (no SMTP validation in the domain — just
	// length and required).
	email255 := strings.Repeat("a", 255-len("@x.com")) + "@x.com"
	require.Len(t, email255, 255)

	_, err := domain.NewVendedorSnapshot(domain.NewVendedorSnapshotParams{
		UsuarioID: uuid.New(),
		Email:     email255,
		Nombre:    "V",
	})
	require.NoError(t, err, "255 bytes must be accepted")

	email256 := strings.Repeat("a", 256-len("@x.com")) + "@x.com"
	require.Len(t, email256, 256)
	_, err = domain.NewVendedorSnapshot(domain.NewVendedorSnapshotParams{
		UsuarioID: uuid.New(),
		Email:     email256,
		Nombre:    "V",
	})
	require.Error(t, err, "256 bytes must be rejected")
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "vendedor_email_too_long", ae.Code)
}

func TestVendedorNombre_BoundaryLengths(t *testing.T) {
	t.Parallel()

	_, err := domain.NewVendedorSnapshot(domain.NewVendedorSnapshotParams{
		UsuarioID: uuid.New(),
		Email:     "v@x.com",
		Nombre:    strings.Repeat("a", 200),
	})
	require.NoError(t, err, "200 bytes must be accepted")

	_, err = domain.NewVendedorSnapshot(domain.NewVendedorSnapshotParams{
		UsuarioID: uuid.New(),
		Email:     "v@x.com",
		Nombre:    strings.Repeat("a", 201),
	})
	require.Error(t, err, "201 bytes must be rejected")
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "vendedor_nombre_too_long", ae.Code)
}

func TestCancelReason_BoundaryLengths(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	by := uuid.New()

	_, err := domain.NewCancelacion(at, by, strings.Repeat("a", 500))
	require.NoError(t, err, "500 bytes must be accepted (exact boundary)")

	_, err = domain.NewCancelacion(at, by, strings.Repeat("a", 501))
	require.Error(t, err, "501 bytes must be rejected")
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "reason_cancelacion_too_long", ae.Code)
}

// ─── A2: GPS boundary values ───────────────────────────────────────────────

func TestGPS_BoundaryValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		lat, lng float64
		errCode  string // "" → expect success
	}{
		{"south pole exact", -90, 0, ""},
		{"north pole exact", 90, 0, ""},
		{"west antimeridian exact", 0, -180, ""},
		{"east antimeridian exact", 0, 180, ""},
		{"origin (null island)", 0, 0, ""},
		{"all corners SW", -90, -180, ""},
		{"all corners NE", 90, 180, ""},
		{"lat just below min", -90.0001, 0, "gps_latitud_invalida"},
		{"lat just above max", 90.0001, 0, "gps_latitud_invalida"},
		{"lng just below min", 0, -180.0001, "gps_longitud_invalida"},
		{"lng just above max", 0, 180.0001, "gps_longitud_invalida"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewGPSCoords(tc.lat, tc.lng)
			if tc.errCode == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			ae, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, tc.errCode, ae.Code)
		})
	}
}
