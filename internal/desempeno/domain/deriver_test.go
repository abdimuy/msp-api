package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

var (
	ventanaInicio = time.Date(2026, 10, 9, 13, 46, 0, 0, time.UTC)
	ventanaFin    = time.Date(2026, 10, 14, 22, 40, 0, 0, time.UTC)
	dentro        = time.Date(2026, 10, 11, 16, 0, 0, 0, time.UTC)
)

func ventana(t *testing.T) domain.Ventana {
	t.Helper()
	v, err := domain.NuevaVentana(ventanaInicio, ventanaFin)
	require.NoError(t, err)
	return v
}

func ventaPtr(id int) *int { return &id }

func TestNuevaVentana_RechazaInvertida(t *testing.T) {
	t.Parallel()
	_, err := domain.NuevaVentana(ventanaFin, ventanaInicio)
	require.ErrorIs(t, err, domain.ErrParametroInvalido)
}

func TestVentana_InclusivaEnLosDosExtremos(t *testing.T) {
	t.Parallel()
	v := ventana(t)
	assert.True(t, v.Contiene(ventanaInicio))
	assert.True(t, v.Contiene(ventanaFin))
	assert.False(t, v.Contiene(ventanaInicio.Add(-time.Second)))
	assert.False(t, v.Contiene(ventanaFin.Add(time.Second)))
}

func TestEstadoPorDinero(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nombre, abono, parcialidad string
		estado                     domain.EstadoCuenta
		ok                         bool
	}{
		{"sin abono", "0", "150", "", false},
		{"abono negativo", "-5", "150", "", false},
		{"parcialidad cero", "50", "0", domain.EstadoAbonoParcial, true},
		{"parcialidad negativa", "50", "-1", domain.EstadoAbonoParcial, true},
		{"borde exacto con escalas distintas", "150", "150.00", domain.EstadoPago, true},
		{"un centavo menos", "149.99", "150.00", domain.EstadoAbonoParcial, true},
		{"más de la parcialidad", "300", "150", domain.EstadoPago, true},
	}
	for _, c := range casos {
		e, ok := domain.EstadoPorDinero(dinero(c.abono), dinero(c.parcialidad))
		assert.Equal(t, c.ok, ok, c.nombre)
		assert.Equal(t, c.estado, e, c.nombre)
	}
}

func TestDerivar_UnaEntradaPorCuentaSiempre(t *testing.T) {
	t.Parallel()
	cuentas := []domain.CuentaPeriodo{
		{VentaID: 1, ClienteID: 10, Parcialidad: dinero("150")},
		{VentaID: 2, ClienteID: 11, Parcialidad: dinero("200")},
	}
	d := domain.Derivar(cuentas, nil, nil, ventana(t))
	require.Len(t, d.PorVenta, 2)
	for _, r := range d.PorVenta {
		assert.Equal(t, domain.EstadoSinTocar, r.Estado)
		assert.False(t, r.ConAlgo)
		assert.Empty(t, r.DeterminadoPor)
		assert.Empty(t, r.UltimaActividadID)
	}
}

func TestDerivar_RechazadoNoEsDineroPeroSiCobertura(t *testing.T) {
	t.Parallel()
	cuentas := []domain.CuentaPeriodo{{VentaID: 1, ClienteID: 10, Parcialidad: dinero("150")}}
	pagos := []domain.PagoPeriodo{{
		VentaID: 1, Importe: dinero("150"), FormaCobroID: domain.FormaCobroEfectivo,
		FechaHora: dentro, Rechazado: true, ActividadID: "pago:7",
	}}
	r := domain.Derivar(cuentas, pagos, nil, ventana(t)).PorVenta[1]
	assert.Equal(t, domain.EstadoSinTocar, r.Estado)
	assert.True(t, r.AbonoVentana.IsZero())
	assert.True(t, r.ConAlgo)
	assert.Equal(t, "pago:7", r.UltimaActividadID)
	assert.Empty(t, r.DeterminadoPor)
}

func TestDerivar_DeterminadoPorYUltimaActividad(t *testing.T) {
	t.Parallel()
	cuentas := []domain.CuentaPeriodo{{VentaID: 1, ClienteID: 10, Parcialidad: dinero("150")}}
	pagos := []domain.PagoPeriodo{
		{VentaID: 1, Importe: dinero("100"), FormaCobroID: 157, FechaHora: dentro, ActividadID: "pago:1"},
		{VentaID: 1, Importe: dinero("50"), FormaCobroID: 52569, FechaHora: dentro.Add(time.Hour), ActividadID: "pago:2"},
	}
	visitas := []domain.VisitaPeriodo{{
		ClienteID: 10, TipoVisita: domain.TipoVisitaNoSeEncontraba,
		FechaHora: dentro.Add(2 * time.Hour), ActividadID: "visita:a",
	}}
	r := domain.Derivar(cuentas, pagos, visitas, ventana(t)).PorVenta[1]
	assert.Equal(t, domain.EstadoPago, r.Estado, "100 + 50 = parcialidad exacta")
	assert.Equal(t, "pago:2", r.DeterminadoPor, "el pago de dinero más reciente")
	assert.Equal(t, "visita:a", r.UltimaActividadID, "lo más reciente de todo")
	assert.Equal(t, dentro.Add(2*time.Hour), r.UltimaActividadHora)
}

func TestDerivar_Incidencias(t *testing.T) {
	t.Parallel()
	cuentas := []domain.CuentaPeriodo{
		{VentaID: 1, ClienteID: 10, Parcialidad: dinero("0")},
		{VentaID: 2, ClienteID: 10, Parcialidad: dinero("0")},
	}
	pagos := []domain.PagoPeriodo{{VentaID: 1, Importe: dinero("50"), FormaCobroID: 157, FechaHora: dentro}}
	visitas := []domain.VisitaPeriodo{
		{ClienteID: 10, TipoVisita: "texto libre", FechaHora: dentro, VentaID: ventaPtr(2)},
		{ClienteID: 10, TipoVisita: domain.TipoVisitaNoVaADarPago, FechaHora: dentro},
		{ClienteID: 10, TipoVisita: "otro texto", FechaHora: ventanaInicio.Add(-time.Hour)},
	}
	d := domain.Derivar(cuentas, pagos, visitas, ventana(t))
	assert.Equal(t, domain.Incidencias{
		TipoVisitaFueraDeCatalogo: 1, // la de fuera de ventana no se cuenta
		ParcialidadNoPositiva:     1, // solo la que tuvo abono
		VisitaSinVenta:            1,
	}, d.Incidencias)
	assert.Equal(t, domain.EstadoAbonoParcial, d.PorVenta[1].Estado)
	assert.Equal(t, domain.EstadoVisiteVuelvo, d.PorVenta[2].Estado)
}

func TestDerivar_UltimaActividadEsElPagoSiEsPosterior(t *testing.T) {
	t.Parallel()
	v := ventana(t)
	assert.Equal(t, ventanaInicio, v.Inicio())
	assert.Equal(t, ventanaFin, v.Fin())
	cuentas := []domain.CuentaPeriodo{{VentaID: 1, ClienteID: 10, Parcialidad: dinero("150")}}
	visitas := []domain.VisitaPeriodo{{
		ClienteID: 10, VentaID: ventaPtr(1), TipoVisita: domain.TipoVisitaFueGrosero,
		FechaHora: dentro, ActividadID: "visita:a",
	}}
	pagos := []domain.PagoPeriodo{{
		VentaID: 1, Importe: dinero("50"), FormaCobroID: domain.FormaCobroCondonacion,
		FechaHora: dentro.Add(time.Hour), ActividadID: "pago:9",
	}}
	r := domain.Derivar(cuentas, pagos, visitas, v).PorVenta[1]
	assert.Equal(t, domain.EstadoSeNego, r.Estado, "la condonación no tapa la visita")
	assert.Equal(t, "visita:a", r.DeterminadoPor)
	assert.Equal(t, "pago:9", r.UltimaActividadID, "la condonación es lo más reciente")
}
