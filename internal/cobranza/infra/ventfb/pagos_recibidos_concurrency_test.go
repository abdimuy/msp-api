//nolint:misspell // Spanish vocabulary (pago, cobrador, importe, intento, etc.) by project convention.
package ventfb_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	cobranzaoutbound "github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// ─── Concurrency harness ───────────────────────────────────────────────────

// concurrencyHarness holds the shared infrastructure for concurrency tests.
// Unlike the rollback-only WithTestTransaction tests, these tests commit real
// transactions and rely on UUID-scoped cleanup to avoid polluting the shared DB.
type concurrencyHarness struct {
	pool     *firebird.Pool
	txMgr    *firebird.TxManager
	repo     *cobranzaventfb.PagosRecibidosRepo
	inserted []uuid.UUID
	mu       sync.Mutex
}

func newConcurrencyHarness(t *testing.T) *concurrencyHarness {
	t.Helper()
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	h := &concurrencyHarness{
		pool:  pool,
		txMgr: firebird.NewTxManager(pool.DB),
		repo:  cobranzaventfb.NewPagosRecibidosRepo(pool),
	}
	t.Cleanup(func() { h.cleanup(context.Background()) })
	return h
}

// trackID registers an ID for cleanup so the test can commit real transactions
// without leaking rows in the shared Firebird DB.
func (h *concurrencyHarness) trackID(id uuid.UUID) {
	h.mu.Lock()
	h.inserted = append(h.inserted, id)
	h.mu.Unlock()
}

// cleanup deletes every tracked pago (and its imagenes) in a single committed
// transaction. Errors are ignored — cleanup is best-effort; a subsequent
// admin sweep or the next test run's unique UUIDs make orphans harmless.
func (h *concurrencyHarness) cleanup(ctx context.Context) {
	h.mu.Lock()
	ids := make([]uuid.UUID, len(h.inserted))
	copy(ids, h.inserted)
	h.mu.Unlock()
	if len(ids) == 0 {
		return
	}
	_ = h.txMgr.RunInTx(ctx, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, h.pool.DB)
		for _, id := range ids {
			_, _ = q.ExecContext(ctx, "DELETE FROM MSP_PAGOS_IMAGENES WHERE PAGO_ID = ?", id.String())
			_, _ = q.ExecContext(ctx, "DELETE FROM MSP_PAGOS_RECIBIDOS WHERE ID = ?", id.String())
		}
		return nil
	})
}

// buildValidPagoRecibidoWithID constructs a PagoRecibido with the given ID so
// concurrency tests can use a shared UUID across goroutines.
func buildValidPagoRecibidoWithID(t *testing.T, id uuid.UUID) *domain.PagoRecibido {
	t.Helper()
	now := time.Now().UTC()
	p, err := domain.NewPagoRecibido(domain.CrearPagoRecibidoParams{
		ID:             id,
		CargoDoctoCCID: 5000,
		ClienteID:      11486,
		CobradorID:     200,
		Cobrador:       "Ramírez García, Jorge",
		Importe:        decimal.NewFromInt(1500),
		FormaCobroID:   87327,
		FechaHoraPago:  now.Add(-30 * time.Minute),
		CreatedBy:      uuid.New(),
		Now:            now,
	})
	require.NoError(t, err, "buildValidPagoRecibidoWithID: NewPagoRecibido must not fail")
	return p
}

// recordingFakeWriter is a MicrosipPagoWriter that records how many times
// Aplicar was called. Used by TestE2E_Concurrent_AplicarPago_SameUUID to
// assert the idempotent fast-path fires exactly once.
type recordingFakeWriter struct {
	mu        sync.Mutex
	callCount int
	result    cobranzaoutbound.MicrosipPagoResult
}

func (r *recordingFakeWriter) Aplicar(_ context.Context, _ cobranzaoutbound.MicrosipPagoInput) (cobranzaoutbound.MicrosipPagoResult, error) {
	r.mu.Lock()
	r.callCount++
	r.mu.Unlock()
	return r.result, nil
}

// ─── Tests ─────────────────────────────────────────────────────────────────

// TestE2E_Concurrent_Insert_SameUUID_IdempotencyConflict fires two goroutines
// that each try to INSERT the same UUID in separate committed transactions.
// Exactly one must succeed and the other must see domain.ErrPagoYaExiste.
// The final DB row count for that UUID must be exactly 1.
//
//nolint:paralleltest // concurrent goroutines own their own txns; t.Parallel would race with the cleanup.
func TestE2E_Concurrent_Insert_SameUUID_IdempotencyConflict(t *testing.T) {
	h := newConcurrencyHarness(t)

	pagoID := uuid.New()
	h.trackID(pagoID)

	// Build both pago objects in the test goroutine (require.NoError must not
	// be called from a spawned goroutine — testifylint go-require rule).
	pagos := [2]*domain.PagoRecibido{
		buildValidPagoRecibidoWithID(t, pagoID),
		buildValidPagoRecibidoWithID(t, pagoID),
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = h.txMgr.RunInTx(context.Background(), func(ctx context.Context) error {
				return h.repo.Insert(ctx, pagos[idx])
			})
		}(i)
	}
	wg.Wait()

	nilCount := 0
	duplicateCount := 0
	for _, e := range errs {
		if e == nil {
			nilCount++
		}
		if errors.Is(e, domain.ErrPagoYaExiste) {
			duplicateCount++
		}
	}
	require.Equal(t, 1, nilCount, "exactly one Insert must succeed")
	require.Equal(t, 1, duplicateCount, "exactly one Insert must see ErrPagoYaExiste")

	// Verify the DB has exactly one row for this UUID.
	var count int
	err := h.txMgr.RunInTx(context.Background(), func(ctx context.Context) error {
		return firebird.GetQuerier(ctx, h.pool.DB).
			QueryRowContext(ctx, "SELECT COUNT(*) FROM MSP_PAGOS_RECIBIDOS WHERE ID = ?", pagoID.String()).
			Scan(&count)
	})
	require.NoError(t, err)
	require.Equal(t, 1, count, "DB must have exactly one row after concurrent inserts of the same UUID")
}

// TestE2E_Concurrent_LockByID_Serializes inserts a pago then runs two
// goroutines that each open a separate transaction, acquire LockByID, read
// the current INTENTOS, call RegistrarFallo, and commit. The pessimistic lock
// must serialize the two writers so no update is lost — final INTENTOS must
// equal 2.
//
//nolint:paralleltest // concurrent goroutines own their own txns; t.Parallel would race with the cleanup.
func TestE2E_Concurrent_LockByID_Serializes(t *testing.T) {
	h := newConcurrencyHarness(t)

	pagoID := uuid.New()
	h.trackID(pagoID)

	// Insert in its own committed tx.
	require.NoError(t, h.txMgr.RunInTx(context.Background(), func(ctx context.Context) error {
		return h.repo.Insert(ctx, buildValidPagoRecibidoWithID(t, pagoID))
	}))

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = h.txMgr.RunInTx(context.Background(), func(ctx context.Context) error {
				if e := h.repo.LockByID(ctx, pagoID); e != nil {
					return e
				}
				p, e := h.repo.FindByID(ctx, pagoID)
				if e != nil {
					return e
				}
				p.RegistrarFallo(fmt.Sprintf("worker_%d", idx), time.Now().UTC(), uuid.Nil)
				return h.repo.Update(ctx, p)
			})
		}(i)
	}
	wg.Wait()

	require.NoError(t, errs[0], "goroutine 0 must succeed")
	require.NoError(t, errs[1], "goroutine 1 must succeed")

	// Both committed — INTENTOS must be 2 (no lost update).
	var finalIntentos int
	require.NoError(t, h.txMgr.RunInTx(context.Background(), func(ctx context.Context) error {
		return firebird.GetQuerier(ctx, h.pool.DB).
			QueryRowContext(ctx, "SELECT INTENTOS FROM MSP_PAGOS_RECIBIDOS WHERE ID = ?", pagoID.String()).
			Scan(&finalIntentos)
	}))
	require.Equal(t, 2, finalIntentos, "LockByID must serialize updates — no lost update; final INTENTOS must be 2")
}

// TestE2E_Concurrent_AplicarPago_SameUUID_OnlyOneWriterCall fires two
// concurrent AplicarPago calls on the same pago UUID. The second call must hit
// the idempotent fast-path after the first commits (it reads IsAplicada=true
// after acquiring the lock). The MicrosipPagoWriter must be called exactly
// once — never twice.
//
//nolint:paralleltest // concurrent goroutines own their own txns; t.Parallel would race with the cleanup.
func TestE2E_Concurrent_AplicarPago_SameUUID_OnlyOneWriterCall(t *testing.T) {
	h := newConcurrencyHarness(t)

	writer := &recordingFakeWriter{
		result: cobranzaoutbound.MicrosipPagoResult{
			DoctoCCID:      99001,
			ImpteDoctoCCID: 99002,
			Folio:          "PG-9999",
		},
	}
	svc := cobranzaapp.NewService(
		nil, nil, nil, // saldos/pagos/ventas not needed for AplicarPago
		cobranzaoutbound.ProductionClock{},
		h.repo, // PagosRecibidosRepo
		h.repo, // PagosImagenesRepo — same struct satisfies both interfaces
		writer,
		nil, nil, // storage, imageProc not needed
		h.txMgr, // real TxManager — *firebird.TxManager satisfies TxRunner
	)

	pagoID := uuid.New()
	h.trackID(pagoID)

	// Insert the pago in a committed tx so both goroutines can see it.
	require.NoError(t, h.txMgr.RunInTx(context.Background(), func(ctx context.Context) error {
		return h.repo.Insert(ctx, buildValidPagoRecibidoWithID(t, pagoID))
	}))

	var wg sync.WaitGroup
	results := make([]*domain.PagoRecibido, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = svc.AplicarPago(context.Background(), pagoID, uuid.Nil)
		}(i)
	}
	wg.Wait()

	require.NoError(t, errs[0], "goroutine 0 AplicarPago must not error")
	require.NoError(t, errs[1], "goroutine 1 AplicarPago must not error")

	// The writer must be called exactly once — the second goroutine takes the
	// idempotent fast-path when it sees IsAplicada=true after lock acquisition.
	writer.mu.Lock()
	callCount := writer.callCount
	writer.mu.Unlock()
	require.Equal(t, 1, callCount, "MicrosipPagoWriter.Aplicar must be called exactly once; idempotent fast-path must fire for second caller")

	// Both results must represent the applied payment.
	require.NotNil(t, results[0], "goroutine 0 must return a non-nil pago")
	require.NotNil(t, results[1], "goroutine 1 must return a non-nil pago")
	require.True(t, results[0].IsAplicada(), "pago returned to goroutine 0 must be aplicada")
	require.True(t, results[1].IsAplicada(), "pago returned to goroutine 1 must be aplicada")
	require.Equal(t, results[0].DoctoCCID(), results[1].DoctoCCID(), "both results must carry the same DoctoCCID")
}

// TestE2E_Concurrent_AplicarPago_AndUpdate_NoDeadlock verifies that
// concurrent operations on different UUIDs (AplicarPago on A and
// RegistrarFallo+Update on B) do not deadlock. Both goroutines must complete
// within 5 seconds — the lock pattern must not serialize across unrelated rows.
//
//nolint:paralleltest // concurrent goroutines own their own txns; t.Parallel would race with the cleanup.
func TestE2E_Concurrent_AplicarPago_AndUpdate_NoDeadlock(t *testing.T) {
	h := newConcurrencyHarness(t)

	writer := &recordingFakeWriter{
		result: cobranzaoutbound.MicrosipPagoResult{
			DoctoCCID:      88001,
			ImpteDoctoCCID: 88002,
			Folio:          "PG-8888",
		},
	}
	svc := cobranzaapp.NewService(
		nil, nil, nil,
		cobranzaoutbound.ProductionClock{},
		h.repo,
		h.repo,
		writer,
		nil, nil,
		h.txMgr,
	)

	pagoAID := uuid.New()
	pagoBID := uuid.New()
	h.trackID(pagoAID)
	h.trackID(pagoBID)

	// Insert both pagos in committed txns before spawning goroutines.
	require.NoError(t, h.txMgr.RunInTx(context.Background(), func(ctx context.Context) error {
		return h.repo.Insert(ctx, buildValidPagoRecibidoWithID(t, pagoAID))
	}))
	require.NoError(t, h.txMgr.RunInTx(context.Background(), func(ctx context.Context) error {
		return h.repo.Insert(ctx, buildValidPagoRecibidoWithID(t, pagoBID))
	}))

	done := make(chan error, 2)

	// Goroutine 1: apply pago A via full AplicarPago flow.
	go func() {
		_, err := svc.AplicarPago(context.Background(), pagoAID, uuid.Nil)
		done <- err
	}()

	// Goroutine 2: register a failure on pago B (lock B, RegistrarFallo, Update).
	go func() {
		done <- h.txMgr.RunInTx(context.Background(), func(ctx context.Context) error {
			if e := h.repo.LockByID(ctx, pagoBID); e != nil {
				return e
			}
			p, e := h.repo.FindByID(ctx, pagoBID)
			if e != nil {
				return e
			}
			p.RegistrarFallo("deadlock_test_error", time.Now().UTC(), uuid.Nil)
			return h.repo.Update(ctx, p)
		})
	}()

	// Both goroutines must finish within 5 seconds; a longer wait indicates deadlock.
	timeout := time.After(5 * time.Second)
	for range 2 {
		select {
		case err := <-done:
			require.NoError(t, err, "concurrent operation on separate UUIDs must not error")
		case <-timeout:
			t.Fatal("deadlock detected: concurrent operations on separate rows did not complete within 5 seconds")
		}
	}
}

// TestE2E_Concurrent_ListPendientes_Stable_Under_Inserts verifies read
// snapshot stability: while one goroutine is inserting pendientes, another is
// listing them. The test asserts that no scan error occurs and no returned row
// has a zero-value UUID (which would indicate a non-atomic read).
//
//nolint:paralleltest // concurrent goroutines own their own txns; t.Parallel would race with the cleanup.
func TestE2E_Concurrent_ListPendientes_Stable_Under_Inserts(t *testing.T) {
	h := newConcurrencyHarness(t)

	const insertCount = 50
	const listCount = 100

	// Pre-build all pago objects in the test goroutine so no goroutine calls
	// require.NoError (testifylint go-require rule forbids t assertions outside
	// the test goroutine). IDs are tracked here for cleanup.
	prebuilt := make([]*domain.PagoRecibido, insertCount)
	for i := range insertCount {
		id := uuid.New()
		h.trackID(id)
		prebuilt[i] = buildValidPagoRecibidoWithID(t, id)
	}

	var wg sync.WaitGroup
	insertErrs := make([]error, insertCount)
	listErrs := make([]error, listCount)

	// Goroutine A: insert 50 pre-built pagos, each in its own committed transaction.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i, p := range prebuilt {
			insertErrs[i] = h.txMgr.RunInTx(context.Background(), func(ctx context.Context) error {
				return h.repo.Insert(ctx, p)
			})
		}
	}()

	// Goroutine B: list pendientes 100 times concurrently with the inserts.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range listCount {
			var rows []*domain.PagoRecibido
			listErrs[i] = h.txMgr.RunInTx(context.Background(), func(ctx context.Context) error {
				var e error
				rows, e = h.repo.ListPendientes(ctx, 100, 200)
				return e
			})
			// Assert row integrity: no row should have a zero-value UUID,
			// which would indicate a partial / non-atomic scan.
			for _, p := range rows {
				if p.ID() == (uuid.UUID{}) {
					t.Errorf("ListPendientes returned a row with zero-value UUID — non-atomic read suspected")
				}
			}
		}
	}()

	wg.Wait()

	for i, e := range insertErrs {
		require.NoError(t, e, "insert goroutine: iteration %d must not error", i)
	}
	for i, e := range listErrs {
		require.NoError(t, e, "list goroutine: iteration %d must not error", i)
	}
}
