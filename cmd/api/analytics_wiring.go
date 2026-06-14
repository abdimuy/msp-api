//nolint:misspell // analytics vocabulary is Spanish per project convention.
package main

import (
	"log/slog"

	"go.uber.org/fx"

	analyticsapp "github.com/abdimuy/msp-api/internal/analytics/app"
	analyticsfb "github.com/abdimuy/msp-api/internal/analytics/infra/analyticsfb"
	analyticsoutbound "github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/lifecycle"
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
func provideAnalyticsService(
	repo analyticsoutbound.WinbackRepo,
	micro analyticsoutbound.MicrosipReader,
	clock analyticsoutbound.Clock,
	txRunner analyticsapp.TxRunner,
) *analyticsapp.Service {
	return analyticsapp.NewService(repo, micro, clock, txRunner)
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
