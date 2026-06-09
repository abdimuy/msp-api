//nolint:misspell // Spanish vocabulary (ventas, eventos, traspaso) by convention.
package ventfb_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
)

// insertOutboxRow inserts one MSP_OUTBOX_EVENTS row with an explicit AGGREGATE
// and CREATED_AT so the test can seed both a venta-keyed event and a
// traspaso-keyed event linked to the venta only through its payload.
func insertOutboxRow(
	ctx context.Context,
	t *testing.T,
	pool *firebird.Pool,
	aggregate string,
	aggregateID uuid.UUID,
	eventType, payload string,
	createdAt time.Time,
) {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	_, err := q.ExecContext(
		ctx,
		`INSERT INTO MSP_OUTBOX_EVENTS
		   (ID, AGGREGATE, AGGREGATE_ID, EVENT_TYPE, PAYLOAD, CREATED_AT, ATTEMPTS)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uuid.New().String(),
		aggregate,
		aggregateID.String(),
		eventType,
		json.RawMessage(payload),
		firebird.ToWallClock(createdAt),
		0,
	)
	require.NoError(t, err)
}

// TestEventoRepo_EventosDeVenta_FoldsInLinkedTraspaso verifies the timeline
// reader merges traspaso events (keyed by their own id, linked to the venta
// only through payload.venta_id) into the venta's chronological event list.
func TestEventoRepo_EventosDeVenta_FoldsInLinkedTraspaso(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()

	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewEventoRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		ventaID := uuid.New()
		traspasoID := uuid.New()
		base := time.Date(2026, 6, 9, 5, 0, 0, 0, time.UTC)

		// venta.creada (t0), then the traspaso (t1) keyed by traspasoID but
		// carrying venta_id in its payload.
		insertOutboxRow(ctx, t, pool, "venta", ventaID, "venta.creada", `{"tipo_venta":"CREDITO"}`, base)
		traspasoPayload := `{"folio":"MST000062","almacen_origen":11341,"almacen_destino":11058,` +
			`"detalles_count":1,"venta_id":"` + ventaID.String() + `"}`
		insertOutboxRow(ctx, t, pool, "traspaso", traspasoID, "traspaso.creado", traspasoPayload, base.Add(time.Minute))

		// A traspaso for a different venta must NOT leak in.
		insertOutboxRow(ctx, t, pool, "traspaso", uuid.New(), "traspaso.creado",
			`{"venta_id":"`+uuid.New().String()+`"}`, base.Add(2*time.Minute))

		eventos, err := repo.EventosDeVenta(ctx, ventaID)
		require.NoError(t, err)
		require.Len(t, eventos, 2, "expected venta.creada + the linked traspaso")

		// Chronological order is preserved across the two sources.
		assert.Equal(t, "venta.creada", eventos[0].EventType)
		assert.Equal(t, "traspaso.creado", eventos[1].EventType)
		assert.Contains(t, string(eventos[1].Payload), `"almacen_origen":11341`)
	})
}
