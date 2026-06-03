// Package middleware provides the HTTP middleware stack used by every router
// in this codebase.
//
// Standard order (outer→inner):
//
//	RequestID → Recovery → AccessLog → CORS → Timeout → BodyLimit → Auth → Idempotency
package middleware

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/logger"
	"github.com/abdimuy/msp-api/internal/platform/response"
)

// HeaderRequestID is the HTTP header used to propagate per-request IDs.
const HeaderRequestID = "X-Request-ID"

// errPanic is wrapped by Recovery so callers can identify panics via errors.Is.
var errPanic = errors.New("panic")

// RequestID assigns a UUID per request unless one already comes in the
// X-Request-ID header. The ID is added to the response and to the context.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(HeaderRequestID)
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set(HeaderRequestID, id)
		ctx := logger.WithRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Recovery converts panics into a 500 Problem and prevents the server from
// dying. The panic value and stack are logged.
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer recoverPanic(w, r)
		next.ServeHTTP(w, r)
	})
}

// recoverPanic is the deferred body of Recovery, extracted so contextcheck and
// gocognit don't trip over an inline closure.
func recoverPanic(w http.ResponseWriter, r *http.Request) {
	rec := recover()
	if rec == nil {
		return
	}
	err, ok := rec.(error)
	if !ok {
		err = fmt.Errorf("%w: %v", errPanic, rec)
	}
	slog.ErrorContext(
		r.Context(), "panic recovered",
		"error", err,
		"stack", string(debug.Stack()),
	)
	response.Error(w, r, apperror.NewInternal("panic", "ocurrió un error interno").WithError(err))
}

// statusRecorder wraps a ResponseWriter to capture the status code and bytes
// written, for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

// WriteHeader records the status code before forwarding it.
func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Write counts the bytes written before forwarding them.
func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Flush forwards to the wrapped ResponseWriter when it implements
// http.Flusher. Without this, the default *statusRecorder hides the
// Flusher interface of the underlying writer, breaking streaming handlers
// (e.g. SSE) that runtime-assert `w.(http.Flusher)`. No-op when the
// underlying writer doesn't support flushing.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap exposes the underlying ResponseWriter so http.NewResponseController
// can walk to it. Without Unwrap, ResponseController.SetWriteDeadline /
// SetReadDeadline / EnableFullDuplex / Hijack would fail with
// errNotSupported on any handler behind AccessLog — which means a streaming
// handler (e.g. SSE) cannot disable the server's WriteTimeout for itself
// even when needed. The Unwrap protocol is the http.ResponseWriter ladder
// formalized in Go 1.20.
func (s *statusRecorder) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}

// AccessLog logs each request with method, path, status, latency and bytes.
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.InfoContext(
			r.Context(), "http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", r.RemoteAddr,
		)
	})
}

// Timeout enforces a deadline on each request via context.
//
// On expiration the handler downstream sees ctx.Err() == context.DeadlineExceeded.
// Note: the response is *not* automatically truncated — handlers must check
// ctx.Err() before doing heavy work.
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isStreamingPath(r.URL.Path) {
				// Streaming endpoints (SSE) por definición duran indefinidamente;
				// aplicarles un deadline fijo cancela el ctx y la app cliente ve
				// un drop cada `d` segundos. Las conexiones se cierran solas
				// cuando el cliente desconecta (write error en el próximo evento
				// o ping del handler) o cuando el server se apaga (lifecycle).
				next.ServeHTTP(w, r)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// isStreamingPath identifica rutas long-lived que no deben sujetarse al
// Timeout middleware. Mantiene la lista pequeña y por sufijo exacto para
// que sea trivial auditar qué endpoints quedan exentos.
func isStreamingPath(p string) bool {
	return strings.HasSuffix(p, "/stream")
}

// BodyLimit caps the size of incoming request bodies.
// If the body exceeds the limit, downstream Read returns an error and we
// reply with a 413 Problem.
func BodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// CORS configures Cross-Origin Resource Sharing.
//
// allowedOrigins is checked exactly (no wildcards). The "Origin" request
// header must match one of the listed origins for the response to include
// access-control headers.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[o] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if _, ok := allowed[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID, Idempotency-Key")
				w.Header().Set("Access-Control-Max-Age", "300")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders adds conservative security headers to every response.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// Helpers for handlers.

// IsClientGone reports whether the request context was cancelled because the
// client disconnected (vs. a timeout / server-side cancellation).
func IsClientGone(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.Canceled)
}
