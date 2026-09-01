//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app

import (
	"context"
	"errors"
	"io"
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

// CrearPagoConImagenes persists a pago + N comprobantes AND pushes the pago
// to Microsip, all inside one transaction. If any step fails — blob
// processing, storage write, repo insert, imagen insert, the Microsip writer,
// or the commit — nothing is left behind: blobs already written are
// best-effort deleted, the pago row is rolled back, and the original error is
// propagated to the caller.
//
// The Microsip writer being INSIDE the transaction is the whole point. It
// used to run after the commit with its error logged and swallowed, so a pago
// Microsip had rejected survived as an ESTADO='P' row behind a 2xx: a queue
// nobody looks at, a cobrador who believes the payment did not register, and
// a client charged twice. Now the rejection propagates and the row is gone —
// the evidence lives in MSP_FAILED_INTENTS instead, one table rather than
// two.
//
// imgs may be nil/empty — that is the path the phone takes, since pagos are
// captured without a photo. It goes through the exact same transaction; the
// only difference is that no blob is written and no imagen row is inserted.
//
// Image idempotency: the caller chooses each ImagenID. If the client wants
// strong replay safety, it should send stable UUIDs (one per image) so this
// method's idempotency fast-path detects them on retry. Without stable IDs,
// reintentos will duplicate images.
//
// Pago idempotency: same UUID twice → second call returns the existing pago
// without rewriting blobs. A pago rolled back by a Microsip rejection never
// reached the table, so its UUID is NOT burned and the phone's retry works.
func (s *Service) CrearPagoConImagenes(
	ctx context.Context,
	in CrearPagoInput,
	imgs []ImagenUploadInput,
	by uuid.UUID,
) (*domain.PagoRecibido, error) {
	if s.pagosRecibidos == nil {
		return nil, errWriteDepsMissing("pagos_recibidos_repo")
	}
	// The writer and the transaction are no longer optional on any path: the
	// pago is created and pushed to Microsip as a single act, so without them
	// there is no creation to speak of.
	if s.microsipPago == nil {
		return nil, errWriteDepsMissing("microsip_pago_writer")
	}
	if s.txMgr == nil {
		return nil, errWriteDepsMissing("tx_manager")
	}
	if len(imgs) > 0 {
		if s.pagosImagenes == nil {
			return nil, errWriteDepsMissing("pagos_imagenes_repo")
		}
		if s.storage == nil {
			return nil, errWriteDepsMissing("storage_provider")
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

	// With imgs empty this is a no-op that touches neither the processor nor
	// the storage provider, so the no-images path shares the code verbatim
	// instead of forking into a second, untested flow.
	processed, storedKeys, perr := s.storeAllBlobs(ctx, imgs)
	if perr != nil {
		s.cleanupBlobs(ctx, storedKeys)
		return nil, perr
	}

	existing, txErr := s.persistPagoTx(ctx, pago, processed, in.ID, by, now)
	switch {
	case errors.Is(txErr, errIdempotentReplay):
		s.cleanupBlobs(ctx, storedKeys)
		return existing, nil
	case txErr != nil:
		s.cleanupBlobs(ctx, storedKeys)
		return nil, txErr
	}
	return pago, nil
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

// persistPagoTx runs the atomic Insert(pago) + InsertImagen(img)*N + push to
// Microsip closure. On ErrPagoYaExiste it loads + returns the existing pago
// and signals the caller via errIdempotentReplay so the tx rolls back without
// leaving partial state.
//
// Order matters: the Microsip writer goes LAST. Everything it can conflict
// with is already written, and the two rows it shares with Cxc.exe
// (SALDOS_CC and MSP_SALDOS_VENTAS) stay locked for the shortest possible
// slice of the transaction.
func (s *Service) persistPagoTx(
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
		if err := s.attachAllImagenes(ctx, pago, imgs, by, now); err != nil {
			return err
		}
		return s.aplicarPagoRecienCreado(ctx, pago, by)
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
