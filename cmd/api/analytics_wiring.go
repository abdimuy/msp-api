//nolint:misspell // analytics vocabulary is Spanish per project convention.
package main

import (
	"log/slog"

	"go.uber.org/fx"

	analyticsapp "github.com/abdimuy/msp-api/internal/analytics/app"
	analyticsfb "github.com/abdimuy/msp-api/internal/analytics/infra/analyticsfb"
	analyticsllm "github.com/abdimuy/msp-api/internal/analytics/infra/llm"
	analyticsoutbound "github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/lifecycle"
	platformllm "github.com/abdimuy/msp-api/internal/platform/llm"
)

// provideAnalyticsRepo builds the Firebird-backed Repo that implements both
// WinbackRepo and MicrosipReader. A single concrete instance is created here;
// it is then exposed as each interface via the two port providers below.
func provideAnalyticsRepo(p *firebird.Pool) *analyticsfb.Repo {
	return analyticsfb.NewRepo(p)
}

// provideAnalyticsWinbackRepo exposes the concrete Repo as the WinbackRepo
// port so fx can inject it into provideAnalyticsService.
func provideAnalyticsWinbackRepo(r *analyticsfb.Repo) analyticsoutbound.WinbackRepo {
	return r
}

// provideAnalyticsMicrosipReader exposes the concrete Repo as the
// MicrosipReader port so fx can inject it into provideAnalyticsService.
func provideAnalyticsMicrosipReader(r *analyticsfb.Repo) analyticsoutbound.MicrosipReader {
	return r
}

// provideAnalyticsClock returns the production UTC clock for the analytics module.
func provideAnalyticsClock() analyticsoutbound.Clock {
	return analyticsoutbound.ProductionClock{}
}

// provideAnalyticsTxRunner exposes *firebird.TxManager as the analyticsapp.TxRunner
// interface that NewService expects. *firebird.TxManager satisfies TxRunner
// implicitly (RunInTx method), but fx resolves by exact type, so this provider
// makes the interface resolution explicit.
func provideAnalyticsTxRunner(m *firebird.TxManager) analyticsapp.TxRunner {
	return m
}

// provideAnalyticsService assembles the analytics query and command service.
// narrativaRepo and cfg are added to wire the Fase-2 narrativa read-path;
// with LLM_ENABLED=false (the default), WithNarrativa keeps the service in
// cache-only mode — no new generation is ever enqueued.
func provideAnalyticsService(
	repo analyticsoutbound.WinbackRepo,
	micro analyticsoutbound.MicrosipReader,
	clock analyticsoutbound.Clock,
	txRunner analyticsapp.TxRunner,
	narrativaRepo analyticsoutbound.NarrativaRepo,
	cfg *config.Config,
) *analyticsapp.Service {
	return analyticsapp.NewService(repo, micro, clock, txRunner).
		WithNarrativa(narrativaRepo, cfg.LLM.Enabled)
}

// provideAnalyticsRefreshWorker builds the background worker that periodically
// calls RefrescarCandidatos: incremental every hour, full at 3 AM UTC.
func provideAnalyticsRefreshWorker(
	svc *analyticsapp.Service,
	clock analyticsoutbound.Clock,
	logger *slog.Logger,
) *analyticsapp.RefreshWorker {
	return analyticsapp.NewRefreshWorker(svc, clock, analyticsapp.RefreshWorkerConfig{}, logger)
}

// registerAnalyticsRefreshWorkerLifecycle hooks the refresh worker into the fx
// lifecycle so it starts with the application and stops cleanly on shutdown.
func registerAnalyticsRefreshWorkerLifecycle(lc fx.Lifecycle, w *analyticsapp.RefreshWorker) {
	lifecycle.Append(lc, "analytics-refresh-worker", w)
}

// provideAnalyticsNarrativeGenerator builds the LLM-backed NarrativeGenerator.
// When the LLM client is disabled (LLM_ENABLED=false), the generator returns
// ErrLLMDisabled on every call; the worker treats that as a permanent error
// and removes the entry from the pending queue without storing a row.
func provideAnalyticsNarrativeGenerator(client platformllm.Client, cfg *config.Config) analyticsoutbound.NarrativeGenerator {
	return analyticsllm.NewGenerator(client, cfg.LLM.Model)
}

// provideAnalyticsNarrativaRepo exposes the concrete Repo as the NarrativaRepo
// port so fx can inject it into provideAnalyticsService and
// provideAnalyticsNarrativaWorker. Mirrors provideAnalyticsWinbackRepo.
func provideAnalyticsNarrativaRepo(r *analyticsfb.Repo) analyticsoutbound.NarrativaRepo {
	return r
}

// provideAnalyticsNarrativaWorker builds the background worker that drains the
// MSP_AN_NARRATIVA_PENDIENTE queue: one client per iteration, serialised,
// ticker-driven. When LLM_ENABLED=false (the default), Start is a no-op —
// no goroutine is launched and no model is ever called.
func provideAnalyticsNarrativaWorker(
	svc *analyticsapp.Service,
	repo analyticsoutbound.NarrativaRepo,
	gen analyticsoutbound.NarrativeGenerator,
	clock analyticsoutbound.Clock,
	cfg *config.Config,
	logger *slog.Logger,
) *analyticsapp.NarrativaWorker {
	return analyticsapp.NewNarrativaWorker(svc, repo, gen, clock,
		analyticsapp.NarrativaWorkerConfig{Model: cfg.LLM.Model, Enabled: cfg.LLM.Enabled}, logger)
}

// registerAnalyticsNarrativaWorkerLifecycle hooks the narrativa worker into the
// fx lifecycle so it starts with the application and drains cleanly on shutdown.
func registerAnalyticsNarrativaWorkerLifecycle(lc fx.Lifecycle, w *analyticsapp.NarrativaWorker) {
	lifecycle.Append(lc, "analytics-narrativa-worker", w)
}
