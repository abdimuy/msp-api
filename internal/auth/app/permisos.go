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

// enrichPermisos resolves a set of bare permission codes (as returned by
// UsuarioRepo.PermisosFor / RolRepo.PermisosFor) into full *domain.Permiso
// values carrying description and categoria. It loads the catalog once and
// filters it by the code set, preserving catalog order (FindAll is ordered
// by codigo) so callers get deterministic output. A code absent from the
// catalog (orphan) is silently skipped — the catalog is source of truth.
func (s *Service) enrichPermisos(ctx context.Context, codes []domain.Permission) ([]*domain.Permiso, error) {
	catalog, err := s.permisos.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	want := make(map[domain.Permission]struct{}, len(codes))
	for _, c := range codes {
		want[c] = struct{}{}
	}
	out := make([]*domain.Permiso, 0, len(codes))
	for _, p := range catalog {
		if _, ok := want[p.Codigo()]; ok {
			out = append(out, p)
		}
	}
	return out, nil
}
