//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/app"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

func TestObtenerConversacion_ConvRepoGetError_Fails(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	deps.convRepo.getErr = assert.AnError
	svc := newCopilotoService(deps, obtenerConvNow)

	_, err := svc.ObtenerConversacion(context.Background(), obtenerConvClienteID)
	require.Error(t, err)
}

func TestObtenerConversacion_ListarTurnosError_Fails(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	putConversacion(deps.convRepo, obtenerConvClienteID, domain.EstadoConversando, obtenerConvNow)
	deps.convRepo.listarTurnosErr = assert.AnError
	svc := newCopilotoService(deps, obtenerConvNow)

	_, err := svc.ObtenerConversacion(context.Background(), obtenerConvClienteID)
	require.Error(t, err)
}

func TestObtenerConversacion_DecisionListError_Fails(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	putConversacion(deps.convRepo, obtenerConvClienteID, domain.EstadoConversando, obtenerConvNow)
	deps.decisionRepo.listarErr = assert.AnError
	svc := newCopilotoService(deps, obtenerConvNow)

	_, err := svc.ObtenerConversacion(context.Background(), obtenerConvClienteID)
	require.Error(t, err)
}

var obtenerConvNow = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

const obtenerConvClienteID = 24037

func TestObtenerConversacion_ReturnsThreadAndDecisiones(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	putConversacion(deps.convRepo, obtenerConvClienteID, domain.EstadoConversando, obtenerConvNow)
	turno := mustTurno(obtenerConvClienteID, domain.DireccionEntrante, domain.AutorCliente, "hola", obtenerConvNow)
	require.NoError(t, deps.convRepo.AppendTurno(context.Background(), turno))
	seedPendingDraft(deps, obtenerConvClienteID, "un borrador", obtenerConvNow)
	svc := newCopilotoService(deps, obtenerConvNow)

	detalle, err := svc.ObtenerConversacion(context.Background(), obtenerConvClienteID)
	require.NoError(t, err)
	require.NotNil(t, detalle.Conversacion)
	assert.Equal(t, obtenerConvClienteID, detalle.Conversacion.ClienteID())
	require.Len(t, detalle.Turnos, 1)
	assert.Equal(t, "hola", detalle.Turnos[0].Cuerpo())
	require.Len(t, detalle.Decisiones, 1)
	assert.Equal(t, "un borrador", detalle.Decisiones[0].Borrador())
}

func TestObtenerConversacion_Absent_Error(t *testing.T) {
	t.Parallel()
	deps := newCopilotoDeps()
	svc := newCopilotoService(deps, obtenerConvNow)

	_, err := svc.ObtenerConversacion(context.Background(), obtenerConvClienteID)
	require.ErrorIs(t, err, app.ErrConversacionNoEncontrada)
}
