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
		// Credit client with outstanding saldo and payment history
		mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         101,
			Nombre:            "Reyes Hernández",
			Zona:              "Norte",
			FechaUltimaCompra: now.AddDate(0, 0, -200),
			FechaUltimoPago:   now.AddDate(0, 0, -15),
			Frecuencia:        6,
			Monetary:          decimal.NewFromInt(28_000),
			Saldo:             decimal.NewFromInt(6_000),
			PorLiquidarPct:    decimal.NewFromFloat(35.0),
			PctPagosATiempo:   decimal.NewFromFloat(75.0),
			DiasAtrasoProm:    8,
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
		// Second credit client — liquidado (saldo=0 but has payment history)
		mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         103,
			Nombre:            "López Sánchez",
			Zona:              "Norte",
			FechaUltimaCompra: now.AddDate(0, 0, -180),
			FechaUltimoPago:   now.AddDate(0, 0, -5),
			Frecuencia:        8,
			Monetary:          decimal.NewFromInt(35_000),
			Saldo:             decimal.Zero, // fully paid
			PorLiquidarPct:    decimal.Zero,
			PctPagosATiempo:   decimal.NewFromFloat(90.0),
			DiasAtrasoProm:    2,
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
	// 101 (saldo>0) and 103 (liquidado, has payment history) both apply and land
	// in BAJO with the placeholder scorecard; 102 is contado-only → no_aplica.
	assert.Equal(t, 3, logInt(t, entry, "total"))
	assert.Equal(t, 2, logInt(t, entry, "bajo"))
	assert.Equal(t, 0, logInt(t, entry, "medio"))
	assert.Equal(t, 0, logInt(t, entry, "alto"))
	assert.Equal(t, 0, logInt(t, entry, "critico"))
	assert.Equal(t, 1, logInt(t, entry, "no_aplica"))
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
