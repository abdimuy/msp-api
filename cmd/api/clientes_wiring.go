//nolint:misspell // clientes vocabulary is Spanish per project convention.
package main

import (
	"context"
	"errors"
	"log/slog"

	"go.uber.org/fx"

	"github.com/abdimuy/msp-api/internal/analytics"
	analyticsapp "github.com/abdimuy/msp-api/internal/analytics/app"
	analyticsdomain "github.com/abdimuy/msp-api/internal/analytics/domain"
	clientesapp "github.com/abdimuy/msp-api/internal/clientes/app"
	clientesfb "github.com/abdimuy/msp-api/internal/clientes/infra/clientesfb"
	clientessearch "github.com/abdimuy/msp-api/internal/clientes/infra/search"
	clientesoutbound "github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/lifecycle"
)

// ── cross-module adapter ──────────────────────────────────────────────────────

// clientesAnalyticsAdapter translates between the analyticsapp.Service API and
// the clientesoutbound.AnalyticsClient port. The adapter lives here in cmd/api
// (not inside internal/clientes/infra/clients) per §6 of the wiring standard,
// because cmd/api is the only layer allowed to import both sides of the
// module boundary.
type clientesAnalyticsAdapter struct {
	svc *analyticsapp.Service
}

// Compile-time check: the adapter satisfies the clientes outbound port.
var _ clientesoutbound.AnalyticsClient = (*clientesAnalyticsAdapter)(nil)

// ObtenerPulso returns the analytics pulse for a single client. When the
// client has no materialized candidato row (analyticsdomain.ErrWinbackCandidatoNotFound)
// the adapter converts the "not found" into (zero, false, nil) so callers
// can degrade gracefully without treating absence as an error.
func (a *clientesAnalyticsAdapter) ObtenerPulso(
	ctx context.Context,
	clienteID int,
) (analytics.ClientePulsoContract, bool, error) {
	pulso, err := a.svc.ObtenerPulsoCliente(ctx, clienteID)
	if err != nil {
		if errors.Is(err, analyticsdomain.ErrWinbackCandidatoNotFound) {
			return analytics.ClientePulsoContract{}, false, nil
		}
		return analytics.ClientePulsoContract{}, false, err
	}
	return pulso, true, nil
}

// ObtenerPulsos delegates directly to the analytics service bulk query.
func (a *clientesAnalyticsAdapter) ObtenerPulsos(
	ctx context.Context,
	clienteIDs []int,
) (map[int]analytics.ClientePulsoContract, error) {
	return a.svc.ObtenerPulsosClientes(ctx, clienteIDs)
}

// provideClientesAnalyticsClient wires the cross-module adapter so fx can
// inject it as the clientesoutbound.AnalyticsClient port.
func provideClientesAnalyticsClient(analyticsSvc *analyticsapp.Service) clientesoutbound.AnalyticsClient {
	return &clientesAnalyticsAdapter{svc: analyticsSvc}
}

// ── port providers ────────────────────────────────────────────────────────────

// provideClientesRepo builds the Firebird-backed ClientesRepo for the clientes
// hub. All reads target Microsip tables; no MSP_* tables are written.
func provideClientesRepo(p *firebird.Pool) clientesoutbound.ClientesRepo {
	return clientesfb.NewClientesRepo(p)
}

// provideClientesSearchIndex builds the in-process Bleve full-text search index.
// The index starts empty (EstaListo = false) until the ReindexWorker completes
// its first warm-up pass. fx produces a single *BleveIndex shared between the
// Service (reads) and the worker (writes via Reconciliar).
func provideClientesSearchIndex() clientesoutbound.SearchIndex {
	return clientessearch.NewBleveIndex()
}

// provideClientesClock returns the production UTC clock for the clientes module.
func provideClientesClock() clientesoutbound.Clock {
	return clientesoutbound.ProductionClock{}
}

// provideClientesService assembles the clientes read-only query service.
func provideClientesService(
	repo clientesoutbound.ClientesRepo,
	analyticsClient clientesoutbound.AnalyticsClient,
	search clientesoutbound.SearchIndex,
	clock clientesoutbound.Clock,
) *clientesapp.Service {
	return clientesapp.NewService(repo, analyticsClient, search, clock)
}

// provideClientesReindexWorker builds the background worker that periodically
// rebuilds the Bleve search index from the Firebird client directory.
// An immediate warm-up reindex runs on Start so search is available ASAP.
func provideClientesReindexWorker(
	svc *clientesapp.Service,
	clock clientesoutbound.Clock,
	logger *slog.Logger,
) *clientesapp.ReindexWorker {
	return clientesapp.NewReindexWorker(svc, clock, clientesapp.ReindexWorkerConfig{}, logger)
}

// registerClientesReindexWorkerLifecycle hooks the reindex worker into the fx
// lifecycle so it starts with the application and stops cleanly on shutdown.
func registerClientesReindexWorkerLifecycle(lc fx.Lifecycle, w *clientesapp.ReindexWorker) {
	lifecycle.Append(lc, "clientes-reindex-worker", w)
}
