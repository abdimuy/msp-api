// Package app hosts the microsip catalog module's query service. The
// service is intentionally a thin pass-through over the outbound repos —
// the module has no business logic, no validation, and no derived state
// to coordinate. Keeping a Service type here (instead of having handlers
// reach into repos directly) preserves vertical-slice coherence with the
// rest of the codebase: a future cache, rate-limit, or instrumentation
// concern has an obvious home.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/microsip/domain"
	"github.com/abdimuy/msp-api/internal/microsip/ports/outbound"
)

// Service is the query surface for the microsip catalog module.
type Service struct {
	almacenes outbound.AlmacenRepo
	zonas     outbound.ZonaClienteRepo
}

// NewService wires a Service against its repos. Both ports are required —
// the surface is small enough that a partial wiring is never useful.
func NewService(almacenes outbound.AlmacenRepo, zonas outbound.ZonaClienteRepo) *Service {
	return &Service{almacenes: almacenes, zonas: zonas}
}

// ListarAlmacenes returns the full visible-almacen list (cf. AlmacenRepo.Listar).
func (s *Service) ListarAlmacenes(ctx context.Context) ([]domain.Almacen, error) {
	return s.almacenes.Listar(ctx)
}

// ObtenerAlmacen returns a single almacen, or nil when not found.
func (s *Service) ObtenerAlmacen(ctx context.Context, almacenID int) (*domain.Almacen, error) {
	return s.almacenes.Obtener(ctx, almacenID)
}

// ListarArticulosDelAlmacen returns articulos with positive existencias for
// the given almacen, optionally filtered by name substring.
func (s *Service) ListarArticulosDelAlmacen(ctx context.Context, almacenID int, buscar string) ([]domain.ArticuloAlmacen, error) {
	return s.almacenes.ListarArticulos(ctx, almacenID, buscar)
}

// ListarZonasCliente returns the zonas catalog with each name augmented by
// the top cobrador (legacy contract).
func (s *Service) ListarZonasCliente(ctx context.Context) ([]domain.ZonaCliente, error) {
	return s.zonas.Listar(ctx)
}
