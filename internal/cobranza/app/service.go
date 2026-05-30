// Package app contains the cobranza module's query services. It depends only
// on the cobranza domain, the module's outbound ports, and the standard
// library. Wiring (database pool, http handlers) lives in infra; cross-module
// surfaces live in the cobranza root package.
package app

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// DefaultVentanaDias is the value the HTTP handler supplies when neither
// `desde` nor `ventana_dias` is provided. 7 days matches the cobrador's
// typical routing window.
const DefaultVentanaDias = 7

// MaxVentanaDias caps the relative window at 90 days; beyond that the caller
// is expected to use the absolute `desde` parameter, which has no cap.
const MaxVentanaDias = 90

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

// EnRutaPorZona returns ventas abiertas for a zona plus saldadas with
// FECHA_ULT_PAGO >= the resolved cutoff date.
//
// Exactly one of desde or ventanaDias may be non-nil:
//   - desde: explicit RFC3339 cutoff (deterministic across calls); the time
//     component is preserved on the way in but truncated to DATE precision
//     by the underlying column.
//   - ventanaDias: relative window in days, resolved at call time via the
//     injected clock. Must be in [0, MaxVentanaDias].
//   - both nil: defaults to ventanaDias=DefaultVentanaDias (7).
//   - both non-nil: returns ErrParametrosExcluyentes.
//
// When the resolved cutoff is zero-valued, the repo returns only abiertas
// (no UNION branch — faster).
func (s *Service) EnRutaPorZona(ctx context.Context, zonaID int, desde *time.Time, ventanaDias *int) ([]domain.Saldo, error) {
	if desde != nil && ventanaDias != nil {
		return nil, domain.ErrParametrosExcluyentes
	}

	var cutoff time.Time
	switch {
	case desde != nil:
		cutoff = *desde
	case ventanaDias != nil:
		if *ventanaDias < 0 || *ventanaDias > MaxVentanaDias {
			return nil, domain.ErrVentanaDiasInvalida
		}
		if *ventanaDias > 0 {
			cutoff = s.clock.Now().AddDate(0, 0, -*ventanaDias)
		}
	default:
		cutoff = s.clock.Now().AddDate(0, 0, -DefaultVentanaDias)
	}

	return s.repo.EnRutaPorZona(ctx, zonaID, cutoff)
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
