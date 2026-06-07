package failedintenthttp

import (
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	apperror "github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	"github.com/abdimuy/msp-api/internal/platform/response"
)

// BlobPartDownload handles GET /{id}/blob-parts/{index}/download.
// Streams the bytes of the file part at the given zero-based index back
// to the caller with Content-Type and Content-Disposition mirroring the
// part's original headers. No Content-Length is set — multipart parsing
// is forward-only, so chunked transfer is required.
//
// Failure modes (mapped to apperror codes the UI handles):
//   - 422 invalid_intent_id          — id path is not a UUID
//   - 422 invalid_part_index         — index is not a non-negative integer
//   - 404 failed_intent_not_found    — id has no row
//   - 422 failed_intent_no_blob      — intent has no captured blob
//   - 422 blob_intent_not_multipart  — captured content type is not multipart
//   - 422 part_index_out_of_range    — the multipart body has fewer parts
//   - 500 failed_intent_blob_unavailable — blob path missing from disk
//   - 500 failed_intent_blob_parse_failed — parser error mid-stream
func (s *Service) BlobPartDownload(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntentID(r)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	indexStr := chi.URLParam(r, "index")
	index, parseErr := strconv.Atoi(indexStr)
	if parseErr != nil || index < 0 {
		response.Error(w, r, apperror.NewValidation(
			"invalid_part_index",
			"el índice del part debe ser un entero no negativo",
		))
		return
	}

	intent, err := s.store.Get(r.Context(), id)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	if intent == nil {
		response.Error(w, r, apperror.NewNotFound(
			"failed_intent_not_found", "intento fallido no encontrado",
		))
		return
	}
	if intent.BodyBlobPath == "" {
		response.Error(w, r, apperror.NewValidation(
			"failed_intent_no_blob",
			"este intento no se capturó con multipart",
		))
		return
	}
	if s.partsInspector == nil {
		response.Error(w, r, apperror.NewInternal(
			"failed_intent_blob_unavailable",
			"el almacenamiento de blobs no está disponible en esta instancia",
		))
		return
	}

	// Set headers eagerly: once the inspector starts streaming, response
	// status is fixed. onLocated captures the part's headers from the
	// original multipart and applies them to the response with hardening
	// against stored-XSS via attacker-controlled content type/filename:
	//
	//   - Content-Disposition is ALWAYS `attachment` (with quoted
	//     filename param when known), so a captured text/html or
	//     image/svg+xml part can never render inline.
	//   - Content-Type is forced to application/octet-stream unless the
	//     part's reported type is in a small known-safe allowlist.
	//   - X-Content-Type-Options: nosniff prevents the browser from
	//     guessing a richer type than what we sent.
	//   - Content-Security-Policy: sandbox; default-src 'none' nukes
	//     any script execution if a browser does try to render.
	//   - Cache-Control: no-store because blobs can be deleted by the
	//     janitor or resolver at any moment.
	headersSet := false
	onLocated := func(p failedintent.BlobPart) {
		dispParams := map[string]string{}
		if p.Filename != "" {
			dispParams["filename"] = p.Filename
		}
		w.Header().Set("Content-Disposition",
			mime.FormatMediaType("attachment", dispParams))
		w.Header().Set("Content-Type", safeDownloadContentType(p.ContentType))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		headersSet = true
	}

	_, downloadErr := s.partsInspector.DownloadPart(
		r.Context(),
		intent.BodyBlobPath, intent.BodyContentType,
		index, onLocated, w,
	)
	if downloadErr == nil {
		return
	}

	// We can only build a structured error response if headers haven't
	// been written yet. Once they have, the only safe move is to log and
	// drop the connection — the body is already partial.
	if headersSet {
		slog.WarnContext(
			r.Context(),
			"failedintent: blob part download interrupted mid-stream",
			"intent_id", id.String(),
			"part_index", index,
			"err", downloadErr.Error(),
		)
		return
	}
	response.Error(w, r, mapBlobDownloadError(downloadErr))
}

// octetStreamContentType is the inert media type we fall back to whenever
// the captured part's reported Content-Type is missing, unparseable, or
// outside the small allowlist below.
const octetStreamContentType = "application/octet-stream"

// safeDownloadContentTypes is the small allowlist of media types we let
// pass through unchanged. Everything else is forced to
// application/octet-stream so a captured part with a hostile
// Content-Type (text/html, image/svg+xml, etc.) cannot be rendered
// inline by the operator's browser — defense in depth on top of the
// `attachment` Content-Disposition and `nosniff` header.
var safeDownloadContentTypes = map[string]struct{}{
	"image/jpeg":       {},
	"image/png":        {},
	"image/webp":       {},
	"image/gif":        {},
	"application/json": {},
	"application/pdf":  {},
	"text/plain":       {},
}

// safeDownloadContentType returns the part's content type if it's in the
// allowlist, otherwise application/octet-stream. Parameters after the
// media type (charset, boundary, etc.) are dropped because they don't
// influence the safety decision.
func safeDownloadContentType(raw string) string {
	if raw == "" {
		return octetStreamContentType
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return octetStreamContentType
	}
	if _, ok := safeDownloadContentTypes[mediaType]; ok {
		return mediaType
	}
	return octetStreamContentType
}

// mapBlobDownloadError translates inspector errors into apperror
// instances. Shares the inspect-side classifier where possible; the
// out-of-range path is unique to the download endpoint.
func mapBlobDownloadError(err error) error {
	if errors.Is(err, failedintent.ErrBlobPartOutOfRange) {
		return apperror.NewValidation(
			"part_index_out_of_range",
			"el índice solicitado excede la cantidad de parts del body",
		)
	}
	return mapBlobInspectError(err)
}
