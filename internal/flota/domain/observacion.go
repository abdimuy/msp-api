package domain

import "strings"

// Observacion is one person exactly as the roster showed them in a single
// snapshot. It is the unit the comparison consumes.
//
// It exists as a domain type — rather than the comparison taking the port's
// struct directly — because normalisation has to happen in exactly one place.
// If a trailing space or a capitalised address slipped through, two snapshots
// of an unchanged roster would look different and the module would report
// churn that never happened.
type Observacion struct {
	usuarioUID string
	email      string
	nombre     string
	camioneta  Camioneta
}

// NuevaObservacion validates and normalises one roster entry.
//
// usuarioUID is required (it is the Firestore document id — without it there
// is no identity to attach history to). email is lowercased because it is an
// identifier, not prose; nombre only gets its surrounding whitespace trimmed
// because its casing is how the roster chose to display the person and that
// is what a history row should preserve.
func NuevaObservacion(usuarioUID, email, nombre string, camioneta Camioneta) (Observacion, error) {
	uid := strings.TrimSpace(usuarioUID)
	if uid == "" {
		return Observacion{}, ErrUsuarioUIDRequerido
	}
	return Observacion{
		usuarioUID: uid,
		email:      strings.ToLower(strings.TrimSpace(email)),
		nombre:     strings.TrimSpace(nombre),
		camioneta:  camioneta,
	}, nil
}

// UsuarioUID returns the Firestore document id of the observed person.
func (o Observacion) UsuarioUID() string { return o.usuarioUID }

// Email returns the canonical (trimmed, lowercased) address, possibly empty.
func (o Observacion) Email() string { return o.email }

// Nombre returns the display name as the roster holds it, possibly empty.
func (o Observacion) Nombre() string { return o.nombre }

// Camioneta returns the observed assignment.
func (o Observacion) Camioneta() Camioneta { return o.camioneta }
