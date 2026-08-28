// Package outbound declares what the flota module needs from the outside: the
// roster it photographs, the tables it writes, a transaction boundary and a
// clock. Nothing here names Firestore or Firebird — the adapters do.
package outbound

import "context"

// RosterUsuario is one raw roster entry, before the domain normalises it.
//
// Camioneta is a *int rather than an int because "no camioneta" has to be
// distinguishable from "camioneta zero", and because the source genuinely
// produces the absence in three different shapes. Measured against the dev
// Firestore project on 2026-08-28, over 62 documents in `users`: 35 carry an
// integer CAMIONETA_ASIGNADA, 26 have no such field at all, and 1 has the
// field present with an explicit null. All three of the last two shapes mean
// the same thing and arrive here as nil.
type RosterUsuario struct {
	// UID is the roster's own identifier for the person — the Firestore
	// document id. Required; an entry without one is rejected by the domain.
	UID string
	// Email is the address as the roster holds it. Normalised downstream.
	Email string
	// Nombre is the display name. May be empty.
	Nombre string
	// Camioneta is the assigned almacén id, or nil when the roster assigns
	// none. Adapters must map every unusable value — absent field, null,
	// wrong type, non-positive number — to nil rather than to an error.
	Camioneta *int
}

// RosterReader photographs the whole roster in one call.
//
// The read is deliberately unfiltered: it returns everybody, not only the
// people who currently have a camioneta. Filtering server-side would be
// cheaper — 35 documents instead of 62 — but it would make the two absences
// indistinguishable, because somebody whose camioneta was taken away and
// somebody whose document was deleted both simply stop matching the filter.
// Telling those apart is a requirement, so the module pays for the extra 27
// reads per pass.
type RosterReader interface {
	// LeerRoster returns every entry in the roster. An error means the
	// snapshot does not happen at all: the caller writes nothing, which is
	// the only safe reading of "I could not see the roster".
	LeerRoster(ctx context.Context) ([]RosterUsuario, error)
}
