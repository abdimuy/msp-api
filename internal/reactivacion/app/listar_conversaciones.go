//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"context"
	"sort"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// ConversacionResumen pairs a Conversacion with its newest Decision — enough
// context for the bandeja queue to render AND to order by urgency without a
// second round-trip per row.
type ConversacionResumen struct {
	Conversacion *domain.Conversacion
	// UltimaDecision is the cliente's newest decision, or nil when the
	// conversation has none yet (e.g. it was just opened by the Fase 2 channel
	// and no inbound message has arrived).
	UltimaDecision *domain.Decision
}

// prioridadAtencion scores r for the bandeja queue's sort — LOWER sorts
// first ("te necesitan" before "al día"):
//
//	0 — Conversacion.Estado is escalado: a human already owns this, surface it.
//	1 — the newest Decision proposed escalating (buy-signal, low confidence,
//	    debt, etc.) even if the conversation itself has not been escalado yet
//	    (e.g. an operator has not acted on it).
//	2 — everything else ("al día": nothing pending needs a human).
func prioridadAtencion(r ConversacionResumen) int {
	if r.Conversacion.Estado() == domain.EstadoEscalado {
		return 0
	}
	if r.UltimaDecision != nil && r.UltimaDecision.AccionPropuesta() == domain.AccionEscalar {
		return 1
	}
	return 2
}

// ListarConversaciones returns the bandeja queue: every conversation matching
// p, ordered so conversations that need human attention ("te necesitan" —
// escaladas first, then a latest-decision escalate) sort before the rest
// ("al día"). The sort is stable — conversaciones with the same
// prioridadAtencion keep the repo's original relative order.
func (s *Service) ListarConversaciones(ctx context.Context, p outbound.ListarConversacionesParams) ([]ConversacionResumen, error) {
	const source = "reactivacion.ListarConversaciones"

	convs, err := s.convRepo.Listar(ctx, p)
	if err != nil {
		return nil, apperror.NewInternal("conversaciones_list_failed", "error al listar las conversaciones").
			WithSource(source).WithError(err)
	}

	resumenes := make([]ConversacionResumen, 0, len(convs))
	for _, c := range convs {
		decisiones, err := s.decisionRepo.ListarPorCliente(ctx, c.ClienteID())
		if err != nil {
			return nil, apperror.NewInternal("decisiones_list_failed", "error al listar las decisiones del cliente").
				WithSource(source).WithError(err)
		}
		resumenes = append(resumenes, ConversacionResumen{
			Conversacion:   c,
			UltimaDecision: newestDecisionPorClienteID(decisiones),
		})
	}

	sort.SliceStable(resumenes, func(i, j int) bool {
		return prioridadAtencion(resumenes[i]) < prioridadAtencion(resumenes[j])
	})

	return resumenes, nil
}
