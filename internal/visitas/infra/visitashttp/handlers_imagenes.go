//nolint:misspell // vocabulario español por convención del proyecto.
package visitashttp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

// ImagenVisitaDTO tiene la misma forma que ImagenPagoDTO (contrato §6), pero
// con `descripcion` como null explícito (contrato §0) en vez de omitida.
type ImagenVisitaDTO struct {
	ID          string  `json:"id"          format:"uuid"`
	Mime        string  `json:"mime"`
	SizeBytes   int64   `json:"size_bytes"`
	Descripcion *string `json:"descripcion"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// ListarImagenesVisitaInput — GET /visitas/{id}/imagenes.
type ListarImagenesVisitaInput struct {
	ID string `path:"id"`
}

// ListarImagenesVisitaOutput envuelve la lista.
type ListarImagenesVisitaOutput struct {
	Body []ImagenVisitaDTO
}

func toImagenVisitaDTO(img *domain.Imagen) ImagenVisitaDTO {
	a := img.Audit()
	return ImagenVisitaDTO{
		ID: img.ID().String(), Mime: img.Mime(), SizeBytes: img.SizeBytes(), Descripcion: img.Descripcion(),
		CreatedAt: a.CreatedAt().UTC().Format(time.RFC3339), UpdatedAt: a.UpdatedAt().UTC().Format(time.RFC3339),
	}
}

// ListarImagenesVisita lista las fotos. Lectura con cobranza:ver_pagos
// (contrato §6), el mismo permiso que GET /visitas/{id}.
func (h *Handlers) ListarImagenesVisita(ctx context.Context, in *ListarImagenesVisitaInput) (*ListarImagenesVisitaOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermCobranzaVerPagos); err != nil {
		return nil, err
	}
	visitaID, err := parseVisitaID(in.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	imgs, err := h.svc.ListarImagenesVisita(ctx, visitaID)
	if err != nil {
		return nil, mapAppError(err)
	}
	out := make([]ImagenVisitaDTO, 0, len(imgs))
	for _, img := range imgs {
		out = append(out, toImagenVisitaDTO(img))
	}
	return &ListarImagenesVisitaOutput{Body: out}, nil
}

// cacheImagenVisita: los blobs nunca se editan (solo se borran), así que se
// cachean "para siempre" y en privado (mismo valor que pagos y ventas).
const cacheImagenVisita = "private, max-age=31536000, immutable"

// obtenerImagenVisita sirve el binario por chi (no Huma): GET
// /visitas/{id}/imagenes/{img_id}, con ETag y 304 si If-None-Match coincide.
func (h *Handlers) obtenerImagenVisita(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cu, ok := auth.CurrentUserFromContext(ctx)
	if !ok {
		writePlainErrorVisitas(w, http.StatusUnauthorized, "no_autenticado", "no autenticado")
		return
	}
	if err := requirePerm(cu, auth.PermCobranzaVerPagos); err != nil {
		writePlainErrorVisitas(w, http.StatusForbidden, "permiso_denegado", "permiso denegado")
		return
	}
	visitaID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writePlainErrorVisitas(w, http.StatusBadRequest, "visita_id_invalido", "el id de la visita no es un UUID válido")
		return
	}
	imagenID, err := uuid.Parse(chi.URLParam(r, "img_id"))
	if err != nil {
		writePlainErrorVisitas(w, http.StatusBadRequest, "imagen_id_invalida", "el id de la imagen no es un UUID válido")
		return
	}
	res, err := h.svc.ObtenerImagenVisita(ctx, visitaID, imagenID)
	if err != nil {
		var ae *apperror.Error
		if errors.As(err, &ae) {
			writePlainErrorVisitas(w, ae.Kind.HTTPStatus(), ae.Code, ae.Message)
			return
		}
		writePlainErrorVisitas(w, http.StatusInternalServerError, "internal_error", "ocurrió un error interno")
		return
	}
	defer func() { _ = res.Body.Close() }()

	etag := `"` + res.Imagen.ID().String() + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", cacheImagenVisita)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", res.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(res.SizeBytes, 10))
	if _, err := io.Copy(w, res.Body); err != nil {
		slog.WarnContext(ctx, "visitas.imagen_stream_failed", "visita_id", visitaID, "imagen_id", imagenID, "error", err)
	}
}

func mountImagenesVisita(r chi.Router, h *Handlers) {
	r.Get("/visitas/{id}/imagenes/{img_id}", h.obtenerImagenVisita)
}

var _ func(context.Context, *ListarImagenesVisitaInput) (*ListarImagenesVisitaOutput, error) = (*Handlers)(nil).ListarImagenesVisita
