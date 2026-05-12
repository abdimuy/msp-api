package app

import (
	"context"

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
func (s *Service) CrearRol(ctx context.Context, p CrearRolParams, by uuid.UUID) (*domain.Rol, error) {
	id := uuid.New()
	now := s.clock.Now()
	rol, err := domain.NewRol(id, p.Nombre, p.Description, false, by, now)
	if err != nil {
		return nil, err
	}
	if err := s.roles.Save(ctx, rol); err != nil {
		return nil, err
	}

	s.enqueueEvent(ctx, outboxAggregateRol, rol.ID(), eventRoleCreated, map[string]any{
		"rol_id":     rol.ID(),
		"nombre":     rol.Nombre(),
		"created_by": by,
	})
	return rol, nil
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
