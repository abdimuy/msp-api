//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// testPulsoNow is a fixed reference time for pulse tests.
var testPulsoNow = time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)

// makePulsoCandidato builds a candidato with known scoring characteristics.
// recenciaDias=400 → lapsed (FRIO or DORMIDO_VALIOSO depending on monetary/por_liquidar),
// monetary=$5000, no saldo → SIN_CREDITO, telefono set.
func makePulsoCandidato(clienteID int, monetary string, recenciaDias int) *domain.WinbackCandidato {
	lastPurchase := testPulsoNow.AddDate(0, 0, -recenciaDias)
	return mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:         clienteID,
		Nombre:            "Test Pulso",
		Zona:              "Z1",
		Telefono:          "555-0001",
		FechaUltimaCompra: lastPurchase,
		Frecuencia:        3,
		Monetary:          decimal.RequireFromString(monetary),
		Saldo:             decimal.Zero,
		PorLiquidarPct:    decimal.Zero,
		CohorteFecha:      testPulsoNow.AddDate(-1, 0, 0),
		Now:               testPulsoNow,
	})
}

// TestObtenerPulsoCliente_Found verifies that a materialized candidato
// returns a correct ClientePulsoContract with computed score/segmento/estado_pago.
func TestObtenerPulsoCliente_Found(t *testing.T) {
	t.Parallel()

	// recenciaDias=400 → in the [180,540] sweet spot → recenciaComp=1.0
	// monetary=25000, telefono set, saldo=0 (SIN_CREDITO, mult=0.85)
	// value=25000/50000=0.5, contact=1, porLiq=0
	// base=0.45*1.0 + 0.30*0.5 + 0.10*1.0 + 0.15*0 = 0.45+0.15+0.10 = 0.70
	// score=round(100*0.70*0.85)=round(59.5)=60
	c := makePulsoCandidato(42, "25000.00", 400)

	repo := newFakeWinbackRepo()
	repo.candidates = []*domain.WinbackCandidato{c}
	svc := app.NewService(repo, nil, fixedClock{testPulsoNow}, nil)

	pulse, err := svc.ObtenerPulsoCliente(context.Background(), 42)
	require.NoError(t, err)

	assert.Equal(t, 42, pulse.ClienteID)
	assert.Equal(t, "SIN_CREDITO", pulse.EstadoPago)
	assert.Equal(t, 400, pulse.RecenciaDias)
	assert.Equal(t, 3, pulse.Frecuencia)
	assert.True(t, decimal.RequireFromString("25000.00").Equal(pulse.Monetary),
		"monetary mismatch: got %s", pulse.Monetary)

	// Verify the score matches what computeSegmentoScore would yield.
	seg, score, recencia, ep := app.ExportComputeSegmentoScore(c, testPulsoNow)
	assert.Equal(t, seg.String(), pulse.Segmento, "segmento mismatch")
	assert.Equal(t, score.Int(), pulse.Score, "score mismatch")
	assert.Equal(t, recencia, pulse.RecenciaDias, "recencia mismatch")
	assert.Equal(t, ep.String(), pulse.EstadoPago, "estado_pago mismatch")

	// FechaUltimaCompra must be the last-purchase date (non-zero).
	assert.False(t, pulse.FechaUltimaCompra.IsZero(), "FechaUltimaCompra must be non-zero")

	t.Logf("pulse: clienteID=%d score=%d segmento=%s estado=%s recencia=%d",
		pulse.ClienteID, pulse.Score, pulse.Segmento, pulse.EstadoPago, pulse.RecenciaDias)
}

// TestObtenerPulsoCliente_NotFound verifies that a missing candidato returns
// domain.ErrWinbackCandidatoNotFound (as a typed apperror).
func TestObtenerPulsoCliente_NotFound(t *testing.T) {
	t.Parallel()

	repo := newFakeWinbackRepo()
	// repo.candidates is empty → any clienteID is absent
	svc := app.NewService(repo, nil, fixedClock{testPulsoNow}, nil)

	_, err := svc.ObtenerPulsoCliente(context.Background(), 99999)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrWinbackCandidatoNotFound,
		"expected ErrWinbackCandidatoNotFound")
}

// TestObtenerPulsosClientes_MixedPresence verifies that only materialized
// candidatos appear in the result map; absent ones are simply missing.
func TestObtenerPulsosClientes_MixedPresence(t *testing.T) {
	t.Parallel()

	c1 := makePulsoCandidato(1, "10000.00", 400)
	c3 := makePulsoCandidato(3, "30000.00", 350)

	repo := newFakeWinbackRepo()
	repo.candidates = []*domain.WinbackCandidato{c1, c3}
	svc := app.NewService(repo, nil, fixedClock{testPulsoNow}, nil)

	// Request clienteIDs 1, 2, 3 — only 1 and 3 are materialized.
	result, err := svc.ObtenerPulsosClientes(context.Background(), []int{1, 2, 3})
	require.NoError(t, err)

	assert.Len(t, result, 2, "only materialized clients appear in the map")
	assert.Contains(t, result, 1, "clienteID=1 must be present")
	assert.Contains(t, result, 3, "clienteID=3 must be present")
	assert.NotContains(t, result, 2, "clienteID=2 must be absent (not materialized)")

	// Validate each pulse has a non-empty segmento and valid score.
	for id, pulse := range result {
		assert.NotEmpty(t, pulse.Segmento, "clienteID=%d: segmento must be non-empty", id)
		assert.GreaterOrEqual(t, pulse.Score, 0, "clienteID=%d: score must be >= 0", id)
		assert.LessOrEqual(t, pulse.Score, 100, "clienteID=%d: score must be <= 100", id)
		assert.Equal(t, id, pulse.ClienteID, "clienteID mismatch in pulse struct")
	}
}

// TestObtenerPulsosClientes_EmptyInput verifies that an empty input
// returns an empty (non-nil) map immediately without calling the repo.
func TestObtenerPulsosClientes_EmptyInput(t *testing.T) {
	t.Parallel()

	repo := newFakeWinbackRepo()
	svc := app.NewService(repo, nil, fixedClock{testPulsoNow}, nil)

	result, err := svc.ObtenerPulsosClientes(context.Background(), []int{})
	require.NoError(t, err)
	assert.NotNil(t, result, "result must be non-nil even for empty input")
	assert.Empty(t, result, "result must be empty for empty input")
}

// TestObtenerPulsosClientes_AllAbsent verifies that when none of the requested
// clienteIDs are materialized, the result is an empty map (no error).
func TestObtenerPulsosClientes_AllAbsent(t *testing.T) {
	t.Parallel()

	repo := newFakeWinbackRepo()
	// repo.candidates is empty — no materialized candidatos
	svc := app.NewService(repo, nil, fixedClock{testPulsoNow}, nil)

	result, err := svc.ObtenerPulsosClientes(context.Background(), []int{100, 200, 300})
	require.NoError(t, err)
	assert.Empty(t, result, "no materialized clients → empty map")
}

// TestObtenerPulsoCliente_ScoreMatchesComputeSegmentoScore asserts that the pulse
// score/segmento/estado_pago are exactly what computeSegmentoScore yields for a
// known candidato, preventing drift between the mapper and the scoring function.
func TestObtenerPulsoCliente_ScoreMatchesComputeSegmentoScore(t *testing.T) {
	t.Parallel()

	// Use a candidato whose score is deterministic and well-understood.
	// recenciaDias=200 → in [180,540] → recenciaComp=1.0
	// monetary=40000, saldo=0 → SIN_CREDITO (mult=0.85)
	// value=40000/50000=0.8, contact=1, porLiq=0
	// base=0.45*1.0 + 0.30*0.8 + 0.10*1.0 = 0.45+0.24+0.10 = 0.79
	// score=round(100*0.79*0.85)=round(67.15)=67
	c := makePulsoCandidato(77, "40000.00", 200)

	repo := newFakeWinbackRepo()
	repo.candidates = []*domain.WinbackCandidato{c}
	svc := app.NewService(repo, nil, fixedClock{testPulsoNow}, nil)

	pulse, err := svc.ObtenerPulsoCliente(context.Background(), 77)
	require.NoError(t, err)

	seg, score, recencia, ep := app.ExportComputeSegmentoScore(c, testPulsoNow)
	assert.Equal(t, score.Int(), pulse.Score)
	assert.Equal(t, seg.String(), pulse.Segmento)
	assert.Equal(t, ep.String(), pulse.EstadoPago)
	assert.Equal(t, recencia, pulse.RecenciaDias)

	// Confirm the NextBestProduct is a primitive string from the candidato.
	assert.IsType(t, "", pulse.NextBestProduct)

	// pulse is typed as ClientePulsoContract by the service return type; no explicit assertion needed.
}
