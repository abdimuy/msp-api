//nolint:misspell // visitas vocabulary is Spanish per project convention.
package main

import (
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	visitasapp "github.com/abdimuy/msp-api/internal/visitas/app"
	visitasfb "github.com/abdimuy/msp-api/internal/visitas/infra/visitasfb"
	visitasoutbound "github.com/abdimuy/msp-api/internal/visitas/ports/outbound"
)

// provideVisitasClock returns the production UTC clock for the visitas module.
func provideVisitasClock() visitasoutbound.Clock {
	return visitasoutbound.ProductionClock{}
}

// provideVisitasRepo builds the Firebird-backed VisitasRepo.
func provideVisitasRepo(pool *firebird.Pool) visitasoutbound.VisitasRepo {
	return visitasfb.New(pool)
}

// provideVisitasService assembles the visitas command service.
func provideVisitasService(repo visitasoutbound.VisitasRepo, clock visitasoutbound.Clock) *visitasapp.Service {
	return visitasapp.NewService(repo, clock)
}
