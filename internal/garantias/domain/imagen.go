//nolint:misspell // Spanish vocabulary (Descripcion) per project convention.
package domain

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Imagen is the evidence attached to a timeline event. It hangs from the
// EVENTO and not from the folio: the photo is the proof of the state the item
// was in when somebody picked it up, and that is a property of the event
// (spec §3.4).
//
// Like Evento it is immutable — it has no setters and no audit embed, because
// the table has no UPDATED_AT: a piece of evidence is not edited, it is
// replaced by a new row. The blob itself is NOT here; RUTA is the relative
// path of the file under STORAGE_DIR (ADR-0003, local filesystem only).
type Imagen struct {
	id          uuid.UUID
	eventoID    uuid.UUID
	ruta        string
	descripcion string
	subidaPor   string
	createdAt   time.Time
}

// NewImagenParams carries the inputs to NewImagen. The id is generated here
// with uuid.New() and the timestamp comes from the service's Clock: the
// database is a dummy store and never invents an identity or a date
// (CLAUDE.md §1).
type NewImagenParams struct {
	EventoID    uuid.UUID
	Ruta        string
	Descripcion string
	SubidaPor   string
	CreatedAt   time.Time
}

// HydrateImagenParams is the persisted shape used to rebuild evidence over
// Firebird.
type HydrateImagenParams struct {
	ID          uuid.UUID
	EventoID    uuid.UUID
	Ruta        string
	Descripcion string
	SubidaPor   string
	CreatedAt   time.Time
}

// NewImagen creates the evidence row for an event. Ruta and SubidaPor are
// mandatory: a row without them says nothing, and the table has no nullable
// excuse for it.
func NewImagen(p NewImagenParams) (*Imagen, error) {
	ruta := strings.TrimSpace(p.Ruta)
	if !rutaRelativa(ruta) {
		return nil, ErrImagenRutaInvalida
	}
	subidaPor := strings.TrimSpace(p.SubidaPor)
	if subidaPor == "" {
		return nil, ErrImagenSubidaPorObligatorio
	}
	return &Imagen{
		id:          uuid.New(),
		eventoID:    p.EventoID,
		ruta:        ruta,
		descripcion: strings.TrimSpace(p.Descripcion),
		subidaPor:   subidaPor,
		createdAt:   p.CreatedAt,
	}, nil
}

// HydrateImagen rebuilds an Imagen from a persisted row without any
// validation; the repository only calls it with rows it already wrote.
func HydrateImagen(p HydrateImagenParams) *Imagen {
	return &Imagen{
		id:          p.ID,
		eventoID:    p.EventoID,
		ruta:        p.Ruta,
		descripcion: p.Descripcion,
		subidaPor:   p.SubidaPor,
		createdAt:   p.CreatedAt,
	}
}

// rutaRelativa reports whether ruta stays inside STORAGE_DIR: no leading
// slash, no Windows drive and no ".." segment, under either separator. The
// check is textual on purpose — path/filepath answers differently on the
// Windows server and on a Linux box, and a path accepted on one and rejected
// on the other is worse than no check at all. StorageProvider rejects
// traversal too, but the domain does not take the adapter's word for it.
func rutaRelativa(ruta string) bool {
	if ruta == "" {
		return false
	}
	norm := strings.ReplaceAll(ruta, `\`, "/")
	if strings.HasPrefix(norm, "/") || (len(norm) >= 2 && norm[1] == ':') {
		return false
	}
	return !slices.Contains(strings.Split(norm, "/"), "..")
}

// ID returns the image's primary key.
func (i *Imagen) ID() uuid.UUID { return i.id }

// EventoID returns the event the evidence hangs from.
func (i *Imagen) EventoID() uuid.UUID { return i.eventoID }

// Ruta returns the blob path relative to STORAGE_DIR.
func (i *Imagen) Ruta() string { return i.ruta }

// Descripcion returns the free-form caption, possibly empty.
func (i *Imagen) Descripcion() string { return i.descripcion }

// SubidaPor returns the operator who uploaded the evidence.
func (i *Imagen) SubidaPor() string { return i.subidaPor }

// CreatedAt returns when the evidence was registered (UTC).
func (i *Imagen) CreatedAt() time.Time { return i.createdAt }
