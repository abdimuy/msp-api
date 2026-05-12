package firebird_test

// Pool lifecycle tests.
//
// TestPool_PingHappyPath needs a real Microsip DB — it's gated by FB_DATABASE
// via fbtestutil. The other two tests construct unreachable pools and need
// no DB at all (run under any environment, including `go test -short`).

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// newDeadPool returns a *firebird.Pool whose underlying *sql.DB is already
// closed. Calling HealthCheck / Ping on it returns an error — useful for
// testing failure branches of probes and lifecycle without disturbing the
// shared fbtestutil pool.
func newDeadPool(t *testing.T) *firebird.Pool {
	t.Helper()
	p, err := firebird.New(config.Firebird{
		Host: "127.0.0.1", Port: 65000,
		Database: "/dev/null/dead.fdb",
		User:     "SYSDBA", Password: "x", Charset: "UTF8", PoolSize: 1,
	})
	require.NoError(t, err)
	require.NoError(t, p.Stop(context.Background()))
	return p
}

func TestPool_PingHappyPath(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t) // skips if FB_DATABASE unset

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := pool.HealthCheck(ctx)
	require.NoError(t, err, "HealthCheck must succeed against the dev Microsip DB")
}

func TestPool_BadDSN_Fails(t *testing.T) {
	t.Parallel()
	// Points at an unreachable port — no live DB needed, Start must fail.
	cfg := config.Firebird{
		Host:     "127.0.0.1",
		Port:     65000,
		Database: "/nonexistent/test.fdb",
		User:     "SYSDBA",
		Password: "masterkey",
		Charset:  "UTF8",
		PoolSize: 1,
	}
	pool, err := firebird.New(cfg)
	require.NoError(t, err, "New must succeed — it only opens, does not connect yet")

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	assert.Error(t, pool.Start(ctx), "Start must fail when the host is unreachable")
}

func TestPool_Stop_Idempotent(t *testing.T) {
	t.Parallel()

	// sql.Open is lazy — Stop closes a pool that never connected. No live
	// Firebird required, and we MUST NOT touch fbtestutil's shared pool here
	// because other parallel tests would lose their connections.
	pool, err := firebird.New(config.Firebird{
		Host: "127.0.0.1", Port: 65000,
		Database: "/dev/null/test.fdb",
		User:     "SYSDBA", Password: "x", Charset: "UTF8", PoolSize: 1,
	})
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, pool.Stop(ctx), "first Stop must succeed")
	require.NoError(t, pool.Stop(ctx), "second Stop must be a no-op (sql.DB.Close is idempotent)")
}
