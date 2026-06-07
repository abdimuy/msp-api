package failedintenthttp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth"
	apperror "github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	"github.com/abdimuy/msp-api/internal/platform/httpdispatch"
	"github.com/abdimuy/msp-api/internal/platform/idempotency"
	"github.com/abdimuy/msp-api/internal/platform/response"
)

// replayBodyPreviewBytes is the maximum number of bytes kept as the replay
// response preview — enough for a typical error JSON but cheap to store.
const replayBodyPreviewBytes = 1024

// UsuarioLookup is a narrow port the handlers use to resolve the original
// requester at replay time. Implementations adapt over auth/ports/outbound.UsuarioRepo.
type UsuarioLookup interface {
	// BuildCurrentUserByID returns the auth.CurrentUser the original request
	// had planted, including its flattened permission codes. Returns
	// apperror.NewNotFound("usuario_not_found", ...) when the user does not
	// exist or apperror.NewForbidden("user_inactive", ...) when the user is
	// inactive.
	BuildCurrentUserByID(ctx context.Context, id uuid.UUID) (auth.CurrentUser, error)
}

// Service bundles dependencies for the four admin handlers.
type Service struct {
	store          failedintent.Store
	dispatcher     failedintent.ReplayDispatcher
	usuarios       UsuarioLookup
	blobs          failedintent.BlobStorage
	partsInspector *failedintent.BlobPartsInspector
	clock          func() time.Time
	newID          func() uuid.UUID
}

// NewService constructs a Service. Nil clock and newID are replaced with
// time.Now and uuid.New respectively, matching the core package convention.
// blobs is optional: pass nil when the deployment does not opt into
// multipart capture; in that case Replay falls back to the inline body and
// any intent that does carry a BodyBlobPath surfaces a clear error.
func NewService(
	store failedintent.Store,
	dispatcher failedintent.ReplayDispatcher,
	usuarios UsuarioLookup,
	blobs failedintent.BlobStorage,
	clock func() time.Time,
	newID func() uuid.UUID,
) *Service {
	if clock == nil {
		clock = time.Now
	}
	if newID == nil {
		newID = uuid.New
	}
	var inspector *failedintent.BlobPartsInspector
	if blobs != nil {
		inspector = failedintent.NewBlobPartsInspector(blobs)
	}
	return &Service{
		store:          store,
		dispatcher:     dispatcher,
		usuarios:       usuarios,
		blobs:          blobs,
		partsInspector: inspector,
		clock:          clock,
		newID:          newID,
	}
}

// parseListQuery parses the cursor, page_size, and status query parameters
// shared by Listar and MeListar. The returned ListParams has UsuarioID unset;
// callers that need per-user scoping must set it themselves.
func parseListQuery(r *http.Request) (failedintent.ListParams, error) {
	q := r.URL.Query()

	cursorStr := q.Get("cursor")
	cursorAt, cursorID, err := decodeCursor(cursorStr)
	if err != nil {
		return failedintent.ListParams{}, apperror.NewValidation("invalid_cursor", "el cursor es inválido")
	}

	pageSize := parsePageSize(q.Get("page_size"))

	var statusFilter failedintent.Status
	if raw := q.Get("status"); raw != "" {
		statusFilter = failedintent.Status(raw)
		if !statusFilter.Valid() {
			return failedintent.ListParams{}, apperror.NewValidation("invalid_status", "el estado proporcionado es inválido")
		}
	}

	return failedintent.ListParams{
		CursorReceivedAt: cursorAt,
		CursorID:         cursorID,
		Status:           statusFilter,
		PageSize:         pageSize,
	}, nil
}

// buildListResponse maps a Page[Intent] into the JSON envelope.
func buildListResponse(page failedintent.Page[failedintent.Intent]) ListResponse {
	items := make([]IntentDTO, 0, len(page.Items))
	for _, i := range page.Items {
		items = append(items, intentToDTO(i))
	}

	var nextCursor string
	if page.HasMore {
		nextCursor = encodeCursor(page.NextReceivedAt, page.NextID)
	}

	return ListResponse{
		Items:      items,
		NextCursor: nextCursor,
		HasMore:    page.HasMore,
	}
}

// Listar handles GET / — cursor-paginated list of intents.
func (s *Service) Listar(w http.ResponseWriter, r *http.Request) {
	params, err := parseListQuery(r)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	page, err := s.store.List(r.Context(), params)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.JSON(w, r, http.StatusOK, buildListResponse(page))
}

// MeListar handles GET /me/failed-intents — lists only the intents owned by
// the authenticated user. No failed_intents:* permission is required; results
// are automatically scoped to the calling user's ID.
func (s *Service) MeListar(w http.ResponseWriter, r *http.Request) {
	cu, ok := auth.CurrentUserFromContext(r.Context())
	if !ok {
		response.Error(w, r, apperror.NewUnauthorized("unauthenticated", "no autenticado"))
		return
	}

	params, err := parseListQuery(r)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	params.UsuarioID = &cu.ID

	page, err := s.store.List(r.Context(), params)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.JSON(w, r, http.StatusOK, buildListResponse(page))
}

// Obtener handles GET /{id}.
func (s *Service) Obtener(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntentID(r)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	intent, err := s.store.Get(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	if intent == nil {
		response.Error(w, r, apperror.NewNotFound("failed_intent_not_found", "intento fallido no encontrado"))
		return
	}
	response.JSON(w, r, http.StatusOK, intentToDTO(*intent))
}

// Resolver handles PATCH /{id}/resolve — marks an intent ignored or resolved_manual.
func (s *Service) Resolver(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntentID(r)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	var req ResolveRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if decErr := dec.Decode(&req); decErr != nil {
		response.Error(w, r, apperror.NewValidation(
			"invalid_request_body",
			"no se pudo decodificar el cuerpo de la solicitud",
		))
		return
	}

	target := failedintent.Status(req.Status)
	if target != failedintent.StatusIgnored && target != failedintent.StatusResolvedManual {
		response.Error(w, r, apperror.NewValidation(
			"invalid_resolve_status",
			"el estado de resolución debe ser ignored o resolved_manual",
		))
		return
	}

	if utf8.RuneCountInString(req.Notes) > 500 {
		response.Error(w, r, apperror.NewValidation(
			"notes_too_long",
			"las notas no pueden exceder 500 caracteres",
		))
		return
	}

	cu, ok := auth.CurrentUserFromContext(r.Context())
	if !ok {
		response.Error(w, r, apperror.NewUnauthorized("unauthenticated", "no autenticado"))
		return
	}

	if err := s.store.UpdateStatus(
		r.Context(), id,
		failedintent.StatusNew, target,
		cu.ID, req.Notes, s.clock(),
	); err != nil {
		response.Error(w, r, err)
		return
	}

	intent, err := s.store.Get(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	if intent == nil {
		response.Error(w, r, apperror.NewNotFound("failed_intent_not_found", "intento fallido no encontrado"))
		return
	}

	slog.InfoContext(
		r.Context(), "failedintent.resolved",
		"intent_id", id.String(),
		"status", string(target),
		"resolver_id", cu.ID.String(),
	)

	response.JSON(w, r, http.StatusOK, intentToDTO(*intent))
}

// replayResult holds the observable outcome of a dispatched replay.
type replayResult struct {
	outcome    failedintent.Status
	httpStatus int
	bodyBytes  []byte
}

// executeReplay performs the core replay steps shared by Replay and ReplayWith:
// increment retry count, build and dispatch the request with the given body,
// and attempt to transition the intent's status.
func (s *Service) executeReplay(
	ctx context.Context,
	intent failedintent.Intent,
	body json.RawMessage,
	cu auth.CurrentUser,
) replayResult {
	id := intent.ID

	// Best-effort retry count increment — a failure here must not abort the
	// replay; we log a warning and continue.
	if incErr := s.store.IncrementRetry(ctx, id); incErr != nil {
		slog.WarnContext(
			ctx, "failedintent: increment retry failed",
			"error", incErr, "intent_id", id.String(),
		)
	}

	// Capture the status BEFORE replay to decide whether to transition it.
	originalStatus := intent.Status

	replayReq, buildErr := s.buildReplayRequest(ctx, &intent, cu, body)
	if buildErr != nil {
		// A build failure is non-recoverable; return a synthetic fail outcome.
		slog.ErrorContext(
			ctx, "failedintent: replay request build failed",
			"error", buildErr, "intent_id", id.String(),
		)
		return replayResult{outcome: failedintent.StatusRetriedFail, httpStatus: http.StatusInternalServerError}
	}

	rw := newReplayWriter()
	s.dispatcher.Dispatch(rw, replayReq)

	outcome := outcomeFor(rw.status)
	s.tryUpdateStatus(ctx, id, originalStatus, outcome, cu.ID)

	return replayResult{
		outcome:    outcome,
		httpStatus: rw.status,
		bodyBytes:  rw.body.Bytes(),
	}
}

// Replay handles POST /{id}/replay — re-dispatches the original request
// through the router and records the outcome.
func (s *Service) Replay(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntentID(r)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	intent, err := s.store.Get(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	if intent == nil {
		response.Error(w, r, apperror.NewNotFound("failed_intent_not_found", "intento fallido no encontrado"))
		return
	}

	if intent.UsuarioID == nil {
		response.Error(w, r, apperror.NewValidation(
			"intent_has_no_usuario",
			"no se puede reproducir un intento sin usuario asociado",
		))
		return
	}

	cu, cuErr := s.usuarios.BuildCurrentUserByID(r.Context(), *intent.UsuarioID)
	if cuErr != nil {
		response.Error(w, r, cuErr)
		return
	}

	// Pass nil so resolveReplayBody auto-selects between blob and inline.
	result := s.executeReplay(r.Context(), *intent, nil, cu)

	response.JSON(w, r, http.StatusOK, ReplayResponse{
		Outcome:           string(result.outcome),
		ReplayHTTPStatus:  result.httpStatus,
		ReplayBodyPreview: bodyPreview(result.bodyBytes),
	})
}

// ReplayWith handles POST /{id}/replay-with — re-dispatches the intent with a
// caller-supplied corrected body instead of the originally captured one.
func (s *Service) ReplayWith(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntentID(r)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	var req ReplayWithRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if decErr := dec.Decode(&req); decErr != nil {
		response.Error(w, r, apperror.NewValidation(
			"invalid_request_body",
			"no se pudo decodificar el cuerpo de la solicitud",
		))
		return
	}

	// Reject absent body, JSON null, or syntactically invalid JSON.
	if len(req.Body) == 0 || string(req.Body) == "null" || !json.Valid(req.Body) {
		response.Error(w, r, apperror.NewValidation(
			"invalid_replay_body",
			"el cuerpo de reproducción es obligatorio",
		))
		return
	}

	intent, err := s.store.Get(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	if intent == nil {
		response.Error(w, r, apperror.NewNotFound("failed_intent_not_found", "intento fallido no encontrado"))
		return
	}

	if intent.BodyBlobPath != "" {
		// A multipart-captured body cannot meaningfully be overridden by a
		// JSON payload — the boundary and binary parts wouldn't line up.
		response.Error(w, r, apperror.NewValidation(
			"blob_intent_replay_with_unsupported",
			"este intento se capturó con multipart; usar /replay en lugar de /replay-with",
		))
		return
	}

	if intent.UsuarioID == nil {
		response.Error(w, r, apperror.NewValidation(
			"intent_has_no_usuario",
			"no se puede reproducir un intento sin usuario asociado",
		))
		return
	}

	cu, cuErr := s.usuarios.BuildCurrentUserByID(r.Context(), *intent.UsuarioID)
	if cuErr != nil {
		response.Error(w, r, cuErr)
		return
	}

	result := s.executeReplay(r.Context(), *intent, req.Body, cu)

	response.JSON(w, r, http.StatusOK, ReplayResponse{
		Outcome:           string(result.outcome),
		ReplayHTTPStatus:  result.httpStatus,
		ReplayBodyPreview: bodyPreview(result.bodyBytes),
	})
}

// buildReplayRequest assembles the http.Request that will be dispatched as a
// replay. The body source depends on the intent shape: a non-empty
// overrideBody (ReplayWith) wins; otherwise a blob path is streamed back
// from disk with the original Content-Type; otherwise the inline JSON body
// is used. Extracted to keep Replay/ReplayWith within the cyclomatic budget.
func (s *Service) buildReplayRequest(
	ctx context.Context,
	intent *failedintent.Intent,
	cu auth.CurrentUser,
	overrideBody json.RawMessage,
) (*http.Request, error) {
	body, contentType, openErr := s.resolveReplayBody(ctx, intent, overrideBody)
	if openErr != nil {
		return nil, openErr
	}

	//nolint:gosec // G704: method/path are captured from this server's own
	// request stream, not from untrusted external input. The intent row's
	// method and path were vetted at capture time by the existing chi router.
	req, err := http.NewRequestWithContext(ctx, intent.Method, intent.Path, body)
	if err != nil {
		if closer, ok := body.(io.Closer); ok {
			_ = closer.Close()
		}
		return nil, err
	}

	req.Header.Set("Content-Type", contentType)
	req.Header.Set(failedintent.HeaderInternalReplay, intent.ID.String())
	req.Header.Set("X-Request-ID", s.newID().String())

	// Always mint a fresh Idempotency-Key for the replay. Reusing the
	// captured key would let the idempotency middleware either short-circuit
	// with the cached failure response (defeating the purpose of replay) or
	// reject a corrected body as idempotency_key_mismatch. A replay is
	// semantically a new operation distinct from the user's original retries.
	req.Header.Set(idempotency.HeaderKey, s.newID().String())

	// httpdispatch.InternalContext strips the chi.RouteContext inherited
	// from the admin handler's request; without it, chi.Mux.ServeHTTP
	// short-circuits routing on the synthesized request and returns 404.
	replayCtx := httpdispatch.InternalContext(req.Context())

	//nolint:contextcheck // intentional: we plant the original requester's
	// CurrentUser on the replay request so the downstream chain sees the
	// same auth context the original request had.
	req = req.WithContext(auth.PlantCurrentUser(replayCtx, cu))
	return req, nil
}

// resolveReplayBody picks the body source for a replay. Returns the body
// (caller owns Close() if it implements io.Closer), the Content-Type to set
// on the replay request, and any error.
func (s *Service) resolveReplayBody(
	ctx context.Context,
	intent *failedintent.Intent,
	overrideBody json.RawMessage,
) (io.Reader, string, error) {
	if len(overrideBody) > 0 {
		return bytes.NewReader(overrideBody), "application/json", nil
	}
	if intent.BodyBlobPath != "" {
		if s.blobs == nil {
			return nil, "", apperror.NewInternal(
				"failed_intent_blob_unavailable",
				"el almacenamiento de blobs no está configurado",
			)
		}
		rc, err := s.blobs.Open(ctx, intent.BodyBlobPath)
		if err != nil {
			return nil, "", apperror.NewInternal(
				"failed_intent_blob_open_failed",
				"no se pudo abrir el blob del intento fallido",
			).WithError(err).WithField("path", intent.BodyBlobPath)
		}
		ct := intent.BodyContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		return rc, ct, nil
	}
	return bytes.NewReader(intent.Body), "application/json", nil
}

// tryUpdateStatus transitions the intent after a replay. Conflict errors and
// terminal-status skips are logged at warn level and discarded — the replay
// outcome is still returned to the caller.
func (s *Service) tryUpdateStatus(
	ctx context.Context,
	id uuid.UUID,
	originalStatus failedintent.Status,
	outcome failedintent.Status,
	resolvedBy uuid.UUID,
) {
	if originalStatus != failedintent.StatusNew {
		// Re-replay of a terminal intent — do not mutate status.
		return
	}
	err := s.store.UpdateStatus(ctx, id, failedintent.StatusNew, outcome, resolvedBy, "", s.clock())
	if err != nil {
		ae, isApp := apperror.As(err)
		if isApp && ae.Code == "failed_intent_status_conflict" {
			slog.WarnContext(
				ctx, "failedintent: status conflict after replay — another actor changed status",
				"intent_id", id.String(),
				"attempted_outcome", string(outcome),
			)
			return
		}
		slog.WarnContext(
			ctx, "failedintent: update status after replay failed",
			"error", err,
			"intent_id", id.String(),
			"attempted_outcome", string(outcome),
		)
	}
}

// parseIntentID extracts and validates the {id} chi path parameter.
func parseIntentID(r *http.Request) (uuid.UUID, error) {
	raw := chi.URLParam(r, "id")
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.UUID{}, apperror.NewValidation(
			"invalid_intent_id",
			"el id del intento fallido es inválido",
		)
	}
	return id, nil
}

// parsePageSize parses the page_size query parameter clamped to [1, 100].
// It falls back to 20 on missing or invalid input.
func parsePageSize(raw string) int {
	if raw == "" {
		return 20
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 20
	}
	if n > 100 {
		return 100
	}
	return n
}

// outcomeFor maps an HTTP status code from a replay into a Status value.
func outcomeFor(status int) failedintent.Status {
	if status < http.StatusBadRequest {
		return failedintent.StatusRetriedOK
	}
	return failedintent.StatusRetriedFail
}

// bodyPreview trims the captured replay body to replayBodyPreviewBytes and
// returns it as a UTF-8 string (control bytes are preserved as-is).
func bodyPreview(b []byte) string {
	if len(b) <= replayBodyPreviewBytes {
		return string(b)
	}
	return string(b[:replayBodyPreviewBytes])
}

// replayWriter is a minimal ResponseWriter that records status + a bounded
// body buffer so the replay outcome can be observed without forwarding to any
// real client socket.
type replayWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newReplayWriter() *replayWriter {
	return &replayWriter{header: make(http.Header), status: http.StatusOK}
}

// Header returns the header map for the replay response.
func (rw *replayWriter) Header() http.Header { return rw.header }

// WriteHeader records the status code. Only the first call takes effect.
func (rw *replayWriter) WriteHeader(code int) {
	if rw.status == http.StatusOK {
		rw.status = code
	}
}

// Write captures up to replayBodyPreviewBytes of response body.
func (rw *replayWriter) Write(b []byte) (int, error) {
	if rw.body.Len() < replayBodyPreviewBytes {
		remaining := replayBodyPreviewBytes - rw.body.Len()
		if len(b) > remaining {
			b = b[:remaining]
		}
		_, _ = rw.body.Write(b)
	}
	return len(b), nil
}
