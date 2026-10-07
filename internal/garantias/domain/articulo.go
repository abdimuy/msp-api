package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

// Articulo is a child entity of the Garantia aggregate: the physical item
// under custody of the folio. It is created and mutated only through the
// aggregate root; external callers obtain articles via Garantia.Articulos().
//
// Its stage machine lives in transiciones.go and is the single source of
// truth: every mutator here validates via CanTransitionTo before changing
// anything, so a rejected call never leaves a half-applied state.
type Articulo struct {
	id          uuid.UUID
	garantiaID  uuid.UUID
	rol         RolArticulo
	reemplazaA  *uuid.UUID
	articuloID  *int
	clave       string
	description string
	ruta        *RutaReparacion
	etapa       Etapa
	ubicacion   Ubicacion
	dictamen    *Dictamen
	desenlace   *Desenlace
	cerradoEn   *time.Time
	audit       audit.Timestamped
}

// NewArticuloParams carries the inputs to newArticulo.
type NewArticuloParams struct {
	ID          uuid.UUID
	GarantiaID  uuid.UUID
	Rol         RolArticulo
	ReemplazaA  *uuid.UUID
	ArticuloID  *int
	Clave       string
	Description string
	Etapa       Etapa
	Ubicacion   Ubicacion
	CreatedAt   time.Time
}

// newArticulo validates and constructs an Articulo. Package-private:
// external callers must go through the aggregate root.
func newArticulo(p NewArticuloParams) (*Articulo, error) {
	description := strings.TrimSpace(p.Description)
	if description == "" {
		return nil, ErrArticuloDescriptionObligatoria
	}
	if utf8.RuneCountInString(p.Clave) > 30 {
		return nil, ErrArticuloClaveMuyLarga
	}
	return &Articulo{
		id:          p.ID,
		garantiaID:  p.GarantiaID,
		rol:         p.Rol,
		reemplazaA:  p.ReemplazaA,
		articuloID:  p.ArticuloID,
		clave:       p.Clave,
		description: description,
		etapa:       p.Etapa,
		ubicacion:   p.Ubicacion,
		audit:       audit.NewTimestamped(p.CreatedAt),
	}, nil
}

// HydrateArticuloParams is the persisted shape used to rebuild an article
// over Firebird.
type HydrateArticuloParams struct {
	ID          uuid.UUID
	GarantiaID  uuid.UUID
	Rol         RolArticulo
	ReemplazaA  *uuid.UUID
	ArticuloID  *int
	Clave       string
	Description string
	Ruta        *RutaReparacion
	Etapa       Etapa
	Ubicacion   Ubicacion
	Dictamen    *Dictamen
	Desenlace   *Desenlace
	CerradoEn   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// HydrateArticulo rebuilds an Articulo from a persisted row without any
// validation; the repository only calls it with rows it already wrote.
func HydrateArticulo(p HydrateArticuloParams) *Articulo {
	return &Articulo{
		id:          p.ID,
		garantiaID:  p.GarantiaID,
		rol:         p.Rol,
		reemplazaA:  p.ReemplazaA,
		articuloID:  p.ArticuloID,
		clave:       p.Clave,
		description: p.Description,
		ruta:        p.Ruta,
		etapa:       p.Etapa,
		ubicacion:   p.Ubicacion,
		dictamen:    p.Dictamen,
		desenlace:   p.Desenlace,
		cerradoEn:   p.CerradoEn,
		audit:       audit.HydrateTimestamped(p.CreatedAt, p.UpdatedAt),
	}
}

// avanzar moves the article stage to hasta. Leaving en_revision through a
// repair route requires a defined route (cross-validation #3) regardless of
// whether the move comes from the diagnosis command or a bare AvanzarArticulo.
// The two swap-only stages are deliberately not reachable here:
// cambio_autorizado and standby can only be entered through
// AutorizarCambioFisico, which also creates the replacement row.
func (a *Articulo) avanzar(hasta Etapa, now time.Time) error {
	if hasta == EtapaStandby || hasta == EtapaCambioAutorizado {
		return ErrTransicionEtapaNoPermitida
	}
	if a.etapa == EtapaEnRevision && a.ruta == nil &&
		(hasta == EtapaOrdenGenerada || hasta == EtapaEnTaller) {
		return ErrArticuloRutaRequerida
	}
	if !a.etapa.CanTransitionTo(hasta) {
		return ErrTransicionEtapaNoPermitida
	}
	a.etapa = hasta
	a.audit.MarkUpdatedAt(now)
	return nil
}

// diagnosticoTarget returns the stage a route lands the article on. The
// wrapper (aggregate root) validates the route first; this helper is shared
// so the event's EtapaHasta always matches the transition target.
func diagnosticoTarget(r RutaReparacion) Etapa {
	if r.EsTaller() {
		return EtapaEnTaller
	}
	return EtapaOrdenGenerada
}

// registrarDiagnostico fixes the repair route and, in the same call, advances
// to the matching stage (en_taller for the workshop, orden_generada for the
// supplier). DICTAMEN stays empty; it only applies to the supplier route.
func (a *Articulo) registrarDiagnostico(r RutaReparacion, now time.Time) error {
	hasta := diagnosticoTarget(r)
	if !a.etapa.CanTransitionTo(hasta) {
		return ErrTransicionEtapaNoPermitida
	}
	ruta := r
	a.ruta = &ruta
	a.etapa = hasta
	a.audit.MarkUpdatedAt(now)
	return nil
}

// dictamenTarget returns the stage a supplier verdict lands the article on.
func dictamenTarget(d Dictamen) Etapa {
	switch {
	case d.EsRechazada():
		return EtapaListoEntrega
	case d.EsSinFalla():
		return EtapaEsperaRespuestaCliente
	default:
		return EtapaReparadoProveedor
	}
}

// registrarDictamen applies the supplier's verdict and, in the same call,
// advances toward the branch target (aceptada -> reparado_proveedor,
// rechazada -> listo_entrega, sin_falla -> espera_respuesta_cliente). It is
// only legal for the supplier route (cross-validation #1) and only from
// dictamen_recibido.
func (a *Articulo) registrarDictamen(d Dictamen, now time.Time) error {
	if a.ruta == nil || !a.ruta.EsProveedor() {
		return ErrArticuloDictamenSoloProveedor
	}
	hasta := dictamenTarget(d)
	if !a.etapa.CanTransitionTo(hasta) {
		return ErrTransicionEtapaNoPermitida
	}
	dictamen := d
	a.dictamen = &dictamen
	a.etapa = hasta
	a.audit.MarkUpdatedAt(now)
	return nil
}

// autorizarCambioFisico moves the article into standby, opening the parallel
// replacement flow. A replacement born in listo_entrega can be swapped too
// (decision 9 carve-out), since the machine gives listo_entrega no other
// exit; that carve-out is the deliberate deviation the task report calls
// out.
func (a *Articulo) autorizarCambioFisico(now time.Time) error {
	if a.rol.EsReemplazo() && a.etapa == EtapaListoEntrega {
		a.etapa = EtapaStandby
		a.audit.MarkUpdatedAt(now)
		return nil
	}
	// The applied edges are validated against the stage machine: for espera
	// the direct espera -> standby edge, for en_taller its composed path
	// en_taller -> cambio_autorizado -> standby, whose meaningful guard is the
	// cambio_autorizado -> standby edge (the first hop is machine-guaranteed).
	from := a.etapa
	if from == EtapaEnTaller {
		from = EtapaCambioAutorizado
	}
	if !from.CanTransitionTo(EtapaStandby) {
		return ErrTransicionEtapaNoPermitida
	}
	a.etapa = EtapaStandby
	a.audit.MarkUpdatedAt(now)
	return nil
}

// desenlaceTarget returns the terminal stage a parallel outcome lands the
// article on. It is the single validation site for outcomes: the three
// terminal ones return their stage, the parallel-only trio and unknown
// values are rejected.
func desenlaceTarget(d Desenlace) (Etapa, error) {
	switch d {
	case DesenlaceSegundaMano:
		return EtapaSegundaMano, nil
	case DesenlaceDesarmado:
		return EtapaDesarmado, nil
	case DesenlaceMerma:
		return EtapaMerma, nil
	case DesenlaceReparado, DesenlaceReemplazado, DesenlaceDevuelto:
		return "", ErrArticuloDesenlaceSoloParalelo
	default:
		return "", ErrDesenlaceInvalido
	}
}

// registrarDesenlace applies a parallel-flow outcome from standby and, in the
// same call, advances to the stage chosen by desenlaceTarget.
func (a *Articulo) registrarDesenlace(d Desenlace, hasta Etapa, now time.Time) error {
	if !a.etapa.CanTransitionTo(hasta) {
		return ErrTransicionEtapaNoPermitida
	}
	desenlace := d
	a.desenlace = &desenlace
	a.etapa = hasta
	a.audit.MarkUpdatedAt(now)
	return nil
}

// ID returns the article's primary key.
func (a *Articulo) ID() uuid.UUID { return a.id }

// GarantiaID returns the owning folio UUID.
func (a *Articulo) GarantiaID() uuid.UUID { return a.garantiaID }

// Rol returns the article's role (original or replacement).
func (a *Articulo) Rol() RolArticulo { return a.rol }

// ReemplazaA returns the original article this replacement takes over, if any.
func (a *Articulo) ReemplazaA() *uuid.UUID { return a.reemplazaA }

// ArticuloID returns the Microsip article identifier, present for client-only
// articles.
func (a *Articulo) ArticuloID() *int { return a.articuloID }

// Clave returns the article key in the folio.
func (a *Articulo) Clave() string { return a.clave }

// Description returns the reported-issue or item description.
func (a *Articulo) Description() string { return a.description }

// Ruta returns the repair route (nil until a diagnosis selects one).
func (a *Articulo) Ruta() *RutaReparacion { return a.ruta }

// Etapa returns the article's position in the stage machine.
func (a *Articulo) Etapa() Etapa { return a.etapa }

// Ubicacion returns where the article is physically located.
func (a *Articulo) Ubicacion() Ubicacion { return a.ubicacion }

// Dictamen returns the supplier verdict after dictamen_recibido.
func (a *Articulo) Dictamen() *Dictamen { return a.dictamen }

// Desenlace returns the parallel-flow outcome after standby.
func (a *Articulo) Desenlace() *Desenlace { return a.desenlace }

// CerradoEn returns when the article was dropped from the folio, if it was.
func (a *Articulo) CerradoEn() *time.Time { return a.cerradoEn }

// CreatedAt returns the article's creation timestamp (UTC).
func (a *Articulo) CreatedAt() time.Time { return a.audit.CreatedAt() }

// UpdatedAt returns the article's last modification timestamp (UTC).
func (a *Articulo) UpdatedAt() time.Time { return a.audit.UpdatedAt() }
