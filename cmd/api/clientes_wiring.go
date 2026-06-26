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
	clientessearchmeili "github.com/abdimuy/msp-api/internal/clientes/infra/clientessearch"
	clientesoutbound "github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/lifecycle"
	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
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

// ObtenerPredicciones delegates Bayesian predictions to the analytics service.
func (a *clientesAnalyticsAdapter) ObtenerPredicciones(
	ctx context.Context,
	clienteID int,
) (analytics.PrediccionesContract, error) {
	return a.svc.ObtenerPredicciones(ctx, clienteID)
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

// provideClientesClock returns the production UTC clock for the clientes module.
func provideClientesClock() clientesoutbound.Clock {
	return clientesoutbound.ProductionClock{}
}

// provideClientesDirectoryIndex builds the Meilisearch-backed directory index
// implementation for the clientes module.
func provideClientesDirectoryIndex(
	client platformmeili.Client,
	cfg *config.Config,
) clientesoutbound.DirectoryIndex {
	return clientessearchmeili.NewMeilisearchDirectoryIndex(client, cfg.Meilisearch.IndexName)
}

// provideClientesService assembles the clientes read-only query service.
func provideClientesService(
	repo clientesoutbound.ClientesRepo,
	analyticsClient clientesoutbound.AnalyticsClient,
	dirIndex clientesoutbound.DirectoryIndex,
	clock clientesoutbound.Clock,
) *clientesapp.Service {
	return clientesapp.NewService(repo, analyticsClient, dirIndex, clock)
}

// provideClientesDirectoryReconcileWorker builds the background worker that
// periodically materializes the Meilisearch directory index from Firebird.
// The interval is taken from cfg.Meilisearch.SyncInterval (default 5m).
func provideClientesDirectoryReconcileWorker(
	svc *clientesapp.Service,
	cfg *config.Config,
	logger *slog.Logger,
) *clientesapp.DirectoryReconcileWorker {
	return clientesapp.NewDirectoryReconcileWorker(
		svc,
		clientesapp.DirectoryReconcileWorkerConfig{Interval: cfg.Meilisearch.SyncInterval},
		logger,
	)
}

// registerClientesDirectoryReconcileWorkerLifecycle hooks the directory reconcile
// worker into the fx lifecycle.
func registerClientesDirectoryReconcileWorkerLifecycle(
	lc fx.Lifecycle,
	w *clientesapp.DirectoryReconcileWorker,
) {
	lifecycle.Append(lc, "clientes-directory-reconcile-worker", w)
}
