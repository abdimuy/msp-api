//nolint:misspell // Spanish vocabulary (productos, etc.) by convention.
package ventfb_test

// Adversarial / error-path coverage for VentaRepo. The happy paths are in
// venta_repo_test.go; this file pins the boundary contract when something
// goes wrong: rolled-back updates, FK violations on InsertImagen, and the
// structural impossibility of cross-venta imagen ID collisions.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
)

// snapshotVentaRow returns a tiny tuple of UPDATE-target columns we use for
// before/after comparison. Keep it short — exhaustive comparison would only
// rediscover what FindByID already does and add fragility.
type ventaRowSnapshot struct {
	calle  string
	monto  string // PRECIO formatted
	nota   *string
	updBy  string
	latLng [2]float64
}

func snapshotVenta(ctx context.Context, t *testing.T, pool *firebird.Pool, id uuid.UUID) ventaRowSnapshot {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	var (
		calle    firebird.Win1252
		anyVal   any
		notaFlag int
		nota     firebird.Win1252
		updBy    string
		lat, lng float64
	)
	row := q.QueryRowContext(ctx,
		`SELECT CALLE, MONTO_ANUAL,
		        CASE WHEN NOTA IS NULL THEN 1 ELSE 0 END,
		        COALESCE(NOTA, ''),
		        UPDATED_BY, LATITUD, LONGITUD
		 FROM MSP_VENTAS WHERE ID = ?`, id.String())
	require.NoError(t, row.Scan(&calle, &anyVal, &notaFlag, &nota, &updBy, &lat, &lng))
	d, err := firebird.ScanDecimal(anyVal, 2)
	require.NoError(t, err)
	var notaPtr *string
	if notaFlag == 0 {
		s := string(nota)
		notaPtr = &s
	}
	return ventaRowSnapshot{
		calle:  string(calle),
		monto:  d.String(),
		nota:   notaPtr,
		updBy:  updBy,
		latLng: [2]float64{lat, lng},
	}
}

// TestVentaRepo_UpdateHeader_RollsBackOnConstraintViolation_Firebird verifies
// that when UpdateHeader hits a CHECK constraint failure (latitud out of
// [-90, 90]), no field on the row changes. UpdateHeader is a single SQL
// UPDATE and Firebird is statement-atomic, so this is the canonical
// statement-level rollback contract — pinned here so that a future
// "smart" multi-statement refactor cannot regress silently.
func TestVentaRepo_UpdateHeader_RollsBackOnConstraintViolation_Firebird(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		v := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, v))

		before := snapshotVenta(ctx, t, pool, v.ID())

		// Forge an out-of-range latitude via Hydrate (bypasses domain
		// validation that would have caught it). 91 violates
		// CK_MSP_VENTAS_LATITUD which requires latitud BETWEEN -90 AND 90.
		a := v.Audit()
		bogus := domain.HydrateVenta(domain.HydrateVentaParams{
			ID: v.ID(), ClienteID: v.ClienteID(),
			Cliente: v.Cliente(), Direccion: v.Direccion(),
			GPS:        domain.HydrateGPSCoords(91.0, 0.0),
			FechaVenta: v.FechaVenta(), TipoVenta: v.TipoVenta(),
			Montos:      v.Montos(),
			PlanCredito: v.PlanCredito(), DiaCobranza: v.DiaCobranza(),
			Nota:       v.Nota(),
			Status:     v.Status(),
			Combos:     v.CombosForRepo(),
			Productos:  v.ProductosForRepo(),
			Vendedores: v.VendedoresForRepo(),
			Imagenes:   v.ImagenesForRepo(),
			CreatedAt:  a.CreatedAt(), UpdatedAt: a.UpdatedAt(),
			CreatedBy: a.CreatedBy(), UpdatedBy: a.UpdatedBy(),
		})

		err := repo.UpdateHeader(ctx, bogus)
		require.Error(t, err, "out-of-range latitud must be rejected by CK constraint")

		after := snapshotVenta(ctx, t, pool, v.ID())
		assert.Equal(t, before, after, "row must be unchanged after constraint failure")
	})
}

// TestVentaRepo_InsertImagen_FKViolation_Firebird pins the FK contract on
// MSP_VENTAS_IMAGENES.VENTA_ID: inserting an imagen targeting a non-existent
// venta must surface a typed error (no orphan row).
func TestVentaRepo_InsertImagen_FKViolation_Firebird(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		ghost := uuid.New() // never inserted as a venta
		img := buildImagen(t, root)

		err := repo.InsertImagen(ctx, ghost, img)
		require.Error(t, err, "InsertImagen against non-existent venta must fail")

		// And no orphan row leaked into the imagenes table.
		q := firebird.GetQuerier(ctx, pool.DB)
		assertCount(ctx, t, q, "MSP_VENTAS_IMAGENES", img.ID().String(), "ID", 0)
	})
}

// TestVentaRepo_InsertImagen_DuplicateID_Firebird verifies that the imagen
// PK constraint structurally prevents the same imagen ID from existing
// across two different ventas. This test exists alongside the existing
// TestVentaRepo_DeleteImagen_OtherVenta_NotFound (which proves DeleteImagen
// scopes by venta_id) — together they pin the cross-venta safety story:
// the schema makes the collision impossible, and even if a future migration
// loosens PK to (VENTA_ID, ID), DeleteImagen still scopes correctly.
func TestVentaRepo_InsertImagen_DuplicateID_Firebird(t *testing.T) {
	requireFBEnv(t)
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := ventfb.NewVentaRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRow(ctx, t, pool)
		a := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		b := buildVenta(t, newVentaInput{createdBy: root, vendedor: root})
		require.NoError(t, repo.Save(ctx, a))
		require.NoError(t, repo.Save(ctx, b))

		img := buildImagen(t, root)
		require.NoError(t, repo.InsertImagen(ctx, a.ID(), img))

		// Attempt to insert the SAME imagen instance into venta B. The
		// PK_MSP_VENTAS_IMAGENES on (ID) makes this structurally impossible.
		err := repo.InsertImagen(ctx, b.ID(), img)
		require.Error(t, err, "duplicate imagen ID across ventas must hit PK constraint")

		// And A still owns the only copy.
		got, err := repo.FindByID(ctx, a.ID())
		require.NoError(t, err)
		assert.Equal(t, 1, got.ImagenesCount())
	})
}
