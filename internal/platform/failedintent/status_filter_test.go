package failedintent_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// problemBody403 is the body platform/response writes when authn rejects a
// request from a usuario that is no longer active. Unlike the 401 it is not
// about the token: the session is valid, the person is not.
const problemBody403 = `{"code":"user_inactive","detail":"el usuario está inactivo","title":"Forbidden"}`

// handler403UsuarioInactivo answers 403 WITHOUT reading the request body —
// exactly what authn.Handler does when the usuario row is marked inactive. A
// cobrador given de baja who posts a pago gets this and nothing else.
func handler403UsuarioInactivo() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, problemBody403)
	})
}

// TestCaptureMiddleware_CaptureStatuses_CapturesOnlyTheListedStatus pins the
// split that keeps two capture instances from writing two rows for one
// request: the narrowed instance owns the statuses the auth boundary answers
// itself and nothing else, so every failure the in-chain instance already
// sees passes it by untouched.
//
// The config under test is StatusesOutsideAuth, the very list the composition
// root mounts — not a hand-made one — so a status missing from it fails here.
func TestCaptureMiddleware_CaptureStatuses_CapturesOnlyTheListedStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		handler   http.Handler
		wantSaved int
	}{
		{"401 is captured", handler401(), 1},
		{"403 user_inactive is captured", handler403UsuarioInactivo(), 1},
		{"422 belongs to the in-chain instance", handler422(), 0},
		{"200 captures nothing", handler200(), 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeStore{}
			cfg := newTestConfig(store, 1024)
			cfg.CaptureStatuses = failedintent.StatusesOutsideAuth

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

// TestCaptureMiddleware_CaptureStatuses_YieldsOnA403RaisedByAHandler is the
// counterpart of the 401 yield, and the one that had to hold before 403 could
// be added to the outside-the-chain set. Unlike 401, a 403 is ALSO raised
// deeper in — a handler refusing a permission — where the in-chain instance
// sees it first and its row carries the requester. The narrowed instance must
// yield, or widening the set would double every permission refusal.
func TestCaptureMiddleware_CaptureStatuses_YieldsOnA403RaisedByAHandler(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}

	inner := newTestConfig(store, 1024)
	outer := newTestConfig(store, 1024)
	outer.CaptureStatuses = failedintent.StatusesOutsideAuth

	chain := failedintent.CaptureMiddleware(outer)(
		failedintent.CaptureMiddleware(inner)(handler403UsuarioInactivo()),
	)

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", bytes.NewBufferString(`{"total":10}`))
	rw := httptest.NewRecorder()
	chain.ServeHTTP(rw, req)

	require.Equal(t, 1, store.count(), "one request, one row")
	assert.Equal(t, http.StatusForbidden, store.first().HTTPStatus)
	assert.Equal(t, http.StatusForbidden, rw.Code, "the response is still the handler's")
}

// TestCaptureMiddleware_CaptureStatuses_MultipartKeepsTheBodyOfAPagoDeUnCobradorDadoDeBaja
// is the case that motivated widening the set. A cobrador given de baja posts
// a pago with the receipt photo; authn answers 403 before anything reads the
// body. The upload must survive anyway, or the money that was collected has
// no record on the server at all.
func TestCaptureMiddleware_CaptureStatuses_MultipartKeepsTheBodyOfAPagoDeUnCobradorDadoDeBaja(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	blobs := newFakeBlobStore()
	cfg := newMultipartTestConfig(store, blobs)
	cfg.CaptureStatuses = failedintent.StatusesOutsideAuth

	req := buildMultipartRequest(t, []byte("imagen-del-comprobante"))
	sent, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	req.Body = io.NopCloser(bytes.NewReader(sent))

	failedintent.CaptureMiddleware(cfg)(handler403UsuarioInactivo()).ServeHTTP(httptest.NewRecorder(), req)

	require.Equal(t, 1, store.count(), "the rejected pago must leave evidence")
	got := store.first()
	assert.Equal(t, http.StatusForbidden, got.HTTPStatus)
	assert.Equal(t, "user_inactive", got.ErrorCode)
	assert.Nil(t, got.UsuarioID, "the request never got past authn, so nobody may be named")
	require.NotEmpty(t, got.BodyBlobPath, "the body is the evidence")
	assert.Equal(t, sent, blobs.blobBytes(got.BodyBlobPath), "the stored body must be the one that was sent")
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
		"custody must NOT be confirmed: the header promises a row that can be re-dispatched, "+
			"and one without a usuario cannot")
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

// TestCaptureMiddleware_CaptureStatuses_JSONDoesNotAlterABodyItDoesNotCapture
// pins the property that makes a narrowed instance safe to mount in front of
// everything: when it captures nothing, it must be invisible.
//
// readCappedBody does not merely read — it CUTS at BodyCapBytes and hands the
// cut bytes to whoever comes next. An instance that reads on the way in would
// therefore truncate every oversized request that goes on to succeed, and
// would hand the in-chain instance a body already shortened, whose remaining
// length it would then measure as fitting.
func TestCaptureMiddleware_CaptureStatuses_JSONDoesNotAlterABodyItDoesNotCapture(t *testing.T) {
	t.Parallel()

	const cap64 = 64

	store := &fakeStore{}
	cfg := newTestConfig(store, cap64)
	cfg.CaptureStatuses = []int{http.StatusUnauthorized}

	sent := `{"x":"` + strings.Repeat("a", 4*cap64) + `"}`
	var received []byte
	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusUnprocessableEntity)
	})

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", bytes.NewBufferString(sent))
	failedintent.CaptureMiddleware(cfg)(downstream).ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, 0, store.count(), "a 422 is not this instance's business")
	assert.Equal(t, sent, string(received),
		"the request must reach the handler exactly as it was sent, not cut at BodyCapBytes")
}

// TestCaptureMiddleware_CaptureStatuses_YieldsEvenWhenTheConfirmationHeaderIsMissing
// covers the hole in the header signal. captureWriter releases a withheld
// response early once it outgrows the deferred cap, and from then on
// flushDeferred can no longer add X-Intent-Captured: the in-chain instance
// saves its row and stamps nothing. Since this is the ONLY lock on the
// overlap, it cannot depend on a signal that legitimately goes missing.
func TestCaptureMiddleware_CaptureStatuses_YieldsEvenWhenTheConfirmationHeaderIsMissing(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	inner := newTestConfig(store, 1024)
	outer := newTestConfig(store, 1024)
	outer.CaptureStatuses = []int{http.StatusUnauthorized}

	// A 401 whose response body is past deferredBodyCapBytes (1 MiB), so the
	// in-chain instance flushes before it knows whether it has custody.
	chatty := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(strings.Repeat("x", 2<<20)))
	})

	chain := failedintent.CaptureMiddleware(outer)(
		failedintent.CaptureMiddleware(inner)(chatty),
	)

	rw := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", bytes.NewBufferString(`{"total":10}`))
	chain.ServeHTTP(rw, req)

	require.Empty(t, rw.Header().Get(failedintent.HeaderIntentCaptured),
		"the premise: the response outgrew the deferred cap, so no header was stamped")
	assert.Equal(t, 1, store.count(), "one request, one row — the header was not the only signal")
}
