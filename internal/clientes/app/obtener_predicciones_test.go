//nolint:misspell // clientes vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics"
	clientesapp "github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
)

// fakeAnalyticsClientPredicciones is a test double that satisfies the full
// AnalyticsClient interface and has configurable predicciones behaviour.
type fakeAnalyticsClientPredicciones struct {
	fakeAnalyticsClient
	predicciones    analytics.PrediccionesContract
	prediccionesErr error
}

func (f *fakeAnalyticsClientPredicciones) ObtenerPredicciones(_ context.Context, _ int) (analytics.PrediccionesContract, error) {
	return f.predicciones, f.prediccionesErr
}

func buildSvcWithPredicciones(ac outbound.AnalyticsClient) *clientesapp.Service {
	return clientesapp.NewService(
		&fakeClientesRepo{clienteByID: map[int]*domain.Cliente{}},
		ac,
		&fakeDirectoryIndex{},
		fixedClock{},
	)
}

// TestClientesService_ObtenerPredicciones_Delegates verifies that the clientes
// Service is a thin pass-through to the AnalyticsClient port.
func TestClientesService_ObtenerPredicciones_Delegates(t *testing.T) {
	t.Parallel()

	expected := analytics.PrediccionesContract{
		Disponible: true,
		PAlive:     analytics.IntervaloContract{Punto: 0.80, Lo: 0.55, Hi: 0.95},
		Draws:      2000,
	}
	ac := &fakeAnalyticsClientPredicciones{predicciones: expected}
	svc := buildSvcWithPredicciones(ac)

	got, err := svc.ObtenerPredicciones(context.Background(), 42)
	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

// TestClientesService_ObtenerPredicciones_Degrades verifies that a non-found
// response from the port (Disponible=false) is returned unchanged.
func TestClientesService_ObtenerPredicciones_Degrades(t *testing.T) {
	t.Parallel()

	ac := &fakeAnalyticsClientPredicciones{predicciones: analytics.PrediccionesContract{Disponible: false}}
	svc := buildSvcWithPredicciones(ac)

	got, err := svc.ObtenerPredicciones(context.Background(), 99)
	require.NoError(t, err)
	assert.False(t, got.Disponible)
}

// TestClientesService_ObtenerPredicciones_PropagatesError verifies that errors
// from the analytics port bubble up to the caller unchanged.
func TestClientesService_ObtenerPredicciones_PropagatesError(t *testing.T) {
	t.Parallel()

	portErr := errors.New("analytics port failure")
	ac := &fakeAnalyticsClientPredicciones{prediccionesErr: portErr}
	svc := buildSvcWithPredicciones(ac)

	_, err := svc.ObtenerPredicciones(context.Background(), 42)
	require.Error(t, err)
	assert.ErrorIs(t, err, portErr)
}
