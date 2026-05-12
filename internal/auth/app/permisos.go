package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/auth/domain"
)

// ListarPermisos returns every permiso currently persisted in MSP_PERMISOS,
// ordered by codigo for deterministic output. The catalog is regenerated at
// boot by SyncPermissionCatalog.
func (s *Service) ListarPermisos(ctx context.Context) ([]*domain.Permiso, error) {
	return s.permisos.FindAll(ctx)
}
