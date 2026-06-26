//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clientesapp "github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// ─── ObtenerTimeline: cliente no existe ───────────────────────────────────────

func TestObtenerTimeline_ClienteNoExiste_NotFound(t *testing.T) {
	t.Parallel()

	repo := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{},
	}
	svc := clientesapp.NewService(repo, &fakeAnalyticsClient{}, &fakeDirectoryIndex{}, fixedClock{T: fixedTime})

	_, err := svc.ObtenerTimeline(context.Background(), 9999)
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected apperror.Error")
	assert.Equal(t, apperror.KindNotFound, appErr.Kind)
}

// ─── ObtenerTimeline: bundle vacío → feed vacío ───────────────────────────────

func TestObtenerTimeline_BundleVacio_FeedVacio(t *testing.T) {
	t.Parallel()

	base := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{42: newCliente(42, "Test")},
	}
	repo := &timelineFakeRepo{
		fakeClientesRepo: base,
		data:             outbound.RitmoPagoData{},
	}
	svc := clientesapp.NewService(repo, &fakeAnalyticsClient{}, &fakeDirectoryIndex{}, fixedClock{T: fixedTime})

	eventos, err := svc.ObtenerTimeline(context.Background(), 42)
	require.NoError(t, err)
	assert.NotNil(t, eventos, "must be a non-nil slice, not nil")
	assert.Empty(t, eventos)
}

// ─── ObtenerTimeline: mezcla y orden ──────────────────────────────────────────

func TestObtenerTimeline_MezclaPagosVentas_OrdenadoDescendente(t *testing.T) {
	t.Parallel()

	tReciente := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	tMedio := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
	tAntiguo := time.Date(2025, 6, 10, 0, 0, 0, 0, time.UTC)

	pagos := []domain.PagoCrudo{
		{Fecha: tAntiguo, Importe: decimal.NewFromInt(500), DoctoCCID: 10, Concepto: "cobranza"},
		{Fecha: tReciente, Importe: decimal.NewFromInt(800), DoctoCCID: 30, Concepto: "enganche"},
	}
	ventas := []domain.VentaCruda{
		{Fecha: tMedio, Total: decimal.NewFromInt(12000), DoctoPvID: 200, Folio: "PV-200", EsCredito: true},
	}

	base := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{42: newCliente(42, "Test")},
	}
	repo := &timelineFakeRepo{
		fakeClientesRepo: base,
		data: outbound.RitmoPagoData{
			Pagos:  pagos,
			Ventas: ventas,
		},
	}
	svc := clientesapp.NewService(repo, &fakeAnalyticsClient{}, &fakeDirectoryIndex{}, fixedClock{T: fixedTime})

	eventos, err := svc.ObtenerTimeline(context.Background(), 42)
	require.NoError(t, err)
	require.Len(t, eventos, 3, "must contain all pagos + ventas")

	// Verify descending order.
	for i := range len(eventos) - 1 {
		assert.False(t, eventos[i].Fecha.Before(eventos[i+1].Fecha),
			"event[%d] must be >= event[%d]", i, i+1)
	}

	assert.Equal(t, tReciente, eventos[0].Fecha, "most recent first")
	assert.Equal(t, tMedio, eventos[1].Fecha)
	assert.Equal(t, tAntiguo, eventos[2].Fecha)
}

// ─── ObtenerTimeline: tipos correctos ─────────────────────────────────────────

func TestObtenerTimeline_TiposCorrectos(t *testing.T) {
	t.Parallel()

	ts := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)

	base := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{1: newCliente(1, "Test")},
	}
	repo := &timelineFakeRepo{
		fakeClientesRepo: base,
		data: outbound.RitmoPagoData{
			Pagos: []domain.PagoCrudo{
				{Fecha: ts, Importe: decimal.NewFromInt(1000), DoctoCCID: 1, Concepto: "pago"},
			},
			Ventas: []domain.VentaCruda{
				{Fecha: ts.AddDate(0, 0, -1), Total: decimal.NewFromInt(5000), DoctoPvID: 10, EsCredito: true},
				{Fecha: ts.AddDate(0, 0, -2), Total: decimal.NewFromInt(2000), DoctoPvID: 20, EsCredito: false},
			},
		},
	}
	svc := clientesapp.NewService(repo, &fakeAnalyticsClient{}, &fakeDirectoryIndex{}, fixedClock{T: fixedTime})

	eventos, err := svc.ObtenerTimeline(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, eventos, 3)

	tiposByRefID := make(map[int]string)
	for _, e := range eventos {
		tiposByRefID[e.RefID] = e.Tipo
	}
	assert.Equal(t, domain.TipoPago, tiposByRefID[1])
	assert.Equal(t, domain.TipoCompraCredito, tiposByRefID[10])
	assert.Equal(t, domain.TipoCompraContado, tiposByRefID[20])
}

// ─── ObtenerTimeline: error del repo ─────────────────────────────────────────

func TestObtenerTimeline_RepoError_ErrorEnvuelto(t *testing.T) {
	t.Parallel()

	base := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{42: newCliente(42, "Test")},
	}
	repo := &timelineFakeRepo{
		fakeClientesRepo: base,
		dataErr:          errors.New("firebird connection refused"),
	}
	svc := clientesapp.NewService(repo, &fakeAnalyticsClient{}, &fakeDirectoryIndex{}, fixedClock{T: fixedTime})

	_, err := svc.ObtenerTimeline(context.Background(), 42)
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected apperror.Error")
	assert.Equal(t, apperror.KindInternal, appErr.Kind)
}

// ─── ObtenerTimeline: rango enviado al repo (sin bounds) ─────────────────────

func TestObtenerTimeline_RangoSinBounds(t *testing.T) {
	t.Parallel()

	base := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{42: newCliente(42, "Test")},
	}
	capturado := &capturingTimelineRepo{fakeClientesRepo: base}
	svc := clientesapp.NewService(capturado, &fakeAnalyticsClient{}, &fakeDirectoryIndex{}, fixedClock{T: fixedTime})

	_, err := svc.ObtenerTimeline(context.Background(), 42)
	require.NoError(t, err)

	assert.Nil(t, capturado.capturedRango.Desde, "Desde must be nil (no lower bound)")
	assert.Nil(t, capturado.capturedRango.Hasta, "Hasta must be nil (no upper bound)")
}

// ─── ObtenerTimeline: apperror del repo de datos ─────────────────────────────

func TestObtenerTimeline_RepoApperror_ErrorEnvuelto(t *testing.T) {
	t.Parallel()

	repoErr := apperror.NewInternal("ritmo_pago_failed", "error al obtener datos")
	base := &fakeClientesRepo{
		clienteByID: map[int]*domain.Cliente{42: newCliente(42, "Test")},
	}
	repo := &timelineFakeRepo{
		fakeClientesRepo: base,
		dataErr:          repoErr,
	}
	svc := clientesapp.NewService(repo, &fakeAnalyticsClient{}, &fakeDirectoryIndex{}, fixedClock{T: fixedTime})

	_, err := svc.ObtenerTimeline(context.Background(), 42)
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected apperror.Error")
	assert.Equal(t, apperror.KindInternal, appErr.Kind)
}

// ─── Test-local repo wrappers ─────────────────────────────────────────────────

// timelineFakeRepo embeds fakeClientesRepo and overrides ObtenerRitmoPagoData
// to return preset data.
type timelineFakeRepo struct {
	*fakeClientesRepo
	data    outbound.RitmoPagoData
	dataErr error
}

func (f *timelineFakeRepo) ObtenerRitmoPagoData(_ context.Context, _ int, _ outbound.RangoFechas) (outbound.RitmoPagoData, error) {
	if f.dataErr != nil {
		return outbound.RitmoPagoData{}, f.dataErr
	}
	return f.data, nil
}

// capturingTimelineRepo records the rango passed to ObtenerRitmoPagoData.
type capturingTimelineRepo struct {
	*fakeClientesRepo
	capturedRango outbound.RangoFechas
}

func (f *capturingTimelineRepo) ObtenerRitmoPagoData(_ context.Context, _ int, rango outbound.RangoFechas) (outbound.RitmoPagoData, error) {
	f.capturedRango = rango
	return outbound.RitmoPagoData{}, nil
}
