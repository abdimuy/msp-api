//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

func TestLogDistribucionBandasCredito_NoError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)

	repo := newFakeWinbackRepo()
	repo.candidates = []*domain.WinbackCandidato{
		// Credit client with outstanding saldo and strong payment history.
		// v1 scorecard: low DIAS_SIN_PAGAR, high PAGOS_90D, high PCT_PAGOS_A_TIEMPO_6M,
		// long history → low risk → BAJO band.
		mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         101,
			Nombre:            "Reyes Hernández",
			Zona:              "Norte",
			FechaUltimaCompra: now.AddDate(0, 0, -200),
			FechaUltimoPago:   now.AddDate(0, 0, -10),
			FechaPrimerCargo:  now.AddDate(-3, 0, 0), // 3 years history
			Frecuencia:        6,
			Monetary:          decimal.NewFromInt(28_000),
			Saldo:             decimal.NewFromInt(6_000),
			PorLiquidarPct:    decimal.NewFromFloat(35.0),
			PctPagosATiempo:   decimal.NewFromFloat(95.0),
			NumPagos:          100,
			Pagos90D:          10,
			CadenciaDias:      25,
			DiasAtrasoProm:    2,
			CohorteFecha:      now.AddDate(-1, 0, 0),
			Now:               now,
		}),
		// Contado-only client — SIN_CREDITO → no aplica
		mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         102,
			Nombre:            "Martínez Gómez",
			Zona:              "Sur",
			FechaUltimaCompra: now.AddDate(0, 0, -350),
			Frecuencia:        3,
			Monetary:          decimal.NewFromInt(12_000),
			Saldo:             decimal.Zero,
			PorLiquidarPct:    decimal.Zero,
			CohorteFecha:      now.AddDate(-1, 0, 0),
			Now:               now,
			// FechaUltimoPago zero → SIN_CREDITO
		}),
		// Second credit client — liquidado (saldo=0 but has payment history).
		// v1 scorecard: low DIAS_SIN_PAGAR, high PAGOS_90D, long history → BAJO.
		mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         103,
			Nombre:            "López Sánchez",
			Zona:              "Norte",
			FechaUltimaCompra: now.AddDate(0, 0, -180),
			FechaUltimoPago:   now.AddDate(0, 0, -5),
			FechaPrimerCargo:  now.AddDate(-2, 0, 0), // 2 years history
			Frecuencia:        8,
			Monetary:          decimal.NewFromInt(35_000),
			Saldo:             decimal.Zero, // fully paid
			PorLiquidarPct:    decimal.Zero,
			PctPagosATiempo:   decimal.NewFromFloat(95.0),
			NumPagos:          80,
			Pagos90D:          8,
			CadenciaDias:      25,
			DiasAtrasoProm:    1,
			CohorteFecha:      now.AddDate(-1, 0, 0),
			Now:               now,
		}),
	}

	micro := &fakeMicrosipReader{}

	// Capture the structured log so we can assert the actual band distribution,
	// not merely that the call doesn't panic.
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	svc := app.NewService(repo, micro, fixedClock{now}, nil).WithLogger(logger)

	assert.NotPanics(t, func() {
		svc.LogDistribucionBandasCredito(context.Background())
	})

	entry := lastDistributionLog(t, &buf)
	// Credit score applies only to clients with current outstanding balance who
	// are performing (paid within 90d): only 101 (saldo>0, paid 10d ago) qualifies.
	// 102 is contado (saldo 0) and 103 is liquidado (saldo 0) → both no_aplica.
	// We assert aggregate counts (aplica vs no_aplica) rather than exact band
	// buckets, since band membership depends on the embedded scorecard weights.
	total := logInt(t, entry, "total")
	noAplica := logInt(t, entry, "no_aplica")
	assert.Equal(t, 3, total, "total must equal number of candidates")
	assert.Equal(t, 2, noAplica, "102 (contado) and 103 (liquidado, saldo 0) → no_aplica")
	// The one performing ower (101) must land in a band.
	aplica := total - noAplica
	bandSum := logInt(t, entry, "bajo") + logInt(t, entry, "medio") +
		logInt(t, entry, "alto") + logInt(t, entry, "critico")
	assert.Equal(t, aplica, bandSum, "band counts must sum to number of applicable clients")
}

// lastDistributionLog parses the JSON log buffer and returns the attributes of
// the analytics.credito_banda_distribution entry. JSON numbers decode to float64.
func lastDistributionLog(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var found map[string]any
	dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	for {
		var entry map[string]any
		if err := dec.Decode(&entry); err != nil {
			break
		}
		if entry["msg"] == "analytics.credito_banda_distribution" {
			found = entry
		}
	}
	require.NotNil(t, found, "expected a credito_banda_distribution log line")
	return found
}

// logInt extracts an integer slog attribute from a decoded JSON log entry
// (JSON numbers decode to float64; we assert exact integer counts).
func logInt(t *testing.T, entry map[string]any, key string) int {
	t.Helper()
	v, ok := entry[key].(float64)
	require.Truef(t, ok, "log key %q is not a number: %v", key, entry[key])
	return int(v)
}

func TestLogDistribucionBandasCredito_EmptyRepo(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)

	repo := newFakeWinbackRepo()
	// No candidates seeded — empty page

	micro := &fakeMicrosipReader{}
	svc := app.NewService(repo, micro, fixedClock{now}, nil)

	assert.NotPanics(t, func() {
		svc.LogDistribucionBandasCredito(context.Background())
	})
}
