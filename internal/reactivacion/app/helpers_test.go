//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/reactivacion/app"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// copilotoDeps groups the fakes newCopilotoService wires onto a *app.Service,
// so tests can preset behavior before construction and inspect captured calls
// after exercising the service.
type copilotoDeps struct {
	convRepo     *fakeConversacionRepo
	decisionRepo *fakeDecisionRepo
	notaReader   *fakeNotaReader
	copilotoLLM  *fakeCopilotoLLM
	factsReader  *fakeClienteFactsReader
	mensajeRepo  *fakeMensajeRepo
}

// newCopilotoDeps builds a fresh set of in-memory fakes for the copiloto ports.
func newCopilotoDeps() *copilotoDeps {
	return &copilotoDeps{
		convRepo:     newFakeConversacionRepo(),
		decisionRepo: newFakeDecisionRepo(),
		notaReader:   &fakeNotaReader{},
		copilotoLLM:  &fakeCopilotoLLM{},
		factsReader:  &fakeClienteFactsReader{},
		mensajeRepo:  &fakeMensajeRepo{},
	}
}

// newCopilotoService builds a *app.Service wired with WithCopiloto (and
// WithCanal, since AprobarBorrador/EditarYAprobar reuse the Fase 2 channel)
// against deps, using a fixed clock at now. txMgr is nil — runInTx executes
// synchronously, matching every other test in this package.
func newCopilotoService(deps *copilotoDeps, now time.Time) *app.Service {
	svc := app.NewService(&fakeUniversoReader{}, &fakeCohorteRepo{}, fixedClock{now: now}, nil, app.Config{})
	svc.WithCanal(deps.mensajeRepo, &fakeSender{}, app.NewOpener(), nil, false)
	return svc.WithCopiloto(deps.convRepo, deps.decisionRepo, deps.notaReader, deps.copilotoLLM, deps.factsReader)
}

// putConversacion seeds repo with a Conversacion for clienteID, hydrated at
// estado with no memory/nota cache. Test-only convenience.
func putConversacion(repo *fakeConversacionRepo, clienteID int, estado domain.EstadoConversacion, now time.Time) *domain.Conversacion {
	conv := domain.HydrateConversacion(domain.HydrateConversacionParams{
		ID:        uuid.New().String(),
		ClienteID: clienteID,
		Estado:    estado,
		Banderas:  []string{},
		CreatedAt: now,
		UpdatedAt: now,
	})
	_ = repo.Upsert(context.Background(), conv)
	return conv
}

// mustTurno builds a domain.Turno, panicking on an invalid input (test-only).
func mustTurno(clienteID int, dir domain.DireccionTurno, autor domain.Autor, cuerpo string, now time.Time) *domain.Turno {
	t, err := domain.CrearTurno(domain.CrearTurnoParams{
		ClienteID: clienteID,
		Direccion: dir,
		Autor:     autor,
		Cuerpo:    cuerpo,
		Now:       now,
	})
	if err != nil {
		panic(err)
	}
	return t
}

// mustDecision builds a domain.Decision, panicking on an invalid input (test-only).
func mustDecision(clienteID int, accion domain.Accion, resultado domain.ResultadoDecision, borrador string, now time.Time) *domain.Decision {
	d, err := domain.CrearDecision(domain.CrearDecisionParams{
		ClienteID: clienteID,
		Accion:    accion,
		Resultado: resultado,
		Borrador:  borrador,
		Now:       now,
	})
	if err != nil {
		panic(err)
	}
	return d
}

// factsFor builds an outbound.ClienteFacts pointer inline (test-only).
func factsFor(nombre, segmento, telefono string) *outbound.ClienteFacts {
	return &outbound.ClienteFacts{Nombre: nombre, Segmento: segmento, Telefono: telefono}
}

// mustSeg parses a segmento string, panicking on an invalid value (test-only).
func mustSeg(raw string) domain.Segmento {
	s, err := domain.ParseSegmento(raw)
	if err != nil {
		panic(err)
	}
	return s
}

// cohorteRow builds a persisted-style CohorteCliente for query/attribution tests.
// ultimaCompra is the (post-cohorte) last purchase used to decide conversion.
func cohorteRow(clienteID int, enControl, contactado bool, cohorteFecha, ultimaCompra time.Time) *domain.CohorteCliente {
	return domain.HydrateCohorteCliente(domain.HydrateCohorteClienteParams{
		ID:                    uuid.New(),
		ClienteID:             clienteID,
		Nombre:                "Cliente Cohorte",
		Telefono:              "238 333 4444",
		Segmento:              domain.SegmentoRecienLiquidado,
		EnControl:             enControl,
		FueContactado:         contactado,
		CohorteFecha:          cohorteFecha,
		FechaUltimaCompraBase: ultimaCompra,
		Saldo:                 decimal.Zero,
		PorLiquidarPct:        decimal.Zero,
		CreatedAt:             cohorteFecha,
		UpdatedAt:             cohorteFecha,
	})
}
