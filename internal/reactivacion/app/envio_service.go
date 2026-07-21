//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// EncolarResult summarises the outcome of a [Service.EncolarCohorte] run.
type EncolarResult struct {
	// Encolados is the number of new MSP_RX_MENSAJES rows inserted.
	Encolados int
}

// EncolarCohorte queues the opener message for every treatment cliente
// (EN_CONTROL=0) in the cohorte that does not already have a mensaje.
//
// Idempotent: running it twice never duplicates a mensaje for the same
// cliente — ClientesConMensaje excludes anyone who already has at least one
// row, regardless of that row's estado.
//
// Decision: if domain.CrearMensaje or the opener returns an error for a
// cohorte row, the whole run is aborted and the error is surfaced — a bad
// row indicates a data/logic bug worth surfacing at run time rather than
// silently skipping (mirrors ConstruirCohorte's decision for the same reason).
func (s *Service) EncolarCohorte(ctx context.Context) (EncolarResult, error) {
	const source = "reactivacion.EncolarCohorte"

	now := s.clock.Now()

	cohorte, err := s.repo.ListarCohorte(ctx, outbound.ListarCohorteParams{SoloTratamiento: true})
	if err != nil {
		return EncolarResult{}, apperror.NewInternal("cohorte_list_failed", "error al listar la cohorte de tratamiento").
			WithSource(source).WithError(err)
	}

	existing, err := s.mensajeRepo.ClientesConMensaje(ctx)
	if err != nil {
		return EncolarResult{}, apperror.NewInternal("mensajes_clientes_con_mensaje_failed", "error al leer los clientes ya encolados").
			WithSource(source).WithError(err)
	}

	mensajes, err := s.buildMensajes(cohorte, existing, now, source)
	if err != nil {
		return EncolarResult{}, err
	}
	if len(mensajes) == 0 {
		return EncolarResult{}, nil
	}

	if err := s.runInTx(ctx, func(ctx context.Context) error {
		return s.mensajeRepo.Insertar(ctx, mensajes)
	}); err != nil {
		return EncolarResult{}, apperror.NewInternal("mensajes_insertar_failed", "error al encolar los mensajes de reactivación").
			WithSource(source).WithError(err)
	}

	return EncolarResult{Encolados: len(mensajes)}, nil
}

// buildMensajes generates one domain.Mensaje per cohorte row not already in
// existing, using the opener template for each row's segmento.
func (s *Service) buildMensajes(
	cohorte []*domain.CohorteCliente,
	existing map[int]bool,
	now time.Time,
	source string,
) ([]*domain.Mensaje, error) {
	mensajes := make([]*domain.Mensaje, 0, len(cohorte))
	for _, c := range cohorte {
		if existing[c.ClienteID()] {
			continue
		}

		cuerpo, err := s.opener.Generar(c.Segmento(), c.Nombre())
		if err != nil {
			return nil, apperror.NewInternal("opener_generar_failed", "error al generar el mensaje de apertura").
				WithSource(source).WithError(err)
		}

		m, err := domain.CrearMensaje(domain.CrearMensajeParams{
			ClienteID: c.ClienteID(),
			Segmento:  c.Segmento(),
			Telefono:  c.Telefono(),
			Cuerpo:    cuerpo,
			Now:       now,
		})
		if err != nil {
			return nil, apperror.NewInternal("mensaje_crear_failed", "error al construir el mensaje de reactivación").
				WithSource(source).WithError(err)
		}
		mensajes = append(mensajes, m)
	}
	return mensajes, nil
}

// EncolarEnSegundoPlano launches EncolarCohorte in a detached goroutine so the
// HTTP handler can return 202 immediately.
//
// Single-flight guard: if an encolado run is already in progress, the method
// returns false immediately without starting a second goroutine. The
// goroutine runs with context.Background() so a client disconnect does not
// cancel the work mid-write.
func (s *Service) EncolarEnSegundoPlano() bool {
	if !s.encolarRunning.CompareAndSwap(false, true) {
		return false
	}

	go func() {
		defer s.encolarRunning.Store(false)

		ctx := context.Background()
		s.logger.InfoContext(ctx, "reactivacion_envio.encolar_background_start")

		result, err := s.EncolarCohorte(ctx)
		if err != nil {
			s.logger.ErrorContext(
				ctx, "reactivacion_envio.encolar_background_failed",
				slog.String("error", err.Error()),
			)
			return
		}

		s.logger.InfoContext(
			ctx, "reactivacion_envio.encolar_background_done",
			slog.Int("encolados", result.Encolados),
		)
	}()

	return true
}

// DrenarResult summarises the outcome of a [Service.DrenarCola] batch.
type DrenarResult struct {
	// Enviados is the number of mensajes the sender accepted this batch.
	Enviados int
	// Fallidos is the number of mensajes the sender rejected this batch (left
	// in EstadoFallido; not counted in Enviados or Saltados).
	Fallidos int
	// Bloqueados is the number of mensajes the circuit-breaker paused.
	// Reserved for a future health signal — Fase 2's fake sender is always
	// "healthy", so this is always 0 today.
	Bloqueados int
	// Saltados is the number of pendientes left untouched this batch: every
	// one of them when auto_send is off, or the remaining tail once the
	// gobernador denies the next send (tope diario / fuera de horario / jitter).
	Saltados int
}

// DrenarCola processes up to max pending mensajes (ESTADO='encolado').
//
// When auto_send is off, no mensaje is sent — they all stay encolado waiting
// for the (Fase 3) approval action, and DrenarResult.Saltados counts every
// one of them.
//
// When auto_send is on, each pendiente is checked against the gobernador
// (daily cap, business hours, jitter since the last send THIS batch — the
// repo does not track a cross-batch "last send" timestamp, so a fresh batch
// starts with no jitter constraint). The first denial stops the batch — the
// remaining pendientes stay encolado for the next tick. A successful send
// marks the mensaje enviado, persists it, and marks the cliente contactado —
// all inside one runInTx per mensaje (measurement integrity: FUE_CONTACTADO
// is set ONLY after Enviar succeeds, with SenderKind from sender.Kind()). A
// failed send marks the mensaje fallido; it does NOT touch FUE_CONTACTADO.
func (s *Service) DrenarCola(ctx context.Context, maxN int) (DrenarResult, error) {
	const source = "reactivacion.DrenarCola"
	var result DrenarResult

	pendientes, err := s.mensajeRepo.ListarPendientes(ctx, maxN)
	if err != nil {
		return DrenarResult{}, apperror.NewInternal("mensajes_listar_pendientes_failed", "error al listar los mensajes pendientes").
			WithSource(source).WithError(err)
	}
	if len(pendientes) == 0 {
		return result, nil
	}

	if !s.autoSend {
		result.Saltados = len(pendientes)
		return result, nil
	}

	enviadosHoy, err := s.mensajeRepo.ContarEnviadosHoy(ctx, startOfBusinessDay(s.clock.Now(), s.gobernador.cfg.Zona))
	if err != nil {
		return DrenarResult{}, apperror.NewInternal("mensajes_contar_enviados_failed", "error al contar los mensajes enviados hoy").
			WithSource(source).WithError(err)
	}

	var ultimoEnvio time.Time
	for i, m := range pendientes {
		decision := s.gobernador.PuedeEnviar(enviadosHoy, ultimoEnvio, s.clock.Now())
		if !decision.Permitido {
			result.Saltados += len(pendientes) - i
			break
		}

		if err := s.enviarUno(ctx, m, source, &result); err != nil {
			return DrenarResult{}, err
		}
		if m.Estado() == domain.EstadoEnviado {
			enviadosHoy++
			ultimoEnvio = m.EnviadoEn()
		}
	}

	return result, nil
}

// enviarUno sends one mensaje and persists the outcome, mutating result in
// place. A sender rejection is a normal outcome (Fallidos++, no error
// returned); only a repository failure returns an error.
func (s *Service) enviarUno(ctx context.Context, m *domain.Mensaje, source string, result *DrenarResult) error {
	dest := outbound.Destino{ClienteID: m.ClienteID(), Telefono: m.Telefono()}
	sendErr := s.sender.Enviar(ctx, dest, m.Cuerpo())
	now := s.clock.Now()

	if sendErr != nil {
		m.MarcarFallido(sendErr.Error(), now)
		if err := s.runInTx(ctx, func(ctx context.Context) error {
			return s.mensajeRepo.Actualizar(ctx, m)
		}); err != nil {
			return apperror.NewInternal("mensaje_actualizar_fallido_failed", "error al registrar el mensaje fallido").
				WithSource(source).WithError(err)
		}
		result.Fallidos++
		return nil
	}

	kind := s.sender.Kind()
	if err := m.MarcarEnviado(kind, now); err != nil {
		// Unreachable in practice: ListarPendientes only returns EstadoEncolado
		// rows, so MarcarEnviado's precondition always holds. Surfaced instead
		// of silently swallowed in case that invariant is ever broken.
		return apperror.NewInternal("mensaje_marcar_enviado_failed", "error al marcar el mensaje como enviado").
			WithSource(source).WithError(err)
	}

	if err := s.runInTx(ctx, func(ctx context.Context) error {
		if err := s.mensajeRepo.Actualizar(ctx, m); err != nil {
			return err
		}
		return s.repo.MarcarContactado(ctx, m.ClienteID(), now)
	}); err != nil {
		return apperror.NewInternal("mensaje_marcar_enviado_persist_failed", "error al persistir el envío del mensaje").
			WithSource(source).WithError(err)
	}

	result.Enviados++
	return nil
}

// startOfBusinessDay returns midnight of now's calendar day in loc, converted
// back to now's original representation — the ContarEnviadosHoy watermark the
// gobernador's daily cap counts from.
func startOfBusinessDay(now time.Time, loc *time.Location) time.Time {
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
}

// ListarMensajesParams groups the input parameters for [Service.ListarMensajes].
type ListarMensajesParams struct {
	// Estado restricts results to one estado. Empty string = no filter.
	Estado string
	// Segmento restricts results to one segmento. Empty string = no filter.
	Segmento string
	// Limit caps the number of rows returned. Zero = no explicit cap.
	Limit int
}

// ListarMensajes returns the MSP_RX_MENSAJES rows matching p.
func (s *Service) ListarMensajes(ctx context.Context, p ListarMensajesParams) ([]*domain.Mensaje, error) {
	const source = "reactivacion.ListarMensajes"

	var estado domain.EstadoMensaje
	if p.Estado != "" {
		parsed, err := domain.ParseEstadoMensaje(p.Estado)
		if err != nil {
			return nil, err
		}
		estado = parsed
	}

	var seg domain.Segmento
	if p.Segmento != "" {
		parsed, err := domain.ParseSegmento(p.Segmento)
		if err != nil {
			return nil, err
		}
		seg = parsed
	}

	mensajes, err := s.mensajeRepo.Listar(ctx, outbound.ListarMensajesParams{
		Estado:   estado,
		Segmento: seg,
		Limit:    p.Limit,
	})
	if err != nil {
		return nil, apperror.NewInternal("mensajes_list_failed", "error al listar los mensajes de reactivación").
			WithSource(source).WithError(err)
	}
	return mensajes, nil
}
