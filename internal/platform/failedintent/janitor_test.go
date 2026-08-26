package failedintent_test

import (
	"context"
	"io"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// ---------------------------------------------------------------------------
// memStore — in-memory Store for janitor tests
// ---------------------------------------------------------------------------

// memStore satisfies failedintent.Store. All methods except PurgeOlderThan
// are no-ops or return nil. PurgeOlderThan deletes intents older than before
// from an in-memory map and broadcasts on purgeCh each time it is called.
type memStore struct {
	mu       sync.Mutex
	intents  map[uuid.UUID]failedintent.Intent
	purgeErr error
	purgeCh  chan struct{} // closed/signalled after each PurgeOlderThan call
	purges   atomic.Int64  // total calls to PurgeOlderThan
	listErr  error
	markErr  error

	guardarResumenErr  error
	resumenesGuardados atomic.Int64
}

func newMemStore() *memStore {
	return &memStore{
		intents: make(map[uuid.UUID]failedintent.Intent),
		purgeCh: make(chan struct{}, 64),
	}
}

// blobContador envuelve un BlobStorage para contar aperturas. Es lo que
// convierte "el blob se abre una sola vez" de una intención escrita en un
// comentario a un hecho medido.
type blobContador struct {
	failedintent.BlobStorage
	abiertos atomic.Int64
}

func (b *blobContador) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	b.abiertos.Add(1)
	return b.BlobStorage.Open(ctx, path)
}

func (m *memStore) add(i failedintent.Intent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.intents[i.ID] = i
}

// get devuelve una copia del intento, o nil si ya no está.
func (m *memStore) get(id uuid.UUID) *failedintent.Intent {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, ok := m.intents[id]
	if !ok {
		return nil
	}
	return &i
}

func (m *memStore) has(id uuid.UUID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.intents[id]
	return ok
}

// MarkResolvedByKeys cierra los pendientes de esa ruta cuya clave esté en la
// lista, igual que la implementación real. Es de verdad y no un no-op porque
// la conciliación se prueba de punta a punta: un doble que devuelve 0 dejaría
// pasar un janitor que llama al checker y tira la respuesta.
func (m *memStore) MarkResolvedByKeys(
	_ context.Context, path string, keys []string, now time.Time,
) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.markErr != nil {
		return 0, m.markErr
	}
	buscadas := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		buscadas[k] = struct{}{}
	}
	var n int64
	for id, i := range m.intents {
		if i.Path != path || i.Status != failedintent.StatusNew {
			continue
		}
		if _, ok := buscadas[i.IdempotencyKey]; !ok {
			continue
		}
		i.Status = failedintent.StatusResolvedManual
		sellado := now
		i.ResolvedAt = &sellado
		m.intents[id] = i
		n++
	}
	return n, nil
}

func (m *memStore) Save(_ context.Context, i failedintent.Intent) (failedintent.SaveOutcome, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.intents[i.ID] = i
	return failedintent.SaveOutcome{}, nil
}

func (m *memStore) Get(_ context.Context, _ uuid.UUID) (*failedintent.Intent, error) {
	return nil, nil //nolint:nilnil // not-found sentinel per Store contract
}

// List devuelve los intentos que coinciden con el filtro de estado, ordenados
// por ReceivedAt para que el cursor de la conciliación avance de verdad.
//
// La paginación es real (PageSize + HasMore) porque el barrido del janitor
// pagina: con un doble que devuelve todo de un tiro, un bug de cursor —el
// clásico que repite la primera página para siempre— pasaría inadvertido.
func (m *memStore) List(
	_ context.Context, p failedintent.ListParams,
) (failedintent.Page[failedintent.Intent], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return failedintent.Page[failedintent.Intent]{}, m.listErr
	}

	var candidatos []failedintent.Intent
	for _, i := range m.intents {
		if p.Status != "" && i.Status != p.Status {
			continue
		}
		if !p.CursorReceivedAt.IsZero() && !i.ReceivedAt.After(p.CursorReceivedAt) {
			continue
		}
		if p.Modulo != "" && i.Modulo != p.Modulo {
			continue
		}
		if p.SinExtraer && (i.Modulo != "" || i.Resumen != nil) {
			continue
		}
		candidatos = append(candidatos, i)
	}
	sort.Slice(candidatos, func(a, b int) bool {
		return candidatos[a].ReceivedAt.Before(candidatos[b].ReceivedAt)
	})

	size := p.PageSize
	if size <= 0 {
		size = 20
	}
	page := failedintent.Page[failedintent.Intent]{}
	if len(candidatos) > size {
		page.Items = candidatos[:size]
		page.HasMore = true
	} else {
		page.Items = candidatos
	}
	if len(page.Items) > 0 {
		ultimo := page.Items[len(page.Items)-1]
		page.NextReceivedAt = ultimo.ReceivedAt
		page.NextID = ultimo.ID
	}
	return page, nil
}

func (m *memStore) UpdateStatus(
	_ context.Context,
	_ uuid.UUID,
	_, _ failedintent.Status,
	_ uuid.UUID,
	_ string,
	_ time.Time,
) error {
	return nil
}

func (m *memStore) TransitionAfterReplay(
	_ context.Context,
	_ uuid.UUID,
	_, _ failedintent.Status,
) error {
	return nil
}

func (m *memStore) IncrementRetry(_ context.Context, _ uuid.UUID) error {
	return nil
}

// GuardarResumen imita al real: escribe MODULO/RESUMEN y NADA más, y sólo
// sobre una fila que siga sin extraer.
func (m *memStore) GuardarResumen(
	_ context.Context, id uuid.UUID, modulo string, r *failedintent.Resumen,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.guardarResumenErr != nil {
		return m.guardarResumenErr
	}
	i, ok := m.intents[id]
	if !ok || i.Modulo != "" || i.Resumen != nil {
		return nil
	}
	i.Modulo = modulo
	i.Resumen = r
	m.intents[id] = i
	m.resumenesGuardados.Add(1)
	return nil
}

// PurgeOlderThan removes intents whose ReceivedAt is strictly before `before`.
// It always signals purgeCh after running (even on error).
//
// `estados` acota igual que la implementación real: vacío es "todos". Sin
// respetarlo aquí, la prueba del corte corto pasaría con una implementación
// que borra pendientes de la misma edad.
func (m *memStore) PurgeOlderThan(
	_ context.Context, before time.Time, estados ...failedintent.Status,
) (failedintent.PurgeResult, error) {
	// El contador y la señal cuentan CICLOS, no sentencias: un tick hace dos
	// cortes (el corto de los resueltos y el largo de todo) y el largo es el
	// último. Contar sentencias metería el número de cortes en pruebas que no
	// hablan de retención — "Start es idempotente" pasaría a depender de
	// cuántos cortes tenga el janitor.
	if len(estados) == 0 {
		m.purges.Add(1)
		defer func() {
			m.purgeCh <- struct{}{}
		}()
	}

	if m.purgeErr != nil {
		return failedintent.PurgeResult{}, m.purgeErr
	}

	permitido := func(s failedintent.Status) bool {
		if len(estados) == 0 {
			return true
		}
		for _, e := range estados {
			if e == s {
				return true
			}
		}
		return false
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	var result failedintent.PurgeResult
	for id, intent := range m.intents {
		if intent.ReceivedAt.Before(before) && permitido(intent.Status) {
			delete(m.intents, id)
			result.RowsDeleted++
			if intent.BodyBlobPath != "" {
				result.BlobPaths = append(result.BlobPaths, intent.BodyBlobPath)
			}
		}
	}
	return result, nil
}

func (m *memStore) ReferencedPaths(_ context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var paths []string
	for _, intent := range m.intents {
		if intent.BodyBlobPath != "" {
			paths = append(paths, intent.BodyBlobPath)
		}
	}
	return paths, nil
}

// waitForPurge blocks until at least one PurgeOlderThan call completes or the
// context is cancelled.
func (m *memStore) waitForPurge(ctx context.Context) bool {
	select {
	case <-m.purgeCh:
		return true
	case <-ctx.Done():
		return false
	}
}

// ---------------------------------------------------------------------------
// Janitor tests
// ---------------------------------------------------------------------------

func TestJanitor_PurgesAtBoot(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	now := time.Now()

	// Old intent: received 100 days ago (exceeds DefaultRetain of 90 days).
	oldID := uuid.New()
	store.add(failedintent.Intent{
		ID:         oldID,
		ReceivedAt: now.Add(-100 * 24 * time.Hour),
		Status:     failedintent.StatusNew,
	})

	// Fresh intent: received 1 day ago.
	freshID := uuid.New()
	store.add(failedintent.Intent{
		ID:         freshID,
		ReceivedAt: now.Add(-24 * time.Hour),
		Status:     failedintent.StatusNew,
	})

	j := failedintent.NewJanitor(failedintent.JanitorConfig{
		Store:    store,
		Interval: time.Hour, // long enough that only the boot purge fires
		Retain:   90 * 24 * time.Hour,
		Clock:    func() time.Time { return now },
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, j.Start(ctx))
	// Wait for the boot purge to complete.
	ok := store.waitForPurge(ctx)
	require.True(t, ok, "purge must complete within timeout")

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	require.NoError(t, j.Stop(stopCtx))

	assert.False(t, store.has(oldID), "old intent must be purged")
	assert.True(t, store.has(freshID), "fresh intent must be kept")
}

func TestJanitor_StopReturnsPromptly(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	j := failedintent.NewJanitor(failedintent.JanitorConfig{
		Store:    store,
		Interval: time.Hour,
		Retain:   90 * 24 * time.Hour,
	})

	startCtx := context.Background()
	require.NoError(t, j.Start(startCtx))

	// Wait for the boot purge so the goroutine is in the select loop.
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	store.waitForPurge(waitCtx)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stopCancel()

	err := j.Stop(stopCtx)
	assert.NoError(t, err, "Stop must return before deadline")
}

func TestJanitor_StartIsIdempotent(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	j := failedintent.NewJanitor(failedintent.JanitorConfig{
		Store:    store,
		Interval: time.Hour,
		Retain:   90 * 24 * time.Hour,
	})

	ctx := context.Background()
	require.NoError(t, j.Start(ctx))
	// Second Start must be a no-op (no panic, no error).
	require.NoError(t, j.Start(ctx))

	// Wait for at most one boot purge signal.
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	store.waitForPurge(waitCtx)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	require.NoError(t, j.Stop(stopCtx))

	// Only one goroutine means exactly one boot purge (the second Start is a
	// no-op). The count should be exactly 1 (boot purge from first Start only).
	assert.Equal(t, int64(1), store.purges.Load(),
		"idempotent Start must not launch a second goroutine")
}

func TestJanitor_PurgeErrorIsLoggedButNotFatal(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	store.purgeErr = assert.AnError // every PurgeOlderThan call fails

	j := failedintent.NewJanitor(failedintent.JanitorConfig{
		Store:    store,
		Interval: 5 * time.Millisecond, // fast tick to get multiple cycles
		Retain:   90 * 24 * time.Hour,
	})

	startCtx := context.Background()
	require.NoError(t, j.Start(startCtx))

	// Wait for at least two purge attempts (boot + one tick).
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	store.waitForPurge(waitCtx) // first (boot)
	store.waitForPurge(waitCtx) // second (tick)

	assert.GreaterOrEqual(t, store.purges.Load(), int64(2),
		"janitor must keep ticking after purge errors")

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	assert.NoError(t, j.Stop(stopCtx), "Stop must return nil even after repeated errors")
}

// TestJanitor_StopWithoutStart_IsNoOp verifies that calling Stop on a janitor
// that was never started is safe and returns nil immediately.
func TestJanitor_StopWithoutStart_IsNoOp(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	j := failedintent.NewJanitor(failedintent.JanitorConfig{
		Store:    store,
		Interval: time.Hour,
		Retain:   time.Hour,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	require.NoError(t, j.Stop(ctx))
}

// fakeBlobs is a Delete-counting BlobStorage used by the janitor test.
// Save/Open are not exercised here; they return zero values.
type fakeBlobs struct {
	mu      sync.Mutex
	deleted []string
}

func (f *fakeBlobs) Save(_ context.Context, _ uuid.UUID, _ io.Reader, _ int64) (string, error) {
	return "", nil
}

func (f *fakeBlobs) Open(_ context.Context, _ string) (io.ReadCloser, error) {
	return nil, failedintent.ErrBlobNotFound
}

func (f *fakeBlobs) Delete(_ context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, path)
	return nil
}

// borrados devuelve una copia de los paths borrados, bajo el candado.
func (f *fakeBlobs) borrados() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deleted...)
}

// TestJanitor_PurgeDeletesBlobs verifies the janitor wires PurgeResult.BlobPaths
// through to BlobStorage.Delete on each purge cycle.
func TestJanitor_PurgeDeletesBlobs(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	blobs := &fakeBlobs{}
	now := time.Now()

	old := uuid.New()
	store.add(failedintent.Intent{
		ID:           old,
		ReceivedAt:   now.Add(-100 * 24 * time.Hour),
		BodyBlobPath: "/blob/old.bin",
	})
	// Old row with no blob — must not appear in the Delete list.
	store.add(failedintent.Intent{
		ID:         uuid.New(),
		ReceivedAt: now.Add(-100 * 24 * time.Hour),
	})

	j := failedintent.NewJanitor(failedintent.JanitorConfig{
		Store:    store,
		Blob:     blobs,
		Interval: time.Hour,
		Retain:   90 * 24 * time.Hour,
		Clock:    func() time.Time { return now },
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, j.Start(ctx))
	store.waitForPurge(ctx)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	require.NoError(t, j.Stop(stopCtx))

	blobs.mu.Lock()
	defer blobs.mu.Unlock()
	assert.Equal(t, []string{"/blob/old.bin"}, blobs.deleted,
		"only the purged row's blob path must be deleted")
}

// TestJanitor_DefaultsApplied verifies that NewJanitor fills zero-valued
// JanitorConfig fields with the documented defaults.
func TestJanitor_DefaultsApplied(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	j := failedintent.NewJanitor(failedintent.JanitorConfig{Store: store})

	// Start + Stop should succeed using the default clock (time.Now) and the
	// default interval. We don't observe the interval but Start should not
	// error, and the boot purge should fire once.
	require.NoError(t, j.Start(context.Background()))
	<-store.purgeCh

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	require.NoError(t, j.Stop(ctx))
	assert.GreaterOrEqual(t, store.purges.Load(), int64(1))
}
