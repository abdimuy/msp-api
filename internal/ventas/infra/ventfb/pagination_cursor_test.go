//nolint:misspell // Spanish vocabulary by convention.
package ventfb_test

// Cursor pagination boundary tests: seeds rows with distinct sub-second
// FECHA_VENTA offsets and walks the cursor end to end. The (FECHA_VENTA, ID)
// tiebreaker contract guarantees no duplicates and no skips even when
// timestamps share a wall-clock second.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// TestVentaRepo_List_CursorMicrosecondPrecision_Firebird seeds N ventas
// inside the same wall-clock second at distinct microsecond offsets and
// walks the cursor at pageSize=2. Every seeded venta must appear exactly
// once. If Firebird truncates microseconds and the (FECHA_VENTA, ID) cursor
// loses its tiebreak, this test surfaces the gap as duplicates or skips
// rather than letting it hide as silent data loss in production listings.
func TestVentaRepo_List_CursorMicrosecondPrecision_Firebird(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)

		// All five ventas share the same wall-clock second but each has a
		// distinct microsecond offset. The cursor's (FECHA_VENTA, ID)
		// tuple must keep them ordered without collisions.
		anchor := time.Date(2026, 6, 15, 12, 30, 45, 0, time.UTC)
		const newRows = 5
		created := make(map[uuid.UUID]bool, newRows)
		for i := range newRows {
			fecha := anchor.Add(time.Duration(i) * time.Microsecond)
			v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root, fecha: fecha})
			require.NoError(t, repo.Save(ctx, v))
			created[v.ID()] = true
		}

		seen := make(map[uuid.UUID]int)
		const pageSize = 2
		const safetyLimit = 500
		var cursor string
		pages := 0
		for pages < safetyLimit {
			page, err := repo.List(ctx,
				outbound.ListParams{Cursor: cursor, PageSize: pageSize},
				outbound.ListVentasFilters{},
			)
			require.NoError(t, err)
			require.LessOrEqual(t, len(page.Items), pageSize)
			for _, it := range page.Items {
				seen[it.ID()]++
			}
			pages++
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
		require.Less(t, pages, safetyLimit, "pagination must terminate")

		// Every seeded venta must appear exactly once.
		for id := range created {
			assert.Equal(t, 1, seen[id],
				"venta %s should appear once across all pages, saw %d times",
				id, seen[id])
		}
	})
}
