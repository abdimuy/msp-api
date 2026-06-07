package failedintenthttp_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	failedintenthttp "github.com/abdimuy/msp-api/internal/platform/failedintent/http"
	"github.com/abdimuy/msp-api/internal/platform/idempotency"
)

// ─── memoryStore ──────────────────────────────────────────────────────────────

// memoryStore is an in-memory failedintent.Store for handler tests.
type memoryStore struct {
	mu      sync.Mutex
	intents map[uuid.UUID]failedintent.Intent
	saveErr error
	getErr  error
	listErr error
	updErr  error
	incrErr error
}

func newMemoryStore() *memoryStore {
	return &memoryStore{intents: make(map[uuid.UUID]failedintent.Intent)}
}

func (m *memoryStore) Save(_ context.Context, i failedintent.Intent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveErr != nil {
		return m.saveErr
	}
	// No overwrite on duplicate id.
	if _, exists := m.intents[i.ID]; exists {
		return nil
	}
	m.intents[i.ID] = i
	return nil
}

func (m *memoryStore) Get(_ context.Context, id uuid.UUID) (*failedintent.Intent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	i, ok := m.intents[id]
	if !ok {
		return nil, nil //nolint:nilnil // (nil, nil) means "not found", matches Store contract
	}
	cp := i
	return &cp, nil
}

func (m *memoryStore) List(_ context.Context, p failedintent.ListParams) (failedintent.Page[failedintent.Intent], error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.listErr != nil {
		return failedintent.Page[failedintent.Intent]{}, m.listErr
	}

	all := make([]failedintent.Intent, 0, len(m.intents))
	for _, i := range m.intents {
		if p.Status != "" && i.Status != p.Status {
			continue
		}
		// Cursor filtering: skip rows that are "before" the cursor
		// (received_at DESC, id DESC means we keep rows strictly earlier).
		if !p.CursorReceivedAt.IsZero() {
			if i.ReceivedAt.After(p.CursorReceivedAt) {
				continue
			}
			if i.ReceivedAt.Equal(p.CursorReceivedAt) && compareUUID(i.ID, p.CursorID) >= 0 {
				continue
			}
		}
		all = append(all, i)
	}

	// UsuarioID filter: applied after status/cursor filters.
	if p.UsuarioID != nil {
		filtered := all[:0]
		for _, it := range all {
			if it.UsuarioID != nil && *it.UsuarioID == *p.UsuarioID {
				filtered = append(filtered, it)
			}
		}
		all = filtered
	}

	// Sort by ReceivedAt DESC, ID DESC.
	sort.Slice(all, func(a, b int) bool {
		if !all[a].ReceivedAt.Equal(all[b].ReceivedAt) {
			return all[a].ReceivedAt.After(all[b].ReceivedAt)
		}
		return compareUUID(all[a].ID, all[b].ID) > 0
	})

	pageSize := p.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}

	// PageSize+1 trick.
	hasMore := len(all) > pageSize
	if hasMore {
		all = all[:pageSize]
	}

	page := failedintent.Page[failedintent.Intent]{
		Items:   all,
		HasMore: hasMore,
	}
	if hasMore && len(all) > 0 {
		last := all[len(all)-1]
		page.NextReceivedAt = last.ReceivedAt
		page.NextID = last.ID
	}
	return page, nil
}

func (m *memoryStore) UpdateStatus(
	_ context.Context,
	id uuid.UUID,
	expected, next failedintent.Status,
	resolvedBy uuid.UUID,
	notes string,
	now time.Time,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.updErr != nil {
		return m.updErr
	}

	i, ok := m.intents[id]
	if !ok {
		return apperror.NewConflict("failed_intent_status_conflict", "el estado del intento no coincide")
	}
	if i.Status != expected {
		return apperror.NewConflict("failed_intent_status_conflict", "el estado del intento no coincide")
	}

	i.Status = next
	i.ResolvedBy = &resolvedBy
	i.ResolvedAt = &now
	i.Notes = notes
	m.intents[id] = i
	return nil
}

func (m *memoryStore) IncrementRetry(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.incrErr != nil {
		return m.incrErr
	}
	i, ok := m.intents[id]
	if !ok {
		return nil
	}
	i.RetryCount++
	m.intents[id] = i
	return nil
}

func (m *memoryStore) PurgeOlderThan(_ context.Context, _ time.Time) (failedintent.PurgeResult, error) {
	return failedintent.PurgeResult{}, nil
}

func (m *memoryStore) ReferencedPaths(_ context.Context) ([]string, error) {
	return nil, nil
}

// compareUUID compares two UUIDs lexicographically. Returns -1, 0, or 1.
func compareUUID(a, b uuid.UUID) int {
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

// ─── fakeDispatcher ───────────────────────────────────────────────────────────

// fakeDispatcher records calls and lets tests script the response.
type fakeDispatcher struct {
	mu             sync.Mutex
	received       []*http.Request
	receivedBodies [][]byte
	receivedUsers  []auth.CurrentUser
	respondStatus  int
	respondBody    []byte
}

func (f *fakeDispatcher) Dispatch(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	cu, _ := auth.CurrentUserFromContext(r.Context())
	f.mu.Lock()
	f.received = append(f.received, r)
	f.receivedBodies = append(f.receivedBodies, body)
	f.receivedUsers = append(f.receivedUsers, cu)
	f.mu.Unlock()
	status := f.respondStatus
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(f.respondBody)
}

func (f *fakeDispatcher) lastRequest() *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.received) == 0 {
		return nil
	}
	return f.received[len(f.received)-1]
}

func (f *fakeDispatcher) lastBody() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.receivedBodies) == 0 {
		return nil
	}
	return f.receivedBodies[len(f.receivedBodies)-1]
}

func (f *fakeDispatcher) lastUser() auth.CurrentUser {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.receivedUsers) == 0 {
		return auth.CurrentUser{}
	}
	return f.receivedUsers[len(f.receivedUsers)-1]
}

func (f *fakeDispatcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.received)
}

// ─── stubUsuarioLookup ────────────────────────────────────────────────────────

// stubUsuarioLookup returns a canned auth.CurrentUser (or error).
type stubUsuarioLookup struct {
	user auth.CurrentUser
	err  error
}

func (s *stubUsuarioLookup) BuildCurrentUserByID(_ context.Context, _ uuid.UUID) (auth.CurrentUser, error) {
	return s.user, s.err
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// newRouter builds a chi router with the admin handlers mounted directly
// (without RequirePermission) so tests can focus on handler logic.
// When cu is non-nil, its value is planted into every request context.
func newRouter(t *testing.T, svc *failedintenthttp.Service, cu *auth.CurrentUser) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	if cu != nil {
		user := *cu
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(auth.PlantCurrentUser(req.Context(), user)))
			})
		})
	}
	r.Get("/", svc.Listar)
	r.Get("/{id}", svc.Obtener)
	r.Patch("/{id}/resolve", svc.Resolver)
	r.Post("/{id}/replay", svc.Replay)
	r.Post("/{id}/replay-with", svc.ReplayWith)
	return r
}

// newMeRouter builds a bare chi router with only MeListar mounted.
// When cu is non-nil, its value is planted into every request context.
func newMeRouter(t *testing.T, svc *failedintenthttp.Service, cu *auth.CurrentUser) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	if cu != nil {
		user := *cu
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(auth.PlantCurrentUser(req.Context(), user)))
			})
		})
	}
	r.Get("/", svc.MeListar)
	return r
}

// fixedClock returns a function that always returns t.
func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

// fixedID returns a function that always returns id.
func fixedID(id uuid.UUID) func() uuid.UUID { return func() uuid.UUID { return id } }

// defaultCU returns a CurrentUser with a known ID for planting in requests.
func defaultCU() auth.CurrentUser {
	return auth.CurrentUser{
		ID:          uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		FirebaseUID: "fbuid-test",
		Email:       "test@example.com",
		Nombre:      "Tester",
		Permisos:    []string{string(auth.PermFailedIntentsVer), string(auth.PermFailedIntentsResolver)},
	}
}

// seedIntent inserts a ready-made intent into the store.
func seedIntent(t *testing.T, store *memoryStore, i failedintent.Intent) failedintent.Intent {
	t.Helper()
	require.NoError(t, store.Save(context.Background(), i))
	return i
}

// makeIntent builds a minimal valid intent for seeding.
func makeIntent(id uuid.UUID, receivedAt time.Time) failedintent.Intent {
	userID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	return failedintent.Intent{
		ID:             id,
		ReceivedAt:     receivedAt,
		Method:         http.MethodPost,
		Path:           "/v2/ventas",
		FirebaseUID:    "fbuid-original",
		UsuarioID:      &userID,
		IdempotencyKey: "idem-key-001",
		RequestID:      uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc"),
		Body:           json.RawMessage(`{"nombre":"test"}`),
		BodyTruncated:  false,
		HTTPStatus:     422,
		ErrorCode:      "some_error",
		ErrorMessage:   "algún error",
		RetryCount:     0,
		Status:         failedintent.StatusNew,
	}
}

// problemBody is the minimal projection of an RFC 9457 Problem Details doc.
type problemBody struct {
	Status int    `json:"status"`
	Code   string `json:"code"`
}

func parseProblem(t *testing.T, body []byte) problemBody {
	t.Helper()
	var p problemBody
	require.NoError(t, json.Unmarshal(body, &p), "response body: %s", body)
	return p
}

// ─── Listar ───────────────────────────────────────────────────────────────────

func TestListar_EmptyStore_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp failedintenthttp.ListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Empty(t, resp.Items)
	assert.False(t, resp.HasMore)
	assert.Empty(t, resp.NextCursor)
}

func TestListar_FilterByStatus(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC().Truncate(time.Second)
	id1 := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	id2 := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	i1 := makeIntent(id1, now.Add(-2*time.Minute))
	i1.Status = failedintent.StatusNew

	i2 := makeIntent(id2, now.Add(-1*time.Minute))
	i2.Status = failedintent.StatusIgnored

	seedIntent(t, store, i1)
	seedIntent(t, store, i2)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/?status=ignored", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp failedintenthttp.ListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 1)
	assert.Equal(t, id2.String(), resp.Items[0].ID)
}

func TestListar_InvalidStatus_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/?status=not_a_real_status", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "invalid_status", p.Code)
}

func TestListar_InvalidCursor_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/?cursor=!!!notbase64!!!", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "invalid_cursor", p.Code)
}

func TestListar_PageSizeClamped(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	// Seed 110 intents so we exceed both 999 and 100.
	baseTime := time.Now().UTC()
	for i := range 110 {
		id := uuid.New()
		intent := makeIntent(id, baseTime.Add(-time.Duration(i)*time.Second))
		require.NoError(t, store.Save(context.Background(), intent))
	}

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/?page_size=999", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp failedintenthttp.ListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.LessOrEqual(t, len(resp.Items), 100, "page_size=999 must be clamped to 100")
	assert.True(t, resp.HasMore)
}

// ─── Obtener ──────────────────────────────────────────────────────────────────

func TestObtener_Existing_ReturnsDTO(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC()
	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	intent := makeIntent(id, now)
	seedIntent(t, store, intent)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/"+id.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var dto failedintenthttp.IntentDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
	assert.Equal(t, id.String(), dto.ID)
	assert.Equal(t, intent.Method, dto.Method)
	assert.Equal(t, intent.Path, dto.Path)
	assert.Equal(t, string(intent.Status), dto.Status)
}

func TestObtener_BlobIntent_ExposesHasBlobFlag(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC()
	id := uuid.MustParse("33333333-3333-3333-3333-bbbbbbbbbbbb")
	intent := makeIntent(id, now)
	// Simulate a captured multipart upload: blob persisted, JSON body empty.
	intent.Body = nil
	intent.BodyBlobPath = "/var/blobs/intents/33/blob.bin"
	intent.BodyContentType = "multipart/form-data; boundary=----WebKitFormBoundaryxyz"
	seedIntent(t, store, intent)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/"+id.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var dto failedintenthttp.IntentDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
	assert.True(t, dto.HasBlob, "blob intent must expose has_blob=true")
	assert.Equal(t, intent.BodyContentType, dto.BodyContentType,
		"blob intent must expose body_content_type to the UI")
}

func TestObtener_JSONIntent_HasBlobFalse(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC()
	id := uuid.MustParse("33333333-3333-3333-3333-cccccccccccc")
	intent := makeIntent(id, now)
	require.Empty(t, intent.BodyBlobPath)
	require.Empty(t, intent.BodyContentType)
	seedIntent(t, store, intent)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/"+id.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var dto failedintenthttp.IntentDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
	assert.False(t, dto.HasBlob, "JSON intent must report has_blob=false")
	assert.Empty(t, dto.BodyContentType,
		"JSON intent must not expose body_content_type")
}

func TestObtener_NotFound_Returns404(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	id := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	req := httptest.NewRequest(http.MethodGet, "/"+id.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "failed_intent_not_found", p.Code)
}

func TestObtener_InvalidUUID_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "invalid_intent_id", p.Code)
}

// ─── Resolver ─────────────────────────────────────────────────────────────────

func TestResolver_ValidIgnored_ReturnsUpdatedDTO(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC()
	id := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	intent := makeIntent(id, now)
	seedIntent(t, store, intent)

	clock := fixedClock(now)
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, clock, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	body := `{"status":"ignored","notes":"falsa alarma"}`
	req := httptest.NewRequest(http.MethodPatch, "/"+id.String()+"/resolve",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var dto failedintenthttp.IntentDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
	assert.Equal(t, "ignored", dto.Status)
	assert.Equal(t, "falsa alarma", dto.Notes)
	require.NotNil(t, dto.ResolvedBy)
	assert.Equal(t, cu.ID.String(), *dto.ResolvedBy)
}

func TestResolver_ValidResolvedManual_ReturnsUpdatedDTO(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC()
	id := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	intent := makeIntent(id, now)
	seedIntent(t, store, intent)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	body := `{"status":"resolved_manual","notes":"arreglado manualmente"}`
	req := httptest.NewRequest(http.MethodPatch, "/"+id.String()+"/resolve",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var dto failedintenthttp.IntentDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
	assert.Equal(t, "resolved_manual", dto.Status)
}

func TestResolver_InvalidStatus_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	intent := makeIntent(id, time.Now().UTC())
	seedIntent(t, store, intent)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	// "retried_ok" is a valid Status but not allowed for manual resolve.
	body := `{"status":"retried_ok"}`
	req := httptest.NewRequest(http.MethodPatch, "/"+id.String()+"/resolve",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "invalid_resolve_status", p.Code)
}

func TestResolver_NotesTooLong_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("88888888-8888-8888-8888-888888888888")
	intent := makeIntent(id, time.Now().UTC())
	seedIntent(t, store, intent)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	notes := strings.Repeat("a", 501) // 501 runes — one over the 500-rune limit
	payload := map[string]string{"status": "ignored", "notes": notes}
	buf, err := json.Marshal(payload)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPatch, "/"+id.String()+"/resolve",
		bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "notes_too_long", p.Code)
}

func TestResolver_NoCurrentUser_Returns401(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	intent := makeIntent(id, time.Now().UTC())
	seedIntent(t, store, intent)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	// Do NOT plant a CurrentUser — pass nil.
	r := newRouter(t, svc, nil)

	body := `{"status":"ignored"}`
	req := httptest.NewRequest(http.MethodPatch, "/"+id.String()+"/resolve",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "unauthenticated", p.Code)
}

func TestResolver_OptimisticConflict_Returns409(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	intent := makeIntent(id, time.Now().UTC())
	intent.Status = failedintent.StatusIgnored // Already terminal
	seedIntent(t, store, intent)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	// PATCH with expected=StatusNew, but store has StatusIgnored → conflict.
	body := `{"status":"ignored"}`
	req := httptest.NewRequest(http.MethodPatch, "/"+id.String()+"/resolve",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "failed_intent_status_conflict", p.Code)
}

func TestResolver_UnknownField_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")
	intent := makeIntent(id, time.Now().UTC())
	seedIntent(t, store, intent)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	body := `{"status":"ignored","extra":"hi"}`
	req := httptest.NewRequest(http.MethodPatch, "/"+id.String()+"/resolve",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "invalid_request_body", p.Code)
}

// ─── Replay ───────────────────────────────────────────────────────────────────

func TestReplay_Success_TransitionsToRetriedOK(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC()
	id := uuid.MustParse("a1a1a1a1-a1a1-a1a1-a1a1-a1a1a1a1a1a1")

	userID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	intent := makeIntent(id, now)
	intent.UsuarioID = &userID
	intent.Body = json.RawMessage(`{"orden":"test"}`)
	intent.IdempotencyKey = "idem-key-001"
	seedIntent(t, store, intent)

	expectedCU := auth.CurrentUser{
		ID:          userID,
		FirebaseUID: "fbuid-original",
		Email:       "original@example.com",
		Nombre:      "Original User",
		Permisos:    []string{string(auth.PermVentasCrear)},
	}
	lookup := &stubUsuarioLookup{user: expectedCU}

	dispatcher := &fakeDispatcher{respondStatus: http.StatusCreated}

	svc := failedintenthttp.NewService(store, dispatcher, lookup, nil, fixedClock(now), nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Verify response body.
	var resp failedintenthttp.ReplayResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "retried_ok", resp.Outcome)
	assert.Equal(t, http.StatusCreated, resp.ReplayHTTPStatus)

	// Verify dispatcher received the right request.
	require.Equal(t, 1, dispatcher.callCount())
	dispatched := dispatcher.lastRequest()
	require.NotNil(t, dispatched)

	// Method and path match the intent.
	assert.Equal(t, intent.Method, dispatched.Method)
	assert.Equal(t, intent.Path, dispatched.URL.Path)

	// Body matches intent body.
	assert.JSONEq(t, `{"orden":"test"}`, string(dispatcher.lastBody()))

	// X-Internal-Replay header equals the intent ID.
	assert.Equal(t, id.String(), dispatched.Header.Get(failedintent.HeaderInternalReplay))

	// Idempotency-Key header is freshly generated for the replay (decoupled
	// from the intent's captured key so the idempotency middleware does not
	// short-circuit with the cached failure response).
	freshKey := dispatched.Header.Get(idempotency.HeaderKey)
	assert.NotEmpty(t, freshKey)
	assert.NotEqual(t, intent.IdempotencyKey, freshKey)

	// CurrentUser is planted on the dispatched request.
	plantedUser := dispatcher.lastUser()
	assert.Equal(t, expectedCU.ID, plantedUser.ID)

	// Intent status transitioned to retried_ok.
	stored, err := store.Get(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, failedintent.StatusRetriedOK, stored.Status)

	// RetryCount incremented by 1.
	assert.Equal(t, 1, stored.RetryCount)
}

func TestReplay_FailureStatus_TransitionsToRetriedFail(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC()
	id := uuid.MustParse("b2b2b2b2-b2b2-b2b2-b2b2-b2b2b2b2b2b2")
	intent := makeIntent(id, now)
	seedIntent(t, store, intent)

	expectedCU := auth.CurrentUser{ID: *intent.UsuarioID}
	lookup := &stubUsuarioLookup{user: expectedCU}

	dispatcher := &fakeDispatcher{respondStatus: http.StatusUnprocessableEntity}

	svc := failedintenthttp.NewService(store, dispatcher, lookup, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp failedintenthttp.ReplayResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "retried_fail", resp.Outcome)
	assert.Equal(t, http.StatusUnprocessableEntity, resp.ReplayHTTPStatus)

	// Intent status transitioned to retried_fail.
	stored, err := store.Get(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, failedintent.StatusRetriedFail, stored.Status)
}

func TestReplay_IntentNotFound_Returns404(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	missingID := uuid.MustParse("c3c3c3c3-c3c3-c3c3-c3c3-c3c3c3c3c3c3")
	req := httptest.NewRequest(http.MethodPost, "/"+missingID.String()+"/replay", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "failed_intent_not_found", p.Code)
}

func TestReplay_IntentMissingUsuario_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("d4d4d4d4-d4d4-d4d4-d4d4-d4d4d4d4d4d4")
	intent := makeIntent(id, time.Now().UTC())
	intent.UsuarioID = nil // No associated user.
	seedIntent(t, store, intent)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "intent_has_no_usuario", p.Code)
}

func TestReplay_UsuarioLookupError_Surfaces(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("e5e5e5e5-e5e5-e5e5-e5e5-e5e5e5e5e5e5")
	intent := makeIntent(id, time.Now().UTC())
	seedIntent(t, store, intent)

	// BuildCurrentUserByID returns a 403 Forbidden.
	lookup := &stubUsuarioLookup{
		err: apperror.NewForbidden("user_inactive", "usuario inactivo"),
	}

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, lookup, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "user_inactive", p.Code)
}

func TestReplay_TerminalIntent_DoesNotMutateStatus(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("f6f6f6f6-f6f6-f6f6-f6f6-f6f6f6f6f6f6")
	intent := makeIntent(id, time.Now().UTC())
	intent.Status = failedintent.StatusRetriedFail // Terminal state.
	seedIntent(t, store, intent)

	expectedCU := auth.CurrentUser{ID: *intent.UsuarioID}
	lookup := &stubUsuarioLookup{user: expectedCU}
	dispatcher := &fakeDispatcher{respondStatus: http.StatusOK}

	svc := failedintenthttp.NewService(store, dispatcher, lookup, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Dispatcher was called.
	assert.Equal(t, 1, dispatcher.callCount())

	// Status must NOT have changed from terminal.
	stored, err := store.Get(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, failedintent.StatusRetriedFail, stored.Status,
		"terminal intent status must not be mutated by re-replay")
}

// TestReplay_AlwaysGeneratesFreshIdempotencyKey enforces that every
// replay mints a new idempotency key, decoupled from the intent's
// captured key. Reusing the captured key either short-circuits with the
// cached failure (defeating the purpose of replay) or — once the body
// bytes diverge by even one whitespace character — yields a 409
// idempotency_key_mismatch. The replay is semantically a new operation
// distinct from the user's own retries.
func TestReplay_AlwaysGeneratesFreshIdempotencyKey(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("a7a7a7a7-a7a7-a7a7-a7a7-a7a7a7a7a7a7")
	intent := makeIntent(id, time.Now().UTC())
	intent.IdempotencyKey = "user-original-key-001" // Intent HAS a captured key.
	seedIntent(t, store, intent)

	expectedCU := auth.CurrentUser{ID: *intent.UsuarioID}
	lookup := &stubUsuarioLookup{user: expectedCU}
	dispatcher := &fakeDispatcher{respondStatus: http.StatusOK}

	generatedID := uuid.MustParse("12345678-1234-1234-1234-123456789012")
	svc := failedintenthttp.NewService(store, dispatcher, lookup, nil, nil, fixedID(generatedID))
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	dispatched := dispatcher.lastRequest()
	require.NotNil(t, dispatched)

	idemKey := dispatched.Header.Get(idempotency.HeaderKey)
	assert.NotEmpty(t, idemKey, "replay must always set an Idempotency-Key")
	assert.NotEqual(t, intent.IdempotencyKey, idemKey,
		"replay key must be FRESH even when the intent already carries one")
	assert.Equal(t, generatedID.String(), idemKey,
		"the fresh key must come from the injected uuid generator")

	// X-Internal-Replay must still be set.
	assert.Equal(t, id.String(), dispatched.Header.Get(failedintent.HeaderInternalReplay))
}

// ─── Additional coverage tests ────────────────────────────────────────────────

// TestListar_ValidCursor_DecodesAndPaginates exercises the decodeCursor happy
// path by hitting the list endpoint with the cursor returned by the first page.
func TestListar_ValidCursor_DecodesAndPaginates(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	baseTime := time.Now().UTC()

	// Seed 3 intents — we'll fetch page size 2, then use the cursor.
	ids := []uuid.UUID{
		uuid.MustParse("11111111-0000-0000-0000-000000000001"),
		uuid.MustParse("11111111-0000-0000-0000-000000000002"),
		uuid.MustParse("11111111-0000-0000-0000-000000000003"),
	}
	for i, id := range ids {
		intent := makeIntent(id, baseTime.Add(-time.Duration(i)*time.Second))
		require.NoError(t, store.Save(context.Background(), intent))
	}

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	// Page 1: page_size=2.
	req1 := httptest.NewRequest(http.MethodGet, "/?page_size=2", nil)
	rec1 := httptest.NewRecorder()
	r.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code, rec1.Body.String())

	var resp1 failedintenthttp.ListResponse
	require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &resp1))
	require.Len(t, resp1.Items, 2)
	assert.True(t, resp1.HasMore)
	require.NotEmpty(t, resp1.NextCursor)

	// Page 2: use the cursor from page 1.
	req2 := httptest.NewRequest(http.MethodGet, "/?page_size=2&cursor="+resp1.NextCursor, nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code, rec2.Body.String())

	var resp2 failedintenthttp.ListResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	assert.NotEmpty(t, resp2.Items)
	assert.False(t, resp2.HasMore)
}

// TestListar_InvalidPageSize_FallsBackToDefault exercises the parsePageSize
// invalid-input path (non-numeric string → default of 20).
func TestListar_InvalidPageSize_FallsBackToDefault(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/?page_size=notanumber", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Should succeed — invalid page_size falls back to 20.
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp failedintenthttp.ListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotNil(t, resp.Items)
}

// TestListar_ZeroPageSize_FallsBackToDefault exercises the parsePageSize
// n<=0 guard path.
func TestListar_ZeroPageSize_FallsBackToDefault(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/?page_size=0", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// TestListar_StoreError_Propagates exercises the Listar store-error path.
func TestListar_StoreError_Propagates(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	store.listErr = apperror.NewInternal("db_error", "error de base de datos")

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
}

// TestObtener_StoreError_Propagates exercises the Obtener store-error path.
func TestObtener_StoreError_Propagates(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	store.getErr = apperror.NewInternal("db_error", "error de base de datos")

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	id := uuid.MustParse("a0a0a0a0-a0a0-a0a0-a0a0-a0a0a0a0a0a0")
	req := httptest.NewRequest(http.MethodGet, "/"+id.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
}

// TestResolver_GetAfterUpdateError exercises the Resolver Get-after-UpdateStatus error path.
func TestResolver_GetAfterUpdateError(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("b0b0b0b0-b0b0-b0b0-b0b0-b0b0b0b0b0b0")
	intent := makeIntent(id, time.Now().UTC())
	seedIntent(t, store, intent)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	// Set getErr AFTER seeding but BEFORE the request, so UpdateStatus succeeds
	// but the subsequent Get fails.
	store.mu.Lock()
	store.getErr = apperror.NewInternal("db_error", "error de base de datos")
	store.mu.Unlock()

	body := `{"status":"ignored"}`
	req := httptest.NewRequest(http.MethodPatch, "/"+id.String()+"/resolve",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// UpdateStatus uses a map lookup, not getErr, so that succeeds.
	// The subsequent Get fails with the injected error.
	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
}

// TestReplay_BodyPreview_Truncation exercises the bodyPreview truncation path
// by using a fakeDispatcher that writes > 1024 bytes.
func TestReplay_BodyPreview_Truncation(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("c0c0c0c0-c0c0-c0c0-c0c0-c0c0c0c0c0c0")
	intent := makeIntent(id, time.Now().UTC())
	seedIntent(t, store, intent)

	expectedCU := auth.CurrentUser{ID: *intent.UsuarioID}
	lookup := &stubUsuarioLookup{user: expectedCU}

	// Respond with a body larger than 1024 bytes.
	largeBody := []byte(strings.Repeat("x", 2048))
	dispatcher := &fakeDispatcher{
		respondStatus: http.StatusOK,
		respondBody:   largeBody,
	}

	svc := failedintenthttp.NewService(store, dispatcher, lookup, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp failedintenthttp.ReplayResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	// Preview should be truncated to 1024 bytes.
	assert.LessOrEqual(t, len(resp.ReplayBodyPreview), 1024)
}

// TestReplay_TryUpdateStatus_ConflictAfterConcurrentChange exercises the
// tryUpdateStatus conflict-log path: replay succeeds but a concurrent actor
// changed the status between the initial Get and the UpdateStatus call.
func TestReplay_TryUpdateStatus_ConflictAfterConcurrentChange(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("d0d0d0d0-d0d0-d0d0-d0d0-d0d0d0d0d0d0")
	intent := makeIntent(id, time.Now().UTC())
	intent.Status = failedintent.StatusNew
	seedIntent(t, store, intent)

	// Make UpdateStatus return a conflict error to exercise that branch in
	// tryUpdateStatus without using a terminal originalStatus.
	store.mu.Lock()
	store.updErr = apperror.NewConflict("failed_intent_status_conflict", "el estado del intento no coincide")
	store.mu.Unlock()

	expectedCU := auth.CurrentUser{ID: *intent.UsuarioID}
	lookup := &stubUsuarioLookup{user: expectedCU}
	dispatcher := &fakeDispatcher{respondStatus: http.StatusOK}

	svc := failedintenthttp.NewService(store, dispatcher, lookup, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// The handler still returns 200 — status conflict in tryUpdateStatus is
	// logged at warn level and discarded.
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp failedintenthttp.ReplayResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "retried_ok", resp.Outcome)
}

// TestReplay_TryUpdateStatus_GenericError exercises the tryUpdateStatus
// generic-error-log path: replay succeeds but UpdateStatus fails with an
// error that is not a status conflict.
func TestReplay_TryUpdateStatus_GenericError(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("e0e0e0e0-e0e0-e0e0-e0e0-e0e0e0e0e0e0")
	intent := makeIntent(id, time.Now().UTC())
	intent.Status = failedintent.StatusNew
	seedIntent(t, store, intent)

	store.mu.Lock()
	store.updErr = apperror.NewInternal("db_error", "error de base de datos")
	store.mu.Unlock()

	expectedCU := auth.CurrentUser{ID: *intent.UsuarioID}
	lookup := &stubUsuarioLookup{user: expectedCU}
	dispatcher := &fakeDispatcher{respondStatus: http.StatusOK}

	svc := failedintenthttp.NewService(store, dispatcher, lookup, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Even with a generic error, the handler returns 200.
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp failedintenthttp.ReplayResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "retried_ok", resp.Outcome)
}

// TestReplayWriter_Header exercises the replayWriter.Header() method via
// the full replay path, ensuring the internal response-recorder header map
// is accessible (the dispatcher writes a header that the replayWriter captures).
func TestReplayWriter_Header(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("f0f0f0f0-f0f0-f0f0-f0f0-f0f0f0f0f0f0")
	intent := makeIntent(id, time.Now().UTC())
	seedIntent(t, store, intent)

	expectedCU := auth.CurrentUser{ID: *intent.UsuarioID}
	lookup := &stubUsuarioLookup{user: expectedCU}

	// A dispatcher that sets a response header — this exercises rw.Header().
	headerSettingDispatcher := &headerDispatcher{status: http.StatusOK}

	svc := failedintenthttp.NewService(store, headerSettingDispatcher, lookup, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// headerDispatcher is a helper dispatcher that calls rw.Header().Set to
// ensure the replayWriter.Header() method gets exercised.
type headerDispatcher struct {
	status int
}

func (h *headerDispatcher) Dispatch(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("X-Test-Header", "value")
	w.WriteHeader(h.status)
}

// ─── Security sweep ───────────────────────────────────────────────────────────

// secProblemBody is the minimal projection of a Problem Details document used
// by the security sweep.
type secProblemBody struct {
	Status int    `json:"status"`
	Code   string `json:"code"`
}

// staticUUID is a known-good UUID used in path parameters for the security sweep.
const staticUUID = "00000000-0000-0000-0000-000000000001"

// TestSecurity_AdminRoutesRequirePermission exercises MountRouter end-to-end.
// It plants a CurrentUser with no permissions and expects 403, then plants
// a CurrentUser with the exact required permission and expects the permission
// gate to pass (handler may 404 since stores are empty, but must not 403/401).
func TestSecurity_AdminRoutesRequirePermission(t *testing.T) {
	t.Parallel()

	type routeCase struct {
		method       string
		path         string
		requiredPerm auth.Permission
	}

	cases := []routeCase{
		{http.MethodGet, "/", auth.PermFailedIntentsVer},
		{http.MethodGet, "/" + staticUUID, auth.PermFailedIntentsVer},
		{http.MethodPatch, "/" + staticUUID + "/resolve", auth.PermFailedIntentsResolver},
		{http.MethodPost, "/" + staticUUID + "/replay", auth.PermFailedIntentsResolver},
		{http.MethodPost, "/" + staticUUID + "/replay-with", auth.PermFailedIntentsResolver},
	}

	store := newMemoryStore()
	dispatcher := &fakeDispatcher{respondStatus: http.StatusOK}
	lookup := &stubUsuarioLookup{}
	svc := failedintenthttp.NewService(store, dispatcher, lookup, nil, nil, nil)

	for _, tc := range cases {
		tc := tc
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			t.Parallel()

			// ── Sub-case A: no permissions → must 403 ──────────────────
			t.Run("no_perms_403", func(t *testing.T) {
				t.Parallel()

				noPerm := auth.CurrentUser{
					ID:       uuid.New(),
					Permisos: []string{},
				}

				parent := chi.NewRouter()
				parent.Use(func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
						next.ServeHTTP(w, req.WithContext(auth.PlantCurrentUser(req.Context(), noPerm)))
					})
				})
				failedintenthttp.MountRouter(parent, svc)

				var body io.Reader
				if tc.method == http.MethodPatch || tc.method == http.MethodPost {
					body = strings.NewReader(`{}`)
				}
				req := httptest.NewRequest(tc.method, tc.path, body)
				if body != nil {
					req.Header.Set("Content-Type", "application/json")
				}
				rec := httptest.NewRecorder()
				parent.ServeHTTP(rec, req)

				require.Equal(t, http.StatusForbidden, rec.Code,
					"want 403 for %s %s with no perms; got %d body=%s",
					tc.method, tc.path, rec.Code, rec.Body.String())
				var pb secProblemBody
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &pb))
				assert.Equal(t, "permission_denied", pb.Code)
			})

			// ── Sub-case B: required permission → must NOT 403/401 ─────
			t.Run("with_required_perm_not_403", func(t *testing.T) {
				t.Parallel()

				withPerm := auth.CurrentUser{
					ID:       uuid.New(),
					Permisos: []string{string(tc.requiredPerm)},
				}

				parent := chi.NewRouter()
				parent.Use(func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
						next.ServeHTTP(w, req.WithContext(auth.PlantCurrentUser(req.Context(), withPerm)))
					})
				})
				failedintenthttp.MountRouter(parent, svc)

				var body io.Reader
				if tc.method == http.MethodPatch || tc.method == http.MethodPost {
					body = strings.NewReader(`{}`)
				}
				req := httptest.NewRequest(tc.method, tc.path, body)
				if body != nil {
					req.Header.Set("Content-Type", "application/json")
				}
				rec := httptest.NewRecorder()
				parent.ServeHTTP(rec, req)

				assert.NotEqual(t, http.StatusForbidden, rec.Code,
					"must NOT 403 for %s %s with required perm; got %d body=%s",
					tc.method, tc.path, rec.Code, rec.Body.String())
				assert.NotEqual(t, http.StatusUnauthorized, rec.Code,
					"must NOT 401 for %s %s with required perm; got %d body=%s",
					tc.method, tc.path, rec.Code, rec.Body.String())
			})
		})
	}
}

// ─── ReplayWith ───────────────────────────────────────────────────────────────

// TestReplayWith_Success_UsesCorrectedBody verifies that ReplayWith dispatches
// the corrected body (not the original captured body) and returns retried_ok.
func TestReplayWith_Success_UsesCorrectedBody(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC()
	id := uuid.MustParse("aa000001-0000-0000-0000-000000000001")

	userID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	intent := makeIntent(id, now)
	intent.UsuarioID = &userID
	intent.Body = json.RawMessage(`{"original":"body"}`)
	seedIntent(t, store, intent)

	expectedCU := auth.CurrentUser{ID: userID, FirebaseUID: "fbuid-original"}
	lookup := &stubUsuarioLookup{user: expectedCU}
	dispatcher := &fakeDispatcher{respondStatus: http.StatusCreated}

	svc := failedintenthttp.NewService(store, dispatcher, lookup, nil, fixedClock(now), nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	correctedBody := `{"body":{"nombre":"corrected","valor":42}}`
	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay-with",
		strings.NewReader(correctedBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp failedintenthttp.ReplayResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "retried_ok", resp.Outcome)
	assert.Equal(t, http.StatusCreated, resp.ReplayHTTPStatus)

	// Dispatcher must have received the CORRECTED body, not the original.
	require.Equal(t, 1, dispatcher.callCount())
	assert.JSONEq(t, `{"nombre":"corrected","valor":42}`, string(dispatcher.lastBody()),
		"dispatcher must receive the corrected body, not the original")

	// X-Internal-Replay must be set to the intent ID.
	dispatched := dispatcher.lastRequest()
	require.NotNil(t, dispatched)
	assert.Equal(t, id.String(), dispatched.Header.Get(failedintent.HeaderInternalReplay))

	// CurrentUser planted on dispatched request must be the original user.
	plantedUser := dispatcher.lastUser()
	assert.Equal(t, expectedCU.ID, plantedUser.ID)

	// Intent status must have transitioned to retried_ok.
	stored, err := store.Get(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, failedintent.StatusRetriedOK, stored.Status)
	assert.Equal(t, 1, stored.RetryCount)
}

// TestReplayWith_InvalidBody_Returns422 verifies that an empty body field
// returns 422 with code invalid_replay_body.
func TestReplayWith_InvalidBody_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("aa000002-0000-0000-0000-000000000002")
	intent := makeIntent(id, time.Now().UTC())
	seedIntent(t, store, intent)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	// body field is null — treated as empty/invalid.
	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay-with",
		strings.NewReader(`{"body":null}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "invalid_replay_body", p.Code)
}

// TestReplayWith_DecodeError_Returns422 verifies that malformed top-level JSON
// returns 422 with code invalid_request_body.
func TestReplayWith_DecodeError_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("aa000003-0000-0000-0000-000000000003")
	intent := makeIntent(id, time.Now().UTC())
	seedIntent(t, store, intent)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay-with",
		strings.NewReader(`{this is not json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "invalid_request_body", p.Code)
}

// TestReplayWith_UnknownField_Returns422 verifies that an unknown top-level
// field (DisallowUnknownFields) causes 422 with code invalid_request_body.
func TestReplayWith_UnknownField_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("aa000004-0000-0000-0000-000000000004")
	intent := makeIntent(id, time.Now().UTC())
	seedIntent(t, store, intent)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay-with",
		strings.NewReader(`{"body":{"k":"v"},"extra":"field"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "invalid_request_body", p.Code)
}

// TestReplayWith_IntentNotFound_Returns404 verifies that a missing intent ID
// returns 404 with code failed_intent_not_found.
func TestReplayWith_IntentNotFound_Returns404(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	missingID := uuid.MustParse("aa000005-0000-0000-0000-000000000005")
	req := httptest.NewRequest(http.MethodPost, "/"+missingID.String()+"/replay-with",
		strings.NewReader(`{"body":{"k":"v"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "failed_intent_not_found", p.Code)
}

// TestReplayWith_TerminalIntent_DoesNotMutateStatus verifies that re-replaying
// a terminal intent with ReplayWith still dispatches but does not change status.
func TestReplayWith_TerminalIntent_DoesNotMutateStatus(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("aa000006-0000-0000-0000-000000000006")
	intent := makeIntent(id, time.Now().UTC())
	intent.Status = failedintent.StatusRetriedFail // terminal
	seedIntent(t, store, intent)

	expectedCU := auth.CurrentUser{ID: *intent.UsuarioID}
	lookup := &stubUsuarioLookup{user: expectedCU}
	dispatcher := &fakeDispatcher{respondStatus: http.StatusOK}

	svc := failedintenthttp.NewService(store, dispatcher, lookup, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay-with",
		strings.NewReader(`{"body":{"corrected":true}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Dispatch must have been called.
	assert.Equal(t, 1, dispatcher.callCount())

	// Status must remain terminal (retried_fail).
	stored, err := store.Get(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, failedintent.StatusRetriedFail, stored.Status,
		"terminal intent status must not be mutated by re-replay-with")
}

// ─── MeListar ─────────────────────────────────────────────────────────────────

// TestMeListar_EmptyForUserWithNoFailures verifies that a user gets an empty
// page when the store has intents belonging to other users only.
func TestMeListar_EmptyForUserWithNoFailures(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC()

	otherUser := uuid.MustParse("cc000001-0000-0000-0000-000000000001")
	for i := range 3 {
		id := uuid.New()
		intent := makeIntent(id, now.Add(-time.Duration(i)*time.Second))
		intent.UsuarioID = &otherUser
		seedIntent(t, store, intent)
	}

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU() // cu.ID is different from otherUser
	r := newMeRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp failedintenthttp.ListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Empty(t, resp.Items, "user must see no intents when none belong to them")
	assert.False(t, resp.HasMore)
}

// TestMeListar_FiltersToCurrentUser verifies that MeListar returns only the
// intents owned by the authenticated user and ignores others'.
func TestMeListar_FiltersToCurrentUser(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC()

	cu := defaultCU()
	userA := cu.ID // aaaaaaaa-…
	userB := uuid.MustParse("cc000002-0000-0000-0000-000000000002")

	// Seed 3 intents for user A.
	for i := range 3 {
		id := uuid.New()
		intent := makeIntent(id, now.Add(-time.Duration(i)*time.Second))
		intent.UsuarioID = &userA
		seedIntent(t, store, intent)
	}
	// Seed 2 intents for user B.
	for i := range 2 {
		id := uuid.New()
		intent := makeIntent(id, now.Add(-time.Duration(i+10)*time.Second))
		intent.UsuarioID = &userB
		seedIntent(t, store, intent)
	}

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	r := newMeRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp failedintenthttp.ListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Len(t, resp.Items, 3, "must return exactly 3 intents for user A")
	for _, item := range resp.Items {
		require.NotNil(t, item.UsuarioID)
		assert.Equal(t, userA.String(), *item.UsuarioID)
	}
}

// TestMeListar_NoCurrentUser_Returns401 verifies that a request without a
// planted CurrentUser returns 401 with code unauthenticated.
func TestMeListar_NoCurrentUser_Returns401(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	// Pass nil — no CurrentUser planted.
	r := newMeRouter(t, svc, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "unauthenticated", p.Code)
}

// TestMeListar_InvalidStatus_Returns422 verifies that an invalid status filter
// returns 422 with code invalid_status.
func TestMeListar_InvalidStatus_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newMeRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/?status=not_valid", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "invalid_status", p.Code)
}

// TestMeListar_PageSizeClamped verifies that page_size=999 is clamped to 100.
func TestMeListar_PageSizeClamped(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC()
	cu := defaultCU()
	userID := cu.ID

	// Seed 110 intents for the authenticated user.
	for i := range 110 {
		id := uuid.New()
		intent := makeIntent(id, now.Add(-time.Duration(i)*time.Second))
		intent.UsuarioID = &userID
		seedIntent(t, store, intent)
	}

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	r := newMeRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/?page_size=999", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp failedintenthttp.ListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.LessOrEqual(t, len(resp.Items), 100, "page_size=999 must be clamped to 100")
	assert.True(t, resp.HasMore)
}

// ─── Group 1 — ReplayWith robustness ─────────────────────────────────────────

// TestReplayWith_AlwaysGeneratesFreshIdempotencyKey enforces the same
// "always fresh" contract as TestReplay_AlwaysGeneratesFreshIdempotencyKey
// for the corrected-body replay path. Reusing the captured key on a
// replay-with with a different body would yield a guaranteed 409
// idempotency_key_mismatch from the idempotency middleware.
func TestReplayWith_AlwaysGeneratesFreshIdempotencyKey(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC()
	id := uuid.MustParse("bb000001-0000-0000-0000-000000000001")
	intent := makeIntent(id, now)
	intent.IdempotencyKey = "user-original-key-002" // Intent HAS a captured key.
	seedIntent(t, store, intent)

	lookup := &stubUsuarioLookup{user: auth.CurrentUser{ID: *intent.UsuarioID}}
	dispatcher := &fakeDispatcher{respondStatus: http.StatusOK}

	svc := failedintenthttp.NewService(store, dispatcher, lookup, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay-with",
		strings.NewReader(`{"body":{"key":"value"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	dispatched := dispatcher.lastRequest()
	require.NotNil(t, dispatched, "dispatcher must have been called")

	idemKey := dispatched.Header.Get(idempotency.HeaderKey)
	assert.NotEmpty(t, idemKey, "replay-with must always set an Idempotency-Key")
	assert.NotEqual(t, intent.IdempotencyKey, idemKey,
		"replay-with key must be FRESH even when the intent already carries one")

	// The generated key must parse as a valid UUID.
	_, parseErr := uuid.Parse(idemKey)
	assert.NoError(t, parseErr, "generated Idempotency-Key must be a valid UUID, got: %q", idemKey)
}

// TestReplayWith_SetsInternalReplayHeader verifies that the dispatched request
// carries the X-Internal-Replay header set to the intent's ID.
func TestReplayWith_SetsInternalReplayHeader(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC()
	id := uuid.MustParse("bb000002-0000-0000-0000-000000000002")
	intent := makeIntent(id, now)
	seedIntent(t, store, intent)

	lookup := &stubUsuarioLookup{user: auth.CurrentUser{ID: *intent.UsuarioID}}
	dispatcher := &fakeDispatcher{respondStatus: http.StatusOK}

	svc := failedintenthttp.NewService(store, dispatcher, lookup, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay-with",
		strings.NewReader(`{"body":{"k":"v"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	dispatched := dispatcher.lastRequest()
	require.NotNil(t, dispatched)

	replayHeader := dispatched.Header.Get(failedintent.HeaderInternalReplay)
	assert.Equal(t, id.String(), replayHeader, "X-Internal-Replay must equal the intent ID")
	assert.NotEmpty(t, replayHeader, "X-Internal-Replay must not be empty")
}

// TestReplayWith_PlantsOriginalCurrentUser verifies that the dispatched request
// carries the CurrentUser resolved via UsuarioLookup (the original requester),
// not the admin who initiated the replay.
func TestReplayWith_PlantsOriginalCurrentUser(t *testing.T) {
	t.Parallel()

	knownID := uuid.MustParse("bb000003-0000-0000-0000-000000000003")
	store := newMemoryStore()
	now := time.Now().UTC()
	id := uuid.MustParse("bb000004-0000-0000-0000-000000000004")
	intent := makeIntent(id, now)
	intent.UsuarioID = &knownID
	seedIntent(t, store, intent)

	originalCU := auth.CurrentUser{
		ID:          knownID,
		FirebaseUID: "fb-known",
		Email:       "known@example.com",
		Nombre:      "Known User",
		Permisos:    []string{"ventas:crear"},
	}
	lookup := &stubUsuarioLookup{user: originalCU}
	dispatcher := &fakeDispatcher{respondStatus: http.StatusOK}

	svc := failedintenthttp.NewService(store, dispatcher, lookup, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay-with",
		strings.NewReader(`{"body":{"item":"test"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	plantedUser := dispatcher.lastUser()
	assert.Equal(t, knownID, plantedUser.ID, "planted user ID must match original requester")
	assert.Equal(t, "fb-known", plantedUser.FirebaseUID, "planted FirebaseUID must match original requester")
	require.Len(t, plantedUser.Permisos, 1, "planted Permisos count must match")
	assert.Equal(t, "ventas:crear", plantedUser.Permisos[0], "planted Permisos must match original requester")
}

// TestReplayWith_CallsIncrementRetry verifies that each ReplayWith call
// increments the intent's RetryCount.
func TestReplayWith_CallsIncrementRetry(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC()
	id := uuid.MustParse("bb000005-0000-0000-0000-000000000005")
	intent := makeIntent(id, now)
	intent.RetryCount = 0
	seedIntent(t, store, intent)

	lookup := &stubUsuarioLookup{user: auth.CurrentUser{ID: *intent.UsuarioID}}
	// Use a fresh store for UpdateStatus (terminal-guard bypassed by status=new).
	dispatcher := &fakeDispatcher{respondStatus: http.StatusUnprocessableEntity} // retried_fail keeps status=new guard working

	// First call.
	svc := failedintenthttp.NewService(store, dispatcher, lookup, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	sendReplayWith := func(t *testing.T) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay-with",
			strings.NewReader(`{"body":{"attempt":"yes"}}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}

	// Use updErr to prevent status update from failing on second call (status
	// will be retried_fail after first call — set to keep the store functional).
	sendReplayWith(t)
	stored, err := store.Get(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, 1, stored.RetryCount, "RetryCount must be 1 after first replay-with")

	// Reset status so second replay can proceed with IncrementRetry.
	store.mu.Lock()
	stored2 := store.intents[id]
	stored2.Status = failedintent.StatusNew
	store.intents[id] = stored2
	store.mu.Unlock()

	sendReplayWith(t)
	stored, err = store.Get(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, 2, stored.RetryCount, "RetryCount must be 2 after second replay-with")
}

// TestReplayWith_UsuarioLookupError_Surfaces verifies that when BuildCurrentUserByID
// returns a Forbidden error, ReplayWith responds 403 and does not dispatch.
func TestReplayWith_UsuarioLookupError_Surfaces(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("bb000006-0000-0000-0000-000000000006")
	intent := makeIntent(id, time.Now().UTC())
	seedIntent(t, store, intent)

	lookup := &stubUsuarioLookup{
		err: apperror.NewForbidden("user_inactive", "el usuario está inactivo"),
	}
	dispatcher := &fakeDispatcher{respondStatus: http.StatusOK}

	svc := failedintenthttp.NewService(store, dispatcher, lookup, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay-with",
		strings.NewReader(`{"body":{"x":1}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "user_inactive", p.Code)

	assert.Equal(t, 0, dispatcher.callCount(), "dispatcher must NOT be called when usuario lookup fails")
}

// TestReplayWith_IntentMissingUsuario_Returns422 verifies that an intent with no
// associated UsuarioID returns 422 with code intent_has_no_usuario.
func TestReplayWith_IntentMissingUsuario_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("bb000007-0000-0000-0000-000000000007")
	intent := makeIntent(id, time.Now().UTC())
	intent.UsuarioID = nil // no associated user
	seedIntent(t, store, intent)

	dispatcher := &fakeDispatcher{respondStatus: http.StatusOK}

	svc := failedintenthttp.NewService(store, dispatcher, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay-with",
		strings.NewReader(`{"body":{"k":"v"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "intent_has_no_usuario", p.Code)

	assert.Equal(t, 0, dispatcher.callCount(), "dispatcher must NOT be called when intent has no usuario")
}

// TestReplayWith_NullBody_Returns422 verifies that a payload with `"body": null`
// returns 422 with code invalid_replay_body and does not dispatch.
func TestReplayWith_NullBody_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	id := uuid.MustParse("bb000008-0000-0000-0000-000000000008")
	intent := makeIntent(id, time.Now().UTC())
	seedIntent(t, store, intent)

	dispatcher := &fakeDispatcher{respondStatus: http.StatusOK}
	svc := failedintenthttp.NewService(store, dispatcher, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay-with",
		strings.NewReader(`{"body":null}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "invalid_replay_body", p.Code)

	assert.Equal(t, 0, dispatcher.callCount(), "dispatcher must NOT be called for null body")

	// RetryCount must be unchanged.
	stored, err := store.Get(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, 0, stored.RetryCount, "RetryCount must be unchanged when body is null")
}

// FuzzReplayWith_BodyParsing fuzzes the top-level JSON decode and the body field
// to ensure no panics occur and that responses are always 200 or 422.
func FuzzReplayWith_BodyParsing(f *testing.F) {
	// Seed corpus.
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"body":{}}`))
	f.Add([]byte(`{"body":null}`))
	f.Add([]byte(`{"body":"plain"}`))
	f.Add([]byte(`{"body":[1,2,3]}`))
	f.Add([]byte("not json"))
	f.Add([]byte(""))
	f.Add([]byte(`{"body":1234567890}`))
	f.Add([]byte(`{"body":"héllo wörld 日本語"}`)) // multibyte UTF-8

	f.Fuzz(func(t *testing.T, payload []byte) {
		store := newMemoryStore()
		id := uuid.New()
		intent := makeIntent(id, time.Now().UTC())
		_ = store.Save(context.Background(), intent)

		lookup := &stubUsuarioLookup{user: auth.CurrentUser{ID: *intent.UsuarioID}}
		dispatcher := &fakeDispatcher{respondStatus: http.StatusOK}

		svc := failedintenthttp.NewService(store, dispatcher, lookup, nil, nil, nil)
		cu := defaultCU()
		r := newRouter(t, svc, &cu)

		req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay-with",
			bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		// Must not panic.
		r.ServeHTTP(rec, req)

		// Status must be 200 or 422 — never 500.
		status := rec.Code
		if status != http.StatusOK && status != http.StatusUnprocessableEntity {
			t.Fatalf("unexpected status %d for payload %q; body: %s", status, payload, rec.Body.String())
		}

		// If 200, outcome must be one of the valid replay outcomes.
		if status == http.StatusOK {
			var resp failedintenthttp.ReplayResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("200 response is not valid ReplayResponse JSON: %v; body: %s", err, rec.Body.String())
			}
			validOutcomes := map[string]bool{
				string(failedintent.StatusRetriedOK):   true,
				string(failedintent.StatusRetriedFail): true,
			}
			if !validOutcomes[resp.Outcome] {
				t.Fatalf("unexpected outcome %q for payload %q", resp.Outcome, payload)
			}
		}
	})
}

// ─── Group 2 — MeListar robustness ───────────────────────────────────────────

// TestMeListar_CrossUserIsolation proves user A never sees user B's data even
// under stress with multiple user populations including nil-UsuarioID intents.
func TestMeListar_CrossUserIsolation(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC()

	userA := uuid.MustParse("dd000001-0000-0000-0000-000000000001")
	userB := uuid.MustParse("dd000002-0000-0000-0000-000000000002")

	var aIDs, bIDs []string

	// Seed 5 intents for user A.
	for i := range 5 {
		id := uuid.New()
		intent := makeIntent(id, now.Add(-time.Duration(i)*time.Second))
		intent.UsuarioID = &userA
		seedIntent(t, store, intent)
		aIDs = append(aIDs, id.String())
	}
	// Seed 5 intents for user B.
	for i := range 5 {
		id := uuid.New()
		intent := makeIntent(id, now.Add(-time.Duration(i+10)*time.Second))
		intent.UsuarioID = &userB
		seedIntent(t, store, intent)
		bIDs = append(bIDs, id.String())
	}
	// Seed 5 anonymous intents (UsuarioID == nil).
	for i := range 5 {
		id := uuid.New()
		intent := makeIntent(id, now.Add(-time.Duration(i+20)*time.Second))
		intent.UsuarioID = nil
		seedIntent(t, store, intent)
	}

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)

	callMeListar := func(t *testing.T, userID uuid.UUID) failedintenthttp.ListResponse {
		t.Helper()
		cu := auth.CurrentUser{ID: userID}
		r := newMeRouter(t, svc, &cu)
		req := httptest.NewRequest(http.MethodGet, "/?page_size=100", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var resp failedintenthttp.ListResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp
	}

	// User A sees exactly their 5 intents.
	respA := callMeListar(t, userA)
	gotAIDs := make([]string, 0, len(respA.Items))
	for _, item := range respA.Items {
		gotAIDs = append(gotAIDs, item.ID)
	}
	assert.ElementsMatch(t, aIDs, gotAIDs, "user A must see exactly their own 5 intents")

	// User B sees exactly their 5 intents.
	respB := callMeListar(t, userB)
	gotBIDs := make([]string, 0, len(respB.Items))
	for _, item := range respB.Items {
		gotBIDs = append(gotBIDs, item.ID)
	}
	assert.ElementsMatch(t, bIDs, gotBIDs, "user B must see exactly their own 5 intents")

	// User C (no intents) sees an empty list.
	userC := uuid.MustParse("dd000003-0000-0000-0000-000000000003")
	respC := callMeListar(t, userC)
	assert.Empty(t, respC.Items, "user C must see an empty list")
}

// TestMeListar_CursorPagination_RoundTrip verifies full cursor-paginated round
// trip across 3 pages of 5, 5, and 2 items from a 12-intent seed.
func TestMeListar_CursorPagination_RoundTrip(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	baseTime := time.Now().UTC()

	cu := defaultCU()
	userID := cu.ID

	// Seed 12 intents for user A, each 1 second apart.
	allIDs := make([]string, 0, 12)
	for i := range 12 {
		id := uuid.New()
		intent := makeIntent(id, baseTime.Add(-time.Duration(i)*time.Second))
		intent.UsuarioID = &userID
		seedIntent(t, store, intent)
		allIDs = append(allIDs, id.String())
	}

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	r := newMeRouter(t, svc, &cu)

	collectPage := func(t *testing.T, query string) failedintenthttp.ListResponse {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/?page_size=5"+query, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var resp failedintenthttp.ListResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp
	}

	// Page 1.
	resp1 := collectPage(t, "")
	require.Len(t, resp1.Items, 5, "page 1 must have 5 items")
	assert.True(t, resp1.HasMore, "page 1 must have more")
	require.NotEmpty(t, resp1.NextCursor, "page 1 must have next_cursor")

	// Page 2.
	resp2 := collectPage(t, "&cursor="+resp1.NextCursor)
	require.Len(t, resp2.Items, 5, "page 2 must have 5 items")
	assert.True(t, resp2.HasMore, "page 2 must have more")
	require.NotEmpty(t, resp2.NextCursor, "page 2 must have next_cursor")

	// Page 3.
	resp3 := collectPage(t, "&cursor="+resp2.NextCursor)
	require.Len(t, resp3.Items, 2, "page 3 must have 2 items")
	assert.False(t, resp3.HasMore, "page 3 must not have more")
	assert.Empty(t, resp3.NextCursor, "page 3 must have empty next_cursor")

	// All IDs across all pages equal the seeded IDs.
	gotIDs := make([]string, 0, 12)
	for _, item := range resp1.Items {
		gotIDs = append(gotIDs, item.ID)
	}
	for _, item := range resp2.Items {
		gotIDs = append(gotIDs, item.ID)
	}
	for _, item := range resp3.Items {
		gotIDs = append(gotIDs, item.ID)
	}
	assert.ElementsMatch(t, allIDs, gotIDs, "all 12 seeded IDs must appear exactly once across all pages")
}

// TestMeListar_StatusAndUserFilterCombined verifies that status and user filters
// combine correctly: user A with status=ignored sees only their ignored intents.
func TestMeListar_StatusAndUserFilterCombined(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC()

	cu := defaultCU()
	userA := cu.ID
	userB := uuid.MustParse("ee000001-0000-0000-0000-000000000001")

	// User A: 3 StatusNew + 2 StatusIgnored.
	var aIgnoredIDs []string
	for i := range 3 {
		id := uuid.New()
		intent := makeIntent(id, now.Add(-time.Duration(i)*time.Second))
		intent.UsuarioID = &userA
		intent.Status = failedintent.StatusNew
		seedIntent(t, store, intent)
	}
	for i := range 2 {
		id := uuid.New()
		intent := makeIntent(id, now.Add(-time.Duration(i+10)*time.Second))
		intent.UsuarioID = &userA
		intent.Status = failedintent.StatusIgnored
		seedIntent(t, store, intent)
		aIgnoredIDs = append(aIgnoredIDs, id.String())
	}
	// User B: 2 StatusNew (must not appear in results).
	for i := range 2 {
		id := uuid.New()
		intent := makeIntent(id, now.Add(-time.Duration(i+20)*time.Second))
		intent.UsuarioID = &userB
		intent.Status = failedintent.StatusNew
		seedIntent(t, store, intent)
	}

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	r := newMeRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/?status=ignored&page_size=100", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp failedintenthttp.ListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	require.Len(t, resp.Items, 2, "must return exactly 2 ignored intents for user A")

	gotIDs := make([]string, 0, len(resp.Items))
	for _, item := range resp.Items {
		gotIDs = append(gotIDs, item.ID)
		assert.Equal(t, "ignored", item.Status, "all returned items must have status=ignored")
		require.NotNil(t, item.UsuarioID, "all returned items must have a usuario_id")
		assert.Equal(t, userA.String(), *item.UsuarioID, "all returned items must belong to user A")
	}
	assert.ElementsMatch(t, aIgnoredIDs, gotIDs)
}

// TestMeListar_ConcurrentRequests_NoRace ensures the handler is safe under
// concurrent load. Run with -race to detect unsynchronised access.
func TestMeListar_ConcurrentRequests_NoRace(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC()
	cu := defaultCU()
	userID := cu.ID

	// Seed 10 intents for the authenticated user.
	for i := range 10 {
		id := uuid.New()
		intent := makeIntent(id, now.Add(-time.Duration(i)*time.Second))
		intent.UsuarioID = &userID
		seedIntent(t, store, intent)
	}

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	r := newMeRouter(t, svc, &cu)

	var wg sync.WaitGroup
	const goroutines = 20
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/?page_size=100", nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				// Use t.Errorf (not t.Fatalf) since we're in a goroutine.
				t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
				return
			}
			var resp failedintenthttp.ListResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Errorf("invalid response JSON: %v", err)
				return
			}
			if len(resp.Items) != 10 {
				t.Errorf("expected 10 items, got %d", len(resp.Items))
			}
		}()
	}

	wg.Wait()
}

// ─── Group 3 — memoryStore filter unit test ──────────────────────────────────

// TestMemoryStore_UsuarioFilter_IsolatesByID directly tests the in-test
// memoryStore's UsuarioID filtering logic independent of the handler.
func TestMemoryStore_UsuarioFilter_IsolatesByID(t *testing.T) {
	t.Parallel()

	ms := newMemoryStore()
	now := time.Now().UTC()

	userA := uuid.MustParse("ff000001-0000-0000-0000-000000000001")
	userB := uuid.MustParse("ff000002-0000-0000-0000-000000000002")
	nonExistent := uuid.MustParse("ff000003-0000-0000-0000-000000000003")

	// 2 for user A.
	for i := range 2 {
		id := uuid.New()
		intent := makeIntent(id, now.Add(-time.Duration(i)*time.Second))
		intent.UsuarioID = &userA
		require.NoError(t, ms.Save(context.Background(), intent))
	}
	// 1 for user B.
	{
		id := uuid.New()
		intent := makeIntent(id, now.Add(-5*time.Second))
		intent.UsuarioID = &userB
		require.NoError(t, ms.Save(context.Background(), intent))
	}
	// 1 with nil UsuarioID.
	{
		id := uuid.New()
		intent := makeIntent(id, now.Add(-10*time.Second))
		intent.UsuarioID = nil
		require.NoError(t, ms.Save(context.Background(), intent))
	}

	// Filter by user A — must return exactly 2.
	pageA, err := ms.List(context.Background(), failedintent.ListParams{UsuarioID: &userA, PageSize: 20})
	require.NoError(t, err)
	assert.Len(t, pageA.Items, 2, "UsuarioID filter for user A must return exactly 2")
	for _, item := range pageA.Items {
		require.NotNil(t, item.UsuarioID)
		assert.Equal(t, userA, *item.UsuarioID)
	}

	// No filter — must return all 4.
	pageAll, err := ms.List(context.Background(), failedintent.ListParams{UsuarioID: nil, PageSize: 20})
	require.NoError(t, err)
	assert.Len(t, pageAll.Items, 4, "nil UsuarioID filter must return all 4 intents")

	// Filter by non-existent user — must return 0.
	pageNone, err := ms.List(context.Background(), failedintent.ListParams{UsuarioID: &nonExistent, PageSize: 20})
	require.NoError(t, err)
	assert.Empty(t, pageNone.Items, "UsuarioID filter for non-existent user must return 0")
}

// ─── Group 4 — Property test for filter consistency ──────────────────────────

// TestProperty_MemoryStoreUsuarioFilter uses rapid to verify that the
// memoryStore UsuarioID filter always returns exactly the matching intents.
func TestProperty_MemoryStoreUsuarioFilter(t *testing.T) {
	t.Parallel()

	// A fixed pool of 5 candidate UUIDs.
	pool := [5]uuid.UUID{
		uuid.MustParse("aa100001-0000-0000-0000-000000000001"),
		uuid.MustParse("aa100002-0000-0000-0000-000000000002"),
		uuid.MustParse("aa100003-0000-0000-0000-000000000003"),
		uuid.MustParse("aa100004-0000-0000-0000-000000000004"),
		uuid.MustParse("aa100005-0000-0000-0000-000000000005"),
	}

	rapid.Check(t, func(rt *rapid.T) {
		ms := newMemoryStore()
		now := time.Now().UTC()

		// Generate N intents (0–50) each with a random UsuarioID from the pool
		// or nil (index 5 = nil).
		n := rapid.IntRange(0, 50).Draw(rt, "n")
		// Track count per candidate.
		countPerUser := make(map[uuid.UUID]int)

		for i := range n {
			id := uuid.New()
			intent := makeIntent(id, now.Add(-time.Duration(i)*time.Second))
			idx := rapid.IntRange(0, 5).Draw(rt, "user_idx")
			if idx < 5 {
				uid := pool[idx]
				intent.UsuarioID = &uid
				countPerUser[uid]++
			} else {
				intent.UsuarioID = nil
			}
			require.NoError(rt, ms.Save(context.Background(), intent))
		}

		// Pick a random candidate and verify the filter.
		candidate := pool[rapid.IntRange(0, 4).Draw(rt, "candidate_idx")]
		page, err := ms.List(context.Background(), failedintent.ListParams{
			UsuarioID: &candidate,
			PageSize:  100,
		})
		require.NoError(rt, err)

		// Every returned intent must belong to candidate.
		for _, item := range page.Items {
			if item.UsuarioID == nil || *item.UsuarioID != candidate {
				rt.Fatalf("returned intent %s does not belong to candidate %s", item.ID, candidate)
			}
		}

		// Count must match the seeded count.
		expected := countPerUser[candidate]
		if len(page.Items) != expected {
			rt.Fatalf("expected %d intents for candidate %s, got %d", expected, candidate, len(page.Items))
		}
	})
}

// ─── Additional coverage — uncovered branches ─────────────────────────────────

// TestMountMeRouter_ServesGET verifies that MountMeRouter correctly wires MeListar
// to GET / on the provided chi.Router.
func TestMountMeRouter_ServesGET(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)

	cu := defaultCU()
	parent := chi.NewRouter()
	parent.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.PlantCurrentUser(req.Context(), cu)))
		})
	})
	failedintenthttp.MountMeRouter(parent, svc)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	parent.ServeHTTP(rec, req)

	// Handler is reachable and returns 200 (empty store → empty page).
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp failedintenthttp.ListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Empty(t, resp.Items)
}

// TestMeListar_StoreError_Propagates exercises the MeListar store-error path.
func TestMeListar_StoreError_Propagates(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	store.listErr = apperror.NewInternal("db_error", "error de base de datos")

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newMeRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
}

// TestReplay_StoreGetError_Propagates exercises the Replay store.Get error path.
func TestReplay_StoreGetError_Propagates(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	store.getErr = apperror.NewInternal("db_error", "error de base de datos")

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	id := uuid.MustParse("ac000001-0000-0000-0000-000000000001")
	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
}

// TestReplayWith_StoreGetError_Propagates exercises the ReplayWith store.Get error path.
func TestReplayWith_StoreGetError_Propagates(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	store.getErr = apperror.NewInternal("db_error", "error de base de datos")

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	id := uuid.MustParse("ac000002-0000-0000-0000-000000000002")
	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay-with",
		strings.NewReader(`{"body":{"k":"v"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
}

// TestReplay_IncrementRetryError_ContinuesReplay exercises the executeReplay
// IncrementRetry error path — the replay must still proceed and return 200.
func TestReplay_IncrementRetryError_ContinuesReplay(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC()
	id := uuid.MustParse("ac000003-0000-0000-0000-000000000003")
	intent := makeIntent(id, now)
	seedIntent(t, store, intent)

	// Inject IncrementRetry error.
	store.mu.Lock()
	store.incrErr = apperror.NewInternal("incr_err", "error incrementing")
	store.mu.Unlock()

	expectedCU := auth.CurrentUser{ID: *intent.UsuarioID}
	lookup := &stubUsuarioLookup{user: expectedCU}
	dispatcher := &fakeDispatcher{respondStatus: http.StatusOK}

	svc := failedintenthttp.NewService(store, dispatcher, lookup, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Replay proceeds despite IncrementRetry error (best-effort, logged as warn).
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp failedintenthttp.ReplayResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "retried_ok", resp.Outcome)

	// Dispatcher must have been called.
	assert.Equal(t, 1, dispatcher.callCount())
}

// TestDecodeCursor_MalformedParts exercises the "split into != 2 parts" error
// path in decodeCursor by encoding a base64 string with no '|' separator.
func TestDecodeCursor_MalformedParts(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	// Encode a string without the '|' separator so SplitN returns 1 part.
	noPipe := base64.RawURLEncoding.EncodeToString([]byte("nopipe")) // valid b64 but no pipe
	req := httptest.NewRequest(http.MethodGet, "/?cursor="+noPipe, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "invalid_cursor", p.Code)
}

// TestDecodeCursor_InvalidTimePart exercises the time.Parse error path in
// decodeCursor by providing a pipe-separated string whose first part is not
// a valid RFC3339Nano timestamp.
func TestDecodeCursor_InvalidTimePart(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	// "notadate|<valid-uuid>" encoded as base64url (no padding).
	raw := "notadate|00000000-0000-0000-0000-000000000001"
	encoded := base64.RawURLEncoding.EncodeToString([]byte(raw))
	req := httptest.NewRequest(http.MethodGet, "/?cursor="+encoded, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "invalid_cursor", p.Code)
}

// TestDecodeCursor_InvalidUUIDPart exercises the uuid.Parse error path in
// decodeCursor by providing a pipe-separated string with a valid timestamp but
// invalid UUID.
func TestDecodeCursor_InvalidUUIDPart(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	raw := "2024-01-01T00:00:00Z|not-a-uuid"
	encoded := base64.RawURLEncoding.EncodeToString([]byte(raw))
	req := httptest.NewRequest(http.MethodGet, "/?cursor="+encoded, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "invalid_cursor", p.Code)
}

// TestExecuteReplay_BuildRequestError exercises the buildErr path in
// executeReplay. An invalid HTTP method (containing "\n") causes
// http.NewRequestWithContext to return an error.
func TestExecuteReplay_BuildRequestError_ReturnsRetriedFail(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	now := time.Now().UTC()
	id := uuid.MustParse("ac000004-0000-0000-0000-000000000004")
	intent := makeIntent(id, now)
	// An invalid method will cause http.NewRequestWithContext to fail.
	intent.Method = "IN\nVALID"
	seedIntent(t, store, intent)

	expectedCU := auth.CurrentUser{ID: *intent.UsuarioID}
	lookup := &stubUsuarioLookup{user: expectedCU}
	dispatcher := &fakeDispatcher{respondStatus: http.StatusOK}

	svc := failedintenthttp.NewService(store, dispatcher, lookup, nil, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	req := httptest.NewRequest(http.MethodPost, "/"+id.String()+"/replay", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// The handler returns 200 with outcome=retried_fail when the build fails.
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp failedintenthttp.ReplayResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "retried_fail", resp.Outcome)
	assert.Equal(t, http.StatusInternalServerError, resp.ReplayHTTPStatus)

	// Dispatcher must NOT have been called.
	assert.Equal(t, 0, dispatcher.callCount())
}
