//nolint:misspell // visitas vocabulary is Spanish (imagen, descripcion) per project convention.
package visitashttp

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	visitasapp "github.com/abdimuy/msp-api/internal/visitas/app"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

// The media types POST /visitas accepts, chosen to reproduce EXACTLY what
// Huma accepted before this route learned to read its own body.
//
// Verified against huma v2.37.3, not assumed:
//
//   - An ABSENT Content-Type is JSON. huma.go:998-1006 falls back to
//     "application/json" when the header is empty, and api.go:338-341 does it
//     again inside Unmarshal. A client that posts a JSON body without the
//     header got a 201 before and must keep getting one — this is precisely
//     the phone-in-the-field case this whole change exists to protect.
//   - A bare "json" is JSON: it is a key in huma.DefaultFormats
//     (defaults.go:32-35) alongside "application/json".
//   - "application/vnd.x+json" and friends are NOT accepted, and were not
//     accepted before either: Unmarshal does an EXACT map lookup
//     (api.go:343) with no "+json" suffix handling on the request path. They
//     answered ErrUnknownContentType then and answer 415 now. Leaving them
//     rejected preserves the contract; accepting them would widen it.
const (
	mediaTypeJSON      = "application/json"
	mediaTypeJSONShort = "json"
	mediaTypeMultipart = "multipart/form-data"
)

// decodedRequest is the outcome of reading either wire format. Closers holds
// every multipart part opened by the decoder; the caller MUST close them all,
// in a deferred loop, or the request leaks file descriptors — a leak that
// only shows up under load, which is the worst kind.
type decodedRequest struct {
	Body    CrearVisitaBody
	Uploads []visitasapp.ImagenUploadInput
	Closers []io.Closer
}

// decodeCrearVisitaRequest reads the request body in whichever of the two
// supported formats it arrived in.
//
// It returns whatever it managed to open even when it also returns an error,
// so the caller's deferred close loop still runs over a partially decoded
// request.
//
// Every failure path here answers a validation apperror, never a panic and
// never an internal error: a malformed multipart envelope is a client
// mistake, and a 500 on this route would look like the server losing visits.
func decodeCrearVisitaRequest(contentType string, raw []byte) (decodedRequest, error) {
	switch mediaTypeOf(contentType) {
	case mediaTypeJSON, mediaTypeJSONShort:
		body, err := decodeCrearVisitaJSON(raw)
		return decodedRequest{Body: body}, err
	case mediaTypeMultipart:
		return decodeCrearVisitaMultipart(contentType, raw)
	default:
		return decodedRequest{}, errUnsupportedMediaType(contentType)
	}
}

// mediaTypeOf extracts the bare media type, lower-cased and without
// parameters, so "application/json; charset=utf-8" is recognized.
//
// An ABSENT header resolves to application/json — see the const block for why
// that is a restoration of Huma's behavior and not a convenience. A header
// present but unparseable resolves to "" and lands on the 415 branch: the
// client stated something and stated it wrong, which is different from saying
// nothing at all.
func mediaTypeOf(contentType string) string {
	if strings.TrimSpace(contentType) == "" {
		return mediaTypeJSON
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	return strings.ToLower(mediaType)
}

// errUnsupportedMediaType answers 415 directly rather than through an
// apperror, because apperror has no 415 kind and inventing one for a single
// route would be worse than this. The detail keeps the same "code=" shape
// mapAppError produces, so clients parse errors from this route uniformly.
func errUnsupportedMediaType(contentType string) error {
	return huma.NewError(
		http.StatusUnsupportedMediaType,
		"el Content-Type debe ser application/json o multipart/form-data",
		&huma.ErrorDetail{
			Message:  "code=content_type_no_soportado",
			Location: "header.Content-Type",
			Value:    contentType,
		},
	)
}

// decodeCrearVisitaJSON parses the legacy JSON body. Unknown fields are
// rejected so a client typo ("nota_" instead of "nota") is reported instead
// of silently dropped.
func decodeCrearVisitaJSON(raw []byte) (CrearVisitaBody, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return CrearVisitaBody{}, apperror.NewValidation(
			"visita_body_requerido", "el cuerpo de la solicitud es obligatorio",
		)
	}
	var body CrearVisitaBody
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		return CrearVisitaBody{}, apperror.NewValidation(
			"visita_body_json_invalido", "el cuerpo de la solicitud no es un JSON válido",
		).WithError(err)
	}
	return body, nil
}

// decodeCrearVisitaMultipart parses the multipart envelope: the `datos` field
// carrying the same JSON document as the legacy body, plus 0..N `imagen`
// parts.
//
// maxMemory is set to the body length so ReadForm never spills a part to a
// temp file — the bytes are already fully in memory (Huma read them under the
// global BodyLimit), so writing them out to disk again would be pure waste.
// form.RemoveAll still runs via the returned closers as a belt-and-braces
// measure in case that reasoning ever stops holding.
func decodeCrearVisitaMultipart(contentType string, raw []byte) (decodedRequest, error) {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return decodedRequest{}, apperror.NewValidation(
			"multipart_content_type_invalido", "el Content-Type multipart no es válido",
		).WithError(err)
	}
	boundary := params["boundary"]
	if boundary == "" {
		return decodedRequest{}, apperror.NewValidation(
			"multipart_boundary_faltante", "el Content-Type multipart no declara boundary",
		)
	}

	reader := multipart.NewReader(bytes.NewReader(raw), boundary)
	form, err := reader.ReadForm(int64(len(raw)) + 1)
	if err != nil {
		return decodedRequest{}, apperror.NewValidation(
			"multipart_invalido", "el cuerpo multipart no se pudo leer",
		).WithError(err)
	}
	out := decodedRequest{Closers: []io.Closer{formCloser{form}}}

	body, err := decodeDatosField(form)
	if err != nil {
		return out, err
	}
	out.Body = body

	// The storage key embeds the visita id, so the id has to be known before
	// any upload can be described. Parsing it here means a multipart request
	// with a broken id fails with the same code a JSON one does — the handler
	// parses it again and agrees.
	visitaID, err := parseVisitaID(body.ID)
	if err != nil {
		return out, err
	}

	uploads, closers, err := parseImagenesFromForm(form, visitaID)
	out.Closers = append(out.Closers, closers...)
	if err != nil {
		return out, err
	}
	out.Uploads = uploads
	return out, nil
}

// decodeDatosField pulls the `datos` part out of the form and parses it as
// the legacy JSON document — byte-for-byte the same shape the JSON contract
// carries, which is what lets one handler serve both.
func decodeDatosField(form *multipart.Form) (CrearVisitaBody, error) {
	values := form.Value["datos"]
	if len(values) == 0 {
		return CrearVisitaBody{}, apperror.NewValidation(
			"datos_requerido", "el campo multipart 'datos' es obligatorio",
		)
	}
	return decodeCrearVisitaJSON([]byte(values[0]))
}

// parseImagenesFromForm turns the repeated `imagen` parts into the service's
// upload inputs, pairing each with its positional `id_<n>` / `descripcion_<n>`
// companions.
//
// It returns the closers for every part it managed to open even on the error
// path, so the caller closes them all in one deferred loop.
func parseImagenesFromForm(
	form *multipart.Form, visitaID uuid.UUID,
) ([]visitasapp.ImagenUploadInput, []io.Closer, error) {
	headers := form.File["imagen"]
	if len(headers) == 0 {
		return nil, nil, nil
	}
	uploads := make([]visitasapp.ImagenUploadInput, 0, len(headers))
	opened := make([]io.Closer, 0, len(headers))

	for i, fh := range headers {
		// Defensive: a part with no header at all is not something Go's
		// parser produces, but skipping it costs nothing and mirrors
		// cobranza's sweep over entries that are not set.
		if fh == nil {
			continue
		}
		imagenID, err := parsePositionalImagenID(form, i)
		if err != nil {
			return uploads, opened, err
		}
		mimeType := partContentType(fh)
		if !domain.IsAllowedMime(mimeType) {
			return uploads, opened, domain.ErrImagenMimeNoPermitido
		}
		file, err := fh.Open()
		if err != nil {
			return uploads, opened, apperror.NewValidation(
				"multipart_parte_ilegible", "no se pudo leer una de las imágenes",
			).WithField("posicion", strconv.Itoa(i)).WithError(err)
		}
		opened = append(opened, file)

		desc := positionalFormValue(form, "descripcion_", i)
		var descPtr *string
		if desc != "" {
			descPtr = &desc
		}

		uploads = append(uploads, visitasapp.ImagenUploadInput{
			ImagenID:    imagenID,
			StorageKind: domain.StorageKindFilesystem,
			StorageKey:  newStorageKeyVisita(visitaID, imagenID, fh.Filename, mimeType),
			Mime:        mimeType,
			SizeBytes:   fh.Size,
			Descripcion: descPtr,
			Body:        file,
		})
	}
	return uploads, opened, nil
}

// partContentType reads a part's declared type, lower-cased and stripped of
// parameters ("image/jpeg; charset=x" → "image/jpeg"). A part that declares
// nothing yields "" and is rejected by the whitelist — clients MUST set the
// per-part Content-Type, which is also what cobranza requires (Go's
// CreateFormFile does not set it; CreatePart with an explicit header does).
func partContentType(fh *multipart.FileHeader) string {
	raw := fh.Header.Get("Content-Type")
	if raw == "" {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(raw))
	}
	return strings.ToLower(mediaType)
}

// parseVisitaID parses the client-generated visita UUID, the idempotency key
// end-to-end. Shared by the multipart decoder and the handler so both formats
// answer the same code for the same broken id.
func parseVisitaID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperror.NewValidation(
			"visita_id_invalido", "el id de la visita no es un UUID válido",
		).WithError(err)
	}
	return id, nil
}

// parsePositionalImagenID reads `id_<n>` from the form. It is REQUIRED for
// every `imagen` part.
//
// It used to be optional, with the server minting a UUID when it was absent.
// That made retry safety depend on client discipline: the storage key is
// derived from the imagen id, so a retry carrying no id got a fresh one and
// stored the same photo a SECOND time — a second blob and a second row. The
// entire argument for putting images inside the create request is that a
// retry is safe BY CONSTRUCTION, and an optional id gave that away.
//
// Requiring it costs nothing today: no client sends images yet, this wire
// format is new in this change. The Android side (Task 23) must send a stable
// UUID per photo — the same one across retries of the same capture.
func parsePositionalImagenID(form *multipart.Form, index int) (uuid.UUID, error) {
	raw := positionalFormValue(form, "id_", index)
	if raw == "" {
		return uuid.Nil, apperror.NewValidation(
			"imagen_id_requerido",
			"cada imagen debe venir acompañada de su id_<n>",
		).WithField("posicion", strconv.Itoa(index))
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperror.NewValidation(
			"imagen_id_invalido", "el id de imagen no es un UUID válido",
		).WithField("posicion", strconv.Itoa(index)).WithError(err)
	}
	return id, nil
}

// positionalFormValue returns the first value of `<prefix><index>`, or "".
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

// newStorageKeyVisita builds the traversal-safe storage key for one photo:
// "visitas/<visita_id>/<imagen_id><ext>", mirroring cobranza's
// "pagos/<pago_id>/<imagen_id><ext>". The extension comes from the uploaded
// filename, lower-cased, falling back to the MIME's canonical extension when
// the client sent no filename.
//
// Both segments are UUIDs rendered by uuid.UUID.String(), so the key cannot
// contain a traversal sequence no matter what the client sent; the domain
// validates it again in NewImagenStorage regardless.
func newStorageKeyVisita(visitaID, imagenID uuid.UUID, filename, mimeType string) string {
	ext := strings.ToLower(path.Ext(filename))
	if ext == "" {
		ext = canonicalExtVisita(mimeType)
	}
	return "visitas/" + visitaID.String() + "/" + imagenID.String() + ext
}

// canonicalExtVisita maps an allowed MIME to its canonical extension. An
// unknown MIME yields "" — callers validate the MIME against the whitelist
// before this is ever reached.
func canonicalExtVisita(mimeType string) string {
	switch mimeType {
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

// formCloser adapts multipart.Form's RemoveAll to io.Closer so the handler's
// single deferred loop cleans up temp files as well as open parts.
type formCloser struct{ form *multipart.Form }

// Close removes any temp file the multipart parser may have spilled.
func (c formCloser) Close() error { return c.form.RemoveAll() }
