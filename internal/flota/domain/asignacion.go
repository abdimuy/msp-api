package domain

import (
	"time"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

// Asignacion is the last observed state of one person in the roster — the
// "before" the next snapshot is compared against.
//
// It is NOT a catalogue and NOT a source of truth: Firestore remains the
// authority on who rides what. This is a mirror kept only so a later
// photograph has something to differ from.
//
// Type A entity by the project's taxonomy, but it embeds audit.Timestamped
// rather than audit.Auditable on purpose: there is no user behind these
// writes. Nobody in this system created the row — a background worker noticed
// a state. Recording a CREATED_BY would invent an actor, which is precisely
// the kind of false precision this module is built to avoid.
type Asignacion struct {
	usuarioUID string
	email      string
	nombre     string
	camioneta  Camioneta
	ts         audit.Timestamped
}

// HidratarAsignacionParams carries the persisted columns back into an entity.
type HidratarAsignacionParams struct {
	UsuarioUID string
	Email      string
	Nombre     string
	Camioneta  Camioneta
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NuevaAsignacion records a person seen for the first time. The Observacion
// was already validated and normalised by NuevaObservacion, so there is
// nothing left to reject here.
func NuevaAsignacion(o Observacion, ahora time.Time) *Asignacion {
	return &Asignacion{
		usuarioUID: o.usuarioUID,
		email:      o.email,
		nombre:     o.nombre,
		camioneta:  o.camioneta,
		ts:         audit.NewTimestamped(ahora),
	}
}

// HidratarAsignacion rebuilds an Asignacion from persistence without
// validation — the write path already validated these values.
func HidratarAsignacion(p HidratarAsignacionParams) *Asignacion {
	return &Asignacion{
		usuarioUID: p.UsuarioUID,
		email:      p.Email,
		nombre:     p.Nombre,
		camioneta:  p.Camioneta,
		ts:         audit.HydrateTimestamped(p.CreatedAt, p.UpdatedAt),
	}
}

// UsuarioUID returns the Firestore document id this row mirrors.
func (a *Asignacion) UsuarioUID() string { return a.usuarioUID }

// Email returns the last observed canonical address.
func (a *Asignacion) Email() string { return a.email }

// Nombre returns the last observed display name.
func (a *Asignacion) Nombre() string { return a.nombre }

// Camioneta returns the last observed assignment.
func (a *Asignacion) Camioneta() Camioneta { return a.camioneta }

// Audit exposes the created/updated stamps for the repository.
func (a *Asignacion) Audit() *audit.Timestamped { return &a.ts }

// CoincideCon reports whether the stored state is byte-for-byte what the
// roster now shows. When it is, the snapshot writes nothing for this person —
// which is what keeps an unchanged roster from touching the database at all.
func (a *Asignacion) CoincideCon(o Observacion) bool {
	return a.email == o.email &&
		a.nombre == o.nombre &&
		a.camioneta.Igual(o.camioneta)
}

// Observar overwrites the stored state with what the roster now shows and
// stamps the moment of observation.
//
// Callers that need the previous camioneta — the comparison does, to decide
// what kind of change happened — must read it BEFORE calling this.
func (a *Asignacion) Observar(o Observacion, ahora time.Time) {
	a.email = o.email
	a.nombre = o.nombre
	a.camioneta = o.camioneta
	a.ts.MarkUpdatedAt(ahora)
}
