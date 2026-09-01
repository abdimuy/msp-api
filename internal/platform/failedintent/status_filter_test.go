package failedintent_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	"github.com/abdimuy/msp-api/internal/platform/idempotency"
)

// ---------------------------------------------------------------------------
// Config.CaptureStatuses — the instance mounted outside the auth chain
// ---------------------------------------------------------------------------

// problemBody401 is the body platform/response writes when authn rejects a
// request: the flat RFC9457 shape with a top-level `code`. It is what the
// outside-the-chain instance actually sees, so the tests below assert against
// it rather than against a hand-made 401.
const problemBody401 = `{"code":"missing_authorization","detail":"encabezado authorization ausente","title":"Unauthorized"}`

// handler401 answers 401 WITHOUT reading the request body — exactly what
// authn.Handler does when the bearer token is missing or expired. Not reading
// the body is not incidental: it is the condition the deferred multipart
// capture has to survive.
func handler401() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, problemBody401)
	})
}

// TestCaptureMiddleware_CaptureStatuses_CapturesOnlyTheListedStatus pins the
// split that keeps two capture instances from writing two rows for one
// request: the narrowed instance owns 401 and nothing else, so every failure
// the in-chain instance already sees passes it by untouched.
func TestCaptureMiddleware_CaptureStatuses_CapturesOnlyTheListedStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		handler   http.Handler
		wantSaved int
	}{
		{"401 is captured", handler401(), 1},
		{"422 belongs to the in-chain instance", handler422(), 0},
		{"200 captures nothing", handler200(), 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeStore{}
			cfg := newTestConfig(store, 1024)
			cfg.CaptureStatuses = []int{http.StatusUnauthorized}

			req := httptest.NewRequest(http.MethodPost, "/v2/ventas", bytes.NewBufferString(`{"total":10}`))
			rw := httptest.NewRecorder()
			failedintent.CaptureMiddleware(cfg)(tc.handler).ServeHTTP(rw, req)

			assert.Equal(t, tc.wantSaved, store.count())
		})
	}
}

// TestCaptureMiddleware_CaptureStatuses_ZeroValueKeepsCapturingEveryFailure is
// the guard on the field itself: adding it must not narrow the instances that
// do not set it. A zero value has to mean exactly what the middleware did
// before the field existed.
func TestCaptureMiddleware_CaptureStatuses_ZeroValueKeepsCapturingEveryFailure(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cfg := newTestConfig(store, 1024)
	require.Nil(t, cfg.CaptureStatuses, "the test config must exercise the zero value")

	for _, h := range []http.Handler{handler401(), handler422()} {
		req := httptest.NewRequest(http.MethodPost, "/v2/ventas", bytes.NewBufferString(`{"total":10}`))
		failedintent.CaptureMiddleware(cfg)(h).ServeHTTP(httptest.NewRecorder(), req)
	}

	assert.Equal(t, 2, store.count(), "an unfiltered instance keeps capturing every 4xx")
}

// TestCaptureMiddleware_CaptureStatuses_YieldsToAnInstanceThatAlreadySaved
// covers the one overlap the status split does not: a 401 raised deeper in the
// chain, past the in-chain instance, which therefore captures it first. The
// narrowed instance sees the same 401 and must not write a second row for it.
//
// The Firebird Store cannot rescue this case: its dedup only fires when the
// intent carries an Idempotency-Key (firebird/store.go:76-78), and the pago
// path does not send one.
func TestCaptureMiddleware_CaptureStatuses_YieldsToAnInstanceThatAlreadySaved(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}

	inner := newTestConfig(store, 1024)
	outer := newTestConfig(store, 1024)
	outer.CaptureStatuses = []int{http.StatusUnauthorized}

	chain := failedintent.CaptureMiddleware(outer)(
		failedintent.CaptureMiddleware(inner)(handler401()),
	)

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", bytes.NewBufferString(`{"total":10}`))
	rw := httptest.NewRecorder()
	chain.ServeHTTP(rw, req)

	require.Equal(t, 1, store.count(), "one request, one row")
	assert.Equal(t, http.StatusUnauthorized, store.first().HTTPStatus)
	assert.Equal(t, http.StatusUnauthorized, rw.Code, "the response is still the handler's")
}

// TestCaptureMiddleware_CaptureStatuses_DoesNotCloseIntentsOnSuccess: closing
// a pending intent whose work just landed belongs to the in-chain instance,
// which sees every authenticated request. Repeating it from outside would add
// a query per successful pago and close nothing new.
func TestCaptureMiddleware_CaptureStatuses_DoesNotCloseIntentsOnSuccess(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cfg := newTestConfig(store, 1024)
	cfg.CaptureStatuses = []int{http.StatusUnauthorized}

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", bytes.NewBufferString(`{"total":10}`))
	req.Header.Set(idempotency.HeaderKey, "k-1")
	failedintent.CaptureMiddleware(cfg)(handler200()).ServeHTTP(httptest.NewRecorder(), req)

	assert.Empty(t, store.marcados, "a narrowed instance does not do the in-chain instance's bookkeeping")
}

// ---------------------------------------------------------------------------
// Multipart: the pago path, where the body is the evidence
// ---------------------------------------------------------------------------

// TestCaptureMiddleware_CaptureStatuses_MultipartKeepsTheBodyOfARejectedRequest
// is the point of the whole exercise. A pago posted with an expired session is
// answered 401 by a handler that never touches the body; the row must still
// carry the upload, and it must say it does not know who sent it rather than
// guess.
func TestCaptureMiddleware_CaptureStatuses_MultipartKeepsTheBodyOfARejectedRequest(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	blobs := newFakeBlobStore()
	cfg := newMultipartTestConfig(store, blobs)
	cfg.CaptureStatuses = []int{http.StatusUnauthorized}

	req := buildMultipartRequest(t, []byte("imagen-del-comprobante"))
	sent, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	req.Body = io.NopCloser(bytes.NewReader(sent))

	rw := httptest.NewRecorder()
	failedintent.CaptureMiddleware(cfg)(handler401()).ServeHTTP(rw, req)

	require.Equal(t, 1, store.count(), "the rejected pago must leave evidence")
	got := store.first()
	assert.Equal(t, http.StatusUnauthorized, got.HTTPStatus)
	assert.Equal(t, "missing_authorization", got.ErrorCode)
	assert.Nil(t, got.UsuarioID, "nobody was authenticated, so nobody may be named")
	assert.Empty(t, got.FirebaseUID)
	assert.False(t, got.BodyTruncated)
	require.NotEmpty(t, got.BodyBlobPath, "the body is the evidence")
	assert.Equal(t, sent, blobs.blobBytes(got.BodyBlobPath), "the stored body must be the one that was sent")
	assert.Empty(t, rw.Header().Get(failedintent.HeaderIntentCaptured),
		"custody must NOT be confirmed: the phone would stop retrying a pago nobody can replay")
}

// countingBlobStore counts Save calls on top of fakeBlobStore. Counting the
// blobs left behind is not enough to prove the narrowed instance never wrote
// one: the tee-based path also ends with zero blobs on a 2xx, because it
// deletes the copy it just made. What must be zero is the write itself.
type countingBlobStore struct {
	*fakeBlobStore
	mu    sync.Mutex
	saves int
}

func (c *countingBlobStore) Save(
	ctx context.Context, intentID uuid.UUID, body io.Reader, limitBytes int64,
) (string, error) {
	c.mu.Lock()
	c.saves++
	c.mu.Unlock()
	return c.fakeBlobStore.Save(ctx, intentID, body, limitBytes)
}

func (c *countingBlobStore) saveCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saves
}

// TestCaptureMiddleware_CaptureStatuses_MultipartWritesNothingWhenNothingFails
// is why the narrowed instance does not tee: it must cost a successful upload
// nothing. Teeing would write a full copy of every pago photo to disk and then
// delete it, on every pago.
func TestCaptureMiddleware_CaptureStatuses_MultipartWritesNothingWhenNothingFails(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	blobs := &countingBlobStore{fakeBlobStore: newFakeBlobStore()}
	cfg := newMultipartTestConfig(store, blobs)
	cfg.CaptureStatuses = []int{http.StatusUnauthorized}

	// The downstream handler drains the body, the way a real multipart
	// handler does before answering.
	drained := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	})

	req := buildMultipartRequest(t, []byte("imagen-del-comprobante"))
	failedintent.CaptureMiddleware(cfg)(drained).ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, 0, store.count())
	assert.Equal(t, 0, blobs.saveCalls(), "a successful upload must not be copied to disk at all")
	assert.Equal(t, 0, blobs.blobCount())
}

// TestCaptureMiddleware_CaptureStatuses_MultipartDoesNotPassOffALeftoverAsTheBody:
// when something downstream already consumed part of the body, what is left is
// no longer the request that was sent. The row is persisted without a body and
// marked truncated — an honest gap beats a fragment that reads as complete.
func TestCaptureMiddleware_CaptureStatuses_MultipartDoesNotPassOffALeftoverAsTheBody(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	blobs := newFakeBlobStore()
	cfg := newMultipartTestConfig(store, blobs)
	cfg.CaptureStatuses = []int{http.StatusUnauthorized}

	halfEaten := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.CopyN(io.Discard, r.Body, 10)
		w.WriteHeader(http.StatusUnauthorized)
	})

	req := buildMultipartRequest(t, []byte("imagen-del-comprobante"))
	failedintent.CaptureMiddleware(cfg)(halfEaten).ServeHTTP(httptest.NewRecorder(), req)

	require.Equal(t, 1, store.count(), "the row exists even when the body cannot")
	got := store.first()
	assert.True(t, got.BodyTruncated)
	assert.Empty(t, got.BodyBlobPath)
	assert.Equal(t, 0, blobs.blobCount())
}

// TestCaptureMiddleware_CaptureStatuses_MultipartWithoutBodyIsNotStoredAsEmpty:
// a multipart request whose body yields nothing (closed downstream, or a
// client that hung up before sending it) must not leave a zero-byte blob. On
// the screen an empty body reads as "the phone sent nothing", which is a
// different accusation and an unfounded one.
func TestCaptureMiddleware_CaptureStatuses_MultipartWithoutBodyIsNotStoredAsEmpty(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	blobs := newFakeBlobStore()
	cfg := newMultipartTestConfig(store, blobs)
	cfg.CaptureStatuses = []int{http.StatusUnauthorized}

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", nil)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=abc123")
	failedintent.CaptureMiddleware(cfg)(handler401()).ServeHTTP(httptest.NewRecorder(), req)

	require.Equal(t, 1, store.count(), "the row still exists")
	got := store.first()
	assert.True(t, got.BodyTruncated)
	assert.Empty(t, got.BodyBlobPath)
	assert.Equal(t, 0, blobs.blobCount(), "no zero-byte blob may be left behind")
}
