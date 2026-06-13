//nolint:misspell // test vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/inventario/app"
	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

// ─── activeDirect tests ──────────────────────────────────────────────────────

func TestActiveDirect_NoTraspasos_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	ventaID := uuid.New()
	// Use a proxy via CrearTraspasoReverso which internally calls activeDirect.
	_, _, err := svc.CrearTraspasoReverso(context.Background(), ventaID, uuid.New())
	if !errors.Is(err, domain.ErrTraspasoNoEncontrado) {
		t.Errorf("expected ErrTraspasoNoEncontrado when no traspasos, got %v", err)
	}
}

func TestActiveDirect_OneActive_ReturnsIt(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	ventaID := uuid.New()
	original, _ := seedDirectTraspaso(t, svc, repo, ventaID)

	// CrearTraspasoReverso relies on activeDirect returning the one active directo.
	reversed, _, err := svc.CrearTraspasoReverso(context.Background(), ventaID, uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Swapped almacenes confirm the correct original was found.
	if reversed.AlmacenOrigen() != original.AlmacenDestino() {
		t.Errorf("almacen_origen mismatch: got %d, want %d", reversed.AlmacenOrigen(), original.AlmacenDestino())
	}
}

func TestActiveDirect_TwoActive_ReturnsMultipleError(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	ventaID := uuid.New()
	// Seed two separate directos without reversing the first.
	seedDirectTraspaso(t, svc, repo, ventaID)
	seedDirectTraspaso(t, svc, repo, ventaID)

	_, _, err := svc.CrearTraspasoReverso(context.Background(), ventaID, uuid.New())
	if !isAppError(err, "multiple_traspasos_directos") {
		t.Errorf("expected multiple_traspasos_directos, got %v", err)
	}
}

func TestActiveDirect_IgnoresAlreadyReversedDirecto(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	outbox := &fakeOutbox{}
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

	ventaID := uuid.New()
	by := uuid.New()

	// D1 → R1 (D1 is now marked reversado). D2 is fresh active.
	seedDirectTraspaso(t, svc, repo, ventaID)
	_, _, err := svc.CrearTraspasoReverso(context.Background(), ventaID, by)
	if err != nil {
		t.Fatalf("first reversal failed: %v", err)
	}
	// Seed D2.
	seedDirectTraspaso(t, svc, repo, ventaID)

	// Now only D2 is active — should succeed (not errMultiples).
	outbox.entries = nil
	_, _, err = svc.CrearTraspasoReverso(context.Background(), ventaID, by)
	if err != nil {
		t.Errorf("expected success reversing D2, got %v", err)
	}
}

// ─── ResincronizarTraspasoParaVenta tests ────────────────────────────────────

func baseResyncParams(ventaID uuid.UUID) app.CrearTraspasoParaVentaParams {
	return app.CrearTraspasoParaVentaParams{
		VentaID:        ventaID,
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "resync",
		Detalles:       []app.CrearTraspasoDetalleInput{{ArticuloID: 100, Cantidad: decimal.NewFromInt(3)}},
		CreatedBy:      uuid.New(),
	}
}

func countActiveDirectos(t *testing.T, repo *fakeTraspasoRepo, ventaID uuid.UUID) int {
	t.Helper()
	all, err := repo.ListByVentaID(context.Background(), ventaID)
	if err != nil {
		t.Fatalf("ListByVentaID failed: %v", err)
	}
	n := 0
	for _, tr := range all {
		if !tr.TipoReverso() && !tr.Reversado() {
			n++
		}
	}
	return n
}

func TestResincronizar_NoActive_NonEmptyDetalles_CreatesDirecto(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	outbox := &fakeOutbox{}
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

	ventaID := uuid.New()
	p := baseResyncParams(ventaID)

	tr, doctoInID, err := svc.ResincronizarTraspasoParaVenta(context.Background(), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr == nil {
		t.Fatal("expected a new directo, got nil")
	}
	if doctoInID < 1 {
		t.Errorf("expected positive doctoInID, got %d", doctoInID)
	}
	if tr.TipoReverso() {
		t.Error("new directo should NOT be a reverso")
	}
	if tr.Reversado() {
		t.Error("new directo should NOT be reversado")
	}
	// Exactly 1 Save call (the new directo; no reverso).
	if repo.SaveCalls != 1 {
		t.Errorf("expected 1 Save call, got %d", repo.SaveCalls)
	}
	if n := countActiveDirectos(t, repo, ventaID); n != 1 {
		t.Errorf("expected 1 active directo, got %d", n)
	}
	// Outbox should have received TraspasoCreadoEvent.
	if len(outbox.entries) == 0 {
		t.Error("expected at least one outbox event")
	}
}

func TestResincronizar_ActivePresent_DifferentDetalles_ReversesAndCreatesNew(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	outbox := &fakeOutbox{}
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

	ventaID := uuid.New()
	p := baseResyncParams(ventaID)
	// Create initial directo.
	if _, _, err := svc.ResincronizarTraspasoParaVenta(context.Background(), p); err != nil {
		t.Fatalf("first resync failed: %v", err)
	}
	savesBefore := repo.SaveCalls
	outbox.entries = nil

	// Change detalles.
	p2 := baseResyncParams(ventaID)
	p2.Detalles = []app.CrearTraspasoDetalleInput{{ArticuloID: 200, Cantidad: decimal.NewFromInt(5)}}

	tr2, id2, err := svc.ResincronizarTraspasoParaVenta(context.Background(), p2)
	if err != nil {
		t.Fatalf("second resync failed: %v", err)
	}
	if tr2 == nil {
		t.Fatal("expected new directo, got nil")
	}
	if id2 < 1 {
		t.Errorf("expected positive doctoInID, got %d", id2)
	}
	// Should have saved exactly 2 more: one reverso + one new directo.
	newSaves := repo.SaveCalls - savesBefore
	if newSaves != 2 {
		t.Errorf("expected 2 new Saves (reverso + directo), got %d", newSaves)
	}
	// Exactly 1 active directo after resync.
	if n := countActiveDirectos(t, repo, ventaID); n != 1 {
		t.Errorf("expected 1 active directo after resync, got %d", n)
	}
	// Two outbox events: TraspasoReversadoEvent + TraspasoCreadoEvent.
	if len(outbox.entries) < 2 {
		t.Errorf("expected at least 2 outbox entries, got %d", len(outbox.entries))
	}
}

func TestResincronizar_ActivePresent_IdenticalDetalles_Noop(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	outbox := &fakeOutbox{}
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

	ventaID := uuid.New()
	p := baseResyncParams(ventaID)
	original, origID, err := svc.ResincronizarTraspasoParaVenta(context.Background(), p)
	if err != nil {
		t.Fatalf("first resync failed: %v", err)
	}
	savesBefore := repo.SaveCalls
	outbox.entries = nil

	// Same params → no-op.
	tr2, id2, err := svc.ResincronizarTraspasoParaVenta(context.Background(), p)
	if err != nil {
		t.Fatalf("second resync (no-op) failed: %v", err)
	}
	if tr2 == nil || tr2.ID() != original.ID() {
		t.Error("expected to get back the same active traspaso on no-op")
	}
	if id2 != origID {
		t.Errorf("expected doctoInID %d, got %d", origID, id2)
	}
	// Zero new Saves.
	if repo.SaveCalls != savesBefore {
		t.Errorf("expected 0 new Saves on no-op, got %d", repo.SaveCalls-savesBefore)
	}
}

func TestResincronizar_EmptyDetalles_ActivePresent_OnlyReverse(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	outbox := &fakeOutbox{}
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

	ventaID := uuid.New()
	// Seed a directo first.
	if _, _, err := svc.ResincronizarTraspasoParaVenta(context.Background(), baseResyncParams(ventaID)); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	savesBefore := repo.SaveCalls
	outbox.entries = nil

	// Empty detalles → reverse only.
	p := baseResyncParams(ventaID)
	p.Detalles = nil

	tr, id, err := svc.ResincronizarTraspasoParaVenta(context.Background(), p)
	if err != nil {
		t.Fatalf("empty-detalles resync failed: %v", err)
	}
	if tr != nil {
		t.Errorf("expected nil new directo, got %v", tr)
	}
	if id != 0 {
		t.Errorf("expected doctoInID=0, got %d", id)
	}
	// One new Save (the reverso).
	if repo.SaveCalls-savesBefore != 1 {
		t.Errorf("expected 1 new Save (reverso only), got %d", repo.SaveCalls-savesBefore)
	}
	// Zero active directos.
	if n := countActiveDirectos(t, repo, ventaID); n != 0 {
		t.Errorf("expected 0 active directos, got %d", n)
	}
	// One outbox event (TraspasoReversadoEvent).
	found := false
	for _, e := range outbox.entries {
		if e.eventType == domain.EventTypeTraspasoReversado {
			found = true
		}
	}
	if !found {
		t.Error("expected TraspasoReversadoEvent in outbox")
	}
}

func TestResincronizar_EmptyDetalles_NoActive_Noop(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	p := baseResyncParams(uuid.New())
	p.Detalles = nil

	tr, id, err := svc.ResincronizarTraspasoParaVenta(context.Background(), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr != nil || id != 0 {
		t.Errorf("expected nil/0, got %v/%d", tr, id)
	}
	if repo.SaveCalls != 0 {
		t.Errorf("expected 0 Saves, got %d", repo.SaveCalls)
	}
}

func TestResincronizar_ThreeSequentialResyncs_ExactlyOneActiveEachTime(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	ventaID := uuid.New()

	rounds := [][]app.CrearTraspasoDetalleInput{
		{{ArticuloID: 100, Cantidad: decimal.NewFromInt(1)}},
		{{ArticuloID: 200, Cantidad: decimal.NewFromInt(2)}},
		{{ArticuloID: 300, Cantidad: decimal.NewFromInt(3)}},
	}

	for i, detalles := range rounds {
		p := app.CrearTraspasoParaVentaParams{
			VentaID:        ventaID,
			AlmacenOrigen:  1,
			AlmacenDestino: 2,
			Fecha:          fixedNow,
			Descripcion:    "resync loop",
			Detalles:       detalles,
			CreatedBy:      uuid.New(),
		}
		_, _, err := svc.ResincronizarTraspasoParaVenta(context.Background(), p)
		if err != nil {
			t.Fatalf("resync round %d failed: %v", i+1, err)
		}
		if n := countActiveDirectos(t, repo, ventaID); n != 1 {
			t.Errorf("after round %d: expected 1 active directo, got %d", i+1, n)
		}
	}
}

func TestResincronizar_FolioMinterFailsOnNewDirecto_PropagatesError(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	// Minter: first call succeeds (seed directo), second succeeds (reverso folio),
	// third fails (new directo folio).
	minter := &countingFolioMinter{failAfter: 2, err: errSentinel}
	svc := app.NewService(repo, newFakeExistenciaQuery(), minter, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	ventaID := uuid.New()
	// Seed initial directo (minter call 1).
	if _, _, err := svc.ResincronizarTraspasoParaVenta(context.Background(), baseResyncParams(ventaID)); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	// Second resync: reverso folio is call 2 (succeeds), new directo folio
	// is call 3 (fails). crearDirecto mints inside the same tx, so the
	// error propagates from the tx closure.
	p2 := baseResyncParams(ventaID)
	p2.Detalles = []app.CrearTraspasoDetalleInput{{ArticuloID: 999, Cantidad: decimal.NewFromInt(1)}}

	_, _, err := svc.ResincronizarTraspasoParaVenta(context.Background(), p2)
	if !errors.Is(err, errSentinel) {
		t.Errorf("expected sentinel error from folio minter, got %v", err)
	}
}

func TestResincronizar_SaveFailsOnNewDirecto_PropagatesError(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	ventaID := uuid.New()
	// Seed initial directo.
	if _, _, err := svc.ResincronizarTraspasoParaVenta(context.Background(), baseResyncParams(ventaID)); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	// The reverso Save happens first (call 1 = succeeds); the new-directo Save
	// is call 2 (fails). Use countingSaveFakeRepo with failAfterSave=1 so the
	// reverso is written but the new directo is not.
	repo2 := &countingSaveFakeRepo{fakeTraspasoRepo: newFakeTraspasoRepo(), failAfterSave: 1}
	// Pre-populate repo2 with the existing directo from repo.
	all, _ := repo.ListByVentaID(context.Background(), ventaID)
	for i, tr := range all {
		repo2.byID[i+1] = tr
		if tr.VentaID() != nil {
			repo2.byVentaID[*tr.VentaID()] = append(repo2.byVentaID[*tr.VentaID()], tr)
		}
		repo2.counter = i + 1
	}

	svc2 := app.NewService(repo2, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)
	p2 := baseResyncParams(ventaID)
	p2.Detalles = []app.CrearTraspasoDetalleInput{{ArticuloID: 999, Cantidad: decimal.NewFromInt(1)}}

	_, _, err := svc2.ResincronizarTraspasoParaVenta(context.Background(), p2)
	if !errors.Is(err, errSentinel) {
		t.Errorf("expected sentinel error from Save, got %v", err)
	}
}

// ─── CrearTraspasoReverso: only reverses the ACTIVE directo ─────────────────

func TestCrearTraspasoReverso_AfterEditChain_ReversesActiveOnly(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	outbox := &fakeOutbox{}
	svc := app.NewService(repo, newFakeExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

	ventaID := uuid.New()
	by := uuid.New()

	// D1 → R1 (via resync) → D2.
	if _, _, err := svc.ResincronizarTraspasoParaVenta(context.Background(), baseResyncParams(ventaID)); err != nil {
		t.Fatalf("first resync failed: %v", err)
	}
	p2 := baseResyncParams(ventaID)
	p2.Detalles = []app.CrearTraspasoDetalleInput{{ArticuloID: 200, Cantidad: decimal.NewFromInt(7)}}
	active2, _, err := svc.ResincronizarTraspasoParaVenta(context.Background(), p2)
	if err != nil {
		t.Fatalf("second resync failed: %v", err)
	}

	outbox.entries = nil
	// Now reverse D2 directly.
	reversed, _, err := svc.CrearTraspasoReverso(context.Background(), ventaID, by)
	if err != nil {
		t.Fatalf("CrearTraspasoReverso failed: %v", err)
	}
	// The reverso should mirror D2's almacenes (swapped).
	if reversed.AlmacenOrigen() != active2.AlmacenDestino() {
		t.Errorf("reverso almacen_origen=%d, want %d", reversed.AlmacenOrigen(), active2.AlmacenDestino())
	}
	// After reversal, no active directos.
	if n := countActiveDirectos(t, repo, ventaID); n != 0 {
		t.Errorf("expected 0 active directos after full reversal, got %d", n)
	}
}

// ─── countingSaveFakeRepo ────────────────────────────────────────────────────

// countingSaveFakeRepo wraps fakeTraspasoRepo and fails after failAfterSave
// successful Save calls.
type countingSaveFakeRepo struct {
	*fakeTraspasoRepo
	failAfterSave int
	savesThisRun  int
}

func (r *countingSaveFakeRepo) Save(ctx context.Context, t *domain.Traspaso) (int, error) {
	r.savesThisRun++
	if r.savesThisRun > r.failAfterSave {
		return 0, errSentinel
	}
	return r.fakeTraspasoRepo.Save(ctx, t)
}
