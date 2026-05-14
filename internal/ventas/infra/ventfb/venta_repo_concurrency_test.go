//nolint:misspell // Spanish vocabulary (productos, etc.) by convention.
package ventfb_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
)

// TestVentaRepo_Concurrent_SavesAndReads verifies the shared *firebird.Pool
// holds up when N parallel sub-tests each open their own test transaction
// and perform a Save → FindByID → Update_Cancel cycle. Each sub-test runs
// with t.Parallel so the connection semaphore is exercised. The point is to
// confirm the pool blocks fairly without timeouts or pool exhaustion errors.
func TestVentaRepo_Concurrent_SavesAndReads(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)

	const goroutines = 25
	var ok int64
	for i := range goroutines {
		name := fmt.Sprintf("worker-%02d", i)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
				root := seedUsuarioRow(ctx, t, pool)
				v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})

				require.NoError(t, repo.Save(ctx, v), "%s: save", name)
				got, err := repo.FindByID(ctx, v.ID())
				require.NoError(t, err, "%s: find", name)
				require.Equal(t, v.ID(), got.ID(), "%s: id round-trip", name)

				require.NoError(t, v.Cancelar("concurrent test", root, testNow().Add(time.Hour)),
					"%s: cancelar", name)
				require.NoError(t, repo.Update(ctx, v), "%s: update", name)

				atomic.AddInt64(&ok, 1)
			})
		})
	}
	t.Cleanup(func() {
		require.Equal(t, int64(goroutines), atomic.LoadInt64(&ok),
			"every goroutine must have completed its tx without pool errors")
	})
}

// TestVentaRepo_Concurrent_OverPoolCap drives parallel sub-tests well past
// the configured FB_POOL_SIZE (default 10) to confirm requests queue and
// drain rather than failing with a "too many connections" style error. We
// use sub-tests with t.Parallel rather than bare goroutines so require.*
// stays legal under testifylint's go-require rule.
func TestVentaRepo_Concurrent_OverPoolCap(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)

	const workers = 30
	for i := range workers {
		name := fmt.Sprintf("over-cap-%02d", i)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
				root := seedUsuarioRow(ctx, t, pool)
				v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
				require.NoError(t, repo.Save(ctx, v), "%s: save", name)
				_, err := repo.FindByID(ctx, v.ID())
				require.NoError(t, err, "%s: find", name)
			})
		})
	}
}
