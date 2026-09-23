package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

// ObtenerVisita loads a Visita by its UUID. Returns
// domain.ErrVisitaNoEncontrada when no MSP_VISITAS row matches.
//
// This is the READ path, and it is deliberately permissive about
// TipoVisita: the repo rehydrates whatever the row holds without checking it
// against the closed catalogue (see domain.RehydrateVisita). MSP_VISITAS
// carries 226k legacy rows written before the catalogue existed — some hold
// retired literals, 200 hold the empty string — and every one of them must
// be readable. Validation belongs to RegistrarVisita, the write path.
func (s *Service) ObtenerVisita(ctx context.Context, id uuid.UUID) (*domain.Visita, error) {
	return s.repo.FindByID(ctx, id)
}
