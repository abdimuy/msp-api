// Package idempotency implements an Idempotency-Key middleware so retries of
// non-idempotent requests (POST, PATCH) do not produce duplicate side effects.
//
// Caching policy: only 2xx responses are persisted. 4xx are not cached so a
// client that fixes its request and retries with the same key sees the new
// outcome (not a fossilized validation error). 5xx are not cached so transient
// infrastructure failures don't pin a key to a failure for 24h. This is the
// pattern used by Google Standard Payments and aligns with Stripe's "do not
// cache pre-execution validation errors" carve-out. The IETF Idempotency-Key
// draft (-07) §2.6 phrases caching as SHOULD, not MUST.
//
// Flow:
//
//  1. Client sends `Idempotency-Key: <opaque>` along with the request.
//  2. Middleware computes a SHA-256 of the request body and looks up the key.
//  3. If a 2xx record exists with the SAME hash, replay it.
//  4. If a 2xx record exists with a DIFFERENT hash, return 422 (per IETF
//     draft §2.7: "key is already being used for a different request
//     payload"). 409 is reserved for in-flight concurrency, not for body
//     mismatch — that was the prior buggy behavior.
//  5. If the key is new (or the prior response was 4xx/5xx and therefore not
//     cached), run the handler. Capture and store the response only if 2xx.
package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/response"
)

// HeaderKey is the HTTP header carrying the idempotency key.
const HeaderKey = "Idempotency-Key"

// DefaultTTL is the lifetime of stored response records.
const DefaultTTL = 24 * time.Hour

// Record is a stored idempotent response.
type Record struct {
	Key            string
	Method         string
	Path           string
	RequestHash    string
	ResponseStatus int
	ResponseBody   json.RawMessage
	ExpiresAt      time.Time
}

// ErrConflict signals that the key is in use with a different payload.
var ErrConflict = errors.New("idempotency: key conflict")

// ErrInProgress signals that another request with the same key is being
// processed concurrently. The Store may return this so the middleware can
// reply with 409 + Retry-After.
var ErrInProgress = errors.New("idempotency: request in progress")

// Store persists and retrieves response records. It is implemented in infra
// (e.g. Postgres) — kept as an interface so unit tests can use a fake.
type Store interface {
	// Get returns the stored record. Returns (nil, nil) when not found.
	Get(ctx context.Context, key string) (*Record, error)
	// Save records the final response. Implementations should be transactional
	// and idempotent themselves (UPSERT on key).
	Save(ctx context.Context, rec Record) error
}

// Config tunes the middleware.
type Config struct {
	Store Store
	TTL   time.Duration
	// Methods is the list of HTTP methods that REQUIRE an Idempotency-Key.
	// Defaults to {POST, PATCH}. Other methods skip the middleware entirely.
	Methods []string
	// RequireKey rejects requests on Methods without the header. Default true.
	RequireKey bool
}

func (c Config) requiresKey(method string) bool {
	methods := c.Methods
	if len(methods) == 0 {
		methods = []string{http.MethodPost, http.MethodPatch}
	}
	for _, m := range methods {
		if m == method {
			return true
		}
	}
	return false
}

// Middleware returns the idempotency middleware.
func Middleware(cfg Config) func(http.Handler) http.Handler {
	if cfg.TTL == 0 {
		cfg.TTL = DefaultTTL
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handle(cfg, next, w, r)
		})
	}
}

// handle is the per-request idempotency logic, extracted from Middleware to
// keep cognitive complexity within bounds.
func handle(cfg Config, next http.Handler, w http.ResponseWriter, r *http.Request) {
	if !cfg.requiresKey(r.Method) {
		next.ServeHTTP(w, r)
		return
	}

	key := r.Header.Get(HeaderKey)
	if key == "" {
		if cfg.RequireKey {
			response.Error(w, r, apperror.NewValidation(
				"idempotency_key_required",
				"el encabezado Idempotency-Key es obligatorio para "+r.Method,
			))
			return
		}
		next.ServeHTTP(w, r)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.Error(w, r, apperror.NewValidation(
			"request_body_read_failed", "no se pudo leer el cuerpo de la solicitud",
		).WithError(err))
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	hash := sha256Hex(body)

	if handled := tryReplay(cfg.Store, key, hash, w, r); handled {
		return
	}

	captureAndSave(cfg, key, hash, next, w, r)
}

// tryReplay returns true if the request was replayed from a stored response
// or if the key was reused with a mismatched payload.
func tryReplay(store Store, key, hash string, w http.ResponseWriter, r *http.Request) bool {
	rec, err := store.Get(r.Context(), key)
	if err != nil {
		slog.ErrorContext(r.Context(), "idempotency: store get failed", "error", err, "key", key)
		return false // fail-open: let the request proceed
	}
	if rec == nil {
		return false
	}
	if rec.RequestHash != hash || rec.Method != r.Method || rec.Path != r.URL.Path {
		// 422 (not 409) per IETF Idempotency-Key draft §2.7: this is a payload
		// mismatch, not in-flight concurrency. The body the client just sent
		// does not match the body that produced the cached 2xx response.
		response.Error(w, r, apperror.NewValidation(
			"idempotency_key_mismatch",
			"el Idempotency-Key fue reutilizado con una solicitud distinta",
		))
		return true
	}
	replay(w, *rec)
	return true
}

// captureAndSave runs the inner handler with a capturing writer. The captured
// response is persisted only when 2xx — see the package doc for the rationale.
func captureAndSave(cfg Config, key, hash string, next http.Handler, w http.ResponseWriter, r *http.Request) {
	rec := newCapture(w)
	next.ServeHTTP(rec, r)

	if !isSuccess(rec.status) {
		slog.DebugContext(r.Context(),
			"idempotency: response not cached (non-2xx)",
			"key", key, "status", rec.status,
		)
		return
	}

	toStore := Record{
		Key:            key,
		Method:         r.Method,
		Path:           r.URL.Path,
		RequestHash:    hash,
		ResponseStatus: rec.status,
		ResponseBody:   rec.body.Bytes(),
		ExpiresAt:      time.Now().Add(cfg.TTL),
	}
	if err := cfg.Store.Save(r.Context(), toStore); err != nil {
		slog.ErrorContext(r.Context(), "idempotency: store save failed", "error", err, "key", key)
	}
}

// isSuccess reports whether the status code is a 2xx success.
func isSuccess(status int) bool { return status >= 200 && status < 300 }

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func replay(w http.ResponseWriter, rec Record) {
	if ct := http.DetectContentType(rec.ResponseBody); ct != "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	w.Header().Set("Idempotent-Replay", "true")
	w.WriteHeader(rec.ResponseStatus)
	_, _ = w.Write(rec.ResponseBody)
}

// capture is a ResponseWriter that buffers status and body for storage.
type capture struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func newCapture(w http.ResponseWriter) *capture {
	return &capture{ResponseWriter: w, status: http.StatusOK}
}

// WriteHeader records the status code and forwards it.
func (c *capture) WriteHeader(code int) {
	c.status = code
	c.ResponseWriter.WriteHeader(code)
}

// Write buffers the bytes for later storage and forwards them to the client.
func (c *capture) Write(b []byte) (int, error) {
	_, _ = c.body.Write(b) // bytes.Buffer.Write never returns an error
	return c.ResponseWriter.Write(b)
}
