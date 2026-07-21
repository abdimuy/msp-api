//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/llm"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// ProcesarResult is the outcome of one ProcesarMensajeEntrante call.
type ProcesarResult struct {
	// Decision is the audit-trail row created for this inbound turno.
	Decision *domain.Decision
	// Borrador is the copiloto's drafted reply, saved PENDING (shadow mode —
	// never sent without operator approval). Empty when Escalada is true.
	Borrador string
	// Escalada reports whether the FINAL (triar) decision was to escalate.
	Escalada bool
}

// ProcesarMensajeEntrante is the copiloto's inbound-message loop: it records
// the cliente's message, asks the LLM to analyze it, applies the
// deterministic triar policy to the raw output, and persists the outcome —
// a new Turno (and, when responding, a PENDING draft reply Turno) plus a new
// Decision — all atomically.
//
// The LLM only PROPOSES via AnalizarOutput; triar makes the actual
// escalate/respond call, so nothing here ever sends a message — an approved
// draft is enqueued only via AprobarBorrador/EditarYAprobar (shadow mode).
func (s *Service) ProcesarMensajeEntrante(ctx context.Context, clienteID int, mensaje string) (ProcesarResult, error) {
	const source = "reactivacion.ProcesarMensajeEntrante"

	if strings.TrimSpace(mensaje) == "" {
		return ProcesarResult{}, ErrMensajeEntranteVacio
	}

	now := s.clock.Now()

	conv, err := s.obtenerOCrearConversacion(ctx, clienteID, now, source)
	if err != nil {
		return ProcesarResult{}, err
	}

	nombre, segmento := s.resolveFactsNombreSegmento(ctx, clienteID)
	s.asegurarContextoNota(ctx, conv, nombre, segmento, now)
	resumen := s.construirResumenActual(ctx, clienteID)

	in := outbound.AnalizarInput{
		ResumenMemoria:  resumen,
		MensajeEntrante: mensaje,
		Nombre:          nombre,
		Segmento:        segmento,
		ContextoNota:    conv.ContextoNota(),
		Banderas:        conv.Banderas(),
		Allowlist:       allowlistText(),
	}

	out, df, err := s.analizarConDegradacion(ctx, in, clienteID, source)
	if err != nil {
		return ProcesarResult{}, err
	}

	var decision *domain.Decision
	var borradorGuardado string
	err = s.runInTx(ctx, func(ctx context.Context) error {
		var txErr error
		decision, borradorGuardado, txErr = s.persistirTurnoEntranteYDecision(ctx, clienteID, mensaje, resumen, conv, out, df, now)
		return txErr
	})
	if err != nil {
		return ProcesarResult{}, apperror.NewInternal("procesar_entrante_persist_failed", "error al procesar el mensaje entrante").
			WithSource(source).WithError(err)
	}

	return ProcesarResult{
		Decision: decision,
		Borrador: borradorGuardado,
		Escalada: df.Accion == domain.AccionEscalar,
	}, nil
}

// obtenerOCrearConversacion loads clienteID's Conversacion, or creates a
// fresh one (in EstadoContactado) when this is its first inbound message.
func (s *Service) obtenerOCrearConversacion(ctx context.Context, clienteID int, now time.Time, source string) (*domain.Conversacion, error) {
	conv, err := s.convRepo.Get(ctx, clienteID)
	if err != nil {
		return nil, apperror.NewInternal("conversacion_get_failed", "error al leer la conversación del cliente").
			WithSource(source).WithError(err)
	}
	if conv != nil {
		return conv, nil
	}
	return domain.CrearConversacion(clienteID, now)
}

// resolveFactsNombreSegmento reads clienteID's ClienteFacts for the LLM
// prompt. A reader error degrades to empty nombre/segmento (logged, not
// fatal) — the copiloto can still analyze the message without them.
func (s *Service) resolveFactsNombreSegmento(ctx context.Context, clienteID int) (string, string) {
	facts, err := s.factsReader.GetFacts(ctx, clienteID)
	if err != nil {
		s.logger.WarnContext(ctx, "reactivacion_copiloto.facts_reader_failed",
			slog.Int("cliente_id", clienteID), slog.String("error", err.Error()))
		return "", ""
	}
	if facts == nil {
		return "", ""
	}
	return facts.Nombre, facts.Segmento
}

// construirResumenActual builds the memory summary from clienteID's turno
// history so far. A listing error degrades to an empty turno list (logged,
// not fatal) — construirResumen(nil) still yields a valid "sin historial"
// summary.
func (s *Service) construirResumenActual(ctx context.Context, clienteID int) string {
	turnos, err := s.convRepo.ListarTurnos(ctx, clienteID)
	if err != nil {
		s.logger.WarnContext(ctx, "reactivacion_copiloto.listar_turnos_failed",
			slog.Int("cliente_id", clienteID), slog.String("error", err.Error()))
	}
	return construirResumen(turnos)
}

// analizarConDegradacion calls CopilotoLLM.Analizar and applies the SAFE
// FALLBACK for an unreachable copiloto: llm.ErrLLMDisabled or a transient
// error degrades to a synthetic escalate (razonCopilotoNoDisponible) instead
// of failing the whole inbound turn — a human must always end up handling a
// message the copiloto could not read. Any OTHER Analizar error is fatal and
// returned as-is. On success, triar makes the final escalate/respond call.
func (s *Service) analizarConDegradacion(
	ctx context.Context, in outbound.AnalizarInput, clienteID int, source string,
) (outbound.AnalizarOutput, DecisionFinal, error) {
	out, err := s.copilotoLLM.Analizar(ctx, in)
	switch {
	case errors.Is(err, llm.ErrLLMDisabled), llm.IsTransient(err):
		s.logger.WarnContext(ctx, "reactivacion_copiloto.llm_no_disponible",
			slog.Int("cliente_id", clienteID), slog.String("error", err.Error()))
		return outbound.AnalizarOutput{}, escalarFinal(razonCopilotoNoDisponible), nil
	case err != nil:
		return outbound.AnalizarOutput{}, DecisionFinal{}, apperror.NewInternal(
			"copiloto_analizar_failed", "error al analizar el mensaje entrante",
		).
			WithSource(source).WithError(err)
	default:
		return out, triar(out), nil
	}
}

// persistirTurnoEntranteYDecision performs every write of one inbound turn:
// the entrante Turno, the Conversacion's estado transition, the Decision
// audit row, and — when df.Accion is responder — a PENDING draft saliente
// Turno (shadow mode: never sent here). Called inside runInTx.
func (s *Service) persistirTurnoEntranteYDecision(
	ctx context.Context,
	clienteID int,
	mensaje, resumen string,
	conv *domain.Conversacion,
	out outbound.AnalizarOutput,
	df DecisionFinal,
	now time.Time,
) (*domain.Decision, string, error) {
	turnoEntrante, err := domain.CrearTurno(domain.CrearTurnoParams{
		ClienteID: clienteID,
		Direccion: domain.DireccionEntrante,
		Autor:     domain.AutorCliente,
		Cuerpo:    mensaje,
		Now:       now,
	})
	if err != nil {
		return nil, "", err
	}
	if err := s.convRepo.AppendTurno(ctx, turnoEntrante); err != nil {
		return nil, "", err
	}

	if err := s.transicionarEstado(conv, df, now); err != nil {
		return nil, "", err
	}

	d, err := domain.CrearDecision(domain.CrearDecisionParams{
		ClienteID:         clienteID,
		TurnoRef:          turnoEntrante.ID(),
		Intencion:         out.Intencion,
		Confianza:         out.Confianza,
		Senales:           out.Senales,
		Accion:            df.Accion,
		Borrador:          out.Borrador,
		Evidencia:         out.Evidencia,
		RazonEscalamiento: df.RazonEscalamiento,
		Resultado:         df.Resultado,
		Now:               now,
	})
	if err != nil {
		return nil, "", err
	}
	if err := s.decisionRepo.Insertar(ctx, d); err != nil {
		return nil, "", err
	}

	var borradorGuardado string
	if df.Accion == domain.AccionResponder {
		// By design, this runs even when conv is already EstadoEscalado (triar
		// did not escalate THIS turn, but a human already owns the conversation
		// per transicionarEstado above): the human keeps ownership of the estado,
		// but a suggested draft is still useful for them to review/approve.
		turnoSaliente, err := domain.CrearTurno(domain.CrearTurnoParams{
			ClienteID: clienteID,
			Direccion: domain.DireccionSaliente,
			Autor:     domain.AutorIA,
			Cuerpo:    out.Borrador,
			Now:       now,
		})
		if err != nil {
			return nil, "", err
		}
		// PENDING draft — shadow mode: never sent here, only saved for an
		// operator to review via AprobarBorrador/EditarYAprobar.
		if err := s.convRepo.AppendTurno(ctx, turnoSaliente); err != nil {
			return nil, "", err
		}
		borradorGuardado = out.Borrador
	}

	conv.SetResumenMemoria(resumen, now)
	if err := s.convRepo.Upsert(ctx, conv); err != nil {
		return nil, "", err
	}

	return d, borradorGuardado, nil
}

// transicionarEstado applies this inbound turn's estado transitions: a first
// reply moves contactado → respondio, then either escalar or conversando —
// except a conversation a human already owns (escalado) never regresses to
// conversando just because triar itself did not escalate THIS turn.
func (s *Service) transicionarEstado(conv *domain.Conversacion, df DecisionFinal, now time.Time) error {
	if conv.Estado() == domain.EstadoContactado {
		if err := conv.MarcarRespondio(now); err != nil {
			return err
		}
	}
	if df.Accion == domain.AccionEscalar {
		return conv.MarcarEscalada("", now)
	}
	if conv.Estado() != domain.EstadoEscalado {
		return conv.MarcarConversando(now)
	}
	return nil
}
