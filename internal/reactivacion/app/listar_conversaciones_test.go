//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/app"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

var listarConvNow = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

func TestListarConversaciones_OrderingPutsEscaladasOnTop(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()

	alDia := putConversacion(deps.convRepo, 1, domain.EstadoConversando, listarConvNow)
	_ = seedPendingDraft(deps, 1, "borrador tranquilo", listarConvNow)

	escalada := putConversacion(deps.convRepo, 2, domain.EstadoEscalado, listarConvNow)
	require.NoError(t, deps.decisionRepo.Insertar(context.Background(), mustDecision(2, domain.AccionEscalar, domain.ResultadoEscalado, "", listarConvNow)))

	buySignalNeedsAttention := putConversacion(deps.convRepo, 3, domain.EstadoConversando, listarConvNow)
	require.NoError(t, deps.decisionRepo.Insertar(context.Background(), mustDecision(3, domain.AccionEscalar, domain.ResultadoEscalado, "", listarConvNow)))

	// Seed in "al día"-first order so the sort itself has to do the work.
	deps.convRepo.listResult = []*domain.Conversacion{alDia, buySignalNeedsAttention, escalada}

	got, err := app.NewService(nil, nil, fixedClock{now: listarConvNow}, nil, app.Config{}).
		WithCopiloto(deps.convRepo, deps.decisionRepo, deps.notaReader, deps.copilotoLLM, deps.factsReader).
		ListarConversaciones(context.Background(), outbound.ListarConversacionesParams{})
	require.NoError(t, err)
	require.Len(t, got, 3)

	// escalado ESTADO is the highest priority, then a latest-decision escalate
	// (buy-signal/low-confidence/etc.), then everything else ("al día").
	assert.Equal(t, 2, got[0].Conversacion.ClienteID())
	assert.Equal(t, 3, got[1].Conversacion.ClienteID())
	assert.Equal(t, 1, got[2].Conversacion.ClienteID())
}

func TestListarConversaciones_StableWithinSamePriority(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	a := putConversacion(deps.convRepo, 10, domain.EstadoConversando, listarConvNow)
	b := putConversacion(deps.convRepo, 11, domain.EstadoConversando, listarConvNow)
	deps.convRepo.listResult = []*domain.Conversacion{a, b}

	svc := app.NewService(nil, nil, fixedClock{now: listarConvNow}, nil, app.Config{}).
		WithCopiloto(deps.convRepo, deps.decisionRepo, deps.notaReader, deps.copilotoLLM, deps.factsReader)

	got, err := svc.ListarConversaciones(context.Background(), outbound.ListarConversacionesParams{})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, 10, got[0].Conversacion.ClienteID())
	assert.Equal(t, 11, got[1].Conversacion.ClienteID())
}

func TestListarConversaciones_RepoError(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.convRepo.listarErr = errors.New("boom")
	svc := newCopilotoService(deps, listarConvNow)

	_, err := svc.ListarConversaciones(context.Background(), outbound.ListarConversacionesParams{})
	require.Error(t, err)
}

func TestListarConversaciones_DecisionRepoError(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	putConversacion(deps.convRepo, 1, domain.EstadoConversando, listarConvNow)
	deps.decisionRepo.listarErr = errors.New("boom")
	svc := newCopilotoService(deps, listarConvNow)

	_, err := svc.ListarConversaciones(context.Background(), outbound.ListarConversacionesParams{})
	require.Error(t, err)
}
