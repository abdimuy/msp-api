//nolint:misspell // ventas vocabulary is Spanish per project convention.
package app_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
)

// TestConcurrency_PATCHHeader_LastWriteWins documents the deliberate
// no-optimistic-locking semantics of the venta edit operations.
//
// Pattern under test: two writers (A and B) each capture a snapshot of
// the venta, then both commit edits derived from their snapshots. With
// optimistic locking, the second commit would observe its snapshot is
// stale and fail with a conflict error. Without it, both succeed and
// the second write silently overwrites the first ("last-write-wins").
//
// This is the current intentional design. The UPDATE statements in
// internal/ventas/infra/ventfb/queries.go (updateVentaHeaderFull,
// updateVentaCliente, etc.) do NOT include a `WHERE updated_at = ?`
// guard or a VERSION column. The trade-off is favoured because:
//
//  1. Edit operations are admin-initiated from a single console UI;
//     true concurrent edits to the same venta are rare in practice.
//  2. Adding version columns to MSP_VENTAS would require a migration
//     plus updates to every UPDATE site across header/cliente/
//     productos/combos/vendedores.
//
// **Architectural follow-up**: when the API gains a multi-tab admin UI
// or any background worker that can mutate ventas while a human edits,
// reconsider this trade-off and add optimistic locking via a VERSION
// column or compare-and-swap on UPDATED_AT. The change site list is
// stable (queries.go update statements). This test must be updated when
// that happens — its existence is a tripwire for silent regression.
//
// Note: the test runs sequentially rather than as goroutines because the
// in-memory fake repository returns a live pointer to the aggregate
// (it does not deep-copy on FindByID), so true concurrent writers would
// race on the same memory. The sequential lost-update pattern below
// proves the same lack-of-conflict-detection without that artifact.
func TestConcurrency_PATCHHeader_LastWriteWins(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	id := *h.seedVenta(t)
	ctx := t.Context()
	actorA := uuid.New()
	actorB := uuid.New()

	// Writer A and Writer B independently read the venta (their reads
	// produce equivalent snapshots since neither has written yet).
	snapA, err := h.svc.ObtenerVenta(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, snapA)

	snapB, err := h.svc.ObtenerVenta(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, snapB)

	// Writer A commits its edit (contado=1111).
	_, err = h.svc.ActualizarHeader(ctx, ventasapp.ActualizarHeaderInput{
		VentaID:       id,
		Calle:         "Av. A",
		Colonia:       "Centro",
		Poblacion:     "Ciudad",
		Ciudad:        "Ciudad",
		Latitud:       19.0,
		Longitud:      -99.0,
		FechaVenta:    time.Now().UTC(),
		PrecioAnual:   decimal.NewFromInt(2111),
		PrecioCorto:   decimal.NewFromInt(1611),
		PrecioContado: decimal.NewFromInt(1111),
	}, actorA)
	require.NoError(t, err, "writer A should commit without optimistic-lock conflict")

	// Writer B commits its edit based on the stale snapshot it read
	// before A wrote (contado=2222). In a system with optimistic
	// locking, this call would fail because B's snapshot version no
	// longer matches the DB. With last-write-wins it succeeds and the
	// final state is B's.
	_, err = h.svc.ActualizarHeader(ctx, ventasapp.ActualizarHeaderInput{
		VentaID:       id,
		Calle:         "Av. B",
		Colonia:       "Centro",
		Poblacion:     "Ciudad",
		Ciudad:        "Ciudad",
		Latitud:       19.0,
		Longitud:      -99.0,
		FechaVenta:    time.Now().UTC(),
		PrecioAnual:   decimal.NewFromInt(2222),
		PrecioCorto:   decimal.NewFromInt(1722),
		PrecioContado: decimal.NewFromInt(2222),
	}, actorB)
	require.NoError(t, err, "writer B should commit without optimistic-lock conflict — last-write-wins is intentional")

	// The final state belongs to the last writer (B). A's changes are
	// silently lost — no notification, no conflict error.
	final, err := h.svc.ObtenerVenta(ctx, id)
	require.NoError(t, err)
	assert.True(t, decimal.NewFromInt(2222).Equal(final.Montos().Contado()),
		"final contado must equal B's write (last-write-wins); A's changes were silently overwritten. got=%s",
		final.Montos().Contado().String())
	// Address text is folded to ALL CAPS by the domain (Microsip convention).
	assert.Equal(t, "AV. B", final.Direccion().Calle(),
		"final calle must equal B's write")
	finalAudit := final.Audit()
	assert.Equal(t, actorB, finalAudit.UpdatedBy(),
		"updated_by must reflect the last writer, not A")
}
