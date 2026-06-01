//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp

import (
	"net/http"
	"path"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// parseUUIDField parses a string into a uuid.UUID with a stable apperror.
// Used by write-side handlers that receive UUIDs from path parameters.
func parseUUIDField(raw, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperror.NewValidation(
			"invalid_uuid", "el identificador en la URL no es un UUID válido",
		).WithField("param", name).WithError(err)
	}
	return id, nil
}

// newStorageKeyPago builds a stable, traversal-safe storage key for a pago
// imagen upload. Layout: "pagos/<pago_id>/<imagen_id><ext>". The extension is
// taken from the original filename (lower-cased), defaulting to the MIME's
// canonical extension when absent.
func newStorageKeyPago(pagoID, imagenID uuid.UUID, filename, mime string) string {
	ext := strings.ToLower(path.Ext(filename))
	if ext == "" {
		ext = canonicalExtPago(mime)
	}
	return "pagos/" + pagoID.String() + "/" + imagenID.String() + ext
}

// canonicalExtPago returns the canonical extension for an allowed MIME type.
// Empty string for an unknown MIME — callers validate the MIME upstream.
func canonicalExtPago(mime string) string {
	switch mime {
	case domain.MimeJPEG:
		return ".jpg"
	case domain.MimePNG:
		return ".png"
	case domain.MimeGIF:
		return ".gif"
	case domain.MimeWebP:
		return ".webp"
	case domain.MimePDF:
		return ".pdf"
	}
	return ""
}

// requireFormFilePago returns the uploaded file or a Huma 422 error when the
// file is missing. Huma's multipart decoder marks IsSet=false for absent
// fields so this is the cheapest precondition for the upload handler.
func requireFormFilePago(f huma.FormFile) (huma.FormFile, error) {
	if !f.IsSet {
		return huma.FormFile{}, huma.NewError(
			http.StatusUnprocessableEntity,
			"el archivo es obligatorio",
			&huma.ErrorDetail{Message: "field \"file\" was not present in the multipart body", Location: "body.file"},
		)
	}
	return f, nil
}
