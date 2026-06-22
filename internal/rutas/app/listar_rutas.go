//nolint:misspell // rutas vocabulary is Spanish per project convention.
package app

import (
	"context"

	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
	"github.com/abdimuy/msp-api/internal/rutas/ports/outbound"
)

// Service is the rutas module's query surface.
type Service struct {
	repo outbound.RutasRepo
}

// NewService builds a Service wired against the given repo.
func NewService(repo outbound.RutasRepo) *Service {
	return &Service{repo: repo}
}

// ListarRutas returns all zonas with cobrador, client count, and total balance.
func (s *Service) ListarRutas(ctx context.Context) ([]rutasdomain.RutaResumen, error) {
	return s.repo.ListarRutas(ctx)
}
