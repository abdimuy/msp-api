//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// ImagenUploadInput describes a single comprobante to attach inside the
// atomic CrearPagoConImagenes flow. The HTTP layer is responsible for
// generating ImagenID (or honoring a client-provided UUID), building a safe
// StorageKey, and surfacing the raw body as a Reader positioned at start.
type ImagenUploadInput struct {
	ImagenID    uuid.UUID
	StorageKind domain.StorageKind
	StorageKey  string
	Mime        string
	SizeBytes   int64
	Descripcion *string
	Body        io.Reader
}

// errIdempotentReplay is an internal sentinel that signals the tx closure
// should be rolled back because the pago already exists. It never escapes
// CrearPagoConImagenes — callers see either the existing pago or the original
// error.
var errIdempotentReplay = errors.New("cobranza: idempotent replay")

// CrearPagoConImagenes persists a pago + N comprobantes atomically. If any
// step fails (blob processing, storage write, repo insert, tx commit), nothing
// is left behind: blobs already written are best-effort deleted, the pago row
// is rolled back, and the original error is propagated.
//
// imgs may be nil/empty — in that case the behavior is identical to
// CrearPago: no transaction is required, no blobs are written, and the
// best-effort fast-path AplicarPago runs after insert.
//
// Image idempotency: the caller chooses each ImagenID. If the client wants
// strong replay safety, it should send stable UUIDs (one per image) so this
// method's idempotency fast-path detects them on retry. Without stable IDs,
// reintentos will duplicate images.
//
// Pago idempotency: same UUID twice → second call returns the existing pago
// without rewriting blobs.
func (s *Service) CrearPagoConImagenes(
	ctx context.Context,
	in CrearPagoInput,
	imgs []ImagenUploadInput,
	by uuid.UUID,
) (*domain.PagoRecibido, error) {
	if s.pagosRecibidos == nil {
		return nil, errWriteDepsMissing("pagos_recibidos_repo")
	}
	if len(imgs) > 0 {
		if s.pagosImagenes == nil {
			return nil, errWriteDepsMissing("pagos_imagenes_repo")
		}
		if s.storage == nil {
			return nil, errWriteDepsMissing("storage_provider")
		}
		if s.txMgr == nil {
			return nil, errWriteDepsMissing("tx_manager")
		}
	}

	now := s.clock.Now()
	if err := validateFechaHoraPago(now, in.FechaHoraPago); err != nil {
		return nil, err
	}
	if err := s.validateCargo(ctx, in.CargoDoctoCCID, in.Importe); err != nil {
		return nil, err
	}

	// Detect duplicate imagen IDs within the same request — caller error.
	if err := detectDuplicateImagenIDs(imgs); err != nil {
		return nil, err
	}

	pago, err := domain.NewPagoRecibido(domain.CrearPagoRecibidoParams{
		ID:             in.ID,
		CargoDoctoCCID: in.CargoDoctoCCID,
		ClienteID:      in.ClienteID,
		CobradorID:     in.CobradorID,
		Cobrador:       in.Cobrador,
		Importe:        in.Importe,
		FormaCobroID:   in.FormaCobroID,
		FechaHoraPago:  in.FechaHoraPago,
		Lat:            in.Lat,
		Lon:            in.Lon,
		CreatedBy:      by,
		Now:            now,
	})
	if err != nil {
		return nil, err
	}

	// No images → preserve the legacy non-tx fast-path verbatim.
	if len(imgs) == 0 {
		return s.insertAndApplyPago(ctx, pago, in.ID, by)
	}

	processed, storedKeys, perr := s.storeAllBlobs(ctx, imgs)
	if perr != nil {
		s.cleanupBlobs(ctx, storedKeys)
		return nil, perr
	}

	existing, txErr := s.persistPagoConImagenesTx(ctx, pago, processed, in.ID, by, now)
	switch {
	case errors.Is(txErr, errIdempotentReplay):
		s.cleanupBlobs(ctx, storedKeys)
		return existing, nil
	case txErr != nil:
		s.cleanupBlobs(ctx, storedKeys)
		return nil, txErr
	}

	// Post-tx: best-effort apply. Mirrors CrearPago — writer errors do not
	// fail the response; the retry worker handles ESTADO='P' rows later.
	return s.tryApplyAfterCreate(ctx, pago, by), nil
}

// storeAllBlobs processes and persists every blob BEFORE the tx opens.
// Firebird transactions cannot enclose filesystem writes; the cost of this
// ordering is compensated via [Service.cleanupBlobs] when the tx rolls back.
// Returns the materialized imagenes ready to attach + the storage keys that
// were written (for cleanup if any later step fails).
func (s *Service) storeAllBlobs(
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

// cleanupBlobs best-effort deletes every blob whose key is in keys. Used on
// any error path after [Service.storeAllBlobs] has run.
func (s *Service) cleanupBlobs(ctx context.Context, keys []string) {
	for _, k := range keys {
		s.bestEffortDeleteBlob(ctx, k)
	}
}

// persistPagoConImagenesTx runs the atomic Insert(pago) + InsertImagen(img)*N
// closure. On ErrPagoYaExiste it loads + returns the existing pago and
// signals the caller via errIdempotentReplay so the tx rolls back without
// leaving partial state.
func (s *Service) persistPagoConImagenesTx(
	ctx context.Context,
	pago *domain.PagoRecibido,
	imgs []processedImagen,
	pagoID uuid.UUID,
	by uuid.UUID,
	now time.Time,
) (*domain.PagoRecibido, error) {
	var existing *domain.PagoRecibido
	err := s.runInTx(ctx, func(ctx context.Context) error {
		if err := s.pagosRecibidos.Insert(ctx, pago); err != nil {
			if errors.Is(err, domain.ErrPagoYaExiste) {
				found, findErr := s.pagosRecibidos.FindByID(ctx, pagoID)
				if findErr != nil {
					return findErr
				}
				existing = found
				return errIdempotentReplay
			}
			return err
		}
		return s.attachAllImagenes(ctx, pago, imgs, by, now)
	})
	return existing, err
}

// attachAllImagenes calls pago.AdjuntarImagen + InsertImagen for every
// processedImagen, inside the surrounding tx. Returning any error rolls back
// the whole atomic write.
func (s *Service) attachAllImagenes(
	ctx context.Context, pago *domain.PagoRecibido, imgs []processedImagen, by uuid.UUID, now time.Time,
) error {
	for _, p := range imgs {
		img, err := pago.AdjuntarImagen(domain.AdjuntarImagenParams{
			ID:          p.ImagenID,
			Storage:     p.Storage,
			Mime:        p.Mime,
			SizeBytes:   p.SizeBytes,
			Descripcion: p.Descripcion,
			By:          by,
			Now:         now,
		})
		if err != nil {
			return err
		}
		if err := s.pagosImagenes.InsertImagen(ctx, pago.ID(), img); err != nil {
			return err
		}
	}
	return nil
}

// processedImagen captures the materialized form of an upload after the
// imageprocessor / storage step has run, ready to be attached + persisted
// inside the tx closure.
type processedImagen struct {
	ImagenID    uuid.UUID
	Storage     domain.ImagenStorage
	Mime        string
	SizeBytes   int64
	Descripcion *string
}

// processAndStoreImagen validates the storage key, runs the upload through
// the image processor (or PDF short-circuit), and persists the blob. Returns
// the materialized values needed to call domain.PagoRecibido.AdjuntarImagen.
func (s *Service) processAndStoreImagen(ctx context.Context, in ImagenUploadInput) (processedImagen, error) {
	storage, err := domain.NewImagenStorage(in.StorageKind, in.StorageKey)
	if err != nil {
		return processedImagen{}, err
	}
	mime, sizeBytes, body, err := s.processBlob(ctx, AdjuntarImagenPagoInput{
		Mime:      in.Mime,
		SizeBytes: in.SizeBytes,
		Body:      in.Body,
	})
	if err != nil {
		return processedImagen{}, err
	}
	if err := s.storage.Store(ctx, storage.Key(), mime, sizeBytes, body); err != nil {
		return processedImagen{}, err
	}
	return processedImagen{
		ImagenID:    in.ImagenID,
		Storage:     storage,
		Mime:        mime,
		SizeBytes:   sizeBytes,
		Descripcion: in.Descripcion,
	}, nil
}

// insertAndApplyPago is the legacy non-tx path: Insert + best-effort apply.
// Factored out of CrearPago so CrearPagoConImagenes can reuse it when imgs
// is empty without duplicating the idempotency / fast-path logic.
func (s *Service) insertAndApplyPago(
	ctx context.Context, pago *domain.PagoRecibido, pagoID, by uuid.UUID,
) (*domain.PagoRecibido, error) {
	if err := s.pagosRecibidos.Insert(ctx, pago); err != nil {
		if errors.Is(err, domain.ErrPagoYaExiste) {
			existing, findErr := s.pagosRecibidos.FindByID(ctx, pagoID)
			if findErr != nil {
				return nil, findErr
			}
			return existing, nil
		}
		return nil, err
	}
	return s.tryApplyAfterCreate(ctx, pago, by), nil
}

// tryApplyAfterCreate runs the best-effort fast-path AplicarPago and returns
// whatever state the pago ends up in. Writer errors do not propagate — the
// retry worker handles ESTADO='P' rows. Returns the freshest pago snapshot
// available (apply result on success, reload on apply-failure, original pago
// if reload also fails).
func (s *Service) tryApplyAfterCreate(
	ctx context.Context, pago *domain.PagoRecibido, by uuid.UUID,
) *domain.PagoRecibido {
	applied, applyErr := s.AplicarPago(ctx, pago.ID(), by)
	if applyErr != nil {
		slog.WarnContext(ctx, "pago.apply_fast_path_failed",
			slog.String("pago_id", pago.ID().String()),
			slog.String("error", applyErr.Error()),
		)
		reloaded, findErr := s.pagosRecibidos.FindByID(ctx, pago.ID())
		if findErr != nil {
			return pago
		}
		return reloaded
	}
	return applied
}

// detectDuplicateImagenIDs rejects a request that includes the same imagen
// UUID more than once. Caller error; surfaced as a validation error so the
// client knows to deduplicate before retrying.
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
