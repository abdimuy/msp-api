//nolint:misspell // ventas vocabulary is Spanish (imagen, descripcion) per project convention.
package venthttp

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth"
	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// AdjuntarImagen handles POST /v2/ventas/{id}/imagenes. The multipart body
// is parsed by Huma into AdjuntarImagenInput.RawBody — this handler reads the
// FormFile, derives a deterministic storage key, and delegates to the
// application service which streams the body into the configured storage
// provider and writes the imagen row.
func (h *Handlers) AdjuntarImagen(ctx context.Context, in *AdjuntarImagenInput) (*AdjuntarImagenOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermVentasSubirImagenes); err != nil {
		return nil, err
	}
	ventaID, err := parseUUIDField(in.ID, "id")
	if err != nil {
		return nil, mapAppError(err)
	}
	fields := in.RawBody.Data()
	file, err := requireFormFile(fields.File)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	imagenID := uuid.New()
	mime := file.ContentType
	storageKey := newStorageKey(ventaID, imagenID, file.Filename, mime)
	var desc *string
	if fields.Descripcion != "" {
		v := fields.Descripcion
		desc = &v
	}
	input := ventasapp.AdjuntarImagenInput{
		VentaID:     ventaID,
		ImagenID:    imagenID,
		StorageKind: string(domain.StorageKindFilesystem),
		StorageKey:  storageKey,
		Mime:        mime,
		SizeBytes:   file.Size,
		Descripcion: desc,
		Body:        file,
	}
	img, err := h.svc.AdjuntarImagen(ctx, input, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &AdjuntarImagenOutput{Body: toImagenDTO(img)}, nil
}

// EliminarImagenOutput is the response wrapper for DELETE imagen. Huma uses
// the zero output type for 204 No Content responses, so this struct is empty.
type EliminarImagenOutput struct{}

// EliminarImagen handles DELETE /v2/ventas/{id}/imagenes/{img_id}.
func (h *Handlers) EliminarImagen(ctx context.Context, in *EliminarImagenInput) (*EliminarImagenOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermVentasEliminarImagenes); err != nil {
		return nil, err
	}
	ventaID, err := parseUUIDField(in.ID, "id")
	if err != nil {
		return nil, mapAppError(err)
	}
	imagenID, err := parseUUIDField(in.ImageID, "img_id")
	if err != nil {
		return nil, mapAppError(err)
	}
	if err := h.svc.EliminarImagen(ctx, ventaID, imagenID, cu.ID); err != nil {
		return nil, mapAppError(err)
	}
	return &EliminarImagenOutput{}, nil
}

// Compile-time assertions: imagen handlers fit the Huma signature.
var (
	_ func(context.Context, *AdjuntarImagenInput) (*AdjuntarImagenOutput, error) = (*Handlers)(nil).AdjuntarImagen
	_ func(context.Context, *EliminarImagenInput) (*EliminarImagenOutput, error) = (*Handlers)(nil).EliminarImagen
)
