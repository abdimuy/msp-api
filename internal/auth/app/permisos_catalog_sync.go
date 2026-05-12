package app

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth/domain"
)

// SyncPermissionCatalog reconciles MSP_PERMISOS with the in-code catalog
// returned by domain.AllPermissions(). Every known permission is UPSERTed;
// rows whose code is no longer in the source are reported as orphans via a
// structured WARN log. Orphans are NOT deleted — manual cleanup belongs to
// an operator-driven migration once we know no rol still references them.
//
// Runs at boot from the auth module's bootstrap routine.
func (s *Service) SyncPermissionCatalog(ctx context.Context) error {
	perms := domain.AllPermissions()
	if err := s.permisos.UpsertCatalog(ctx, perms); err != nil {
		return err
	}

	known := make([]domain.Permission, len(perms))
	for i, p := range perms {
		known[i] = p.Code
	}
	orphans, err := s.permisos.FindOrphans(ctx, known)
	if err != nil {
		return err
	}
	for _, o := range orphans {
		slog.WarnContext(ctx, "auth.permission_orphan", "code", o.Code())
	}

	// Best-effort outbox event so downstream listeners can react. The
	// aggregate id is a zero-uuid since the catalog has no single owner.
	s.enqueueEvent(ctx, outboxAggregateRol, uuid.Nil, eventPermCatalogSynced, map[string]any{
		"known_count":  len(known),
		"orphan_count": len(orphans),
	})
	return nil
}
