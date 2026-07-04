//nolint:misspell // domain vocabulary is Spanish (ventas) per project convention.
package ventoutbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
	"github.com/abdimuy/msp-api/internal/platform/outboxfb"
	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventoutbox"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// ─── Minimal fakes ──────────────────────────────────────────────────────────

// stubVentaRepo implements outbound.VentaRepo with only FindByID behaving;
// every other method is an unused stub — the handler under test never
// calls them.
type stubVentaRepo struct {
	byID map[uuid.UUID]*domain.Venta
	err  error
}

func newStubVentaRepo() *stubVentaRepo {
	return &stubVentaRepo{byID: map[uuid.UUID]*domain.Venta{}}
}

func (s *stubVentaRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.Venta, error) {
	if s.err != nil {
		return nil, s.err
	}
	v, ok := s.byID[id]
	if !ok {
		return nil, domain.ErrVentaNotFound
	}
	return v, nil
}

func (s *stubVentaRepo) Save(_ context.Context, _ *domain.Venta) error              { return nil }
func (s *stubVentaRepo) Update(_ context.Context, _ *domain.Venta) error            { return nil }
func (s *stubVentaRepo) UpdateHeader(_ context.Context, _ *domain.Venta) error      { return nil }
func (s *stubVentaRepo) UpdateCliente(_ context.Context, _ *domain.Venta) error     { return nil }
func (s *stubVentaRepo) ReplaceProductos(_ context.Context, _ *domain.Venta) error  { return nil }
func (s *stubVentaRepo) ReplaceCombos(_ context.Context, _ *domain.Venta) error     { return nil }
func (s *stubVentaRepo) ReplaceVendedores(_ context.Context, _ *domain.Venta) error { return nil }
func (s *stubVentaRepo) LockByID(_ context.Context, _ uuid.UUID) error              { return nil }
func (s *stubVentaRepo) InsertImagen(_ context.Context, _ uuid.UUID, _ *domain.Imagen) error {
	return nil
}
func (s *stubVentaRepo) DeleteImagen(_ context.Context, _, _ uuid.UUID) error { return nil }

func (s *stubVentaRepo) FindByIDs(_ context.Context, ids []uuid.UUID) ([]*domain.Venta, error) {
	out := make([]*domain.Venta, 0, len(ids))
	for _, id := range ids {
		if v, ok := s.byID[id]; ok {
			out = append(out, v)
		}
	}
	return out, nil
}

func (s *stubVentaRepo) List(_ context.Context, _ outbound.ListParams, _ outbound.ListVentasFilters) (outbound.Page[*domain.Venta], error) {
	return outbound.Page[*domain.Venta]{}, nil
}

// stubSearchIndex implements outbound.VentaSearchIndex, recording every call.
type stubSearchIndex struct {
	indexarErr error

	indexarCalls []outbound.VentaSearchDoc
	eliminarIDs  []uuid.UUID
}

func (s *stubSearchIndex) Buscar(_ context.Context, _ outbound.VentasSearchQuery) (outbound.VentasSearchResultado, error) {
	return outbound.VentasSearchResultado{}, nil
}

func (s *stubSearchIndex) Reconciliar(_ context.Context, _ []outbound.VentaSearchDoc) error {
	return nil
}

func (s *stubSearchIndex) IndexarUno(_ context.Context, doc outbound.VentaSearchDoc) error {
	s.indexarCalls = append(s.indexarCalls, doc)
	return s.indexarErr
}

func (s *stubSearchIndex) Eliminar(_ context.Context, id uuid.UUID) error {
	s.eliminarIDs = append(s.eliminarIDs, id)
	return nil
}

// ─── Fixtures ───────────────────────────────────────────────────────────────

func newTestVenta(t *testing.T) *domain.Venta {
	t.Helper()
	nombre, err := domain.NewNombreCliente("Juan Perez")
	require.NoError(t, err)
	cliente := domain.HydrateClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: nombre})
	dir := domain.HydrateDireccion(domain.NewDireccionParams{
		Calle: "Av. Reforma", Colonia: "Centro", Poblacion: "Cuauhtemoc", Ciudad: "CDMX",
	})
	montos := domain.HydrateMontoSnapshot(
		decimal.NewFromInt(1000), decimal.NewFromInt(900), decimal.NewFromInt(800),
	)
	now := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	return domain.HydrateVenta(domain.HydrateVentaParams{
		ID:             uuid.New(),
		Cliente:        cliente,
		Direccion:      dir,
		FechaVenta:     now,
		TipoVenta:      domain.TipoVentaContado,
		Montos:         montos,
		Estado:         domain.EstadoActive,
		Situacion:      domain.SituacionBorrador,
		Sincronizacion: domain.SincronizacionPendiente,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
}

func newTestService(t *testing.T, repo outbound.VentaRepo, idx outbound.VentaSearchIndex) *ventasapp.Service {
	t.Helper()
	return ventasapp.NewService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).WithSearchIndex(idx)
}

// ─── Registration ───────────────────────────────────────────────────────────

func TestNewVentaReindexHandlers_OneHandlerPerEventType_NoDuplicatePanic(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, newStubVentaRepo(), &stubSearchIndex{})
	handlers := ventoutbox.NewVentaReindexHandlers(svc)

	require.Len(t, handlers, 13, "plan+ref-notes: 13 venta event types")

	reg := outboxfb.NewHandlerRegistry()
	seen := map[string]struct{}{}
	for _, h := range handlers {
		_, dup := seen[h.EventType()]
		require.False(t, dup, "duplicate EventType %q", h.EventType())
		seen[h.EventType()] = struct{}{}
		require.NotPanics(t, func() { reg.Register(h) })
	}

	assert.ElementsMatch(t, []string{
		domain.EventTypeVentaCreada,
		domain.EventTypeVentaCancelada,
		domain.EventTypeImagenAdjuntada,
		domain.EventTypeImagenEliminada,
		domain.EventTypeVentaHeaderActualizado,
		domain.EventTypeVentaClienteActualizado,
		domain.EventTypeVentaProductosReemplazados,
		domain.EventTypeVentaCombosReemplazados,
		domain.EventTypeVentaVendedoresReemplazados,
		domain.EventTypeVentaEnviadaARevision,
		domain.EventTypeVentaAprobada,
		domain.EventTypeVentaRegresadaABorrador,
		domain.EventTypeVentaAplicada,
	}, reg.KnownTypes())
}

// ─── Handle ──────────────────────────────────────────────────────────────

func TestReindexHandler_Handle(t *testing.T) {
	t.Parallel()

	t.Run("existing_venta_indexes_mapped_doc", func(t *testing.T) {
		t.Parallel()
		repo := newStubVentaRepo()
		v := newTestVenta(t)
		repo.byID[v.ID()] = v
		idx := &stubSearchIndex{}
		svc := newTestService(t, repo, idx)
		handlers := ventoutbox.NewVentaReindexHandlers(svc)

		h := findHandler(t, handlers, domain.EventTypeVentaCreada)
		err := h.Handle(t.Context(), outboxfb.Event{
			ID: uuid.New(), Aggregate: "venta", AggregateID: v.ID(),
			EventType: domain.EventTypeVentaCreada, Payload: json.RawMessage("{}"),
		})
		require.NoError(t, err)

		require.Len(t, idx.indexarCalls, 1)
		assert.Equal(t, v.ID(), idx.indexarCalls[0].ID)
		assert.Equal(t, "JUAN PEREZ", idx.indexarCalls[0].NombreCliente)
		assert.Empty(t, idx.eliminarIDs)
	})

	t.Run("not_found_venta_purges_via_eliminar", func(t *testing.T) {
		t.Parallel()
		repo := newStubVentaRepo() // empty: every id misses
		idx := &stubSearchIndex{}
		svc := newTestService(t, repo, idx)
		handlers := ventoutbox.NewVentaReindexHandlers(svc)

		missingID := uuid.New()
		h := findHandler(t, handlers, domain.EventTypeVentaCancelada)
		err := h.Handle(t.Context(), outboxfb.Event{
			ID: uuid.New(), Aggregate: "venta", AggregateID: missingID,
			EventType: domain.EventTypeVentaCancelada, Payload: json.RawMessage("{}"),
		})
		require.NoError(t, err)

		assert.Empty(t, idx.indexarCalls)
		require.Len(t, idx.eliminarIDs, 1)
		assert.Equal(t, missingID, idx.eliminarIDs[0])
	})

	t.Run("transient_index_error_maps_to_outboxfb_ErrTransient", func(t *testing.T) {
		t.Parallel()
		repo := newStubVentaRepo()
		v := newTestVenta(t)
		repo.byID[v.ID()] = v
		idx := &stubSearchIndex{indexarErr: fakeTransientMeiliErr()}
		svc := newTestService(t, repo, idx)
		handlers := ventoutbox.NewVentaReindexHandlers(svc)

		h := findHandler(t, handlers, domain.EventTypeVentaAplicada)
		err := h.Handle(t.Context(), outboxfb.Event{
			ID: uuid.New(), Aggregate: "venta", AggregateID: v.ID(),
			EventType: domain.EventTypeVentaAplicada, Payload: json.RawMessage("{}"),
		})
		require.ErrorIs(t, err, outboxfb.ErrTransient)
	})

	t.Run("permanent_index_error_propagates_unmapped", func(t *testing.T) {
		t.Parallel()
		repo := newStubVentaRepo()
		v := newTestVenta(t)
		repo.byID[v.ID()] = v
		boom := errors.New("boom")
		idx := &stubSearchIndex{indexarErr: boom}
		svc := newTestService(t, repo, idx)
		handlers := ventoutbox.NewVentaReindexHandlers(svc)

		h := findHandler(t, handlers, domain.EventTypeVentaAprobada)
		err := h.Handle(t.Context(), outboxfb.Event{
			ID: uuid.New(), Aggregate: "venta", AggregateID: v.ID(),
			EventType: domain.EventTypeVentaAprobada, Payload: json.RawMessage("{}"),
		})
		require.ErrorIs(t, err, boom)
		require.NotErrorIs(t, err, outboxfb.ErrTransient)
	})
}

// ─── Helpers ─────────────────────────────────────────────────────────────

func findHandler(t *testing.T, handlers []outboxfb.Handler, eventType string) outboxfb.Handler {
	t.Helper()
	for _, h := range handlers {
		if h.EventType() == eventType {
			return h
		}
	}
	t.Fatalf("no handler registered for event type %q", eventType)
	return nil
}

// fakeTransientMeiliErr returns an error satisfying
// errors.Is(err, platformmeili.ErrMeilisearchTransient), mirroring how the
// real ventsearch adapter wraps the platform client's transientError with
// %w.
func fakeTransientMeiliErr() error {
	return &transientWrapError{cause: platformmeili.ErrMeilisearchTransient}
}

type transientWrapError struct{ cause error }

func (w *transientWrapError) Error() string { return "ventsearch: wrapped: " + w.cause.Error() }
func (w *transientWrapError) Unwrap() error { return w.cause }
