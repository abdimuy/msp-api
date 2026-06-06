// Package failedintent captures every 4xx/5xx response to a mutating HTTP
// request — the "venta-zombie" problem — so admins can later inspect the
// original payload and either replay or resolve it.
//
// Flow:
//
//  1. CaptureMiddleware sits inside the authentication + idempotency group,
//     so it only sees requests with a planted CurrentUser and a request body
//     that was not rejected at the auth boundary.
//  2. For requests on the configured method+path-prefix set, the middleware
//     buffers up to BodyCapBytes of the request body (then restores it for
//     the downstream handler) and wraps the ResponseWriter so it can
//     observe the final status code and an error-body snippet.
//  3. When the response status is >= 400, the middleware builds an Intent
//     and persists it via Store.Save. A Save failure is logged but never
//     propagated — failing the request because the capture pipeline broke
//     would be worse than losing one piece of evidence.
//
// Replay is performed by an admin via the http subpackage. To prevent the
// captured replay from being re-captured, the replay request carries the
// HeaderInternalReplay header which CaptureMiddleware checks before doing
// any work.
package failedintent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/platform/idempotency"
	"github.com/abdimuy/msp-api/internal/platform/logger"
)

// HeaderInternalReplay marks a request as an admin-initiated replay so the
// CaptureMiddleware skips it (preventing the replay from being recaptured).
// Intentionally distinct from idempotency.Idempotent-Replay (which is set on
// the response, not the request, when an idempotent cache hit replays a body).
const HeaderInternalReplay = "X-Internal-Replay"

// DefaultBodyCapBytes is the maximum request-body bytes captured per intent.
// Anything past this point is discarded and Intent.BodyTruncated is set.
const DefaultBodyCapBytes int64 = 256 * 1024

// DefaultResponseBodyCapBytes caps the captured *response* body snippet used
// to derive the apperror code/message. Responses past this size are
// truncated; the resulting error_code falls back to "" if parsing fails.
const DefaultResponseBodyCapBytes = 64 * 1024

// Status is the lifecycle state of a captured FailedIntent.
//
// Transition graph:
//
//	new ──────▶ retried_ok         (replay returned 2xx/3xx)
//	new ──────▶ retried_fail       (replay returned 4xx/5xx)
//	new ──────▶ ignored            (admin marked it intentional)
//	new ──────▶ resolved_manual    (admin fixed downstream and marked it)
//
// Re-replay of a terminal intent is permitted but it does NOT change the
// status — see Service.Replay in the http subpackage.
type Status string

// Status values; keep stable strings — they are persisted.
const (
	// StatusNew is the freshly-captured state.
	StatusNew Status = "new"
	// StatusRetriedOK records a successful admin replay (2xx/3xx).
	StatusRetriedOK Status = "retried_ok"
	// StatusRetriedFail records a failed admin replay (4xx/5xx).
	StatusRetriedFail Status = "retried_fail"
	// StatusIgnored marks the intent as a known false-positive.
	StatusIgnored Status = "ignored"
	// StatusResolvedManual marks the intent as fixed downstream.
	StatusResolvedManual Status = "resolved_manual"
)

// Valid reports whether s is one of the defined Status values.
func (s Status) Valid() bool {
	switch s {
	case StatusNew, StatusRetriedOK, StatusRetriedFail, StatusIgnored, StatusResolvedManual:
		return true
	}
	return false
}

// IsTerminal reports whether s is a non-new (terminal) state.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusRetriedOK, StatusRetriedFail, StatusIgnored, StatusResolvedManual:
		return true
	case StatusNew:
		return false
	}
	return false
}

// String implements fmt.Stringer.
func (s Status) String() string { return string(s) }

// Intent is the canonical captured record.
//
// Body and BodyBlobPath are mutually exclusive in practice:
//
//   - JSON capture path: Body holds the (possibly truncated) payload bytes
//     and BodyBlobPath is "".
//   - Multipart capture path: Body is empty (null JSON), BodyBlobPath points
//     to the on-disk blob written by BlobStorage.Save, and BodyContentType
//     holds the original Content-Type header (including the multipart
//     boundary) so replay can reconstruct the request byte-exact.
//
// The schema does NOT enforce this invariant — it lives here, per CLAUDE.md.
type Intent struct {
	ID              uuid.UUID
	ReceivedAt      time.Time
	Method          string
	Path            string
	FirebaseUID     string
	UsuarioID       *uuid.UUID
	IdempotencyKey  string
	RequestID       uuid.UUID
	Body            json.RawMessage
	BodyTruncated   bool
	BodyBlobPath    string
	BodyContentType string
	HTTPStatus      int
	ErrorCode       string
	ErrorMessage    string
	RetryCount      int
	Status          Status
	ResolvedAt      *time.Time
	ResolvedBy      *uuid.UUID
	Notes           string
}

// ListParams is the cursor-paginated input for Store.List.
type ListParams struct {
	// CursorReceivedAt + CursorID together form a stable cursor.
	// Both zero means "from the newest row".
	CursorReceivedAt time.Time
	CursorID         uuid.UUID
	// Status, when non-empty, restricts results to that status.
	Status Status
	// UsuarioID, when non-nil, restricts the result set to intents owned by
	// the specified usuario. Used by GET /v2/me/failed-intents.
	UsuarioID *uuid.UUID
	// PageSize is clamped to [1, 100] by implementations.
	PageSize int
}

// Page is the generic cursor-paginated result returned by Store.List.
type Page[T any] struct {
	Items          []T
	NextReceivedAt time.Time
	NextID         uuid.UUID
	HasMore        bool
}

// PurgeResult is the outcome of Store.PurgeOlderThan. BlobPaths lists every
// non-empty body_blob_path of the rows that were just deleted, so the caller
// (janitor) can hand them to BlobStorage.Delete in one pass — keeping rows
// and blobs in lockstep without a second SELECT.
type PurgeResult struct {
	RowsDeleted int64
	BlobPaths   []string
}

// Store persists and retrieves Intent records.
//
//nolint:interfacebloat // 7 methods, within the 8-method cap.
type Store interface {
	// Save inserts an intent. Implementations should treat (id) as a
	// uniqueness constraint and silently no-op on duplicate primary key.
	Save(ctx context.Context, i Intent) error

	// Get loads an intent by id. Returns (nil, nil) when not found.
	Get(ctx context.Context, id uuid.UUID) (*Intent, error)

	// List returns a page of intents ordered by received_at DESC, id DESC.
	List(ctx context.Context, p ListParams) (Page[Intent], error)

	// UpdateStatus moves the intent from expected → next in a single
	// statement. Returns an apperror.NewConflict("failed_intent_status_conflict",
	// ...) when 0 rows match (i.e. the row's status no longer equals expected).
	UpdateStatus(
		ctx context.Context,
		id uuid.UUID,
		expected, next Status,
		resolvedBy uuid.UUID,
		notes string,
		now time.Time,
	) error

	// IncrementRetry bumps retry_count by 1 without changing status. Used on
	// each replay attempt START so the count reflects attempts, not outcomes.
	IncrementRetry(ctx context.Context, id uuid.UUID) error

	// PurgeOlderThan deletes rows whose received_at is strictly less than
	// `before`. Returns the deletion count plus every non-empty
	// body_blob_path of the deleted rows so the caller can clean the
	// matching on-disk blobs.
	PurgeOlderThan(ctx context.Context, before time.Time) (PurgeResult, error)

	// ReferencedPaths returns every non-empty body_blob_path currently in
	// failed_intents. Used by the boot-time orphan sweep to detect blob
	// files on disk that no longer have a database referent.
	ReferencedPaths(ctx context.Context) ([]string, error)
}

// ReplayDispatcher dispatches a reconstructed *http.Request through the
// application router and writes the response into w. Implementations are
// expected to wrap a chi.Router; the interface keeps the http subpackage
// decoupled from chi types and breaks the dispatcher↔router↔handler cycle.
type ReplayDispatcher interface {
	Dispatch(w http.ResponseWriter, r *http.Request)
}

// Config tunes CaptureMiddleware.
type Config struct {
	// Store is the persistence backend. Required.
	Store Store
	// PathPrefixes lists request-path prefixes that opt-in to capture.
	// Defaults to {"/v2/ventas"}.
	PathPrefixes []string
	// Methods lists HTTP methods that opt-in to capture.
	// Defaults to {POST, PATCH, PUT}.
	Methods []string
	// BodyCapBytes is the maximum captured request body. Defaults to
	// DefaultBodyCapBytes (256 KiB).
	BodyCapBytes int64
	// Clock supplies the current time. Injected for tests; defaults to
	// time.Now when nil.
	Clock func() time.Time
	// NewID supplies the captured Intent's primary key. Injected for tests;
	// defaults to uuid.New when nil.
	NewID func() uuid.UUID
}

func (c *Config) defaults() {
	if len(c.PathPrefixes) == 0 {
		c.PathPrefixes = []string{"/v2/ventas"}
	}
	if len(c.Methods) == 0 {
		c.Methods = []string{http.MethodPost, http.MethodPatch, http.MethodPut}
	}
	if c.BodyCapBytes == 0 {
		c.BodyCapBytes = DefaultBodyCapBytes
	}
	if c.Clock == nil {
		c.Clock = time.Now
	}
	if c.NewID == nil {
		c.NewID = uuid.New
	}
}

// CaptureMiddleware returns a chi-compatible middleware that captures every
// response with status >= 400 on requests matching cfg.Methods and
// cfg.PathPrefixes. The middleware buffers the request body up to
// cfg.BodyCapBytes, restores it for downstream handlers, and persists the
// captured Intent best-effort: a Store.Save failure is logged but never
// propagated.
//
// CaptureMiddleware must run INSIDE the auth chain so it sees the planted
// CurrentUser; requests that fail auth never reach this middleware by design.
func CaptureMiddleware(cfg Config) func(http.Handler) http.Handler {
	cfg.defaults()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handle(cfg, next, w, r)
		})
	}
}

// handle is the per-request capture body, extracted so the closure stays
// cyclomatically trivial.
func handle(cfg Config, next http.Handler, w http.ResponseWriter, r *http.Request) {
	if !shouldCapture(cfg, r) {
		next.ServeHTTP(w, r)
		return
	}

	body, truncated, err := readCappedBody(r, cfg.BodyCapBytes)
	if err != nil {
		// Reading the body failed before we even reached the handler — log
		// and pass through with an empty body (caller will see EOF and
		// likely produce a 400, which we then capture as best-effort).
		slog.WarnContext(r.Context(),
			"failedintent: body read failed before capture",
			"error", err, "path", r.URL.Path,
		)
		next.ServeHTTP(w, r)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	cw := newCaptureWriter(w)
	next.ServeHTTP(cw, r)

	if cw.status < http.StatusBadRequest {
		return
	}
	intent := buildIntent(cfg, r, body, truncated, cw)
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	if saveErr := cfg.Store.Save(saveCtx, intent); saveErr != nil {
		slog.ErrorContext(r.Context(),
			"failedintent: store save failed",
			"error", saveErr,
			"intent_id", intent.ID,
			"path", intent.Path,
			"http_status", intent.HTTPStatus,
		)
		return
	}
	emitCapturedLog(r.Context(), intent)
}

// shouldCapture is the predicate gating the rest of the middleware. Kept
// extracted so the negative cases are a single boolean expression in handle.
func shouldCapture(cfg Config, r *http.Request) bool {
	if r.Header.Get(HeaderInternalReplay) != "" {
		return false
	}
	if !methodMatches(cfg.Methods, r.Method) {
		return false
	}
	if !pathPrefixMatches(cfg.PathPrefixes, r.URL.Path) {
		return false
	}
	// Skip multipart uploads — bodies are binary and far over BodyCapBytes;
	// capturing would either crash JSONB or dump base64 noise into the DB.
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		return false
	}
	return true
}

func methodMatches(methods []string, m string) bool {
	for _, mm := range methods {
		if mm == m {
			return true
		}
	}
	return false
}

func pathPrefixMatches(prefixes []string, p string) bool {
	for _, pp := range prefixes {
		if strings.HasPrefix(p, pp) {
			return true
		}
	}
	return false
}

// readCappedBody reads up to cap+1 bytes from r.Body so we can detect overflow
// in a single pass. Returns the (possibly trimmed) body and whether truncation
// occurred. Caller must restore r.Body.
func readCappedBody(r *http.Request, capBytes int64) ([]byte, bool, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, false, nil
	}
	buf, err := io.ReadAll(io.LimitReader(r.Body, capBytes+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(buf)) > capBytes {
		return buf[:capBytes], true, nil
	}
	return buf, false, nil
}

// captureWriter is a ResponseWriter that records status code + a bounded body
// snippet so we can derive the error code/message.
type captureWriter struct {
	http.ResponseWriter
	status   int
	body     bytes.Buffer
	bodyCap  int
	bodyFull bool
}

func newCaptureWriter(w http.ResponseWriter) *captureWriter {
	return &captureWriter{ResponseWriter: w, status: http.StatusOK, bodyCap: DefaultResponseBodyCapBytes}
}

// WriteHeader records the status code and forwards it.
func (c *captureWriter) WriteHeader(code int) {
	c.status = code
	c.ResponseWriter.WriteHeader(code)
}

// Write buffers up to bodyCap bytes for later inspection then forwards to the
// underlying writer.
func (c *captureWriter) Write(b []byte) (int, error) {
	c.bufferForCapture(b)
	return c.ResponseWriter.Write(b)
}

// bufferForCapture stores as much of b as fits in the remaining capacity.
// Extracted from Write to keep nesting shallow.
func (c *captureWriter) bufferForCapture(b []byte) {
	if c.bodyFull {
		return
	}
	remaining := c.bodyCap - c.body.Len()
	if remaining <= 0 {
		c.bodyFull = true
		return
	}
	take := len(b)
	if take > remaining {
		take = remaining
		c.bodyFull = true
	}
	_, _ = c.body.Write(b[:take])
}

// buildIntent assembles the canonical Intent record from the captured
// request, body and response.
func buildIntent(cfg Config, r *http.Request, body []byte, truncated bool, cw *captureWriter) Intent {
	now := cfg.Clock()
	intent := Intent{
		ID:             cfg.NewID(),
		ReceivedAt:     now,
		Method:         r.Method,
		Path:           r.URL.Path,
		IdempotencyKey: r.Header.Get(idempotency.HeaderKey),
		RequestID:      requestIDOrNew(r.Context()),
		Body:           normaliseBody(body, &truncated),
		BodyTruncated:  truncated,
		HTTPStatus:     cw.status,
		RetryCount:     0,
		Status:         StatusNew,
	}
	if cu, ok := auth.CurrentUserFromContext(r.Context()); ok {
		intent.FirebaseUID = cu.FirebaseUID
		id := cu.ID
		intent.UsuarioID = &id
	}
	intent.ErrorCode, intent.ErrorMessage = parseProblemJSON(cw.body.Bytes())
	return intent
}

// requestIDOrNew returns the planted request ID parsed as a UUID; if the
// header was set to a non-UUID value (a free-form trace ID, for instance)
// we generate a fresh UUID so the JSONB column constraint is honoured.
func requestIDOrNew(ctx context.Context) uuid.UUID {
	rid := logger.RequestIDFrom(ctx)
	if rid == "" {
		return uuid.New()
	}
	parsed, err := uuid.Parse(rid)
	if err != nil {
		return uuid.New()
	}
	return parsed
}

// normaliseBody ensures the captured body is valid JSON for the JSONB column.
// Non-JSON bodies (e.g. text/plain) are wrapped as a JSON string and flagged
// as truncated so admins know the original wasn't pure JSON.
func normaliseBody(body []byte, truncated *bool) json.RawMessage {
	if len(body) == 0 {
		return json.RawMessage(`null`)
	}
	if json.Valid(body) {
		return json.RawMessage(body)
	}
	*truncated = true
	// json.Marshal of a Go string only fails when the string contains invalid
	// UTF-8 sequences; in that case the fallback below produces a stable
	// JSON null so the JSONB column constraint is honoured.
	wrapped, err := json.Marshal(string(body))
	if err != nil {
		return json.RawMessage(`null`)
	}
	return wrapped
}

// problemBodyShape is the subset of RFC 9457 Problem fields we need to read
// back to populate Intent.ErrorCode / ErrorMessage. Other fields are ignored.
type problemBodyShape struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
	Title  string `json:"title"`
}

// parseProblemJSON tolerantly extracts the error code + user-facing message
// from a captured response body. Non-Problem-shaped bodies yield ("", "").
func parseProblemJSON(body []byte) (string, string) {
	if len(body) == 0 || !json.Valid(body) {
		return "", ""
	}
	var p problemBodyShape
	if err := json.Unmarshal(body, &p); err != nil {
		return "", ""
	}
	msg := p.Detail
	if msg == "" {
		msg = p.Title
	}
	return p.Code, msg
}

// emitCapturedLog records the capture as a structured event. Never logs the
// body — only metadata that's safe for support staff to see.
func emitCapturedLog(ctx context.Context, i Intent) {
	slog.InfoContext(ctx, "failedintent.captured",
		"intent_id", i.ID,
		"firebase_uid", i.FirebaseUID,
		"http_status", i.HTTPStatus,
		"error_code", i.ErrorCode,
		"method", i.Method,
		"path", i.Path,
		"body_truncated", i.BodyTruncated,
	)
}

// ErrStatusConflict is the sentinel returned by Store.UpdateStatus when the
// expected→next transition no longer applies. Implementations should wrap it
// inside an apperror.NewConflict so the HTTP layer maps it to 409.
var ErrStatusConflict = errors.New("failedintent: status conflict")
