// Package app_test — cartera_rollrate_test.go exercises ObtenerRollRate and
// MaterializarCarteraSnapshot using hand-written in-memory fakes.
//
//nolint:misspell // Spanish domain vocabulary per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
)

// ─── ObtenerRollRate ──────────────────────────────────────────────────────────

// TestObtenerRollRate_TwoCuts_CorrectScalar verifies the exact roll-rate scalar
// produced by two snapshot cuts with a known bucket distribution.
//
// Cut 1 (anterior): 100% balance in 0-30 bucket → weighted avg ordinal = 0.0
// Cut 2 (reciente): 100% balance in 90+  bucket → weighted avg ordinal = 3.0
//
// Expected RollRate = (3.0 − 0.0) / 3 = +1.0 (maximum deterioration).
func TestObtenerRollRate_TwoCuts_CorrectScalar(t *testing.T) {
	t.Parallel()

	fechaAnterior := time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC)
	fechaReciente := time.Date(2026, 6, 1, 3, 0, 0, 0, time.UTC)

	// Build snapshots via HydrateCarteraSnapshot (repo hydration path).
	prevSnap := domain.HydrateCarteraSnapshot(domain.HydrateCarteraSnapshotParams{
		ID:            mustUUID(),
		FechaCorte:    fechaAnterior,
		ZonaClienteID: 1,
		Bucket:        domain.BucketAgingDias0_30, // ordinal 0
		Saldo:         decimal.NewFromInt(1000),
		Conteo:        10,
		CreatedAt:     fechaAnterior,
		UpdatedAt:     fechaAnterior,
	})
	currSnap := domain.HydrateCarteraSnapshot(domain.HydrateCarteraSnapshotParams{
		ID:            mustUUID(),
		FechaCorte:    fechaReciente,
		ZonaClienteID: 1,
		Bucket:        domain.BucketAgingDias90Plus, // ordinal 3
		Saldo:         decimal.NewFromInt(1000),
		Conteo:        10,
		CreatedAt:     fechaReciente,
		UpdatedAt:     fechaReciente,
	})

	// ListRecentSnapshots returns FECHA_CORTE DESC — most recent first.
	cartera := &fakeCarteraRepo{
		snapshots: []domain.CarteraSnapshot{*currSnap, *prevSnap},
	}
	svc := newCarteraService(newFakeWinbackRepo(), cartera)

	result, err := svc.ObtenerRollRate(context.Background())
	require.NoError(t, err)

	assert.True(t, result.Disponible, "must be disponible with 2 distinct cuts")
	assert.InDelta(t, 1.0, result.RollRate, 1e-9, "roll-rate should be +1.0 (max deterioration)")
	assert.True(t, result.FechaCorteAnterior.Equal(fechaAnterior),
		"FechaCorteAnterior should be the older cut")
	assert.True(t, result.FechaCorteReciente.Equal(fechaReciente),
		"FechaCorteReciente should be the newer cut")
}

// TestObtenerRollRate_TwoCuts_MultiZone verifies that saldos from multiple zones
// are correctly aggregated before computing the roll-rate.
//
// Cut anterior: Zona 1 → 0-30 saldo=500; Zona 2 → 0-30 saldo=500.
//
//	Total: 0-30=1000 → weighted avg = 0.0
//
// Cut reciente: Zona 1 → 31-60 saldo=500; Zona 2 → 31-60 saldo=500.
//
//	Total: 31-60=1000 → weighted avg = 1.0
//
// Expected RollRate = (1.0 − 0.0) / 3 ≈ +0.333.
func TestObtenerRollRate_TwoCuts_MultiZone(t *testing.T) {
	t.Parallel()

	fechaAnterior := time.Date(2026, 4, 1, 3, 0, 0, 0, time.UTC)
	fechaReciente := time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC)

	snapshots := []domain.CarteraSnapshot{
		// Most-recent cut (reciente) — returned first by ListRecentSnapshots.
		*domain.HydrateCarteraSnapshot(domain.HydrateCarteraSnapshotParams{
			ID:            mustUUID(),
			FechaCorte:    fechaReciente,
			ZonaClienteID: 1,
			Bucket:        domain.BucketAgingDias31_60,
			Saldo:         decimal.NewFromInt(500),
			Conteo:        5,
			CreatedAt:     fechaReciente,
			UpdatedAt:     fechaReciente,
		}),
		*domain.HydrateCarteraSnapshot(domain.HydrateCarteraSnapshotParams{
			ID:            mustUUID(),
			FechaCorte:    fechaReciente,
			ZonaClienteID: 2,
			Bucket:        domain.BucketAgingDias31_60,
			Saldo:         decimal.NewFromInt(500),
			Conteo:        5,
			CreatedAt:     fechaReciente,
			UpdatedAt:     fechaReciente,
		}),
		// Anterior cut — older.
		*domain.HydrateCarteraSnapshot(domain.HydrateCarteraSnapshotParams{
			ID:            mustUUID(),
			FechaCorte:    fechaAnterior,
			ZonaClienteID: 1,
			Bucket:        domain.BucketAgingDias0_30,
			Saldo:         decimal.NewFromInt(500),
			Conteo:        5,
			CreatedAt:     fechaAnterior,
			UpdatedAt:     fechaAnterior,
		}),
		*domain.HydrateCarteraSnapshot(domain.HydrateCarteraSnapshotParams{
			ID:            mustUUID(),
			FechaCorte:    fechaAnterior,
			ZonaClienteID: 2,
			Bucket:        domain.BucketAgingDias0_30,
			Saldo:         decimal.NewFromInt(500),
			Conteo:        5,
			CreatedAt:     fechaAnterior,
			UpdatedAt:     fechaAnterior,
		}),
	}

	cartera := &fakeCarteraRepo{snapshots: snapshots}
	svc := newCarteraService(newFakeWinbackRepo(), cartera)

	result, err := svc.ObtenerRollRate(context.Background())
	require.NoError(t, err)

	assert.True(t, result.Disponible)
	// prev weighted avg = (0×1000)/1000 = 0; curr weighted avg = (1×1000)/1000 = 1
	// RollRate = (1 - 0) / 3 ≈ 0.3333
	assert.InDelta(t, 1.0/3.0, result.RollRate, 1e-9,
		"multi-zone aggregate: one-bucket deterioration → RollRate ≈ 0.333")
}

// TestObtenerRollRate_TwoCuts_Improvement verifies a negative roll-rate
// (portfolio improved between cuts).
//
// Cut anterior: 100% balance in 90+ bucket → weighted avg = 3.0
// Cut reciente: 100% balance in 0-30 bucket → weighted avg = 0.0
//
// Expected RollRate = (0.0 − 3.0) / 3 = −1.0 (maximum improvement).
func TestObtenerRollRate_TwoCuts_Improvement(t *testing.T) {
	t.Parallel()

	fechaAnterior := time.Date(2026, 3, 1, 3, 0, 0, 0, time.UTC)
	fechaReciente := time.Date(2026, 4, 1, 3, 0, 0, 0, time.UTC)

	snapshots := []domain.CarteraSnapshot{
		*domain.HydrateCarteraSnapshot(domain.HydrateCarteraSnapshotParams{
			ID:            mustUUID(),
			FechaCorte:    fechaReciente,
			ZonaClienteID: 1,
			Bucket:        domain.BucketAgingDias0_30, // ordinal 0
			Saldo:         decimal.NewFromInt(1000),
			Conteo:        10,
			CreatedAt:     fechaReciente,
			UpdatedAt:     fechaReciente,
		}),
		*domain.HydrateCarteraSnapshot(domain.HydrateCarteraSnapshotParams{
			ID:            mustUUID(),
			FechaCorte:    fechaAnterior,
			ZonaClienteID: 1,
			Bucket:        domain.BucketAgingDias90Plus, // ordinal 3
			Saldo:         decimal.NewFromInt(1000),
			Conteo:        10,
			CreatedAt:     fechaAnterior,
			UpdatedAt:     fechaAnterior,
		}),
	}

	cartera := &fakeCarteraRepo{snapshots: snapshots}
	svc := newCarteraService(newFakeWinbackRepo(), cartera)

	result, err := svc.ObtenerRollRate(context.Background())
	require.NoError(t, err)

	assert.True(t, result.Disponible)
	assert.InDelta(t, -1.0, result.RollRate, 1e-9, "portfolio improved → RollRate = -1.0")
}

// TestObtenerRollRate_OneCut_Indisponible verifies that a single snapshot cut
// returns Disponible=false (acumulando datos) without an error.
func TestObtenerRollRate_OneCut_Indisponible(t *testing.T) {
	t.Parallel()

	fecha := time.Date(2026, 6, 1, 3, 0, 0, 0, time.UTC)
	snap := domain.HydrateCarteraSnapshot(domain.HydrateCarteraSnapshotParams{
		ID:            mustUUID(),
		FechaCorte:    fecha,
		ZonaClienteID: 1,
		Bucket:        domain.BucketAgingDias0_30,
		Saldo:         decimal.NewFromInt(500),
		Conteo:        5,
		CreatedAt:     fecha,
		UpdatedAt:     fecha,
	})

	cartera := &fakeCarteraRepo{snapshots: []domain.CarteraSnapshot{*snap}}
	svc := newCarteraService(newFakeWinbackRepo(), cartera)

	result, err := svc.ObtenerRollRate(context.Background())
	require.NoError(t, err, "single cut must not error")

	assert.False(t, result.Disponible, "single cut → Disponible=false (acumulando datos)")
	assert.InDelta(t, 0.0, result.RollRate, 0, "RollRate must be zero when not disponible")
	assert.True(t, result.FechaCorteAnterior.IsZero(), "FechaCorteAnterior must be zero")
	assert.True(t, result.FechaCorteReciente.IsZero(), "FechaCorteReciente must be zero")
}

// TestObtenerRollRate_ZeroCuts_Indisponible verifies that an empty snapshot
// store returns Disponible=false without an error.
func TestObtenerRollRate_ZeroCuts_Indisponible(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{} // no snapshots
	svc := newCarteraService(newFakeWinbackRepo(), cartera)

	result, err := svc.ObtenerRollRate(context.Background())
	require.NoError(t, err, "zero cuts must not error")

	assert.False(t, result.Disponible, "zero cuts → Disponible=false")
}

// TestObtenerRollRate_NilRepo_Error verifies that a nil carteraRepo returns
// an internal error.
func TestObtenerRollRate_NilRepo_Error(t *testing.T) {
	t.Parallel()

	svc := app.NewService(newFakeWinbackRepo(), nil, fixedClock{testNow}, nil)
	// carteraRepo NOT configured.

	_, err := svc.ObtenerRollRate(context.Background())
	require.Error(t, err, "nil carteraRepo must return an error")
}

// TestObtenerRollRate_RepoError_Propagated verifies that a ListRecentSnapshots
// failure is returned as an error.
func TestObtenerRollRate_RepoError_Propagated(t *testing.T) {
	t.Parallel()

	cartera := &fakeCarteraRepo{snapshotErr: errors.New("db timeout")}
	svc := newCarteraService(newFakeWinbackRepo(), cartera)

	_, err := svc.ObtenerRollRate(context.Background())
	require.Error(t, err, "ListRecentSnapshots failure must be propagated")
}

// ─── MaterializarCarteraSnapshot ──────────────────────────────────────────────

// TestMaterializarCarteraSnapshot_PersistsRows verifies that valid aging rows
// produce snapshot rows that are persisted via SaveCarteraSnapshot, and that
// SaveRefreshState("cartera_snapshot") is recorded.
func TestMaterializarCarteraSnapshot_PersistsRows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 20, 3, 0, 0, 0, time.UTC)

	cartera := &fakeCarteraRepo{
		agingByZona: []outbound.AgingRow{
			{ZonaClienteID: 1, Bucket: domain.BucketAgingDias0_30, Saldo: decimal.NewFromInt(800), Conteo: 8},
			{ZonaClienteID: 1, Bucket: domain.BucketAgingDias31_60, Saldo: decimal.NewFromInt(200), Conteo: 2},
			{ZonaClienteID: 2, Bucket: domain.BucketAgingDias0_30, Saldo: decimal.NewFromInt(300), Conteo: 3},
		},
	}
	wb := newFakeWinbackRepo()
	svc := app.NewService(wb, nil, fixedClock{now}, nil)
	svc.WithCarteraRepo(cartera)

	svc.MaterializarCarteraSnapshot(context.Background(), now)

	// Verify snapshot rows persisted.
	require.Len(t, cartera.snapshots, 3, "one snapshot row per aging row")

	// Verify each row's FechaCorte.
	for _, snap := range cartera.snapshots {
		assert.True(t, snap.FechaCorte().Equal(now.UTC()), "FechaCorte must match now")
	}

	// Verify SaveRefreshState("cartera_snapshot") was saved.
	wb.mu.Lock()
	st, hasState := wb.refreshStateBy["cartera_snapshot"]
	wb.mu.Unlock()
	assert.True(t, hasState, "cartera_snapshot refresh state must be recorded")
	assert.True(t, st.LastRunAt.Equal(now), "LastRunAt must equal now")
}

// TestMaterializarCarteraSnapshot_SaveFailure_NoState verifies that when
// SaveCarteraSnapshot returns an error:
//   - The method returns without panicking (tick must not abort).
//   - SaveRefreshState("cartera_snapshot") is NOT called (incomplete run).
func TestMaterializarCarteraSnapshot_SaveFailure_NoState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 20, 3, 0, 0, 0, time.UTC)
	cartera := &fakeCarteraRepo{
		agingByZona: []outbound.AgingRow{
			{ZonaClienteID: 1, Bucket: domain.BucketAgingDias0_30, Saldo: decimal.NewFromInt(100), Conteo: 1},
		},
		snapshotErr: errors.New("db write failed"),
	}
	wb := newFakeWinbackRepo()
	svc := app.NewService(wb, nil, fixedClock{now}, nil)
	svc.WithCarteraRepo(cartera)

	// Must not panic or crash — tick must continue.
	svc.MaterializarCarteraSnapshot(context.Background(), now)

	// No state should be saved (SaveCarteraSnapshot failed).
	wb.mu.Lock()
	_, hasState := wb.refreshStateBy["cartera_snapshot"]
	wb.mu.Unlock()
	assert.False(t, hasState, "state must NOT be saved when SaveCarteraSnapshot fails")
}

// TestMaterializarCarteraSnapshot_EmptyPortfolio_SavesState verifies that an
// empty portfolio (no aging rows) still records a "cartera_snapshot" state
// (valid run, nothing to persist).
func TestMaterializarCarteraSnapshot_EmptyPortfolio_SavesState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 20, 3, 0, 0, 0, time.UTC)
	cartera := &fakeCarteraRepo{agingByZona: nil} // empty
	wb := newFakeWinbackRepo()
	svc := app.NewService(wb, nil, fixedClock{now}, nil)
	svc.WithCarteraRepo(cartera)

	svc.MaterializarCarteraSnapshot(context.Background(), now)

	// No snapshot rows.
	assert.Empty(t, cartera.snapshots, "no rows saved for empty portfolio")

	// State must still be saved (job ran, just found no data).
	wb.mu.Lock()
	_, hasState := wb.refreshStateBy["cartera_snapshot"]
	wb.mu.Unlock()
	assert.True(t, hasState, "cartera_snapshot state must be recorded even for empty portfolio")
}

// TestMaterializarCarteraSnapshot_NilRepo_NoOp verifies that a nil carteraRepo
// does not panic — the method returns early with a warning log.
func TestMaterializarCarteraSnapshot_NilRepo_NoOp(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 20, 3, 0, 0, 0, time.UTC)
	wb := newFakeWinbackRepo()
	svc := app.NewService(wb, nil, fixedClock{now}, nil)
	// carteraRepo NOT configured.

	// Must not panic.
	svc.MaterializarCarteraSnapshot(context.Background(), now)

	wb.mu.Lock()
	_, hasState := wb.refreshStateBy["cartera_snapshot"]
	wb.mu.Unlock()
	assert.False(t, hasState, "no state saved when carteraRepo is nil")
}

// TestMaterializarCarteraSnapshot_InvalidRow_Skipped verifies that aging rows
// with invalid parameters (ZonaClienteID <= 0) are skipped with a warning and
// the remaining valid rows are still persisted.
func TestMaterializarCarteraSnapshot_InvalidRow_Skipped(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 20, 3, 0, 0, 0, time.UTC)
	cartera := &fakeCarteraRepo{
		agingByZona: []outbound.AgingRow{
			// Invalid: ZonaClienteID=0 → NewCarteraSnapshot returns error.
			{ZonaClienteID: 0, Bucket: domain.BucketAgingDias0_30, Saldo: decimal.NewFromInt(100), Conteo: 1},
			// Valid.
			{ZonaClienteID: 5, Bucket: domain.BucketAgingDias31_60, Saldo: decimal.NewFromInt(200), Conteo: 2},
		},
	}
	wb := newFakeWinbackRepo()
	svc := app.NewService(wb, nil, fixedClock{now}, nil)
	svc.WithCarteraRepo(cartera)

	svc.MaterializarCarteraSnapshot(context.Background(), now)

	// Only 1 valid row should be saved (the zona=0 row was skipped).
	require.Len(t, cartera.snapshots, 1, "invalid row must be skipped; valid row persisted")
	assert.Equal(t, 5, cartera.snapshots[0].ZonaClienteID(), "valid row zona")

	// State still saved (run completed with partial data).
	wb.mu.Lock()
	_, hasState := wb.refreshStateBy["cartera_snapshot"]
	wb.mu.Unlock()
	assert.True(t, hasState, "state must be saved even when some rows were skipped")
}

// TestMaterializarCarteraSnapshot_AgingError_NoState verifies that an
// AgingSaldosByZona failure causes the method to return early WITHOUT saving
// a refresh state row.
func TestMaterializarCarteraSnapshot_AgingError_NoState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 20, 3, 0, 0, 0, time.UTC)
	cartera := &fakeCarteraRepo{agingErr: errors.New("db read error")}
	wb := newFakeWinbackRepo()
	svc := app.NewService(wb, nil, fixedClock{now}, nil)
	svc.WithCarteraRepo(cartera)

	svc.MaterializarCarteraSnapshot(context.Background(), now)

	wb.mu.Lock()
	_, hasState := wb.refreshStateBy["cartera_snapshot"]
	wb.mu.Unlock()
	assert.False(t, hasState, "state must NOT be saved when aging fetch fails")
}

// TestMaterializarCarteraSnapshot_SaveStateFails_NoAbort verifies that a
// SaveRefreshState failure is only logged — the method must not panic or
// propagate the error (it has no return value).
//
// Evidence: the snapshot rows are already persisted before state is saved, so
// the test also confirms rows were not rolled back.
func TestMaterializarCarteraSnapshot_SaveStateFails_NoAbort(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 20, 3, 0, 0, 0, time.UTC)
	cartera := &fakeCarteraRepo{
		agingByZona: []outbound.AgingRow{
			{ZonaClienteID: 3, Bucket: domain.BucketAgingDias0_30, Saldo: decimal.NewFromInt(500), Conteo: 5},
		},
	}
	errWB := &errorSaveStateWinbackRepo{fakeWinbackRepo: newFakeWinbackRepo()}
	svc := app.NewService(errWB, nil, fixedClock{now}, nil)
	svc.WithCarteraRepo(cartera)

	// Must not panic — save_state_failed is logged, not propagated.
	svc.MaterializarCarteraSnapshot(context.Background(), now)

	// Snapshot rows were persisted before the state save failed.
	require.Len(t, cartera.snapshots, 1, "snapshot row must be saved even when state save fails")
}

// errorSaveStateWinbackRepo is a fakeWinbackRepo that always fails on
// SaveRefreshState. Used to exercise the save_state_failed log branch.
type errorSaveStateWinbackRepo struct {
	*fakeWinbackRepo
}

func (r *errorSaveStateWinbackRepo) SaveRefreshState(_ context.Context, _ outbound.RefreshState) error {
	return errors.New("forced state save error")
}

// ─── UUID helper ──────────────────────────────────────────────────────────────

// mustUUID returns a fresh random UUID for test fixtures. uuid.New() is
// goroutine-safe and guaranteed unique per call.
func mustUUID() uuid.UUID { return uuid.New() }
