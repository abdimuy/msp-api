package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth/domain"
)

// AsignarPermisoARol attaches a permission code to a rol. The rol must
// exist and must not be inmutable — inmutable roles are managed by the
// catalog-sync routine and refuse mutation here. Emits
// "role.permission_granted" on success.
func (s *Service) AsignarPermisoARol(ctx context.Context, rolID uuid.UUID, codigo domain.Permission, by uuid.UUID) error {
	rol, err := s.roles.FindByID(ctx, rolID)
	if err != nil {
		return err
	}
	if rol.Inmutable() {
		return domain.ErrRolInmutable
	}
	if _, err := s.permisos.FindByCodigo(ctx, codigo); err != nil {
		return err
	}

	now := s.clock.Now()
	if err := s.roles.AsignarPermiso(ctx, rolID, codigo, by, now); err != nil {
		return err
	}

	s.enqueueEvent(ctx, outboxAggregateRol, rolID, eventRolePermGranted, map[string]any{
		"rol_id":     rolID,
		"permiso":    codigo.Code(),
		"granted_by": by,
	})
	return nil
}

// RevocarPermisoDeRol detaches a permission code from a rol. Refuses on
// inmutable roles. Emits "role.permission_revoked" on success.
func (s *Service) RevocarPermisoDeRol(ctx context.Context, rolID uuid.UUID, codigo domain.Permission) error {
	rol, err := s.roles.FindByID(ctx, rolID)
	if err != nil {
		return err
	}
	if rol.Inmutable() {
		return domain.ErrRolInmutable
	}
	if err := s.roles.RevocarPermiso(ctx, rolID, codigo); err != nil {
		return err
	}

	s.enqueueEvent(ctx, outboxAggregateRol, rolID, eventRolePermRevoked, map[string]any{
		"rol_id":  rolID,
		"permiso": codigo.Code(),
	})
	return nil
}
