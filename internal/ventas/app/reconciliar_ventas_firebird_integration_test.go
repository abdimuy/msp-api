//nolint:misspell // domain vocabulary is Spanish (ventas) per project convention.
package app_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// requireFBEnvIntegration skips the calling test when FB_DATABASE is unset.
// Mirrors ventfb_test's requireFBEnv gating.
func requireFBEnvIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird integration test")
	}
}

// seedUsuarioRowForIntegration inserts a throwaway MSP_USUARIOS row so
// CrearVenta's real FK constraints (CREATED_BY, VENDEDOR_USUARIO_ID, ...)
// are satisfied. Runs inside the caller's ambient transaction so it rolls
// back with everything else.
func seedUsuarioRowForIntegration(ctx context.Context, t *testing.T, pool *firebird.Pool) uuid.UUID {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	id := uuid.New()
	now := time.Now().UTC()
	suffix := id.String()
	_, err := q.ExecContext(
		ctx,
		`INSERT INTO MSP_USUARIOS
		 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO, ESTATUS,
		  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
		 VALUES (?, ?, ?, 'ventas-search-integration-test', TRUE, 'FIREBASE_USER', ?, ?, ?, ?)`,
		id.String(), "fb-search-"+suffix, "search-"+suffix+"@example.invalid",
		firebird.ToWallClock(now), firebird.ToWallClock(now), id.String(), id.String(),
	)
	require.NoError(t, err, "seed usuario for ventas search integration test")
	return id
}

// TestReconciliarVentas_FirebirdIntegration seeds two real ventas (via the
// real ventfb.VentaRepo, inside a rolled-back transaction) — one left in
// 'borrador', one canceled — and asserts ReconciliarVentas (backed by a fake
// search index) reconciles BOTH, including the canceled one (soft-delete
// upsert, never a hard DeleteDocs call). Also asserts ReindexVenta reflects
// a subsequent header mutation.
func TestReconciliarVentas_FirebirdIntegration(t *testing.T) {
	requireFBEnvIntegration(t)
	t.Parallel()

	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedUsuarioRowForIntegration(ctx, t, pool)
		repo := ventfb.NewVentaRepo(pool)
		idx := &fakeVentaSearchIndex{}
		svc := app.NewService(repo, nil, nil, nil, outbound.ProductionClock{}, nil, nil, nil, nil, nil, nil).
			WithSearchIndex(idx)

		// Venta 1: stays in borrador (active, not canceled).
		in1 := validContadoInput()
		in1.ID = uuid.New()
		in1.Vendedores = []app.CrearVentaVendedorInput{{
			ID: uuid.New(), UsuarioID: root, Email: "root@example.invalid", Nombre: "Root Tester",
		}}
		v1, err := svc.CrearVenta(ctx, in1, root)
		require.NoError(t, err)

		// Venta 2: created then canceled.
		in2 := validCreditoInput()
		in2.ID = uuid.New()
		in2.Vendedores = []app.CrearVentaVendedorInput{{
			ID: uuid.New(), UsuarioID: root, Email: "root@example.invalid", Nombre: "Root Tester",
		}}
		v2, err := svc.CrearVenta(ctx, in2, root)
		require.NoError(t, err)
		_, err = svc.CancelarVenta(ctx, v2.ID(), "prueba de integración", root)
		require.NoError(t, err)

		total, err := svc.ReconciliarVentas(ctx)
		require.NoError(t, err, "ReconciliarVentas must succeed against the live Firebird DB")
		assert.Positive(t, total, "should reconcile at least the two seeded ventas")

		// Flatten every doc pushed across all Reconciliar batches.
		byID := map[uuid.UUID]outbound.VentaSearchDoc{}
		for _, batch := range idx.ReconciliarCalls {
			for _, d := range batch {
				byID[d.ID] = d
			}
		}

		doc1, ok := byID[v1.ID()]
		require.True(t, ok, "borrador venta must be reconciled")
		assert.Equal(t, "borrador", doc1.Situacion)
		assert.Equal(t, "active", doc1.Estado)

		doc2, ok := byID[v2.ID()]
		require.True(t, ok, "canceled venta MUST be reconciled too (soft-delete upsert)")
		assert.Equal(t, "cancelada", doc2.Situacion, "cancellation is a situacion flag, never a hard delete")
		assert.Equal(t, "active", doc2.Estado, "estado is unaffected by cancellation")

		// ReindexVenta after a mutation reflects the new state.
		_, err = svc.ActualizarHeader(ctx, app.ActualizarHeaderInput{
			VentaID:    v1.ID(),
			Calle:      "Calle Reindexada",
			Colonia:    doc1.Direccion, // arbitrary non-empty value; not asserted
			Poblacion:  "Poblacion",
			Ciudad:     "Ciudad",
			Latitud:    19.0,
			Longitud:   -99.0,
			FechaVenta: time.Now().UTC(),
		}, root)
		require.NoError(t, err)

		require.NoError(t, svc.ReindexVenta(ctx, v1.ID()))
		require.NotEmpty(t, idx.IndexarUnoCalls)
		last := idx.IndexarUnoCalls[len(idx.IndexarUnoCalls)-1]
		assert.Equal(t, v1.ID(), last.ID)
		assert.Contains(t, last.Direccion, "CALLE REINDEXADA", "ReindexVenta must reflect the header mutation")
	})
}
