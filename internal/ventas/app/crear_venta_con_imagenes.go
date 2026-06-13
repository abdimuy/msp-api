//nolint:misspell // ventas vocabulary is Spanish (imagenes, comprobantes) per convention.
package app

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// ImagenUploadInput describes a single comprobante to attach inside the
// atomic CrearVentaConImagenes flow. The HTTP layer is responsible for
// generating ImagenID (or honoring a client-provided UUID), building a safe
// StorageKey, and surfacing the raw body as a Reader positioned at start.
type ImagenUploadInput struct {
	ImagenID    uuid.UUID
	StorageKind string
	StorageKey  string
	Mime        string
	SizeBytes   int64
	Descripcion *string
	Body        io.Reader
}

// CrearVentaConImagenes persists a venta + N comprobantes atómicamente.
// Unlike cobranza, ventas REQUIERE al menos una imagen — toda venta del
// showroom lleva firma o ID del cliente; un fallo posterior dejaría una
// venta sin evidencia, lo que es inaceptable para auditoría.
//
// Flujo:
//
//  1. Validar cliente, vendedores, imgs (count, MIME, dedup).
//  2. Construir agregado venta (sin persistir).
//  3. Process + store cada blob (filesystem) acumulando storedKeys.
//  4. Para cada upload: venta.AdjuntarImagen → muta el agregado en memoria.
//  5. runInTx → ventas.Save persiste header + combos + productos +
//     vendedores + imagenes en orden FK en una sola tx Firebird.
//  6. Cualquier fallo en pasos 3-5: best-effort delete cada storedKey
//     antes de propagar el error original. Tx rollback se encarga del resto.
//  7. Post-tx: drainEvents al outbox (no bloqueante).
//
// Cuando imgs está vacío devuelve [domain.ErrVentaEvidenciaRequerida] sin
// efectos secundarios — la regla "venta sin foto = no venta" se enforza
// antes que cualquier escritura.
func (s *Service) CrearVentaConImagenes(
	ctx context.Context, in CrearVentaInput, imgs []ImagenUploadInput, by uuid.UUID,
) (*domain.Venta, error) {
	if len(imgs) == 0 {
		return nil, domain.ErrVentaEvidenciaRequerida
	}
	if err := detectDuplicateImagenIDs(imgs); err != nil {
		return nil, err
	}

	now := s.clock.Now()
	if err := s.validateClienteID(ctx, in.ClienteID); err != nil {
		return nil, err
	}
	if err := s.validateVendedorUsuarios(ctx, in.Vendedores); err != nil {
		return nil, err
	}
	params, err := in.intoDomain(by, now)
	if err != nil {
		return nil, err
	}
	venta, err := domain.CrearVenta(params)
	if err != nil {
		return nil, err
	}
	// Validate stock using the fully built venta (includes combo children).
	if err := s.validateStockFromVenta(ctx, venta); err != nil {
		return nil, err
	}

	processed, storedKeys, perr := s.storeAllImagenBlobs(ctx, imgs)
	if perr != nil {
		s.cleanupBlobs(ctx, storedKeys)
		return nil, perr
	}

	if err := attachAllImagenesToVenta(venta, processed, by, now); err != nil {
		s.cleanupBlobs(ctx, storedKeys)
		return nil, err
	}

	if err := s.runInTx(ctx, func(ctx context.Context) error {
		if saveErr := s.ventas.Save(ctx, venta); saveErr != nil {
			return saveErr
		}
		return s.crearTraspasoParaVenta(ctx, venta, by, now)
	}); err != nil {
		s.cleanupBlobs(ctx, storedKeys)
		return nil, err
	}

	s.drainEvents(ctx, venta)
	return venta, nil
}

// processedImagen captures the materialized form of an upload after the
// imageprocessor / storage step has run, ready to attach to the aggregate.
type processedImagen struct {
	ImagenID    uuid.UUID
	Storage     domain.ImagenStorage
	Mime        string
	SizeBytes   int64
	Descripcion *string
}

// storeAllImagenBlobs validates each storage key, runs every upload through
// the image processor, and persists every blob. Returns the materialized
// inputs plus the storage keys written (for cleanup on later failure).
//
// On the first error the loop stops and returns what was already stored —
// callers MUST invoke [Service.cleanupBlobs] on the returned keys before
// propagating the error.
func (s *Service) storeAllImagenBlobs(
	ctx context.Context, imgs []ImagenUploadInput,
) ([]processedImagen, []string, error) {
	processed := make([]processedImagen, 0, len(imgs))
	storedKeys := make([]string, 0, len(imgs))
	for i := range imgs {
		p, err := s.processAndStoreImagen(ctx, imgs[i])
		if err != nil {
			return processed, storedKeys, err
		}
		storedKeys = append(storedKeys, p.Storage.Key())
		processed = append(processed, p)
	}
	return processed, storedKeys, nil
}

// processAndStoreImagen runs one upload through ParseStorageKind →
// NewImagenStorage → imageProc → storage.Store. Returns the values needed
// to call venta.AdjuntarImagen.
func (s *Service) processAndStoreImagen(ctx context.Context, in ImagenUploadInput) (processedImagen, error) {
	kind, err := domain.ParseStorageKind(in.StorageKind)
	if err != nil {
		return processedImagen{}, err
	}
	storageVO, err := domain.NewImagenStorage(kind, in.StorageKey)
	if err != nil {
		return processedImagen{}, err
	}
	out, err := s.imageProc.Process(ctx, outbound.ImageProcessorInput{
		Body:        in.Body,
		ContentType: in.Mime,
		SizeBytes:   in.SizeBytes,
	})
	if err != nil {
		return processedImagen{}, mapImageProcessorError(err)
	}
	if err := s.storage.Store(ctx, storageVO.Key(), out.ContentType, out.SizeBytes, out.Body); err != nil {
		return processedImagen{}, err
	}
	return processedImagen{
		ImagenID:    in.ImagenID,
		Storage:     storageVO,
		Mime:        out.ContentType,
		SizeBytes:   out.SizeBytes,
		Descripcion: in.Descripcion,
	}, nil
}

// attachAllImagenesToVenta calls venta.AdjuntarImagen for each processed
// upload. Returns the first domain error if any imagen is rejected (e.g.
// descripcion too long, mime out of whitelist).
func attachAllImagenesToVenta(
	venta *domain.Venta, imgs []processedImagen, by uuid.UUID, now time.Time,
) error {
	for _, p := range imgs {
		if _, err := venta.AdjuntarImagen(domain.AdjuntarImagenParams{
			ID:          p.ImagenID,
			Storage:     p.Storage,
			Mime:        p.Mime,
			SizeBytes:   p.SizeBytes,
			Descripcion: p.Descripcion,
			By:          by,
			Now:         now,
		}); err != nil {
			return err
		}
	}
	return nil
}

// cleanupBlobs best-effort deletes each blob whose key is in keys. Used on
// any error path after [Service.storeAllImagenBlobs] has run.
func (s *Service) cleanupBlobs(ctx context.Context, keys []string) {
	for _, k := range keys {
		if err := s.storage.Delete(ctx, k); err != nil {
			slog.WarnContext(ctx, "ventas.storage_rollback_failed",
				"storage_key", k,
				"error", err,
			)
		}
	}
}

// detectDuplicateImagenIDs rejects a request that includes the same imagen
// UUID more than once. Caller error; surfaced as a validation apperror so
// the client knows to deduplicate before retrying.
func detectDuplicateImagenIDs(imgs []ImagenUploadInput) error {
	if len(imgs) < 2 {
		return nil
	}
	seen := make(map[uuid.UUID]struct{}, len(imgs))
	for i := range imgs {
		id := imgs[i].ImagenID
		if id == uuid.Nil {
			continue
		}
		if _, dup := seen[id]; dup {
			return domain.ErrImagenIDDuplicado
		}
		seen[id] = struct{}{}
	}
	return nil
}
