//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"context"
	"log/slog"
	"sort"
	"unicode/utf8"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// ultimoMensajeMaxRunes caps ConversacionResumen.UltimoMensaje for the bandeja
// queue preview — mirrors the analytics module's nota cap
// (internal/analytics/app/narrativa_validate.go's caparContexto).
const ultimoMensajeMaxRunes = 120

// ConversacionResumen pairs a Conversacion with its newest Decision — enough
// context for the bandeja queue to render AND to order by urgency without a
// second round-trip per row.
type ConversacionResumen struct {
	Conversacion *domain.Conversacion
	// UltimaDecision is the cliente's newest decision, or nil when the
	// conversation has none yet (e.g. it was just opened by the Fase 2 channel
	// and no inbound message has arrived).
	UltimaDecision *domain.Decision

	// ─── Fase 3c bandeja enrichment — hydrated per-row from ClienteFactsReader
	// and the turno thread. Empty when the cliente has no facts/turnos yet or
	// when hydration degrades (see hydrateResumen).

	Nombre        string
	Segmento      string
	UltimoMensaje string
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
		resumen := ConversacionResumen{
			Conversacion:   c,
			UltimaDecision: newestDecisionPorClienteID(decisiones),
		}
		// N+1 over the queue (one GetFacts + one ListarTurnos per conversación) —
		// acceptable at pilot scale; revisit with a batch read if the bandeja
		// queue grows past a few hundred rows.
		s.hydrateResumen(ctx, &resumen)
		resumenes = append(resumenes, resumen)
	}

	sort.SliceStable(resumenes, func(i, j int) bool {
		return prioridadAtencion(resumenes[i]) < prioridadAtencion(resumenes[j])
	})

	return resumenes, nil
}

// hydrateResumen fills r's Nombre/Segmento (from ClienteFactsReader) and
// UltimoMensaje (the most recent entrante turno, capped to
// ultimoMensajeMaxRunes) for the bandeja queue. Both hydration steps degrade
// independently on error: a failure is logged and leaves the corresponding
// field(s) empty rather than failing the whole list.
func (s *Service) hydrateResumen(ctx context.Context, r *ConversacionResumen) {
	clienteID := r.Conversacion.ClienteID()

	facts, err := s.factsReader.GetFacts(ctx, clienteID)
	switch {
	case err != nil:
		s.logger.WarnContext(ctx, "reactivacion_bandeja.facts_reader_failed",
			slog.Int("cliente_id", clienteID), slog.String("error", err.Error()))
	case facts != nil:
		r.Nombre = facts.Nombre
		r.Segmento = facts.Segmento
	}

	turnos, err := s.convRepo.ListarTurnos(ctx, clienteID)
	if err != nil {
		s.logger.WarnContext(ctx, "reactivacion_bandeja.listar_turnos_failed",
			slog.Int("cliente_id", clienteID), slog.String("error", err.Error()))
		return
	}
	r.UltimoMensaje = capRunes(ultimoTurnoEntrante(turnos), ultimoMensajeMaxRunes)
}

// ultimoTurnoEntrante returns the cuerpo of the last entrante turno in turnos
// (assumed ascending CreatedAt order, matching ConversacionRepo.ListarTurnos),
// or "" when there is none.
func ultimoTurnoEntrante(turnos []*domain.Turno) string {
	var ultimo string
	for _, t := range turnos {
		if t.Direccion() == domain.DireccionEntrante {
			ultimo = t.Cuerpo()
		}
	}
	return ultimo
}

// capRunes truncates s to at most maxRunes runes, leaving it untouched when
// shorter. Mirrors the analytics module's caparContexto.
func capRunes(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	return string([]rune(s)[:maxRunes])
}
