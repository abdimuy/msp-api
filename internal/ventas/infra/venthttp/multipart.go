//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp

import (
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
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

// parseImagenesFromMultipart converts the typed `imagen` slice from the
// Huma multipart decoder into the slice of [ventasapp.ImagenUploadInput]
// the service expects, pairing each upload with its optional `id_<n>` /
// `descripcion_<n>` fields read from the raw form by position.
//
// Returns the slice of app inputs, the opened files the caller MUST defer-
// close, and the first validation error encountered.
func parseImagenesFromMultipart(
	ventaID uuid.UUID, files []huma.FormFile, rawForm *multipart.Form,
) ([]ventasapp.ImagenUploadInput, []io.Closer, error) {
	if len(files) == 0 {
		return nil, nil, nil
	}
	uploads := make([]ventasapp.ImagenUploadInput, 0, len(files))
	opened := make([]io.Closer, 0, len(files))
	for i, f := range files {
		if !f.IsSet {
			continue
		}
		opened = append(opened, f)

		imagenID, err := parsePositionalImagenID(rawForm, i)
		if err != nil {
			return uploads, opened, err
		}

		desc := positionalFormValue(rawForm, "descripcion_", i)
		var descPtr *string
		if desc != "" {
			descPtr = &desc
		}

		uploads = append(uploads, ventasapp.ImagenUploadInput{
			ImagenID:    imagenID,
			StorageKind: string(domain.StorageKindFilesystem),
			StorageKey:  newStorageKey(ventaID, imagenID, f.Filename, f.ContentType),
			Mime:        f.ContentType,
			SizeBytes:   f.Size,
			Descripcion: descPtr,
			Body:        f,
		})
	}
	return uploads, opened, nil
}

// parsePositionalImagenID looks up `id_<n>` in the raw multipart form. When
// absent, returns a fresh UUID (the client should send stable IDs for
// replay safety). When present but not a UUID, returns a validation error.
func parsePositionalImagenID(form *multipart.Form, index int) (uuid.UUID, error) {
	raw := positionalFormValue(form, "id_", index)
	if raw == "" {
		return uuid.New(), nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperror.NewValidation(
			"imagen_id_invalido", "el id de imagen no es un UUID válido",
		).WithField("posicion", strconv.Itoa(index)).WithError(err)
	}
	return id, nil
}

// positionalFormValue returns the first value of `<prefix><index>` from the
// raw multipart form, or "" when absent.
func positionalFormValue(form *multipart.Form, prefix string, index int) string {
	if form == nil {
		return ""
	}
	values := form.Value[prefix+strconv.Itoa(index)]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
