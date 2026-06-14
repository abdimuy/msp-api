//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func buildAnclas(n int) []outbound.AnclaCliente {
	out := make([]outbound.AnclaCliente, n)
	for i := range out {
		out[i] = outbound.AnclaCliente{
			ClienteID:         i + 1000,
			Nombre:            "Cliente",
			Zona:              "Z1",
			Telefono:          "555-0001",
			FechaUltimaCompra: testNow.AddDate(0, 0, -400),
			Frecuencia:        3,
			Monetary:          decimal.NewFromInt(10_000),
			Saldo:             decimal.NewFromInt(500),
			PorLiquidarPct:    decimal.NewFromFloat(20.0),
		}
	}
	return out
}

// ─── Full refresh ─────────────────────────────────────────────────────────────

func TestRefrescarCandidatos_Full_SinceIsNil(t *testing.T) {
	t.Parallel()

	anclas := buildAnclas(3)
	micro := &fakeMicrosipReader{anclas: anclas}
	repo := newFakeWinbackRepo()
	svc := app.NewService(repo, micro, fixedClock{testNow}, nil)

	result, err := svc.RefrescarCandidatos(context.Background(), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Full refresh always passes since=nil.
	if micro.since != nil {
		t.Errorf("full refresh: expected since=nil, got %v", micro.since)
	}
	if result.Procesados != 3 {
		t.Errorf("Procesados: got %d, want 3", result.Procesados)
	}
	if !result.Watermark.Equal(testNow) {
		t.Errorf("Watermark: got %v, want %v", result.Watermark, testNow)
	}
}

// ─── Incremental — first run (ErrRefreshStateNotFound) ───────────────────────

func TestRefrescarCandidatos_Incremental_FirstRun_SinceNil(t *testing.T) {
	t.Parallel()

	anclas := buildAnclas(2)
	micro := &fakeMicrosipReader{anclas: anclas}
	repo := newFakeWinbackRepo() // no state seeded → returns ErrRefreshStateNotFound
	svc := app.NewService(repo, micro, fixedClock{testNow}, nil)

	_, err := svc.RefrescarCandidatos(context.Background(), false)
	if err != nil {
		t.Fatalf("unexpected error on first incremental run: %v", err)
	}

	// First incremental run → since must be nil (treated as full read).
	if micro.since != nil {
		t.Errorf("first incremental run: expected since=nil, got %v", micro.since)
	}
}

// ─── Incremental — with existing state ───────────────────────────────────────

func TestRefrescarCandidatos_Incremental_WithState_SinceFromWatermark(t *testing.T) {
	t.Parallel()

	prevWatermark := testNow.AddDate(0, 0, -7)

	anclas := buildAnclas(1)
	micro := &fakeMicrosipReader{anclas: anclas}
	repo := newFakeWinbackRepo()
	// Seed an existing incremental state.
	repo.refreshStateBy["winback_incr"] = outbound.RefreshState{
		Job:           "winback_incr",
		LastWatermark: &prevWatermark,
		LastRunAt:     prevWatermark,
	}

	svc := app.NewService(repo, micro, fixedClock{testNow}, nil)

	_, err := svc.RefrescarCandidatos(context.Background(), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if micro.since == nil {
		t.Fatal("incremental run with state: expected since=&prevWatermark, got nil")
	}
	if !micro.since.Equal(prevWatermark) {
		t.Errorf("since: got %v, want %v", micro.since, prevWatermark)
	}
}

// ─── SaveRefreshState called with watermark=now and correct job ───────────────

func TestRefrescarCandidatos_SaveRefreshState_WatermarkAndJob(t *testing.T) {
	t.Parallel()

	micro := &fakeMicrosipReader{anclas: buildAnclas(1)}
	repo := newFakeWinbackRepo()
	svc := app.NewService(repo, micro, fixedClock{testNow}, nil)

	_, err := svc.RefrescarCandidatos(context.Background(), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.savedState == nil {
		t.Fatal("SaveRefreshState was not called")
	}
	if repo.savedState.Job != "winback_full" {
		t.Errorf("job: got %q, want %q", repo.savedState.Job, "winback_full")
	}
	if repo.savedState.LastWatermark == nil {
		t.Fatal("LastWatermark is nil")
	}
	if !repo.savedState.LastWatermark.Equal(testNow) {
		t.Errorf("LastWatermark: got %v, want %v", repo.savedState.LastWatermark, testNow)
	}
	if !repo.savedState.LastRunAt.Equal(testNow) {
		t.Errorf("LastRunAt: got %v, want %v", repo.savedState.LastRunAt, testNow)
	}
}

// ─── UpsertCandidatos called BEFORE SaveRefreshState ─────────────────────────

func TestRefrescarCandidatos_CallOrder_UpsertBeforeSave(t *testing.T) {
	t.Parallel()

	micro := &fakeMicrosipReader{anclas: buildAnclas(1)}
	repo := newFakeWinbackRepo()
	svc := app.NewService(repo, micro, fixedClock{testNow}, nil)

	_, err := svc.RefrescarCandidatos(context.Background(), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.callOrder) < 2 {
		t.Fatalf("expected at least 2 recorded calls, got %v", repo.callOrder)
	}
	if repo.callOrder[0] != "upsert" {
		t.Errorf("first call: got %q, want %q", repo.callOrder[0], "upsert")
	}
	if repo.callOrder[1] != "save_state" {
		t.Errorf("second call: got %q, want %q", repo.callOrder[1], "save_state")
	}
}

// ─── Existing clients preserve their en_control flag ──────────────────────────

func TestRefrescarCandidatos_ExistingClients_PreserveEnControl(t *testing.T) {
	t.Parallel()

	// clienteID 1000 is in the control group in the DB.
	existingControl := true
	// deterministicControl(1000) may or may not be true — we don't care.
	// The important thing is that we preserve the STORED flag, not the hash.

	anclas := []outbound.AnclaCliente{
		{
			ClienteID: 1000, Nombre: "Existente", Zona: "Z1", Telefono: "555",
			FechaUltimaCompra: testNow.AddDate(0, 0, -400),
			Frecuencia:        3, Monetary: decimal.NewFromInt(10_000),
		},
	}
	micro := &fakeMicrosipReader{anclas: anclas}
	repo := newFakeWinbackRepo()
	repo.controlFlags[1000] = existingControl // stored: true (control group)

	svc := app.NewService(repo, micro, fixedClock{testNow}, nil)

	_, err := svc.RefrescarCandidatos(context.Background(), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.upserted) != 1 {
		t.Fatalf("expected 1 upserted candidato, got %d", len(repo.upserted))
	}
	if repo.upserted[0].EnControl() != existingControl {
		t.Errorf("en_control: got %v, want %v (existing flag must be preserved)",
			repo.upserted[0].EnControl(), existingControl)
	}
}

// ─── New clients get deterministic control assignment ─────────────────────────

func TestRefrescarCandidatos_NewClients_DeterministicControl(t *testing.T) {
	t.Parallel()

	// Run twice with the same ancla — en_control must be identical both times.
	anclas := []outbound.AnclaCliente{
		{
			ClienteID: 42, Nombre: "Nuevo", Zona: "Z1", Telefono: "555",
			FechaUltimaCompra: testNow.AddDate(0, 0, -400),
			Frecuencia:        3, Monetary: decimal.NewFromInt(10_000),
		},
	}

	run := func() bool {
		micro := &fakeMicrosipReader{anclas: anclas}
		repo := newFakeWinbackRepo() // no existing flags → new client
		svc := app.NewService(repo, micro, fixedClock{testNow}, nil)
		if _, err := svc.RefrescarCandidatos(context.Background(), true); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return repo.upserted[0].EnControl()
	}

	flag1 := run()
	flag2 := run()
	if flag1 != flag2 {
		t.Errorf("deterministic control: got different results across runs (%v != %v)", flag1, flag2)
	}
	// Cross-check via the exported deterministic function.
	if flag1 != app.ExportDeterministicControl(42) {
		t.Errorf("en_control mismatch with deterministicControl(42): service=%v, fn=%v",
			flag1, app.ExportDeterministicControl(42))
	}
}

// ─── deterministicControl — determinism and ~15% rate ─────────────────────────

func TestDeterministicControl_Determinism(t *testing.T) {
	t.Parallel()

	for _, id := range []int{0, 1, 42, 999, 100_000} {
		a := app.ExportDeterministicControl(id)
		b := app.ExportDeterministicControl(id)
		if a != b {
			t.Errorf("deterministicControl(%d) not deterministic: %v != %v", id, a, b)
		}
	}
}

func TestDeterministicControl_ApproxRate(t *testing.T) {
	t.Parallel()

	const n = 10_000
	count := 0
	for i := range n {
		if app.ExportDeterministicControl(i) {
			count++
		}
	}
	// Expect roughly 15% ± 3% (i.e. 12-18% of 10000 = 1200–1800).
	if count < 1_200 || count > 1_800 {
		t.Errorf("deterministicControl rate out of expected range [12%%, 18%%]: %d/%d = %.1f%%",
			count, n, float64(count)/float64(n)*100)
	}
}

// ─── Incremental job name is "winback_incr" ───────────────────────────────────

func TestRefrescarCandidatos_Incremental_JobName(t *testing.T) {
	t.Parallel()

	micro := &fakeMicrosipReader{anclas: buildAnclas(1)}
	repo := newFakeWinbackRepo()
	svc := app.NewService(repo, micro, fixedClock{testNow}, nil)

	_, err := svc.RefrescarCandidatos(context.Background(), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.savedState == nil {
		t.Fatal("SaveRefreshState was not called")
	}
	if repo.savedState.Job != "winback_incr" {
		t.Errorf("incremental job name: got %q, want %q", repo.savedState.Job, "winback_incr")
	}
}
