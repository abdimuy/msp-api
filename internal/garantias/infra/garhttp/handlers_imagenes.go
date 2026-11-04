//nolint:misspell // vocabulario español del módulo.
package garhttp

import (
	"context"
	"io"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

const maxEvidenciaHTTPBytes int64 = 10 * 1024 * 1024

// imagenCommandHandlers atiende los comandos HTTP de imágenes.
type imagenCommandHandlers struct {
	servicio *app.Service
}

func newImagenCommandHandlers(servicio *app.Service) *imagenCommandHandlers {
	return &imagenCommandHandlers{servicio: servicio}
}

// AdjuntarImagen atiende POST /garantias/{id}/eventos/{eid}/imagenes.
func (h *imagenCommandHandlers) AdjuntarImagen(
	ctx context.Context,
	in *AdjuntarImagenRequest,
) (*AdjuntarImagenResponse, error) {
	garantiaID, err := parseUUIDDeRuta(in.ID, "la garantía")
	if err != nil {
		return nil, mapAppError(ctx, err)
	}

	eventoID, err := parseUUIDDeRuta(in.EventoID, "el evento")
	if err != nil {
		return nil, mapAppError(ctx, err)
	}

	contenido, err := leerEvidencia(in.RawBody.Data().Archivo)
	if err != nil {
		return nil, mapAppError(ctx, err)
	}

	imagen, err := h.servicio.AdjuntarImagen(ctx, app.AdjuntarImagenCmd{
		GarantiaID:        garantiaID,
		EventoID:          eventoID,
		Description:       in.RawBody.Data().Description,
		Contenido:         contenido,
		ClaveIdempotencia: in.IdempotencyKey,
	})
	if err != nil {
		return nil, mapAppError(ctx, err)
	}

	out := &AdjuntarImagenResponse{}
	out.Body = ImagenDTO{
		ID:          imagen.ID().String(),
		Tipo:        imagen.Tipo().String(),
		Description: imagen.Description(),
		SubidaPor:   imagen.SubidaPor(),
		CreatedAt:   formatTime(imagen.CreatedAt()),
		URL: "/garantias/" + garantiaID.String() +
			"/eventos/" + eventoID.String() +
			"/imagenes/" + imagen.ID().String(),
	}

	return out, nil
}

func leerEvidencia(archivo huma.FormFile) ([]byte, error) {
	if !archivo.IsSet {
		return nil, domain.ErrImagenVacia
	}
	defer func() { _ = archivo.Close() }()

	contenido, err := io.ReadAll(
		io.LimitReader(archivo, maxEvidenciaHTTPBytes+1),
	)
	if err != nil {
		return nil, err
	}

	return contenido, nil
}

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
