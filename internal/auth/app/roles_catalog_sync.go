package app

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
)

// superAdminNombre is the canonical name of the built-in inmutable rol that
// owns every permission. Used by SyncRolesCatalog and by the bootstrap CLI.
const superAdminNombre = "super_admin"

// SyncRolesCatalog ensures the built-in "super_admin" inmutable rol exists
// and owns every permission in domain.AllPermissions(). Run at boot, AFTER
// SyncPermissionCatalog. Idempotent.
//
// The bootUserID is recorded as CREATED_BY on the rol and on each
// rol↔permiso link. Pass the operator's usuario id (the CLI `auth
// bootstrap` command supplies one). If no usuario yet exists in the system
// the routine logs a WARN and returns nil — the catalog will be rebuilt on
// the next boot once an admin usuario has been provisioned.
func (s *Service) SyncRolesCatalog(ctx context.Context, bootUserID uuid.UUID) error {
	hasUser, err := s.hasAnyUsuario(ctx)
	if err != nil {
		return err
	}
	if !hasUser {
		slog.WarnContext(ctx, "auth.roles_catalog_skipped",
			"reason", "no_usuario_exists",
			"hint", "run `auth bootstrap` to create the first usuario, then restart")
		return nil
	}

	return s.runInTx(ctx, func(ctx context.Context) error {
		description := "rol con todos los permisos del sistema"
		now := s.clock.Now()
		rol, err := domain.NewRol(uuid.New(), superAdminNombre, &description, true, bootUserID, now)
		if err != nil {
			return err
		}
		if err := s.roles.UpsertInmutableByName(ctx, rol); err != nil {
			return err
		}

		persisted, err := s.roles.FindByNombre(ctx, superAdminNombre)
		if err != nil {
			return err
		}

		perms := domain.AllPermissions()
		codes := make([]domain.Permission, len(perms))
		for i, p := range perms {
			codes[i] = p.Code
		}

		if err := s.roles.SyncPermisos(ctx, persisted.ID(), codes, bootUserID, now); err != nil {
			return err
		}

		s.enqueueEvent(ctx, outboxAggregateRol, persisted.ID(), eventRolesCatalogSynced, map[string]any{
			"rol_id":        persisted.ID(),
			"nombre":        persisted.Nombre(),
			"permiso_count": len(codes),
		})
		return nil
	})
}

// hasAnyUsuario asks the repository for a single-row page; success with at
// least one item means at least one usuario exists.
func (s *Service) hasAnyUsuario(ctx context.Context) (bool, error) {
	page, err := s.usuarios.List(ctx, outbound.ListParams{PageSize: 1})
	if err != nil {
		return false, err
	}
	return len(page.Items) > 0, nil
}
