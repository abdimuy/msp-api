//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

// ConversacionDetalle is the full picture of one cliente's conversation: the
// state machine header, the complete turno thread, and the decision audit
// trail — everything the operator's ficha view needs in one query.
type ConversacionDetalle struct {
	Conversacion *domain.Conversacion
	Turnos       []*domain.Turno
	Decisiones   []*domain.Decision
}

// ObtenerConversacion returns clienteID's full conversation detail. Returns
// ErrConversacionNoEncontrada when the cliente has no conversation yet.
func (s *Service) ObtenerConversacion(ctx context.Context, clienteID int) (ConversacionDetalle, error) {
	const source = "reactivacion.ObtenerConversacion"

	conv, err := s.convRepo.Get(ctx, clienteID)
	if err != nil {
		return ConversacionDetalle{}, apperror.NewInternal("conversacion_get_failed", "error al leer la conversación del cliente").
			WithSource(source).WithError(err)
	}
	if conv == nil {
		return ConversacionDetalle{}, ErrConversacionNoEncontrada
	}

	turnos, err := s.convRepo.ListarTurnos(ctx, clienteID)
	if err != nil {
		return ConversacionDetalle{}, apperror.NewInternal("turnos_list_failed", "error al listar los turnos de la conversación").
			WithSource(source).WithError(err)
	}

	decisiones, err := s.decisionRepo.ListarPorCliente(ctx, clienteID)
	if err != nil {
		return ConversacionDetalle{}, apperror.NewInternal("decisiones_list_failed", "error al listar las decisiones del cliente").
			WithSource(source).WithError(err)
	}

	return ConversacionDetalle{Conversacion: conv, Turnos: turnos, Decisiones: decisiones}, nil
}
