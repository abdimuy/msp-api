//nolint:misspell // domain vocabulary is Spanish (imagen) per project convention.
package app

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// EliminarImagen removes the imagen identified by imagenID from the venta.
// The storage key is captured before the aggregate is mutated so the blob
// can be removed after the database row is gone. The blob removal is
// best-effort — the database row is the source of truth, so a storage
// failure does not abort the request.
func (s *Service) EliminarImagen(ctx context.Context, ventaID, imagenID, by uuid.UUID) error {
	venta, err := s.ventas.FindByID(ctx, ventaID)
	if err != nil {
		return err
	}
	storageKey, ok := findImagenStorageKey(venta, imagenID)
	if !ok {
		return domain.ErrImagenNotFound
	}
	if err := venta.EliminarImagen(imagenID, by, s.clock.Now()); err != nil {
		return err
	}
	if err := s.runInTx(ctx, func(ctx context.Context) error {
		return s.ventas.DeleteImagen(ctx, ventaID, imagenID)
	}); err != nil {
		return err
	}
	if err := s.storage.Delete(ctx, storageKey); err != nil {
		slog.WarnContext(
			ctx, "ventas.storage_delete_failed",
			"venta_id", ventaID,
			"imagen_id", imagenID,
			"storage_key", storageKey,
			"error", err,
		)
	}
	s.drainEvents(ctx, venta)
	return nil
}

// findImagenStorageKey scans the venta's imagenes for the given ID and
// returns the storage key. Returns ("", false) when no imagen matches.
func findImagenStorageKey(v *domain.Venta, imagenID uuid.UUID) (string, bool) {
	for img := range v.Imagenes() {
		if img.ID() == imagenID {
			return img.Storage().Key(), true
		}
	}
	return "", false
}
