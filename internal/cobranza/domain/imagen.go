//nolint:misspell // domain vocabulary is Spanish (descripcion, etc.) per project convention.
package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

// maxImagenDescripcionLength is the byte width of the descripcion column.
const maxImagenDescripcionLength = 200

// Imagen is a comprobante (recibo de transferencia, foto de cheque, etc.)
// adjunto a un pago. It is a child entity of the PagoRecibido aggregate root;
// new images are added via PagoRecibido.AdjuntarImagen and removed via
// PagoRecibido.EliminarImagen.
type Imagen struct {
	id          uuid.UUID
	storage     ImagenStorage
	mime        string
	sizeBytes   int64
	descripcion *string
	audit       audit.Auditable
}

// NewImagenParams carries the inputs to newImagen.
type NewImagenParams struct {
	ID          uuid.UUID
	Storage     ImagenStorage
	Mime        string
	SizeBytes   int64
	Descripcion *string
	CreatedBy   uuid.UUID
	Now         time.Time
}

// newImagen validates and constructs an Imagen. Package-private.
func newImagen(p NewImagenParams) (*Imagen, error) {
	if !IsAllowedMime(p.Mime) {
		return nil, ErrMimeNoPermitido
	}
	if p.SizeBytes < 0 {
		return nil, ErrSizeBytesNegativo
	}
	desc, err := trimOptionalBounded(p.Descripcion, maxImagenDescripcionLength, ErrImagenDescripcionDemasiadoLarga)
	if err != nil {
		return nil, err
	}
	return &Imagen{
		id:          p.ID,
		storage:     p.Storage,
		mime:        strings.ToLower(p.Mime),
		sizeBytes:   p.SizeBytes,
		descripcion: desc,
		audit:       audit.NewAuditable(p.Now, p.CreatedBy),
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

// HydrateImagen rebuilds an Imagen from persistence without validation.
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

// ID returns the imagen's primary key.
func (i *Imagen) ID() uuid.UUID { return i.id }

// Storage returns the imagen's storage location.
func (i *Imagen) Storage() ImagenStorage { return i.storage }

// Mime returns the imagen's mime type.
func (i *Imagen) Mime() string { return i.mime }

// SizeBytes returns the imagen's size in bytes.
func (i *Imagen) SizeBytes() int64 { return i.sizeBytes }

// Descripcion returns the optional descripcion text.
func (i *Imagen) Descripcion() *string { return i.descripcion }

// Audit returns a copy of the imagen's audit subrecord.
func (i *Imagen) Audit() audit.Auditable { return i.audit }
