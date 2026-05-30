// Package app contains the cobranza module's query services. It depends only
// on the cobranza domain, the module's outbound ports, and the standard
// library. Wiring (database pool, http handlers) lives in infra; cross-module
// surfaces live in the cobranza root package.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// Service is the cobranza module's query surface. Handlers depend on
// *Service; everything Service depends on goes through the outbound ports.
type Service struct {
	repo  outbound.SaldosRepo
	clock outbound.Clock
}

// NewService builds a Service wired against the given ports.
func NewService(repo outbound.SaldosRepo, clock outbound.Clock) *Service {
	return &Service{repo: repo, clock: clock}
}

// PorVenta returns the cached saldo for the given PV document ID.
// Returns domain.ErrSaldoNoEncontrado when no cache row exists.
func (s *Service) PorVenta(ctx context.Context, doctoPVID int) (*domain.Saldo, error) {
	return s.repo.PorVenta(ctx, doctoPVID)
}

// PorCargo returns the cached saldo for the given cargo (DOCTOS_CC) ID.
// Returns domain.ErrSaldoNoEncontrado when no cache row exists.
func (s *Service) PorCargo(ctx context.Context, doctoCCID int) (*domain.Saldo, error) {
	return s.repo.PorCargo(ctx, doctoCCID)
}

// EnRutaPorZona returns ventas abiertas and recently paid (within ventanaDias
// days) for the given zona. ventanaDias must be in [0, 90]; the HTTP layer
// supplies the default of 7. Returns domain.ErrVentanaDiasInvalida when the
// range constraint is violated.
func (s *Service) EnRutaPorZona(ctx context.Context, zonaID, ventanaDias int) ([]domain.Saldo, error) {
	if ventanaDias < 0 || ventanaDias > 90 {
		return nil, domain.ErrVentanaDiasInvalida
	}
	return s.repo.EnRutaPorZona(ctx, zonaID, ventanaDias)
}

// AbiertasPorCliente returns all open saldos (positive balance, not cancelled)
// for the given cliente.
func (s *Service) AbiertasPorCliente(ctx context.Context, clienteID int) ([]domain.Saldo, error) {
	return s.repo.AbiertasPorCliente(ctx, clienteID)
}

// ResumenZonas returns an aggregated view of open saldos grouped by zona.
func (s *Service) ResumenZonas(ctx context.Context) ([]domain.ResumenZona, error) {
	return s.repo.ResumenZonas(ctx)
}
