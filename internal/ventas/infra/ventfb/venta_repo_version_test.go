//nolint:misspell // Spanish vocabulary (venta, aprobada, borrador) by convention.
package ventfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
)

// TestVentaRepo_Version_InsertStartsAtZero pins that Save() always writes a
// freshly-created venta's MSP_VENTAS.VERSION at 0, matching the migration's
// backfill of every pre-existing row.
func TestVentaRepo_Version_InsertStartsAtZero(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.Equal(t, 0, v.Version(), "a freshly CrearVenta'd aggregate starts at version 0")
		require.NoError(t, repo.Save(ctx, v))

		got, err := repo.FindByID(ctx, v.ID())
		require.NoError(t, err)
		assert.Equal(t, 0, got.Version())
	})
}

// TestVentaRepo_Version_ConcurrentWritersOneWinsOneConflicts is Task G1's
// central acceptance test: two readers load the SAME row at the SAME
// version, both mutate their own in-memory copy, and write back through the
// SAME code path (Update). The first write must succeed and bump the row's
// VERSION; the second — computed from a snapshot that is now stale — must
// fail with ErrVentaVersionConflicto and leave the row exactly as the first
// writer left it, field by field.
func TestVentaRepo_Version_ConcurrentWritersOneWinsOneConflicts(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		seed := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, seed))

		// Two independent readers load the same row at version 0.
		readerA, err := repo.FindByID(ctx, seed.ID())
		require.NoError(t, err)
		readerB, err := repo.FindByID(ctx, seed.ID())
		require.NoError(t, err)
		require.Equal(t, 0, readerA.Version())
		require.Equal(t, 0, readerB.Version())

		// UPDATED_BY is FK-constrained to MSP_USUARIOS, so both actors need
		// their own seeded row.
		actorA := seedUsuarioRow(ctx, t, pool)
		actorB := seedUsuarioRow(ctx, t, pool)
		now := testNow().Add(time.Hour)

		// Writer A commits first. Its CAS precondition (VERSION=0) matches.
		require.NoError(t, readerA.Cancelar("writer A", actorA, now))
		require.NoError(t, repo.Update(ctx, readerA))
		assert.Equal(t, 1, readerA.Version(), "a successful write confirms PersistedVersion up to Version")

		// Writer B commits from its stale snapshot (still believes VERSION=0).
		require.NoError(t, readerB.Cancelar("writer B", actorB, now))
		err = repo.Update(ctx, readerB)
		require.ErrorIs(t, err, domain.ErrVentaVersionConflicto,
			"writer B's precondition (version 0) no longer matches the row (version 1)")

		// The row must be exactly what writer A wrote — writer B's write must
		// not have landed at all, on any column.
		got, err := repo.FindByID(ctx, seed.ID())
		require.NoError(t, err)
		require.Equal(t, 1, got.Version())
		require.NotNil(t, got.Cancelacion())
		assert.Equal(t, "writer A", got.Cancelacion().Reason())
		assert.Equal(t, actorA, got.Cancelacion().By())
		assert.NotEqual(t, actorB, got.Cancelacion().By())
	})
}

// TestVentaRepo_Version_EachWriteMethodBumpsIt exercises every one of the
// four VERSION-guarded UPDATE statements — Update, UpdateHeader,
// UpdateCliente, and touchHeader (reached via ReplaceProductos) — and pins
// that each one advances the persisted VERSION by at least 1.
func TestVentaRepo_Version_EachWriteMethodBumpsIt(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v))

		readVersion := func() int {
			got, err := repo.FindByID(ctx, v.ID())
			require.NoError(t, err)
			return got.Version()
		}
		require.Equal(t, 0, readVersion())

		// Update (transition command path).
		require.NoError(t, v.Cancelar("sube version", root, testNow().Add(time.Hour)))
		require.NoError(t, repo.Update(ctx, v))
		assert.Equal(t, 1, readVersion(), "Update must bump VERSION")

		// UpdateHeader — needs a borrador venta, so use a fresh one.
		v2 := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v2))
		newDir, err := domain.NewDireccion(domain.NewDireccionParams{
			Calle: "Otra", Colonia: "Centro", Poblacion: "CDMX", Ciudad: "CDMX",
		})
		require.NoError(t, err)
		require.NoError(t, v2.ActualizarHeader(domain.ActualizarHeaderParams{
			Direccion: newDir, GPS: v2.GPS(), FechaVenta: v2.FechaVenta().Add(time.Hour),
			By: root, Now: testNow().Add(time.Hour),
		}))
		require.NoError(t, repo.UpdateHeader(ctx, v2))
		got2, err := repo.FindByID(ctx, v2.ID())
		require.NoError(t, err)
		assert.Equal(t, 1, got2.Version(), "UpdateHeader must bump VERSION")

		// UpdateCliente.
		v3 := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v3))
		snap, err := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: v3.Cliente().Nombre()})
		require.NoError(t, err)
		require.NoError(t, v3.ActualizarCliente(domain.ActualizarClienteParams{
			Cliente: snap, By: root, Now: testNow().Add(time.Hour),
		}))
		require.NoError(t, repo.UpdateCliente(ctx, v3))
		got3, err := repo.FindByID(ctx, v3.ID())
		require.NoError(t, err)
		assert.Equal(t, 1, got3.Version(), "UpdateCliente must bump VERSION")

		// touchHeader via ReplaceProductos.
		v4 := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v4))
		precios, err := domain.NewMontoSnapshot(
			decimal.RequireFromString("60.00"), decimal.RequireFromString("55.00"), decimal.RequireFromString("50.00"),
		)
		require.NoError(t, err)
		one, two := 1, 2
		require.NoError(t, v4.ReemplazarProductos(domain.ReemplazarProductosParams{
			Productos: []domain.CrearVentaProductoInput{{
				ID: uuid.New(), ArticuloID: 1, Articulo: "P1",
				Cantidad: decimal.RequireFromString("1"), Precios: precios,
				AlmacenOrigen: &one, AlmacenDestino: &two,
			}},
			By: root, Now: testNow().Add(time.Hour),
		}))
		require.NoError(t, repo.ReplaceProductos(ctx, v4))
		got4, err := repo.FindByID(ctx, v4.ID())
		require.NoError(t, err)
		assert.Equal(t, 1, got4.Version(), "ReplaceProductos (touchHeader) must bump VERSION")
	})
}
