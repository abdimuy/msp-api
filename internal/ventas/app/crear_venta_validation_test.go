//nolint:misspell // domain vocabulary is Spanish (ventas, productos, etc.) per project convention.
package app_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// TestCrearVentaInputValidation exercises every branch of intoDomain so the
// per-VO constructors are observed under the service entry point.
func TestCrearVentaInputValidation(t *testing.T) {
	t.Parallel()

	t.Run("invalid_gps_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validContadoInput()
		in.Latitud = 200

		_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.ErrorIs(t, err, domain.ErrGPSLatitudInvalida)
	})

	t.Run("invalid_direccion_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validContadoInput()
		in.Calle = ""

		_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.ErrorIs(t, err, domain.ErrCalleRequerida)
	})

	t.Run("invalid_cliente_nombre_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validContadoInput()
		in.ClienteNombre = ""

		_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.ErrorIs(t, err, domain.ErrNombreClienteRequerido)
	})

	t.Run("invalid_telefono_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validContadoInput()
		bad := "not-a-phone"
		in.ClienteTel = &bad

		_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.Error(t, err)
	})

	t.Run("valid_telefono_accepted", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validContadoInput()
		tel := "5512345678"
		in.ClienteTel = &tel

		venta, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.NoError(t, err)
		require.NotNil(t, venta.Cliente().Telefono())
		assert.Equal(t, "5512345678", venta.Cliente().Telefono().Value())
	})

	t.Run("blank_telefono_treated_as_absent", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validContadoInput()
		blank := ""
		in.ClienteTel = &blank

		venta, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.NoError(t, err)
		assert.Nil(t, venta.Cliente().Telefono())
	})

	t.Run("valid_aval_accepted", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validContadoInput()
		aval := "Maria Aval"
		in.ClienteAval = &aval

		venta, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.NoError(t, err)
		require.NotNil(t, venta.Cliente().Aval())
	})

	t.Run("blank_aval_treated_as_absent", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validContadoInput()
		blank := ""
		in.ClienteAval = &blank

		venta, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.NoError(t, err)
		assert.Nil(t, venta.Cliente().Aval())
	})

	t.Run("invalid_aval_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validContadoInput()
		// build a long aval that exceeds 200 chars but is also non-empty.
		bad := strings.Repeat("x", 250)
		in.ClienteAval = &bad

		_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.ErrorIs(t, err, domain.ErrNombreClienteDemasiadoLargo)
	})

	t.Run("negative_monto_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validContadoInput()
		in.PrecioAnual = decimal.NewFromInt(-1)

		_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.ErrorIs(t, err, domain.ErrMontoNegativo)
	})

	t.Run("invalid_frec_pago_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validCreditoInput()
		in.PlanCredito.FrecPago = "BOGUS"

		_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.ErrorIs(t, err, domain.ErrFrecPagoInvalida)
	})

	t.Run("plan_credito_invalid_plazo_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validCreditoInput()
		in.PlanCredito.PlazoMeses = 0

		_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.ErrorIs(t, err, domain.ErrPlazoNoPositivo)
	})

	t.Run("dia_cobranza_semanal_accepted", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validCreditoInput()
		in.PlanCredito.FrecPago = "SEMANAL"
		lunes := "LUNES"
		in.DiaCobranza = &app.CrearVentaDiaCobranzaInput{Semana: &lunes}

		venta, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.NoError(t, err)
		require.NotNil(t, venta.DiaCobranza())
		assert.True(t, venta.DiaCobranza().IsSemana())
	})

	t.Run("dia_cobranza_invalid_semana_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validCreditoInput()
		in.PlanCredito.FrecPago = "SEMANAL"
		bogus := "BOGUS"
		in.DiaCobranza = &app.CrearVentaDiaCobranzaInput{Semana: &bogus}

		_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.ErrorIs(t, err, domain.ErrDiaSemanaInvalido)
	})

	t.Run("dia_cobranza_invalid_mes_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validCreditoInput()
		out := 42
		in.DiaCobranza = &app.CrearVentaDiaCobranzaInput{Mes: &out}

		_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.ErrorIs(t, err, domain.ErrDiaMesInvalido)
	})

	t.Run("dia_cobranza_both_semana_and_mes_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validCreditoInput()
		lunes := "LUNES"
		mes := 5
		in.DiaCobranza = &app.CrearVentaDiaCobranzaInput{Semana: &lunes, Mes: &mes}

		_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.ErrorIs(t, err, domain.ErrDiaCobranzaIncoherenteQuincenalMensual)
	})

	t.Run("dia_cobranza_empty_struct_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validCreditoInput()
		in.DiaCobranza = &app.CrearVentaDiaCobranzaInput{}

		_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.ErrorIs(t, err, domain.ErrDiaCobranzaIncoherenteQuincenalMensual)
	})

	t.Run("producto_negative_price_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validContadoInput()
		in.Productos[0].PrecioAnual = decimal.NewFromInt(-1)

		_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.ErrorIs(t, err, domain.ErrMontoNegativo)
	})

	t.Run("combo_carried_through", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validContadoInput()
		comboID := uuid.New()
		in.Combos = []app.CrearVentaComboInput{{
			ID:            comboID,
			Nombre:        "Promo enero",
			PrecioAnual:   decimal.NewFromInt(900),
			PrecioCorto:   decimal.NewFromInt(850),
			PrecioContado: decimal.NewFromInt(800),
		}}
		in.Productos[0].ComboID = &comboID

		venta, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.NoError(t, err)
		assert.Equal(t, 1, venta.CombosCount())
	})

	t.Run("vendedor_invalid_email_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validContadoInput()
		in.Vendedores[0].Email = ""

		_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.ErrorIs(t, err, domain.ErrVendedorEmailRequerido)
	})

	t.Run("missing_vendedores_rejected", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		in := validContadoInput()
		in.Vendedores = nil

		_, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
		require.ErrorIs(t, err, domain.ErrVentaVendedoresVacios)
	})
}
