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

// provideCobranzaPagosRepo builds the Firebird-backed PagosRepo.
func provideCobranzaPagosRepo(p *firebird.Pool) cobranzaoutbound.PagosRepo {
	return cobranzaventfb.NewPagosRepo(p)
}

// provideCobranzaRecomputer builds the Firebird-backed SaldosRecomputer.
// The repo is injected so the re-read step shares the same pool.
func provideCobranzaRecomputer(p *firebird.Pool, repo cobranzaoutbound.SaldosRepo) cobranzaoutbound.SaldosRecomputer {
	return cobranzaventfb.NewRecomputer(p, repo)
}

// provideCobranzaPagosRecomputer builds the Firebird-backed PagosRecomputer.
func provideCobranzaPagosRecomputer(p *firebird.Pool) cobranzaoutbound.PagosRecomputer {
	return cobranzaventfb.NewPagosRecomputer(p)
}

// provideCobranzaSaldosLister builds the Firebird-backed SaldosLister.
func provideCobranzaSaldosLister(p *firebird.Pool) cobranzaoutbound.SaldosLister {
	return cobranzaventfb.NewSaldosLister(p)
}

// provideCobranzaPagosLister builds the Firebird-backed PagosLister.
func provideCobranzaPagosLister(p *firebird.Pool) cobranzaoutbound.PagosLister {
	return cobranzaventfb.NewPagosLister(p)
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
func provideCobranzaService(
	saldos cobranzaoutbound.SaldosRepo,
	pagos cobranzaoutbound.PagosRepo,
	clock cobranzaoutbound.Clock,
) *cobranzaapp.Service {
	return cobranzaapp.NewService(saldos, pagos, clock)
}

// provideCobranzaReconcilerConfig returns the reconciler configuration.
// Hardcoded for now; promote to config.Config once a second deployment
// needs different cadence.
func provideCobranzaReconcilerConfig() cobranzaapp.ReconcilerConfig {
	return cobranzaapp.ReconcilerConfig{
		Interval:               7 * 24 * time.Hour,
		PageSize:               1000,
		DriftLog:               true,
		FixDrift:               true,
		TombstoneRetentionDays: 30,
	}
}

// provideCobranzaTombstoneCleaner exposes the SaldosRepo as a
// SaldosTombstoneCleaner port (the concrete *SaldosRepo satisfies both).
func provideCobranzaTombstoneCleaner(p *firebird.Pool) cobranzaoutbound.SaldosTombstoneCleaner {
	return cobranzaventfb.NewSaldosRepo(p)
}

// provideCobranzaReconciler assembles the cobranza reconciler.
func provideCobranzaReconciler(
	saldosLister cobranzaoutbound.SaldosLister,
	recomputer cobranzaoutbound.SaldosRecomputer,
	saldosRepo cobranzaoutbound.SaldosRepo,
	pagosLister cobranzaoutbound.PagosLister,
	pagosRecomputer cobranzaoutbound.PagosRecomputer,
	cleaner cobranzaoutbound.SaldosTombstoneCleaner,
	clock cobranzaoutbound.Clock,
	cfg cobranzaapp.ReconcilerConfig,
	logger *slog.Logger,
) *cobranzaapp.Reconciler {
	return cobranzaapp.NewReconciler(cobranzaapp.ReconcilerDeps{
		SaldosLister:     saldosLister,
		SaldosRepo:       saldosRepo,
		Recomputer:       recomputer,
		PagosLister:      pagosLister,
		PagosRecomputer:  pagosRecomputer,
		TombstoneCleaner: cleaner,
		Clock:            clock,
		Config:           cfg,
		Logger:           logger,
	})
}

// registerCobranzaReconcilerLifecycle hooks the reconciler into the fx lifecycle.
func registerCobranzaReconcilerLifecycle(lc fx.Lifecycle, r *cobranzaapp.Reconciler) {
	lifecycle.Append(lc, "saldos-reconciler", r)
}
