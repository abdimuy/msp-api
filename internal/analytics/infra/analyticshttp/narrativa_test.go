//nolint:misspell // analytics vocabulary is Spanish per project convention.
package analyticshttp

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	analyticsapp "github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// makeTestItem builds a WinbackListItem for narrative tests without needing a DB.
func makeTestItem(
	seg domain.Segmento,
	ep domain.EstadoPago,
	monetary decimal.Decimal,
	frecuencia int,
	recenciaDias int,
	porLiquidarPct decimal.Decimal,
	nbp string,
) analyticsapp.WinbackListItem {
	now := time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)
	ultimaCompra := now.AddDate(0, 0, -recenciaDias)
	c, err := domain.CrearWinbackCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:         1,
		Nombre:            "Test Cliente",
		Zona:              "NORTE",
		Telefono:          "5550000001",
		FechaUltimaCompra: ultimaCompra,
		Frecuencia:        frecuencia,
		Monetary:          monetary,
		Saldo:             decimal.NewFromInt(0),
		PorLiquidarPct:    porLiquidarPct,
		NextBestProduct:   nbp,
		EnControl:         false,
		CohorteFecha:      now.AddDate(-1, 0, 0),
		Now:               now,
	})
	if err != nil {
		panic("makeTestItem: " + err.Error())
	}
	return analyticsapp.WinbackListItem{
		Candidato:    c,
		Segmento:     seg,
		RecenciaDias: recenciaDias,
		EstadoPago:   ep,
	}
}

func TestEtiquetaFor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		seg  domain.Segmento
		ep   domain.EstadoPago
		want string
	}{
		{"moroso overrides segment", domain.SegmentoDormidoValioso, domain.EstadoPagoMoroso, "Moroso"},
		{"atrasado overrides segment", domain.SegmentoLealPorLiquidar, domain.EstadoPagoAtrasado, "Atrasado en pagos"},
		{"dormido valioso al corriente", domain.SegmentoDormidoValioso, domain.EstadoPagoAlCorriente, "Valioso dormido"},
		{"leal por liquidar", domain.SegmentoLealPorLiquidar, domain.EstadoPagoAlCorriente, "Leal dormido"},
		{"activo", domain.SegmentoActivo, domain.EstadoPagoSinCredito, "Activo"},
		{"nuevo", domain.SegmentoNuevo, domain.EstadoPagoSinCredito, "Nuevo"},
		{"frio", domain.SegmentoFrio, domain.EstadoPagoLiquidado, "Frío"},
		{"perdido", domain.SegmentoPerdido, domain.EstadoPagoLiquidado, "Perdido"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := etiquetaFor(tc.seg, tc.ep)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestTierFor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		monetary   int64
		frecuencia int
		ep         domain.EstadoPago
		wantTier   string
	}{
		{"A: high monetary + high frecuencia", 60_000, 5, domain.EstadoPagoLiquidado, "A"},
		{"B: high monetary only", 55_000, 1, domain.EstadoPagoAlCorriente, "B"},
		{"B: high frecuencia only", 10_000, 4, domain.EstadoPagoAlCorriente, "B"},
		{"C: mid monetary", 20_000, 1, domain.EstadoPagoLiquidado, "C"},
		{"C: mid frecuencia", 5_000, 2, domain.EstadoPagoLiquidado, "C"},
		{"D: low value", 5_000, 1, domain.EstadoPagoSinCredito, "D"},
		{"moroso A demoted to C", 60_000, 5, domain.EstadoPagoMoroso, "C"},
		{"moroso B demoted to C", 55_000, 1, domain.EstadoPagoMoroso, "C"},
		{"moroso D stays D", 5_000, 1, domain.EstadoPagoMoroso, "D"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			item := makeTestItem(
				domain.SegmentoDormidoValioso,
				tc.ep,
				decimal.NewFromInt(tc.monetary),
				tc.frecuencia,
				365,
				decimal.Zero,
				"",
			)
			got := tierFor(item)
			assert.Equal(t, tc.wantTier, got)
		})
	}
}

func TestResumenFor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		seg            domain.Segmento
		ep             domain.EstadoPago
		recenciaDias   int
		porLiquidarPct decimal.Decimal
		nbp            string
		mustContain    []string
		maxWords       int
	}{
		{
			name:         "moroso",
			seg:          domain.SegmentoDormidoValioso,
			ep:           domain.EstadoPagoMoroso,
			recenciaDias: 400,
			mustContain:  []string{"pagar", "crédito"},
			maxWords:     15,
		},
		{
			name:         "valioso dormido buen pagador",
			seg:          domain.SegmentoDormidoValioso,
			ep:           domain.EstadoPagoLiquidado,
			recenciaDias: 365,
			mustContain:  []string{"meses"},
			maxWords:     15,
		},
		{
			name:         "frio",
			seg:          domain.SegmentoFrio,
			ep:           domain.EstadoPagoSinCredito,
			recenciaDias: 200,
			mustContain:  []string{"Bajo"},
			maxWords:     12,
		},
		{
			name:           "casi liquidado",
			seg:            domain.SegmentoLealPorLiquidar,
			ep:             domain.EstadoPagoAlCorriente,
			recenciaDias:   120,
			porLiquidarPct: decimal.NewFromInt(10),
			mustContain:    []string{"pagar"},
			maxWords:       12,
		},
		{
			name:         "atrasado",
			seg:          domain.SegmentoFrio,
			ep:           domain.EstadoPagoAtrasado,
			recenciaDias: 90,
			mustContain:  []string{"Atrasado", "meses"},
			maxWords:     12,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			item := makeTestItem(tc.seg, tc.ep, decimal.NewFromInt(30_000), 3, tc.recenciaDias, tc.porLiquidarPct, tc.nbp)
			got := resumenFor(item)
			assert.NotEmpty(t, got, "resumen must not be empty")
			wordCount := len(strings.Fields(got))
			assert.LessOrEqual(t, wordCount, tc.maxWords, "resumen too long: %q (%d words)", got, wordCount)
			for _, substr := range tc.mustContain {
				assert.Contains(t, got, substr, "resumen must contain %q: got %q", substr, got)
			}
		})
	}
}
