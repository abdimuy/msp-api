// Integration tests for MSP_RX_DECISION (Fase 3a copiloto audit trail). All
// writes execute inside a transaction that always rolls back so the shared
// dev DB never accumulates test data.
//
// Prerequisites:
//   - FB_DATABASE env var pointing at the dev Microsip Firebird DB.
//   - Migration 000046 applied (creates MSP_RX_DECISION).
//
// Run: FB_DATABASE=/firebird/data/MUEBLERA.FDB go test ./internal/reactivacion/infra/reactivacionfb/...
//
//nolint:misspell // Spanish vocabulary (decisión) by convention.
package reactivacionfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionfb"
)

var decisionFixedNow = time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)

func findDecisionByID(ds []*domain.Decision, id string) *domain.Decision {
	for _, d := range ds {
		if d.ID() == id {
			return d
		}
	}
	return nil
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestDecisionRepo_InsertarAndListarPorCliente_Cronologico(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewCopilotoRepo(pool)

		const clienteID = 901000301
		primera, err := domain.CrearDecision(domain.CrearDecisionParams{
			ClienteID: clienteID,
			TurnoRef:  "turno-ref-1",
			Intencion: "pregunta por su saldo",
			Confianza: 82,
			Senales:   []string{"deuda", "confianza_baja"},
			Accion:    domain.AccionEscalar,
			Evidencia: []string{"cliente preguntó monto exacto"},
			RazonEscalamiento: "el cliente pide un monto que la política no permite " +
				"afirmar directamente",
			Resultado: domain.ResultadoEscalado,
			Now:       decisionFixedNow,
		})
		require.NoError(t, err)

		// Second decision: no Senales/Evidencia passed (nil) — domain.CrearDecision
		// defaults both to a non-nil empty slice, exercising the empty→"[]" round
		// trip end to end (never SQL NULL, never json "null").
		segunda, err := domain.CrearDecision(domain.CrearDecisionParams{
			ClienteID: clienteID,
			TurnoRef:  "turno-ref-2",
			Intencion: "saluda, sin intención clara",
			Confianza: 40,
			Accion:    domain.AccionResponder,
			Borrador:  "¡Hola! Con gusto le ayudo, ¿en qué puedo apoyarle?",
			Resultado: domain.ResultadoPropuesto,
			Now:       decisionFixedNow.Add(time.Minute),
		})
		require.NoError(t, err)

		if err := repo.Insertar(ctx, primera); err != nil {
			t.Skipf("Insertar failed — migration 000046 may not be applied: %v", err)
		}
		require.NoError(t, repo.Insertar(ctx, segunda))

		decisiones, err := repo.ListarPorCliente(ctx, clienteID)
		require.NoError(t, err)
		require.Len(t, decisiones, 2)

		assert.Equal(t, primera.ID(), decisiones[0].ID(), "ascending CreatedAt: primera comes first")
		assert.Equal(t, segunda.ID(), decisiones[1].ID())

		got1 := findDecisionByID(decisiones, primera.ID())
		require.NotNil(t, got1)
		assert.Equal(t, "turno-ref-1", got1.TurnoRef())
		assert.Equal(t, "pregunta por su saldo", got1.Intencion())
		assert.Equal(t, 82, got1.Confianza())
		assert.Equal(t, []string{"deuda", "confianza_baja"}, got1.Senales())
		assert.Equal(t, domain.AccionEscalar, got1.AccionPropuesta())
		assert.Equal(t, []string{"cliente preguntó monto exacto"}, got1.Evidencia())
		assert.Equal(t, domain.ResultadoEscalado, got1.Resultado())

		got2 := findDecisionByID(decisiones, segunda.ID())
		require.NotNil(t, got2)
		assert.Equal(t, []string{}, got2.Senales(), "nil Senales must round-trip as an empty (non-nil) slice")
		assert.Equal(t, []string{}, got2.Evidencia(), "nil Evidencia must round-trip as an empty (non-nil) slice")
		assert.Equal(t, "¡Hola! Con gusto le ayudo, ¿en qué puedo apoyarle?", got2.Borrador())
		assert.Equal(t, domain.AccionResponder, got2.AccionPropuesta())
		assert.Equal(t, domain.ResultadoPropuesto, got2.Resultado())
		assert.Empty(t, got2.RazonEscalamiento())
	})
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestDecisionRepo_ListarPorCliente_UnknownClienteReturnsEmpty(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewCopilotoRepo(pool)

		decisiones, err := repo.ListarPorCliente(ctx, 999999999)
		require.NoError(t, err)
		assert.Empty(t, decisiones)
	})
}
