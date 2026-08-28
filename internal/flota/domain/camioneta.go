// Package domain holds the flota module's entities and value objects: who
// rides which camioneta, and how that assignment has changed over time.
//
// The module exists because the assignment itself lives somewhere this API
// does not own — a Firestore `users` collection whose CAMIONETA_ASIGNADA
// field is rewritten by the desktop app, by the Android app and by hand from
// the Firebase console, with no history and no usable modification time (the
// document's update_time tracks "the app was opened", not "somebody was
// moved"). The only way to learn that an assignment changed is to photograph
// the roster periodically and COMPARE VALUES.
//
// That method has two honest limits, and they are encoded in the names here
// rather than papered over: a change can only be bounded to the window
// between two snapshots (hence Cambio.DetectadoEn and Cambio.VentanaDesde,
// never a "changed at"), and two changes inside one window collapse into one
// (A→B→C is recorded as A→C).
package domain

// Camioneta is the almacén a person rides, or the explicit absence of an
// assignment. It is a value object: two Camionetas with the same contents are
// interchangeable, and there is no way to build an "assigned to nothing in
// particular" state.
//
// The zero value is the unassigned one, which is deliberate — every field of
// this module that can be "nobody's truck" reads naturally as the zero value.
type Camioneta struct {
	id       int
	asignada bool
}

// SinCamioneta is the unassigned value. Returned for every roster shape that
// means "this person drives nothing": the field missing from the document
// (26 of 62 users in the dev project), the field present but null (1 user),
// and any non-positive number.
func SinCamioneta() Camioneta {
	return Camioneta{}
}

// NuevaCamioneta builds an assigned Camioneta, rejecting non-positive ids.
// Use it when the id is supposed to be real (tests, explicit construction).
// The roster adapter path uses CamionetaDesdeRoster instead, where a bad
// value is data, not a defect.
func NuevaCamioneta(id int) (Camioneta, error) {
	if id <= 0 {
		return Camioneta{}, ErrCamionetaInvalida
	}
	return Camioneta{id: id, asignada: true}, nil
}

// CamionetaDesdeRoster maps a raw roster value to a Camioneta without ever
// failing. nil means the document had no usable CAMIONETA_ASIGNADA — absent,
// null, or of an unexpected type — and a non-positive number means the same
// thing said differently; both collapse to SinCamioneta.
//
// This deliberately does not report an error. The roster is written by three
// clients we do not control; a value we cannot use is a fact to record, not a
// reason to abandon the whole snapshot and lose the other sixty people.
func CamionetaDesdeRoster(id *int) Camioneta {
	if id == nil || *id <= 0 {
		return SinCamioneta()
	}
	return Camioneta{id: *id, asignada: true}
}

// Asignada reports whether the person rides a camioneta at all.
func (c Camioneta) Asignada() bool { return c.asignada }

// ID returns the almacén id. Meaningless (zero) when Asignada is false —
// callers must check Asignada first.
func (c Camioneta) ID() int { return c.id }

// Igual reports whether two Camionetas denote the same assignment. Two
// unassigned values are equal.
func (c Camioneta) Igual(otra Camioneta) bool {
	return c.asignada == otra.asignada && c.id == otra.id
}
