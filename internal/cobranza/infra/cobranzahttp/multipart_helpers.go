//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp

import (
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
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

// parseImagenesFromMultipart converts the typed `imagen` slice from the Huma
// multipart decoder into the slice of [cobranzaapp.ImagenUploadInput] the
// service expects, pairing each upload with its optional `id_<n>` /
// `descripcion_<n>` fields read from the raw form by position.
//
// Returns:
//   - the slice of app inputs ready to hand to CrearPagoConImagenes.
//   - the slice of opened files the caller MUST defer-close (every entry
//     remains open for the service to stream-into-storage).
//   - the first validation error encountered, if any. Returning early on
//     error still hands back any files already opened so the caller can
//     close them in a single deferred loop.
//
// Validation:
//   - Each imagen entry must be non-empty (Huma keeps zero-value FormFiles
//     in the slice when IsSet=false; we filter those defensively).
//   - `id_<n>`, when present, must parse as a UUID.
//   - `descripcion_<n>` length is enforced by the domain (200 runa cap).
func parseImagenesFromMultipart(
	pagoID uuid.UUID, files []huma.FormFile, rawForm *multipart.Form,
) ([]cobranzaapp.ImagenUploadInput, []io.Closer, error) {
	if len(files) == 0 {
		return nil, nil, nil
	}
	uploads := make([]cobranzaapp.ImagenUploadInput, 0, len(files))
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

		uploads = append(uploads, cobranzaapp.ImagenUploadInput{
			ImagenID:    imagenID,
			StorageKind: domain.StorageKindFilesystem,
			StorageKey:  newStorageKeyPago(pagoID, imagenID, f.Filename, f.ContentType),
			Mime:        f.ContentType,
			SizeBytes:   f.Size,
			Descripcion: descPtr,
			Body:        f,
		})
	}
	return uploads, opened, nil
}

// parsePositionalImagenID looks up `id_<n>` in the raw multipart form. When
// absent, returns a fresh UUID (reintents will duplicate; the client should
// send stable IDs for replay safety). When present but not a UUID, returns
// a validation apperror.
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
// raw multipart form, or "" when absent. multipart.Form keeps Value as
// map[string][]string — we read element 0 and ignore further repeats.
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

// jsonStringReader adapts a string to an io.Reader for json.Decoder. Local
// helper so the handler stays free of strings.NewReader noise.
func jsonStringReader(s string) io.Reader {
	return strings.NewReader(s)
}
