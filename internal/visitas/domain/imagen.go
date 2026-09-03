//nolint:misspell // domain vocabulary is Spanish (descripcion, imagen) per project convention.
package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

// maxImagenDescripcionLength is the rune width of
// MSP_VISITAS_IMAGENES.DESCRIPCION.
const maxImagenDescripcionLength = 200

// Imagen is a photo the cobrador attached to a visita — the door, the client,
// a handwritten receipt. It is a child entity of the Visita aggregate root:
// new images enter only through [Visita.AdjuntarImagen], so the MIME
// whitelist and the storage-key rules cannot be bypassed by constructing one
// directly from outside the package.
//
// Unlike its parent, Imagen DOES persist its audit subrecord:
// MSP_VISITAS_IMAGENES is a greenfield MSP_* table with the full audit block
// (see migration 000063), whereas MSP_VISITAS is legacy and has no audit
// columns at all (see the Visita doc comment).
type Imagen struct {
	id          uuid.UUID
	storage     ImagenStorage
	mime        string
	sizeBytes   int64
	descripcion *string
	audit       audit.Auditable
}

// AdjuntarImagenParams carries the inputs to [Visita.AdjuntarImagen].
//
// SizeBytes has no upper bound here on purpose. The only ceiling on an upload
// is the global HTTP BodyLimit (cfg.HTTP.MaxBodySizeMB, 10 MB by default) for
// the whole request; there is no per-image cap anywhere in this codebase and
// this module does not invent one. A per-image cap would fail the ENTIRE
// atomic request — and with it the visita — over one oversized photo, which
// is exactly the outcome the single-request design exists to prevent.
type AdjuntarImagenParams struct {
	ID          uuid.UUID
	Storage     ImagenStorage
	Mime        string
	SizeBytes   int64
	Descripcion *string
	By          uuid.UUID
	Now         time.Time
}

// AdjuntarImagen validates the upload and appends it to the visita's image
// collection, returning the new child entity so the caller can persist it in
// the same transaction as the parent row.
func (v *Visita) AdjuntarImagen(p AdjuntarImagenParams) (*Imagen, error) {
	img, err := newImagen(p)
	if err != nil {
		return nil, err
	}
	v.imagenes = append(v.imagenes, img)
	return img, nil
}

// Imagenes returns the images attached to this visita, in attachment order.
// The slice is a copy: callers cannot append to or reorder the aggregate's
// collection from outside.
func (v *Visita) Imagenes() []*Imagen {
	if len(v.imagenes) == 0 {
		return nil
	}
	out := make([]*Imagen, len(v.imagenes))
	copy(out, v.imagenes)
	return out
}

// newImagen validates and constructs an Imagen. Package-private: the only way
// in is through the aggregate root.
func newImagen(p AdjuntarImagenParams) (*Imagen, error) {
	if p.ID == uuid.Nil {
		return nil, ErrImagenIDRequerido
	}
	if !IsAllowedMime(strings.ToLower(p.Mime)) {
		return nil, ErrImagenMimeNoPermitido
	}
	if p.SizeBytes < 0 {
		return nil, ErrImagenSizeBytesNegativo
	}
	if p.Storage.Key() == "" || !p.Storage.Kind().IsValid() {
		return nil, ErrImagenStorageKeyInvalida
	}
	desc, err := trimOptionalBoundedPtr(p.Descripcion, maxImagenDescripcionLength, ErrImagenDescripcionDemasiadoLarga)
	if err != nil {
		return nil, err
	}
	return &Imagen{
		id:          p.ID,
		storage:     p.Storage,
		mime:        strings.ToLower(p.Mime),
		sizeBytes:   p.SizeBytes,
		descripcion: desc,
		audit:       audit.NewAuditable(p.Now, p.By),
	}, nil
}

// HydrateImagenParams carries the persisted shape of an Imagen.
type HydrateImagenParams struct {
	ID          uuid.UUID
	Storage     ImagenStorage
	Mime        string
	SizeBytes   int64
	Descripcion *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CreatedBy   uuid.UUID
	UpdatedBy   uuid.UUID
}

// HydrateImagen rebuilds an Imagen from persistence WITHOUT validation. Same
// rule as RehydrateVisita: the read path trusts what was written and never
// rejects a stored row, so a MIME that later leaves the whitelist still loads.
func HydrateImagen(p HydrateImagenParams) *Imagen {
	return &Imagen{
		id:          p.ID,
		storage:     p.Storage,
		mime:        p.Mime,
		sizeBytes:   p.SizeBytes,
		descripcion: p.Descripcion,
		audit:       audit.HydrateAuditable(p.CreatedAt, p.UpdatedAt, p.CreatedBy, p.UpdatedBy),
	}
}

// ID returns the imagen's primary key. It is the client-supplied UUID when
// the phone sent one, which is what makes a retry idempotent per image.
func (i *Imagen) ID() uuid.UUID { return i.id }

// Storage returns where the blob lives.
func (i *Imagen) Storage() ImagenStorage { return i.storage }

// Mime returns the lower-cased MIME type.
func (i *Imagen) Mime() string { return i.mime }

// SizeBytes returns the blob size in bytes.
func (i *Imagen) SizeBytes() int64 { return i.sizeBytes }

// Descripcion returns the optional per-image note, or nil when absent.
func (i *Imagen) Descripcion() *string { return i.descripcion }

// Audit returns a copy of the imagen's audit subrecord. Unlike the parent
// Visita's, this one IS persisted.
func (i *Imagen) Audit() audit.Auditable { return i.audit }
