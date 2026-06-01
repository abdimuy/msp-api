//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth"
	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// AdjuntarImagenPago handles POST /cobranza/pagos/{id}/imagenes.
//
// Accepts a multipart/form-data body with a required "file" field (image or
// PDF) and an optional "descripcion" text field. The blob is stored on the
// local filesystem (StorageKindFilesystem) before the imagen row is written.
//
// Permission: PermCobranzaVerPagos (same as the read side — no separate
// upload permission exists yet; a fine-grained PermCobranzaSubirImagenes can
// be added later without changing the handler signature).
func (h *Handlers) AdjuntarImagenPago(ctx context.Context, in *AdjuntarImagenPagoInput) (*AdjuntarImagenPagoOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermCobranzaVerPagos); err != nil {
		return nil, err
	}

	pagoID, err := parseUUIDField(in.ID, "id")
	if err != nil {
		return nil, mapAppError(err)
	}

	fields := in.RawBody.Data()
	file, err := requireFormFilePago(fields.File)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	imagenID := uuid.New()
	mime := file.ContentType
	storageKey := newStorageKeyPago(pagoID, imagenID, file.Filename, mime)

	var desc *string
	if fields.Descripcion != "" {
		v := fields.Descripcion
		desc = &v
	}

	appIn := cobranzaapp.AdjuntarImagenPagoInput{
		PagoID:      pagoID,
		ImagenID:    imagenID,
		StorageKind: domain.StorageKindFilesystem,
		StorageKey:  storageKey,
		Mime:        mime,
		SizeBytes:   file.Size,
		Descripcion: desc,
		Body:        file,
	}
	img, err := h.svc.AdjuntarImagenPago(ctx, appIn, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &AdjuntarImagenPagoOutput{Body: toImagenPagoDTO(img)}, nil
}

// ListarImagenesPago handles GET /cobranza/pagos/{id}/imagenes.
//
// Lists all comprobantes attached to the pago.
func (h *Handlers) ListarImagenesPago(ctx context.Context, in *ListarImagenesPagoInput) (*ListarImagenesPagoOutput, error) {
	if err := authorize(ctx, auth.PermCobranzaVerPagos); err != nil {
		return nil, err
	}
	pagoID, err := parseUUIDField(in.ID, "id")
	if err != nil {
		return nil, mapAppError(err)
	}
	imagenes, err := h.svc.ListarImagenesPago(ctx, pagoID)
	if err != nil {
		return nil, mapAppError(err)
	}
	items := make([]ImagenPagoDTO, 0, len(imagenes))
	for _, img := range imagenes {
		items = append(items, toImagenPagoDTO(img))
	}
	return &ListarImagenesPagoOutput{Body: items}, nil
}

// EliminarImagenPago handles DELETE /cobranza/pagos/{id}/imagenes/{img_id}.
//
// Removes the imagen row and best-effort deletes the blob from storage.
func (h *Handlers) EliminarImagenPago(ctx context.Context, in *EliminarImagenPagoInput) (*EliminarImagenPagoOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermCobranzaVerPagos); err != nil {
		return nil, err
	}
	pagoID, err := parseUUIDField(in.ID, "id")
	if err != nil {
		return nil, mapAppError(err)
	}
	imagenID, err := parseUUIDField(in.ImageID, "img_id")
	if err != nil {
		return nil, mapAppError(err)
	}
	if err := h.svc.EliminarImagenPago(ctx, pagoID, imagenID, cu.ID); err != nil {
		return nil, mapAppError(err)
	}
	return &EliminarImagenPagoOutput{}, nil
}

// obtenerImagenPagoHandler streams the imagen blob with caching headers.
//
//	GET /cobranza/pagos/{id}/imagenes/{img_id}
//	Authorization: Bearer <token>
//	If-None-Match: "<imagen-id>"   (optional)
//
// This endpoint is wired as a raw chi handler (not Huma) because Huma's
// response model assumes structured payloads; streaming arbitrary-size binary
// blobs with ETag + Cache-Control is cleaner via http.ResponseWriter.
// It is documented manually in api/openapi.yaml (or the cobranza docs page).
func (h *Handlers) obtenerImagenPagoHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	cu, ok := auth.CurrentUserFromContext(ctx)
	if !ok {
		writePlainErrorCobranza(w, http.StatusUnauthorized, "no_autenticado", "no autenticado")
		return
	}
	if err := requirePerm(cu, auth.PermCobranzaVerPagos); err != nil {
		writePlainErrorCobranza(w, http.StatusForbidden, "permiso_denegado", "permiso denegado")
		return
	}

	pagoID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writePlainErrorCobranza(w, http.StatusBadRequest, "pago_id_invalido", "el id del pago no es un UUID válido")
		return
	}
	imagenID, err := uuid.Parse(chi.URLParam(r, "img_id"))
	if err != nil {
		writePlainErrorCobranza(w, http.StatusBadRequest, "imagen_id_invalida", "el id de la imagen no es un UUID válido")
		return
	}

	result, err := h.svc.ObtenerImagenPago(ctx, pagoID, imagenID)
	if err != nil {
		writeAppErrorCobranza(w, err)
		return
	}
	defer func() { _ = result.Object.Body.Close() }()

	// ETag derived from the imagen's immutable ID. Quoted per RFC 7232.
	etag := `"` + result.Imagen.ID().String() + `"`
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", obtenerImagenPagoCacheControl)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", result.Object.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(result.Object.SizeBytes, 10))
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", obtenerImagenPagoCacheControl)
	if _, copyErr := io.Copy(w, result.Object.Body); copyErr != nil {
		slog.WarnContext(ctx, "cobranza.imagen_stream_failed",
			"pago_id", pagoID,
			"imagen_id", imagenID,
			"error", copyErr,
		)
	}
}

// obtenerImagenPagoCacheControl is the Cache-Control header attached to every
// imagen download. Blobs are immutable once stored — only ever deleted, never
// edited — so they are safe to cache for the canonical "forever" of 1 year.
// The `private` directive prevents shared proxies from caching across users
// since the endpoint requires a bearer token.
const obtenerImagenPagoCacheControl = "private, max-age=31536000, immutable"

// mountObtenerImagenPago wires the streaming GET endpoint onto r. Kept off the
// Huma surface because Huma's response model assumes structured JSON or a
// fixed schema; streaming arbitrary-size blobs is awkward there.
func mountObtenerImagenPago(r chi.Router, h *Handlers) {
	r.Get("/pagos/{id}/imagenes/{img_id}", h.obtenerImagenPagoHandler)
}

// ─── Plain-error helpers (mirrored from ventas pattern) ───────────────────────

// writePlainErrorCobranza emits a JSON error body shaped like Huma's so
// clients have a consistent parsing path across Huma and raw chi endpoints.
func writePlainErrorCobranza(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, `{"status":`+strconv.Itoa(status)+
		`,"title":"`+http.StatusText(status)+
		`","detail":"`+message+
		`","errors":[{"message":"code=`+code+`"}]}`)
}

// writeAppErrorCobranza funnels an app-layer error through the plain JSON
// envelope. apperror values carry the canonical code + Spanish message;
// everything else degrades to a 500.
func writeAppErrorCobranza(w http.ResponseWriter, err error) {
	var ae *apperror.Error
	if errors.As(err, &ae) {
		writePlainErrorCobranza(w, ae.Kind.HTTPStatus(), ae.Code, ae.Message)
		return
	}
	writePlainErrorCobranza(w, http.StatusInternalServerError, "internal_error", "ocurrió un error interno")
}

// ─── Compile-time handler signature checks ────────────────────────────────────

var (
	_ func(context.Context, *AdjuntarImagenPagoInput) (*AdjuntarImagenPagoOutput, error) = (*Handlers)(nil).AdjuntarImagenPago
	_ func(context.Context, *ListarImagenesPagoInput) (*ListarImagenesPagoOutput, error) = (*Handlers)(nil).ListarImagenesPago
	_ func(context.Context, *EliminarImagenPagoInput) (*EliminarImagenPagoOutput, error) = (*Handlers)(nil).EliminarImagenPago
)
