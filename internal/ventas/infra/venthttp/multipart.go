//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp

import (
	"net/http"
	"path"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// newStorageKey builds a stable, traversal-safe storage key for an imagen
// upload. Layout: "ventas/<venta_id>/<imagen_id><ext>". The extension is
// taken from the original filename (lower-cased), defaulting to the MIME's
// canonical extension when absent.
func newStorageKey(ventaID, imagenID uuid.UUID, filename, mime string) string {
	ext := strings.ToLower(path.Ext(filename))
	if ext == "" {
		ext = canonicalExt(mime)
	}
	return "ventas/" + ventaID.String() + "/" + imagenID.String() + ext
}

// canonicalExt returns the canonical extension for an allowed image MIME.
// Empty string for an unknown MIME — callers validate the MIME upstream.
func canonicalExt(mime string) string {
	switch mime {
	case domain.MimeJPEG:
		return ".jpg"
	case domain.MimePNG:
		return ".png"
	case domain.MimeGIF:
		return ".gif"
	case domain.MimeWebP:
		return ".webp"
	}
	return ""
}

// requireFormFile returns the uploaded file or a Huma 422 error when the
// file is missing. Huma's multipart decoder marks IsSet=false for absent
// fields so this is the cheapest precondition for the upload handler.
func requireFormFile(f huma.FormFile) (huma.FormFile, error) {
	if !f.IsSet {
		return huma.FormFile{}, huma.NewError(
			http.StatusUnprocessableEntity,
			"el archivo es obligatorio",
			&huma.ErrorDetail{Message: "field \"file\" was not present in the multipart body", Location: "body.file"},
		)
	}
	return f, nil
}
