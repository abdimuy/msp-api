// Package app contains the use cases of the garantias module.
package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

// Deps holds the outbound dependencies of the garantias application service.
type Deps struct {
	GarantiaRepo   outbound.GarantiaRepo
	EventoRepo     outbound.EventoRepo
	FolioGenerator outbound.FolioGenerator
	TxRunner       outbound.TxRunner
	Identity       outbound.Identity
	Clock          outbound.Clock
}

// Service is the application service for garantias commands.
//
// An aggregate is saved once: a command loads it, mutates it, saves it and
// drops it. The repository does not empty eventosPendientes after saving and
// no command reuses an aggregate that was already saved.
type Service struct {
	garantias outbound.GarantiaRepo
	eventos   outbound.EventoRepo
	folios    outbound.FolioGenerator
	tx        outbound.TxRunner
	identity  outbound.Identity
	clock     outbound.Clock
}

// NewService creates a new Service.
func NewService(deps Deps) *Service {
	return &Service{
		garantias: deps.GarantiaRepo,
		eventos:   deps.EventoRepo,
		folios:    deps.FolioGenerator,
		tx:        deps.TxRunner,
		identity:  deps.Identity,
		clock:     deps.Clock,
	}
}

// operacion is what a command does to the folio once it is locked and the
// idempotency key is clear. Every mutating command is this plus the identity
// and permission checks around it, which is why the five steps live in one
// helper instead of being copied per command.
type operacion func(ctx context.Context, g *domain.Garantia, actor domain.ActorParams) error

// ejecutarFolio applies the five steps every mutating command shares:
//
//  1. resolve the user from Identity — never from the command
//  2. check the command's permission
//  3. inside a transaction: lock the folio first, then reject a replayed or
//     foreign idempotency key, mutate it, save it
//  4. a duplicate key surfacing from Guardar is a rollback, not a commit: the
//     write already placed the folio rows but failed on the event UNIQUE, and
//     committing them would persist a change without its event (§4.4)
//  5. after the rollback, re-read who owns the key — the twin won, so the
//     command answers with the current state — and re-read the folio outside
//     the transaction to return it
//
// tipo is the event this command records. The lock must come before the key
// lookup (3 before a): two requests to the same folio then serialize, the
// second one sees the first one's event and answers as a replay. That serial
// behavior is not a performance detail — it is what the phone receives: the
// simultaneous retry must be a success, not a stage_transition_forbidden over
// an article that actually saved.
func (s *Service) ejecutarFolio(
	ctx context.Context,
	garantiaID uuid.UUID,
	permiso domain.Permiso,
	actor domain.ActorParams,
	tipo domain.TipoEvento,
	mutar operacion,
) (*domain.Garantia, error) {
	usuario, err := s.usuario(ctx)
	if err != nil {
		return nil, err
	}
	actor.Usuario = usuario
	if err := s.exigirPermiso(ctx, permiso); err != nil {
		return nil, err
	}
	clave, err := normalizarClave(actor.ClaveIdempotencia)
	if err != nil {
		return nil, err
	}
	actor.ClaveIdempotencia = clave

	err = s.tx.RunInTx(ctx, func(ctx context.Context) error {
		// Lock the folio before looking at the key. With the lock held, two
		// requests to the same folio serialize, so the second one does see the
		// event the first one wrote and the cheap replay path below triggers
		// instead of racing into Guardar.
		g, err := s.garantias.ObtenerParaActualizar(ctx, garantiaID)
		if err != nil {
			return err
		}
		vista, err := s.eventos.ObtenerPorClaveIdempotencia(ctx, actor.ClaveIdempotencia)
		if err != nil {
			return err
		}
		repite, err := revisarClave(vista, garantiaID, tipo)
		if err != nil {
			return err
		}
		if repite {
			// Replay of this exact command: succeed with the current state
			// and write nothing.
			return nil
		}

		if err := mutar(ctx, g, actor); err != nil {
			return err
		}
		if err := s.garantias.Guardar(ctx, g); err != nil {
			if errors.Is(err, domain.ErrClaveIdempotenciaDuplicada) {
				// Return the error so RunInTx rolls back the half-written
				// folio; the twin's key is resolved again right below.
				return domain.ErrClaveIdempotenciaDuplicada
			}
			return err
		}
		return nil
	})
	if err != nil {
		if !errors.Is(err, domain.ErrClaveIdempotenciaDuplicada) {
			return nil, err
		}
		return s.resolverDuplicada(ctx, actor.ClaveIdempotencia, func(v *domain.Evento) bool {
			return v.GarantiaID() == garantiaID && v.Tipo() == tipo
		})
	}
	return s.garantias.Obtener(ctx, garantiaID)
}

// revisarClave decides what an already-registered idempotency key means for
// this command, inside the transaction and after the folio is locked. Absent:
// the command proceeds. Present on this same folio and of this same event
// type: a replay of this exact command, which succeeds writing nothing. Any
// other owner is a key reused against the wrong folio or the wrong command.
func revisarClave(vista *domain.Evento, garantiaID uuid.UUID, tipo domain.TipoEvento) (bool, error) {
	switch {
	case vista == nil:
		return false, nil
	case vista.GarantiaID() == garantiaID && vista.Tipo() == tipo:
		return true, nil
	default:
		return false, domain.ErrClaveIdempotenciaDeOtroFolio
	}
}

// resolverDuplicada runs after the rollback caused by a duplicate key: the
// twin's event owns it now. An absent key means there was no twin and the
// write is simply lost; an event that is not ours means the key was reused
// against the wrong folio (or command). Otherwise the folio behind the event is
// re-read and returned.
func (s *Service) resolverDuplicada(ctx context.Context, clave string, esMia func(*domain.Evento) bool) (*domain.Garantia, error) {
	vista, err := s.eventos.ObtenerPorClaveIdempotencia(ctx, clave)
	if err != nil {
		return nil, err
	}
	switch {
	case vista == nil:
		return nil, domain.ErrClaveIdempotenciaDuplicada
	case !esMia(vista):
		return nil, domain.ErrClaveIdempotenciaDeOtroFolio
	}
	return s.garantias.Obtener(ctx, vista.GarantiaID())
}

// normalizarClave canonicalizes the idempotency key before any lookup. The
// domain stores the canonical form (parsed.String(), ronda 3 of #24): a
// search done with the raw spelling would miss a replay sent in uppercase or
// with braces and let it race into the UNIQUE index again.
func normalizarClave(clave string) (string, error) {
	clave = strings.TrimSpace(clave)
	if clave == "" {
		return "", domain.ErrEventoClaveIdempotenciaObligatoria
	}
	parsed, err := uuid.Parse(clave)
	if err != nil {
		return "", domain.ErrEventoClaveIdempotenciaInvalida
	}
	return parsed.String(), nil
}

// usuario resolves the caller from Identity. The value never comes from the
// command: if it did, a phone could sign as somebody else.
func (s *Service) usuario(ctx context.Context) (string, error) {
	u, err := s.identity.UsuarioActual(ctx)
	if err != nil {
		return "", err
	}
	return u.Nombre, nil
}

// exigirPermiso enforces the permission in app, not in http: the command knows
// which action is being asked for, the handler only knows the route.
func (s *Service) exigirPermiso(ctx context.Context, p domain.Permiso) error {
	ok, err := s.identity.TienePermiso(ctx, p)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrPermisoDenegado
	}
	return nil
}

// actorDe builds the ActorParams of a command. Usuario is filled in by
// ejecutarFolio from Identity; the rest (key, device time, GPS) comes from the
// command because it describes what the device reported, not who is calling.
func actorDe(clave string, deviceCreatedAt time.Time, lat, lon *float64) domain.ActorParams {
	return domain.ActorParams{
		ClaveIdempotencia: clave,
		DeviceCreatedAt:   deviceCreatedAt,
		GPSLat:            lat,
		GPSLon:            lon,
	}
}
