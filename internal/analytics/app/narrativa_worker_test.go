//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/infra/llm/llmfake"
	"github.com/abdimuy/msp-api/internal/analytics/infra/narrativamem"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/llm"
)

// ─── Fake pulsoLoader ─────────────────────────────────────────────────────────

// fakePulsoLoader is a test double for the pulsoLoader interface.
// It returns the configured candidate/comp or error.
type fakePulsoLoader struct {
	c    *domain.WinbackCandidato
	comp analytics.PulsoComputado
	Nota string
	err  error
}

func (f *fakePulsoLoader) candidatoYPulso(_ context.Context, _ int) (*domain.WinbackCandidato, analytics.PulsoComputado, string, error) {
	return f.c, f.comp, f.Nota, f.err
}

// ─── Test helpers ─────────────────────────────────────────────────────────────

// testNarrativaWorkerNow is the fixed reference time used across worker tests.
var testNarrativaWorkerNow = time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)

// lowRiskCompWorker returns a PulsoComputado for a low-risk client (passes direction check).
func lowRiskCompWorker() analytics.PulsoComputado {
	return analytics.PulsoComputado{
		TierRiesgo:   domain.TierRiesgoAlDia.String(),
		EstadoPago:   domain.EstadoPagoAlCorriente.String(),
		BandaCredito: domain.BandaCreditoBajo.String(),
	}
}

// highRiskCompWorker returns a PulsoComputado for a CRITICO-tier client.
// A "good payer" narrativa from the LLM would fail the direction check.
func highRiskCompWorker() analytics.PulsoComputado {
	return analytics.PulsoComputado{
		TierRiesgo:   domain.TierRiesgoCritico.String(),
		EstadoPago:   domain.EstadoPagoMoroso.String(),
		BandaCredito: domain.BandaCreditoCritico.String(),
	}
}

// validNarrativaText is a >40-rune Spanish paragraph without forbidden phrases.
const validNarrativaText = "Este cliente mantiene un comportamiento de pago consistente y su historial muestra una relación comercial estable con la empresa."

// makeWorkerCandidato builds a minimal WinbackCandidato for worker tests.
func makeWorkerCandidato(clienteID int) *domain.WinbackCandidato {
	now := testNarrativaWorkerNow
	c, err := domain.CrearWinbackCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:         clienteID,
		Nombre:            "Test Worker",
		Zona:              "Z1",
		Telefono:          "555-0001",
		FechaUltimaCompra: now.AddDate(0, -1, 0),
		Frecuencia:        3,
		Monetary:          decimal.RequireFromString("15000.00"),
		Saldo:             decimal.Zero,
		PorLiquidarPct:    decimal.Zero,
		CohorteFecha:      now.AddDate(-2, 0, 0),
		Now:               now,
	})
	if err != nil {
		panic("makeWorkerCandidato: " + err.Error())
	}
	return c
}

// buildWorker constructs a NarrativaWorker wired with the given fakes.
func buildWorker(loader pulsoLoader, repo outbound.NarrativaRepo, gen outbound.NarrativeGenerator, enabled bool) *NarrativaWorker {
	return NewNarrativaWorker(
		loader,
		repo,
		gen,
		fixedClock{t: testNarrativaWorkerNow},
		NarrativaWorkerConfig{
			Interval:  time.Hour, // long enough that ticker never fires in tests
			BatchSize: 10,
			Model:     "test-model-v1",
			Enabled:   enabled,
		},
		nil,
	)
}

// ─── Test cases ───────────────────────────────────────────────────────────────

// TestNarrativaWorker_GeneratesValidatesAndCaches verifies the happy path:
// enqueue a client, run tick, expect the repo to contain a valid narrativa row
// with Texto, filtered Rasgos (invalid code dropped + deduped), correct InputHash
// and Modelo, and the pending queue to be empty.
func TestNarrativaWorker_GeneratesValidatesAndCaches(t *testing.T) {
	t.Parallel()

	const clienteID = 101
	comp := lowRiskCompWorker()
	c := makeWorkerCandidato(clienteID)

	repo := narrativamem.New()
	require.NoError(t, repo.Encolar(context.Background(), clienteID, NarrativaInputHash(comp, "")))

	gen := &llmfake.Generator{
		Out: outbound.NarrativeOutput{
			Narrativa: validNarrativaText,
			// "NOT_A_CODE" is invalid; "loyal_but_stagnant" appears twice → deduped to one.
			Rasgos: []string{"loyal_but_stagnant", "NOT_A_CODE", "loyal_but_stagnant"},
		},
	}
	loader := &fakePulsoLoader{c: c, comp: comp}
	w := buildWorker(loader, repo, gen, true)

	w.tick(context.Background())

	// Queue must be empty.
	assert.Equal(t, 0, repo.PendientesCount(), "pending queue should be empty after successful generation")

	// Narrativa row must exist.
	row, err := repo.GetNarrativa(context.Background(), clienteID)
	require.NoError(t, err)
	require.NotNil(t, row, "narrativa row should be persisted")

	assert.Equal(t, validNarrativaText, row.Texto, "Texto should match the valid narrativa")
	assert.Equal(t, []string{"loyal_but_stagnant"}, row.Rasgos, "Rasgos: invalid dropped and deduped")
	assert.Equal(t, NarrativaInputHash(comp, ""), row.InputHash, "InputHash must equal NarrativaInputHash(comp)")
	assert.Equal(t, "test-model-v1", row.Modelo, "Modelo must match config")
}

// TestNarrativaWorker_DisabledIsNoOp verifies that when Enabled=false, Start
// returns nil without launching a goroutine, and running remains false.
func TestNarrativaWorker_DisabledIsNoOp(t *testing.T) {
	t.Parallel()

	repo := narrativamem.New()
	gen := &llmfake.Generator{}
	loader := &fakePulsoLoader{}
	w := buildWorker(loader, repo, gen, false)

	ctx := context.Background()
	err := w.Start(ctx)
	require.NoError(t, err, "Start on disabled worker must return nil")

	// running flag must remain false — no goroutine launched.
	w.mu.Lock()
	running := w.running
	w.mu.Unlock()
	assert.False(t, running, "disabled worker must not set running=true")
}

// TestNarrativaWorker_TransientGenError_LeftInQueue verifies that a transient
// LLM error leaves the pending entry in the queue without creating any row.
func TestNarrativaWorker_TransientGenError_LeftInQueue(t *testing.T) {
	t.Parallel()

	const clienteID = 102
	comp := lowRiskCompWorker()
	c := makeWorkerCandidato(clienteID)

	repo := narrativamem.New()
	require.NoError(t, repo.Encolar(context.Background(), clienteID, NarrativaInputHash(comp, "")))

	// Wrap a sentinel so llm.IsTransient returns true.
	transientErr := &llm.TransientError{Cause: errors.New("connection timeout")}
	gen := &llmfake.Generator{Err: transientErr}
	loader := &fakePulsoLoader{c: c, comp: comp}
	w := buildWorker(loader, repo, gen, true)

	w.tick(context.Background())

	// Pending entry must still be in the queue.
	assert.Equal(t, 1, repo.PendientesCount(), "pending entry must remain for transient error")

	// No narrativa row must be created.
	assert.Equal(t, 0, repo.NarrativaCount(), "no narrativa row must be created on transient error")
}

// TestNarrativaWorker_PermanentGenError_Dropped verifies that a permanent LLM
// error (ErrLLMDisabled) removes the pending entry without creating any row.
func TestNarrativaWorker_PermanentGenError_Dropped(t *testing.T) {
	t.Parallel()

	const clienteID = 103
	comp := lowRiskCompWorker()
	c := makeWorkerCandidato(clienteID)

	repo := narrativamem.New()
	require.NoError(t, repo.Encolar(context.Background(), clienteID, NarrativaInputHash(comp, "")))

	gen := &llmfake.Generator{Err: llm.ErrLLMDisabled}
	loader := &fakePulsoLoader{c: c, comp: comp}
	w := buildWorker(loader, repo, gen, true)

	w.tick(context.Background())

	// Pending entry must be removed (permanent → dropped).
	assert.Equal(t, 0, repo.PendientesCount(), "pending entry must be removed for permanent error")

	// No narrativa row must be created.
	assert.Equal(t, 0, repo.NarrativaCount(), "no narrativa row must be created on permanent error")
}

// TestNarrativaWorker_FallbackPath verifies that a contradictory "good payer"
// narrativa for a CRITICO client results in an empty Texto row (negative cache)
// with no Rasgos, correct InputHash, and queue empty.
func TestNarrativaWorker_FallbackPath(t *testing.T) {
	t.Parallel()

	const clienteID = 104
	comp := highRiskCompWorker()
	c := makeWorkerCandidato(clienteID)

	repo := narrativamem.New()
	require.NoError(t, repo.Encolar(context.Background(), clienteID, NarrativaInputHash(comp, "")))

	// A contradictory "good payer" paragraph — will fail direction check.
	gen := &llmfake.Generator{
		Out: outbound.NarrativeOutput{
			Narrativa: "Es un excelente pagador que siempre paga a tiempo y tiene bajo riesgo crediticio.",
			Rasgos:    []string{"steady_reliable"},
		},
	}
	loader := &fakePulsoLoader{c: c, comp: comp}
	w := buildWorker(loader, repo, gen, true)

	w.tick(context.Background())

	// Queue must be empty.
	assert.Equal(t, 0, repo.PendientesCount(), "pending queue should be empty after fallback")

	// Narrativa row must exist with empty Texto (negative cache).
	row, err := repo.GetNarrativa(context.Background(), clienteID)
	require.NoError(t, err)
	require.NotNil(t, row, "fallback row must be persisted")

	assert.Empty(t, row.Texto, "Texto must be empty on direction-check failure")
	assert.Empty(t, row.Rasgos, "Rasgos must be empty on direction-check failure")
	assert.Equal(t, NarrativaInputHash(comp, ""), row.InputHash, "InputHash must be set even on fallback")
}

// TestNarrativaWorker_CandidateNotFound_Removed verifies that a not-found error
// from the loader removes the pending entry without creating any row.
func TestNarrativaWorker_CandidateNotFound_Removed(t *testing.T) {
	t.Parallel()

	const clienteID = 105

	repo := narrativamem.New()
	require.NoError(t, repo.Encolar(context.Background(), clienteID, "somehash"))

	gen := &llmfake.Generator{}
	// Loader returns not-found (same error the real repo returns).
	loader := &fakePulsoLoader{err: domain.ErrWinbackCandidatoNotFound}
	w := buildWorker(loader, repo, gen, true)

	w.tick(context.Background())

	// Pending entry must be removed.
	assert.Equal(t, 0, repo.PendientesCount(), "pending entry must be removed when candidate not found")

	// No narrativa row must be created.
	assert.Equal(t, 0, repo.NarrativaCount(), "no narrativa row must be created for missing candidate")
}

// TestNarrativaWorker_ContextoOperativo verifies that:
//   - the worker passes loader.Nota to the generator (via NarrativeInput.Nota),
//   - the generator's ContextoOperativo is persisted in the narrativa row, and
//   - the row hash equals NarrativaInputHash(comp, nota).
func TestNarrativaWorker_ContextoOperativo(t *testing.T) {
	t.Parallel()

	const clienteID = 201
	const nota = "acuerdo con Carmelo"
	const contextoOut = "paga con Carmelo"

	comp := lowRiskCompWorker()
	c := makeWorkerCandidato(clienteID)

	repo := narrativamem.New()
	require.NoError(t, repo.Encolar(context.Background(), clienteID, NarrativaInputHash(comp, nota)))

	gen := &llmfake.Generator{
		Out: outbound.NarrativeOutput{
			Narrativa:         validNarrativaText,
			Rasgos:            []string{"loyal_but_stagnant"},
			ContextoOperativo: contextoOut,
		},
	}
	loader := &fakePulsoLoader{c: c, comp: comp, Nota: nota}
	w := buildWorker(loader, repo, gen, true)

	w.tick(context.Background())

	// Generator must have received the nota.
	require.Len(t, gen.Inputs, 1, "generator must have been called once")
	assert.Equal(t, nota, gen.Inputs[0].Nota, "generator must receive the loader's nota")

	// Narrativa row must persist ContextoOperativo.
	row, err := repo.GetNarrativa(context.Background(), clienteID)
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.Equal(t, contextoOut, row.ContextoOperativo, "ContextoOperativo must be persisted")

	// Row hash must be keyed on (comp, nota).
	assert.Equal(t, NarrativaInputHash(comp, nota), row.InputHash,
		"InputHash must equal NarrativaInputHash(comp, nota)")
}
