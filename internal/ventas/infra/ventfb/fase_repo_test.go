//nolint:misspell // Spanish vocabulary (venta, fase) by convention.
package ventfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
)

// TestFaseRepo_EmptyInput_DoesNotQuery pins the short-circuit: an empty
// id list returns an empty map WITHOUT touching the database. The repo is
// built over a nil pool on purpose — any attempt to query would panic, so a
// passing test is proof no query was issued.
func TestFaseRepo_EmptyInput_DoesNotQuery(t *testing.T) {
	t.Parallel()

	repo := ventfb.NewFaseRepo(nil)
	out, err := repo.FasesPorVenta(t.Context(), nil)

	require.NoError(t, err)
	assert.Empty(t, out)
}

// TestFaseRepo_FasesPorVenta_ReadsNewestPhaseEvent seeds a venta's
// outbox timeline and verifies the repo returns the newest PHASE event, not
// the newest event overall.
func TestFaseRepo_FasesPorVenta_ReadsNewestPhaseEvent(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewFaseRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		ventaID := uuid.New()
		base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
		insertOutboxRow(ctx, t, pool, "venta", ventaID,
			domain.EventTypeVentaCreada, `{"tipo_venta":"CREDITO"}`, base)
		insertOutboxRow(ctx, t, pool, "venta", ventaID,
			domain.EventTypeVentaEnviadaARevision, `{}`, base.Add(24*time.Hour))
		// An edit made AFTER the phase change must not win.
		insertOutboxRow(ctx, t, pool, "venta", ventaID,
			domain.EventTypeVentaHeaderActualizado, `{}`, base.Add(72*time.Hour))

		out, err := repo.FasesPorVenta(ctx, []uuid.UUID{ventaID})
		require.NoError(t, err)
		got, ok := out[ventaID]
		require.True(t, ok, "the venta has phase events, it must appear in the map")
		assert.True(t, got.Desde.Equal(base.Add(24*time.Hour)),
			"expected %s, got %s", base.Add(24*time.Hour), got.Desde)
		assert.Equal(t, time.UTC, got.Desde.Location(), "timestamps cross the port in UTC")
		assert.Equal(t, 2, got.Alcanzada,
			"creada + enviada_a_revision means the venta reached fase 2")
	})
}

// TestFaseRepo_FasesPorVenta_DedupsAndOmitsMisses verifies duplicate
// ids collapse into one placeholder and that a venta with no outbox row (or
// with only edit rows) is simply absent from the map.
func TestFaseRepo_FasesPorVenta_DedupsAndOmitsMisses(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewFaseRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		conFase := uuid.New()
		soloEdiciones := uuid.New()
		inexistente := uuid.New()
		base := time.Date(2026, 8, 2, 8, 30, 0, 0, time.UTC)

		insertOutboxRow(ctx, t, pool, "venta", conFase,
			domain.EventTypeVentaAprobada, `{}`, base)
		insertOutboxRow(ctx, t, pool, "venta", soloEdiciones,
			domain.EventTypeVentaProductosReemplazados, `{}`, base)

		out, err := repo.FasesPorVenta(ctx,
			[]uuid.UUID{conFase, conFase, soloEdiciones, inexistente})
		require.NoError(t, err)

		require.Contains(t, out, conFase)
		assert.True(t, out[conFase].Desde.Equal(base))
		assert.Equal(t, 3, out[conFase].Alcanzada)
		assert.NotContains(t, out, soloEdiciones,
			"a venta with only edit events has no fase at all")
		assert.NotContains(t, out, inexistente,
			"a venta with no outbox rows is omitted, never invented")
		assert.Len(t, out, 1)
	})
}

// TestFaseRepo_FasesPorVenta_RegresadaABorradorRestartsTheClock
// exercises the backwards transition end-to-end against Firebird.
func TestFaseRepo_FasesPorVenta_RegresadaABorradorRestartsTheClock(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewFaseRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		ventaID := uuid.New()
		base := time.Date(2026, 8, 3, 7, 15, 0, 0, time.UTC)
		insertOutboxRow(ctx, t, pool, "venta", ventaID,
			domain.EventTypeVentaAprobada, `{}`, base)
		insertOutboxRow(ctx, t, pool, "venta", ventaID,
			domain.EventTypeVentaRegresadaABorrador, `{}`, base.Add(48*time.Hour))

		out, err := repo.FasesPorVenta(ctx, []uuid.UUID{ventaID})
		require.NoError(t, err)
		assert.True(t, out[ventaID].Desde.Equal(base.Add(48*time.Hour)),
			"regresada_a_borrador must restart the fase clock")
		assert.Equal(t, 3, out[ventaID].Alcanzada,
			"…but it must NOT lower the highest fase the venta reached")
	})
}

// TestFaseRepo_FasesPorVenta_CanceladaKeepsTheFaseReached exercises the
// defect end-to-end against Firebird: a venta cancelled while in revisada
// keeps reporting fase 2, even though its situacion no longer says so.
func TestFaseRepo_FasesPorVenta_CanceladaKeepsTheFaseReached(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewFaseRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		ventaID := uuid.New()
		base := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
		insertOutboxRow(ctx, t, pool, "venta", ventaID,
			domain.EventTypeVentaCreada, `{"tipo_venta":"CREDITO"}`, base)
		insertOutboxRow(ctx, t, pool, "venta", ventaID,
			domain.EventTypeVentaEnviadaARevision, `{}`, base.Add(24*time.Hour))
		insertOutboxRow(ctx, t, pool, "venta", ventaID,
			domain.EventTypeVentaCancelada, `{}`, base.Add(48*time.Hour))

		out, err := repo.FasesPorVenta(ctx, []uuid.UUID{ventaID})
		require.NoError(t, err)
		require.Contains(t, out, ventaID)
		assert.True(t, out[ventaID].Desde.Equal(base.Add(48*time.Hour)),
			"the cancelación is the newest phase event")
		assert.Equal(t, 2, out[ventaID].Alcanzada,
			"el aspa no borra el avance: the venta reached revisada")
	})
}
