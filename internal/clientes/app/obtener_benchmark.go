//nolint:misspell // clientes vocabulary is Spanish per project convention.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/analytics"
)

// ObtenerBenchmark returns the peer-benchmark for the given client, delegating
// to the analytics outbound port. It is a pass-through: all cohort construction
// and scoring live in the analytics module.
func (s *Service) ObtenerBenchmark(ctx context.Context, clienteID int, cohortBy string) (analytics.BenchmarkContract, error) {
	return s.analytics.ObtenerBenchmark(ctx, clienteID, cohortBy)
}
