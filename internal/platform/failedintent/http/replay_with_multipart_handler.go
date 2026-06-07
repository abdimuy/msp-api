package failedintenthttp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth"
	apperror "github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	"github.com/abdimuy/msp-api/internal/platform/httpdispatch"
	"github.com/abdimuy/msp-api/internal/platform/idempotency"
	"github.com/abdimuy/msp-api/internal/platform/response"
)

// MaxReplayWithMultipartBytes caps the incoming admin form so a runaway
// upload can't spike API memory. Mirrors the capture-time multipart limit
// (DefaultMaxMultipartBytes) by default.
const MaxReplayWithMultipartBytes int64 = 100 * 1024 * 1024 // 100 MiB

const manifestFieldName = "__manifest"

// manifestDTO is the wire shape of the __manifest form field. It mirrors
// the failedintent.ManifestPart/Source structs but uses string Kinds and
// base64-encoded values so the wire format is JSON-friendly.
type manifestDTO struct {
	Parts []manifestPartDTO `json:"parts"`
}

type manifestPartDTO struct {
	Name        string            `json:"name"`
	ContentType string            `json:"content_type,omitempty"`
	Filename    string            `json:"filename,omitempty"`
	Source      manifestSourceDTO `json:"source"`
}

type manifestSourceDTO struct {
	Kind          string `json:"kind"`
	OriginalIndex *int   `json:"original_index,omitempty"`
	// Value is base64-encoded so the JSON envelope can carry arbitrary
	// bytes — the manifest comes from the operator's UI, which encodes
	// edited field bytes that way.
	Value       string `json:"value,omitempty"`
	UploadField string `json:"upload_field,omitempty"`
}

// ReplayWithMultipart handles POST /{id}/replay-with-multipart.
// The request body is multipart/form-data with:
//   - a `__manifest` field carrying a JSON description of the desired
//     final body (parts, content types, file vs. field, etc.).
//   - one form-data file part per `kind: upload` reference in the
//     manifest, named however the manifest's `upload_field` says.
//
// The handler resolves the manifest against the captured blob, builds a
// fresh multipart body via Reassembler, and dispatches that body
// through the same internal-replay path Replay/ReplayWith use.
func (s *Service) ReplayWithMultipart(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntentID(r)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	dto, formErr := parseReplayMultipartForm(w, r)
	if formErr != nil {
		response.Error(w, r, formErr)
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	intent, intentErr := s.loadIntentForMultipartReplay(r.Context(), id)
	if intentErr != nil {
		response.Error(w, r, intentErr)
		return
	}

	parts, partsErr := manifestPartsFromDTO(dto)
	if partsErr != nil {
		response.Error(w, r, partsErr)
		return
	}

	cu, cuErr := s.usuarios.BuildCurrentUserByID(r.Context(), *intent.UsuarioID)
	if cuErr != nil {
		response.Error(w, r, cuErr)
		return
	}

	uploads := buildUploadMap(r.MultipartForm)
	bodyBytes, contentType, reaErr := s.reassembleMultipartBody(
		r.Context(), intent, parts, uploads,
	)
	if reaErr != nil {
		response.Error(w, r, mapReassembleError(reaErr))
		return
	}

	result := s.executeReplayWithBody(r.Context(), *intent, bodyBytes, contentType, cu)

	response.JSON(w, r, http.StatusOK, ReplayResponse{
		Outcome:           string(result.outcome),
		ReplayHTTPStatus:  result.httpStatus,
		ReplayBodyPreview: bodyPreview(result.bodyBytes),
	})
}

// parseReplayMultipartForm enforces the body-size cap and parses the
// incoming admin multipart form. The returned manifestDTO is populated
// from the __manifest field; the request's MultipartForm is left on r
// for the caller to drain later.
func parseReplayMultipartForm(w http.ResponseWriter, r *http.Request) (manifestDTO, error) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxReplayWithMultipartBytes)
	if parseErr := r.ParseMultipartForm(8 * 1024 * 1024); parseErr != nil {
		return manifestDTO{}, apperror.NewValidation(
			"invalid_replay_multipart_form",
			"el formulario multipart no es válido",
		).WithError(parseErr)
	}
	manifestJSON := r.FormValue(manifestFieldName)
	if manifestJSON == "" {
		return manifestDTO{}, apperror.NewValidation(
			"replay_manifest_required",
			"el campo __manifest es obligatorio",
		)
	}
	var dto manifestDTO
	dec := json.NewDecoder(strings.NewReader(manifestJSON))
	dec.DisallowUnknownFields()
	if dErr := dec.Decode(&dto); dErr != nil {
		return manifestDTO{}, apperror.NewValidation(
			"invalid_replay_manifest",
			"el __manifest no es un JSON válido",
		).WithError(dErr)
	}
	return dto, nil
}

// loadIntentForMultipartReplay loads the intent and asserts every
// precondition multipart replay needs: the intent exists, has a blob,
// belongs to a usuario, and the deployment has blob storage wired.
func (s *Service) loadIntentForMultipartReplay(
	ctx context.Context, id uuid.UUID,
) (*failedintent.Intent, error) {
	intent, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if intent == nil {
		return nil, apperror.NewNotFound(
			"failed_intent_not_found", "intento fallido no encontrado",
		)
	}
	if intent.BodyBlobPath == "" {
		return nil, apperror.NewValidation(
			"failed_intent_no_blob",
			"este intento no se capturó con multipart; usá /replay-with",
		)
	}
	if intent.UsuarioID == nil {
		return nil, apperror.NewValidation(
			"intent_has_no_usuario",
			"no se puede reproducir un intento sin usuario asociado",
		)
	}
	if s.blobs == nil {
		return nil, apperror.NewInternal(
			"failed_intent_blob_unavailable",
			"el almacenamiento de blobs no está disponible en esta instancia",
		)
	}
	return intent, nil
}

// reassembleMultipartBody invokes the Reassembler with the manifest and
// returns the produced body bytes plus the Content-Type to set on the
// dispatched request.
func (s *Service) reassembleMultipartBody(
	ctx context.Context,
	intent *failedintent.Intent,
	parts []failedintent.ManifestPart,
	uploads map[string]failedintent.Upload,
) ([]byte, string, error) {
	reas := failedintent.NewReassembler(s.blobs)
	var buf bytes.Buffer
	contentType, err := reas.Reassemble(ctx, failedintent.ReassembleInput{
		OriginalBlobPath:    intent.BodyBlobPath,
		OriginalContentType: intent.BodyContentType,
		Parts:               parts,
		Uploads:             uploads,
	}, &buf)
	if err != nil {
		return nil, "", err
	}
	return buf.Bytes(), contentType, nil
}

// executeReplayWithBody is the shared internal path that dispatches a
// pre-resolved request body. The bytes+content-type approach avoids the
// JSON-only assumption baked into resolveReplayBody.
func (s *Service) executeReplayWithBody(
	ctx context.Context,
	intent failedintent.Intent,
	body []byte,
	contentType string,
	cu auth.CurrentUser,
) replayResult {
	id := intent.ID

	if incErr := s.store.IncrementRetry(ctx, id); incErr != nil {
		slog.WarnContext(ctx, "failedintent: increment retry failed",
			"error", incErr, "intent_id", id.String())
	}
	originalStatus := intent.Status

	//nolint:gosec // G704: method/path captured by our own router; see ReplayWith.
	req, err := http.NewRequestWithContext(ctx, intent.Method, intent.Path,
		bytes.NewReader(body))
	if err != nil {
		slog.ErrorContext(ctx, "failedintent: multipart replay request build failed",
			"error", err, "intent_id", id.String())
		return replayResult{outcome: failedintent.StatusRetriedFail, httpStatus: http.StatusInternalServerError}
	}

	req.Header.Set("Content-Type", contentType)
	req.Header.Set(failedintent.HeaderInternalReplay, intent.ID.String())
	req.Header.Set("X-Request-ID", s.newID().String())
	req.Header.Set(idempotency.HeaderKey, s.newID().String())

	replayCtx := httpdispatch.InternalContext(req.Context())
	//nolint:contextcheck // intentional: plant the original requester's user.
	req = req.WithContext(auth.PlantCurrentUser(replayCtx, cu))

	rw := newReplayWriter()
	s.dispatcher.Dispatch(rw, req)

	outcome := outcomeFor(rw.status)
	s.tryUpdateStatus(ctx, id, originalStatus, outcome, cu.ID)

	return replayResult{
		outcome:    outcome,
		httpStatus: rw.status,
		bodyBytes:  rw.body.Bytes(),
	}
}

// buildUploadMap converts the parsed MultipartForm into the Upload map
// the reassembler expects. The `__manifest` field is skipped; every
// other form-data file becomes one entry keyed by its form name.
func buildUploadMap(form *multipart.Form) map[string]failedintent.Upload {
	uploads := map[string]failedintent.Upload{}
	if form == nil {
		return uploads
	}
	for name, headers := range form.File {
		if name == manifestFieldName {
			continue
		}
		if len(headers) == 0 {
			continue
		}
		// Only the first file per name is honored — duplicates would
		// require manifest disambiguation we don't support.
		fh := headers[0]
		uploads[name] = failedintent.Upload{
			Filename:    fh.Filename,
			ContentType: fh.Header.Get("Content-Type"),
			Open: func() (io.ReadCloser, error) {
				return fh.Open()
			},
		}
	}
	return uploads
}

// manifestPartsFromDTO converts the wire DTO into the domain ManifestPart
// slice the reassembler consumes. Performs base64 decoding for field
// values and presence checks for kind-specific fields.
func manifestPartsFromDTO(dto manifestDTO) ([]failedintent.ManifestPart, error) {
	if len(dto.Parts) == 0 {
		return nil, apperror.NewValidation(
			"replay_manifest_empty",
			"el __manifest no contiene partes",
		)
	}
	out := make([]failedintent.ManifestPart, 0, len(dto.Parts))
	for i, p := range dto.Parts {
		mp := failedintent.ManifestPart{
			Name:        p.Name,
			ContentType: p.ContentType,
			Filename:    p.Filename,
		}
		switch p.Source.Kind {
		case string(failedintent.ManifestSourceKeep):
			if p.Source.OriginalIndex == nil {
				return nil, apperror.NewValidation(
					"invalid_replay_manifest",
					fmt.Sprintf("part %d (%q): kind=keep requires original_index", i, p.Name),
				)
			}
			mp.Source = failedintent.ManifestSource{
				Kind:          failedintent.ManifestSourceKeep,
				OriginalIndex: *p.Source.OriginalIndex,
			}
		case string(failedintent.ManifestSourceField):
			decoded, err := base64.StdEncoding.DecodeString(p.Source.Value)
			if err != nil {
				return nil, apperror.NewValidation(
					"invalid_replay_manifest",
					fmt.Sprintf("part %d (%q): kind=field value is not valid base64", i, p.Name),
				).WithError(err)
			}
			mp.Source = failedintent.ManifestSource{
				Kind:  failedintent.ManifestSourceField,
				Value: decoded,
			}
		case string(failedintent.ManifestSourceUpload):
			if p.Source.UploadField == "" {
				return nil, apperror.NewValidation(
					"invalid_replay_manifest",
					fmt.Sprintf("part %d (%q): kind=upload requires upload_field", i, p.Name),
				)
			}
			mp.Source = failedintent.ManifestSource{
				Kind:        failedintent.ManifestSourceUpload,
				UploadField: p.Source.UploadField,
			}
		default:
			return nil, apperror.NewValidation(
				"invalid_replay_manifest",
				fmt.Sprintf("part %d (%q): unknown source kind %q",
					i, p.Name, p.Source.Kind),
			)
		}
		out = append(out, mp)
	}
	return out, nil
}

// mapReassembleError translates Reassembler sentinels into apperror
// instances. Anything else gets a generic 500.
func mapReassembleError(err error) error {
	switch {
	case errors.Is(err, failedintent.ErrManifestEmpty):
		return apperror.NewValidation("replay_manifest_empty",
			"el __manifest no contiene partes")
	case errors.Is(err, failedintent.ErrManifestPartNameEmpty):
		return apperror.NewValidation("invalid_replay_manifest",
			"toda parte del manifest requiere un name")
	case errors.Is(err, failedintent.ErrManifestPartKindInvalid):
		return apperror.NewValidation("invalid_replay_manifest",
			"el manifest contiene un kind no soportado")
	case errors.Is(err, failedintent.ErrManifestKeepIndexInvalid):
		return apperror.NewValidation("manifest_keep_index_invalid",
			"el original_index del manifest no existe en el body capturado")
	case errors.Is(err, failedintent.ErrManifestUploadMissing):
		return apperror.NewValidation("manifest_upload_missing",
			"falta el archivo referenciado por el manifest")
	case errors.Is(err, failedintent.ErrBlobNotFound):
		return apperror.NewInternal("failed_intent_blob_unavailable",
			"el archivo del intento se perdió")
	case errors.Is(err, failedintent.ErrBlobNotMultipart):
		return apperror.NewValidation("blob_intent_not_multipart",
			"el body capturado no es multipart")
	default:
		return apperror.NewInternal("replay_multipart_reassemble_failed",
			"no se pudo armar el body multipart corregido")
	}
}
