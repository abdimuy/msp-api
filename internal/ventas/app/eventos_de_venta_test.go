//nolint:misspell // domain vocabulary is Spanish (ventas, eventos) per project convention.
package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// fakeEventReader returns a canned timeline (actor fields left empty, mirroring
// the real reader which leaves resolution to the service).
type fakeEventReader struct {
	eventos []outbound.VentaEvento
	err     error
}

func (f *fakeEventReader) EventosDeVenta(_ context.Context, _ uuid.UUID) ([]outbound.VentaEvento, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.eventos, nil
}

// fakeUsuarioResolver maps a fixed set of ids to names.
type fakeUsuarioResolver struct {
	nombres map[uuid.UUID]string
	err     error
	calls   int
}

func (f *fakeUsuarioResolver) NombresPorID(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[uuid.UUID]string, len(ids))
	for _, id := range ids {
		if n, ok := f.nombres[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}

func eventoConPayload(eventType, payload string, at time.Time) outbound.VentaEvento {
	return outbound.VentaEvento{
		ID:         uuid.New(),
		EventType:  eventType,
		Payload:    json.RawMessage(payload),
		OccurredAt: at,
	}
}

func TestEventosDeVenta(t *testing.T) {
	t.Parallel()

	t.Run("not_found_propagates_before_reading_events", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.svc.WithEventReader(&fakeEventReader{})

		_, err := h.svc.EventosDeVenta(t.Context(), uuid.New())
		require.ErrorIs(t, err, domain.ErrVentaNotFound)
	})

	t.Run("nil_reader_returns_empty_without_error", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)

		eventos, err := h.svc.EventosDeVenta(t.Context(), *ventaID)
		require.NoError(t, err)
		assert.Empty(t, eventos)
	})

	t.Run("resolves_actor_names_from_varied_by_fields", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)

		creador := uuid.New()
		aprobador := uuid.New()
		aplicador := uuid.New()
		base := time.Date(2026, 6, 9, 1, 0, 0, 0, time.UTC)

		reader := &fakeEventReader{eventos: []outbound.VentaEvento{
			eventoConPayload("venta.creada", `{"created_by":"`+creador.String()+`","tipo_venta":"CREDITO"}`, base),
			eventoConPayload("venta.imagen_adjuntada", `{"imagen_id":"`+uuid.New().String()+`","size_bytes":1000}`, base.Add(time.Minute)),
			eventoConPayload("venta.aprobada", `{"updated_by":"`+aprobador.String()+`"}`, base.Add(2*time.Minute)),
			eventoConPayload("venta.aplicada", `{"applied_by":"`+aplicador.String()+`","microsip_folio":"Y1"}`, base.Add(3*time.Minute)),
		}}
		resolver := &fakeUsuarioResolver{nombres: map[uuid.UUID]string{
			creador:   "Ana Creadora",
			aprobador: "Beto Aprobador",
			aplicador: "Caro Aplicadora",
		}}
		h.svc.WithEventReader(reader).WithUsuarioResolver(resolver)

		eventos, err := h.svc.EventosDeVenta(t.Context(), *ventaID)
		require.NoError(t, err)
		require.Len(t, eventos, 4)

		// venta.creada → created_by resolved.
		require.NotNil(t, eventos[0].ActorID)
		assert.Equal(t, creador, *eventos[0].ActorID)
		assert.Equal(t, "Ana Creadora", eventos[0].ActorNombre)

		// imagen_adjuntada carries no actor.
		assert.Nil(t, eventos[1].ActorID)
		assert.Empty(t, eventos[1].ActorNombre)

		// aprobada → updated_by, aplicada → applied_by.
		assert.Equal(t, "Beto Aprobador", eventos[2].ActorNombre)
		assert.Equal(t, "Caro Aplicadora", eventos[3].ActorNombre)

		// Resolver was called exactly once (batched).
		assert.Equal(t, 1, resolver.calls)
	})

	t.Run("unresolved_actor_keeps_id_but_empty_name", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)

		actor := uuid.New()
		reader := &fakeEventReader{eventos: []outbound.VentaEvento{
			eventoConPayload("venta.aprobada", `{"updated_by":"`+actor.String()+`"}`, time.Now().UTC()),
		}}
		// Resolver returns no name for this id.
		resolver := &fakeUsuarioResolver{nombres: map[uuid.UUID]string{}}
		h.svc.WithEventReader(reader).WithUsuarioResolver(resolver)

		eventos, err := h.svc.EventosDeVenta(t.Context(), *ventaID)
		require.NoError(t, err)
		require.Len(t, eventos, 1)
		require.NotNil(t, eventos[0].ActorID)
		assert.Equal(t, actor, *eventos[0].ActorID)
		assert.Empty(t, eventos[0].ActorNombre)
	})

	t.Run("resolver_error_degrades_to_ids_without_names", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)

		actor := uuid.New()
		reader := &fakeEventReader{eventos: []outbound.VentaEvento{
			eventoConPayload("venta.aprobada", `{"updated_by":"`+actor.String()+`"}`, time.Now().UTC()),
		}}
		resolver := &fakeUsuarioResolver{err: errors.New("usuarios down")}
		h.svc.WithEventReader(reader).WithUsuarioResolver(resolver)

		// Best-effort: the read succeeds, actor id present, name empty.
		eventos, err := h.svc.EventosDeVenta(t.Context(), *ventaID)
		require.NoError(t, err)
		require.Len(t, eventos, 1)
		require.NotNil(t, eventos[0].ActorID)
		assert.Empty(t, eventos[0].ActorNombre)
	})

	t.Run("reader_error_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		ventaID := h.seedVenta(t)
		boom := errors.New("outbox read failed")
		h.svc.WithEventReader(&fakeEventReader{err: boom})

		_, err := h.svc.EventosDeVenta(t.Context(), *ventaID)
		require.ErrorIs(t, err, boom)
	})
}
