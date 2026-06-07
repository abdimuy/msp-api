package failedintenthttp

import (
	"encoding/base64"
	"errors"
	"net/http"

	apperror "github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	"github.com/abdimuy/msp-api/internal/platform/response"
)

// BlobParts handles GET /{id}/blob-parts. It returns the parsed multipart
// structure of an intent's captured blob — one entry per section, with
// inline values for text fields and metadata-only for files. The UI uses
// this to populate the multipart variant of the replay-with editor.
//
// Errors surfaced to the client:
//   - 404 failed_intent_not_found        — id has no row
//   - 422 failed_intent_no_blob          — intent is JSON, not multipart
//   - 422 blob_intent_not_multipart      — content type is not multipart/*
//   - 503 failed_intent_blob_unavailable — blob path missing from disk
//   - 500 failed_intent_blob_parse_failed — parser error (corrupt body)
func (s *Service) BlobParts(w http.ResponseWriter, r *http.Request) {
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

	parts, perr := s.partsInspector.ListParts(
		r.Context(), intent.BodyBlobPath, intent.BodyContentType,
	)
	if perr != nil {
		response.Error(w, r, mapBlobInspectError(perr))
		return
	}

	dtos := make([]BlobPartDTO, 0, len(parts))
	for _, p := range parts {
		dtos = append(dtos, blobPartToDTO(p))
	}
	response.JSON(w, r, http.StatusOK, BlobPartsResponse{
		ContentType: intent.BodyContentType,
		Parts:       dtos,
	})
}

// blobPartToDTO maps a domain BlobPart to its JSON projection. Field
// values are base64-encoded so arbitrary bytes survive — the UI decodes
// when rendering text and may surface a "binary" affordance otherwise.
func blobPartToDTO(p failedintent.BlobPart) BlobPartDTO {
	dto := BlobPartDTO{
		Index:       p.Index,
		Name:        p.Name,
		Kind:        string(p.Kind),
		ContentType: p.ContentType,
		Filename:    p.Filename,
		SizeBytes:   p.SizeBytes,
	}
	if p.Kind == failedintent.BlobPartKindField && p.Value != nil {
		dto.Value = base64.StdEncoding.EncodeToString(p.Value)
	}
	return dto
}

// mapBlobInspectError translates internal inspector errors into apperror
// instances the response layer turns into RFC 9457 Problem Details.
func mapBlobInspectError(err error) error {
	switch {
	case errors.Is(err, failedintent.ErrBlobNotFound):
		return apperror.NewInternal(
			"failed_intent_blob_unavailable",
			"el archivo del intento se perdió, no se puede inspeccionar",
		)
	case errors.Is(err, failedintent.ErrBlobNotMultipart):
		return apperror.NewValidation(
			"blob_intent_not_multipart",
			"el body capturado no es un multipart/form-data",
		)
	default:
		return apperror.NewInternal(
			"failed_intent_blob_parse_failed",
			"no se pudo inspeccionar el body multipart",
		)
	}
}
