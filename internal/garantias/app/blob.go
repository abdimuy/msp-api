package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// guardarBlob valida el contenido y guarda cada intento bajo una ruta nueva.
// prefijo llega como "garantias/<garantia_id>".
func (s *Service) guardarBlob(
	ctx context.Context,
	prefijo string,
	contenido []byte,
	maxBytes int64,
) (string, error) {
	if len(contenido) == 0 {
		return "", domain.ErrImagenVacia
	}

	if int64(len(contenido)) > maxBytes {
		return "", domain.ErrImagenMuyGrande
	}

	var extension string

	contentType := http.DetectContentType(contenido)
	switch contentType {
	case "image/jpeg":
		extension = ".jpg"
	case "image/png":
		extension = ".png"
	default:
		return "", domain.ErrImagenFormatoInvalido
	}

	ruta := strings.TrimSuffix(prefijo, "/") +
		"/" + uuid.NewString() + extension

	if err := s.storage.Store(
		ctx,
		ruta,
		contentType,
		int64(len(contenido)),
		bytes.NewReader(contenido),
	); err != nil {
		return "", err
	}

	return ruta, nil
}

// descartarBlobSiNoQuedo elimina el blob recién escrito salvo que la fila que
// sobrevivió en la base apunte exactamente a esa misma ruta.
func (s *Service) descartarBlobSiNoQuedo(
	ctx context.Context,
	imagenID uuid.UUID,
	ruta string,
) {
	img, err := s.imagenes.Obtener(ctx, imagenID)

	if err == nil && img != nil && img.Ruta() == ruta {
		return
	}

	if err != nil && !errors.Is(err, domain.ErrImagenNoEncontrada) {
		slog.WarnContext(
			ctx,
			"garantias.blob_lookup_failed",
			"imagen_id", imagenID,
			"storage_key", ruta,
			"error", err,
		)
		return
	}

	if err := s.storage.Delete(ctx, ruta); err != nil {
		slog.WarnContext(
			ctx,
			"garantias.storage_rollback_failed",
			"imagen_id", imagenID,
			"storage_key", ruta,
			"error", err,
		)
	}
}
