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

// recenciaNow is the fixed reference "now" for the recency adjustment tests.
var recenciaNow = time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)

// makeCobranzaCandidato builds a candidato carrying the materialized cobranza
// facts that ajustarCobranzaRecencia reads. diasSinPago < 0 means "never paid"
// (FechaUltimoPago left zero).
func makeCobranzaCandidato(saldo string, cadencia, numPagos, diasSinPago, diasAtrasoProm int, pct string) *domain.WinbackCandidato {
	fechaUltimoPago := time.Time{}
	if diasSinPago >= 0 {
		fechaUltimoPago = recenciaNow.AddDate(0, 0, -diasSinPago)
	}
	return mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:       1,
		Nombre:          "Cobranza Test",
		Zona:            "Z1",
		Saldo:           decimal.RequireFromString(saldo),
		CohorteFecha:    recenciaNow.AddDate(-1, 0, 0),
		Now:             recenciaNow,
		FechaUltimoPago: fechaUltimoPago,
		NumPagos:        numPagos,
		CadenciaDias:    cadencia,
		DiasAtrasoProm:  diasAtrasoProm,
		PctPagosATiempo: decimal.RequireFromString(pct),
	})
}

func TestAjustarCobranzaRecencia(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		c              *domain.WinbackCandidato
		wantDiasAtraso int
		// wantPctMax is an upper bound; the adjusted punctuality must be < this when
		// penalized, or == the historical value when unchanged (use wantPctExact then).
		wantPctExact string // "" → use wantPctBelow comparison
		wantPctBelow string // "" → skip
	}{
		{
			// Moroso: cadence 7d, last paid 120d ago, has balance and history.
			// diasSinPagar=120, atrasoActual=120-7=113, diasAtraso=max(1,113)=113.
			// missed=113/7=16, pct=88.5*52/(52+16)=67.67...
			name:           "moroso con hueco abierto largo penaliza atraso y puntualidad",
			c:              makeCobranzaCandidato("12000", 7, 52, 120, 1, "88.5"),
			wantDiasAtraso: 113,
			wantPctBelow:   "70",
		},
		{
			// Al día: cadence 7d, last paid 3d ago → atrasoActual=0 → no change.
			name:           "al dia conserva historico",
			c:              makeCobranzaCandidato("12000", 7, 52, 3, 4, "92"),
			wantDiasAtraso: 4,
			wantPctExact:   "92",
		},
		{
			// Saldo 0 (liquidado): never adjusted even with an old last payment.
			name:           "saldo cero conserva historico",
			c:              makeCobranzaCandidato("0", 7, 52, 120, 1, "88.5"),
			wantDiasAtraso: 1,
			wantPctExact:   "88.5",
		},
		{
			// No cadence known → leave historical values (EstadoPago flags MOROSO).
			name:           "sin cadencia conserva historico",
			c:              makeCobranzaCandidato("12000", 0, 52, 120, 1, "88.5"),
			wantDiasAtraso: 1,
			wantPctExact:   "88.5",
		},
		{
			// No payments recorded → leave historical values.
			name:           "sin pagos conserva historico",
			c:              makeCobranzaCandidato("12000", 7, 0, -1, 1, "88.5"),
			wantDiasAtraso: 1,
			wantPctExact:   "88.5",
		},
		{
			// Has balance but never paid (FechaUltimoPago zero) → leave historical.
			name:           "nunca pago conserva historico",
			c:              makeCobranzaCandidato("12000", 7, 5, -1, 1, "88.5"),
			wantDiasAtraso: 1,
			wantPctExact:   "88.5",
		},
		{
			// Historical atraso already exceeds the open gap → keep the larger one.
			// cadence 30d, last paid 35d ago → atrasoActual=5; diasAtrasoProm=40 wins.
			name:           "historico mayor que hueco se conserva",
			c:              makeCobranzaCandidato("12000", 30, 10, 35, 40, "75"),
			wantDiasAtraso: 40,
			wantPctBelow:   "76", // missed=5/30=0 → pct unchanged at 75 (< 76)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotDias, gotPct := app.ExportAjustarCobranzaRecencia(tt.c, recenciaNow)

			assert.Equal(t, tt.wantDiasAtraso, gotDias, "diasAtraso mismatch")

			if tt.wantPctExact != "" {
				want := decimal.RequireFromString(tt.wantPctExact)
				assert.Truef(t, want.Equal(gotPct), "pct mismatch: want %s got %s", want, gotPct)
			}
			if tt.wantPctBelow != "" {
				bound := decimal.RequireFromString(tt.wantPctBelow)
				assert.Truef(t, gotPct.LessThan(bound), "pct must be < %s, got %s", bound, gotPct)
			}
		})
	}
}
