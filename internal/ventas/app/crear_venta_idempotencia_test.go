//nolint:misspell // domain vocabulary is Spanish (ventas, contado, etc.) per project convention.
package app_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// Idempotency by the body's id, with no expiry.
//
// Ventas used to lean on the idempotency cache (24 h TTL, 2xx only). Past that
// window a retry of an already-created venta hit the primary key and surfaced
// as an unclassified 500 — which the phone's new decision table retries
// forever. crear_pago has had this guard since day one; ventas now matches it.
// See docs/module-standards/ENTREGA_GARANTIZADA.md.
func TestCrearVenta_Idempotencia(t *testing.T) {
	t.Parallel()

	t.Run("mismo_id_devuelve_la_venta_existente", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		by := uuid.New()
		in := validContadoInput()

		first, err := h.svc.CrearVenta(t.Context(), in, by)
		require.NoError(t, err)
		require.Equal(t, 1, h.ventas.SaveCalls)

		// The retry the phone sends after a timeout, days later.
		second, err := h.svc.CrearVenta(t.Context(), in, by)
		require.NoError(t, err, "a retry with the same id must not error")
		require.NotNil(t, second)

		assert.Equal(t, first.ID(), second.ID(), "the existing venta is returned")
		assert.Equal(t, 1, h.ventas.SaveCalls, "the retry must not insert a second row")
	})

	t.Run("no_emite_evento_dos_veces", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validContadoInput()

		_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.NoError(t, err)
		require.Equal(t, []string{domain.EventTypeVentaCreada}, h.outbox.eventTypes())

		_, err = h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.NoError(t, err)

		assert.Equal(
			t, []string{domain.EventTypeVentaCreada}, h.outbox.eventTypes(),
			"replaying a create must not emit VentaCreada twice",
		)
	})

	t.Run("id_distinto_sigue_creando", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)

		in1 := validContadoInput()
		_, err := h.svc.CrearVenta(t.Context(), in1, uuid.New())
		require.NoError(t, err)

		in2 := validContadoInput()
		in2.ID = uuid.New()
		_, err = h.svc.CrearVenta(t.Context(), in2, uuid.New())
		require.NoError(t, err)

		assert.Equal(t, 2, h.ventas.SaveCalls, "distinct ids are distinct ventas")
	})

	// A lookup that fails for a reason other than "not found" must not be read
	// as "does not exist" — that would insert and trip the PK.
	t.Run("error_de_lookup_se_propaga", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		boom := errors.New("firebird pool wedged")
		h.ventas.FindErr = boom

		_, err := h.svc.CrearVenta(t.Context(), validContadoInput(), uuid.New())

		require.ErrorIs(t, err, boom, "an indeterminate lookup must not be treated as a miss")
		assert.Zero(t, h.ventas.SaveCalls, "must not insert when existence is unknown")
	})
}
