package domain

import (
	"time"

	"github.com/google/uuid"
)

// TipoCambio names the four transitions the roster can go through. They are
// kept apart because their causes are different and an investigation asks
// different questions of each; collapsing them into "changed" would throw
// away the only distinction that matters when a venta is under review.
type TipoCambio string

const (
	// TipoAsignacion — the person had no camioneta and now has one. Includes
	// somebody who appears in the roster for the first time already driving.
	TipoAsignacion TipoCambio = "asignacion"
	// TipoReasignacion — the person moved from one camioneta to another.
	TipoReasignacion TipoCambio = "reasignacion"
	// TipoRetiro — the camioneta was taken away (field deleted, nulled or
	// zeroed) while the person stayed in the roster.
	TipoRetiro TipoCambio = "retiro"
	// TipoBajaUsuario — the whole document disappeared while the person had a
	// camioneta. Nobody took their truck: they left.
	TipoBajaUsuario TipoCambio = "baja_usuario"
)

// Valido reports whether t is one of the four known transitions. This is the
// canonical rule; the CHECK constraint in migration 000062 only mirrors it
// (CLAUDE.md §1).
func (t TipoCambio) Valido() bool {
	switch t {
	case TipoAsignacion, TipoReasignacion, TipoRetiro, TipoBajaUsuario:
		return true
	default:
		return false
	}
}

// Cambio is one detected transition — an append-only history row.
//
// Read the two timestamps together or not at all. DetectadoEn is when the
// snapshot that noticed the difference ran; VentanaDesde is when the previous
// snapshot ran. All the module can honestly claim is that the change happened
// somewhere in the half-open interval (VentanaDesde, DetectadoEn].
//
// There is deliberately no "who did it" field. The roster carries no author
// on a write, and the three clients that mutate it — desktop screen, Android
// app, Firebase console — leave nothing behind that a snapshot could read.
// A column that could only ever hold NULL is worse than no column: it invites
// the reader to believe the answer exists somewhere.
type Cambio struct {
	id           uuid.UUID
	usuarioUID   string
	email        string
	nombre       string
	anterior     Camioneta
	nueva        Camioneta
	tipo         TipoCambio
	ventanaDesde time.Time
	detectadoEn  time.Time
	createdAt    time.Time
}

// nuevoCambio builds a history row. Package-private on purpose: a Cambio is
// only ever the conclusion of Comparar, never something an outer layer gets
// to assert on its own.
func nuevoCambio(o Observacion, anterior, nueva Camioneta, tipo TipoCambio, p ParametrosComparacion) *Cambio {
	return &Cambio{
		id:           uuid.New(),
		usuarioUID:   o.usuarioUID,
		email:        o.email,
		nombre:       o.nombre,
		anterior:     anterior,
		nueva:        nueva,
		tipo:         tipo,
		ventanaDesde: p.VentanaDesde,
		detectadoEn:  p.DetectadoEn,
		createdAt:    p.DetectadoEn,
	}
}

// ID returns the row's UUID, generated in Go (CLAUDE.md §1).
func (c *Cambio) ID() uuid.UUID { return c.id }

// UsuarioUID returns the Firestore document id of the person who moved.
func (c *Cambio) UsuarioUID() string { return c.usuarioUID }

// Email returns the address as observed at detection time — a snapshot, not
// a reference to the current value.
func (c *Cambio) Email() string { return c.email }

// Nombre returns the display name as observed at detection time.
func (c *Cambio) Nombre() string { return c.nombre }

// Anterior returns the camioneta the person was recorded on before.
func (c *Cambio) Anterior() Camioneta { return c.anterior }

// Nueva returns the camioneta the person is recorded on now.
func (c *Cambio) Nueva() Camioneta { return c.nueva }

// Tipo returns the kind of transition.
func (c *Cambio) Tipo() TipoCambio { return c.tipo }

// VentanaDesde returns the lower bound of the detection window: the moment
// the PREVIOUS snapshot ran. The change is not known to have happened then.
func (c *Cambio) VentanaDesde() time.Time { return c.ventanaDesde }

// DetectadoEn returns the upper bound: the moment this snapshot noticed.
// This is not when the change was made, and the column is named accordingly.
func (c *Cambio) DetectadoEn() time.Time { return c.detectadoEn }

// CreatedAt returns when the row was produced. Equal to DetectadoEn by
// construction; kept separate so the audit column means what it means
// everywhere else in the schema.
func (c *Cambio) CreatedAt() time.Time { return c.createdAt }

// tipoDeTransicion classifies a camioneta change. It assumes the two values
// actually differ — Comparar checks that before calling.
func tipoDeTransicion(anterior, nueva Camioneta) TipoCambio {
	switch {
	case !anterior.Asignada():
		return TipoAsignacion
	case !nueva.Asignada():
		return TipoRetiro
	default:
		return TipoReasignacion
	}
}
