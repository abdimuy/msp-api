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
	svc := app.NewService(repo, newAbundantExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

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
	svc := app.NewService(repo, newAbundantExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

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
	svc := app.NewService(repo, newAbundantExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

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
	svc := app.NewService(repo, newAbundantExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

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
	svc := app.NewService(repo, newAbundantExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

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
	svc := app.NewService(repo, newAbundantExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

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
	svc := app.NewService(repo, newAbundantExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

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
	svc := app.NewService(repo, newAbundantExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

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
	svc := app.NewService(repo, newAbundantExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

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
	svc := app.NewService(repo, newAbundantExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

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
	svc := app.NewService(repo, newAbundantExistenciaQuery(), minter, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

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

// TestResincronizar_SaveFailsOnNewDirecto_ReturnsError verifies that when the
// Save call for the new directo fails, the error is propagated to the caller.
// Note: the fake repo does NOT roll back the reverso write — true DB rollback
// is an e2e concern. This test only verifies error propagation.
func TestResincronizar_SaveFailsOnNewDirecto_ReturnsError(t *testing.T) {
	t.Parallel()
	// Seed the initial directo through the service so byID / byVentaID are in
	// a fully consistent state. Allow unlimited saves during seeding.
	const unlimited = 1_000_000
	repo := &countingSaveFakeRepo{fakeTraspasoRepo: newFakeTraspasoRepo(), failAfterSave: unlimited}
	svc := app.NewService(repo, newAbundantExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	ventaID := uuid.New()
	if _, _, err := svc.ResincronizarTraspasoParaVenta(context.Background(), baseResyncParams(ventaID)); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	// Allow exactly 1 more Save (the reverso); the new-directo Save will fail.
	repo.failAfterSave = repo.savesThisRun + 1

	p2 := baseResyncParams(ventaID)
	p2.Detalles = []app.CrearTraspasoDetalleInput{{ArticuloID: 999, Cantidad: decimal.NewFromInt(1)}}

	_, _, err := svc.ResincronizarTraspasoParaVenta(context.Background(), p2)
	if !errors.Is(err, errSentinel) {
		t.Errorf("expected sentinel error from Save, got %v", err)
	}
}

// ─── CrearTraspasoReverso: only reverses the ACTIVE directo ─────────────────

func TestCrearTraspasoReverso_AfterEditChain_ReversesActiveOnly(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	outbox := &fakeOutbox{}
	svc := app.NewService(repo, newAbundantExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

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

// ─── M1: ListByVentaID repo error propagates ────────────────────────────────

// TestResincronizar_ListByVentaIDError_Propagates mirrors
// TestCrearTraspasoReverso_ListByVentaIDError: injecting a repo error on
// ListByVentaID (via findErr) must propagate out of
// ResincronizarTraspasoParaVenta unchanged.
func TestResincronizar_ListByVentaIDError_Propagates(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	repo.findErr = errSentinel
	svc := app.NewService(repo, newAbundantExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	_, _, err := svc.ResincronizarTraspasoParaVenta(context.Background(), baseResyncParams(uuid.New()))
	if !errors.Is(err, errSentinel) {
		t.Errorf("expected sentinel error from ListByVentaID, got %v", err)
	}
}

// ─── M3: nil DoctoInID on active directo returns internal error ──────────────

// TestResincronizar_ActiveDirectoNilDoctoInID_ReturnsError verifies that when
// the active directo exists but has no DoctoInID (was never applied to
// Microsip), ResincronizarTraspasoParaVenta returns a
// traspaso_directo_sin_docto_in_id error rather than panicking.
func TestResincronizar_ActiveDirectoNilDoctoInID_ReturnsError(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	svc := app.NewService(repo, newAbundantExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	ventaID := uuid.New()
	// Inject an active directo with DoctoInID=nil directly into the fake repo.
	// HydrateTraspaso is used because CrearTraspaso always calls MarcarAplicado.
	tr := domain.HydrateTraspaso(domain.HydrateTraspasoParams{
		ID:             uuid.New(),
		Folio:          mustFolio("MST000099"),
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "sin docto in",
		VentaID:        &ventaID,
		TipoReverso:    false,
		Reversado:      false,
		DoctoInID:      nil, // explicitly not applied
		Detalles:       nil,
		CreatedAt:      fixedNow,
		UpdatedAt:      fixedNow,
		CreatedBy:      uuid.New(),
		UpdatedBy:      uuid.New(),
	})
	repo.byID[1] = tr
	repo.byVentaID[ventaID] = []*domain.Traspaso{tr}
	repo.counter = 1

	// Use the same params so sameNetEffect fires (empty detalles on both sides
	// would be a match, but we want to hit the nil-DoctoInID guard). Params
	// with detalles that differ from the nil-detalles active will NOT hit
	// sameNetEffect, but will instead hit the non-fast-path nil guard.
	p := baseResyncParams(ventaID)

	_, _, err := svc.ResincronizarTraspasoParaVenta(context.Background(), p)
	if !isAppError(err, "traspaso_directo_sin_docto_in_id") {
		t.Errorf("expected traspaso_directo_sin_docto_in_id error, got %v", err)
	}
}

// ─── M4: duplicate articuloIDs in params collapse to same net effect ─────────

// TestResincronizar_DuplicateArticuloIDs_CollapseToSameNetEffect verifies that
// when the new params contain two lines for the same articuloID whose cantidades
// sum to the active directo's cantidad for that articuloID, sameNetEffect
// detects a no-op and no new Saves are performed.
func TestResincronizar_DuplicateArticuloIDs_CollapseToSameNetEffect(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	outbox := &fakeOutbox{}
	svc := app.NewService(repo, newAbundantExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

	ventaID := uuid.New()
	// Seed active directo: articuloID=100, cantidad=5.
	seed := app.CrearTraspasoParaVentaParams{
		VentaID:        ventaID,
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "seed",
		Detalles:       []app.CrearTraspasoDetalleInput{{ArticuloID: 100, Cantidad: decimal.NewFromInt(5)}},
		CreatedBy:      uuid.New(),
	}
	if _, _, err := svc.ResincronizarTraspasoParaVenta(context.Background(), seed); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	savesBefore := repo.SaveCalls
	outbox.entries = nil

	// Call resync with two lines for articuloID=100: {100,2} + {100,3} = 5.
	// Same almacenes, same net cantidad → no-op.
	p := app.CrearTraspasoParaVentaParams{
		VentaID:        ventaID,
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "resync duplicate",
		Detalles: []app.CrearTraspasoDetalleInput{
			{ArticuloID: 100, Cantidad: decimal.NewFromInt(2)},
			{ArticuloID: 100, Cantidad: decimal.NewFromInt(3)},
		},
		CreatedBy: uuid.New(),
	}

	tr, _, err := svc.ResincronizarTraspasoParaVenta(context.Background(), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr == nil {
		t.Fatal("expected active traspaso returned on no-op, got nil")
	}
	// No new Saves — this is a no-op.
	if repo.SaveCalls != savesBefore {
		t.Errorf("expected 0 new Saves on no-op, got %d", repo.SaveCalls-savesBefore)
	}
	if len(outbox.entries) != 0 {
		t.Errorf("expected 0 outbox entries on no-op, got %d", len(outbox.entries))
	}
}

// ─── M5: same detalles but different almacenOrigen → NOT a no-op ─────────────

// TestResincronizar_DifferentAlmacenOrigen_NotNoop verifies that when the new
// params have the same detalles multiset as the active directo but a different
// almacenOrigen, sameNetEffect returns false and the service reverses the active
// directo and creates a new one.
func TestResincronizar_DifferentAlmacenOrigen_NotNoop(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	outbox := &fakeOutbox{}
	svc := app.NewService(repo, newAbundantExistenciaQuery(), &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

	ventaID := uuid.New()
	// Seed active directo: almacenOrigen=1, almacenDestino=2, articuloID=100, cantidad=5.
	seed := app.CrearTraspasoParaVentaParams{
		VentaID:        ventaID,
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "seed",
		Detalles:       []app.CrearTraspasoDetalleInput{{ArticuloID: 100, Cantidad: decimal.NewFromInt(5)}},
		CreatedBy:      uuid.New(),
	}
	if _, _, err := svc.ResincronizarTraspasoParaVenta(context.Background(), seed); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	savesBefore := repo.SaveCalls
	outbox.entries = nil

	// Same detalles but almacenOrigen changed to 3 → different net effect.
	p := app.CrearTraspasoParaVentaParams{
		VentaID:        ventaID,
		AlmacenOrigen:  3, // changed
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "resync diff almacen",
		Detalles:       []app.CrearTraspasoDetalleInput{{ArticuloID: 100, Cantidad: decimal.NewFromInt(5)}},
		CreatedBy:      uuid.New(),
	}

	newTr, _, err := svc.ResincronizarTraspasoParaVenta(context.Background(), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newTr == nil {
		t.Fatal("expected a new directo, got nil")
	}
	// Should have produced exactly 2 new Saves: reverso + new directo.
	newSaves := repo.SaveCalls - savesBefore
	if newSaves != 2 {
		t.Errorf("expected 2 new Saves (reverso + directo), got %d", newSaves)
	}
	// Exactly 1 active directo after resync.
	if n := countActiveDirectos(t, repo, ventaID); n != 1 {
		t.Errorf("expected 1 active directo after resync, got %d", n)
	}
	// The new directo uses the new almacenOrigen.
	if newTr.AlmacenOrigen() != 3 {
		t.Errorf("expected almacenOrigen=3 on new directo, got %d", newTr.AlmacenOrigen())
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

// ─── sequentialExistenciaQuery ───────────────────────────────────────────────

// sequentialExistenciaQuery returns a pre-configured sequence of existencia
// values on successive Existencia calls. Once the sequence is exhausted it
// returns the last value in the list. This lets resync tests model the
// "post-reversal" state: first call returns the current (pre-reversal)
// existencia; second call returns the released (post-reversal) existencia.
type sequentialExistenciaQuery struct {
	values []decimal.Decimal // returned in order; last value repeats
	calls  int
}

func (q *sequentialExistenciaQuery) Existencia(_ context.Context, _, _ int) (decimal.Decimal, error) {
	idx := q.calls
	if idx >= len(q.values) {
		idx = len(q.values) - 1
	}
	q.calls++
	return q.values[idx], nil
}

func (q *sequentialExistenciaQuery) ExistenciasPorAlmacen(_ context.Context, _ int) ([]domain.Existencia, error) {
	return nil, nil
}

// ─── Stock-validation-in-resync regression tests ─────────────────────────────

// TestResincronizar_StockValidation_PassesAfterReversal is the primary
// regression test for the false-rejection bug. It models the scenario where:
//
//   - Article 100 in almacén 1 has an old reservation of 7 units, leaving
//     only 3 units of "free" existencia visible before the reversal.
//   - The edit requests 8 units.
//
// Before the fix, ventas pre-checked existencia (saw 3 < 8) and rejected the
// edit. After the fix, the resync reverses the old directo first, then checks
// existencia. The sequentialExistenciaQuery returns 3 on the first call (for
// the initial crearDirecto stock check when seeding), then 10 on subsequent
// calls (simulating the post-reversal state: 3 free + 7 released = 10).
// The edit to qty=8 must succeed.
func TestResincronizar_StockValidation_PassesAfterReversal(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()
	outbox := &fakeOutbox{}

	// First call (seed resync checkExistencia with qty=3): returns 3 — exact.
	// Subsequent calls (edit resync checkExistencia post-reversal): returns 10.
	eq := &sequentialExistenciaQuery{values: []decimal.Decimal{
		decimal.NewFromInt(3),  // first call: seed qty=3 passes (3>=3)
		decimal.NewFromInt(10), // second call: post-reversal existencia = 10
	}}
	svc := app.NewService(repo, eq, &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, outbox, nil)

	ventaID := uuid.New()

	// Seed: create an initial directo with qty=3 (simulating old reservation).
	seed := app.CrearTraspasoParaVentaParams{
		VentaID:        ventaID,
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "seed reservation",
		Detalles:       []app.CrearTraspasoDetalleInput{{ArticuloID: 100, Cantidad: decimal.NewFromInt(3)}},
		CreatedBy:      uuid.New(),
	}
	if _, _, err := svc.ResincronizarTraspasoParaVenta(context.Background(), seed); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	// Edit: request qty=8 — fits in 10 (post-reversal) but NOT in 3 (pre-reversal).
	// With the fix, this must succeed because checkExistencia runs after reversal.
	edit := app.CrearTraspasoParaVentaParams{
		VentaID:        ventaID,
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "edit to larger qty",
		Detalles:       []app.CrearTraspasoDetalleInput{{ArticuloID: 100, Cantidad: decimal.NewFromInt(8)}},
		CreatedBy:      uuid.New(),
	}
	newTr, _, err := svc.ResincronizarTraspasoParaVenta(context.Background(), edit)
	if err != nil {
		t.Fatalf("edit resync must succeed (qty=8 fits in post-reversal existencia=10), got error: %v", err)
	}
	if newTr == nil {
		t.Fatal("expected a new directo, got nil")
	}
	// Exactly one active directo after the edit.
	if n := countActiveDirectos(t, repo, ventaID); n != 1 {
		t.Errorf("expected 1 active directo after edit resync, got %d", n)
	}
}

// TestResincronizar_StockValidation_FailsWhenTrulyInsufficient verifies that
// when the new quantity exceeds even the post-reversal existencia, the resync
// returns ErrArticuloSinExistencia and the entire transaction rolls back
// atomically (the reversal is not committed, no new directo is created).
//
// The sequentialExistenciaQuery returns 10 on all calls (enough for the seed
// qty=7 check), then 5 for the edit's post-reversal check — so qty=8 fails.
func TestResincronizar_StockValidation_FailsWhenTrulyInsufficient(t *testing.T) {
	t.Parallel()
	repo := newFakeTraspasoRepo()

	// First call (seed resync, qty=7): 10 >= 7 → passes.
	// Second call (edit resync post-reversal, qty=8): 5 < 8 → fails.
	eq := &sequentialExistenciaQuery{values: []decimal.Decimal{
		decimal.NewFromInt(10), // seed check passes
		decimal.NewFromInt(5),  // post-reversal check: 5 < 8 → fail
	}}
	svc := app.NewService(repo, eq, &fakeFolioMinter{}, newFakeAlmacenRepo(), &fakeClock{fixedNow}, &fakeOutbox{}, nil)

	ventaID := uuid.New()

	// Seed: initial directo qty=7.
	seed := app.CrearTraspasoParaVentaParams{
		VentaID:        ventaID,
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "seed",
		Detalles:       []app.CrearTraspasoDetalleInput{{ArticuloID: 100, Cantidad: decimal.NewFromInt(7)}},
		CreatedBy:      uuid.New(),
	}
	if _, _, err := svc.ResincronizarTraspasoParaVenta(context.Background(), seed); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	savesBefore := repo.SaveCalls

	// Edit: qty=8 exceeds post-reversal existencia=5 → must fail.
	edit := app.CrearTraspasoParaVentaParams{
		VentaID:        ventaID,
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "edit over limit",
		Detalles:       []app.CrearTraspasoDetalleInput{{ArticuloID: 100, Cantidad: decimal.NewFromInt(8)}},
		CreatedBy:      uuid.New(),
	}
	_, _, err := svc.ResincronizarTraspasoParaVenta(context.Background(), edit)
	if !errors.Is(err, domain.ErrArticuloSinExistencia) {
		t.Errorf("expected ErrArticuloSinExistencia, got %v", err)
	}

	// With txMgr=nil the fake repo does not roll back in-memory writes — the
	// reverso Save may have landed in the fake repo. In production (Firebird tx),
	// the entire tx rolls back atomically. What we assert here is that:
	// (a) the error is returned to the caller, and
	// (b) no NEW directo was persisted (the new directo Save never ran).
	//
	// The fake repo does not implement rollback, so the reverso may appear.
	// We verify that no extra directo (non-reverso, non-reversado) was created.
	if n := countActiveDirectos(t, repo, ventaID); n != 0 {
		t.Errorf("expected 0 active directos after failed edit resync (new directo must not be persisted), got %d", n)
	}
	// At most 1 additional Save should have occurred (the reverso, if the fake
	// ran it before checkExistencia failed). The new-directo Save must not run.
	extraSaves := repo.SaveCalls - savesBefore
	if extraSaves > 1 {
		t.Errorf("expected at most 1 extra Save (reverso only; new directo must not be saved), got %d", extraSaves)
	}
}
