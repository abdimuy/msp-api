package domain

import (
	"slices"
	"time"
)

// ParametrosComparacion frames one comparison in time.
type ParametrosComparacion struct {
	// LineaBase suppresses every history row. It is set for the first-ever
	// snapshot, the one with nothing to compare against. Without it the first
	// run would emit one "change" per person — sixty rows claiming somebody
	// moved when in truth we simply started looking.
	//
	// Note it is decided by the caller from the snapshots ledger ("have we
	// ever run?"), NOT from the mirror being empty. Those are different
	// questions: a mirror wiped by hand must not be mistaken for a first run,
	// or the wipe would be laundered into sixty false assignments.
	LineaBase bool
	// VentanaDesde is when the previous snapshot ran — the lower bound of
	// every change detected now. Ignored when LineaBase is set.
	VentanaDesde time.Time
	// DetectadoEn is when this snapshot ran.
	DetectadoEn time.Time
}

// Plan is the complete set of writes one snapshot implies. The caller applies
// it inside a single transaction so a snapshot is all-or-nothing: a partially
// applied plan would move the mirror forward without its matching history and
// the change would be lost forever.
type Plan struct {
	// Altas are people seen for the first time — rows to insert in the mirror.
	Altas []*Asignacion
	// Actualizaciones are people whose observed state differs — rows to update.
	Actualizaciones []*Asignacion
	// Bajas are the uids that vanished from the roster — rows to delete.
	Bajas []string
	// Cambios are the history rows, and only camioneta transitions produce
	// them. Somebody entering or leaving the roster without a camioneta moves
	// the mirror but writes no history: nothing happened about camionetas,
	// and the bitácora is about camionetas.
	Cambios []*Cambio
}

// SinEscrituras reports whether the plan touches nothing at all. Two
// identical snapshots produce such a plan, and the caller uses it to keep an
// unchanged roster from writing a single row.
func (p Plan) SinEscrituras() bool {
	return len(p.Altas) == 0 &&
		len(p.Actualizaciones) == 0 &&
		len(p.Bajas) == 0 &&
		len(p.Cambios) == 0
}

// Comparar diffs the stored mirror against a fresh roster photograph.
//
// It is a pure function: no clock, no database, no Firestore. Everything the
// module claims about who moved and when is decided here, which is why this
// is where the tests concentrate.
//
// The empty-photograph rail comes first and is not negotiable. A roster that
// reports zero people while the mirror holds assignments is overwhelmingly
// more likely to be an outage, a revoked credential or a silent downgrade to
// the free plan than a simultaneous mass resignation — and applying it would
// delete every row and emit a flood of bajas. The whole tick fails instead,
// which also leaves a gap in the snapshots ledger where the failure is
// visible.
func Comparar(previas []*Asignacion, observadas []Observacion, p ParametrosComparacion) (Plan, error) {
	if len(observadas) == 0 && len(previas) > 0 {
		return Plan{}, ErrFotoVacia
	}
	if !p.LineaBase && !p.DetectadoEn.After(p.VentanaDesde) {
		return Plan{}, ErrVentanaInvalida
	}

	pendientes := make(map[string]*Asignacion, len(previas))
	for _, a := range previas {
		pendientes[a.usuarioUID] = a
	}

	var plan Plan
	vistos := make(map[string]struct{}, len(observadas))
	for _, o := range observadas {
		if _, repetido := vistos[o.usuarioUID]; repetido {
			return Plan{}, ErrObservacionDuplicada
		}
		vistos[o.usuarioUID] = struct{}{}

		previa, conocida := pendientes[o.usuarioUID]
		if !conocida {
			aplicarAlta(&plan, o, p)
			continue
		}
		delete(pendientes, o.usuarioUID)
		aplicarObservacion(&plan, previa, o, p)
	}

	aplicarBajas(&plan, pendientes, p)
	return plan, nil
}

// aplicarAlta handles somebody the mirror has never seen. The mirror always
// gets the row; history only gets one when the newcomer already drives
// something, because "a new person exists" is not a camioneta event.
func aplicarAlta(plan *Plan, o Observacion, p ParametrosComparacion) {
	plan.Altas = append(plan.Altas, NuevaAsignacion(o, p.DetectadoEn))
	if p.LineaBase || !o.camioneta.Asignada() {
		return
	}
	plan.Cambios = append(plan.Cambios,
		nuevoCambio(o, SinCamioneta(), o.camioneta, TipoAsignacion, p))
}

// aplicarObservacion handles somebody already mirrored. When the stored state
// matches the photograph exactly, nothing at all is written — that is what
// makes an unchanged roster free.
func aplicarObservacion(plan *Plan, previa *Asignacion, o Observacion, p ParametrosComparacion) {
	if previa.CoincideCon(o) {
		return
	}
	// Read the previous camioneta BEFORE Observar overwrites it.
	anterior := previa.camioneta
	movio := !anterior.Igual(o.camioneta)

	previa.Observar(o, p.DetectadoEn)
	plan.Actualizaciones = append(plan.Actualizaciones, previa)

	if p.LineaBase || !movio {
		return
	}
	plan.Cambios = append(plan.Cambios,
		nuevoCambio(o, anterior, o.camioneta, tipoDeTransicion(anterior, o.camioneta), p))
}

// aplicarBajas handles the mirrored people the photograph did not contain.
// The mirror row goes; the history row is written only when they were driving
// something, and it is typed 'baja_usuario' rather than 'retiro' because the
// cause is different: nobody took their camioneta away, they stopped existing
// in the roster.
//
// The uids are sorted so a plan is deterministic — map iteration order would
// otherwise make the produced rows unorderable and the tests flaky.
func aplicarBajas(plan *Plan, pendientes map[string]*Asignacion, p ParametrosComparacion) {
	if len(pendientes) == 0 {
		return
	}
	uids := make([]string, 0, len(pendientes))
	for uid := range pendientes {
		uids = append(uids, uid)
	}
	slices.Sort(uids)

	for _, uid := range uids {
		previa := pendientes[uid]
		plan.Bajas = append(plan.Bajas, uid)
		if p.LineaBase || !previa.camioneta.Asignada() {
			continue
		}
		// The person is gone, so the only identity available is the one the
		// mirror last recorded. That is the honest thing to stamp on the row.
		ultima := Observacion{
			usuarioUID: previa.usuarioUID,
			email:      previa.email,
			nombre:     previa.nombre,
			camioneta:  previa.camioneta,
		}
		plan.Cambios = append(plan.Cambios,
			nuevoCambio(ultima, previa.camioneta, SinCamioneta(), TipoBajaUsuario, p))
	}
}
