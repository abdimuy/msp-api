// Package app contains the clientes module's query services. This is a
// read-only Customer 360 hub: there are no commands, no domain events, no
// outbox, and no write transactions. Only query recipes apply.
//
//nolint:misspell // clientes vocabulary is Spanish (clientes, ficha, directorio, etc.) per project convention.
package app

import (
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
)

// Service is the clientes module's query surface. Handlers depend on *Service;
// everything Service depends on goes through the outbound ports.
//
// This service is intentionally read-only: no outbox, no txMgr, no runInTx,
// no drainEvents. The clientes hub aggregates reads from Microsip projections
// and the analytics module without ever mutating state.
type Service struct {
	repo      outbound.ClientesRepo
	analytics outbound.AnalyticsClient
	search    outbound.SearchIndex
	// clock is injected per the module Service convention. Phase 1 reads derive
	// no values from the wall clock (recencia and date math live in the analytics
	// pulse), so it is currently unused; it is reserved for the Fase 2 date-range
	// KPIs/charts that compute relative time in this layer.
	clock outbound.Clock
}

// NewService builds a Service wired against the given ports. All four ports are
// required in production; tests may pass fakes.
func NewService(
	repo outbound.ClientesRepo,
	analytics outbound.AnalyticsClient,
	search outbound.SearchIndex,
	clock outbound.Clock,
) *Service {
	return &Service{
		repo:      repo,
		analytics: analytics,
		search:    search,
		clock:     clock,
	}
}
