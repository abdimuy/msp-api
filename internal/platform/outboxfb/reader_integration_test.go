//go:build !ci_skip_firebird

package outboxfb_test

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
	"github.com/abdimuy/msp-api/internal/platform/outboxfb"
)

func TestReadByAggregateID_ReturnsEventsOldestFirst(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()

	pool := fbtestutil.NewTestFirebirdPool(t)
	requireOutboxTable(t, pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		aggregateID := uuid.New()

		// Insert three events with controlled CREATED_AT, out of order, so we
		// can assert the reader sorts them ascending.
		base := time.Date(2026, 6, 9, 1, 0, 0, 0, time.UTC)
		insertEventAt(ctx, t, pool, aggregateID, "venta.aprobada", `{"by":"u1"}`, base.Add(2*time.Minute))
		insertEventAt(ctx, t, pool, aggregateID, "venta.creada", `{"cliente":"x"}`, base)
		insertEventAt(ctx, t, pool, aggregateID, "venta.aplicada", `{"folio":"Y1"}`, base.Add(4*time.Minute))

		events, err := outboxfb.ReadByAggregateID(ctx, pool.DB, aggregateID)
		require.NoError(t, err)
		require.Len(t, events, 3)

		// Ascending by CREATED_AT.
		assert.Equal(t, "venta.creada", events[0].EventType)
		assert.Equal(t, "venta.aprobada", events[1].EventType)
		assert.Equal(t, "venta.aplicada", events[2].EventType)

		// Aggregate + payload round-trip.
		assert.Equal(t, "venta", events[0].Aggregate)
		assert.Equal(t, aggregateID, events[0].AggregateID)
		assert.JSONEq(t, `{"cliente":"x"}`, string(events[0].Payload))

		// Pending events have nil ProcessedAt/FailedAt and 0 attempts.
		assert.Nil(t, events[0].ProcessedAt)
		assert.Nil(t, events[0].FailedAt)
		assert.Nil(t, events[0].LastError)
		assert.Equal(t, 0, events[0].Attempts)
	})
}

func TestReadByAggregateID_EmptyWhenNoEvents(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()

	pool := fbtestutil.NewTestFirebirdPool(t)
	requireOutboxTable(t, pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		events, err := outboxfb.ReadByAggregateID(ctx, pool.DB, uuid.New())
		require.NoError(t, err)
		assert.Empty(t, events)
	})
}

func TestReadByAggregateID_OnlyReturnsMatchingAggregate(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()

	pool := fbtestutil.NewTestFirebirdPool(t)
	requireOutboxTable(t, pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		wanted := uuid.New()
		other := uuid.New()
		base := time.Date(2026, 6, 9, 2, 0, 0, 0, time.UTC)
		insertEventAt(ctx, t, pool, wanted, "venta.creada", `{}`, base)
		insertEventAt(ctx, t, pool, other, "venta.creada", `{}`, base)

		events, err := outboxfb.ReadByAggregateID(ctx, pool.DB, wanted)
		require.NoError(t, err)
		require.Len(t, events, 1)
		assert.Equal(t, wanted, events[0].AggregateID)
	})
}

func TestReadByAggregateAndPayloadContaining_MatchesByPayload(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()

	pool := fbtestutil.NewTestFirebirdPool(t)
	requireOutboxTable(t, pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		ventaID := uuid.New()
		base := time.Date(2026, 6, 9, 3, 0, 0, 0, time.UTC)

		// A traspaso event for our venta (linked only via payload), a traspaso
		// for a different venta, and a non-traspaso event — only the first must
		// match.
		mine := `{"folio":"MST1","almacen_origen":7,"venta_id":"` + ventaID.String() + `"}`
		insertEventFull(ctx, t, pool, "traspaso", uuid.New(), "traspaso.creado", mine, base)
		other := `{"folio":"MST2","venta_id":"` + uuid.New().String() + `"}`
		insertEventFull(ctx, t, pool, "traspaso", uuid.New(), "traspaso.creado", other, base)
		insertEventFull(ctx, t, pool, "venta", ventaID, "venta.creada", `{}`, base)

		needle := `"venta_id":"` + ventaID.String() + `"`
		events, err := outboxfb.ReadByAggregateAndPayloadContaining(ctx, pool.DB, "traspaso", needle)
		require.NoError(t, err)
		require.Len(t, events, 1)
		assert.Equal(t, "traspaso.creado", events[0].EventType)
		assert.Equal(t, "traspaso", events[0].Aggregate)
		assert.JSONEq(t, mine, string(events[0].Payload))
	})
}

func TestReadByAggregateAndPayloadContaining_EmptyWhenNoMatch(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()

	pool := fbtestutil.NewTestFirebirdPool(t)
	requireOutboxTable(t, pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		events, err := outboxfb.ReadByAggregateAndPayloadContaining(
			ctx, pool.DB, "traspaso", `"venta_id":"`+uuid.New().String()+`"`,
		)
		require.NoError(t, err)
		assert.Empty(t, events)
	})
}

// insertEventAt inserts one outbox row with an explicit CREATED_AT so tests
// can control ordering. It bypasses outboxfb.Enqueue (which stamps now())
// precisely because we need deterministic timestamps.
func insertEventAt(
	ctx context.Context,
	t *testing.T,
	pool *firebird.Pool,
	aggregateID uuid.UUID,
	eventType, payload string,
	createdAt time.Time,
) {
	t.Helper()
	insertEventFull(ctx, t, pool, "venta", aggregateID, eventType, payload, createdAt)
}

// insertEventFull is insertEventAt with an explicit AGGREGATE so tests can
// seed traspaso-aggregate rows linked to a venta only through their payload.
func insertEventFull(
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
