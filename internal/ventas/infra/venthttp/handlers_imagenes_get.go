//nolint:misspell // ventas vocabulary is Spanish (imagen, ventas) per project convention.
package venthttp

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// obtenerImagenCacheControl is the Cache-Control header attached to every
// imagen download. Uploaded blobs are immutable — once attached they are
// only ever deleted, never edited — so the response is safe to cache for
// the canonical "forever" of 1 year (RFC 7234 recommends ≤ 1 year). The
// `private` directive prevents shared proxies from caching across users
// since the endpoint is bearer-token protected.
const obtenerImagenCacheControl = "private, max-age=31536000, immutable"

// mountObtenerImagen wires the streaming GET endpoint onto r. Kept off the
// Huma surface because Huma's response model assumes structured JSON or a
// fixed schema; streaming arbitrary-size blobs is awkward there. The
// endpoint is documented manually in api/openapi.yaml.
func mountObtenerImagen(r chi.Router, h *Handlers) {
	r.Get("/ventas/{id}/imagenes/{img_id}", h.obtenerImagenHandler)
}

// obtenerImagenHandler streams the imagen blob with caching headers.
//
//	GET /v2/ventas/{id}/imagenes/{img_id}
//	Authorization: Bearer <token>
//	If-None-Match: "<imagen-id>"   (optional)
//
// 200 OK with binary body, or 304 Not Modified when the client's
// If-None-Match matches the imagen's ETag. 401 if unauthenticated, 403 if
// the caller is missing ventas_ver, 404 if the venta or imagen does not
// exist.
func (h *Handlers) obtenerImagenHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	cu, ok := auth.CurrentUserFromContext(ctx)
	if !ok {
		writePlainError(w, http.StatusUnauthorized, "no_autenticado", "no autenticado")
		return
	}
	if err := requirePerm(cu, auth.PermVentasVer); err != nil {
		writePlainError(w, http.StatusForbidden, "permiso_denegado", "permiso denegado")
		return
	}

	ventaID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writePlainError(w, http.StatusBadRequest, "venta_id_invalida", "el id de la venta no es un UUID válido")
		return
	}
	imagenID, err := uuid.Parse(chi.URLParam(r, "img_id"))
	if err != nil {
		writePlainError(w, http.StatusBadRequest, "imagen_id_invalida", "el id de la imagen no es un UUID válido")
		return
	}

	result, err := h.svc.ObtenerImagen(ctx, ventaID, imagenID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	defer func() { _ = result.Object.Body.Close() }()

	// ETag derived from the imagen's immutable ID. Quoted per RFC 7232.
	etag := `"` + result.Imagen.ID().String() + `"`
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", obtenerImagenCacheControl)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", result.Object.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(result.Object.SizeBytes, 10))
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", obtenerImagenCacheControl)
	if _, copyErr := io.Copy(w, result.Object.Body); copyErr != nil {
		slog.WarnContext(ctx, "ventas.imagen_stream_failed",
			"venta_id", ventaID,
			"imagen_id", imagenID,
			"error", copyErr,
		)
	}
}

// writePlainError emits a JSON error body shaped like Huma's so clients
// have a consistent parsing path across Huma and raw chi endpoints.
func writePlainError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, `{"status":`+strconv.Itoa(status)+
		`,"title":"`+http.StatusText(status)+
		`","detail":"`+message+
		`","errors":[{"message":"code=`+code+`"}]}`)
}

// writeAppError funnels an app-layer error through the same code-style
// JSON envelope writePlainError uses. apperror values carry the canonical
// code + Spanish message; everything else degrades to a 500.
func writeAppError(w http.ResponseWriter, err error) {
	var ae *apperror.Error
	if errors.As(err, &ae) {
		writePlainError(w, ae.Kind.HTTPStatus(), ae.Code, ae.Message)
		return
	}
	writePlainError(w, http.StatusInternalServerError, "internal_error", "ocurrió un error interno")
}
