// Package failedintent captures every 4xx/5xx response to a mutating HTTP
// request — the "venta-zombie" problem — so admins can later inspect the
// original payload and either replay or resolve it.
//
// Flow:
//
//  1. CaptureMiddleware is mounted twice per captured module: inside the
//     authentication + idempotency group, where it sees the planted
//     CurrentUser, and outside it, narrowed via Config.CaptureStatuses to the
//     401 the auth boundary answers on its own — the one failure the inside
//     instance can never observe.
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
	"slices"
	"strings"
	"sync"
	"sync/atomic"
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

// HeaderIdempotentReplay is the response header the idempotency middleware
// sets when it replays a cached response. When CaptureMiddleware wraps the
// idempotency middleware (the venta-zombie order), it MUST skip saving on
// replays — the underlying response was already captured on the original
// 4xx/5xx call and saving again would duplicate the audit row.
const HeaderIdempotentReplay = "Idempotent-Replay"

// HeaderIntentCaptured is the response header that confirms custody: the
// server could not apply the capture, but it is stored and the office can
// correct it. Its value is the Intent UUID.
//
// It is emitted ONLY when Store.Save returned nil. The field device reads it as
// the stop condition for retrying — without it, a 4xx/5xx is retried forever —
// so emitting it on a failed save would silently drop the capture. See
// docs/module-standards/ENTREGA_GARANTIZADA.md.
const HeaderIntentCaptured = "X-Intent-Captured"

// deferredBodyCapBytes caps how much of an error response the middleware holds
// in memory while it decides whether to confirm custody. Problem+JSON bodies
// are a few hundred bytes; anything past this cap gives up the confirmation
// rather than risk the response.
const deferredBodyCapBytes = 1 << 20 // 1 MiB

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
	ID         uuid.UUID
	ReceivedAt time.Time
	// LastSeenAt es el ÚLTIMO intento con esta clave. Nil cuando la fila se
	// ha visto una sola vez.
	//
	// Con la dedup, ReceivedAt dejó de significar "cuándo pasó esto" para
	// significar "el PRIMER intento". Los dos juntos son lo que permite
	// escribir "13 intentos · desde 13:20 · el último hace 4 minutos"; sólo
	// con el primero, una venta cuyo último reintento fue hace cuatro minutos
	// se leería como de hace seis horas.
	LastSeenAt      *time.Time
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
	// Modulo es el módulo dueño de la ruta ('ventas', 'pagos'). Vacío cuando
	// ninguna ruta registrada casa con Path — el escritorio degrada entonces
	// a deducirlo de la ruta, como hacía antes.
	//
	// Es columna real e indexada, no un derivado en memoria, porque los chips
	// del listado filtran por él: filtrar en memoria sobre una página daría
	// "las ventas que cupieron en los primeros veinte renglones".
	Modulo string
	// Resumen es quién y cuánto — el dato que hace legible un renglón. Nil
	// cuando no se pudo extraer, que es honesto: la pantalla muestra un hueco
	// en vez de un nombre inventado.
	Resumen *Resumen
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
	// Modulo, cuando no está vacío, restringe el resultado a ese módulo. Es
	// el filtro de los chips del escritorio y va en SQL a propósito: filtrar
	// en memoria sobre una página ya paginada mostraría "las ventas que
	// cupieron en los primeros veinte renglones" y nada advertiría del resto.
	Modulo string
	// SinExtraer, cuando es true, restringe el resultado a las filas a las
	// que NUNCA se les corrió el extractor: MODULO y RESUMEN los dos nulos.
	//
	// Son las dos columnas y no sólo RESUMEN a propósito. Un cuerpo que el
	// extractor no reconoce deja MODULO puesto y RESUMEN nulo — es un
	// resultado, no un pendiente. Si el filtro mirara sólo RESUMEN, esas filas
	// se reabrirían en cada ciclo del janitor para siempre, releyendo su blob
	// cada hora para volver a no reconocerlo.
	//
	// Lo usa el relleno del janitor; el listado del escritorio nunca lo
	// enciende.
	SinExtraer bool
	// RutasRaiz, cuando no está vacía, acota el resultado a las filas cuyo
	// PATH es EXACTAMENTE una de esas rutas.
	//
	// Es el corte por etapa. Una petición a la ruta raíz de un módulo
	// —`POST /v2/ventas`— es una creación: si falló, la venta no quedó en
	// ninguna parte y esta pantalla es su único rastro. Una petición con id
	// en la ruta —`/v2/ventas/{id}/aplicar`, `PATCH /v2/ventas/{id}`— es
	// posterior: la fila YA existe, y el renglón nunca podrá decir de quién
	// es porque el cuerpo de esa petición jamás llevó un nombre.
	//
	// La lista NO se arma aquí. Esta plataforma no sabe —ni debe saber— qué
	// ruta de qué módulo es una creación: eso lo sabe la raíz de composición,
	// que es donde se declaran los prefijos de captura de cada módulo. Se
	// pasa desde ahí para que no exista una segunda lista capaz de
	// desincronizarse de la primera.
	//
	// Vacía significa "sin acotar", que es la respuesta correcta para quien
	// no tiene esa lista (las pruebas, el janitor).
	RutasRaiz []string
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

// SaveOutcome reports what Save actually did, so the caller can keep the
// on-disk blobs in step with the rows.
//
// Sin esto la dedup arreglaría la mitad del problema: la tabla dejaría de
// crecer y el disco no. Medido en producción sobre 7 días: 609 filas para 130
// ventas, **685 archivos y 896 MB**; una sola venta dejó 13 copias de 2.8 MB.
// El cuerpo que la dedup descarta es un archivo ya escrito, y el único que
// sabe borrarlo es quien tiene el BlobStorage — el middleware, no el Store.
type SaveOutcome struct {
	// Deduped reports that the intent was folded into an existing pending
	// row instead of inserting a new one.
	Deduped bool
	// OrphanedBlobPath is the blob that no row references anymore after this
	// Save: el nuevo cuando se conservó el guardado, el viejo cuando el nuevo
	// lo reemplazó. Vacío cuando no sobra ninguno.
	OrphanedBlobPath string
}

// Store persists and retrieves Intent records.
//
// MarkResolvedByKeys. La alternativa era un segundo puerto con una sola
// operación sobre la misma tabla, que reparte la custodia de la evidencia
// entre dos interfaces sin ganar nada.
//
//nolint:interfacebloat // 9 métodos: el cap de 8 se subió una vez, por
type Store interface {
	// Save persists an intent.
	//
	// Cuando el intento trae IDEMPOTENCY_KEY y ya existe una fila PENDIENTE
	// con la misma (PATH, IDEMPOTENCY_KEY), la implementación debe FUNDIRLO
	// en ella —subir RETRY_COUNT, refrescar el error y LAST_SEEN_AT, y
	// CONSERVAR RECEIVED_AT— en vez de insertar una fila nueva. Sin clave no
	// hay nada que deduplicar y se inserta.
	//
	// Un conflicto de llave primaria (mismo id) se trata como no-op, igual
	// que antes: un reintento de la captura no puede corromper la evidencia
	// que ya está guardada.
	Save(ctx context.Context, i Intent) (SaveOutcome, error)

	// Get loads an intent by id. Returns (nil, nil) when not found.
	Get(ctx context.Context, id uuid.UUID) (*Intent, error)

	// List returns a page of intents ordered by received_at DESC, id DESC.
	List(ctx context.Context, p ListParams) (Page[Intent], error)

	// UpdateStatus moves the intent from expected → next AND records the
	// operator (ResolvedAt + ResolvedBy + Notes). This signature is reserved
	// for the operator-driven Resolver endpoint (PATCH /{id}/resolve) where
	// next is StatusIgnored or StatusResolvedManual. Do NOT use this for
	// replay outcomes — call TransitionAfterReplay instead.
	//
	// Returns an apperror.NewConflict("failed_intent_status_conflict", ...)
	// when 0 rows match (i.e. the row's status no longer equals expected).
	UpdateStatus(
		ctx context.Context,
		id uuid.UUID,
		expected, next Status,
		resolvedBy uuid.UUID,
		notes string,
		now time.Time,
	) error

	// TransitionAfterReplay updates STATUS only, leaving ResolvedAt /
	// ResolvedBy / Notes unchanged. Used by Service.tryUpdateStatus after a
	// replay so the operator-resolution fields stay reserved for explicit
	// "marked as ignored or resolved_manual" actions.
	//
	// Returns failed_intent_status_conflict (same shape as UpdateStatus) when
	// 0 rows match — the id is gone or the current status differs from
	// expected.
	TransitionAfterReplay(
		ctx context.Context,
		id uuid.UUID,
		expected, next Status,
	) error

	// IncrementRetry bumps retry_count by 1 without changing status. Used on
	// each replay attempt START so the count reflects attempts, not outcomes.
	IncrementRetry(ctx context.Context, id uuid.UUID) error

	// GuardarResumen escribe MODULO y RESUMEN de una fila ya capturada.
	//
	// Sólo lo llama el relleno del janitor, que ilumina las filas anteriores
	// al despliegue del extractor. No toca ninguna otra columna: la fila ya
	// es evidencia y el resumen es un adorno encima de ella.
	//
	// Un resumen nil escribe RESUMEN NULL y deja MODULO en lo que se le pase
	// — la combinación honesta de "sé de qué módulo es, no supe leer su
	// cuerpo".
	GuardarResumen(ctx context.Context, id uuid.UUID, modulo string, r *Resumen) error

	// PurgeOlderThan deletes rows whose received_at is strictly less than
	// `before`. Returns the deletion count plus every non-empty
	// body_blob_path of the deleted rows so the caller can clean the
	// matching on-disk blobs.
	//
	// `estados` acota el borrado a esos STATUS; vacío significa todos. Es
	// variádico y no un parámetro más porque el corte por estado llegó
	// después: sin él, los intentos ya resueltos —cuyo cuerpo ya no le sirve
	// a nadie— ocupaban disco los mismos 90 días que los pendientes.
	PurgeOlderThan(ctx context.Context, before time.Time, estados ...Status) (PurgeResult, error)

	// ReferencedPaths returns every non-empty body_blob_path currently in
	// failed_intents. Used by the boot-time orphan sweep to detect blob
	// files on disk that no longer have a database referent.
	ReferencedPaths(ctx context.Context) ([]string, error)

	// MarkResolvedByKeys cierra como StatusResolvedManual los intentos
	// PENDIENTES de `path` cuya IDEMPOTENCY_KEY esté en keys, sellando
	// RESOLVED_AT con `now`. Devuelve cuántas filas cambiaron.
	//
	// Es el único camino por el que un intento se cierra sin que una persona
	// lo toque, y por eso el criterio es estrecho: misma ruta, misma clave,
	// y sólo filas pendientes. Una fila ya cerrada no se reabre ni se
	// re-sella; la decisión que tomó alguien se queda.
	//
	// Lo usan dos llamadores con la misma pregunta: el middleware cuando ve
	// llegar un 2xx con una clave que ya tenía intento fallido, y el janitor
	// cuando la fuente confirma que el trabajo aterrizó por otro camino.
	// keys vacío es un no-op sin ir a la base.
	MarkResolvedByKeys(ctx context.Context, path string, keys []string, now time.Time) (int64, error)
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
	// Blob, when non-nil, opts the middleware into multipart capture:
	// the body is streamed to BlobStorage.Save and the resulting path is
	// persisted on the Intent. Leaving it nil preserves the pre-blob
	// behavior of skipping multipart requests entirely.
	Blob BlobStorage
	// PathPrefixes lists request-path prefixes that opt-in to capture.
	// Defaults to {"/v2/ventas"}.
	PathPrefixes []string
	// Methods lists HTTP methods that opt-in to capture.
	// Defaults to {POST, PATCH, PUT}.
	Methods []string
	// CaptureStatuses restricts this instance to the listed response status
	// codes. The zero value (nil) captures every response >= 400, which is
	// the historical behavior and what the in-chain instances use.
	//
	// It exists for an instance mounted OUTSIDE the authentication chain,
	// which owns the one status the in-chain instance can never observe: the
	// 401 that authn answers before the request ever reaches it. Splitting
	// the statuses between the two instances is what keeps one request from
	// producing two rows.
	//
	// Setting it also makes this instance a narrow outside observer, with
	// two deliberate consequences:
	//
	//   - Multipart bodies are NOT streamed to blob storage while the
	//     handler runs. An instance that captures almost nothing must not
	//     write (and then delete) a copy of every upload that succeeds. The
	//     body is read AFTER the response instead, which works precisely
	//     because the rejection this instance exists for answers without
	//     reading it. When something downstream did read it, the body cannot
	//     be reconstructed and the row is persisted truncated.
	//   - Closing a pending intent whose work just landed (cerrarPorExito)
	//     is left to the in-chain instance, which sees every authenticated
	//     request anyway. Doing it here would only repeat the query.
	CaptureStatuses []int
	// BodyCapBytes is the maximum captured request body. Defaults to
	// DefaultBodyCapBytes (256 KiB).
	BodyCapBytes int64
	// MaxMultipartBytes caps the per-blob size on the multipart path.
	// Defaults to DefaultMaxMultipartBytes (50 MiB).
	MaxMultipartBytes int64
	// Clock supplies the current time. Injected for tests; defaults to
	// time.Now when nil.
	Clock func() time.Time
	// NewID supplies the captured Intent's primary key. Injected for tests;
	// defaults to uuid.New when nil.
	NewID func() uuid.UUID
	// Resumen, cuando no es nil, extrae el dato de negocio del cuerpo
	// capturado (quién, cuánto) y el módulo dueño de la ruta. Dependencia
	// OPCIONAL: sin ella la captura funciona igual que antes y las filas
	// quedan con MODULO/RESUMEN nulos.
	//
	// En producción es un RegistroExtractores armado en la raíz de
	// composición — el único sitio donde la plataforma se entera de que
	// existen módulos.
	Resumen ResumenExtractor
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
	if c.MaxMultipartBytes == 0 {
		c.MaxMultipartBytes = DefaultMaxMultipartBytes
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
// The instance that owns UsuarioID must run INSIDE the auth chain so it sees
// the planted CurrentUser — a captured intent without it cannot be replayed as
// the original requester. That placement is also why a request rejected AT the
// auth boundary never reaches it: covering those takes a second instance
// mounted outside the chain and narrowed with CaptureStatuses.
func CaptureMiddleware(cfg Config) func(http.Handler) http.Handler {
	cfg.defaults()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			//nolint:contextcheck // the detached save contexts are annotated at their own call sites.
			handle(cfg, next, w, r)
		})
	}
}

// handle is the per-request capture body, extracted so the closure stays
// cyclomatically trivial. Multipart requests are routed to handleMultipart
// when Blob storage is configured; everything else falls through to
// handleJSON.
func handle(cfg Config, next http.Handler, w http.ResponseWriter, r *http.Request) {
	if !shouldCapture(cfg, r) {
		next.ServeHTTP(w, r)
		return
	}
	multipart := cfg.Blob != nil && isMultipart(r)
	switch {
	case multipart && statusFiltered(cfg):
		handleMultipartDeferred(cfg, next, w, r)
	case multipart:
		handleMultipart(cfg, next, w, r)
	case statusFiltered(cfg):
		handleJSONDeferred(cfg, next, w, r)
	default:
		handleJSON(cfg, next, w, r)
	}
}

// statusFiltered reports whether cfg restricts capture to a fixed set of
// status codes — that is, whether this is the narrow instance mounted outside
// the auth chain rather than the in-chain one.
func statusFiltered(cfg Config) bool {
	return len(cfg.CaptureStatuses) > 0
}

// shouldPersist decides, once the response is known, whether THIS instance
// owns the evidence for it. shouldCapture is its counterpart before the
// request runs, when only method, path and content-type are known.
func shouldPersist(ctx context.Context, cfg Config, cw *captureWriter) bool {
	if cw.status < http.StatusBadRequest {
		return false
	}
	if isIdempotentReplay(cw) {
		// Cached response from idempotency — the underlying intent was
		// already captured on the original call. Skipping avoids duplicate
		// rows on every retry.
		return false
	}
	if custodyClaimed(ctx, cw) {
		// Another capture instance further in already saved this response
		// and stamped the confirmation on the shared header map. It knows
		// more than we do (it ran inside the auth chain, so its row carries
		// the requester), and a second row would be the same evidence twice.
		return false
	}
	if statusFiltered(cfg) && !slices.Contains(cfg.CaptureStatuses, cw.status) {
		return false
	}
	return true
}

// custodyClaimed reports whether a capture instance further in already took
// custody of this response.
//
// Two signals, because neither alone is enough:
//
//   - The nested captureWriters share one header map, so a confirmation an
//     inner instance stamped is visible here. But that signal can legitimately
//     be missing: captureWriter.Write releases the withheld response early
//     when it outgrows deferredBodyCapBytes, and from then on flushDeferred
//     cannot add the header — the row exists and the header does not.
//   - The custody mark on the context, which saveIntent sets the moment a row
//     is in the table, regardless of what the response looks like. The outer
//     instance plants the holder before running the chain; without one, the
//     mark is a no-op.
//
// Being the ONLY lock on the overlap, it cannot rest on a signal that may go
// missing.
func custodyClaimed(ctx context.Context, cw *captureWriter) bool {
	return cw.Header().Get(HeaderIntentCaptured) != "" || custodyMarked(ctx)
}

// custodyMarkKey is the context key for the custody holder.
type custodyMarkKey struct{}

// custodyMark is the holder a narrowed instance plants on the request context
// so any capture running further in can report that it saved a row. A pointer
// with an atomic flag, because the context itself is immutable and a handler
// may run the chain from a goroutine of its own.
type custodyMark struct {
	claimed atomic.Bool
}

// withCustodyMark returns ctx carrying a fresh custody holder.
func withCustodyMark(ctx context.Context) context.Context {
	return context.WithValue(ctx, custodyMarkKey{}, &custodyMark{})
}

// markCustody records that a row for this request is in the table. No-op when
// nobody planted a holder, which is the single-instance case.
func markCustody(ctx context.Context) {
	if m, ok := ctx.Value(custodyMarkKey{}).(*custodyMark); ok {
		m.claimed.Store(true)
	}
}

// custodyMarked reports whether some capture already saved a row for this
// request.
func custodyMarked(ctx context.Context) bool {
	m, ok := ctx.Value(custodyMarkKey{}).(*custodyMark)
	return ok && m.claimed.Load()
}

// handleJSON is the original capture path: buffer up to BodyCapBytes, run the
// downstream handler, persist the captured intent on >=400. Unchanged
// behavior from before the blob support.
func handleJSON(cfg Config, next http.Handler, w http.ResponseWriter, r *http.Request) {
	body, truncated, err := readCappedBody(r, cfg.BodyCapBytes)
	if err != nil {
		// Reading the body failed before we even reached the handler — log
		// and pass through with an empty body (caller will see EOF and
		// likely produce a 400, which we then capture as best-effort).
		slog.WarnContext(
			r.Context(),
			"failedintent: body read failed before capture",
			"error", err, "path", r.URL.Path,
		)
		next.ServeHTTP(w, r)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	cw := newCaptureWriter(w)
	next.ServeHTTP(cw, r)

	if !shouldPersist(r.Context(), cfg, cw) {
		// No Save runs now, so no custody is claimed now.
		if cw.status < http.StatusBadRequest && !statusFiltered(cfg) {
			cerrarPorExito(r, cfg, cw)
		}
		cw.flushDeferred("")
		return
	}
	intent := buildIntent(cfg, r, body, truncated, cw)
	aplicarResumen(r.Context(), cfg, &intent)
	cw.flushDeferred(confirmationFor(r.Context(), cfg, intent))
}

// aplicarResumen rellena Modulo y Resumen del intento antes de guardarlo.
//
// Corre DESPUÉS de que el handler terminó y sólo en la rama que ya decidió
// persistir —o sea, en un 4xx/5xx—. La ruta feliz, que es la común, no paga
// nada por esto.
//
// Un resumen que sólo trae módulo se descarta como resumen y se conserva como
// módulo: son dos hechos distintos y guardar el envoltorio vacío haría que la
// fila se viera "ya rellenada" ante el janitor, que dejaría de intentarlo.
func aplicarResumen(ctx context.Context, cfg Config, intent *Intent) {
	res := ResumenDeIntento(ctx, cfg.Resumen, cfg.Blob, *intent)
	if res == nil {
		return
	}
	intent.Modulo = res.Modulo
	if res.Vacio() {
		return
	}
	intent.Resumen = res
}

// confirmationFor persists the intent and returns the value for
// HeaderIntentCaptured — the Intent UUID when it is in custody, "" otherwise.
//
// A narrowed instance saves but never confirms. The header means "this is
// stored AND recoverable" — it is what a client is allowed to release its own
// copy on. A row born outside the auth chain carries no UsuarioID, and the
// admin screen refuses to re-dispatch an intent without one
// (failedintent/http/handlers.go:397, "intent_has_no_usuario"), so the second
// half of that promise would be false.
//
// Nothing is lost by staying quiet: the phone's decision table answers
// REINTENTA for 401 in every combination, above the rows that read the header
// at all (docs/module-standards/ENTREGA_GARANTIZADA.md:112,155), so it never
// looks here. The row is still written, which is the whole point: the office
// sees the pago that never landed.
func confirmationFor(ctx context.Context, cfg Config, intent Intent) string {
	if !saveIntent(ctx, cfg, intent) {
		return ""
	}
	if statusFiltered(cfg) {
		return ""
	}
	return intent.ID.String()
}

// handleMultipart tees the request body to BlobStorage while the downstream
// handler reads it. On 4xx/5xx the intent is persisted with BodyBlobPath +
// BodyContentType; on 2xx/3xx the on-disk blob is best-effort deleted to
// avoid leaving a useless artifact. A blob-save failure does not stop the
// request: when the response is bad we still persist the intent with
// BodyTruncated=true so the audit row exists.
func handleMultipart(cfg Config, next http.Handler, w http.ResponseWriter, r *http.Request) {
	intentID := cfg.NewID()
	contentType := r.Header.Get("Content-Type")

	pipeR, pipeW := io.Pipe()
	saveDone := make(chan multipartSaveResult, 1)
	// Decouple from r.Context() so a client disconnect mid-upload does not
	// abort the blob write — we still want the partial body persisted as
	// part of the audit row.
	saveCtx := context.WithoutCancel(r.Context())
	go func() {
		//nolint:contextcheck // saveCtx is already detached from r.Context().
		path, saveErr := cfg.Blob.Save(saveCtx, intentID, pipeR, cfg.MaxMultipartBytes)
		// Drain anything still in the pipe so the TeeReader is not blocked
		// (handler may abort mid-read on overflow / error / 4xx).
		_, _ = io.Copy(io.Discard, pipeR)
		_ = pipeR.Close()
		saveDone <- multipartSaveResult{path: path, err: saveErr}
	}()

	original := r.Body
	tee := &teeReadCloser{
		reader: io.TeeReader(original, pipeW),
		closer: original,
		writer: pipeW,
	}
	r.Body = tee

	cw := newCaptureWriter(w)
	next.ServeHTTP(cw, r)

	// Ensure the pipe writer is closed even when the handler did not drain
	// the body — without this the Save goroutine blocks on its read.
	tee.closeWriterOnce()

	saveResult := <-saveDone

	// Nothing to persist here: best-effort cleanup of the blob, because the
	// request either succeeded or its evidence belongs to somebody else, and
	// either way there is no audit row to anchor the blob to.
	if !shouldPersist(r.Context(), cfg, cw) {
		if saveResult.err == nil && saveResult.path != "" {
			//nolint:contextcheck // detached so a client disconnect does not abort cleanup.
			_ = cfg.Blob.Delete(saveCtx, saveResult.path)
		}
		if cw.status < http.StatusBadRequest && !statusFiltered(cfg) {
			cerrarPorExito(r, cfg, cw)
		}
		cw.flushDeferred("")
		return
	}

	intent := buildMultipartIntent(cfg, r, intentID, contentType, saveResult, cw)
	// El cuerpo ya está en disco: se abre UNA vez para sacarle el resumen.
	// No se parsea mientras se transmite —eso costaría en cada venta, incluidas
	// las que entran bien— y no se abre por renglón en el listado, que es el
	// N+1 que este diseño existe para evitar.
	//
	// saveCtx, no r.Context(): un teléfono que colgó a media subida no debe
	// dejar la fila sin el dato que la hace legible.
	//nolint:contextcheck // saveCtx ya está desacoplado de r.Context().
	aplicarResumen(saveCtx, cfg, &intent)
	cw.flushDeferred(confirmationFor(r.Context(), cfg, intent))
}

// handleMultipartDeferred is the multipart path of a status-filtered
// instance — the one mounted OUTSIDE the auth chain (see Config.CaptureStatuses).
//
// It does not tee the body into blob storage while the handler runs, the way
// handleMultipart does. Teeing here would write, and immediately delete, a
// full copy of every upload that goes on to succeed: on the pago path that is
// megabytes of receipt photos per payment, paid on every payment, by an
// instance whose whole point is that it almost never captures.
//
// The body is read afterwards instead, once the response says it is worth
// reading. That is sound for exactly the rejection this instance exists for —
// a 401 is answered before anyone parses the body — and the read counter is
// what proves it: if anything downstream consumed even one byte, what remains
// is no longer the request that was sent, so the row is persisted without a
// body rather than with a fragment that looks whole.
//
// The cost, and it is deliberate: captureWriter withholds every 4xx until
// flushDeferred, which runs AFTER this read. A pago rejected for an expired
// session therefore no longer gets its 401 immediately — the phone uploads the
// whole body first, and only then reads the rejection. Reading before
// answering is the safe order (answering first would race the client into
// closing the connection with the evidence half-written), but it does move the
// cost of a stale session from "one rejected handshake" to "one full upload".
func handleMultipartDeferred(cfg Config, next http.Handler, w http.ResponseWriter, r *http.Request) {
	intentID := cfg.NewID()
	contentType := r.Header.Get("Content-Type")

	r, body := deferBody(r)

	cw := newCaptureWriter(w)
	next.ServeHTTP(cw, r)

	if !shouldPersist(r.Context(), cfg, cw) {
		cw.flushDeferred("")
		return
	}

	// Detached from r.Context(), like handleMultipart: a phone that hung up
	// must still leave its evidence.
	saveCtx := context.WithoutCancel(r.Context())
	//nolint:contextcheck // saveCtx is already detached from r.Context().
	saveResult := saveUnreadBody(saveCtx, cfg, intentID, body)

	intent := buildMultipartIntent(cfg, r, intentID, contentType, saveResult, cw)
	//nolint:contextcheck // saveCtx is already detached from r.Context().
	aplicarResumen(saveCtx, cfg, &intent)
	cw.flushDeferred(confirmationFor(r.Context(), cfg, intent))
}

// deferBody prepares a request for a narrowed instance: it plants the custody
// holder other captures report into, and wraps the body in a counter so the
// caller can tell whether anything downstream consumed it.
//
// Returns the derived request, which the caller MUST pass on: the holder lives
// on its context.
func deferBody(r *http.Request) (*http.Request, *countingReadCloser) {
	r = r.WithContext(withCustodyMark(r.Context()))
	source := r.Body
	if source == nil {
		source = http.NoBody
	}
	body := &countingReadCloser{ReadCloser: source}
	r.Body = body
	return r, body
}

// handleJSONDeferred is the JSON path of a narrowed instance. Like
// handleMultipartDeferred it leaves the body alone until the response says it
// is worth reading, and for a second reason beyond cost:
//
// readCappedBody TRUNCATES at BodyCapBytes and hands the truncated bytes to
// whoever comes next. If the outer instance did that on the way in, the
// in-chain instance would receive an already-cut body, measure it as fitting,
// and record BodyTruncated=false on a row whose body is missing its tail —
// the screen would claim the evidence is whole. Not reading it on the way in
// keeps that instance's flag honest.
func handleJSONDeferred(cfg Config, next http.Handler, w http.ResponseWriter, r *http.Request) {
	r, body := deferBody(r)

	cw := newCaptureWriter(w)
	next.ServeHTTP(cw, r)

	if !shouldPersist(r.Context(), cfg, cw) {
		cw.flushDeferred("")
		return
	}

	buf, truncated := readUnreadJSONBody(cfg, r, body)
	intent := buildIntent(cfg, r, buf, truncated, cw)
	aplicarResumen(r.Context(), cfg, &intent)
	cw.flushDeferred(confirmationFor(r.Context(), cfg, intent))
}

// readUnreadJSONBody returns the request body, and whether what it returns is
// less than what was sent. A body something downstream already read cannot be
// reconstructed, so it comes back empty and flagged rather than as a leftover
// posing as the whole thing.
func readUnreadJSONBody(cfg Config, r *http.Request, body *countingReadCloser) ([]byte, bool) {
	if body.bytesRead() > 0 {
		return nil, true
	}
	buf, truncated, err := readCappedBody(r, cfg.BodyCapBytes)
	if err != nil {
		slog.WarnContext(
			r.Context(),
			"failedintent: body read failed after the response",
			"error", err, "path", r.URL.Path,
		)
		return nil, true
	}
	return buf, truncated
}

// errBodyAlreadyConsumed is the save outcome when the downstream handler read
// part of the request body before the response turned out to be capturable.
// It travels as multipartSaveResult.err so the intent is persisted with
// BodyTruncated=true and no blob path — the audit row still exists.
var errBodyAlreadyConsumed = errors.New(
	"failedintent: request body was already consumed downstream",
)

// errBodyUnavailable is the save outcome when the body yielded no bytes at
// all: closed downstream, or never sent because the client hung up.
var errBodyUnavailable = errors.New(
	"failedintent: request body yielded no bytes",
)

// saveUnreadBody streams the still-unread request body into blob storage.
func saveUnreadBody(
	ctx context.Context, cfg Config, intentID uuid.UUID, body *countingReadCloser,
) multipartSaveResult {
	if body.bytesRead() > 0 {
		return multipartSaveResult{err: errBodyAlreadyConsumed}
	}
	path, err := cfg.Blob.Save(ctx, intentID, body, cfg.MaxMultipartBytes)
	if err != nil {
		return multipartSaveResult{err: err}
	}
	if body.bytesRead() == 0 {
		// Nothing came back. An empty blob that presents itself as the
		// request would read on the screen as "the phone sent nothing",
		// which is a different — and unfounded — accusation.
		_ = cfg.Blob.Delete(ctx, path)
		return multipartSaveResult{err: errBodyUnavailable}
	}
	return multipartSaveResult{path: path}
}

// countingReadCloser counts the bytes the downstream handler read from the
// request body. It buffers nothing and starts no goroutine: on the happy path
// it costs one atomic add per Read. The counter is atomic because the handler
// may read the body from a goroutine of its own.
type countingReadCloser struct {
	io.ReadCloser
	n atomic.Int64
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	if n > 0 {
		c.n.Add(int64(n))
	}
	return n, err
}

func (c *countingReadCloser) bytesRead() int64 {
	return c.n.Load()
}

// isIdempotentReplay reports whether the captured response was emitted by
// the idempotency middleware replaying a cached entry. Capture must skip
// such responses because the original 4xx/5xx call already produced an
// Intent.
func isIdempotentReplay(cw *captureWriter) bool {
	return cw.Header().Get(HeaderIdempotentReplay) == "true"
}

// multipartSaveResult is the outcome of the blob-save goroutine.
type multipartSaveResult struct {
	path string
	err  error
}

// teeReadCloser wraps a TeeReader so its Close also closes the underlying
// body AND the pipe writer feeding the Save goroutine. Closing the pipe
// writer unblocks the goroutine when the handler stops reading early.
type teeReadCloser struct {
	reader      io.Reader
	closer      io.Closer
	writer      *io.PipeWriter
	writerClose sync.Once
}

func (t *teeReadCloser) Read(p []byte) (int, error) {
	n, err := t.reader.Read(p)
	if errors.Is(err, io.EOF) {
		// EOF on the source — close the pipe writer so Save returns.
		t.closeWriterOnce()
	}
	return n, err
}

// Close closes the underlying body and unblocks the Save goroutine by
// closing the pipe writer if it has not been closed yet.
func (t *teeReadCloser) Close() error {
	t.closeWriterOnce()
	if t.closer != nil {
		return t.closer.Close()
	}
	return nil
}

func (t *teeReadCloser) closeWriterOnce() {
	t.writerClose.Do(func() {
		_ = t.writer.Close()
	})
}

// buildMultipartIntent assembles the Intent for a multipart capture. When the
// blob save failed (overflow / I/O error) the audit row is still produced
// with BodyTruncated=true and an empty BodyBlobPath — the promise "evidence
// remains" matters more than the body.
func buildMultipartIntent(
	cfg Config,
	r *http.Request,
	intentID uuid.UUID,
	contentType string,
	save multipartSaveResult,
	cw *captureWriter,
) Intent {
	now := cfg.Clock()
	intent := Intent{
		ID:              intentID,
		ReceivedAt:      now,
		Method:          r.Method,
		Path:            r.URL.Path,
		IdempotencyKey:  r.Header.Get(idempotency.HeaderKey),
		RequestID:       requestIDOrNew(r.Context()),
		Body:            json.RawMessage(`null`),
		BodyTruncated:   save.err != nil,
		BodyBlobPath:    save.path,
		BodyContentType: contentType,
		HTTPStatus:      cw.status,
		RetryCount:      0,
		Status:          StatusNew,
	}
	if save.err != nil {
		// Don't leave a dangling path on the row when Save failed.
		intent.BodyBlobPath = ""
		slog.WarnContext(
			r.Context(),
			"failedintent: multipart blob save failed; persisting intent without body",
			"error", save.err, "intent_id", intentID.String(),
		)
	}
	if cu, ok := auth.CurrentUserFromContext(r.Context()); ok {
		intent.FirebaseUID = cu.FirebaseUID
		id := cu.ID
		intent.UsuarioID = &id
	}
	intent.ErrorCode, intent.ErrorMessage = parseProblemJSON(cw.body.Bytes())
	return intent
}

// saveIntent persists the captured intent and emits the structured log. A
// Store.Save failure is logged but never propagated — failing the request
// because the capture pipeline broke would be worse than losing one piece of
// evidence.
//
// It reports whether the intent is now in custody. Only a true return may be
// confirmed to the client with HeaderIntentCaptured: the phone releases a
// capture on that promise, so claiming custody we do not have is precisely the
// failure that loses payments.
func saveIntent(parentCtx context.Context, cfg Config, intent Intent) bool {
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(parentCtx), 5*time.Second)
	defer cancel()
	outcome, saveErr := cfg.Store.Save(saveCtx, intent)
	if saveErr != nil {
		slog.ErrorContext(
			parentCtx,
			"failedintent: store save failed",
			"error", saveErr,
			"intent_id", intent.ID,
			"path", intent.Path,
			"http_status", intent.HTTPStatus,
		)
		return false
	}
	// El cuerpo que la dedup descartó ya está escrito en disco y ninguna fila
	// lo referencia. Se borra aquí porque el Store no conoce el BlobStorage —
	// ni debe: su trabajo es la fila. Best-effort: el barrido de huérfanos del
	// arranque es la red de abajo, pero esperar a un reinicio para recuperar
	// disco es exactamente el problema que esta parte arregla.
	if outcome.OrphanedBlobPath != "" && cfg.Blob != nil {
		if delErr := cfg.Blob.Delete(saveCtx, outcome.OrphanedBlobPath); delErr != nil {
			slog.WarnContext(
				parentCtx,
				"failedintent: no se pudo borrar el blob descartado por la dedup",
				"error", delErr, "path", outcome.OrphanedBlobPath,
			)
		}
	}
	markCustody(parentCtx)
	emitCapturedLog(parentCtx, intent)
	return true
}

// cerrarPorExito cierra el intento pendiente que esta misma petición acaba de
// dejar obsoleto.
//
// El caso: el teléfono reintenta una venta que falló, esta vez entra, y la
// fila de MSP_FAILED_INTENTS se queda ahí como pendiente para siempre. Quien
// abre la consola ve trabajo que ya no existe, y el rezago de esas filas es
// lo que hace que la pantalla deje de mirarse.
//
// La clave de idempotencia es lo que las liga: **el mismo trabajo acabó bien**.
// La plataforma no aprende que existen las ventas —no sabe qué se creó, ni
// mira ninguna tabla de negocio—; sólo sabe que una clave que había fallado
// terminó en 2xx.
//
// Best-effort de principio a fin: si esto falla, el intento se queda
// pendiente y el reconciliador del janitor lo alcanza después. Nunca puede
// tumbar una petición que YA salió bien.
func cerrarPorExito(r *http.Request, cfg Config, cw *captureWriter) {
	if isIdempotentReplay(cw) {
		// La respuesta la sirvió el caché de idempotencia: el 2xx es de la
		// llamada original, no de ésta. Aquella ya cerró lo que tocaba.
		return
	}
	key := strings.TrimSpace(r.Header.Get(idempotency.HeaderKey))
	if key == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()

	n, err := cfg.Store.MarkResolvedByKeys(ctx, r.URL.Path, []string{key}, cfg.Clock())
	if err != nil {
		slog.WarnContext(
			r.Context(),
			"failedintent: no se pudo cerrar el intento tras el éxito",
			"error", err, "path", r.URL.Path,
		)
		return
	}
	if n > 0 {
		slog.InfoContext(
			r.Context(),
			"failedintent: intento cerrado porque el mismo trabajo entró",
			"path", r.URL.Path, "rows", n,
		)
	}
}

// isMultipart reports whether the request's Content-Type is multipart/form-data.
func isMultipart(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data")
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
	// Multipart uploads opt-out unless a Blob storage is wired. Without
	// one, capturing would either truncate the body over BodyCapBytes or
	// dump base64 noise into the inline column.
	if isMultipart(r) && cfg.Blob == nil {
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
//
// On a 4xx/5xx it also WITHHOLDS the response: status and body are buffered
// instead of forwarded, because HeaderIntentCaptured can only be added while
// the header block is still open, and whether to add it is not known until
// Store.Save has run — which happens after the handler returns. flushDeferred
// releases the withheld response. 2xx/3xx are forwarded as they are written,
// so the happy path keeps streaming and buffers nothing.
type captureWriter struct {
	http.ResponseWriter
	status   int
	body     bytes.Buffer
	bodyCap  int
	bodyFull bool

	// headerSeen records that the handler called WriteHeader, so a second
	// call is ignored the way net/http ignores a superfluous one.
	headerSeen bool
	// deferring reports that status+pending are being withheld.
	deferring bool
	// pending holds the withheld response body verbatim (unlike body, which
	// is a bounded snippet used only for diagnostics).
	pending bytes.Buffer
}

func newCaptureWriter(w http.ResponseWriter) *captureWriter {
	return &captureWriter{ResponseWriter: w, status: http.StatusOK, bodyCap: DefaultResponseBodyCapBytes}
}

// WriteHeader records the status code. A 4xx/5xx is withheld until
// flushDeferred; anything else is forwarded immediately.
func (c *captureWriter) WriteHeader(code int) {
	if c.headerSeen {
		return
	}
	c.headerSeen = true
	c.status = code
	if code >= http.StatusBadRequest {
		c.deferring = true
		return
	}
	c.ResponseWriter.WriteHeader(code)
}

// Write buffers up to bodyCap bytes for later inspection, then either withholds
// the bytes (deferring) or forwards them.
func (c *captureWriter) Write(b []byte) (int, error) {
	if !c.headerSeen {
		// Implicit 200 the way net/http does it — never a deferred status.
		c.WriteHeader(http.StatusOK)
	}
	c.bufferForCapture(b)

	if !c.deferring {
		return c.ResponseWriter.Write(b)
	}
	if c.pending.Len()+len(b) > deferredBodyCapBytes {
		// Too large to hold: release what we have without a confirmation
		// rather than buffer an unbounded response.
		c.flushDeferred("")
		return c.ResponseWriter.Write(b)
	}
	return c.pending.Write(b)
}

// flushDeferred releases a withheld response, optionally stamping it with the
// custody confirmation. intentID is the saved Intent's UUID, or "" when nothing
// is in custody. Calling it when nothing is withheld is a no-op, so both the
// JSON and multipart paths can call it unconditionally.
func (c *captureWriter) flushDeferred(intentID string) {
	if !c.deferring {
		return
	}
	c.deferring = false
	if intentID != "" {
		c.Header().Set(HeaderIntentCaptured, intentID)
	}
	c.ResponseWriter.WriteHeader(c.status)
	if c.pending.Len() > 0 {
		_, _ = c.ResponseWriter.Write(c.pending.Bytes())
		c.pending.Reset()
	}
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

// humaCodePrefix is the marker the Huma-served modules put in front of the
// apperror code inside errors[].message.
//
// Two writers produce our error bodies and they do NOT agree on where the
// machine-readable code goes:
//
//   - response.Error (platform/response) — used by the middlewares and the
//     chi-served auth module — writes a top-level "code" member.
//   - mapAppError, duplicated in every Huma module (ventas, cobranza,
//     clientes, inventario, analytics, rutas, config, visitas, reactivacion,
//     microsip), calls huma.NewError(status, ae.Message, &huma.ErrorDetail{
//     Message: "code=" + ae.Code}). huma.ErrorModel has no "code" member at
//     all, so the code only survives inside errors[].message.
//
// Reading just the top-level member left ERROR_CODE empty for every Huma
// module, which is worse than wrong: a filter by code returns an empty set,
// and an empty set reads like "nothing to see here".
const humaCodePrefix = "code="

// errorCodeMaxLen is the width of MSP_FAILED_INTENTS.ERROR_CODE
// (VARCHAR(80) CHARACTER SET ASCII, migration 000031). A longer value would
// not "just get stored badly" — Firebird rejects the INSERT and the whole
// capture is lost, which is the one thing this package must never do. The
// longest code authored in the repo today is 37 chars, so the cap only ever
// bites on a value that is not really a code.
const errorCodeMaxLen = 80

// problemErrorDetail is the subset of huma.ErrorDetail we read back. Huma
// also emits `location` and `value`; we ignore them. It is deliberately
// tolerant of platform/response's FieldError shape too (field/code/message):
// the extra members are simply dropped by encoding/json.
type problemErrorDetail struct {
	Message string `json:"message"`
}

// problemBodyShape is the subset of RFC 9457 Problem fields we need to read
// back to populate Intent.ErrorCode / ErrorMessage. Other fields are ignored.
type problemBodyShape struct {
	Code   string               `json:"code"`
	Detail string               `json:"detail"`
	Title  string               `json:"title"`
	Errors []problemErrorDetail `json:"errors"`
}

// parseProblemJSON tolerantly extracts the error code + user-facing message
// from a captured response body. Non-Problem-shaped bodies yield ("", "").
//
// The top-level "code" wins when both shapes are present: it is the explicit
// one, and the errors[] convention is a workaround for a model that has no
// place to put it.
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
	code := p.Code
	if code == "" {
		code = codeFromHumaDetails(p.Errors)
	}
	return capErrorCode(code), msg
}

// codeFromHumaDetails scans errors[] for the "code=<something>" convention.
//
// errors[] is not ours to control: Huma's own request validation fills it
// with one entry per offending field ({"message":"expected array length >= 1",
// "location":"body.productos"}). Those carry no prefix and must not be
// mistaken for a code, hence the exact-prefix requirement rather than a
// substring search.
//
// When several entries carry the prefix the FIRST one wins. mapAppError only
// ever attaches a single detail, so more than one can only come from a writer
// that does not exist yet; taking the first keeps the result deterministic
// instead of depending on map/slice ordering elsewhere.
//
// The candidate must also look like a code — printable ASCII, no spaces.
// The 500 branch of every mapAppError passes a raw err.Error() as the detail
// message; if some wrapped error text ever began with "code=" we would
// otherwise store a sentence, and ERROR_CODE is an ASCII column that a
// non-ASCII value can make the INSERT reject outright.
func codeFromHumaDetails(details []problemErrorDetail) string {
	for _, d := range details {
		candidate, found := strings.CutPrefix(d.Message, humaCodePrefix)
		if !found || candidate == "" {
			continue
		}
		if !isCodeToken(candidate) {
			continue
		}
		return candidate
	}
	return ""
}

// isCodeToken reports whether s is plausible as a machine-readable code:
// printable ASCII with no spaces. Every apperror code in the repo (114 of
// them, measured) is [a-z0-9_]+, so this is deliberately looser than the
// convention — it rejects prose and non-ASCII, not future naming choices.
func isCodeToken(s string) bool {
	for _, r := range s {
		if r <= ' ' || r > '~' {
			return false
		}
	}
	return true
}

// capErrorCode clips the code to the column width. Truncating beats
// discarding: a clipped code still groups and filters, while an empty one is
// indistinguishable from "no error code", which is the failure this whole
// function exists to avoid.
func capErrorCode(code string) string {
	if len(code) > errorCodeMaxLen {
		return code[:errorCodeMaxLen]
	}
	return code
}

// emitCapturedLog records the capture as a structured event. Never logs the
// body — only metadata that's safe for support staff to see.
func emitCapturedLog(ctx context.Context, i Intent) {
	slog.InfoContext(
		ctx, "failedintent.captured",
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
