package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func TestClasificarTipoPago(t *testing.T) {
	t.Parallel()
	assert.Equal(t, domain.TipoCondonacion, domain.ClasificarTipoPago(137026, 87327))
	assert.Equal(t, domain.TipoCondonacion, domain.ClasificarTipoPago(157, 27969))
	assert.Equal(t, domain.TipoPago, domain.ClasificarTipoPago(157, 87327))
	assert.Equal(t, domain.TipoPago, domain.ClasificarTipoPago(0, 87327))
}

func TestActividad_ConGPS(t *testing.T) {
	t.Parallel()
	assert.False(t, domain.Actividad{}.ConGPS(), "sin coordenadas")
	assert.False(t, domain.Actividad{Lat: f64(0), Lng: f64(0)}.ConGPS(), "centinela 0,0")
	assert.False(t, domain.Actividad{Lat: f64(18.46)}.ConGPS(), "solo latitud")
	assert.True(t, domain.Actividad{Lat: f64(18.46), Lng: f64(-97.39)}.ConGPS())
	assert.True(t, domain.Actividad{Lat: f64(0), Lng: f64(-97.39)}.ConGPS(), "solo uno en cero sí es dato")
}

func TestActividad_DineroYNoAplicado(t *testing.T) {
	t.Parallel()
	pago := func(aplicado, rechazado bool) domain.Actividad {
		return domain.Actividad{Tipo: domain.TipoPago, Pago: &domain.DatosPago{
			Importe: dinero("150"), FormaCobroID: domain.FormaCobroEfectivo, Aplicado: aplicado, Rechazado: rechazado,
		}}
	}
	assert.True(t, pago(true, false).CuentaComoDinero())
	assert.False(t, pago(true, false).NoAplicado())
	assert.False(t, pago(false, false).CuentaComoDinero())
	assert.True(t, pago(false, false).NoAplicado())
	assert.False(t, pago(false, true).CuentaComoDinero())
	assert.True(t, pago(false, true).NoAplicado())
	otra := pago(true, false)
	otra.Pago.FormaCobroID = 999
	assert.False(t, otra.CuentaComoDinero(), "forma desconocida no es dinero (contrato §4)")
	cond := domain.Actividad{Tipo: domain.TipoCondonacion, Pago: &domain.DatosPago{Importe: dinero("150"), Aplicado: true}}
	assert.False(t, cond.CuentaComoDinero(), "la condonación no es dinero")
	assert.False(t, cond.NoAplicado())
	assert.True(t, domain.Actividad{Tipo: domain.TipoVisita}.Importe().IsZero())
}

func TestIDs(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "pago:15082369", domain.IDPagoAplicado(15082369))
	assert.Equal(t, "pago:7a1f39e4-0000-0000-0000-000000000000", domain.IDPagoPendiente("7a1f39e4-0000-0000-0000-000000000000"))
	assert.Equal(t, "visita:412cc2fc", domain.IDVisita("412cc2fc"))
}

func TestActividad_CapturaTardia(t *testing.T) {
	t.Parallel()
	h := time.Date(2026, 10, 14, 16, 0, 0, 0, time.UTC)
	en := func(d time.Duration) *time.Time { r := h.Add(d); return &r }
	assert.False(t, domain.Actividad{Hora: h}.CapturaTardia(), "sin recibido_at")
	assert.False(t, domain.Actividad{Hora: h, RecibidoAt: en(2*time.Hour - time.Second)}.CapturaTardia())
	assert.True(t, domain.Actividad{Hora: h, RecibidoAt: en(2 * time.Hour)}.CapturaTardia(), "2 h exactas")
}
