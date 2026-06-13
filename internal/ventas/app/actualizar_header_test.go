//nolint:misspell // ventas vocabulary is Spanish per project convention.
package app_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

func TestActualizarHeader_Happy(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	id := h.seedVenta(t)

	out, err := h.svc.ActualizarHeader(t.Context(), ventasapp.ActualizarHeaderInput{
		VentaID:    *id,
		Calle:      "Av. Nueva",
		Colonia:    "Centro",
		Poblacion:  "Cd.",
		Ciudad:     "CDMX",
		Latitud:    19.5,
		Longitud:   -99.5,
		FechaVenta: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	}, uuid.New())
	require.NoError(t, err)
	// Address text is folded to ALL CAPS by the domain (Microsip convention).
	assert.Equal(t, "AV. NUEVA", out.Direccion().Calle())
}

func TestActualizarHeader_VentaNotFound(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, err := h.svc.ActualizarHeader(t.Context(), ventasapp.ActualizarHeaderInput{
		VentaID: uuid.New(), Calle: "X", Colonia: "X", Poblacion: "X", Ciudad: "X",
		FechaVenta: time.Now(),
	}, uuid.New())
	require.ErrorIs(t, err, domain.ErrVentaNotFound)
}

func TestActualizarHeader_RejectsCancelada(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	id := h.seedVenta(t)
	_, err := h.svc.CancelarVenta(t.Context(), *id, "razon", uuid.New())
	require.NoError(t, err)

	_, err = h.svc.ActualizarHeader(t.Context(), ventasapp.ActualizarHeaderInput{
		VentaID: *id, Calle: "X", Colonia: "X", Poblacion: "X", Ciudad: "X",
		FechaVenta: time.Now(),
	}, uuid.New())
	require.ErrorIs(t, err, domain.ErrVentaNoEditable)
}
