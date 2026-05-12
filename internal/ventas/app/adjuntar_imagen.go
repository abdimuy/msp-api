//nolint:misspell // domain vocabulary is Spanish (imagen, descripcion) per project convention.
package app

import (
	"context"
	"errors"
	"io"
	"log/slog"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/imageprocessor"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// AdjuntarImagenInput is the request DTO for attaching an imagen to a venta.
// The Body stream is consumed by the storage provider before the database
// row is written, so callers must not reuse it after invocation.
type AdjuntarImagenInput struct {
	VentaID     uuid.UUID
	ImagenID    uuid.UUID
	StorageKind string
	StorageKey  string
	Mime        string
	SizeBytes   int64
	Descripcion *string
	Body        io.Reader
}

// AdjuntarImagen attaches a fresh imagen to the venta. The flow is:
//  1. Load the aggregate and reject canceled ventas.
//  2. Construct the ImagenStorage VO.
//  3. Pass the upload through the image processor (resize + recompress).
//     Mime/SizeBytes/Body in the input struct are rewritten to the
//     processor's output before the storage provider sees them.
//  4. Stream the processed body to the storage provider (writes the blob
//     first so a row is never persisted pointing to a nonexistent object).
//  5. Mutate the aggregate (emits an ImagenAdjuntadaEvent).
//  6. Persist the new imagen row inside a Firebird transaction. On failure,
//     best-effort delete the blob to avoid orphaned storage.
//  7. Drain pending events onto the outbox.
func (s *Service) AdjuntarImagen(ctx context.Context, in AdjuntarImagenInput, by uuid.UUID) (*domain.Imagen, error) {
	venta, err := s.ventas.FindByID(ctx, in.VentaID)
	if err != nil {
		return nil, err
	}
	if venta.IsCanceled() {
		return nil, domain.ErrVentaCanceladaInmutable
	}
	kind, err := domain.ParseStorageKind(in.StorageKind)
	if err != nil {
		return nil, err
	}
	storageVO, err := domain.NewImagenStorage(kind, in.StorageKey)
	if err != nil {
		return nil, err
	}

	processed, err := s.imageProc.Process(ctx, outbound.ImageProcessorInput{
		Body:        in.Body,
		ContentType: in.Mime,
		SizeBytes:   in.SizeBytes,
	})
	if err != nil {
		return nil, mapImageProcessorError(err)
	}
	in.Mime = processed.ContentType
	in.SizeBytes = processed.SizeBytes
	in.Body = processed.Body

	if err := s.storage.Store(ctx, storageVO.Key(), in.Mime, in.SizeBytes, in.Body); err != nil {
		return nil, err
	}
	img, err := venta.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID:          in.ImagenID,
		Storage:     storageVO,
		Mime:        in.Mime,
		SizeBytes:   in.SizeBytes,
		Descripcion: in.Descripcion,
		By:          by,
		Now:         s.clock.Now(),
	})
	if err != nil {
		s.bestEffortStorageDelete(ctx, storageVO.Key())
		return nil, err
	}
	if err := s.runInTx(ctx, func(ctx context.Context) error {
		return s.ventas.InsertImagen(ctx, in.VentaID, img)
	}); err != nil {
		s.bestEffortStorageDelete(ctx, storageVO.Key())
		return nil, err
	}
	s.drainEvents(ctx, venta)
	return img, nil
}

// bestEffortStorageDelete removes a blob, logging any failure but never
// returning it — the caller has already decided to abort with another error.
func (s *Service) bestEffortStorageDelete(ctx context.Context, key string) {
	if err := s.storage.Delete(ctx, key); err != nil {
		slog.WarnContext(
			ctx, "ventas.storage_rollback_failed",
			"storage_key", key,
			"error", err,
		)
	}
}

// mapImageProcessorError translates platform-level imageprocessor sentinels
// to the corresponding ventas-domain apperror values so the HTTP layer
// renders a stable Spanish message with the right status. Unknown errors
// pass through so they surface as 500 with the underlying detail.
func mapImageProcessorError(err error) error {
	switch {
	case errors.Is(err, imageprocessor.ErrInputTooLarge):
		return domain.ErrImagenDemasiadoGrande
	case errors.Is(err, imageprocessor.ErrUnsupportedMIME):
		return domain.ErrMimeNoPermitido
	case errors.Is(err, imageprocessor.ErrDecodeFailed):
		return domain.ErrImagenDecodeFallo
	}
	return err
}
