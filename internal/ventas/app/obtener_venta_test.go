//nolint:misspell // domain vocabulary is Spanish (ventas) per project convention.
package app_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

func TestObtenerVenta(t *testing.T) {
	t.Parallel()

	t.Run("happy_path_returns_aggregate", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)

		got, err := h.svc.ObtenerVenta(t.Context(), *ventaID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, *ventaID, got.ID())
	})

	t.Run("not_found_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)

		_, err := h.svc.ObtenerVenta(t.Context(), uuid.New())
		require.ErrorIs(t, err, domain.ErrVentaNotFound)
	})

	t.Run("repo_error_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		boom := errors.New("repo down")
		h.ventas.FindErr = boom

		_, err := h.svc.ObtenerVenta(t.Context(), uuid.New())
		require.ErrorIs(t, err, boom)
	})
}

// seedVentaConCliente creates and persists a CONTADO venta with a Microsip
// clienteID (47913) and zona_cliente_id (21563) via the service. Returns the
// seeded venta (not just the ID) so callers can pass it to ZonaMicrosipDeVenta.
func seedVentaConCliente(t *testing.T, h *testHarness) *domain.Venta {
	t.Helper()
	in := validContadoInput()
	cid := 47913
	zona := 21563
	in.ClienteID = &cid
	in.ZonaClienteID = &zona
	by := uuid.New()
	v, err := h.svc.CrearVenta(t.Context(), in, by)
	require.NoError(t, err)
	return v
}

func TestZonaMicrosipDeVenta(t *testing.T) {
	t.Parallel()

	// ventaZona is the zona_cliente_id stored on the seeded venta (21563).
	const ventaZona = 21563
	// otherZona is a different zona used to trigger mismatch.
	const otherZona = 99999

	t.Run("zonaReader_nil_returns_nil_false", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		// no WithZonaReader — reader is nil
		v := seedVentaConCliente(t, h)

		zm, mismatch, err := h.svc.ZonaMicrosipDeVenta(t.Context(), v)
		require.NoError(t, err)
		assert.Nil(t, zm)
		assert.False(t, mismatch)
	})

	t.Run("cliente_nil_returns_nil_false", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.svc = h.svc.WithZonaReader(newFakeClienteZonaReader(ventaZona))
		// Seed a venta WITHOUT a clienteID using standard seedVenta helper.
		ventaID := h.seedVenta(t)
		v, err := h.svc.ObtenerVenta(t.Context(), *ventaID)
		require.NoError(t, err)

		zm, mismatch, err := h.svc.ZonaMicrosipDeVenta(t.Context(), v)
		require.NoError(t, err)
		assert.Nil(t, zm)
		assert.False(t, mismatch)
	})

	t.Run("microsip_zona_nil_returns_nil_false", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		r := newFakeClienteZonaReader(0)
		r.ZonaNil = true
		h.svc = h.svc.WithZonaReader(r)
		v := seedVentaConCliente(t, h)

		zm, mismatch, err := h.svc.ZonaMicrosipDeVenta(t.Context(), v)
		require.NoError(t, err)
		assert.Nil(t, zm)
		assert.False(t, mismatch)
	})

	t.Run("cliente_not_found_returns_nil_false_no_error", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		r := newFakeClienteZonaReader(0)
		r.Err = domain.ErrClienteNotFoundInMicrosip
		h.svc = h.svc.WithZonaReader(r)
		v := seedVentaConCliente(t, h)

		zm, mismatch, err := h.svc.ZonaMicrosipDeVenta(t.Context(), v)
		require.NoError(t, err, "ErrClienteNotFoundInMicrosip must NOT propagate — degrade gracefully")
		assert.Nil(t, zm)
		assert.False(t, mismatch)
	})

	t.Run("reader_error_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		boom := errors.New("microsip unavailable")
		r := newFakeClienteZonaReader(0)
		r.Err = boom
		h.svc = h.svc.WithZonaReader(r)
		v := seedVentaConCliente(t, h)

		_, _, err := h.svc.ZonaMicrosipDeVenta(t.Context(), v)
		require.ErrorIs(t, err, boom)
	})

	t.Run("match_returns_zonaID_false", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		// Reader returns same zona as the venta (21563).
		h.svc = h.svc.WithZonaReader(newFakeClienteZonaReader(ventaZona))
		v := seedVentaConCliente(t, h)

		zm, mismatch, err := h.svc.ZonaMicrosipDeVenta(t.Context(), v)
		require.NoError(t, err)
		require.NotNil(t, zm)
		assert.Equal(t, ventaZona, *zm)
		assert.False(t, mismatch)
	})

	t.Run("mismatch_returns_zonaID_true", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		// Reader returns a different zona from the venta's (21563 vs 99999).
		h.svc = h.svc.WithZonaReader(newFakeClienteZonaReader(otherZona))
		v := seedVentaConCliente(t, h)

		zm, mismatch, err := h.svc.ZonaMicrosipDeVenta(t.Context(), v)
		require.NoError(t, err)
		require.NotNil(t, zm)
		assert.Equal(t, otherZona, *zm)
		assert.True(t, mismatch)
	})

	t.Run("venta_zona_nil_microsip_has_zona_returns_false", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.svc = h.svc.WithZonaReader(newFakeClienteZonaReader(ventaZona))

		// Build a venta with clienteID but NO zona on the direccion.
		in := validContadoInput()
		cid := 47913
		in.ClienteID = &cid
		// ZonaClienteID intentionally left nil.
		by := uuid.New()
		v, err := h.svc.CrearVenta(t.Context(), in, by)
		require.NoError(t, err)

		zm, mismatch, err := h.svc.ZonaMicrosipDeVenta(t.Context(), v)
		require.NoError(t, err)
		require.NotNil(t, zm)
		assert.Equal(t, ventaZona, *zm)
		assert.False(t, mismatch, "venta zona nil must never trigger mismatch")
	})
}
