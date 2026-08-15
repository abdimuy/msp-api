//nolint:misspell // ventas vocabulary is Spanish (contado, credito, zona) per project convention.
package app_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// seedAprobadaContadoSinZona (aplicar_venta_test.go) builds an approved
// CONTADO venta with no zona_cliente_id — the shape the backlog is made of.

// With the flag OFF a CONTADO venta with no zona still applies, through the
// fixed mostrador caja from MSP_CFG_APLICAR. This is the backlog path and it
// must keep working until the backlog is zero.
func TestAplicarVenta_ZonaObligatoriaOff_ContadoSinZonaAplica(t *testing.T) {
	t.Parallel()
	h, cfg, writer, _ := newAplicarHarness(t)
	id := seedAprobadaContadoSinZona(t, h)

	v, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.NoError(t, err, "the backlog path must keep draining while the flag is off")
	require.Equal(t, 1, writer.callsCount())
	assert.Equal(t, domain.SincronizacionAplicada, v.Sincronizacion())

	// It used the mostrador caja, not the zona mapping.
	assert.Equal(t, cfg.defs.CajaContadoID, writer.lastInput().CajaID)
	assert.Equal(t, cfg.defs.CajeroContadoID, writer.lastInput().CajeroID)
}

// With the flag ON the same venta is rejected before anything is written.
func TestAplicarVenta_ZonaObligatoriaOn_ContadoSinZonaRechazada(t *testing.T) {
	t.Parallel()
	h, _, writer, _ := newAplicarHarness(t)
	h.svc.WithZonaObligatoria(true)
	id := seedAprobadaContadoSinZona(t, h)

	_, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.ErrorIs(t, err, domain.ErrVentaSinZona, "zona is required for every tipo de venta")
	assert.Equal(t, 0, writer.callsCount(), "nothing may be written to Microsip")
}

// With the flag ON a CONTADO venta WITH a zona resolves through
// MSP_CFG_ZONA_CAJA — not through the mostrador defaults.
func TestAplicarVenta_ZonaObligatoriaOn_ContadoConZonaUsaLaCajaDeLaZona(t *testing.T) {
	t.Parallel()
	h, cfg, writer, _ := newAplicarHarness(t)
	h.svc.WithZonaObligatoria(true)
	id := seedAprobadaContado(t, h) // seeds zona 21563

	_, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())
	require.NoError(t, err)

	require.Equal(t, 1, writer.callsCount())
	in := writer.lastInput()
	assert.Equal(t, cfg.cc.CajaID, in.CajaID, "caja must come from the zona mapping")
	assert.Equal(t, cfg.cc.CajeroID, in.CajeroID, "cajero must come from the zona mapping")
	assert.NotEqual(t, cfg.defs.CajaContadoID, in.CajaID, "must NOT fall back to the mostrador caja")
}

// A zona with no row in MSP_CFG_ZONA_CAJA fails with zona_sin_caja rather than
// silently posting somewhere else. This is the MEDIO MAYOREO case measured in
// production: an active zona with clients and no configured caja.
func TestAplicarVenta_ZonaObligatoriaOn_ZonaSinCajaFalla(t *testing.T) {
	t.Parallel()
	h, cfg, writer, _ := newAplicarHarness(t)
	h.svc.WithZonaObligatoria(true)
	cfg.setCajaCajeroErr(domain.ErrZonaSinCaja)
	id := seedAprobadaContado(t, h)

	_, err := h.svc.AplicarVenta(t.Context(), id, uuid.New())

	require.ErrorIs(t, err, domain.ErrZonaSinCaja)
	assert.Equal(t, 0, writer.callsCount())
}

// CREDITO keeps requiring a zona regardless of the flag.
func TestAplicarVenta_ZonaObligatoriaOn_CreditoSinZonaSigueRechazada(t *testing.T) {
	t.Parallel()
	h, _, writer, _ := newAplicarHarness(t)
	h.svc.WithZonaObligatoria(true)

	base := validCreditoInput()
	cid := 47913
	base.ClienteID = &cid
	by := uuid.New()
	v, err := h.svc.CrearVenta(t.Context(), base, by)
	require.NoError(t, err)
	_, err = h.svc.EnviarARevision(t.Context(), v.ID(), by)
	require.NoError(t, err)
	_, err = h.svc.Aprobar(t.Context(), v.ID(), by)
	require.NoError(t, err)

	_, err = h.svc.AplicarVenta(t.Context(), v.ID(), uuid.New())

	require.ErrorIs(t, err, domain.ErrVentaSinZona)
	assert.Equal(t, 0, writer.callsCount())
}
