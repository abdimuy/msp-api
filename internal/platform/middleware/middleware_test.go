package middleware_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/logger"
	"github.com/abdimuy/msp-api/internal/platform/middleware"
)

func TestRequestID_GeneratesNewWhenAbsent(t *testing.T) {
	t.Parallel()
	captured := ""
	h := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = logger.RequestIDFrom(r.Context())
	}))

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	h.ServeHTTP(rec, r)

	assert.NotEmpty(t, captured)
	assert.Equal(t, captured, rec.Header().Get(middleware.HeaderRequestID))
}

func TestRequestID_PreservesIncomingHeader(t *testing.T) {
	t.Parallel()
	captured := ""
	h := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = logger.RequestIDFrom(r.Context())
	}))

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set(middleware.HeaderRequestID, "incoming-id-1")
	h.ServeHTTP(rec, r)

	assert.Equal(t, "incoming-id-1", captured)
	assert.Equal(t, "incoming-id-1", rec.Header().Get(middleware.HeaderRequestID))
}

func TestRecovery_TurnsPanicInto500Problem(t *testing.T) {
	t.Parallel()
	h := middleware.Recovery(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(errors.New("boom"))
	}))

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	h.ServeHTTP(rec, r)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "panic")
}

func TestRecovery_NonErrorPanic(t *testing.T) {
	t.Parallel()
	h := middleware.Recovery(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("string panic")
	}))

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	h.ServeHTTP(rec, r)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestAccessLog_DoesNotAlterResponse(t *testing.T) {
	t.Parallel()
	h := middleware.AccessLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("hi"))
	}))

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	h.ServeHTTP(rec, r)

	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.Equal(t, "hi", rec.Body.String())
}

func TestTimeout_AppliesDeadlineToContext(t *testing.T) {
	t.Parallel()
	var deadlineSet bool
	h := middleware.Timeout(50 * time.Millisecond)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, deadlineSet = r.Context().Deadline()
	}))
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	h.ServeHTTP(rec, r)
	assert.True(t, deadlineSet, "downstream context must carry deadline")
}

func TestBodyLimit_RejectsLargeBody(t *testing.T) {
	t.Parallel()
	h := middleware.BodyLimit(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("this is way over ten bytes"))
	h.ServeHTTP(rec, r)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestCORS_AllowsKnownOrigin(t *testing.T) {
	t.Parallel()
	h := middleware.CORS([]string{"https://app.local"})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Origin", "https://app.local")
	h.ServeHTTP(rec, r)

	assert.Equal(t, "https://app.local", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", rec.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORS_BlocksUnknownOrigin(t *testing.T) {
	t.Parallel()
	h := middleware.CORS([]string{"https://app.local"})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Origin", "https://evil.example")
	h.ServeHTTP(rec, r)

	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_PreflightReturns204(t *testing.T) {
	t.Parallel()
	h := middleware.CORS([]string{"https://app.local"})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("inner handler must not be called for OPTIONS")
	}))

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodOptions, "/x", nil)
	r.Header.Set("Origin", "https://app.local")
	h.ServeHTTP(rec, r)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestSecurityHeaders_Set(t *testing.T) {
	t.Parallel()
	h := middleware.SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	h.ServeHTTP(rec, r)

	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
	assert.NotEmpty(t, rec.Header().Get("Referrer-Policy"))
}

// TestAccessLog_PreservesFlusher es la regresión del bug donde el
// statusRecorder interno wrappeaba el ResponseWriter sin re-exponer
// http.Flusher, rompiendo handlers de streaming (SSE) que hacen
// w.(http.Flusher) — devolvían 500 con code=no_flusher detrás del
// middleware aunque funcionaran en aislamiento (httptest.NewServer sin
// AccessLog). El test pega un handler que afirma la conversión a Flusher
// dentro de un servidor real con el middleware AccessLog encima.
func TestAccessLog_PreservesFlusher(t *testing.T) {
	t.Parallel()

	var (
		flusherOK bool
		flushed   bool
	)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f, ok := w.(http.Flusher)
		flusherOK = ok
		if ok {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			f.Flush()
			flushed = true
		}
	})

	srv := httptest.NewServer(middleware.AccessLog(handler))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.True(t, flusherOK, "AccessLog must preserve http.Flusher for streaming handlers")
	assert.True(t, flushed, "Flush() must not panic when called through the AccessLog wrapper")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "ok", string(body))
}

func TestIsClientGone(t *testing.T) {
	t.Parallel()
	assert.False(t, middleware.IsClientGone(context.Background()))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.True(t, middleware.IsClientGone(ctx))

	deadlineCtx, deadlineCancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer deadlineCancel()
	time.Sleep(time.Millisecond)
	require.Error(t, deadlineCtx.Err())
	assert.False(t, middleware.IsClientGone(deadlineCtx), "deadline exceeded is not 'client gone'")
}
