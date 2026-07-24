//nolint:misspell // Spanish domain vocabulary per project convention.

// Package smoke_test is an end-to-end integration smoke for the reactivación
// copiloto: it wires the REAL Firebird repositories + the REAL Claude LLM
// client to the app Service and runs one inbound-message flow
// (Analizar → triar → persist) inside a rolled-back transaction, so the shared
// dev DB never accumulates state. No WhatsApp, no HTTP server, no auth — the
// inbound message is simulated directly.
//
// Env-gated: needs FB_DATABASE (dev Firebird, migrations 000044 + 000046
// applied) and ANTHROPIC_API_KEY. `go test ./...` skips cleanly without them.
//
// Run:
//
//	source .env && go test ./internal/reactivacion/smoke/ -run SmokeCopiloto -v -count=1
package smoke_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	platformllm "github.com/abdimuy/msp-api/internal/platform/llm"
	"github.com/abdimuy/msp-api/internal/reactivacion/app"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionfb"
	"github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionllm"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// stubNBP is a fixed next-best-product reader for the smoke — it stands in for
// the production Microsip/association-rules NBP source (a later slice).
type stubNBP struct{ nbp *outbound.NextBestProduct }

func (s stubNBP) GetNBP(context.Context, int) (*outbound.NextBestProduct, error) {
	return s.nbp, nil
}

//nolint:paralleltest // integration smoke: real shared dev DB + real API, must run serially
func TestSmokeCopilotoEndToEnd(t *testing.T) {
	if os.Getenv("FB_DATABASE") == "" || os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("requires FB_DATABASE + ANTHROPIC_API_KEY; skipping — no DB/API spend")
	}

	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		clienteID := 900000001 // large synthetic id — never collides with real Microsip ids
		now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

		// Seed a cohorte row so the copiloto's facts (nombre/segmento) populate.
		coh, err := domain.CrearCohorteCliente(domain.CrearCohorteClienteParams{
			ClienteID:             clienteID,
			Nombre:                "José Guadalupe Ramírez",
			Telefono:              "238 100 2000",
			Segmento:              domain.SegmentoRecienLiquidado,
			EnControl:             false,
			FueContactado:         true,
			CohorteFecha:          now,
			FechaUltimaCompraBase: now.AddDate(0, -2, 0),
			Saldo:                 decimal.Zero,
			PorLiquidarPct:        decimal.RequireFromString("0.00"),
			Now:                   now,
		})
		require.NoError(t, err)

		repo := reactivacionfb.NewRepo(pool) // NotaReader + ClienteFactsReader + CohorteRepo
		if err := repo.UpsertCohorte(ctx, []*domain.CohorteCliente{coh}); err != nil {
			t.Skipf("UpsertCohorte failed — migration 000044/000046 may not be applied to dev: %v", err)
		}

		// REAL Claude client (the chosen production model).
		llmClient := platformllm.NewClient(config.LLM{
			Enabled: true,
			BaseURL: "https://api.anthropic.com/v1",
			Model:   "claude-haiku-4-5",
			APIKey:  os.Getenv("ANTHROPIC_API_KEY"),
			Timeout: 60 * time.Second,
		})
		gen := reactivacionllm.NewGenerator(llmClient, "claude-haiku-4-5")

		copRepo := reactivacionfb.NewCopilotoRepo(pool) // ConversacionRepo + DecisionRepo
		// NBP inyectado (producto+precio reales del catálogo) — demuestra la venta
		// DIRIGIDA: el copiloto ofrece este producto con su plan de pago calculado
		// en Go. En producción esto lo dará un reader Microsip / motor de NBP.
		nbp := stubNBP{&outbound.NextBestProduct{
			Nombre: "Refrigerador Hisense de 11 pies",
			Precio: decimal.RequireFromString("8500"),
		}}
		svc := app.NewService(nil, nil, outbound.ProductionClock{}, nil, app.Config{}).
			WithCopiloto(copRepo, copRepo, repo, gen, repo).
			WithNBP(nbp)

		// ── The whole flow: real Claude Analizar → deterministic triar → persist ──
		res, err := svc.ProcesarMensajeEntrante(ctx, clienteID,
			"hola, sí me llegó su mensaje, ¿qué muebles tienen?")
		require.NoError(t, err)
		require.NotNil(t, res.Decision, "una Decision debe haberse creado")

		t.Logf("✓ flujo OK — escalada=%v borrador=%q", res.Escalada, res.Borrador)

		// Taxonomía B: buying INTEREST responds (never escalates) with a draft.
		assert.False(t, res.Escalada, "un mensaje de interés debe RESPONDER, no escalar")
		assert.NotEmpty(t, res.Borrador, "debe guardar un borrador pendiente (modo sombra)")

		// Persistence: the conversation + entrante & saliente (draft) turnos landed.
		conv, err := copRepo.Get(ctx, clienteID)
		require.NoError(t, err)
		require.NotNil(t, conv, "la conversación debe haberse persistido")

		turnos, err := copRepo.ListarTurnos(ctx, clienteID)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(turnos), 2,
			"deben persistir al menos el turno entrante + el borrador saliente")
		t.Logf("✓ persistencia OK — %d turnos, estado=%v", len(turnos), conv.Estado())
	})
}
