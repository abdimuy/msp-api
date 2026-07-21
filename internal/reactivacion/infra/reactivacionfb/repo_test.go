// Package reactivacionfb_test contains Firebird-backed integration tests for the
// reactivación infra layer. All writes execute inside a transaction that always
// rolls back so the shared dev DB never accumulates test data.
//
// Prerequisites:
//   - FB_DATABASE env var pointing at the dev Microsip Firebird DB.
//   - Migration 000044 applied (creates MSP_RX_COHORTE). The upsert/list tests
//     skip cleanly when the table is absent.
//
// Run: FB_DATABASE=/firebird/data/MUEBLERA.FDB go test ./internal/reactivacion/infra/reactivacionfb/...
//
//nolint:misspell // Spanish vocabulary (cohorte, segmento) by convention.
package reactivacionfb_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionfb"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

func requireFBEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird integration tests")
	}
}

var (
	fixedNow          = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	fixedCohorte      = time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	fixedUltimaCompra = time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
)

// makeCohorte builds a CohorteCliente with a large positive synthetic clienteID
// (real Microsip IDs are far smaller, and MSP_RX_COHORTE starts empty) so tests
// never collide with production rows.
func makeCohorte(t *testing.T, clienteID int, seg domain.Segmento, saldo string, enControl, contactado bool) *domain.CohorteCliente {
	t.Helper()
	c, err := domain.CrearCohorteCliente(domain.CrearCohorteClienteParams{
		ClienteID:             clienteID,
		Nombre:                "Cliente Prueba Ñandú",
		Telefono:              "238 100 2000",
		Segmento:              seg,
		EnControl:             enControl,
		FueContactado:         contactado,
		CohorteFecha:          fixedCohorte,
		FechaUltimaCompraBase: fixedUltimaCompra,
		Saldo:                 decimal.RequireFromString(saldo),
		PorLiquidarPct:        decimal.RequireFromString("12.50"),
		Now:                   fixedNow,
	})
	require.NoError(t, err)
	return c
}

func findByID(cohorte []*domain.CohorteCliente, clienteID int) *domain.CohorteCliente {
	for _, c := range cohorte {
		if c.ClienteID() == clienteID {
			return c
		}
	}
	return nil
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestRepo_UpsertAndList_RoundTrip(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewRepo(pool)

		const clienteID = 900000001
		c := makeCohorte(t, clienteID, domain.SegmentoPorLiquidarHueco, "1200.50", true, false)

		if err := repo.UpsertCohorte(ctx, []*domain.CohorteCliente{c}); err != nil {
			t.Skipf("UpsertCohorte failed — migration 000044 may not be applied: %v", err)
		}

		cohorte, err := repo.ListarCohorte(ctx, outbound.ListarCohorteParams{})
		require.NoError(t, err)

		got := findByID(cohorte, clienteID)
		require.NotNil(t, got, "inserted cohorte cliente must appear in ListarCohorte")

		assert.Equal(t, c.ID(), got.ID())
		assert.Equal(t, clienteID, got.ClienteID())
		assert.Equal(t, "Cliente Prueba Ñandú", got.Nombre(), "UTF-8 name must round-trip")
		assert.Equal(t, "238 100 2000", got.Telefono())
		assert.Equal(t, domain.SegmentoPorLiquidarHueco, got.Segmento())
		assert.True(t, got.EnControl())
		assert.False(t, got.FueContactado())
		assert.True(t, decimal.RequireFromString("1200.50").Equal(got.Saldo()))
		assert.True(t, decimal.RequireFromString("12.50").Equal(got.PorLiquidarPct()))
		assert.WithinDuration(t, fixedCohorte, got.CohorteFecha(), time.Second)
		assert.WithinDuration(t, fixedUltimaCompra, got.FechaUltimaCompraBase(), time.Second)
		assert.WithinDuration(t, fixedNow, got.CreatedAt(), time.Second)
	})
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestRepo_Upsert_PreservesFlagsAndCohorte(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewRepo(pool)

		const clienteID = 900000002
		// First insert: control=true, contactado=true, cohorte=fixedCohorte.
		c1 := makeCohorte(t, clienteID, domain.SegmentoRecienLiquidado, "0.00", true, true)
		if err := repo.UpsertCohorte(ctx, []*domain.CohorteCliente{c1}); err != nil {
			t.Skipf("UpsertCohorte failed — migration 000044 may not be applied: %v", err)
		}

		// Second upsert: different flags + cohort date + mutable fields.
		differentCohorte := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		c2, err := domain.CrearCohorteCliente(domain.CrearCohorteClienteParams{
			ClienteID:             clienteID,
			Nombre:                "Cliente Actualizado",
			Telefono:              "238 999 9999",
			Segmento:              domain.SegmentoPorLiquidarHueco, // mutable — must change
			EnControl:             false,                           // must NOT be saved
			FueContactado:         false,                           // must NOT be saved
			CohorteFecha:          differentCohorte,                // must NOT be saved
			FechaUltimaCompraBase: time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC),
			Saldo:                 decimal.RequireFromString("300.00"),
			PorLiquidarPct:        decimal.RequireFromString("5.00"),
			Now:                   fixedNow.Add(time.Hour),
		})
		require.NoError(t, err)
		require.NoError(t, repo.UpsertCohorte(ctx, []*domain.CohorteCliente{c2}))

		cohorte, err := repo.ListarCohorte(ctx, outbound.ListarCohorteParams{})
		require.NoError(t, err)
		got := findByID(cohorte, clienteID)
		require.NotNil(t, got)

		// Preserved (frozen) fields.
		assert.True(t, got.EnControl(), "EN_CONTROL must be preserved from first INSERT")
		assert.True(t, got.FueContactado(), "FUE_CONTACTADO must be preserved from first INSERT")
		assert.WithinDuration(t, fixedCohorte, got.CohorteFecha(), time.Second,
			"COHORTE_FECHA must be preserved from first INSERT")

		// Mutable fields must reflect the second upsert.
		assert.Equal(t, "Cliente Actualizado", got.Nombre())
		assert.Equal(t, domain.SegmentoPorLiquidarHueco, got.Segmento())
		assert.True(t, decimal.RequireFromString("300.00").Equal(got.Saldo()))
	})
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestRepo_ExistingFlags(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewRepo(pool)

		rows := []*domain.CohorteCliente{
			makeCohorte(t, 900000010, domain.SegmentoRecienLiquidado, "0.00", true, false),
			makeCohorte(t, 900000011, domain.SegmentoRecienLiquidado, "0.00", false, true),
		}
		if err := repo.UpsertCohorte(ctx, rows); err != nil {
			t.Skipf("UpsertCohorte failed — migration 000044 may not be applied: %v", err)
		}

		control, err := repo.ExistingControlFlags(ctx)
		require.NoError(t, err)
		assert.True(t, control[900000010])
		assert.False(t, control[900000011])

		contactado, err := repo.ExistingContactadoFlags(ctx)
		require.NoError(t, err)
		assert.False(t, contactado[900000010])
		assert.True(t, contactado[900000011])
	})
}

//nolint:paralleltest // serial: shares rollback-only tx.
func TestRepo_ListarCohorte_Filters(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := reactivacionfb.NewRepo(pool)

		rows := []*domain.CohorteCliente{
			makeCohorte(t, 900000020, domain.SegmentoRecienLiquidado, "0.00", true, false),     // control
			makeCohorte(t, 900000021, domain.SegmentoRecienLiquidado, "0.00", false, false),    // treatment
			makeCohorte(t, 900000022, domain.SegmentoPorLiquidarHueco, "500.00", false, false), // treatment, other seg
		}
		if err := repo.UpsertCohorte(ctx, rows); err != nil {
			t.Skipf("UpsertCohorte failed — migration 000044 may not be applied: %v", err)
		}

		// Segmento filter.
		recien, err := repo.ListarCohorte(ctx, outbound.ListarCohorteParams{Segmento: domain.SegmentoRecienLiquidado})
		require.NoError(t, err)
		assert.NotNil(t, findByID(recien, 900000020))
		assert.NotNil(t, findByID(recien, 900000021))
		assert.Nil(t, findByID(recien, 900000022), "por_liquidar_hueco must be filtered out")

		// SoloTratamiento filter (excludes control).
		tratamiento, err := repo.ListarCohorte(ctx, outbound.ListarCohorteParams{SoloTratamiento: true})
		require.NoError(t, err)
		assert.Nil(t, findByID(tratamiento, 900000020), "control cliente must be excluded")
		assert.NotNil(t, findByID(tratamiento, 900000021))
		assert.NotNil(t, findByID(tratamiento, 900000022))
	})
}

// TestRepo_LeerUniversoTehuacan is read-only against the live dev Microsip DB.
// It asserts the query returns tratable Tehuacán clientes with both segmentos and
// valid invariants (phone present, segmento canonical, saldo >= 0).
//
//nolint:paralleltest // serial: shares the test pool.
func TestRepo_LeerUniversoTehuacan(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	repo := reactivacionfb.NewRepo(pool)
	universo, err := repo.LeerUniversoTehuacan(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, universo, "the Tehuacán tratable universe must not be empty in the dev clone")

	segCounts := map[domain.Segmento]int{}
	for _, u := range universo {
		assert.Positive(t, u.ClienteID)
		assert.True(t, u.Segmento.Valido(), "segmento must be canonical: %q", u.Segmento)
		assert.NotEmpty(t, u.Telefono, "universe rows must carry a phone")
		assert.False(t, u.Saldo.IsNegative(), "saldo must be >= 0")
		if u.Segmento == domain.SegmentoRecienLiquidado {
			assert.True(t, u.Saldo.IsZero(), "recien_liquidado implies saldo 0")
		}
		segCounts[u.Segmento]++
	}
	t.Logf("universo tehuacán: total=%d recien_liquidado=%d por_liquidar_hueco=%d",
		len(universo), segCounts[domain.SegmentoRecienLiquidado], segCounts[domain.SegmentoPorLiquidarHueco])
	assert.Positive(t, segCounts[domain.SegmentoRecienLiquidado], "recien_liquidado segment expected in dev")
	assert.Positive(t, segCounts[domain.SegmentoPorLiquidarHueco], "por_liquidar_hueco segment expected in dev")
	// Live-verified: 6,721 clientes with phone (6,270 recien + 451 hueco after tiebreak).
	// Assert a plausible floor so a semantic regression (e.g. a per-SUM revert) trips.
	assert.Greater(t, len(universo), 5000, "tratable universe with phone should be in the thousands")
}
