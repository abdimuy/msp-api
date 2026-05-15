package postgres_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/idempotency"
	idempotencypg "github.com/abdimuy/msp-api/internal/platform/idempotency/postgres"
	"github.com/abdimuy/msp-api/internal/platform/testutil"
	"github.com/abdimuy/msp-api/internal/platform/transaction"
)

var testPool *pgxpool.Pool

// TestMain spins up the shared per-package test DB only when integration
// signals are present. Unit-test runs (`go test -short ./...`) still pass by
// skipping every test via requirePool.
func TestMain(m *testing.M) {
	if os.Getenv("INTEGRATION") != "" || os.Getenv("TEST_DATABASE_URL") != "" {
		testPool = testutil.NewTestDatabasePool()
	}
	code := m.Run()
	testutil.DropPackageDBs()
	os.Exit(code)
}

func requirePool(t *testing.T) {
	t.Helper()
	if testPool == nil {
		t.Skip("skipping integration test: set INTEGRATION=1 to run")
	}
}

func newRecord(key, body string, expiresAt time.Time) idempotency.Record {
	return idempotency.Record{
		Key:            key,
		Method:         "POST",
		Path:           "/v2/cobros",
		RequestHash:    "hash-" + key,
		ResponseStatus: 201,
		ResponseBody:   json.RawMessage(body),
		ExpiresAt:      expiresAt,
	}
}

func TestStore_Get_MissingKey_ReturnsNilNil_Integration(t *testing.T) {
	t.Parallel()
	requirePool(t)
	s := idempotencypg.New(testPool)
	testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
		got, err := s.Get(ctx, "does-not-exist")
		require.NoError(t, err)
		assert.Nil(t, got)
	})
}

func TestStore_SaveThenGet_RoundTrip_Integration(t *testing.T) {
	t.Parallel()
	requirePool(t)
	s := idempotencypg.New(testPool)
	testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
		rec := newRecord("rt-1", `{"ok":true}`, time.Now().Add(time.Hour))
		require.NoError(t, s.Save(ctx, rec))

		got, err := s.Get(ctx, "rt-1")
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

// Save twice with the same key but different payload before expiry: the
// second Save MUST be silently dropped (existing valid row wins).
func TestStore_SaveTwice_BeforeExpiry_FirstWins_Integration(t *testing.T) {
	t.Parallel()
	requirePool(t)
	s := idempotencypg.New(testPool)
	testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
		first := newRecord("dup-1", `{"first":true}`, time.Now().Add(time.Hour))
		require.NoError(t, s.Save(ctx, first))

		second := first
		second.RequestHash = "hash-different"
		second.ResponseStatus = 500
		second.ResponseBody = json.RawMessage(`{"second":true}`)
		require.NoError(t, s.Save(ctx, second))

		got, err := s.Get(ctx, "dup-1")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, first.RequestHash, got.RequestHash, "first writer must win")
		assert.Equal(t, first.ResponseStatus, got.ResponseStatus)
		assert.JSONEq(t, string(first.ResponseBody), string(got.ResponseBody))
	})
}

// Save with an expired row + same key MUST replace the row.
func TestStore_Save_ReplacesExpiredRow_Integration(t *testing.T) {
	t.Parallel()
	requirePool(t)
	testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
		// Insert an already-expired row directly through the tx so it is visible
		// to subsequent store calls inside the same tx.
		expired := newRecord("exp-1", `{"old":true}`, time.Now().Add(-time.Hour))
		_, err := transaction.GetQuerier(ctx, testPool).Exec(ctx, `
			INSERT INTO idempotency_keys
				(key, method, path, request_hash, response_status, response_body, expires_at, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			expired.Key, expired.Method, expired.Path, expired.RequestHash,
			expired.ResponseStatus, []byte(expired.ResponseBody),
			expired.ExpiresAt, time.Now().Add(-2*time.Hour),
		)
		require.NoError(t, err)

		s := idempotencypg.New(testPool)
		fresh := newRecord("exp-1", `{"new":true}`, time.Now().Add(time.Hour))
		fresh.RequestHash = "hash-fresh"
		fresh.ResponseStatus = 202
		require.NoError(t, s.Save(ctx, fresh))

		got, err := s.Get(ctx, "exp-1")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, fresh.RequestHash, got.RequestHash, "expired row must be replaced")
		assert.Equal(t, fresh.ResponseStatus, got.ResponseStatus)
		assert.JSONEq(t, string(fresh.ResponseBody), string(got.ResponseBody))
	})
}

// Rows whose expires_at has passed must be filtered out by Get.
func TestStore_Get_FiltersExpired_Integration(t *testing.T) {
	t.Parallel()
	requirePool(t)
	testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
		// Insert the stale row through the tx so it is visible within this test.
		_, err := transaction.GetQuerier(ctx, testPool).Exec(ctx, `
			INSERT INTO idempotency_keys
				(key, method, path, request_hash, response_status, response_body, expires_at, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			"stale-1", "POST", "/x", "h", 200, []byte(`{"a":1}`),
			time.Now().Add(-time.Minute), time.Now().Add(-time.Hour),
		)
		require.NoError(t, err)

		s := idempotencypg.New(testPool)
		got, err := s.Get(ctx, "stale-1")
		require.NoError(t, err)
		assert.Nil(t, got, "expired row must not be returned")
	})
}
