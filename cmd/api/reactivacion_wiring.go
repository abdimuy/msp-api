//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package main

import (
	"log/slog"

	reactivacionapp "github.com/abdimuy/msp-api/internal/reactivacion/app"
	reactivacionfb "github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionfb"
	reactivacionoutbound "github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// pilotoControlPct is the control-group share for the reactivación piloto. The
// channel (not the universe) is the bottleneck, so a large control group is free
// and tightens the attribution estimate.
const pilotoControlPct = 50

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

// provideReactivacionClock returns the production UTC clock for the reactivación module.
func provideReactivacionClock() reactivacionoutbound.Clock {
	return reactivacionoutbound.ProductionClock{}
}

// provideReactivacionTxRunner exposes *firebird.TxManager as the reactivación
// TxRunner interface that NewService expects.
func provideReactivacionTxRunner(m *firebird.TxManager) reactivacionapp.TxRunner {
	return m
}

// provideReactivacionService assembles the reactivación query and command service.
func provideReactivacionService(
	reader reactivacionoutbound.UniversoReader,
	repo reactivacionoutbound.CohorteRepo,
	clock reactivacionoutbound.Clock,
	txRunner reactivacionapp.TxRunner,
	logger *slog.Logger,
) *reactivacionapp.Service {
	return reactivacionapp.NewService(reader, repo, clock, txRunner, reactivacionapp.Config{
		ControlPct: pilotoControlPct,
	}).WithLogger(logger)
}
