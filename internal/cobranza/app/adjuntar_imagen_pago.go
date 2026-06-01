//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app

import (
	"bytes"
	"context"
	"io"
	"log/slog"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// AdjuntarImagenPagoInput is the request value object for AdjuntarImagenPago.
// Body must be a fresh reader positioned at the start of the file.
type AdjuntarImagenPagoInput struct {
	PagoID      uuid.UUID
	ImagenID    uuid.UUID
	StorageKind domain.StorageKind
	StorageKey  string
	Mime        string
	SizeBytes   int64
	Descripcion *string
	Body        io.Reader
}

// AdjuntarImagenPago attaches a comprobante (image or PDF) to a PagoRecibido.
// The flow:
//
//  1. Load the pago (FindByID).
//  2. Process the blob:
//     - image/* MIME → run through imageprocessor (resize, recompress).
//     - application/pdf → bypass processor; store as-is.
//  3. Persist the blob via storage.Store.
//  4. Insert the imagen row via the repo.
//  5. On step 4 failure, best-effort delete the blob.
func (s *Service) AdjuntarImagenPago(ctx context.Context, in AdjuntarImagenPagoInput, by uuid.UUID) (*domain.Imagen, error) {
	if s.pagosImagenes == nil {
		return nil, errWriteDepsMissing("pagos_imagenes_repo")
	}
	if s.storage == nil {
		return nil, errWriteDepsMissing("storage_provider")
	}
	if s.pagosRecibidos == nil {
		return nil, errWriteDepsMissing("pagos_recibidos_repo")
	}

	// Verify the pago exists before touching storage.
	pago, err := s.pagosRecibidos.FindByID(ctx, in.PagoID)
	if err != nil {
		return nil, err
	}

	storage, err := domain.NewImagenStorage(in.StorageKind, in.StorageKey)
	if err != nil {
		return nil, err
	}

	mime, sizeBytes, body, err := s.processBlob(ctx, in)
	if err != nil {
		return nil, err
	}

	if err := s.storage.Store(ctx, storage.Key(), mime, sizeBytes, body); err != nil {
		return nil, err
	}

	img, err := pago.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID:          in.ImagenID,
		Storage:     storage,
		Mime:        mime,
		SizeBytes:   sizeBytes,
		Descripcion: in.Descripcion,
		By:          by,
		Now:         s.clock.Now(),
	})
	if err != nil {
		s.bestEffortDeleteBlob(ctx, storage.Key())
		return nil, err
	}

	if err := s.pagosImagenes.InsertImagen(ctx, in.PagoID, img); err != nil {
		s.bestEffortDeleteBlob(ctx, storage.Key())
		return nil, err
	}
	return img, nil
}

// processBlob runs the imageprocessor on image/* MIME types and short-circuits
// for application/pdf (just buffers the body to know its size).
func (s *Service) processBlob(ctx context.Context, in AdjuntarImagenPagoInput) (string, int64, io.Reader, error) {
	if in.Mime == domain.MimePDF {
		// PDF: buffer to count bytes if the upstream didn't tell us, but
		// do NOT decode/recompress.
		if in.SizeBytes > 0 {
			return in.Mime, in.SizeBytes, in.Body, nil
		}
		buf, err := io.ReadAll(in.Body)
		if err != nil {
			return "", 0, nil, err
		}
		return in.Mime, int64(len(buf)), bytes.NewReader(buf), nil
	}

	if s.imageProc == nil {
		return "", 0, nil, errWriteDepsMissing("image_processor")
	}
	out, err := s.imageProc.Process(ctx, in.processorInput())
	if err != nil {
		return "", 0, nil, err
	}
	return out.ContentType, out.SizeBytes, out.Body, nil
}

// processorInput repacks an upload as the value object the platform image
// processor expects. Defined as a method on the DTO so the processBlob path
// stays linear.
func (in AdjuntarImagenPagoInput) processorInput() outbound.ImageProcessorInput {
	return outbound.ImageProcessorInput{
		Body:        in.Body,
		ContentType: in.Mime,
		SizeBytes:   in.SizeBytes,
	}
}

// bestEffortDeleteBlob logs but does not propagate failures; the row was not
// persisted, so an orphan blob is the lesser evil. Operators can run a
// reconciler later.
func (s *Service) bestEffortDeleteBlob(ctx context.Context, key string) {
	if err := s.storage.Delete(ctx, key); err != nil {
		slog.WarnContext(ctx, "pago_imagen.blob_delete_failed",
			slog.String("key", key),
			slog.String("error", err.Error()),
		)
	}
}

// EliminarImagenPago removes a comprobante row + its blob.
func (s *Service) EliminarImagenPago(ctx context.Context, pagoID, imagenID, by uuid.UUID) error {
	if s.pagosImagenes == nil {
		return errWriteDepsMissing("pagos_imagenes_repo")
	}
	if s.storage == nil {
		return errWriteDepsMissing("storage_provider")
	}

	img, err := s.pagosImagenes.FindImagenByID(ctx, imagenID)
	if err != nil {
		return err
	}
	key := img.Storage().Key()

	if err := s.pagosImagenes.DeleteImagen(ctx, imagenID); err != nil {
		return err
	}
	// Best-effort blob removal; if the blob fails the row is gone and a
	// reconciler can clean up dangling files later.
	s.bestEffortDeleteBlob(ctx, key)
	_ = pagoID // pagoID validated by the handler via the imagen's FK; no extra check needed here.
	_ = by
	return nil
}

// ObtenerImagenPagoResult carries an Imagen plus an open storage object that
// the HTTP layer streams to the response. The caller MUST close Object.Body.
type ObtenerImagenPagoResult struct {
	Imagen *domain.Imagen
	Object struct {
		Body        io.ReadCloser
		ContentType string
		SizeBytes   int64
	}
}

// ObtenerImagenPago loads the imagen metadata + opens its blob for streaming.
func (s *Service) ObtenerImagenPago(ctx context.Context, pagoID, imagenID uuid.UUID) (*ObtenerImagenPagoResult, error) {
	if s.pagosImagenes == nil {
		return nil, errWriteDepsMissing("pagos_imagenes_repo")
	}
	if s.storage == nil {
		return nil, errWriteDepsMissing("storage_provider")
	}

	img, err := s.pagosImagenes.FindImagenByID(ctx, imagenID)
	if err != nil {
		return nil, err
	}
	obj, err := s.storage.Get(ctx, img.Storage().Key())
	if err != nil {
		return nil, err
	}
	out := &ObtenerImagenPagoResult{Imagen: img}
	out.Object.Body = obj.Body
	out.Object.ContentType = obj.ContentType
	out.Object.SizeBytes = obj.SizeBytes
	_ = pagoID
	return out, nil
}

// ListarImagenesPago returns every imagen attached to pagoID.
func (s *Service) ListarImagenesPago(ctx context.Context, pagoID uuid.UUID) ([]*domain.Imagen, error) {
	if s.pagosImagenes == nil {
		return nil, errWriteDepsMissing("pagos_imagenes_repo")
	}
	return s.pagosImagenes.ListImagenes(ctx, pagoID)
}
