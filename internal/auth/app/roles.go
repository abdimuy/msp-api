package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
)

// CrearRolParams carries the input for creating a new user-defined rol.
type CrearRolParams struct {
	Nombre      string
	Description *string
}

// CrearRol creates a non-inmutable rol. The domain constructor validates the
// name and description; on success a "role.created" event is enqueued.
//
// Because "delete" is a soft-deactivation and MSP_ROLES.NOMBRE is UNIQUE, a
// name freed by a soft-deleted rol would otherwise collide on re-creation.
// To keep the intuitive "delete then create the same name again" flow working,
// re-creating a name held only by a soft-deleted rol REACTIVATES that rol
// cleanly: it is re-activated, its description reset to the new value, and its
// previous permisos dropped — so it behaves like a brand-new rol. A name held
// by an ACTIVO or inmutable rol is still a genuine conflict (ErrRolYaExiste).
func (s *Service) CrearRol(ctx context.Context, p CrearRolParams, by uuid.UUID) (*domain.Rol, error) {
	now := s.clock.Now()
	// Build+validate first so we work with the normalized (trimmed) name.
	nuevo, err := domain.NewRol(uuid.New(), p.Nombre, p.Description, false, by, now)
	if err != nil {
		return nil, err
	}

	var result *domain.Rol
	err = s.runInTx(ctx, func(ctx context.Context) error {
		existing, ferr := s.roles.FindByNombre(ctx, nuevo.Nombre())
		switch {
		case ferr == nil:
			// A rol with this name already exists.
			if existing.Activo() || existing.Inmutable() {
				return domain.ErrRolYaExiste
			}
			// Soft-deleted rol: reactivate it as if freshly created.
			if aerr := existing.Reactivar(by, now); aerr != nil {
				return aerr
			}
			if uerr := existing.Update(nuevo.Nombre(), p.Description, by, now); uerr != nil {
				return uerr
			}
			if uerr := s.roles.Update(ctx, existing); uerr != nil {
				return uerr
			}
			if serr := s.roles.SyncPermisos(ctx, existing.ID(), nil, by, now); serr != nil {
				return serr
			}
			result = existing
		case errors.Is(ferr, domain.ErrRolNotFound):
			if serr := s.roles.Save(ctx, nuevo); serr != nil {
				return serr
			}
			result = nuevo
		default:
			return ferr
		}

		s.enqueueEvent(ctx, outboxAggregateRol, result.ID(), eventRoleCreated, map[string]any{
			"rol_id":     result.ID(),
			"nombre":     result.Nombre(),
			"created_by": by,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ActualizarRolParams carries the input for editing an existing rol.
type ActualizarRolParams struct {
	ID          uuid.UUID
	Nombre      string
	Description *string
}

// ActualizarRol updates an existing rol's name and description. The domain
// refuses with ErrRolInmutable when the rol is system-managed.
func (s *Service) ActualizarRol(ctx context.Context, p ActualizarRolParams, by uuid.UUID) (*domain.Rol, error) {
	rol, err := s.roles.FindByID(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if err := rol.Update(p.Nombre, p.Description, by, s.clock.Now()); err != nil {
		return nil, err
	}
	if err := s.roles.Update(ctx, rol); err != nil {
		return nil, err
	}

	s.enqueueEvent(ctx, outboxAggregateRol, rol.ID(), eventRoleUpdated, map[string]any{
		"rol_id":     rol.ID(),
		"nombre":     rol.Nombre(),
		"updated_by": by,
	})
	return rol, nil
}

// DesactivarRol soft-deactivates a rol. Refuses inmutable roles via the
// domain method. Emits "role.deactivated" on success.
func (s *Service) DesactivarRol(ctx context.Context, id, by uuid.UUID) error {
	rol, err := s.roles.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if err := rol.Desactivar(by, s.clock.Now()); err != nil {
		return err
	}
	if err := s.roles.Update(ctx, rol); err != nil {
		return err
	}

	s.enqueueEvent(ctx, outboxAggregateRol, rol.ID(), eventRoleDeactivated, map[string]any{
		"rol_id":         rol.ID(),
		"deactivated_by": by,
	})
	return nil
}

// ObtenerRol loads a single rol by id. Returns ErrRolNotFound on miss.
func (s *Service) ObtenerRol(ctx context.Context, id uuid.UUID) (*domain.Rol, error) {
	return s.roles.FindByID(ctx, id)
}

// ListarRoles returns a cursor-paginated page of roles.
func (s *Service) ListarRoles(ctx context.Context, p outbound.ListParams) (outbound.Page[*domain.Rol], error) {
	return s.roles.List(ctx, p)
}
