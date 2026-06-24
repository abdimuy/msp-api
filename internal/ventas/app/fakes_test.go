//nolint:misspell // domain vocabulary is Spanish (imagenes, ventas) per project convention.
package app_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// fakeImageProcessor is an outbound.ImageProcessor that records every
// Process call and either passes the body through unchanged or returns a
// configured error. Concurrency-safe.
type fakeImageProcessor struct {
	mu    sync.Mutex
	calls int
	// Err overrides successful pass-through with the supplied error.
	Err error
	// LastContentType captures the ContentType seen on the most recent call.
	LastContentType string
	// LastSizeBytes captures the SizeBytes seen on the most recent call.
	LastSizeBytes int64
	// OverrideOutput when non-nil supplies the Output returned to the
	// caller, mimicking a real processor that resizes/recompresses the
	// payload. When nil the body is read through verbatim.
	OverrideOutput *outbound.ImageProcessorOutput
}

// Process drains the input body and returns either the configured override
// or a passthrough Output.
func (f *fakeImageProcessor) Process(_ context.Context, in outbound.ImageProcessorInput) (outbound.ImageProcessorOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.LastContentType = in.ContentType
	f.LastSizeBytes = in.SizeBytes
	if f.Err != nil {
		return outbound.ImageProcessorOutput{}, f.Err
	}
	if f.OverrideOutput != nil {
		return *f.OverrideOutput, nil
	}
	buf, err := io.ReadAll(in.Body)
	if err != nil {
		return outbound.ImageProcessorOutput{}, err
	}
	return outbound.ImageProcessorOutput{
		Body:        bytes.NewReader(buf),
		ContentType: in.ContentType,
		SizeBytes:   int64(len(buf)),
	}, nil
}

// callsCount returns how many times Process has been invoked.
func (f *fakeImageProcessor) callsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fixedClock is an outbound.Clock that always returns T. Tests use it to
// freeze time so assertions on audit timestamps are deterministic.
type fixedClock struct{ T time.Time }

// Now returns the fixed instant T.
func (c fixedClock) Now() time.Time { return c.T }

// outboxCall records a single Enqueue invocation captured by fakeOutbox.
type outboxCall struct {
	Aggregate   string
	AggregateID uuid.UUID
	EventType   string
	Payload     any
}

// fakeOutbox captures every Enqueue call. Concurrency-safe so it can be
// shared across parallel subtests without data races.
type fakeOutbox struct {
	mu    sync.Mutex
	calls []outboxCall
	err   error
}

// Enqueue appends the call and returns the configured error.
func (f *fakeOutbox) Enqueue(_ context.Context, aggregate string, aggregateID uuid.UUID, eventType string, payload any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, outboxCall{
		Aggregate:   aggregate,
		AggregateID: aggregateID,
		EventType:   eventType,
		Payload:     payload,
	})
	return f.err
}

// eventTypes returns the ordered slice of event types captured so far.
func (f *fakeOutbox) eventTypes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.EventType
	}
	return out
}

// snapshot returns a copy of the calls slice for assertions.
func (f *fakeOutbox) snapshot() []outboxCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]outboxCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// sawEventType reports whether any captured call has the supplied event type.
func (f *fakeOutbox) sawEventType(eventType string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c.EventType == eventType {
			return true
		}
	}
	return false
}

// fakeStorage is an in-memory outbound.StorageProvider. Records call counts
// and error overrides per method.
type fakeStorage struct {
	mu          sync.Mutex
	blobs       map[string][]byte
	StoreErr    error
	DeleteErr   error
	GetErr      error
	StoreCalls  int
	DeleteCalls int
	DeletedKeys []string
	// LastStoreContentType is the contentType arg passed to the most
	// recent Store call — used by ordering tests to verify the processor
	// rewrites the MIME before storage sees it.
	LastStoreContentType string
	// LastStoreSizeBytes is the sizeBytes arg passed to the most recent
	// Store call — used to verify the processor's compressed size is the
	// one that reaches storage.
	LastStoreSizeBytes int64
	// LastStoreBody is the body bytes received by the most recent Store
	// call, after the body Reader has been fully consumed.
	LastStoreBody []byte
}

// newFakeStorage builds an empty in-memory storage.
func newFakeStorage() *fakeStorage {
	return &fakeStorage{blobs: map[string][]byte{}}
}

// Store records the call and persists the buffered body bytes when no error
// override is configured. Captures the contentType/sizeBytes args so
// ordering tests can verify the image processor has normalized the
// metadata before storage sees it.
func (f *fakeStorage) Store(_ context.Context, key, contentType string, sizeBytes int64, body io.Reader) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.StoreCalls++
	f.LastStoreContentType = contentType
	f.LastStoreSizeBytes = sizeBytes
	if f.StoreErr != nil {
		return f.StoreErr
	}
	buf, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	f.LastStoreBody = buf
	f.blobs[key] = buf
	return nil
}

// Get fetches a previously stored blob; returns the configured error if any.
func (f *fakeStorage) Get(_ context.Context, key string) (outbound.StorageObject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.GetErr != nil {
		return outbound.StorageObject{}, f.GetErr
	}
	buf, ok := f.blobs[key]
	if !ok {
		return outbound.StorageObject{}, errors.New("fake storage: not found")
	}
	return outbound.StorageObject{
		Body:        io.NopCloser(bytes.NewReader(buf)),
		ContentType: "application/octet-stream",
		SizeBytes:   int64(len(buf)),
	}, nil
}

// Delete records the call and removes the blob unless an override is set.
func (f *fakeStorage) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.DeleteCalls++
	f.DeletedKeys = append(f.DeletedKeys, key)
	if f.DeleteErr != nil {
		return f.DeleteErr
	}
	delete(f.blobs, key)
	return nil
}

// has reports whether the storage currently holds a blob at key.
func (f *fakeStorage) has(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.blobs[key]
	return ok
}

// fakeVentaRepo is an in-memory outbound.VentaRepo. The byID map owns the
// canonical pointer; FindByID returns it directly so tests can observe live
// child mutations after AdjuntarImagen / EliminarImagen.
type fakeVentaRepo struct {
	mu sync.Mutex

	byID map[uuid.UUID]*domain.Venta

	SaveErr         error
	UpdateErr       error
	FindErr         error
	ListErr         error
	InsertImagenErr error
	DeleteImagenErr error

	SaveCalls         int
	UpdateCalls       int
	InsertImagenCalls int
	DeleteImagenCalls int

	ListPage outbound.Page[*domain.Venta]
}

// newFakeVentaRepo builds an empty repo.
func newFakeVentaRepo() *fakeVentaRepo {
	return &fakeVentaRepo{byID: map[uuid.UUID]*domain.Venta{}}
}

// Save inserts a new venta. Duplicate IDs are rejected to mirror the real
// repository's UNIQUE constraint on the primary key.
func (f *fakeVentaRepo) Save(_ context.Context, v *domain.Venta) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.SaveCalls++
	if f.SaveErr != nil {
		return f.SaveErr
	}
	if _, ok := f.byID[v.ID()]; ok {
		return errors.New("fake venta repo: duplicate id")
	}
	f.byID[v.ID()] = v
	return nil
}

// Update rewrites the entry; returns ErrVentaNotFound if the venta is unknown.
func (f *fakeVentaRepo) Update(_ context.Context, v *domain.Venta) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.UpdateCalls++
	if f.UpdateErr != nil {
		return f.UpdateErr
	}
	if _, ok := f.byID[v.ID()]; !ok {
		return domain.ErrVentaNotFound
	}
	f.byID[v.ID()] = v
	return nil
}

// FindByID returns the live aggregate pointer or ErrVentaNotFound.
func (f *fakeVentaRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.Venta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FindErr != nil {
		return nil, f.FindErr
	}
	v, ok := f.byID[id]
	if !ok {
		return nil, domain.ErrVentaNotFound
	}
	return v, nil
}

// LockByID is a no-op lock for the in-memory fake: it only validates existence
// so the AplicarVenta anti-double-submit guard can be exercised in unit tests.
func (f *fakeVentaRepo) LockByID(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.byID[id]; !ok {
		return domain.ErrVentaNotFound
	}
	return nil
}

// List returns the configured ListPage or the in-memory contents when the
// page is unset, capped by p.PageSize.
func (f *fakeVentaRepo) List(_ context.Context, p outbound.ListParams, _ outbound.ListVentasFilters) (outbound.Page[*domain.Venta], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ListErr != nil {
		return outbound.Page[*domain.Venta]{}, f.ListErr
	}
	if f.ListPage.Items != nil || f.ListPage.NextCursor != "" {
		return f.ListPage, nil
	}
	items := make([]*domain.Venta, 0, len(f.byID))
	for _, v := range f.byID {
		items = append(items, v)
	}
	if p.PageSize > 0 && len(items) > p.PageSize {
		items = items[:p.PageSize]
	}
	return outbound.Page[*domain.Venta]{Items: items}, nil
}

// InsertImagen records the call. The aggregate already carries the new
// imagen via Venta.AdjuntarImagen so we do not need to mutate the map.
func (f *fakeVentaRepo) InsertImagen(_ context.Context, _ uuid.UUID, _ *domain.Imagen) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.InsertImagenCalls++
	return f.InsertImagenErr
}

// DeleteImagen records the call. The aggregate already had its imagen
// pruned via Venta.EliminarImagen by the time we get here.
func (f *fakeVentaRepo) DeleteImagen(_ context.Context, _, _ uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.DeleteImagenCalls++
	return f.DeleteImagenErr
}

// UpdateHeader rewrites the entry using the same map; reuses UpdateErr.
func (f *fakeVentaRepo) UpdateHeader(_ context.Context, v *domain.Venta) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.UpdateErr != nil {
		return f.UpdateErr
	}
	if _, ok := f.byID[v.ID()]; !ok {
		return domain.ErrVentaNotFound
	}
	f.byID[v.ID()] = v
	return nil
}

// UpdateCliente rewrites the entry; reuses UpdateErr.
func (f *fakeVentaRepo) UpdateCliente(_ context.Context, v *domain.Venta) error {
	return f.UpdateHeader(context.Background(), v)
}

// ReplaceProductos rewrites the entry; reuses UpdateErr.
func (f *fakeVentaRepo) ReplaceProductos(_ context.Context, v *domain.Venta) error {
	return f.UpdateHeader(context.Background(), v)
}

// ReplaceCombos rewrites the entry; reuses UpdateErr.
func (f *fakeVentaRepo) ReplaceCombos(_ context.Context, v *domain.Venta) error {
	return f.UpdateHeader(context.Background(), v)
}

// ReplaceVendedores rewrites the entry; reuses UpdateErr.
func (f *fakeVentaRepo) ReplaceVendedores(_ context.Context, v *domain.Venta) error {
	return f.UpdateHeader(context.Background(), v)
}

// fakeClienteChecker is an in-memory outbound.ClienteExistenceChecker.
// Behavior is controlled by the Exists field — true accepts any id, the
// IDs slice restricts which ids are valid.
type fakeClienteChecker struct {
	mu     sync.Mutex
	calls  int
	exists bool
	ids    map[int]struct{}
	err    error
}

func newFakeClienteChecker(exists bool, ids ...int) *fakeClienteChecker {
	out := &fakeClienteChecker{exists: exists, ids: map[int]struct{}{}}
	for _, id := range ids {
		out.ids[id] = struct{}{}
	}
	return out
}

func (f *fakeClienteChecker) Exists(_ context.Context, id int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	if len(f.ids) > 0 {
		_, ok := f.ids[id]
		return ok, nil
	}
	return f.exists, nil
}

// fakeClienteZonaReader is an in-memory outbound.ClienteZonaReader.
// ZonaID is the zona returned for any clienteID when ZonaNil is false.
// When ZonaNil is true, ZonaDeCliente returns (nil, nil) — simulating a
// Microsip cliente with a NULL ZONA_CLIENTE_ID (no zona constraint).
// Err overrides both paths with an error.
type fakeClienteZonaReader struct {
	mu      sync.Mutex
	calls   int
	ZonaID  int
	ZonaNil bool
	Err     error
}

func newFakeClienteZonaReader(zonaID int) *fakeClienteZonaReader {
	return &fakeClienteZonaReader{ZonaID: zonaID}
}

func (f *fakeClienteZonaReader) ZonaDeCliente(_ context.Context, _ int) (*int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.Err != nil {
		return nil, f.Err
	}
	if f.ZonaNil {
		return nil, nil
	}
	z := f.ZonaID
	return &z, nil
}

func (f *fakeClienteZonaReader) callsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakeUsuarioChecker is an in-memory outbound.VendedorUsuarioExistenceChecker.
// The known set decides which uuids are present in MSP_USUARIOS; anything
// outside it is returned as missing. Calls counts invocations so tests can
// assert the checker was (or wasn't) consulted.
type fakeUsuarioChecker struct {
	mu    sync.Mutex
	calls int
	known map[uuid.UUID]struct{}
	err   error
}

func newFakeUsuarioChecker(known ...uuid.UUID) *fakeUsuarioChecker {
	out := &fakeUsuarioChecker{known: map[uuid.UUID]struct{}{}}
	for _, id := range known {
		out.known[id] = struct{}{}
	}
	return out
}

func (f *fakeUsuarioChecker) MissingIDs(_ context.Context, ids []uuid.UUID) ([]uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	missing := make([]uuid.UUID, 0)
	for _, id := range ids {
		if _, ok := f.known[id]; !ok {
			missing = append(missing, id)
		}
	}
	return missing, nil
}
