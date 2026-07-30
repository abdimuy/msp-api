// Package app contains the visitas module's command service. It depends only
// on the visitas domain, the module's outbound ports, and the standard
// library. Wiring (database pool, http handlers) lives in infra; cross-module
// surfaces live in the visitas root package.
package app

import (
	"github.com/abdimuy/msp-api/internal/visitas/ports/outbound"
)

// Service is the visitas module's command surface. Handlers depend on
// *Service; everything Service depends on goes through the outbound ports.
type Service struct {
	repo  outbound.VisitasRepo
	clock outbound.Clock
}

// NewService builds a Service wired against the given ports.
func NewService(repo outbound.VisitasRepo, clock outbound.Clock) *Service {
	return &Service{
		repo:  repo,
		clock: clock,
	}
}
