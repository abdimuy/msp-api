//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package main

import (
	"log/slog"
	"math/rand"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/microsip"
	reactivacionapp "github.com/abdimuy/msp-api/internal/reactivacion/app"
	reactivacionfb "github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionfb"
	reactivacionllm "github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionllm"
	reactivacionmicrosip "github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionmicrosip"
	reactivacionsender "github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionsender"
	reactivacionoutbound "github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"

	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/lifecycle"
	platformllm "github.com/abdimuy/msp-api/internal/platform/llm"

	"go.uber.org/fx"
)

// provideReactivacionRepo builds the Firebird-backed Repo that implements both
// CohorteRepo and UniversoReader. A single concrete instance is created here; it
// is then exposed as each interface via the two port providers below.
func provideReactivacionRepo(p *firebird.Pool) *reactivacionfb.Repo {
	return reactivacionfb.NewRepo(p)
}

// provideReactivacionCohorteRepo exposes the concrete Repo as the CohorteRepo port.
func provideReactivacionCohorteRepo(r *reactivacionfb.Repo) reactivacionoutbound.CohorteRepo {
	return r
}

// provideReactivacionUniversoReader exposes the concrete Repo as the UniversoReader port.
func provideReactivacionUniversoReader(r *reactivacionfb.Repo) reactivacionoutbound.UniversoReader {
	return r
}

// provideReactivacionMensajeRepo exposes the concrete Repo as the MensajeRepo
// port (Fase 2 canal queue, MSP_RX_MENSAJES).
func provideReactivacionMensajeRepo(r *reactivacionfb.Repo) reactivacionoutbound.MensajeRepo {
	return r
}

// provideReactivacionClock returns the production UTC clock for the reactivación module.
func provideReactivacionClock() reactivacionoutbound.Clock {
	return reactivacionoutbound.ProductionClock{}
}

// provideReactivacionTxRunner exposes *firebird.TxManager as the reactivación
// TxRunner interface that NewService expects.
func provideReactivacionTxRunner(m *firebird.TxManager) reactivacionapp.TxRunner {
	return m
}

// provideReactivacionSender selects the MessageSender implementation by
// REACTIVACION_SENDER: "fake" (default) never touches a real number;
// "whatsmeow" is a stub that fails until Fase 3 wires the real channel.
// Any other value falls back to the fake sender — the safer default.
func provideReactivacionSender(cfg *config.Config, logger *slog.Logger) reactivacionoutbound.MessageSender {
	if cfg.Reactivacion.Sender == "whatsmeow" {
		return reactivacionsender.NewWhatsmeowSender()
	}
	return reactivacionsender.NewFakeSender(logger)
}

// provideReactivacionRand builds the *rand.Rand backing the gobernador's
// jitter draws. Time-seeded in production — tests inject their own seeded
// *rand.Rand directly, bypassing this provider entirely.
func provideReactivacionRand() *rand.Rand {
	//nolint:gosec // jitter timing does not need cryptographic randomness.
	return rand.New(rand.NewSource(time.Now().UnixNano()))
}

// provideReactivacionGobernador builds the anti-baneo pacing engine from the
// configured perfil (REACTIVACION_PERFIL_ENVIO: produccion|demo).
func provideReactivacionGobernador(cfg *config.Config, rng *rand.Rand) *reactivacionapp.Gobernador {
	perfil := reactivacionapp.PerfilProduccion
	if cfg.Reactivacion.PerfilEnvio == string(reactivacionapp.PerfilDemo) {
		perfil = reactivacionapp.PerfilDemo
	}
	return reactivacionapp.NewGobernador(reactivacionapp.PerfilConfig(perfil), rng)
}

// provideReactivacionOpener builds the opener template generator.
func provideReactivacionOpener() reactivacionapp.Opener {
	return reactivacionapp.NewOpener()
}

// provideReactivacionCopilotoRepo builds the Firebird-backed CopilotoRepo that
// implements both ConversacionRepo and DecisionRepo. It is a SEPARATE struct
// from Repo — not an addition to it — because Insertar/Listar collide by name
// with MensajeRepo's methods of the same names but different signatures (Go
// does not support overloading by parameter type). See
// internal/reactivacion/infra/reactivacionfb/copiloto_repo.go for the full
// rationale. It shares Repo's *firebird.Pool — no new connection lifecycle.
func provideReactivacionCopilotoRepo(p *firebird.Pool) *reactivacionfb.CopilotoRepo {
	return reactivacionfb.NewCopilotoRepo(p)
}

// provideReactivacionConversacionRepo exposes the concrete CopilotoRepo as
// the ConversacionRepo port.
func provideReactivacionConversacionRepo(r *reactivacionfb.CopilotoRepo) reactivacionoutbound.ConversacionRepo {
	return r
}

// provideReactivacionDecisionRepo exposes the concrete CopilotoRepo as the
// DecisionRepo port.
func provideReactivacionDecisionRepo(r *reactivacionfb.CopilotoRepo) reactivacionoutbound.DecisionRepo {
	return r
}

// provideReactivacionNotaReader exposes the concrete Repo as the NotaReader
// port (GetNotaCliente has no name collision with MensajeRepo, so it stays on
// the original Repo alongside CohorteRepo/UniversoReader/MensajeRepo).
func provideReactivacionNotaReader(r *reactivacionfb.Repo) reactivacionoutbound.NotaReader {
	return r
}

// provideReactivacionClienteFactsReader exposes the concrete Repo as the
// ClienteFactsReader port (GetFacts has no name collision either).
func provideReactivacionClienteFactsReader(r *reactivacionfb.Repo) reactivacionoutbound.ClienteFactsReader {
	return r
}

// provideReactivacionCategoriasClienteReader exposes the concrete Repo as the
// CategoriasClienteReader port (reads the cliente's purchased product lines
// off the Microsip venta history — no name collision with MensajeRepo).
func provideReactivacionCategoriasClienteReader(r *reactivacionfb.Repo) reactivacionoutbound.CategoriasClienteReader {
	return r
}

// provideReactivacionNBPReader builds the personalized next-best-product reader
// by composing the microsip catalog contract with the cliente's purchased
// categorías. It suggests an in-stock product above the price floor in a line
// the cliente does not already own (global fallback otherwise). Exposed as the
// OPTIONAL NextBestProductReader port wired via WithNBP.
func provideReactivacionNBPReader(
	cat microsip.Catalogo,
	categorias reactivacionoutbound.CategoriasClienteReader,
	cfg *config.Config,
	logger *slog.Logger,
) reactivacionoutbound.NextBestProductReader {
	return reactivacionmicrosip.NewNBPReader(cat, categorias, reactivacionmicrosip.NBPConfig{
		AlmacenID:    cfg.Reactivacion.NBPAlmacenID,
		PisoPrecio:   decimal.NewFromInt(int64(cfg.Reactivacion.NBPPisoPrecio)),
		ListaCredito: cfg.Reactivacion.NBPListaCredito,
	}, logger)
}

// provideReactivacionCopilotoLLM builds the copiloto's LLM adapter, reusing
// the SHARED platform LLM client (platformllm.Client, wired once in
// llm_wiring.go and already shared with analytics) — no per-feature LLM
// config. When LLM_ENABLED=false (the default), every call degrades per
// ProcesarMensajeEntrante's documented ErrLLMDisabled fallback (synthetic
// escalate), never panics or blocks.
func provideReactivacionCopilotoLLM(client platformllm.Client, cfg *config.Config) reactivacionoutbound.CopilotoLLM {
	return reactivacionllm.NewGenerator(client, cfg.LLM.Model)
}

// provideReactivacionService assembles the reactivación query and command
// service, wiring the Fase 2 canal dependencies (MensajeRepo, MessageSender,
// Opener, Gobernador, auto_send) onto the Fase 1 base via WithCanal, then the
// Fase 3a copiloto dependencies via WithCopiloto. AprobarBorrador/
// EditarYAprobar reuse the canal's mensajeRepo, so WithCanal must run first.
func provideReactivacionService(
	reader reactivacionoutbound.UniversoReader,
	repo reactivacionoutbound.CohorteRepo,
	clock reactivacionoutbound.Clock,
	txRunner reactivacionapp.TxRunner,
	mensajeRepo reactivacionoutbound.MensajeRepo,
	sender reactivacionoutbound.MessageSender,
	opener reactivacionapp.Opener,
	gobernador *reactivacionapp.Gobernador,
	convRepo reactivacionoutbound.ConversacionRepo,
	decisionRepo reactivacionoutbound.DecisionRepo,
	notaReader reactivacionoutbound.NotaReader,
	copilotoLLM reactivacionoutbound.CopilotoLLM,
	factsReader reactivacionoutbound.ClienteFactsReader,
	nbpReader reactivacionoutbound.NextBestProductReader,
	cfg *config.Config,
	logger *slog.Logger,
) *reactivacionapp.Service {
	return reactivacionapp.NewService(reader, repo, clock, txRunner, reactivacionapp.Config{
		ControlPct: cfg.Reactivacion.ControlPct,
	}).
		WithLogger(logger).
		WithCanal(mensajeRepo, sender, opener, gobernador, cfg.Reactivacion.AutoSend).
		WithCopiloto(convRepo, decisionRepo, notaReader, copilotoLLM, factsReader).
		WithNBP(nbpReader)
}

// provideReactivacionEnvioWorker builds the background worker that drains the
// canal queue on a ticker when auto_send is on (a no-op tick otherwise).
func provideReactivacionEnvioWorker(
	svc *reactivacionapp.Service,
	clock reactivacionoutbound.Clock,
	cfg *config.Config,
	logger *slog.Logger,
) *reactivacionapp.EnvioWorker {
	return reactivacionapp.NewEnvioWorker(svc, clock, reactivacionapp.EnvioWorkerConfig{
		Interval: time.Duration(cfg.Reactivacion.WorkerIntervalSeg) * time.Second,
	}, cfg.Reactivacion.AutoSend, logger)
}

// registerReactivacionEnvioWorkerLifecycle hooks the envío worker into the fx
// lifecycle so it starts/stops with the app.
func registerReactivacionEnvioWorkerLifecycle(lc fx.Lifecycle, w *reactivacionapp.EnvioWorker) {
	lifecycle.Append(lc, "reactivacion-envio-worker", w)
}
