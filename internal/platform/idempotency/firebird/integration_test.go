//go:build !ci_skip_firebird

package firebird_test

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
	"github.com/abdimuy/msp-api/internal/platform/idempotency"
	idempotencyfb "github.com/abdimuy/msp-api/internal/platform/idempotency/firebird"
)

// requireIdempotencyTable skips the test when MSP_IDEMPOTENCY_KEYS is not
// present. Migration 000030 must be applied before these tests can run.
func requireIdempotencyTable(t *testing.T, pool *firebird.Pool) {
	t.Helper()
	ctx := context.Background()
	err := firebird.RunInReadTx(ctx, pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, pool.DB)
		row := q.QueryRowContext(
			ctx,
			`SELECT COUNT(*) FROM RDB$RELATIONS WHERE RDB$RELATION_NAME = 'MSP_IDEMPOTENCY_KEYS'`,
		)
		var count int
		if scanErr := row.Scan(&count); scanErr != nil {
			return scanErr
		}
		if count == 0 {
			t.Skip("MSP_IDEMPOTENCY_KEYS table not found; run migration 000030 first")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("requireIdempotencyTable: check table: %v", err)
	}
}

// newTestRecord builds a test idempotency.Record.
//
// NOTE: RESPONSE_STATUS must be 200-299 — the CK_MSP_IDEMPOTENCY_KEYS_STATUS
// constraint enforces this. The middleware only caches 2xx so test data must
// honour the same invariant.
//
// NOTE: ExpiresAt must be strictly after CreatedAt (now) — the
// CK_MSP_IDEMPOTENCY_KEYS_TTL constraint enforces this. The TTL argument must
// be positive.
func newTestRecord(key, body string, ttl time.Duration) idempotency.Record {
	return idempotency.Record{
		Key:            key,
		Method:         "POST",
		Path:           "/v2/ventas",
		RequestHash:    "hash-" + key,
		ResponseStatus: 201,
		ResponseBody:   json.RawMessage(body),
		ExpiresAt:      time.Now().Add(ttl),
	}
}

func TestStore_Get_MissingKey_ReturnsNilNil(t *testing.T) {
	t.Parallel()

	pool := fbtestutil.NewTestFirebirdPool(t)
	requireIdempotencyTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := idempotencyfb.New(pool)
		got, err := s.Get(ctx, uuid.NewString()+"-missing")
		require.NoError(t, err)
		assert.Nil(t, got)
	})
}

func TestStore_SaveThenGet_RoundTrip(t *testing.T) {
	t.Parallel()

	pool := fbtestutil.NewTestFirebirdPool(t)
	requireIdempotencyTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := idempotencyfb.New(pool)
		key := uuid.NewString() + "-rt"
		rec := newTestRecord(key, `{"ok":true}`, time.Hour)

		require.NoError(t, s.Save(ctx, rec))

		got, err := s.Get(ctx, key)
		require.NoError(t, err)
		require.NotNil(t, got)

		assert.Equal(t, rec.Key, got.Key)
		assert.Equal(t, rec.Method, got.Method)
		assert.Equal(t, rec.Path, got.Path)
		assert.Equal(t, rec.RequestHash, got.RequestHash)
		assert.Equal(t, rec.ResponseStatus, got.ResponseStatus)
		assert.JSONEq(t, string(rec.ResponseBody), string(got.ResponseBody))
		assert.WithinDuration(t, rec.ExpiresAt, got.ExpiresAt, time.Second)
	})
}

// TestStore_SaveTwice_BeforeExpiry_FirstWins verifies that a second Save for
// the same key before expiry is silently dropped — first writer wins.
func TestStore_SaveTwice_BeforeExpiry_FirstWins(t *testing.T) {
	t.Parallel()

	pool := fbtestutil.NewTestFirebirdPool(t)
	requireIdempotencyTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := idempotencyfb.New(pool)
		key := uuid.NewString() + "-dup"

		first := newTestRecord(key, `{"first":true}`, time.Hour)
		require.NoError(t, s.Save(ctx, first))

		second := first
		second.RequestHash = "hash-different"
		second.ResponseStatus = 202
		second.ResponseBody = json.RawMessage(`{"second":true}`)
		require.NoError(t, s.Save(ctx, second))

		got, err := s.Get(ctx, key)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, first.RequestHash, got.RequestHash, "first writer must win")
		assert.Equal(t, first.ResponseStatus, got.ResponseStatus)
		assert.JSONEq(t, string(first.ResponseBody), string(got.ResponseBody))
	})
}

// TestStore_Save_ReplacesExpiredRow verifies that Save replaces a row whose
// EXPIRES_AT is in the past with the new record.
//
// The direct INSERT sets CREATED_AT two hours in the past and EXPIRES_AT one
// hour in the past to satisfy both the TTL CHECK constraint (EXPIRES_AT >
// CREATED_AT) and the expiry condition (EXPIRES_AT < now).
func TestStore_Save_ReplacesExpiredRow(t *testing.T) {
	t.Parallel()

	pool := fbtestutil.NewTestFirebirdPool(t)
	requireIdempotencyTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		key := uuid.NewString() + "-exp-replace"
		s := idempotencyfb.New(pool)

		// Insert an expired row directly through the ambient tx.
		// CREATED_AT = 2h ago, EXPIRES_AT = 1h ago → satisfies CHECK + expired.
		q := firebird.GetQuerier(ctx, pool.DB)
		now := time.Now()
		createdWC := firebird.ToWallClock(now.Add(-2 * time.Hour))
		expiresWC := firebird.ToWallClock(now.Add(-1 * time.Hour))
		_, err := q.ExecContext(
			ctx, `
			INSERT INTO MSP_IDEMPOTENCY_KEYS
				(IDEM_KEY, METHOD, PATH, REQUEST_HASH, RESPONSE_STATUS,
				 RESPONSE_BODY, CREATED_AT, EXPIRES_AT)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			key, "POST", "/v2/ventas", "hash-old", 200,
			[]byte(`{"old":true}`), createdWC, expiresWC,
		)
		require.NoError(t, err)

		fresh := newTestRecord(key, `{"new":true}`, time.Hour)
		fresh.RequestHash = "hash-fresh"
		fresh.ResponseStatus = 202
		require.NoError(t, s.Save(ctx, fresh))

		got, err := s.Get(ctx, key)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, fresh.RequestHash, got.RequestHash, "expired row must be replaced")
		assert.Equal(t, fresh.ResponseStatus, got.ResponseStatus)
		assert.JSONEq(t, string(fresh.ResponseBody), string(got.ResponseBody))
	})
}

// TestStore_Get_FiltersExpired verifies that Get treats an expired row as absent
// and returns (nil, nil).
//
// The direct INSERT uses CREATED_AT = 2h ago, EXPIRES_AT = 1h ago to satisfy
// the TTL CHECK constraint while also being expired at query time.
func TestStore_Get_FiltersExpired(t *testing.T) {
	t.Parallel()

	pool := fbtestutil.NewTestFirebirdPool(t)
	requireIdempotencyTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		key := uuid.NewString() + "-stale"
		s := idempotencyfb.New(pool)

		q := firebird.GetQuerier(ctx, pool.DB)
		now := time.Now()
		createdWC := firebird.ToWallClock(now.Add(-2 * time.Hour))
		expiresWC := firebird.ToWallClock(now.Add(-1 * time.Hour))
		_, err := q.ExecContext(
			ctx, `
			INSERT INTO MSP_IDEMPOTENCY_KEYS
				(IDEM_KEY, METHOD, PATH, REQUEST_HASH, RESPONSE_STATUS,
				 RESPONSE_BODY, CREATED_AT, EXPIRES_AT)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			key, "GET", "/v2/clientes", "hash-stale", 200,
			[]byte(`{"stale":1}`), createdWC, expiresWC,
		)
		require.NoError(t, err)

		got, err := s.Get(ctx, key)
		require.NoError(t, err)
		assert.Nil(t, got, "expired row must not be returned by Get")
	})
}

// TestStore_PurgeExpired_DeletesAndReturnsCount inserts 3 expired rows and 1
// fresh row, then asserts:
//   - PurgeExpired returns count == 3
//   - Get for any expired key returns nil
//   - Get for the fresh key still resolves
func TestStore_PurgeExpired_DeletesAndReturnsCount(t *testing.T) {
	t.Parallel()

	pool := fbtestutil.NewTestFirebirdPool(t)
	requireIdempotencyTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := idempotencyfb.New(pool)
		q := firebird.GetQuerier(ctx, pool.DB)
		now := time.Now()

		// Seed helper that respects CK_TTL: CREATED_AT must be < EXPIRES_AT.
		insertRaw := func(key string, createdDelta, expiresDelta time.Duration) {
			t.Helper()
			createdWC := firebird.ToWallClock(now.Add(createdDelta))
			expiresWC := firebird.ToWallClock(now.Add(expiresDelta))
			_, err := q.ExecContext(
				ctx, `
				INSERT INTO MSP_IDEMPOTENCY_KEYS
					(IDEM_KEY, METHOD, PATH, REQUEST_HASH, RESPONSE_STATUS,
					 RESPONSE_BODY, CREATED_AT, EXPIRES_AT)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				key, "POST", "/v2/ventas", "hash-"+key, 200,
				[]byte(`{"x":1}`), createdWC, expiresWC,
			)
			require.NoError(t, err, "seed row %s", key)
		}

		prefix := uuid.NewString()
		expiredKey1 := prefix + "-expired-1"
		expiredKey2 := prefix + "-expired-2"
		expiredKey3 := prefix + "-expired-3"
		freshKey := prefix + "-fresh"

		// Expired rows: CREATED_AT 3h ago, EXPIRES_AT 1h ago.
		insertRaw(expiredKey1, -3*time.Hour, -1*time.Hour)
		insertRaw(expiredKey2, -3*time.Hour, -1*time.Hour)
		insertRaw(expiredKey3, -3*time.Hour, -1*time.Hour)
		// Fresh row: CREATED_AT now, EXPIRES_AT 1h from now.
		insertRaw(freshKey, -time.Minute, time.Hour)

		n, err := s.PurgeExpired(ctx, now)
		require.NoError(t, err)
		// n may be ≥ 3 if other expired rows exist in the DB from other tests;
		// we only assert our 3 were included.
		assert.GreaterOrEqual(t, n, int64(3), "must delete at least the 3 expired test rows")

		// Expired keys must be gone.
		for _, k := range []string{expiredKey1, expiredKey2, expiredKey3} {
			got, getErr := s.Get(ctx, k)
			require.NoError(t, getErr)
			assert.Nil(t, got, "expired key %s must be absent after purge", k)
		}

		// Fresh key must still resolve.
		got, err := s.Get(ctx, freshKey)
		require.NoError(t, err)
		require.NotNil(t, got, "fresh key must still be present after purge")
		assert.Equal(t, freshKey, got.Key)
	})
}
