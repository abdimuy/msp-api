package garhttp

import (
	"context"
	"io"

	"github.com/google/uuid"
)

// ObtenerImagen devuelve el contenido binario de una imagen de la línea de tiempo.
func (h *Handlers) ObtenerImagen(
	ctx context.Context,
	in *ObtenerImagenRequest,
) (*ObtenerImagenResponse, error) {
	if err := h.consultas.ValidarAutenticacion(ctx); err != nil {
		return nil, mapAppError(ctx, err)
	}

	garantiaID, err := uuid.Parse(in.ID)
	if err != nil {
		return nil, mapAppError(ctx, errorValidacion(
			"invalid_uuid",
			"el identificador de la garantía no es un UUID válido",
		))
	}

	eventoID, err := uuid.Parse(in.EventoID)
	if err != nil {
		return nil, mapAppError(ctx, errorValidacion(
			"invalid_uuid",
			"el identificador del evento no es un UUID válido",
		))
	}

	imagenID, err := uuid.Parse(in.ImagenID)
	if err != nil {
		return nil, mapAppError(ctx, errorValidacion(
			"invalid_uuid",
			"el identificador de la imagen no es un UUID válido",
		))
	}

	descarga, err := h.consultas.ObtenerImagen(
		ctx,
		garantiaID,
		eventoID,
		imagenID,
	)
	if err != nil {
		return nil, mapAppError(ctx, err)
	}
	defer func() { _ = descarga.Objeto.Body.Close() }()

	body, err := io.ReadAll(descarga.Objeto.Body)
	if err != nil {
		return nil, mapAppError(ctx, err)
	}

	return &ObtenerImagenResponse{
		ContentType: descarga.Objeto.ContentType,
		Body:        body,
	}, nil
}
