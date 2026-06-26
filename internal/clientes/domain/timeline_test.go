//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func pagoAt(t time.Time, importe, doctoCCID int, concepto, folio string) domain.PagoCrudo {
	return domain.PagoCrudo{
		Fecha:     t,
		Importe:   decimal.NewFromInt(int64(importe)),
		DoctoCCID: doctoCCID,
		Concepto:  concepto,
		Folio:     folio,
	}
}

func ventaAt(t time.Time, total, doctoPvID int, folio string, esCredito bool) domain.VentaCruda {
	return domain.VentaCruda{
		Fecha:     t,
		Total:     decimal.NewFromInt(int64(total)),
		DoctoPvID: doctoPvID,
		Folio:     folio,
		EsCredito: esCredito,
	}
}

var (
	t1 = time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)
	t2 = time.Date(2025, 2, 5, 0, 0, 0, 0, time.UTC)
	t3 = time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC)
)

// ─── BuildTimeline: nil / empty inputs ─────────────────────────────────────────

func TestBuildTimeline_NilInputs_EmptySlice(t *testing.T) {
	t.Parallel()

	result := domain.BuildTimeline(nil, nil)
	require.NotNil(t, result, "must be a non-nil slice")
	assert.Empty(t, result)
}

func TestBuildTimeline_EmptySlices_EmptySlice(t *testing.T) {
	t.Parallel()

	result := domain.BuildTimeline([]domain.PagoCrudo{}, []domain.VentaCruda{})
	require.NotNil(t, result)
	assert.Empty(t, result)
}

// ─── BuildTimeline: conteo total ──────────────────────────────────────────────

func TestBuildTimeline_Count_EqualsSum(t *testing.T) {
	t.Parallel()

	pagos := []domain.PagoCrudo{
		pagoAt(t1, 1000, 1, "cobranza", ""),
		pagoAt(t2, 500, 2, "enganche", ""),
	}
	ventas := []domain.VentaCruda{
		ventaAt(t3, 12000, 100, "PV-0100", true),
	}

	result := domain.BuildTimeline(pagos, ventas)
	assert.Len(t, result, 3, "total = len(pagos)+len(ventas)")
}

// ─── BuildTimeline: tipos correctos ──────────────────────────────────────────

func TestBuildTimeline_TiposCorrectos(t *testing.T) {
	t.Parallel()

	pagos := []domain.PagoCrudo{pagoAt(t1, 1000, 10, "pago semanal", "")}
	ventas := []domain.VentaCruda{
		ventaAt(t2, 5000, 200, "PV-200", true),  // credito
		ventaAt(t3, 3000, 300, "PV-300", false), // contado
	}

	result := domain.BuildTimeline(pagos, ventas)
	require.Len(t, result, 3)

	tiposByRefID := make(map[int]string, 3)
	for _, e := range result {
		tiposByRefID[e.RefID] = e.Tipo
	}

	assert.Equal(t, domain.TipoPago, tiposByRefID[10])
	assert.Equal(t, domain.TipoCompraCredito, tiposByRefID[200])
	assert.Equal(t, domain.TipoCompraContado, tiposByRefID[300])
}

// ─── BuildTimeline: orden descendente ─────────────────────────────────────────

func TestBuildTimeline_OrdenDescendente(t *testing.T) {
	t.Parallel()

	pagos := []domain.PagoCrudo{
		pagoAt(t1, 1000, 1, "pago", ""),
		pagoAt(t3, 800, 3, "pago", ""),
	}
	ventas := []domain.VentaCruda{
		ventaAt(t2, 5000, 100, "PV-100", true),
	}

	result := domain.BuildTimeline(pagos, ventas)
	require.Len(t, result, 3)

	for i := range len(result) - 1 {
		assert.False(
			t, result[i].Fecha.Before(result[i+1].Fecha),
			"result[%d].Fecha=%v should be >= result[%d].Fecha=%v",
			i, result[i].Fecha, i+1, result[i+1].Fecha,
		)
	}

	assert.Equal(t, t3, result[0].Fecha, "most recent first")
	assert.Equal(t, t2, result[1].Fecha)
	assert.Equal(t, t1, result[2].Fecha)
}

// ─── BuildTimeline: empate de fecha → desempate por RefID descendente ─────────

func TestBuildTimeline_EmpateOrdenEstable(t *testing.T) {
	t.Parallel()

	ts := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	pagos := []domain.PagoCrudo{
		pagoAt(ts, 500, 5, "pago", ""),  // RefID=5
		pagoAt(ts, 700, 20, "pago", ""), // RefID=20
	}
	ventas := []domain.VentaCruda{
		ventaAt(ts, 3000, 10, "PV-10", true), // RefID=10
	}

	result := domain.BuildTimeline(pagos, ventas)
	require.Len(t, result, 3)

	// All same date → sorted by RefID descending: 20, 10, 5
	assert.Equal(t, 20, result[0].RefID)
	assert.Equal(t, 10, result[1].RefID)
	assert.Equal(t, 5, result[2].RefID)
}

// ─── BuildTimeline: etiqueta fallback a folio ─────────────────────────────────

func TestBuildTimeline_EtiquetaUsaConcepto(t *testing.T) {
	t.Parallel()

	p := pagoAt(t1, 1000, 1, "cobranza semanal", "PV-0001")
	result := domain.BuildTimeline([]domain.PagoCrudo{p}, nil)
	require.Len(t, result, 1)
	assert.Equal(t, "cobranza semanal", result[0].Etiqueta, "uses Concepto when non-empty")
}

func TestBuildTimeline_EtiquetaFallbackAFolio(t *testing.T) {
	t.Parallel()

	p := pagoAt(t1, 1000, 1, "", "PV-0001") // Concepto empty → fallback to Folio
	result := domain.BuildTimeline([]domain.PagoCrudo{p}, nil)
	require.Len(t, result, 1)
	assert.Equal(t, "PV-0001", result[0].Etiqueta, "falls back to Folio when Concepto empty")
}

func TestBuildTimeline_VentaEtiquetaEsFolio(t *testing.T) {
	t.Parallel()

	v := ventaAt(t1, 5000, 100, "PV-9999", true)
	result := domain.BuildTimeline(nil, []domain.VentaCruda{v})
	require.Len(t, result, 1)
	assert.Equal(t, "PV-9999", result[0].Etiqueta)
}

// ─── BuildTimeline: RefID correcto por tipo ────────────────────────────────────

func TestBuildTimeline_RefID_CompraUsaDoctoPvID(t *testing.T) {
	t.Parallel()

	v := ventaAt(t1, 5000, 777, "PV-777", false)
	result := domain.BuildTimeline(nil, []domain.VentaCruda{v})
	require.Len(t, result, 1)
	assert.Equal(t, 777, result[0].RefID)
}

func TestBuildTimeline_RefID_PagoUsaDoctoCCID(t *testing.T) {
	t.Parallel()

	p := pagoAt(t1, 1000, 999, "pago", "")
	result := domain.BuildTimeline([]domain.PagoCrudo{p}, nil)
	require.Len(t, result, 1)
	assert.Equal(t, 999, result[0].RefID)
}

// ─── BuildTimeline: fechas normalizadas a UTC ──────────────────────────────────

func TestBuildTimeline_FechasUTC(t *testing.T) {
	t.Parallel()

	loc := time.FixedZone("CDT", -5*60*60) // UTC-5
	localTime := time.Date(2025, 6, 1, 14, 0, 0, 0, loc)
	expectedUTC := localTime.UTC()

	p := pagoAt(localTime, 1000, 1, "pago", "")
	result := domain.BuildTimeline([]domain.PagoCrudo{p}, nil)
	require.Len(t, result, 1)
	assert.Equal(t, time.UTC, result[0].Fecha.Location())
	assert.Equal(t, expectedUTC, result[0].Fecha)
}

// ─── BuildTimeline: solo pagos ────────────────────────────────────────────────

func TestBuildTimeline_SoloPagos(t *testing.T) {
	t.Parallel()

	pagos := []domain.PagoCrudo{
		pagoAt(t3, 800, 3, "C", ""),
		pagoAt(t1, 500, 1, "A", ""),
		pagoAt(t2, 700, 2, "B", ""),
	}
	result := domain.BuildTimeline(pagos, nil)
	require.Len(t, result, 3)
	// Descending: t3 > t2 > t1
	assert.Equal(t, t3, result[0].Fecha)
	assert.Equal(t, t2, result[1].Fecha)
	assert.Equal(t, t1, result[2].Fecha)
}

// ─── BuildTimeline: solo ventas ───────────────────────────────────────────────

func TestBuildTimeline_SoloVentas(t *testing.T) {
	t.Parallel()

	ventas := []domain.VentaCruda{
		ventaAt(t1, 3000, 1, "PV-1", true),
		ventaAt(t3, 8000, 3, "PV-3", false),
		ventaAt(t2, 5000, 2, "PV-2", true),
	}
	result := domain.BuildTimeline(nil, ventas)
	require.Len(t, result, 3)
	assert.Equal(t, t3, result[0].Fecha)
	assert.Equal(t, t2, result[1].Fecha)
	assert.Equal(t, t1, result[2].Fecha)
}

// ─── Property tests ───────────────────────────────────────────────────────────

// genPagoCrudo is a rapid generator for PagoCrudo with arbitrary but valid data.
func genPagoCrudo(rt *rapid.T, idx int) domain.PagoCrudo {
	year := rapid.IntRange(2020, 2026).Draw(rt, "pago_year")
	month := rapid.IntRange(1, 12).Draw(rt, "pago_month")
	day := rapid.IntRange(1, 28).Draw(rt, "pago_day")
	importe := rapid.Int64Range(1, 1_000_000).Draw(rt, "pago_importe")
	id := rapid.IntRange(1, 99999).Draw(rt, "pago_docto_cc_id")
	concepto := rapid.StringMatching(`[a-z ]{0,20}`).Draw(rt, "pago_concepto")
	folio := rapid.StringMatching(`PV-[0-9]{1,5}`).Draw(rt, "pago_folio")

	_ = idx // used to stabilise the label namespace when calling multiple times
	return domain.PagoCrudo{
		Fecha:     time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC),
		Importe:   decimal.NewFromInt(importe),
		DoctoCCID: id,
		Concepto:  concepto,
		Folio:     folio,
	}
}

// genVentaCruda is a rapid generator for VentaCruda with arbitrary but valid data.
func genVentaCruda(rt *rapid.T, idx int) domain.VentaCruda {
	year := rapid.IntRange(2020, 2026).Draw(rt, "venta_year")
	month := rapid.IntRange(1, 12).Draw(rt, "venta_month")
	day := rapid.IntRange(1, 28).Draw(rt, "venta_day")
	total := rapid.Int64Range(1, 2_000_000).Draw(rt, "venta_total")
	id := rapid.IntRange(1, 99999).Draw(rt, "venta_docto_pv_id")
	credito := rapid.Bool().Draw(rt, "venta_credito")
	folio := rapid.StringMatching(`PV-[0-9]{1,5}`).Draw(rt, "venta_folio")

	_ = idx
	return domain.VentaCruda{
		Fecha:     time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC),
		Total:     decimal.NewFromInt(total),
		DoctoPvID: id,
		Folio:     folio,
		EsCredito: credito,
	}
}

// TestBuildTimeline_Property_ConteoTotal verifies that the output length always
// equals len(pagos)+len(ventas) with arbitrary inputs.
func TestBuildTimeline_Property_ConteoTotal(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		np := rapid.IntRange(0, 20).Draw(rt, "num_pagos")
		nv := rapid.IntRange(0, 20).Draw(rt, "num_ventas")

		pagos := make([]domain.PagoCrudo, np)
		for i := range pagos {
			pagos[i] = genPagoCrudo(rt, i)
		}
		ventas := make([]domain.VentaCruda, nv)
		for i := range ventas {
			ventas[i] = genVentaCruda(rt, i)
		}

		result := domain.BuildTimeline(pagos, ventas)
		if len(result) != np+nv {
			rt.Fatalf("expected %d events, got %d", np+nv, len(result))
		}
	})
}

// TestBuildTimeline_Property_OrdenMonotonico verifies that the output is always
// sorted in non-increasing date order (descending) with arbitrary inputs.
func TestBuildTimeline_Property_OrdenMonotonico(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		np := rapid.IntRange(0, 15).Draw(rt, "num_pagos")
		nv := rapid.IntRange(0, 15).Draw(rt, "num_ventas")

		pagos := make([]domain.PagoCrudo, np)
		for i := range pagos {
			pagos[i] = genPagoCrudo(rt, i)
		}
		ventas := make([]domain.VentaCruda, nv)
		for i := range ventas {
			ventas[i] = genVentaCruda(rt, i)
		}

		result := domain.BuildTimeline(pagos, ventas)
		for i := range len(result) - 1 {
			if result[i].Fecha.Before(result[i+1].Fecha) {
				rt.Fatalf("not descending at index %d: %v < %v",
					i, result[i].Fecha, result[i+1].Fecha)
			}
		}
	})
}

// TestBuildTimeline_Property_NoPanic verifies that BuildTimeline never panics
// on arbitrary inputs (including zero amounts, zero IDs, empty strings).
func TestBuildTimeline_Property_NoPanic(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		np := rapid.IntRange(0, 30).Draw(rt, "num_pagos")
		nv := rapid.IntRange(0, 30).Draw(rt, "num_ventas")

		pagos := make([]domain.PagoCrudo, np)
		for i := range pagos {
			pagos[i] = genPagoCrudo(rt, i)
		}
		ventas := make([]domain.VentaCruda, nv)
		for i := range ventas {
			ventas[i] = genVentaCruda(rt, i)
		}

		// Must not panic.
		result := domain.BuildTimeline(pagos, ventas)
		_ = result
	})
}

// TestBuildTimeline_Property_TiposValidos verifies that every event in the
// output carries one of the three v1 type constants.
func TestBuildTimeline_Property_TiposValidos(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		np := rapid.IntRange(0, 10).Draw(rt, "num_pagos")
		nv := rapid.IntRange(0, 10).Draw(rt, "num_ventas")

		pagos := make([]domain.PagoCrudo, np)
		for i := range pagos {
			pagos[i] = genPagoCrudo(rt, i)
		}
		ventas := make([]domain.VentaCruda, nv)
		for i := range ventas {
			ventas[i] = genVentaCruda(rt, i)
		}

		result := domain.BuildTimeline(pagos, ventas)
		valid := map[string]bool{
			domain.TipoCompraCredito: true,
			domain.TipoCompraContado: true,
			domain.TipoPago:          true,
		}
		for _, e := range result {
			if !valid[e.Tipo] {
				rt.Fatalf("unexpected Tipo %q", e.Tipo)
			}
		}
	})
}
