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

// fakeAnalyticsClientBenchmark satisfies AnalyticsClient with configurable benchmark behavior.
type fakeAnalyticsClientBenchmark struct {
	fakeAnalyticsClient
	benchmark    analytics.BenchmarkContract
	benchmarkErr error
}

func (f *fakeAnalyticsClientBenchmark) ObtenerBenchmark(_ context.Context, _ int, _ string) (analytics.BenchmarkContract, error) {
	return f.benchmark, f.benchmarkErr
}

func buildSvcWithBenchmark(ac outbound.AnalyticsClient) *clientesapp.Service {
	return clientesapp.NewService(
		&fakeClientesRepo{clienteByID: map[int]*domain.Cliente{}},
		ac,
		&fakeDirectoryIndex{},
		fixedClock{},
	)
}

// TestClientesService_ObtenerBenchmark_Delegates verifies the clientes Service
// is a thin pass-through to the AnalyticsClient port.
func TestClientesService_ObtenerBenchmark_Delegates(t *testing.T) {
	t.Parallel()

	expected := analytics.BenchmarkContract{
		Disponible: true,
		CohortBy:   "zona",
		Zona:       "NORTE",
		N:          42,
		Puntualidad: analytics.MetricaBenchmark{
			Aplica:    true,
			Valor:     85.5,
			Percentil: 72.0,
			N:         40,
		},
	}
	ac := &fakeAnalyticsClientBenchmark{benchmark: expected}
	svc := buildSvcWithBenchmark(ac)

	got, err := svc.ObtenerBenchmark(context.Background(), 42, "zona")
	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

// TestClientesService_ObtenerBenchmark_Degrades verifies that Disponible=false
// is returned unchanged when the analytics port signals no data.
func TestClientesService_ObtenerBenchmark_Degrades(t *testing.T) {
	t.Parallel()

	ac := &fakeAnalyticsClientBenchmark{benchmark: analytics.BenchmarkContract{Disponible: false}}
	svc := buildSvcWithBenchmark(ac)

	got, err := svc.ObtenerBenchmark(context.Background(), 99, "zona")
	require.NoError(t, err)
	assert.False(t, got.Disponible)
}

// TestClientesService_ObtenerBenchmark_PropagatesError verifies that errors
// from the analytics port bubble up unchanged.
func TestClientesService_ObtenerBenchmark_PropagatesError(t *testing.T) {
	t.Parallel()

	portErr := errors.New("analytics port failure")
	ac := &fakeAnalyticsClientBenchmark{benchmarkErr: portErr}
	svc := buildSvcWithBenchmark(ac)

	_, err := svc.ObtenerBenchmark(context.Background(), 42, "zona")
	require.Error(t, err)
	assert.ErrorIs(t, err, portErr)
}
