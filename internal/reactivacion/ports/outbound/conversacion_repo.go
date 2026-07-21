//nolint:misspell // Spanish domain vocabulary by project convention.
package outbound

import (
	"context"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

// ListarConversacionesParams controls which MSP_RX_CONVERSACION rows
// ConversacionRepo.Listar returns.
type ListarConversacionesParams struct {
	// Estado restricts results to one estado. Empty string = no filter.
	Estado string
	// SoloEscaladas restricts results to conversations in EstadoEscalado when
	// true.
	SoloEscaladas bool
}

// ConversacionRepo persists and retrieves the copiloto's conversation state
// (MSP_RX_CONVERSACION) and its turn log (MSP_RX_TURNO).
type ConversacionRepo interface {
	// Get returns the Conversacion for clienteID, or (nil, nil) when the
	// cliente has no conversation yet.
	Get(ctx context.Context, clienteID int) (*domain.Conversacion, error)

	// Upsert inserts or updates c, matched by ClienteID.
	Upsert(ctx context.Context, c *domain.Conversacion) error

	// Listar returns conversaciones matching p.
	Listar(ctx context.Context, p ListarConversacionesParams) ([]*domain.Conversacion, error)

	// AppendTurno appends t to the conversation's turn log. Turnos are
	// append-only — this is never an upsert.
	AppendTurno(ctx context.Context, t *domain.Turno) error

	// ListarTurnos returns every turno for clienteID, ordered by CreatedAt
	// ascending.
	ListarTurnos(ctx context.Context, clienteID int) ([]*domain.Turno, error)
}
