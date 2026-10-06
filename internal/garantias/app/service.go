// Package app contains the use cases of the garantias module.
package app

import (
	"context"
	"errors"
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
	IDGenerator    outbound.IDGenerator
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
	idGen     outbound.IDGenerator
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
		idGen:     deps.IDGenerator,
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
//  3. inside a transaction: reject a replayed or foreign idempotency key,
//     lock the folio, mutate it, save it
//  4. a duplicate key surfacing from Guardar is a replay, not a failure: two
//     phones raced and the first had not committed when we looked
//  5. re-read the folio outside the transaction and return it
func (s *Service) ejecutarFolio(
	ctx context.Context,
	garantiaID uuid.UUID,
	permiso domain.Permiso,
	actor domain.ActorParams,
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

	err = s.tx.RunInTx(ctx, func(ctx context.Context) error {
		vista, err := s.eventos.ObtenerPorClaveIdempotencia(ctx, actor.ClaveIdempotencia)
		if err != nil {
			return err
		}
		switch {
		case vista == nil:
		case vista.GarantiaID() == garantiaID:
			// Replay of a command this folio already recorded: succeed with
			// the current state and write nothing.
			return nil
		default:
			return domain.ErrClaveIdempotenciaDeOtroFolio
		}

		g, err := s.garantias.ObtenerParaActualizar(ctx, garantiaID)
		if err != nil {
			return err
		}
		if err := mutar(ctx, g, actor); err != nil {
			return err
		}
		if err := s.garantias.Guardar(ctx, g); err != nil {
			if errors.Is(err, domain.ErrClaveIdempotenciaDuplicada) {
				return nil
			}
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.garantias.Obtener(ctx, garantiaID)
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
