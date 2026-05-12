//nolint:misspell // domain vocabulary is Spanish (imagen, ventas) per project convention.
package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// ObtenerImagenResult bundles the imagen metadata with the streaming blob
// object returned by the storage provider. The caller MUST close
// Object.Body once the bytes have been consumed.
type ObtenerImagenResult struct {
	// Imagen is the persisted imagen child, used by the HTTP layer to
	// derive caching headers (ETag, etc.).
	Imagen *domain.Imagen
	// Object is the storage backend's view of the blob: an open reader,
	// the content-type recorded at upload, and the byte length.
	Object outbound.StorageObject
}

// ObtenerImagen streams a previously attached imagen back to the caller.
// The flow is:
//  1. Load the venta. Returns [domain.ErrVentaNotFound] on miss.
//  2. Locate the imagen child by ID. Returns [domain.ErrImagenNotFound]
//     when the venta exists but does not own the requested imagen.
//  3. Fetch the blob from the storage provider keyed by the imagen's
//     storage key. The returned [outbound.StorageObject] carries an open
//     reader the caller streams to the HTTP response.
//
// Authorization (permiso ventas_ver) is enforced at the HTTP boundary.
// Cancelled ventas remain readable — the resource is immutable post-
// cancellation, not hidden.
func (s *Service) ObtenerImagen(ctx context.Context, ventaID, imagenID uuid.UUID) (ObtenerImagenResult, error) {
	venta, err := s.ventas.FindByID(ctx, ventaID)
	if err != nil {
		return ObtenerImagenResult{}, err
	}
	var found *domain.Imagen
	for img := range venta.Imagenes() {
		if img.ID() == imagenID {
			found = img
			break
		}
	}
	if found == nil {
		return ObtenerImagenResult{}, domain.ErrImagenNotFound
	}
	obj, err := s.storage.Get(ctx, found.Storage().Key())
	if err != nil {
		return ObtenerImagenResult{}, err
	}
	return ObtenerImagenResult{Imagen: found, Object: obj}, nil
}
