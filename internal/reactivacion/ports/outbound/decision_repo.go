//nolint:misspell // Spanish domain vocabulary by project convention.
package outbound

import (
	"context"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

// DecisionRepo persists and retrieves the copiloto's LLM decision audit
// trail (MSP_RX_DECISIONES).
type DecisionRepo interface {
	// Insertar appends a newly produced decision. Every row is a fresh INSERT
	// — never an upsert.
	Insertar(ctx context.Context, d *domain.Decision) error

	// ListarPorCliente returns every decision for clienteID, ordered by
	// CreatedAt ascending.
	ListarPorCliente(ctx context.Context, clienteID int) ([]*domain.Decision, error)
}
