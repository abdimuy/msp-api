//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package main

import (
	"log/slog"
	"time"

	"go.uber.org/fx"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/lifecycle"

	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	cobranzaoutbound "github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// provideCobranzaSaldosRepo builds the Firebird-backed SaldosRepo.
func provideCobranzaSaldosRepo(p *firebird.Pool) cobranzaoutbound.SaldosRepo {
	return cobranzaventfb.NewSaldosRepo(p)
}

// provideCobranzaRecomputer builds the Firebird-backed SaldosRecomputer.
// The repo is injected so the re-read step shares the same pool.
func provideCobranzaRecomputer(p *firebird.Pool, repo cobranzaoutbound.SaldosRepo) cobranzaoutbound.SaldosRecomputer {
	return cobranzaventfb.NewRecomputer(p, repo)
}

// provideCobranzaSaldosLister builds the Firebird-backed SaldosLister.
func provideCobranzaSaldosLister(p *firebird.Pool) cobranzaoutbound.SaldosLister {
	return cobranzaventfb.NewSaldosLister(p)
}

// provideCobranzaErrorsRepo builds the Firebird-backed ErrorsRepo.
func provideCobranzaErrorsRepo(p *firebird.Pool) cobranzaoutbound.ErrorsRepo {
	return cobranzaventfb.NewErrorsRepo(p)
}

// provideCobranzaClock returns the production UTC clock for the cobranza module.
func provideCobranzaClock() cobranzaoutbound.Clock {
	return cobranzaoutbound.ProductionClock{}
}

// provideCobranzaService assembles the cobranza query service.
func provideCobranzaService(repo cobranzaoutbound.SaldosRepo, clock cobranzaoutbound.Clock) *cobranzaapp.Service {
	return cobranzaapp.NewService(repo, clock)
}

// provideCobranzaReconcilerConfig returns the reconciler configuration.
// Hardcoded for now; promote to config.Config once a second deployment
// needs different cadence.
func provideCobranzaReconcilerConfig() cobranzaapp.ReconcilerConfig {
	return cobranzaapp.ReconcilerConfig{
		Interval: 7 * 24 * time.Hour,
		PageSize: 1000,
		DriftLog: true,
		FixDrift: true,
	}
}

// provideCobranzaReconciler assembles the cobranza reconciler.
func provideCobranzaReconciler(
	lister cobranzaoutbound.SaldosLister,
	recomputer cobranzaoutbound.SaldosRecomputer,
	repo cobranzaoutbound.SaldosRepo,
	clock cobranzaoutbound.Clock,
	cfg cobranzaapp.ReconcilerConfig,
	logger *slog.Logger,
) *cobranzaapp.Reconciler {
	return cobranzaapp.NewReconciler(lister, recomputer, repo, clock, cfg, logger)
}

// registerCobranzaReconcilerLifecycle hooks the reconciler into the fx lifecycle.
func registerCobranzaReconcilerLifecycle(lc fx.Lifecycle, r *cobranzaapp.Reconciler) {
	lifecycle.Append(lc, "saldos-reconciler", r)
}
