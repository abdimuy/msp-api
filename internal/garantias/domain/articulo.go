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

	// R10 dirty tracking: nuevo marks an article born in this session, which is
	// still only an INSERT; modificado marks a hydrated one changed since, which
	// is an UPDATE. They never hold together, and a hydrated untouched article
	// is clean, so the repository writes exactly the rows that moved and none of
	// the rest.
	nuevo      bool
	modificado bool

	reingresadoInventario bool
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
	description := normalizarNFC(strings.TrimSpace(p.Description))
	if description == "" {
		return nil, ErrArticuloDescriptionObligatoria
	}
	if utf8.RuneCountInString(description) > 300 {
		return nil, ErrArticuloDescriptionMuyLarga
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
		nuevo:       true,
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

// avanzar moves the article stage to hasta. The origin decides which routes the
// folio may walk (R1) and AvanzarArticulo's reach is bounded by R2; both rules
// are resolved against the single table in transiciones.go via puedeAvanzar.
func (a *Articulo) avanzar(hasta Etapa, origen OrigenFolio, now time.Time) error {
	if !puedeAvanzar(a.etapa, hasta, origen) {
		return ErrTransicionEtapaNoPermitida
	}
	a.etapa = hasta
	a.aplicarUbicacion(hasta)
	a.aplicarDesenlaceAutomatico()
	a.tocar(now)
	return nil
}

// aplicarUbicacion moves the article to the location its new stage dictates
// (R3). Stages absent from the table leave the location untouched: the item
// does not move just because its stage changed.
func (a *Articulo) aplicarUbicacion(hasta Etapa) {
	if ubicacion, impone := ubicacionParaEtapa(hasta); impone {
		a.ubicacion = ubicacion
	}
	if hasta == EtapaReingresadoInventario {
		a.reingresadoInventario = true
	}
}

// aplicarDesenlaceAutomatico gives the article the outcome it earns by landing
// on a terminal stage of the client or floor path (R6). It is a no-op on every
// other stage, so it is safe to call from any stage mutator.
func (a *Articulo) aplicarDesenlaceAutomatico() {
	if a.etapa != EtapaEntregado && a.etapa != EtapaReingresadoInventario {
		return
	}
	a.desenlace = desenlaceAutomatico(a)
}

// desenlaceAutomatico resolves R6 by priority: a replacement is always
// reemplazado; a rejected or no-fault verdict returns the item; anything that
// passed through a repair (workshop route, or supplier route accepted) is
// reparado; a floor item reviewed without repair has no outcome.
func desenlaceAutomatico(a *Articulo) *Desenlace {
	if a.rol.EsReemplazo() {
		d := DesenlaceReemplazado
		return &d
	}
	if a.dictamen != nil && (a.dictamen.EsRechazada() || a.dictamen.EsSinFalla()) {
		d := DesenlaceDevuelto
		return &d
	}
	if a.pasoPorReparado() {
		d := DesenlaceReparado
		return &d
	}
	return nil
}

// pasoPorReingresadoInventario reports whether the article passed through
// reingresado_inventario in its history.
func (a *Articulo) pasoPorReingresadoInventario() bool {
	if a.etapa == EtapaReingresadoInventario {
		return true
	}
	return a.reingresadoInventario
}

// pasoPorReparado reports whether the article went through a repair. It is
// derived from RUTA + DICTAMEN rather than from a dedicated column: ruta taller
// only reaches listo_entrega through reparado_taller, and ruta proveedor only
// through reparado_proveedor when the verdict was aceptada. No new column is
// needed (see the 000070 comment and the task report).
func (a *Articulo) pasoPorReparado() bool {
	if a.ruta == nil {
		return false
	}
	if a.ruta.EsTaller() {
		return true
	}
	return a.dictamen != nil && a.dictamen.EsAceptada()
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
	a.aplicarUbicacion(hasta)
	a.tocar(now)
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
// dictamen_recibido: the machine alone is not enough, because its
// reparado_proveedor -> listo_entrega and espera_respuesta_cliente ->
// listo_entrega edges would otherwise let a second verdict be applied.
func (a *Articulo) registrarDictamen(d Dictamen, now time.Time) error {
	if a.ruta == nil || !a.ruta.EsProveedor() {
		return ErrArticuloDictamenSoloProveedor
	}
	if a.etapa != EtapaDictamenRecibido {
		return ErrTransicionEtapaNoPermitida
	}
	hasta := dictamenTarget(d)
	if !a.etapa.CanTransitionTo(hasta) {
		return ErrTransicionEtapaNoPermitida
	}
	dictamen := d
	a.dictamen = &dictamen
	a.etapa = hasta
	a.aplicarUbicacion(hasta)
	a.tocar(now)
	return nil
}

// autorizarCambioFisico moves the article along the swap path resolved by
// swapPath, ending in standby and opening the parallel replacement flow.
func (a *Articulo) autorizarCambioFisico(now time.Time) error {
	path, err := a.swapPath()
	if err != nil {
		return err
	}
	a.etapa = path[len(path)-1]
	a.aplicarUbicacion(a.etapa)
	a.tocar(now)
	return nil
}

// swapPath resolves the stage hops a physical swap walks from the current
// stage, validating every edge against the machine. The workshop path passes
// through cambio_autorizado, so authorizing the swap stays a fact of its own in
// the timeline instead of jumping straight from en_taller to standby; the
// customer path (espera_respuesta_cliente -> standby) is a single hop. A
// replacement born in listo_entrega is swapped too (decision 9 carve-out),
// since the machine gives listo_entrega no other exit; that carve-out is the
// deliberate deviation the task report calls out.
func (a *Articulo) swapPath() ([]Etapa, error) {
	if a.rol.EsReemplazo() && a.etapa == EtapaListoEntrega {
		return []Etapa{EtapaStandby}, nil
	}
	if a.etapa == EtapaEnTaller {
		if !a.etapa.CanTransitionTo(EtapaCambioAutorizado) {
			return nil, ErrTransicionEtapaNoPermitida
		}
		if !EtapaCambioAutorizado.CanTransitionTo(EtapaStandby) {
			return nil, ErrTransicionEtapaNoPermitida
		}
		return []Etapa{EtapaCambioAutorizado, EtapaStandby}, nil
	}
	if !a.etapa.CanTransitionTo(EtapaStandby) {
		return nil, ErrTransicionEtapaNoPermitida
	}
	return []Etapa{EtapaStandby}, nil
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
	a.aplicarUbicacion(hasta)
	a.tocar(now)
	return nil
}

// marcarNoDevuelto moves an article from espera_respuesta_cliente to standby
// without creating a replacement (R8): the client never answered or refused to
// take back a working item. It is a decision, so the aggregate requires a
// decider role. The location is untouched — standby does not move the item.
func (a *Articulo) marcarNoDevuelto(now time.Time) error {
	if a.etapa != EtapaEsperaRespuestaCliente {
		return ErrTransicionEtapaNoPermitida
	}
	a.etapa = EtapaStandby
	a.tocar(now)
	return nil
}

// entregar closes an article's client path: listo_entrega -> entregado, with
// the entregado location and the outcome it earns (R6). It is the one mutator
// without a CanTransitionTo guard of its own: its only caller is Entregar, which
// already filters to the articles it is delivering (R9), so handing it anything
// else is a programmer error, not an input to reject.
func (a *Articulo) entregar(now time.Time) {
	a.etapa = EtapaEntregado
	a.aplicarUbicacion(EtapaEntregado)
	a.aplicarDesenlaceAutomatico()
	a.tocar(now)
}

// moverA applies a manual location change (R3). The destination must be one a
// manual move may set, the article must not be terminal, and the move must
// actually move the item; the caller builds the articulo_movido event from the
// from/to pair.
func (a *Articulo) moverA(hasta Ubicacion, now time.Time) error {
	if a.etapa.EsTerminal() {
		return ErrArticuloTerminalNoSeMueve
	}
	if !ubicacionManualPermitida(hasta) {
		return ErrUbicacionManualNoPermitida
	}
	if hasta == a.ubicacion {
		return ErrUbicacionSinCambio
	}
	a.ubicacion = hasta
	a.tocar(now)
	return nil
}

// tocar records that a mutator changed the article: it flags the article as
// modified for the repository (R10) and stamps the update time. A new article
// stays nuevo — it is an INSERT, not an UPDATE — though it still gets the stamp,
// so a load of every kind of change leaves the three states as the repository
// needs them.
func (a *Articulo) tocar(now time.Time) {
	if !a.nuevo {
		a.modificado = true
	}
	a.audit.MarkUpdatedAt(now)
}

// EsNuevo reports whether the article was born in this session and is still only
// an INSERT for the repository (R10).
func (a *Articulo) EsNuevo() bool { return a.nuevo }

// EstaModificado reports whether a hydrated article changed in this session, the
// UPDATE half of R10.
func (a *Articulo) EstaModificado() bool { return a.modificado }

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
func (a *Articulo) Ruta() *RutaReparacion {
	if a.ruta == nil {
		return nil
	}
	r := *a.ruta
	return &r
}

// Etapa returns the article's position in the stage machine.
func (a *Articulo) Etapa() Etapa { return a.etapa }

// Ubicacion returns where the article is physically located.
func (a *Articulo) Ubicacion() Ubicacion { return a.ubicacion }

// Dictamen returns the supplier verdict after dictamen_recibido.
func (a *Articulo) Dictamen() *Dictamen {
	if a.dictamen == nil {
		return nil
	}
	d := *a.dictamen
	return &d
}

// Desenlace returns the parallel-flow outcome after standby.
func (a *Articulo) Desenlace() *Desenlace {
	if a.desenlace == nil {
		return nil
	}
	d := *a.desenlace
	return &d
}

// CerradoEn returns when the article was dropped from the folio, if it was.
func (a *Articulo) CerradoEn() *time.Time { return a.cerradoEn }

// CreatedAt returns the article's creation timestamp (UTC).
func (a *Articulo) CreatedAt() time.Time { return a.audit.CreatedAt() }

// UpdatedAt returns the article's last modification timestamp (UTC).
func (a *Articulo) UpdatedAt() time.Time { return a.audit.UpdatedAt() }
