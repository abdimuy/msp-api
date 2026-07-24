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

	"github.com/abdimuy/msp-api/internal/microsip"
	microsipapp "github.com/abdimuy/msp-api/internal/microsip/app"
	"github.com/abdimuy/msp-api/internal/microsip/infra/microsipfb"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	platformllm "github.com/abdimuy/msp-api/internal/platform/llm"
	"github.com/abdimuy/msp-api/internal/reactivacion/app"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionfb"
	"github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionllm"
	"github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionmicrosip"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

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

		// REAL next-best-product reader: the microsip catalog contract (in-stock
		// articles + parsed prices) composed with the cliente's purchased
		// categorías (reactivacionfb.Repo). It suggests a real, in-stock product
		// above the price floor in a line the cliente does not already own —
		// demuestra la venta DIRIGIDA personalizada, con su plan calculado en Go.
		// Price lists 42/8437/6925 are the config default (MICROSIP_PRICE_LIST_IDS);
		// list 42's Microsip NOMBRE is "Precio de lista" — the base list the plan
		// funds (legacy code nicknamed it "MUEBLERIAS").
		catalogo := microsip.NewServiceAdapter(
			microsipapp.NewService(
				microsipfb.NewAlmacenRepo(pool, []int{42, 8437, 6925}),
				microsipfb.NewZonaRepo(pool),
			),
		)
		nbp := reactivacionmicrosip.NewNBPReader(catalogo, repo, reactivacionmicrosip.NBPConfig{
			AlmacenID:    19,
			PisoPrecio:   decimal.NewFromInt(3000),
			ListaCredito: "Precio de lista",
		}, nil)

		// Exercise the real reader directly first: proves it resolves a product
		// from the live catalog. Skip cleanly if almacén 19 has nothing offerable
		// in this dev snapshot (the copiloto would just degrade, no product).
		sugerido, err := nbp.GetNBP(ctx, clienteID)
		require.NoError(t, err, "el reader NBP real no debe fallar")
		if sugerido == nil {
			t.Skip("sin producto ofertable en almacén 19 (dev snapshot) — nada que demostrar")
		}
		t.Logf("✓ NBP real → %q precio=%s", sugerido.Nombre, sugerido.Precio)

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
