//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp

import (
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// ─── ImagenPagoDTO ────────────────────────────────────────────────────────────

// ImagenPagoDTO is the JSON projection of a domain.Imagen attached to a pago.
type ImagenPagoDTO struct {
	ID          string  `json:"id"                    format:"uuid"`
	Mime        string  `json:"mime"`
	SizeBytes   int64   `json:"size_bytes"`
	Descripcion *string `json:"descripcion,omitempty"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// toImagenPagoDTO projects a domain.Imagen into an ImagenPagoDTO.
func toImagenPagoDTO(img *domain.Imagen) ImagenPagoDTO {
	aud := img.Audit()
	return ImagenPagoDTO{
		ID:          img.ID().String(),
		Mime:        img.Mime(),
		SizeBytes:   img.SizeBytes(),
		Descripcion: img.Descripcion(),
		CreatedAt:   aud.CreatedAt().UTC().Format(time.RFC3339),
		UpdatedAt:   aud.UpdatedAt().UTC().Format(time.RFC3339),
	}
}

// ─── Multipart upload fields ───────────────────────────────────────────────────

// ImagenPagoUploadFields is the set of typed multipart fields accepted by
// POST /cobranza/pagos/{id}/imagenes. Huma populates this from the request
// via MultipartFormFiles[T].
type ImagenPagoUploadFields struct {
	File        huma.FormFile `form:"file"        contentType:"image/jpeg,image/png,image/gif,image/webp,application/pdf"`
	Descripcion string        `form:"descripcion" required:"false" doc:"Descripción opcional del comprobante"`
}

// ─── Adjuntar imagen ──────────────────────────────────────────────────────────

// AdjuntarImagenPagoInput carries the pago id path param and the multipart body.
type AdjuntarImagenPagoInput struct {
	ID      string `path:"id" format:"uuid"`
	RawBody huma.MultipartFormFiles[ImagenPagoUploadFields]
}

// AdjuntarImagenPagoOutput is the response wrapper for an upload.
type AdjuntarImagenPagoOutput struct {
	Body ImagenPagoDTO
}

// ─── Listar imagenes ─────────────────────────────────────────────────────────

// ListarImagenesPagoInput carries the pago id path param.
type ListarImagenesPagoInput struct {
	ID string `path:"id" format:"uuid"`
}

// ListarImagenesPagoOutput wraps the imagenes slice.
type ListarImagenesPagoOutput struct {
	Body []ImagenPagoDTO
}

// ─── Eliminar imagen ──────────────────────────────────────────────────────────

// EliminarImagenPagoInput carries the pago and imagen path params.
type EliminarImagenPagoInput struct {
	ID      string `path:"id"     format:"uuid"`
	ImageID string `path:"img_id" format:"uuid"`
}

// EliminarImagenPagoOutput is empty — Huma renders 204 No Content.
type EliminarImagenPagoOutput struct{}
