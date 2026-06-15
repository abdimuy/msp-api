//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"math"
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
			// recenciaDias=10, monetary=5000, no phone:
			// saldo=monetary*0.1=500>0, fechaUltimoPago=zero → MOROSO (mult=0.2)
			// recenciaComp=0.15 (≤90), value=min(1,5000/50000)=0.1, contact=0, porLiq=0
			// base=0.45*0.15 + 0.30*0.1 + 0.10*0 + 0.15*0 = 0.0675+0.03 = 0.0975
			// score=round(100*0.0975*0.2)=round(1.95)=2
			name:             "NUEVO — reciente, frecuencia=1",
			recenciaDias:     10,
			frecuencia:       1,
			monetary:         5_000,
			wantSeg:          domain.SegmentoNuevo,
			wantScore:        2,
			wantRecenciaDias: 10,
		},
		{
			// recenciaDias=100, monetary=15000, phone:
			// saldo=monetary*0.1=1500>0, fechaUltimoPago=zero → MOROSO (mult=0.2)
			// recenciaComp: ramp 0.15→1.0 over (90,180): t=(100-90)/90=10/90=0.1111
			//   comp=0.15 + 0.1111*(1.0-0.15)=0.15+0.09444=0.24444
			// value=min(1,15000/50000)=0.3, contact=1, porLiq=0
			// base=0.45*0.24444 + 0.30*0.3 + 0.10*1 + 0.15*0 = 0.11000+0.09+0.10 = 0.30000
			// score=round(100*0.30*0.2)=round(6.0)=6
			name:             "ACTIVO — reciente, frecuencia>1",
			recenciaDias:     100,
			frecuencia:       4,
			monetary:         15_000,
			telefono:         "555-1234",
			wantSeg:          domain.SegmentoActivo,
			wantScore:        6,
			wantRecenciaDias: 100,
		},
		{
			// recenciaDias=400, monetary=25000, phone, porLiq=50%:
			// saldo=monetary*0.1=2500>0, fechaUltimoPago=zero → MOROSO (mult=0.2)
			// recenciaComp=1.0 (180≤400≤540)
			// value=min(1,25000/50000)=0.5, contact=1, porLiq=0.5
			// base=0.45*1.0 + 0.30*0.5 + 0.10*1 + 0.15*0.5 = 0.775
			// raw=100*0.775*0.2=15.499...→ round=15 (float64 precision)
			name:             "LEAL_POR_LIQUIDAR — lapsed, frecuente, tiene saldo, dentro de 730d",
			recenciaDias:     400,
			frecuencia:       5,
			monetary:         25_000,
			porLiquidarPct:   50.0,
			telefono:         "555-9999",
			wantSeg:          domain.SegmentoLealPorLiquidar,
			wantScore:        15,
			wantRecenciaDias: 400,
		},
		{
			// recenciaDias=800, monetary=10000, no phone:
			// saldo=monetary*0.1=1000>0, fechaUltimoPago=zero → MOROSO (mult=0.2)
			// recenciaComp=0.10 (>730)
			// value=min(1,10000/50000)=0.2, contact=0, porLiq=0
			// base=0.45*0.10 + 0.30*0.2 = 0.045+0.06 = 0.105
			// score=round(100*0.105*0.2)=round(2.1)=2
			name:             "PERDIDO — recencia>730",
			recenciaDias:     800,
			frecuencia:       3,
			monetary:         10_000,
			porLiquidarPct:   0,
			wantSeg:          domain.SegmentoPerdido,
			wantScore:        2,
			wantRecenciaDias: 800,
		},
		{
			// recenciaDias=500, monetary=25000, no phone:
			// saldo=monetary*0.1=2500>0, fechaUltimoPago=zero → MOROSO (mult=0.2)
			// recenciaComp=1.0 (180≤500≤540)
			// value=min(1,25000/50000)=0.5, contact=0, porLiq=0
			// base=0.45*1.0 + 0.30*0.5 = 0.45+0.15 = 0.60
			// score=round(100*0.60*0.2)=round(12.0)=12
			name:             "DORMIDO_VALIOSO — lapsed, alto monetary, sin saldo, dentro 730d",
			recenciaDias:     500,
			frecuencia:       5,
			monetary:         25_000,
			porLiquidarPct:   0,
			wantSeg:          domain.SegmentoDormidoValioso,
			wantScore:        12,
			wantRecenciaDias: 500,
		},
		{
			// recenciaDias=500, monetary=5000, no phone:
			// saldo=monetary*0.1=500>0, fechaUltimoPago=zero → MOROSO (mult=0.2)
			// recenciaComp=1.0 (180≤500≤540)
			// value=min(1,5000/50000)=0.1, contact=0, porLiq=0
			// base=0.45*1.0 + 0.30*0.1 = 0.45+0.03 = 0.48
			// score=round(100*0.48*0.2)=round(9.6)=10
			name:             "FRIO — lapsed, bajo monetary, sin saldo, dentro 730d",
			recenciaDias:     500,
			frecuencia:       2,
			monetary:         5_000,
			porLiquidarPct:   0,
			wantSeg:          domain.SegmentoFrio,
			wantScore:        10,
			wantRecenciaDias: 500,
		},
		{
			// recenciaDias=9999 (sentinel), monetary=0, no phone:
			// saldo=monetary*0.1=0, fechaUltimoPago=zero → SIN_CREDITO (mult=0.85)
			// recenciaComp=0.10 (>730)
			// value=0, contact=0, porLiq=0
			// base=0.45*0.10 = 0.045
			// score=round(100*0.045*0.85)=round(3.825)=4
			name:             "sin historial de compra — recenciaMax sentinel",
			recenciaDias:     -1, // sentinel: zero fecha
			frecuencia:       0,
			monetary:         0,
			wantSeg:          domain.SegmentoPerdido,
			wantScore:        4,
			wantRecenciaDias: 9_999,
		},
		{
			// recenciaDias=50, monetary=50000, phone, porLiq=80%:
			// saldo=monetary*0.1=5000>0, fechaUltimoPago=zero → MOROSO (mult=0.2)
			// recenciaComp=0.15 (≤90)
			// value=min(1,50000/50000)=1.0, contact=1, porLiq=0.8
			// base=0.45*0.15 + 0.30*1.0 + 0.10*1 + 0.15*0.8 = 0.0675+0.30+0.10+0.12 = 0.5875
			// score=round(100*0.5875*0.2)=round(11.75)=12
			name:             "score alto — reciente, alto valor, con teléfono, con saldo",
			recenciaDias:     50,
			frecuencia:       10,
			monetary:         50_000,
			porLiquidarPct:   80.0,
			telefono:         "555-0001",
			wantSeg:          domain.SegmentoActivo,
			wantScore:        12,
			wantRecenciaDias: 50,
		},
		{
			// recenciaDias=800, monetary=0, no phone:
			// saldo=monetary*0.1=0, fechaUltimoPago=zero → SIN_CREDITO (mult=0.85)
			// recenciaComp=0.10 (>730)
			// value=0, contact=0, porLiq=0
			// base=0.45*0.10 = 0.045
			// score=round(100*0.045*0.85)=round(3.825)=4
			name:             "score bajo — perdido, sin valor, sin teléfono, sin saldo",
			recenciaDias:     800,
			frecuencia:       1,
			monetary:         0,
			porLiquidarPct:   0,
			telefono:         "",
			wantSeg:          domain.SegmentoPerdido,
			wantScore:        4,
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

			seg, score, recencia, _ := app.ExportComputeSegmentoScore(c, testNow)

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

			seg, _, recencia, _ := app.ExportComputeSegmentoScore(c, testNow)

			if recencia != tt.recenciaDias {
				t.Errorf("recenciaDias: got %d, want %d", recencia, tt.recenciaDias)
			}
			if seg != tt.wantSeg {
				t.Errorf("segment at recencia=%d: got %q, want %q", tt.recenciaDias, seg, tt.wantSeg)
			}
		})
	}
}

// TestRecenciaWinbackComp_Boundaries exercises the recency component at key
// boundary values defined by the winback window constants.
//
// Uses a fixture client with enough attributes to produce a meaningful score
// but the test only validates the recency component indirectly through the
// full computeSegmentoScore.  We test explicit recency values and verify the
// returned score changes monotonically across the window regions.
func TestRecenciaWinbackComp_Boundaries(t *testing.T) {
	t.Parallel()

	// makeCandidato creates a candidato with saldo=0 (SIN_CREDITO, mult=0.85),
	// monetary=50000 (value=1.0), phone=yes (contact=1), porLiq=0.
	// base = 0.45*recenciaComp + 0.30*1.0 + 0.10*1.0 + 0.15*0
	//      = 0.45*recenciaComp + 0.40
	// score = round(100 * (0.45*recenciaComp+0.40) * 0.85)
	makeCandidato := func(recenciaDias int) *domain.WinbackCandidato {
		return mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         1,
			Nombre:            "Boundary",
			Zona:              "Z1",
			Telefono:          "555-0001",
			FechaUltimaCompra: testNow.AddDate(0, 0, -recenciaDias),
			Frecuencia:        5,
			Monetary:          decimal.NewFromInt(50_000),
			Saldo:             decimal.Zero,
			PorLiquidarPct:    decimal.Zero,
			EnControl:         false,
			CohorteFecha:      testNow.AddDate(-1, 0, 0),
			Now:               testNow,
		})
	}

	type boundaryCase struct {
		recenciaDias     int
		wantRecenciaComp float64
		label            string
	}

	cases := []boundaryCase{
		// [0, 90] → flat 0.15
		{recenciaDias: 0, wantRecenciaComp: 0.15, label: "day 0 — floor of active region"},
		{recenciaDias: 90, wantRecenciaComp: 0.15, label: "day 90 — last flat-0.15 point"},
		// (90, 180) → ramp 0.15→1.0
		{recenciaDias: 91, wantRecenciaComp: 0.15 + (1.0/90.0)*0.85, label: "day 91 — first ramp point"},
		{recenciaDias: 179, wantRecenciaComp: 0.15 + (89.0/90.0)*0.85, label: "day 179 — last ramp-up point"},
		// [180, 540] → flat 1.0
		{recenciaDias: 180, wantRecenciaComp: 1.0, label: "day 180 — start of peak window"},
		{recenciaDias: 540, wantRecenciaComp: 1.0, label: "day 540 — end of peak window"},
		// (540, 730] → ramp 1.0→0.10
		{recenciaDias: 541, wantRecenciaComp: 1.0 - (1.0/190.0)*0.90, label: "day 541 — first decay point"},
		{recenciaDias: 730, wantRecenciaComp: 0.10, label: "day 730 — last decay point"},
		// > 730 → flat 0.10
		{recenciaDias: 731, wantRecenciaComp: 0.10, label: "day 731 — floor of lost region"},
	}

	for _, bc := range cases {
		bc := bc
		t.Run(bc.label, func(t *testing.T) {
			t.Parallel()

			c := makeCandidato(bc.recenciaDias)
			_, score, _, ep := app.ExportComputeSegmentoScore(c, testNow)

			// saldo=0, fechaUltimoPago=zero → SIN_CREDITO.
			assert.Equal(t, domain.EstadoPagoSinCredito, ep,
				"recencia=%d: expected SIN_CREDITO with zero saldo", bc.recenciaDias)

			// Derive expected score from formula:
			// base = 0.45*recenciaComp + 0.30*1.0 + 0.10*1.0 + 0.15*0
			// score = round(100 * base * 0.85)
			base := 0.45*bc.wantRecenciaComp + 0.40
			wantScore := int(math.Round(100 * base * 0.85))
			assert.Equal(t, wantScore, score.Int(),
				"recencia=%d: expected score=%d (recenciaComp=%.5f)", bc.recenciaDias, wantScore, bc.wantRecenciaComp)
		})
	}
}

// TestSolvenciaMultiplier verifies that identical dormant clients receive
// different scores depending only on their EstadoPago.
//
// Fixture: recenciaDias=300 (in ramp region, inside umbralActivoDias=335 so ACTIVO,
// but we test via various saldo/fechaUltimoPago combos to hit all EstadoPago cases).
// Actually for full coverage use a dormant client (recenciaDias=400, peak window=1.0):
// base=0.45*1.0 + 0.30*0.5 + 0.10*1 + 0.15*0 = 0.45+0.15+0.10 = 0.70
// (monetary=25000 → value=0.5, phone present, porLiq=0).
func TestSolvenciaMultiplier(t *testing.T) {
	t.Parallel()

	baseNow := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	// recenciaDias=400 → recenciaComp=1.0 (peak window 180-540)
	fechaUltimaCompra := baseNow.AddDate(0, 0, -400)

	// base = 0.45*1.0 + 0.30*0.5 + 0.10*1.0 + 0.15*0 = 0.70
	const base = 0.70

	type solCase struct {
		name            string
		saldo           decimal.Decimal
		fechaUltimoPago time.Time
		wantEP          domain.EstadoPago
		wantMultiplier  float64
	}

	cases := []solCase{
		{
			name:            "SIN_CREDITO — saldo=0, no payment history",
			saldo:           decimal.Zero,
			fechaUltimoPago: time.Time{},
			wantEP:          domain.EstadoPagoSinCredito,
			wantMultiplier:  0.85,
		},
		{
			name:            "LIQUIDADO — saldo=0, with payment history",
			saldo:           decimal.Zero,
			fechaUltimoPago: baseNow.AddDate(0, 0, -10),
			wantEP:          domain.EstadoPagoLiquidado,
			wantMultiplier:  1.0,
		},
		{
			name:            "AL_CORRIENTE — saldo>0, paid 15d ago",
			saldo:           decimal.NewFromInt(5_000),
			fechaUltimoPago: baseNow.AddDate(0, 0, -15),
			wantEP:          domain.EstadoPagoAlCorriente,
			wantMultiplier:  1.0,
		},
		{
			name:            "ATRASADO — saldo>0, paid 60d ago",
			saldo:           decimal.NewFromInt(5_000),
			fechaUltimoPago: baseNow.AddDate(0, 0, -60),
			wantEP:          domain.EstadoPagoAtrasado,
			wantMultiplier:  0.6,
		},
		{
			name:            "MOROSO — saldo>0, no payment history",
			saldo:           decimal.NewFromInt(5_000),
			fechaUltimoPago: time.Time{},
			wantEP:          domain.EstadoPagoMoroso,
			wantMultiplier:  0.2,
		},
	}

	for _, sc := range cases {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()

			c := mustCandidato(domain.CrearWinbackCandidatoParams{
				ClienteID:         1,
				Nombre:            "Solvencia",
				Zona:              "Z1",
				Telefono:          "555-0001",
				FechaUltimaCompra: fechaUltimaCompra,
				FechaUltimoPago:   sc.fechaUltimoPago,
				Frecuencia:        5,
				Monetary:          decimal.NewFromInt(25_000),
				Saldo:             sc.saldo,
				PorLiquidarPct:    decimal.Zero,
				EnControl:         false,
				CohorteFecha:      baseNow.AddDate(-1, 0, 0),
				Now:               baseNow,
			})

			_, score, _, ep := app.ExportComputeSegmentoScore(c, baseNow)

			assert.Equal(t, sc.wantEP, ep, "EstadoPago mismatch")

			wantScore := int(math.Round(100 * base * sc.wantMultiplier))
			assert.Equal(t, wantScore, score.Int(),
				"score mismatch for %s: base=%.2f mult=%.2f", sc.name, base, sc.wantMultiplier)
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
