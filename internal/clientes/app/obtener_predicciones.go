//nolint:misspell // clientes vocabulary is Spanish per project convention.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/analytics"
)

// ObtenerPredicciones returns the Bayesian credible-interval predictions for
// the given client, delegating to the analytics outbound port. It is a
// pass-through: all gating and computation live in the analytics module.
func (s *Service) ObtenerPredicciones(ctx context.Context, clienteID int) (analytics.PrediccionesContract, error) {
	return s.analytics.ObtenerPredicciones(ctx, clienteID)
}
