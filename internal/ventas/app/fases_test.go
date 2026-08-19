//nolint:misspell // domain vocabulary is Spanish (ventas, fase) per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
	ventasoutbound "github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// fakeFaseResolver answers with a fixed map, recording every call so tests
// can assert the page is resolved in a single batch.
type fakeFaseResolver struct {
	fases   map[uuid.UUID]ventasoutbound.FaseDeVenta
	err     error
	calls   int
	lastIDs []uuid.UUID
}

func (f *fakeFaseResolver) FasesPorVenta(
	_ context.Context, ids []uuid.UUID,
) (map[uuid.UUID]ventasoutbound.FaseDeVenta, error) {
	f.calls++
	f.lastIDs = append([]uuid.UUID(nil), ids...)
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[uuid.UUID]ventasoutbound.FaseDeVenta, len(ids))
	for _, id := range ids {
		if t, ok := f.fases[id]; ok {
			out[id] = t
		}
	}
	return out, nil
}

func TestFases_ResolvesTheWholePageInOneCall(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	a := ventaConZona(t, nil)
	b := ventaConZona(t, nil)
	sinEventos := ventaConZona(t, nil)
	entroEn := time.Date(2026, 8, 10, 15, 0, 0, 0, time.UTC)
	fases := &fakeFaseResolver{fases: map[uuid.UUID]ventasoutbound.FaseDeVenta{
		a.ID(): {Desde: entroEn, Alcanzada: 2},
		b.ID(): {Desde: entroEn.Add(time.Hour), Alcanzada: 4},
	}}
	h.svc.WithFaseResolver(fases)

	out := h.svc.Fases(t.Context(), []*domain.Venta{a, b, sinEventos, nil})

	assert.Equal(t, 1, fases.calls, "the page must be resolved in a single batched call")
	assert.ElementsMatch(t, []uuid.UUID{a.ID(), b.ID(), sinEventos.ID()}, fases.lastIDs,
		"nil entries contribute no id")
	assert.Equal(t, entroEn, out[a.ID()].Desde)
	assert.Equal(t, 2, out[a.ID()].Alcanzada)
	assert.Equal(t, entroEn.Add(time.Hour), out[b.ID()].Desde)
	assert.Equal(t, 4, out[b.ID()].Alcanzada)
	assert.NotContains(t, out, sinEventos.ID(),
		"a venta with no phase event carries neither fase_desde nor fase_alcanzada")
}

func TestFases_NoResolverWired_ReturnsEmptyMap(t *testing.T) {
	t.Parallel()
	h := newHarness(t) // fase resolver deliberately not wired

	out := h.svc.Fases(t.Context(), []*domain.Venta{ventaConZona(t, nil)})

	assert.Empty(t, out)
}

func TestFases_ResolverError_ReturnsEmptyMap(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	fases := &fakeFaseResolver{err: errors.New("firebird down")}
	h.svc.WithFaseResolver(fases)

	out := h.svc.Fases(t.Context(), []*domain.Venta{ventaConZona(t, nil)})

	assert.Empty(t, out, "a lookup failure is best-effort: no dates, no error")
}

func TestFases_EmptyInput_SkipsTheResolverEntirely(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	fases := &fakeFaseResolver{}
	h.svc.WithFaseResolver(fases)

	out := h.svc.Fases(t.Context(), nil)

	assert.Empty(t, out)
	assert.Equal(t, 0, fases.calls)
}

func TestFases_OnlyNilVentas_SkipsTheResolverEntirely(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	fases := &fakeFaseResolver{}
	h.svc.WithFaseResolver(fases)

	out := h.svc.Fases(t.Context(), []*domain.Venta{nil, nil})

	assert.Empty(t, out)
	assert.Equal(t, 0, fases.calls, "no ids means no query")
}
