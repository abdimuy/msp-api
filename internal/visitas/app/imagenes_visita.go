package app

import (
	"context"
	"errors"
	"io"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

// errLecturaImagenesSinConfigurar: el servicio se armó sin WithImagenes.
var errLecturaImagenesSinConfigurar = apperror.NewInternal(
	"visitas_imagenes_no_configuradas", "el servicio de visitas no tiene imágenes configuradas",
)

// ImagenVisitaAbierta es la imagen más su blob abierto. Quien la recibe DEBE
// cerrar Body.
type ImagenVisitaAbierta struct {
	Imagen      *domain.Imagen
	Body        io.ReadCloser
	ContentType string
	SizeBytes   int64
}

// ListarImagenesVisita devuelve las fotos de una visita existente (404 si la
// visita no existe; lista vacía si no tiene fotos).
func (s *Service) ListarImagenesVisita(ctx context.Context, visitaID uuid.UUID) ([]*domain.Imagen, error) {
	if s.imagenes == nil {
		return nil, errLecturaImagenesSinConfigurar
	}
	if _, err := s.repo.FindByID(ctx, visitaID); err != nil {
		return nil, err
	}
	return s.imagenes.ListImagenes(ctx, visitaID)
}

// ObtenerImagenVisita abre el blob de una imagen que pertenece a la visita.
// Una imagen de otra visita es domain.ErrImagenNoEncontrada (404).
func (s *Service) ObtenerImagenVisita(ctx context.Context, visitaID, imagenID uuid.UUID) (*ImagenVisitaAbierta, error) {
	if s.imagenes == nil || s.storage == nil {
		return nil, errLecturaImagenesSinConfigurar
	}
	img, err := s.imagenes.FindImagenDeVisita(ctx, visitaID, imagenID)
	if err != nil {
		return nil, err
	}
	obj, err := s.storage.Get(ctx, img.Storage().Key())
	if err != nil {
		var ae *apperror.Error
		if errors.As(err, &ae) {
			return nil, err
		}
		return nil, apperror.NewInternal("visita_imagen_blob_error", "no se pudo leer la imagen").WithError(err)
	}
	ct := obj.ContentType
	if ct == "" {
		ct = img.Mime()
	}
	return &ImagenVisitaAbierta{Imagen: img, Body: obj.Body, ContentType: ct, SizeBytes: obj.SizeBytes}, nil
}
