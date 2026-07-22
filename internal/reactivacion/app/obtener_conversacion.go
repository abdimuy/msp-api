//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"context"
	"log/slog"

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

	// ─── Fase 3c bandeja enrichment — hydrated from ClienteFactsReader. Empty
	// when the cliente has no facts yet or when hydration degrades (see
	// hydrateDetalleFacts).

	Nombre   string
	Segmento string
	Telefono string
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

	det := ConversacionDetalle{Conversacion: conv, Turnos: turnos, Decisiones: decisiones}
	s.hydrateDetalleFacts(ctx, &det, clienteID)
	return det, nil
}

// hydrateDetalleFacts fills det's Nombre/Segmento/Telefono from
// ClienteFactsReader. A reader error, or the cliente not being in the
// cohorte snapshot (nil facts), degrades to empty fields (logged, not
// fatal) — the ficha view still renders without them.
func (s *Service) hydrateDetalleFacts(ctx context.Context, det *ConversacionDetalle, clienteID int) {
	facts, err := s.factsReader.GetFacts(ctx, clienteID)
	if err != nil {
		s.logger.WarnContext(ctx, "reactivacion_bandeja.facts_reader_failed",
			slog.Int("cliente_id", clienteID), slog.String("error", err.Error()))
		return
	}
	if facts == nil {
		return
	}
	det.Nombre = facts.Nombre
	det.Segmento = facts.Segmento
	det.Telefono = facts.Telefono
}
