package authhttp

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/logger"
	"github.com/abdimuy/msp-api/internal/platform/middleware"
)

// obsRecordHandler is a slog.Handler that captures every record into a
// thread-safe slice. It implements the contextHandler enrichment manually so
// the request_id attribute that the production logger injects is also visible
// to the assertions below.
type obsRecordHandler struct {
	mu      sync.Mutex
	records []slog.Record
	attrs   []slog.Attr
}

func (h *obsRecordHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *obsRecordHandler) Handle(ctx context.Context, r slog.Record) error {
	// Mirror the contextHandler in internal/platform/logger so we see
	// request_id without depending on private types.
	if v := logger.RequestIDFrom(ctx); v != "" {
		r.AddAttrs(slog.String("request_id", v))
	}
	for _, a := range h.attrs {
		r.AddAttrs(a)
	}
	h.mu.Lock()
	h.records = append(h.records, r.Clone())
	h.mu.Unlock()
	return nil
}

func (h *obsRecordHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := &obsRecordHandler{attrs: append([]slog.Attr{}, h.attrs...)}
	clone.attrs = append(clone.attrs, attrs...)
	return clone
}

func (h *obsRecordHandler) WithGroup(_ string) slog.Handler { return h }

// obsFindRecord returns the first record matching msg, or nil.
func (h *obsRecordHandler) obsFindRecord(msg string) *slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.records {
		if h.records[i].Message == msg {
			return &h.records[i]
		}
	}
	return nil
}

// obsRecordsByMsg counts records with Message == msg.
func (h *obsRecordHandler) obsRecordsByMsg(msg string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if r.Message == msg {
			n++
		}
	}
	return n
}

// obsAttrs collects every (key, value) pair of a record into a map for
// assertion-friendly lookups.
func obsAttrs(r slog.Record) map[string]any {
	out := map[string]any{}
	r.Attrs(func(a slog.Attr) bool {
		out[a.Key] = a.Value.Any()
		return true
	})
	return out
}

// obsWithRecordingLogger installs a slog logger backed by handler as the
// default for the duration of the test and restores the previous default on
// cleanup.
func obsWithRecordingLogger(t *testing.T, handler *obsRecordHandler) {
	t.Helper()
	previous := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(previous) })
}

// TestObservability_AccessLog_EmitsExactlyOneRecord wires the middleware stack
// (RequestID → AccessLog) around a no-op handler, issues one request, and
// asserts the access-log invariant: exactly one record with msg="http request"
// carrying method, path, status, request_id.
//
// These tests intentionally do not call t.Parallel(): slog.SetDefault is
// process-global, so concurrent tests would race on the recorded handler.
//
//nolint:paralleltest // see comment above
func TestObservability_AccessLog_EmitsExactlyOneRecord(t *testing.T) {
	h := &obsRecordHandler{}
	obsWithRecordingLogger(t, h)

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	stack := middleware.RequestID(middleware.AccessLog(inner))

	req := httptest.NewRequest(http.MethodGet, "/observability-probe", nil)
	rec := httptest.NewRecorder()
	stack.ServeHTTP(rec, req)

	require.Equal(t, http.StatusTeapot, rec.Code)
	require.Equal(t, 1, h.obsRecordsByMsg("http request"), "expected exactly one access-log record")

	rec0 := h.obsFindRecord("http request")
	require.NotNil(t, rec0)
	attrs := obsAttrs(*rec0)
	assert.Equal(t, http.MethodGet, attrs["method"])
	assert.Equal(t, "/observability-probe", attrs["path"])
	assert.Equal(t, int64(http.StatusTeapot), assertInt64(attrs["status"]))
	assert.NotEmptyf(t, attrs["request_id"], "expected request_id attr on access-log record")
}

// TestObservability_AccessLog_PropagatesRequestIDHeader confirms that when the
// client supplies X-Request-ID, the access-log record carries the same value.
//
//nolint:paralleltest // slog.SetDefault is process-global
func TestObservability_AccessLog_PropagatesRequestIDHeader(t *testing.T) {
	h := &obsRecordHandler{}
	obsWithRecordingLogger(t, h)

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	stack := middleware.RequestID(middleware.AccessLog(inner))

	const fixedID = "deadbeef-1234-5678-9abc-def012345678"
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set(middleware.HeaderRequestID, fixedID)
	rec := httptest.NewRecorder()
	stack.ServeHTTP(rec, req)

	require.Equal(t, fixedID, rec.Header().Get(middleware.HeaderRequestID))
	rec0 := h.obsFindRecord("http request")
	require.NotNil(t, rec0)
	attrs := obsAttrs(*rec0)
	assert.Equal(t, fixedID, attrs["request_id"])
}

// TestObservability_AccessLog_OnAuthRouter exercises the full auth router
// surface (no bearer → 401) and confirms the access-log entry records the
// final 401 status, not the 200 default.
//
//nolint:paralleltest // slog.SetDefault is process-global
func TestObservability_AccessLog_OnAuthRouter(t *testing.T) {
	h := &obsRecordHandler{}
	obsWithRecordingLogger(t, h)

	rig := newTestRig(t)
	authRouter := chi.NewRouter()
	MountRouter(authRouter, rig.svc, rig.firebase, rig.usuarios, newNoopIdempotencyStore())
	stack := middleware.RequestID(middleware.AccessLog(authRouter))

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	rec := httptest.NewRecorder()
	stack.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, 1, h.obsRecordsByMsg("http request"))

	rec0 := h.obsFindRecord("http request")
	require.NotNil(t, rec0)
	attrs := obsAttrs(*rec0)
	assert.Equal(t, http.MethodGet, attrs["method"])
	assert.Equal(t, "/me", attrs["path"])
	assert.Equal(t, int64(http.StatusUnauthorized), assertInt64(attrs["status"]))
	assert.NotEmpty(t, attrs["request_id"])
}

// assertInt64 normalizes a status attr value to int64 — slog stores ints as
// int64 internally, but the underlying Value.Any() returns whatever concrete
// numeric type was passed. We coerce here so the assertion is total.
func assertInt64(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case uint64:
		return int64(n)
	}
	return -1
}
