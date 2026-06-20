//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
)

// helpers

func mustDecimal(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic("bad decimal: " + s)
	}
	return d
}

func noRango() domain.RangoFechasRitmo { return domain.RangoFechasRitmo{} }

func monday(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// ─── Sin actividad ─────────────────────────────────────────────────────────────

func TestBuildRitmoPago_SinActividad(t *testing.T) {
	t.Parallel()

	ahora := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	saldo := decimal.NewFromInt(0)

	r := domain.BuildRitmoPago(nil, nil, saldo, ahora, noRango())

	assert.Equal(t, time.Monday, r.AnclaDiaRuta, "sin pagos → ancla=lunes")
	assert.Empty(t, r.Semanas, "sin actividad → Semanas vacío")
	assert.Empty(t, r.Eventos, "sin actividad → Eventos vacío")
	assert.True(t, decimal.Zero.Equal(r.Resumen.TotalAbonado))
	assert.Equal(t, 0, r.Resumen.SemanasConPago)
	assert.Equal(t, 0, r.Resumen.SemanasActivas)
	assert.Equal(t, 0, r.Resumen.RachaActualSem)
	assert.True(t, decimal.Zero.Equal(r.Resumen.ConstanciaPct))
	assert.True(t, decimal.Zero.Equal(r.Resumen.SaldoActual))
}

func TestBuildRitmoPago_SinActividad_SaldoNegativoClamp(t *testing.T) {
	t.Parallel()
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(nil, nil, decimal.NewFromInt(-500), ahora, noRango())
	assert.True(t, decimal.Zero.Equal(r.Resumen.SaldoActual), "saldo negativo clamp a 0")
}

// ─── Día ancla (moda) ──────────────────────────────────────────────────────────

func TestBuildRitmoPago_AnclaDiaRuta_Moda(t *testing.T) {
	t.Parallel()

	// 3 miércoles, 1 lunes → ancla = miércoles
	wed := time.Wednesday
	pagos := []domain.PagoCrudo{
		{Fecha: time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)},  // miércoles
		{Fecha: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)}, // miércoles
		{Fecha: time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)}, // miércoles
		{Fecha: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)},  // lunes
	}
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(pagos, nil, decimal.Zero, ahora, noRango())
	assert.Equal(t, wed, r.AnclaDiaRuta)
}

func TestBuildRitmoPago_AnclaDiaRuta_EmpateEligeAntesEnSemana(t *testing.T) {
	t.Parallel()

	// 1 lunes, 1 miércoles → empate → elige lunes (más temprano en lun..dom)
	pagos := []domain.PagoCrudo{
		{Fecha: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)}, // lunes
		{Fecha: time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)}, // miércoles
	}
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(pagos, nil, decimal.Zero, ahora, noRango())
	assert.Equal(t, time.Monday, r.AnclaDiaRuta, "empate → lunes (antes en semana)")
}

func TestBuildRitmoPago_AnclaDiaRuta_SinPagos_EsLunes(t *testing.T) {
	t.Parallel()
	// Solo ventas, sin pagos → ancla = lunes
	ventas := []domain.VentaCruda{
		{Fecha: time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC), Total: mustDecimal("100"), DoctoPvID: 1, EsCredito: true},
	}
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(nil, ventas, decimal.Zero, ahora, noRango())
	assert.Equal(t, time.Monday, r.AnclaDiaRuta, "sin pagos → ancla=lunes aunque haya ventas")
}

// ─── Semanas intermedias rellenas ──────────────────────────────────────────────

func TestBuildRitmoPago_SemanasIntermedias_Rellenas(t *testing.T) {
	t.Parallel()

	// Primer pago: lunes 2026-06-01; segundo pago: lunes 2026-06-15 (salta la semana 08)
	// Ancla = lunes. Ahora = 2026-06-18.
	pagos := []domain.PagoCrudo{
		{Fecha: monday(2026, 6, 1), Importe: mustDecimal("200")},
		{Fecha: monday(2026, 6, 15), Importe: mustDecimal("300")},
	}
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(pagos, nil, decimal.Zero, ahora, noRango())

	require.Equal(t, time.Monday, r.AnclaDiaRuta)
	// Semanas esperadas: 01-jun, 08-jun, 15-jun (3 semanas)
	require.Len(t, r.Semanas, 3)
	assert.Equal(t, monday(2026, 6, 1), r.Semanas[0].SemanaInicio)
	assert.Equal(t, monday(2026, 6, 8), r.Semanas[1].SemanaInicio)
	assert.Equal(t, monday(2026, 6, 15), r.Semanas[2].SemanaInicio)

	// Semana intermedia sin pago
	assert.True(t, decimal.Zero.Equal(r.Semanas[1].MontoAbonado), "semana 08-jun sin pago → 0")
	assert.Equal(t, 0, r.Semanas[1].NumPagos)
}

// ─── Reconstrucción de saldo ───────────────────────────────────────────────────

func TestBuildRitmoPago_Saldo_UltimaSemanaSaldoActual(t *testing.T) {
	t.Parallel()

	// 1 venta crédito $1000, 1 abono $400 en la misma semana.
	// saldoActual = $600.
	pagos := []domain.PagoCrudo{
		{Fecha: monday(2026, 6, 15), Importe: mustDecimal("400")},
	}
	ventas := []domain.VentaCruda{
		{Fecha: monday(2026, 6, 15), Total: mustDecimal("1000"), DoctoPvID: 1, EsCredito: true},
	}
	saldo := mustDecimal("600")
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)

	r := domain.BuildRitmoPago(pagos, ventas, saldo, ahora, noRango())

	lastSemana := r.Semanas[len(r.Semanas)-1]
	assert.True(t, saldo.Equal(lastSemana.Saldo), "última semana Saldo == saldoActual")
}

func TestBuildRitmoPago_Saldo_ContadoNoAlteraSaldo(t *testing.T) {
	t.Parallel()

	// Venta contado $500 + abono $200. Solo el abono debería reducir el saldo.
	pagos := []domain.PagoCrudo{
		{Fecha: monday(2026, 6, 15), Importe: mustDecimal("200")},
	}
	ventas := []domain.VentaCruda{
		{Fecha: monday(2026, 6, 15), Total: mustDecimal("500"), DoctoPvID: 2, EsCredito: false},
	}
	saldoActual := mustDecimal("300") // saldo no relacionado con venta contado
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)

	r := domain.BuildRitmoPago(pagos, ventas, saldoActual, ahora, noRango())

	// La última semana debe ser saldoActual.
	last := r.Semanas[len(r.Semanas)-1]
	assert.True(t, saldoActual.Equal(last.Saldo), "contado no altera saldo; última semana == saldoActual")
}

func TestBuildRitmoPago_Saldo_ClampNegativo(t *testing.T) {
	t.Parallel()

	// Abono masivo que excede la venta → saldo debería quedar clamp a 0.
	pagos := []domain.PagoCrudo{
		{Fecha: monday(2026, 6, 15), Importe: mustDecimal("5000")},
	}
	ventas := []domain.VentaCruda{
		{Fecha: monday(2026, 6, 15), Total: mustDecimal("100"), DoctoPvID: 3, EsCredito: true},
	}
	// saldoActual = 0 (ya liquidado)
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(pagos, ventas, decimal.Zero, ahora, noRango())

	for i, s := range r.Semanas {
		assert.False(t, s.Saldo.IsNegative(), "semana %d: saldo no debe ser negativo", i)
	}
}

func TestBuildRitmoPago_Saldo_AbonoArrastraSaldo(t *testing.T) {
	t.Parallel()

	// Semana 1: venta $1000, abono $300. Semana 2: abono $200. Semana 3: sin pago.
	// saldoActual = $500.
	pagos := []domain.PagoCrudo{
		{Fecha: monday(2026, 6, 1), Importe: mustDecimal("300")},
		{Fecha: monday(2026, 6, 8), Importe: mustDecimal("200")},
	}
	ventas := []domain.VentaCruda{
		{Fecha: monday(2026, 6, 1), Total: mustDecimal("1000"), DoctoPvID: 4, EsCredito: true},
	}
	saldo := mustDecimal("500")
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)

	r := domain.BuildRitmoPago(pagos, ventas, saldo, ahora, noRango())

	require.Len(t, r.Semanas, 3)
	// Saldo última semana == saldoActual
	assert.True(t, saldo.Equal(r.Semanas[2].Saldo), "último saldo == saldoActual")
}

// ─── Eventos ──────────────────────────────────────────────────────────────────

func TestBuildRitmoPago_Eventos_VentaCreditoYContadoMapeados(t *testing.T) {
	t.Parallel()

	ventas := []domain.VentaCruda{
		{Fecha: monday(2026, 6, 1), Total: mustDecimal("1000"), DoctoPvID: 10, Folio: "F001", EsCredito: true, PlazoMeses: 6},
		{Fecha: monday(2026, 6, 8), Total: mustDecimal("500"), DoctoPvID: 11, Folio: "F002", EsCredito: false},
	}
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(nil, ventas, decimal.Zero, ahora, noRango())

	var creditoEvt, contadoEvt *domain.EventoRitmo
	for i := range r.Eventos {
		switch r.Eventos[i].Tipo {
		case domain.EventoVentaCredito:
			e := r.Eventos[i]
			creditoEvt = &e
		case domain.EventoVentaContado:
			e := r.Eventos[i]
			contadoEvt = &e
		case domain.EventoLiquidacion:
			// not expected in this test; ignore
		}
	}
	require.NotNil(t, creditoEvt, "debe haber un evento venta_credito")
	assert.Equal(t, 10, creditoEvt.DoctoPvID)
	assert.Equal(t, "F001", creditoEvt.Folio)
	assert.Equal(t, 6, creditoEvt.PlazoMeses)
	assert.True(t, mustDecimal("1000").Equal(creditoEvt.Monto))

	require.NotNil(t, contadoEvt, "debe haber un evento venta_contado")
	assert.Equal(t, 11, contadoEvt.DoctoPvID)
	assert.Equal(t, "F002", contadoEvt.Folio)
	assert.True(t, mustDecimal("500").Equal(contadoEvt.Monto))
}

func TestBuildRitmoPago_Eventos_LiquidacionEmitida(t *testing.T) {
	t.Parallel()

	// Semana 1: venta $500, sin abono. Semana 2: abono $500 (liquida).
	// saldoActual = 0.
	pagos := []domain.PagoCrudo{
		{Fecha: monday(2026, 6, 8), Importe: mustDecimal("500")},
	}
	ventas := []domain.VentaCruda{
		{Fecha: monday(2026, 6, 1), Total: mustDecimal("500"), DoctoPvID: 20, Folio: "F020", EsCredito: true},
	}
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(pagos, ventas, decimal.Zero, ahora, noRango())

	var liqEvt *domain.EventoRitmo
	for i := range r.Eventos {
		if r.Eventos[i].Tipo == domain.EventoLiquidacion {
			e := r.Eventos[i]
			liqEvt = &e
			break
		}
	}
	require.NotNil(t, liqEvt, "debe haber un evento liquidacion")
	assert.Equal(t, 20, liqEvt.DoctoPvID, "folio del último crédito previo")
	assert.Equal(t, "F020", liqEvt.Folio)
	assert.True(t, decimal.Zero.Equal(liqEvt.Monto))
}

func TestBuildRitmoPago_Eventos_OrdenadosPorFecha(t *testing.T) {
	t.Parallel()

	ventas := []domain.VentaCruda{
		{Fecha: monday(2026, 6, 8), Total: mustDecimal("200"), DoctoPvID: 30, Folio: "F030", EsCredito: false},
		{Fecha: monday(2026, 6, 1), Total: mustDecimal("100"), DoctoPvID: 31, Folio: "F031", EsCredito: false},
	}
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(nil, ventas, decimal.Zero, ahora, noRango())

	require.Len(t, r.Eventos, 2)
	assert.True(t, r.Eventos[0].Fecha.Before(r.Eventos[1].Fecha) || r.Eventos[0].Fecha.Equal(r.Eventos[1].Fecha),
		"eventos deben estar ordenados ascendente")
}

func TestBuildRitmoPago_Eventos_LiquidacionSinCreditoPrevio(t *testing.T) {
	t.Parallel()

	// Pagos con saldo previo positivo pero sin venta_credito registrada en la ventana.
	// La liquidación debe emitirse con DoctoPvID=0 y Folio="".
	//
	// Scenario: two pagos spread across two weeks; no credit ventas in the window.
	// saldoActual=0, totalAbonos=100 → saldoInicial = clampZero(0+100-0) = 100.
	// Week 0 (Jun 8): abono 50 → saldo=50 (>0).
	// Week 1 (Jun 15): abono 50 → saldo=0. Transition >0→0 emits liquidacion.
	// Week 2 (Jun 22): empty → saldo=0 (last week pinned to saldoActual=0).
	// liquidacionEventos fires at i=1 because semanas[0].Saldo=50>0, semanas[1].Saldo=0.
	pagos := []domain.PagoCrudo{
		{Fecha: monday(2026, 6, 8), Importe: mustDecimal("50")},
		{Fecha: monday(2026, 6, 15), Importe: mustDecimal("50")},
	}
	ahora := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(pagos, nil, decimal.Zero, ahora, noRango())

	var liqEvt *domain.EventoRitmo
	for i := range r.Eventos {
		if r.Eventos[i].Tipo == domain.EventoLiquidacion {
			e := r.Eventos[i]
			liqEvt = &e
			break
		}
	}
	// The scenario deterministically transitions saldo >0→0 so a liquidacion must be emitted.
	require.NotNil(t, liqEvt, "debe emitirse un evento de liquidacion")
	assert.Equal(t, 0, liqEvt.DoctoPvID, "sin crédito previo: DoctoPvID=0")
	assert.Empty(t, liqEvt.Folio, "sin crédito previo: Folio vacío")
}

// ─── Resumen ──────────────────────────────────────────────────────────────────

func TestBuildRitmoPago_Resumen_SemanasConPago(t *testing.T) {
	t.Parallel()

	// 3 semanas, solo 2 con pago.
	pagos := []domain.PagoCrudo{
		{Fecha: monday(2026, 6, 1), Importe: mustDecimal("100")},
		{Fecha: monday(2026, 6, 15), Importe: mustDecimal("200")},
	}
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(pagos, nil, decimal.Zero, ahora, noRango())

	assert.Equal(t, 2, r.Resumen.SemanasConPago)
}

func TestBuildRitmoPago_Resumen_SemanasActivas(t *testing.T) {
	t.Parallel()

	// Primera semana con pago: 2026-06-01 (semana 0). Ventana termina en semana 2 (15-jun).
	// SemanasActivas = 3 (0..2 inclusive).
	pagos := []domain.PagoCrudo{
		{Fecha: monday(2026, 6, 1), Importe: mustDecimal("100")},
		{Fecha: monday(2026, 6, 15), Importe: mustDecimal("200")},
	}
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(pagos, nil, decimal.Zero, ahora, noRango())

	assert.Equal(t, 3, r.Resumen.SemanasActivas)
}

func TestBuildRitmoPago_Resumen_ConstanciaPct_Redondeo2Decimales(t *testing.T) {
	t.Parallel()

	// 2 semanas con pago, 3 activas → 2/3 * 100 = 66.67%
	pagos := []domain.PagoCrudo{
		{Fecha: monday(2026, 6, 1), Importe: mustDecimal("100")},
		{Fecha: monday(2026, 6, 15), Importe: mustDecimal("200")},
	}
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(pagos, nil, decimal.Zero, ahora, noRango())

	expected := mustDecimal("66.67")
	assert.True(t, expected.Equal(r.Resumen.ConstanciaPct),
		"ConstanciaPct esperado 66.67, obtenido %s", r.Resumen.ConstanciaPct.String())
}

func TestBuildRitmoPago_Resumen_ConstanciaPct_CeroSinActivas(t *testing.T) {
	t.Parallel()

	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(nil, nil, decimal.Zero, ahora, noRango())
	assert.True(t, decimal.Zero.Equal(r.Resumen.ConstanciaPct), "sin semanas activas → ConstanciaPct=0")
}

func TestBuildRitmoPago_Resumen_RachaActual_HuecoPorDetras(t *testing.T) {
	t.Parallel()

	// Pagos solo en semanas 1 y 2 (no en la última semana 3). Racha = 0.
	pagos := []domain.PagoCrudo{
		{Fecha: monday(2026, 6, 1), Importe: mustDecimal("100")},
		{Fecha: monday(2026, 6, 8), Importe: mustDecimal("200")},
		// semana 15-jun: sin pago
	}
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(pagos, nil, decimal.Zero, ahora, noRango())

	assert.Equal(t, 0, r.Resumen.RachaActualSem, "último semana sin pago → racha=0")
}

func TestBuildRitmoPago_Resumen_RachaActual_SeCortaEnHueco(t *testing.T) {
	t.Parallel()

	// Semana 1: pago. Semana 2: sin pago. Semana 3: pago. Racha desde el final = 1.
	pagos := []domain.PagoCrudo{
		{Fecha: monday(2026, 6, 1), Importe: mustDecimal("100")},
		{Fecha: monday(2026, 6, 15), Importe: mustDecimal("200")},
	}
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(pagos, nil, decimal.Zero, ahora, noRango())

	assert.Equal(t, 1, r.Resumen.RachaActualSem, "hueco intermedio corta racha: solo 1 sem")
}

func TestBuildRitmoPago_Resumen_RachaActual_Continua(t *testing.T) {
	t.Parallel()

	// 3 semanas consecutivas con pago → racha = 3.
	pagos := []domain.PagoCrudo{
		{Fecha: monday(2026, 6, 1), Importe: mustDecimal("100")},
		{Fecha: monday(2026, 6, 8), Importe: mustDecimal("200")},
		{Fecha: monday(2026, 6, 15), Importe: mustDecimal("300")},
	}
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(pagos, nil, decimal.Zero, ahora, noRango())

	assert.Equal(t, 3, r.Resumen.RachaActualSem)
}

// ─── RangoFechas ──────────────────────────────────────────────────────────────

func TestBuildRitmoPago_Rango_DesdeRestringeVentana(t *testing.T) {
	t.Parallel()

	desde := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC) // segunda semana
	pagos := []domain.PagoCrudo{
		{Fecha: monday(2026, 6, 1), Importe: mustDecimal("100")}, // fuera del rango
		{Fecha: monday(2026, 6, 8), Importe: mustDecimal("200")}, // dentro del rango
		{Fecha: monday(2026, 6, 15), Importe: mustDecimal("300")},
	}
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	rango := domain.RangoFechasRitmo{Desde: &desde}

	r := domain.BuildRitmoPago(pagos, nil, decimal.Zero, ahora, rango)

	// La primera semana de la ventana debe ser >= desde.
	require.NotEmpty(t, r.Semanas)
	assert.False(t, r.Semanas[0].SemanaInicio.Before(monday(2026, 6, 8)),
		"primera semana debe ser >= desde")
	// El abono de la semana 01-jun no debe estar contabilizado.
	var totalContabilizado decimal.Decimal
	for _, s := range r.Semanas {
		totalContabilizado = totalContabilizado.Add(s.MontoAbonado)
	}
	assert.True(t, mustDecimal("500").Equal(totalContabilizado),
		"solo abonos dentro del rango: 200+300=500, obtenido %s", totalContabilizado)
}

func TestBuildRitmoPago_Rango_HastaRestringeVentana(t *testing.T) {
	t.Parallel()

	hasta := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	pagos := []domain.PagoCrudo{
		{Fecha: monday(2026, 6, 1), Importe: mustDecimal("100")},
		{Fecha: monday(2026, 6, 8), Importe: mustDecimal("200")},
		{Fecha: monday(2026, 6, 15), Importe: mustDecimal("300")}, // después del hasta
	}
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	rango := domain.RangoFechasRitmo{Hasta: &hasta}

	r := domain.BuildRitmoPago(pagos, nil, decimal.Zero, ahora, rango)

	// La última semana debe ser la semana del hasta.
	require.NotEmpty(t, r.Semanas)
	last := r.Semanas[len(r.Semanas)-1]
	assert.False(t, last.SemanaInicio.After(monday(2026, 6, 8)),
		"última semana debe ser <= hasta")
}

func TestBuildRitmoPago_Rango_DesdeYHastaExplicitos(t *testing.T) {
	t.Parallel()

	desde := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	hasta := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	pagos := []domain.PagoCrudo{
		{Fecha: monday(2026, 6, 1), Importe: mustDecimal("100")},
		{Fecha: monday(2026, 6, 8), Importe: mustDecimal("200")},
	}
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	rango := domain.RangoFechasRitmo{Desde: &desde, Hasta: &hasta}

	r := domain.BuildRitmoPago(pagos, nil, decimal.Zero, ahora, rango)

	// Exactamente 1 semana (08-jun)
	require.Len(t, r.Semanas, 1)
	assert.Equal(t, monday(2026, 6, 8), r.Semanas[0].SemanaInicio)
	assert.True(t, mustDecimal("200").Equal(r.Semanas[0].MontoAbonado))
}

// ─── Pagos por semana [BE-R] ──────────────────────────────────────────────────

func TestBuildRitmoPago_Pagos_MultiPagoEnMismaSemana(t *testing.T) {
	t.Parallel()

	// Dos pagos en la semana 01-jun (lunes), uno en 08-jun, semana 15-jun vacía.
	// Semana 0: pagos con DoctoCCID 101 (concepto 87327 → pago) y 102 (concepto 25116 → condonacion).
	// Semana 1: pago con DoctoCCID 201 (concepto 24533 → enganche) que apunta a venta 9001/AB0001.
	// Semana 2: sin pagos → Pagos vacío (no nil).
	pagos := []domain.PagoCrudo{
		{
			Fecha: monday(2026, 6, 1), Importe: mustDecimal("100"), DoctoCCID: 101,
			Hora: "10:00:00", ConceptoCCID: 87327, Concepto: "Cobranza en ruta",
			DoctoPVID: 5001, Folio: "AB0001714",
		},
		{
			Fecha: monday(2026, 6, 1), Importe: mustDecimal("50"), DoctoCCID: 102,
			Hora: "11:00:00", ConceptoCCID: 25116, Concepto: "Condonación",
			DoctoPVID: 5001, Folio: "AB0001714",
		},
		{
			Fecha: monday(2026, 6, 8), Importe: mustDecimal("200"), DoctoCCID: 201,
			Hora: "14:30:00", ConceptoCCID: 24533, Concepto: "Enganche",
			DoctoPVID: 9001, Folio: "AB0001",
		},
	}
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(pagos, nil, decimal.Zero, ahora, noRango())

	require.Len(t, r.Semanas, 3)

	// Semana 0 (01-jun): dos pagos → DoctoCCIDs en orden de entrada.
	require.Len(t, r.Semanas[0].Pagos, 2, "semana 0: dos pagos")
	assert.Equal(t, 101, r.Semanas[0].Pagos[0].DoctoCCID)
	assert.Equal(t, domain.CategoriaIngresoPago, r.Semanas[0].Pagos[0].Categoria)
	assert.Equal(t, 5001, r.Semanas[0].Pagos[0].DoctoPVID)
	assert.Equal(t, "AB0001714", r.Semanas[0].Pagos[0].Folio)
	assert.Equal(t, "10:00:00", r.Semanas[0].Pagos[0].Hora)
	assert.Equal(t, 102, r.Semanas[0].Pagos[1].DoctoCCID)
	assert.Equal(t, domain.CategoriaCondonacion, r.Semanas[0].Pagos[1].Categoria)

	// Semana 1 (08-jun): un pago enganche.
	require.Len(t, r.Semanas[1].Pagos, 1, "semana 1: un pago")
	assert.Equal(t, 201, r.Semanas[1].Pagos[0].DoctoCCID)
	assert.Equal(t, domain.CategoriaIngresoEnganche, r.Semanas[1].Pagos[0].Categoria)
	assert.Equal(t, 9001, r.Semanas[1].Pagos[0].DoctoPVID)
	assert.Equal(t, "AB0001", r.Semanas[1].Pagos[0].Folio)

	// Semana 2 (15-jun): sin pagos → slice vacío, NO nil.
	assert.NotNil(t, r.Semanas[2].Pagos, "semana vacía: Pagos debe ser no-nil")
	assert.Empty(t, r.Semanas[2].Pagos, "semana vacía: Pagos debe estar vacío")
}

func TestBuildRitmoPago_Pagos_SemanaVaciaNoEsNil(t *testing.T) {
	t.Parallel()

	// Un único pago en la primera semana; la segunda semana queda vacía.
	pagos := []domain.PagoCrudo{
		{
			Fecha: monday(2026, 6, 1), Importe: mustDecimal("300"), DoctoCCID: 999,
			Hora: "09:00:00", ConceptoCCID: 11, Concepto: "Cobro mostrador",
			DoctoPVID: 8888, Folio: "AB0009",
		},
	}
	ahora := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(pagos, nil, decimal.Zero, ahora, noRango())

	require.GreaterOrEqual(t, len(r.Semanas), 2, "esperamos al menos 2 semanas")

	// Primera semana: DoctoCCID 999, categoría pago.
	require.Len(t, r.Semanas[0].Pagos, 1)
	assert.Equal(t, 999, r.Semanas[0].Pagos[0].DoctoCCID)
	assert.Equal(t, domain.CategoriaIngresoPago, r.Semanas[0].Pagos[0].Categoria)

	// Semanas vacías: Pagos no-nil y vacío.
	for i := 1; i < len(r.Semanas); i++ {
		assert.NotNil(t, r.Semanas[i].Pagos, "semana %d: Pagos no debe ser nil", i)
		assert.Empty(t, r.Semanas[i].Pagos, "semana %d: Pagos debe estar vacío", i)
	}
}

// ─── Resumen: ingreso vs perdón [BE-S] ────────────────────────────────────────

func TestBuildRitmoPago_Resumen_IngresoVsPerdon(t *testing.T) {
	t.Parallel()

	// Semana 01-jun: pago $300 (ingreso) + condonación $200 (no ingreso) = MontoAbonado $500.
	// Semana 08-jun: pérdida $100 (no ingreso) = MontoAbonado $100.
	// Resumen.TotalAbonado   = $300 (solo ingreso).
	// Resumen.TotalPerdonado = $300 (condonacion $200 + perdida $100).
	// MontoAbonado por semana = todo (no cambia).
	pagos := []domain.PagoCrudo{
		{
			Fecha: monday(2026, 6, 1), Importe: mustDecimal("300"), DoctoCCID: 1,
			ConceptoCCID: 87327, Concepto: "Cobranza en ruta",
		},
		{
			Fecha: monday(2026, 6, 1), Importe: mustDecimal("200"), DoctoCCID: 2,
			ConceptoCCID: 25116, Concepto: "Condonación",
		},
		{
			Fecha: monday(2026, 6, 8), Importe: mustDecimal("100"), DoctoCCID: 3,
			ConceptoCCID: 27966, Concepto: "Pérdida",
		},
	}
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(pagos, nil, decimal.Zero, ahora, noRango())

	// Resumen: TotalAbonado = ingreso únicamente.
	assert.True(t, mustDecimal("300").Equal(r.Resumen.TotalAbonado),
		"TotalAbonado debe ser solo ingreso (pago $300), obtenido %s", r.Resumen.TotalAbonado)

	// Resumen: TotalPerdonado = condonacion + perdida.
	assert.True(t, mustDecimal("300").Equal(r.Resumen.TotalPerdonado),
		"TotalPerdonado debe ser condonacion $200 + perdida $100 = $300, obtenido %s", r.Resumen.TotalPerdonado)

	// MontoAbonado por semana sigue sumando TODO (incluyendo condonacion/perdida),
	// para que la reconstrucción del saldo no cambie.
	require.Len(t, r.Semanas, 3)
	assert.True(t, mustDecimal("500").Equal(r.Semanas[0].MontoAbonado),
		"semana 0 MontoAbonado debe incluir pago+condonacion = $500, obtenido %s", r.Semanas[0].MontoAbonado)
	assert.True(t, mustDecimal("100").Equal(r.Semanas[1].MontoAbonado),
		"semana 1 MontoAbonado debe incluir perdida = $100, obtenido %s", r.Semanas[1].MontoAbonado)
}

// ─── Artículo del 1er artículo [BE-R2] ────────────────────────────────────────

func TestBuildRitmoPago_Pagos_ArticuloPropagado(t *testing.T) {
	t.Parallel()

	// Pago con Articulo poblado → PagoRitmo debe conservar el nombre del artículo.
	pagos := []domain.PagoCrudo{
		{
			Fecha: monday(2026, 6, 1), Importe: mustDecimal("500"), DoctoCCID: 301,
			Hora: "10:00:00", ConceptoCCID: 87327, Concepto: "Cobranza en ruta",
			DoctoPVID: 12843622, Folio: "AB0001714",
			Articulo: "JUEGO DE SALA MODELO ATLANTA",
		},
	}
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(pagos, nil, decimal.Zero, ahora, noRango())

	require.Len(t, r.Semanas, 3)
	require.Len(t, r.Semanas[0].Pagos, 1)
	assert.Equal(t, "JUEGO DE SALA MODELO ATLANTA", r.Semanas[0].Pagos[0].Articulo,
		"Articulo debe propagarse de PagoCrudo a PagoRitmo")
}

func TestBuildRitmoPago_Pagos_ArticuloVacioPermitido(t *testing.T) {
	t.Parallel()

	// Pago sin artículo resolvable (Articulo="") → PagoRitmo.Articulo también vacío.
	pagos := []domain.PagoCrudo{
		{
			Fecha: monday(2026, 6, 1), Importe: mustDecimal("200"), DoctoCCID: 401,
			Hora: "08:00:00", ConceptoCCID: 87327, Concepto: "Cobranza en ruta",
			DoctoPVID: 0, Folio: "", Articulo: "",
		},
	}
	ahora := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	r := domain.BuildRitmoPago(pagos, nil, decimal.Zero, ahora, noRango())

	require.Len(t, r.Semanas[0].Pagos, 1)
	assert.Empty(t, r.Semanas[0].Pagos[0].Articulo, "Articulo vacío cuando no resoluble")
}

// ─── Constantes EventoTipo ─────────────────────────────────────────────────────

func TestEventoTipo_Constantes(t *testing.T) {
	t.Parallel()
	assert.Equal(t, domain.EventoVentaCredito, domain.EventoTipo("venta_credito"))
	assert.Equal(t, domain.EventoVentaContado, domain.EventoTipo("venta_contado"))
	assert.Equal(t, domain.EventoLiquidacion, domain.EventoTipo("liquidacion"))
}
