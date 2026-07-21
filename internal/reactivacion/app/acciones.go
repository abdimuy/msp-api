//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"context"
	"strings"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// razonEscaladoPorOperador is the RazonEscalamiento recorded when a human
// operator escalates directly, as opposed to the LLM/triar policy escalating.
const razonEscaladoPorOperador = "escalado por el operador"

// Decisiones are an append-only audit log: every operator action below
// APPENDS a new Decision row, never mutates an existing one.

// newestDecisionPorClienteID returns the "newest" decision among decisiones,
// or nil when decisiones is empty.
//
// Contract: decisiones is assumed to already be in ascending CreatedAt/
// insertion order, per DecisionRepo.ListarPorCliente's documented contract
// ("ordered by CreatedAt ascending") — so "newest" means the LAST element.
//
// On a CreatedAt TIE (legacy Firebird's TIMESTAMP has ~100µs resolution, so
// two decisions written moments apart in the same operator action — e.g. a
// propuesto immediately followed by its aprobado — can share a wall-clock
// value), the scan must resolve the tie by keeping the
// LATER-in-the-list element, i.e. the LAST-inserted among the tied rows.
// This is why the comparison below is "!Before" (later-OR-EQUAL wins) rather
// than a strict "After": a strict After would keep the FIRST element of a
// tied group, silently resurrecting an already-superseded propuesto and
// breaking AprobarBorrador/EditarYAprobar's idempotency guarantee (a second
// approve would find that stale propuesto and re-enqueue the message).
func newestDecisionPorClienteID(decisiones []*domain.Decision) *domain.Decision {
	if len(decisiones) == 0 {
		return nil
	}
	newest := decisiones[0]
	for _, d := range decisiones[1:] {
		if !d.CreatedAt().Before(newest.CreatedAt()) {
			newest = d
		}
	}
	return newest
}

// pendingDraft returns the cliente's newest decision when it is a pending
// draft (accion responder, resultado propuesto) ready for an operator to
// approve/edit, or nil otherwise (including when there is no decision at
// all, or the newest one was already acted on).
func pendingDraft(decisiones []*domain.Decision) *domain.Decision {
	newest := newestDecisionPorClienteID(decisiones)
	if newest == nil {
		return nil
	}
	if newest.AccionPropuesta() != domain.AccionResponder || newest.Resultado() != domain.ResultadoPropuesto {
		return nil
	}
	return newest
}

// enqueueAprobado enqueues cuerpo via the Fase 2 channel (mensajeRepo — set
// via WithCanal) for clienteID, then appends a new Decision carrying
// resultado, cloning the rest of base's audit fields (Intencion, Confianza,
// Senales, Evidencia) so the append-only log keeps the LLM's original
// analysis alongside the operator's outcome. Runs both writes in one
// transaction.
func (s *Service) enqueueAprobado(
	ctx context.Context,
	clienteID int,
	base *domain.Decision,
	cuerpo string,
	resultado domain.ResultadoDecision,
	source string,
) error {
	facts, err := s.factsReader.GetFacts(ctx, clienteID)
	if err != nil {
		return apperror.NewInternal("facts_get_failed", "error al leer los datos de contacto del cliente").
			WithSource(source).WithError(err)
	}
	if facts == nil {
		return ErrClienteSinDatosContacto
	}

	seg, err := domain.ParseSegmento(facts.Segmento)
	if err != nil {
		return err
	}

	now := s.clock.Now()
	return s.runInTx(ctx, func(ctx context.Context) error {
		m, err := domain.CrearMensaje(domain.CrearMensajeParams{
			ClienteID: clienteID,
			Segmento:  seg,
			Telefono:  facts.Telefono,
			Cuerpo:    cuerpo,
			Now:       now,
		})
		if err != nil {
			return err
		}
		if err := s.mensajeRepo.Insertar(ctx, []*domain.Mensaje{m}); err != nil {
			return err
		}

		d, err := domain.CrearDecision(domain.CrearDecisionParams{
			ClienteID:         clienteID,
			TurnoRef:          base.TurnoRef(),
			Intencion:         base.Intencion(),
			Confianza:         base.Confianza(),
			Senales:           base.Senales(),
			Accion:            domain.AccionResponder,
			Borrador:          cuerpo,
			Evidencia:         base.Evidencia(),
			RazonEscalamiento: "",
			Resultado:         resultado,
			Now:               now,
		})
		if err != nil {
			return err
		}
		return s.decisionRepo.Insertar(ctx, d)
	})
}

// AprobarBorrador enqueues the cliente's newest pending draft as-is via the
// Fase 2 channel, then appends a Decision recording the approval.
//
// Idempotent: a second call finds the newest decision already resultado
// aprobado (not propuesto) and returns ErrNoHayBorradorPendiente WITHOUT
// re-enqueuing — approving twice never sends the message twice.
func (s *Service) AprobarBorrador(ctx context.Context, clienteID int) error {
	const source = "reactivacion.AprobarBorrador"

	decisiones, err := s.decisionRepo.ListarPorCliente(ctx, clienteID)
	if err != nil {
		return apperror.NewInternal("decisiones_list_failed", "error al listar las decisiones del cliente").
			WithSource(source).WithError(err)
	}
	draft := pendingDraft(decisiones)
	if draft == nil {
		return ErrNoHayBorradorPendiente
	}

	return s.enqueueAprobado(ctx, clienteID, draft, draft.Borrador(), domain.ResultadoAprobado, source)
}

// EditarYAprobar enqueues texto (the operator's edited version of the
// cliente's newest pending draft) via the Fase 2 channel, then appends a
// Decision recording the edit. Same idempotency contract as AprobarBorrador.
func (s *Service) EditarYAprobar(ctx context.Context, clienteID int, texto string) error {
	const source = "reactivacion.EditarYAprobar"

	if strings.TrimSpace(texto) == "" {
		return ErrTextoEditadoVacio
	}

	decisiones, err := s.decisionRepo.ListarPorCliente(ctx, clienteID)
	if err != nil {
		return apperror.NewInternal("decisiones_list_failed", "error al listar las decisiones del cliente").
			WithSource(source).WithError(err)
	}
	draft := pendingDraft(decisiones)
	if draft == nil {
		return ErrNoHayBorradorPendiente
	}

	return s.enqueueAprobado(ctx, clienteID, draft, texto, domain.ResultadoEditado, source)
}

// Escalar hands clienteID's conversation off to asignadoA — an operator
// escalating directly (as opposed to the LLM/triar policy escalating a
// specific turn). Appends a Decision recording the escalation and persists
// the conversation's new estado, both in one transaction.
func (s *Service) Escalar(ctx context.Context, clienteID int, asignadoA string) error {
	const source = "reactivacion.Escalar"

	conv, err := s.convRepo.Get(ctx, clienteID)
	if err != nil {
		return apperror.NewInternal("conversacion_get_failed", "error al leer la conversación del cliente").
			WithSource(source).WithError(err)
	}
	if conv == nil {
		return ErrConversacionNoEncontrada
	}

	now := s.clock.Now()
	if err := conv.MarcarEscalada(asignadoA, now); err != nil {
		return err
	}

	err = s.runInTx(ctx, func(ctx context.Context) error {
		d, err := domain.CrearDecision(domain.CrearDecisionParams{
			ClienteID:         clienteID,
			Accion:            domain.AccionEscalar,
			Resultado:         domain.ResultadoEscalado,
			RazonEscalamiento: razonEscaladoPorOperador,
			Now:               now,
		})
		if err != nil {
			return err
		}
		if err := s.decisionRepo.Insertar(ctx, d); err != nil {
			return err
		}
		return s.convRepo.Upsert(ctx, conv)
	})
	if err != nil {
		return apperror.NewInternal("escalar_persist_failed", "error al escalar la conversación").
			WithSource(source).WithError(err)
	}
	return nil
}

// Dictar drafts a fresh AI reply from an operator's stated intent (as opposed
// to reacting to a cliente message, which is ProcesarMensajeEntrante's job),
// saves it as a PENDING draft turno, and appends a Decision proposing it —
// giving the operator a new borrador they can then AprobarBorrador. Returns
// the drafted borrador.
func (s *Service) Dictar(ctx context.Context, clienteID int, intencion string) (string, error) {
	const source = "reactivacion.Dictar"

	if strings.TrimSpace(intencion) == "" {
		return "", ErrIntencionVacia
	}

	conv, err := s.convRepo.Get(ctx, clienteID)
	if err != nil {
		return "", apperror.NewInternal("conversacion_get_failed", "error al leer la conversación del cliente").
			WithSource(source).WithError(err)
	}
	if conv == nil {
		return "", ErrConversacionNoEncontrada
	}

	var nombre, segmento string
	if facts, factsErr := s.factsReader.GetFacts(ctx, clienteID); factsErr == nil && facts != nil {
		nombre, segmento = facts.Nombre, facts.Segmento
	}

	turnos, err := s.convRepo.ListarTurnos(ctx, clienteID)
	if err != nil {
		return "", apperror.NewInternal("turnos_list_failed", "error al listar los turnos de la conversación").
			WithSource(source).WithError(err)
	}
	resumen := construirResumen(turnos)

	borrador, err := s.copilotoLLM.Redactar(ctx, outbound.RedactarInput{
		Intencion:      intencion,
		ResumenMemoria: resumen,
		Nombre:         nombre,
		Segmento:       segmento,
		ContextoNota:   conv.ContextoNota(),
		Banderas:       conv.Banderas(),
		Allowlist:      allowlistText(),
	})
	if err != nil {
		return "", apperror.NewInternal("copiloto_redactar_failed", "error al redactar el mensaje dictado").
			WithSource(source).WithError(err)
	}

	now := s.clock.Now()
	err = s.runInTx(ctx, func(ctx context.Context) error {
		turno, err := domain.CrearTurno(domain.CrearTurnoParams{
			ClienteID: clienteID,
			Direccion: domain.DireccionSaliente,
			Autor:     domain.AutorIA,
			Cuerpo:    borrador,
			Now:       now,
		})
		if err != nil {
			return err
		}
		if err := s.convRepo.AppendTurno(ctx, turno); err != nil {
			return err
		}

		d, err := domain.CrearDecision(domain.CrearDecisionParams{
			ClienteID: clienteID,
			TurnoRef:  turno.ID(),
			Accion:    domain.AccionResponder,
			Borrador:  borrador,
			Resultado: domain.ResultadoPropuesto,
			Now:       now,
		})
		if err != nil {
			return err
		}
		return s.decisionRepo.Insertar(ctx, d)
	})
	if err != nil {
		return "", apperror.NewInternal("dictar_persist_failed", "error al guardar el borrador dictado").
			WithSource(source).WithError(err)
	}

	return borrador, nil
}
