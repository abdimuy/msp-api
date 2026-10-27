//nolint:misspell // visitas vocabulary is Spanish (imagen, descripcion) per project convention.
package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

// ImagenUploadInput describes one photo arriving inside the atomic
// RegistrarVisitaConImagenes flow.
//
// The HTTP layer owns ImagenID (honoring the client's `id_<n>` when present)
// and StorageKey (built as "visitas/<visita_id>/<imagen_id><ext>"); Body is
// the multipart part, already open and positioned at the start. The service
// never re-reads Body, so a caller may stream a part straight through
// without buffering it.
type ImagenUploadInput struct {
	ImagenID    uuid.UUID
	StorageKind domain.StorageKind
	StorageKey  string
	Mime        string
	SizeBytes   int64
	Descripcion *string
	Body        io.Reader
}

// errVisitasImagenesNoConfigurado is returned when a request carries photos
// but the service was built without [WithImagenes]. It is an INTERNAL error,
// not a validation one: the client did nothing wrong, the deployment is
// misconfigured.
var errVisitasImagenesNoConfigurado = apperror.NewInternal(
	"visitas_imagenes_no_configurado",
	"el servidor no está configurado para recibir imágenes de visita",
)

// RegistrarVisitaConImagenes persists a visita and its photos in ONE atomic
// write, and is idempotent on the client-generated visita UUID exactly as
// [Service.RegistrarVisita] is.
//
// With no photos it delegates to RegistrarVisita verbatim: no transaction, no
// storage, no behavior change for the callers that existed before photos did.
//
// # Why the blobs are written before the transaction
//
// A Firebird transaction cannot enclose a filesystem write. So blobs go to
// disk first and the transaction follows; if the transaction fails, every
// blob this call wrote is deleted again. That ordering is why the pre-flight
// read below matters so much — see the next section.
//
// # Idempotency, including the case with a photo
//
// Before writing anything, the service asks what the server already has: does
// this visita exist, and which of these image ids are already attached to it?
// That answer drives everything:
//
//   - Visita exists and every image id is already attached → nothing is
//     written at all. The existing visita is returned. No duplicate row, no
//     blob touched.
//   - Visita exists and some image id is new → only the new photos are stored
//     and attached to the EXISTING visita. A cobrador who retried and this
//     time got the camera to work does not lose the photo.
//   - Visita does not exist → the ordinary path; everything is new.
//
// The pre-flight read is the reason a replay cannot orphan a blob. The
// storage key is deterministic ("visitas/<visita_id>/<imagen_id><ext>"), so
// re-storing an already-attached image would OVERWRITE the live blob — and
// then the rollback-cleanup would DELETE it, leaving a committed row pointing
// at a file that no longer exists. Skipping those uploads up front means the
// cleanup only ever deletes keys that no committed row references.
//
// A concurrent duplicate request can still slip past the pre-flight read; the
// transaction absorbs it. domain.ErrVisitaYaExiste from Insert re-loads the
// existing visita and carries on, and domain.ErrImagenYaExiste from
// InsertImagen means another attempt attached that exact image first, which
// is success, not failure.
func (s *Service) RegistrarVisitaConImagenes(
	ctx context.Context,
	in RegistrarVisitaInput,
	imgs []ImagenUploadInput,
	by uuid.UUID,
) (*domain.Visita, error) {
	if len(imgs) == 0 {
		return s.RegistrarVisita(ctx, in, by)
	}
	if s.imagenes == nil || s.storage == nil || s.tx == nil {
		return nil, errVisitasImagenesNoConfigurado
	}
	if err := detectDuplicateImagenIDs(imgs); err != nil {
		return nil, err
	}

	now := s.clock.Now()
	visita, err := domain.NewVisita(crearVisitaParams(in, by, now))
	if err != nil {
		return nil, err
	}

	existing, pendientes, err := s.planImagenes(ctx, in.ID, imgs)
	if err != nil {
		return nil, err
	}
	if existing != nil && len(pendientes) == 0 {
		// Pure replay: the visita and every one of its photos are already
		// stored. Nothing to write and, crucially, nothing to clean up —
		// but still worth announcing, since the phone retrying here is the
		// phone that never saw the original response.
		s.anunciarVisitaConfirmada()
		return existing, nil
	}

	stored, storedKeys, err := s.storeAllBlobs(ctx, pendientes)
	if err != nil {
		s.cleanupBlobs(ctx, storedKeys)
		return nil, err
	}

	result, err := s.persistVisitaConImagenes(ctx, visita, existing, stored, in.ID, by, now)
	if err != nil {
		s.cleanupBlobs(ctx, storedKeys)
		return nil, err
	}
	// After the transaction has committed, never inside it.
	s.anunciarVisitaConfirmada()
	return result, nil
}

// planImagenes performs the pre-flight read described in
// RegistrarVisitaConImagenes' doc comment. It returns the already-stored
// visita (nil when this is a first attempt) and the subset of uploads that
// are genuinely new.
func (s *Service) planImagenes(
	ctx context.Context, visitaID uuid.UUID, imgs []ImagenUploadInput,
) (*domain.Visita, []ImagenUploadInput, error) {
	existing, err := s.repo.FindByID(ctx, visitaID)
	if err != nil {
		if errors.Is(err, domain.ErrVisitaNoEncontrada) {
			return nil, imgs, nil
		}
		return nil, nil, err
	}

	attached, err := s.imagenes.FindImagenIDs(ctx, visitaID)
	if err != nil {
		return nil, nil, err
	}
	yaEstan := make(map[uuid.UUID]struct{}, len(attached))
	for _, id := range attached {
		yaEstan[id] = struct{}{}
	}

	pendientes := make([]ImagenUploadInput, 0, len(imgs))
	for _, img := range imgs {
		if _, dup := yaEstan[img.ImagenID]; dup {
			continue
		}
		pendientes = append(pendientes, img)
	}
	return existing, pendientes, nil
}

// storedImagen is an upload after its blob has landed on disk, carrying the
// validated storage VO the domain needs to attach it.
type storedImagen struct {
	ImagenID    uuid.UUID
	Storage     domain.ImagenStorage
	Mime        string
	SizeBytes   int64
	Descripcion *string
}

// storeAllBlobs writes every pending blob to the storage provider, returning
// the materialized uploads plus the keys actually written — the second value
// is what the caller hands to cleanupBlobs on any later failure. On error it
// still returns the keys written so far, so a partial run is fully cleaned.
func (s *Service) storeAllBlobs(
	ctx context.Context, imgs []ImagenUploadInput,
) ([]storedImagen, []string, error) {
	stored := make([]storedImagen, 0, len(imgs))
	keys := make([]string, 0, len(imgs))
	for _, in := range imgs {
		storage, err := domain.NewImagenStorage(in.StorageKind, in.StorageKey)
		if err != nil {
			return stored, keys, err
		}
		// The MIME is checked here as well as in AdjuntarImagen so a
		// disallowed type never reaches the disk at all. Lower-cased first,
		// exactly as the domain does, so the two checks agree on a client
		// that shouts "IMAGE/JPEG".
		mime := strings.ToLower(in.Mime)
		if !domain.IsAllowedMime(mime) {
			return stored, keys, domain.ErrImagenMimeNoPermitido
		}
		if err := s.storage.Store(ctx, storage.Key(), mime, in.SizeBytes, in.Body); err != nil {
			return stored, keys, err
		}
		keys = append(keys, storage.Key())
		stored = append(stored, storedImagen{
			ImagenID:    in.ImagenID,
			Storage:     storage,
			Mime:        mime,
			SizeBytes:   in.SizeBytes,
			Descripcion: in.Descripcion,
		})
	}
	return stored, keys, nil
}

// cleanupBlobs best-effort deletes every key written by this call. Delete is
// idempotent by contract, so a key that never made it to disk is harmless.
// Errors are swallowed on purpose: the caller is already returning a failure
// and a cleanup error must not replace it.
//
// # Why the context is detached
//
// The most common reason to be here is that the request was cancelled or
// timed out — and on THAT context every Delete would return immediately
// without deleting anything, leaving exactly the orphans this function
// exists to prevent. The failure mode was silent: cleanup appearing to run
// while doing nothing. context.WithoutCancel keeps the values (tracing,
// the ambient transaction) and drops the cancellation.
//
// # The window this does NOT close
//
// Blobs are written BEFORE the transaction opens, because a Firebird
// transaction cannot enclose a filesystem write. If the process dies between
// storeAllBlobs and the commit — a panic, a kill, the machine losing power —
// nothing runs this function and the blobs stay on disk with no row pointing
// at them. That window is inherent to writing to two stores without a
// distributed transaction; it cannot be closed here, only swept later.
//
// It is a leak of disk space, never of correctness: an orphan is unreachable
// (nothing references its key) and a retry of the same request overwrites it
// at the same deterministic key rather than adding another. If those bytes
// ever matter, the fix is a janitor that lists STORAGE_DIR/visitas/ and
// deletes keys absent from MSP_VISITAS_IMAGENES — not more logic in this
// path.
func (s *Service) cleanupBlobs(ctx context.Context, keys []string) {
	ctx = context.WithoutCancel(ctx)
	for _, k := range keys {
		_ = s.storage.Delete(ctx, k)
	}
}

// persistVisitaConImagenes runs the atomic write. When existing is non-nil
// the visita row is already there and only the new images are inserted;
// otherwise the visita is inserted first and the images hang off it.
func (s *Service) persistVisitaConImagenes(
	ctx context.Context,
	visita *domain.Visita,
	existing *domain.Visita,
	imgs []storedImagen,
	visitaID uuid.UUID,
	by uuid.UUID,
	now time.Time,
) (*domain.Visita, error) {
	target := existing
	err := s.runInTx(ctx, func(ctx context.Context) error {
		if target == nil {
			inserted, err := s.insertOrLoadVisita(ctx, visita, visitaID)
			if err != nil {
				return err
			}
			target = inserted
		}
		return s.attachAllImagenes(ctx, target, imgs, by, now)
	})
	if err != nil {
		return nil, err
	}
	return target, nil
}

// insertOrLoadVisita inserts the visita, or — when a concurrent identical
// request beat this one to it between the pre-flight read and here — loads
// the row that request committed. Either way the caller gets the aggregate
// the images must hang off.
func (s *Service) insertOrLoadVisita(
	ctx context.Context, visita *domain.Visita, visitaID uuid.UUID,
) (*domain.Visita, error) {
	err := s.repo.Insert(ctx, visita)
	if err == nil {
		return visita, nil
	}
	if !errors.Is(err, domain.ErrVisitaYaExiste) {
		return nil, err
	}
	return s.repo.FindByID(ctx, visitaID)
}

// attachAllImagenes validates each stored upload through the aggregate root
// and persists it inside the surrounding transaction. Any error rolls the
// whole write back.
//
// domain.ErrImagenYaExiste is absorbed: it means a concurrent attempt already
// inserted that exact imagen id, which is the outcome we wanted anyway.
func (s *Service) attachAllImagenes(
	ctx context.Context, v *domain.Visita, imgs []storedImagen, by uuid.UUID, now time.Time,
) error {
	for _, p := range imgs {
		img, err := v.AdjuntarImagen(domain.AdjuntarImagenParams{
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
		if err := s.imagenes.InsertImagen(ctx, v.ID(), img); err != nil {
			if errors.Is(err, domain.ErrImagenYaExiste) {
				continue
			}
			return err
		}
	}
	return nil
}

// detectDuplicateImagenIDs rejects a request carrying the same imagen id
// twice. Collapsing them silently would hide a bug in the uploader, and
// storing them twice would mean two rows pointing at one blob.
func detectDuplicateImagenIDs(imgs []ImagenUploadInput) error {
	if len(imgs) < 2 {
		return nil
	}
	seen := make(map[uuid.UUID]struct{}, len(imgs))
	for _, img := range imgs {
		if img.ImagenID == uuid.Nil {
			continue
		}
		if _, dup := seen[img.ImagenID]; dup {
			return domain.ErrImagenIDDuplicado
		}
		seen[img.ImagenID] = struct{}{}
	}
	return nil
}
