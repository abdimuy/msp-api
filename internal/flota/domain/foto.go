package domain

import (
	"time"

	"github.com/google/uuid"
)

// Foto is the record that one snapshot ran. One row per successful pass,
// whether or not anything changed.
//
// It carries the module's weight in two places:
//
//  1. It is where the next run reads VentanaDesde from. The lower bound of a
//     change has to be "when we last looked", not "when we last saw this
//     person in this state" — the latter could be weeks old and would turn a
//     fifteen-minute window into a useless one.
//
//  2. It is the positive control. Without it, "nothing changed this month"
//     and "the worker has been dead for a month" produce the identical empty
//     table, and an absence would get read as a finding. With it, the absence
//     is checkable: the snapshots kept coming, so the query would have found
//     the row had there been one.
type Foto struct {
	id                 uuid.UUID
	ejecutadoEn        time.Time
	usuariosObservados int
	cambiosDetectados  int
	esLineaBase        bool
	createdAt          time.Time
}

// NuevaFoto records one completed pass. esLineaBase marks the very first run,
// the one that only photographs the starting state and emits no changes
// because it has nothing to compare against.
func NuevaFoto(ejecutadoEn time.Time, usuariosObservados, cambiosDetectados int, esLineaBase bool) *Foto {
	return &Foto{
		id:                 uuid.New(),
		ejecutadoEn:        ejecutadoEn,
		usuariosObservados: usuariosObservados,
		cambiosDetectados:  cambiosDetectados,
		esLineaBase:        esLineaBase,
		createdAt:          ejecutadoEn,
	}
}

// ID returns the row's UUID, generated in Go (CLAUDE.md §1).
func (f *Foto) ID() uuid.UUID { return f.id }

// EjecutadoEn returns when the snapshot was taken.
func (f *Foto) EjecutadoEn() time.Time { return f.ejecutadoEn }

// UsuariosObservados returns how many roster entries the snapshot saw. A
// sudden drop here is the cheapest smoke detector the module has.
func (f *Foto) UsuariosObservados() int { return f.usuariosObservados }

// CambiosDetectados returns how many history rows this pass produced.
func (f *Foto) CambiosDetectados() int { return f.cambiosDetectados }

// EsLineaBase reports whether this was the first-ever snapshot.
func (f *Foto) EsLineaBase() bool { return f.esLineaBase }

// CreatedAt returns when the row was produced. Equal to EjecutadoEn by
// construction.
func (f *Foto) CreatedAt() time.Time { return f.createdAt }
