//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// ─── computeSegmentoScore — table-driven tests ────────────────────────────────

func TestComputeSegmentoScore_Segments(t *testing.T) {
	t.Parallel()

	type tc struct {
		name             string
		recenciaDias     int // how many days before testNow was the last purchase (0=today, -1=no purchase)
		frecuencia       int
		monetary         float64
		porLiquidarPct   float64
		telefono         string
		wantSeg          domain.Segmento
		wantScore        int
		wantRecenciaDias int
	}

	tests := []tc{
		{
			// recenciaDias=10, monetary=5000, no phone, no saldo:
			// value=min(1,5000/50000)=0.1, prop=1.0 (recencia≤335), contact=0, porLiq=0
			// raw=100*(0.40*0.1 + 0.30*1.0 + 0.15*0 + 0.15*0)=100*0.34=34 → score=34
			name:             "NUEVO — reciente, frecuencia=1",
			recenciaDias:     10,
			frecuencia:       1,
			monetary:         5_000,
			wantSeg:          domain.SegmentoNuevo,
			wantScore:        34,
			wantRecenciaDias: 10,
		},
		{
			// recenciaDias=100, monetary=15000, phone, no saldo:
			// value=min(1,15000/50000)=0.3, prop=1.0 (recencia≤335), contact=1, porLiq=0
			// raw=100*(0.40*0.3 + 0.30*1.0 + 0.15*1 + 0.15*0)=100*0.57=57 → score=57
			name:             "ACTIVO — reciente, frecuencia>1",
			recenciaDias:     100,
			frecuencia:       4,
			monetary:         15_000,
			telefono:         "555-1234",
			wantSeg:          domain.SegmentoActivo,
			wantScore:        57,
			wantRecenciaDias: 100,
		},
		{
			// recenciaDias=400, monetary=25000, phone, porLiq=50%:
			// value=0.5, prop=clamp01(1-(400-335)/395)=1-65/395≈0.8354, contact=1, porLiq=0.5
			// raw=100*(0.40*0.5 + 0.30*0.8354 + 0.15*1 + 0.15*0.5)≈67.563 → score=68
			name:             "LEAL_POR_LIQUIDAR — lapsed, frecuente, tiene saldo, dentro de 730d",
			recenciaDias:     400,
			frecuencia:       5,
			monetary:         25_000,
			porLiquidarPct:   50.0,
			telefono:         "555-9999",
			wantSeg:          domain.SegmentoLealPorLiquidar,
			wantScore:        68,
			wantRecenciaDias: 400,
		},
		{
			// recenciaDias=800, monetary=10000, no phone, no saldo:
			// value=0.2, prop=clamp01(1-(800-335)/395)=clamp01(1-465/395)=clamp01(-0.177)=0, contact=0, porLiq=0
			// raw=100*(0.40*0.2 + 0 + 0 + 0)=8 → score=8
			name:             "PERDIDO — recencia>730",
			recenciaDias:     800,
			frecuencia:       3,
			monetary:         10_000,
			porLiquidarPct:   0,
			wantSeg:          domain.SegmentoPerdido,
			wantScore:        8,
			wantRecenciaDias: 800,
		},
		{
			// recenciaDias=500, monetary=25000, no phone, no saldo:
			// value=0.5, prop=clamp01(1-(500-335)/395)=1-165/395≈0.5823, contact=0, porLiq=0
			// raw=100*(0.40*0.5 + 0.30*0.5823)≈37.468 → score=37
			name:             "DORMIDO_VALIOSO — lapsed, alto monetary, sin saldo, dentro 730d",
			recenciaDias:     500,
			frecuencia:       5,
			monetary:         25_000,
			porLiquidarPct:   0,
			wantSeg:          domain.SegmentoDormidoValioso,
			wantScore:        37,
			wantRecenciaDias: 500,
		},
		{
			// recenciaDias=500, monetary=5000, no phone, no saldo:
			// value=0.1, prop≈0.5823, contact=0, porLiq=0
			// raw=100*(0.40*0.1 + 0.30*0.5823)≈21.468 → score=21
			name:             "FRIO — lapsed, bajo monetary, sin saldo, dentro 730d",
			recenciaDias:     500,
			frecuencia:       2,
			monetary:         5_000,
			porLiquidarPct:   0,
			wantSeg:          domain.SegmentoFrio,
			wantScore:        21,
			wantRecenciaDias: 500,
		},
		{
			// recenciaDias=9999 (sentinel), monetary=0, no phone, no saldo:
			// value=0, prop=clamp01(1-(9999-335)/395)=0, contact=0, porLiq=0
			// raw=0 → score=0
			name:             "sin historial de compra — recenciaMax sentinel",
			recenciaDias:     -1, // sentinel: zero fecha
			frecuencia:       0,
			monetary:         0,
			wantSeg:          domain.SegmentoPerdido,
			wantScore:        0,
			wantRecenciaDias: 9_999,
		},
		{
			// recenciaDias=50, monetary=50000, phone, porLiq=80%:
			// value=1.0, prop=1.0 (recencia≤335), contact=1, porLiq=0.8
			// raw=100*(0.40*1 + 0.30*1 + 0.15*1 + 0.15*0.8)=100*0.97=97 → score=97
			name:             "score alto — reciente, alto valor, con teléfono, con saldo",
			recenciaDias:     50,
			frecuencia:       10,
			monetary:         50_000,
			porLiquidarPct:   80.0,
			telefono:         "555-0001",
			wantSeg:          domain.SegmentoActivo,
			wantScore:        97,
			wantRecenciaDias: 50,
		},
		{
			// recenciaDias=800, monetary=0, no phone, no saldo:
			// value=0, prop=0, contact=0, porLiq=0 → raw=0 → score=0
			name:             "score bajo — perdido, sin valor, sin teléfono, sin saldo",
			recenciaDias:     800,
			frecuencia:       1,
			monetary:         0,
			porLiquidarPct:   0,
			telefono:         "",
			wantSeg:          domain.SegmentoPerdido,
			wantScore:        0,
			wantRecenciaDias: 800,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var fechaUltimaCompra time.Time
			if tt.recenciaDias >= 0 {
				fechaUltimaCompra = testNow.AddDate(0, 0, -tt.recenciaDias)
			}

			c := mustCandidato(domain.CrearWinbackCandidatoParams{
				ClienteID:         1,
				Nombre:            "Test",
				Zona:              "Z1",
				Telefono:          tt.telefono,
				FechaUltimaCompra: fechaUltimaCompra,
				Frecuencia:        tt.frecuencia,
				Monetary:          decimal.NewFromFloat(tt.monetary),
				Saldo:             decimal.NewFromFloat(tt.monetary * 0.1),
				PorLiquidarPct:    decimal.NewFromFloat(tt.porLiquidarPct),
				EnControl:         false,
				CohorteFecha:      testNow.AddDate(-1, 0, 0),
				Now:               testNow,
			})

			seg, score, recencia := app.ExportComputeSegmentoScore(c, testNow)

			if seg != tt.wantSeg {
				t.Errorf("segment: got %q, want %q", seg, tt.wantSeg)
			}
			if score.Int() != tt.wantScore {
				t.Errorf("score: got %d, want %d", score.Int(), tt.wantScore)
			}
			if recencia != tt.wantRecenciaDias {
				t.Errorf("recenciaDias: got %d, want %d", recencia, tt.wantRecenciaDias)
			}
		})
	}
}

// TestComputeSegmentoScore_SegmentBoundaries tests the EXACT boundary values
// for umbralActivoDias (335) and umbralPerdidoDias (730). Only recenciaDias
// varies; monetary/frecuencia/porLiquidarPct are held constant to isolate
// the threshold logic.
func TestComputeSegmentoScore_SegmentBoundaries(t *testing.T) {
	t.Parallel()

	// Constants mirror scoring.go values; they are not exported, so we test
	// against their expected effect rather than the constant itself.
	//   umbralActivoDias  = 335  → ≤335 active, >335 lapsed
	//   umbralPerdidoDias = 730  → >730 perdido, ≤730 lapsed-but-not-lost

	type tc struct {
		name         string
		recenciaDias int
		wantSeg      domain.Segmento
	}

	tests := []tc{
		// umbralActivoDias boundary (335 / 336)
		{
			name:         "recencia=335 — still active (≤ umbralActivoDias)",
			recenciaDias: 335,
			wantSeg:      domain.SegmentoActivo,
		},
		{
			name:         "recencia=336 — just lapsed (> umbralActivoDias, ≤ umbralPerdidoDias)",
			recenciaDias: 336,
			// frecuencia=5 ≥ frecuenciaLeal(3) but porLiquidarPct=0 so no LEAL_POR_LIQUIDAR;
			// monetary=25000 ≥ 20000 so DORMIDO_VALIOSO.
			wantSeg: domain.SegmentoDormidoValioso,
		},

		// umbralPerdidoDias boundary (730 / 731)
		{
			name:         "recencia=730 — last lapsed day (≤ umbralPerdidoDias)",
			recenciaDias: 730,
			// Same conditions as above → DORMIDO_VALIOSO.
			wantSeg: domain.SegmentoDormidoValioso,
		},
		{
			name:         "recencia=731 — just lost (> umbralPerdidoDias)",
			recenciaDias: 731,
			wantSeg:      domain.SegmentoPerdido,
		},
	}

	// Shared fixture: frecuencia=5 (≥ frecuenciaLeal), monetary=25000 (≥ umbralValioso),
	// porLiquidarPct=0 (no saldo), no phone — only recencia drives segment classification.
	const (
		fixedFrecuencia     = 5
		fixedMonetary       = 25_000.0
		fixedPorLiquidarPct = 0.0
	)

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fechaUltimaCompra := testNow.AddDate(0, 0, -tt.recenciaDias)

			c := mustCandidato(domain.CrearWinbackCandidatoParams{
				ClienteID:         2,
				Nombre:            "Boundary",
				Zona:              "Z1",
				Telefono:          "",
				FechaUltimaCompra: fechaUltimaCompra,
				Frecuencia:        fixedFrecuencia,
				Monetary:          decimal.NewFromFloat(fixedMonetary),
				Saldo:             decimal.Zero,
				PorLiquidarPct:    decimal.NewFromFloat(fixedPorLiquidarPct),
				EnControl:         false,
				CohorteFecha:      testNow.AddDate(-1, 0, 0),
				Now:               testNow,
			})

			seg, _, recencia := app.ExportComputeSegmentoScore(c, testNow)

			if recencia != tt.recenciaDias {
				t.Errorf("recenciaDias: got %d, want %d", recencia, tt.recenciaDias)
			}
			if seg != tt.wantSeg {
				t.Errorf("segment at recencia=%d: got %q, want %q", tt.recenciaDias, seg, tt.wantSeg)
			}
		})
	}
}

// TestEstadoPagoFor covers all branches of the estadoPagoFor pure function.
// All inputs are deterministic UTC times to guarantee no TZ sensitivity.
func TestEstadoPagoFor(t *testing.T) {
	t.Parallel()

	baseNow := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	zero := time.Time{}
	recent := baseNow.AddDate(0, 0, -15) // 15 days ago — within 30d threshold
	mid := baseNow.AddDate(0, 0, -60)    // 60 days ago — between 30 and 90
	old := baseNow.AddDate(0, 0, -120)   // 120 days ago — beyond 90d threshold

	tests := []struct {
		name            string
		saldo           decimal.Decimal
		fechaUltimoPago time.Time
		want            domain.EstadoPago
	}{
		// saldo == 0 branch
		{"saldo zero and no payment date → SIN_CREDITO", decimal.Zero, zero, domain.EstadoPagoSinCredito},
		{"saldo zero and has payment date → LIQUIDADO", decimal.Zero, recent, domain.EstadoPagoLiquidado},
		// saldo > 0 branch
		{"saldo positive, paid 15d ago → AL_CORRIENTE", decimal.NewFromInt(500), recent, domain.EstadoPagoAlCorriente},
		{"saldo positive, paid exactly at umbralAlCorrienteDias → AL_CORRIENTE", decimal.NewFromInt(500), baseNow.AddDate(0, 0, -30), domain.EstadoPagoAlCorriente},
		{"saldo positive, paid 31d ago → ATRASADO", decimal.NewFromInt(500), baseNow.AddDate(0, 0, -31), domain.EstadoPagoAtrasado},
		{"saldo positive, paid 60d ago → ATRASADO", decimal.NewFromInt(500), mid, domain.EstadoPagoAtrasado},
		{"saldo positive, paid exactly at umbralAtrasadoDias → ATRASADO", decimal.NewFromInt(500), baseNow.AddDate(0, 0, -90), domain.EstadoPagoAtrasado},
		{"saldo positive, paid 91d ago → MOROSO", decimal.NewFromInt(500), baseNow.AddDate(0, 0, -91), domain.EstadoPagoMoroso},
		{"saldo positive, paid 120d ago → MOROSO", decimal.NewFromInt(500), old, domain.EstadoPagoMoroso},
		{"saldo positive, no payment date → MOROSO", decimal.NewFromInt(500), zero, domain.EstadoPagoMoroso},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := app.ExportEstadoPagoFor(tc.saldo, tc.fechaUltimoPago, baseNow)
			assert.Equal(t, tc.want, got, "saldo=%s fechaUltimoPago=%v", tc.saldo, tc.fechaUltimoPago)
		})
	}
}
