//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// ─── Property test: atomicity invariant ──────────────────────────────────────

// TestProperty_CrearPagoConImagenes_AtomicityInvariant explores a small space
// of failure modes (which imagen errors, at which stage, with which fault
// type) and asserts the invariant: after each scenario, either everything
// committed (1 pago + N imagenes + N blobs) or nothing did (0 pago + 0
// imagenes + 0 blobs). No partial state is ever visible.
func TestProperty_CrearPagoConImagenes_AtomicityInvariant(t *testing.T) {
	t.Parallel()

	type fault int
	const (
		faultNone fault = iota
		faultStore
		faultInsertImagen
	)

	cases := []struct {
		name       string
		nImgs      int
		failAt     int // 1-indexed; 0 means no failure
		faultStage fault
		wantCount  int // expected (pago, imagen) row counts: 0 on rollback, 1/N on success
	}{
		{"3img_ok", 3, 0, faultNone, 1},
		{"5img_ok", 5, 0, faultNone, 1},
		{"1img_storage_fails_first", 1, 1, faultStore, 0},
		{"3img_storage_fails_middle", 3, 2, faultStore, 0},
		{"3img_storage_fails_last", 3, 3, faultStore, 0},
		{"5img_insertimagen_fails_first", 5, 1, faultInsertImagen, 0},
		{"5img_insertimagen_fails_middle", 5, 3, faultInsertImagen, 0},
		{"5img_insertimagen_fails_last", 5, 5, faultInsertImagen, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
			saldos := seedCargoSaldo(t)
			pagosRepo := newFakePagosRecibidosRepo()
			imgRepo := newFakePagosImagenesRepo()
			store := newFakeStorageProvider()

			var svc *app.Service
			if tc.faultStage == faultStore && tc.failAt > 0 {
				flaky := &flakyStorage{
					fakeStorageProvider: store,
					failOnCall:          tc.failAt,
					failErr:             errors.New("induced_store_failure"),
				}
				svc = app.NewService(
					saldos, newFakePagosRepo(), nil, fixedClock{T: now},
					pagosRepo, imgRepo, nil, flaky, nil, fakeTxRunner{},
				)
			} else if tc.faultStage == faultInsertImagen && tc.failAt > 0 {
				counting := &countingImagenRepo{
					fakePagosImagenesRepo: imgRepo,
					failOnCall:            tc.failAt,
					failErr:               errors.New("induced_insert_failure"),
				}
				txRunner := newSnapshottingTxRunner(pagosRepo, imgRepo)
				svc = app.NewService(
					saldos, newFakePagosRepo(), nil, fixedClock{T: now},
					pagosRepo, counting, nil, store, nil, txRunner,
				)
			} else {
				svc = app.NewService(
					saldos, newFakePagosRepo(), nil, fixedClock{T: now},
					pagosRepo, imgRepo, nil, store, nil, fakeTxRunner{},
				)
			}

			in := baseCrearInput(now)
			imgs := make([]app.ImagenUploadInput, tc.nImgs)
			for i := range imgs {
				imgs[i] = baseImagenUpload(in.ID, domain.MimePDF,
					bytes.Repeat([]byte{byte('A' + i)}, 32))
				imgs[i].StorageKey = "pagos/" + in.ID.String() + "/" + imgs[i].ImagenID.String() + ".pdf"
			}

			_, err := svc.CrearPagoConImagenes(context.Background(), in, imgs, uuid.New())

			if tc.wantCount == 1 {
				require.NoError(t, err, "happy case must succeed")
				assert.Len(t, pagosRepo.rows, 1)
				assert.Len(t, imgRepo.images, tc.nImgs)
				assert.Len(t, store.objects, tc.nImgs)
			} else {
				require.Error(t, err, "induced failure case must error")
				assert.Empty(t, pagosRepo.rows, "atomicity: no pago on failure")
				assert.Empty(t, imgRepo.images, "atomicity: no imagenes on failure")
				assert.Empty(t, store.objects, "atomicity: no blobs remain on failure")
			}
		})
	}
}

// ─── Concurrency test: same-UUID double POST ─────────────────────────────────

// TestConcurrent_CrearPagoConImagenes_SameUUID_OnePagoOneImagenSet runs N
// goroutines that each call CrearPagoConImagenes with the same pago UUID and
// each with their own batch of imagenes. The invariant: exactly one pago in
// the repo at the end, regardless of N. (Image attribution is whichever
// goroutine won the Insert race; the others get the existing pago back and
// their blobs are cleaned up.)
//
// This is the app-level shape of the integration test
// TestE2E_Concurrent_CrearPagoConImagenes_SameUUID — same invariant, but
// uses in-memory fakes so it runs in ~ms.
func TestConcurrent_CrearPagoConImagenes_SameUUID_OnePagoOneImagenSet(t *testing.T) {
	t.Parallel()

	const goroutines = 8
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

	saldos := seedCargoSaldo(t)
	pagosRepo := newConcurrentSafePagosRecibidosRepo()
	imgRepo := newConcurrentSafePagosImagenesRepo()
	store := newConcurrentSafeStorage()
	svc := app.NewService(
		saldos, newFakePagosRepo(), nil, fixedClock{T: now},
		pagosRepo, imgRepo, nil, store, nil, fakeTxRunner{},
	)

	in := baseCrearInput(now)

	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			img := baseImagenUpload(in.ID, domain.MimePDF, []byte("X"))
			img.StorageKey = "pagos/" + in.ID.String() + "/" + img.ImagenID.String() + ".pdf"
			_, err := svc.CrearPagoConImagenes(context.Background(), in, []app.ImagenUploadInput{img}, uuid.New())
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err, "concurrent CrearPagoConImagenes must all succeed (idempotency replay)")
	}
	// Exactly one pago row exists; exactly one imagen attached (only the
	// winner's; losers' blobs were cleaned up because the pago already
	// existed). Total stores happened goroutines times; cleanups happened
	// goroutines-1 times.
	assert.Equal(t, 1, pagosRepo.count(), "exactly one pago after concurrent same-UUID writes")
	assert.Equal(t, 1, imgRepo.count(), "winner's single imagen survives")
	assert.Equal(t, goroutines, store.storeCallCount(), "every goroutine stored its blob")
	assert.Equal(t, goroutines-1, store.deleteCallCount(), "losers' blobs cleaned up via idempotent replay path")
}

// concurrentSafePagosImagenesRepo wraps fakePagosImagenesRepo with a mutex so
// the race detector stays quiet under parallel writes.
type concurrentSafePagosImagenesRepo struct {
	*fakePagosImagenesRepo
	mu sync.Mutex
	n  atomic.Int32
}

func newConcurrentSafePagosImagenesRepo() *concurrentSafePagosImagenesRepo {
	return &concurrentSafePagosImagenesRepo{fakePagosImagenesRepo: newFakePagosImagenesRepo()}
}

func (r *concurrentSafePagosImagenesRepo) InsertImagen(ctx context.Context, pagoID uuid.UUID, img *domain.Imagen) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.fakePagosImagenesRepo.InsertImagen(ctx, pagoID, img); err != nil {
		return err
	}
	r.n.Add(1)
	return nil
}

func (r *concurrentSafePagosImagenesRepo) count() int {
	return int(r.n.Load())
}

// concurrentSafeStorage wraps fakeStorageProvider with atomic counters and a
// mutex around the underlying map writes so concurrent goroutines don't trip
// the race detector.
type concurrentSafeStorage struct {
	*fakeStorageProvider
	mu      sync.Mutex
	stores  atomic.Int32
	deletes atomic.Int32
}

func newConcurrentSafeStorage() *concurrentSafeStorage {
	return &concurrentSafeStorage{fakeStorageProvider: newFakeStorageProvider()}
}

func (s *concurrentSafeStorage) Store(ctx context.Context, key, contentType string, sizeBytes int64, body io.Reader) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stores.Add(1)
	// Bypass the embedded counter (raced) and call the underlying logic.
	if s.storeErr != nil {
		return s.storeErr
	}
	buf, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	s.objects[key] = buf
	s.mimes[key] = contentType
	_ = sizeBytes
	return nil
}

func (s *concurrentSafeStorage) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletes.Add(1)
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.objects, key)
	delete(s.mimes, key)
	return nil
}

func (s *concurrentSafeStorage) storeCallCount() int  { return int(s.stores.Load()) }
func (s *concurrentSafeStorage) deleteCallCount() int { return int(s.deletes.Load()) }

// concurrentSafePagosRecibidosRepo wraps fakePagosRecibidosRepo with a mutex
// so the race detector stays quiet under parallel access. Insert still
// honors ErrPagoYaExiste on duplicate UUIDs.
type concurrentSafePagosRecibidosRepo struct {
	*fakePagosRecibidosRepo
	mu sync.Mutex
	n  atomic.Int32
}

func newConcurrentSafePagosRecibidosRepo() *concurrentSafePagosRecibidosRepo {
	return &concurrentSafePagosRecibidosRepo{fakePagosRecibidosRepo: newFakePagosRecibidosRepo()}
}

func (r *concurrentSafePagosRecibidosRepo) Insert(ctx context.Context, p *domain.PagoRecibido) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.fakePagosRecibidosRepo.Insert(ctx, p); err != nil {
		return err
	}
	r.n.Add(1)
	return nil
}

func (r *concurrentSafePagosRecibidosRepo) FindByID(ctx context.Context, id uuid.UUID) (*domain.PagoRecibido, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fakePagosRecibidosRepo.FindByID(ctx, id)
}

func (r *concurrentSafePagosRecibidosRepo) count() int {
	return int(r.n.Load())
}
