package domain

import (
	"iter"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/audit"
)

// Garantia is the aggregate root of a warranty folio: the folio header plus
// its child articles and the timeline events each command emits. Business
// rules live here and in the VOs; the database is a dummy store and all
// behavior happens in Go (CLAUDE.md §1).
//
// A folio starts in abierto, gains at least one article, and then follows the
// estado machine (estado_folio.go). Articles move through the stage machine
// (transiciones.go) driven by the methods below. No method mutates without
// checking its transition first.
type Garantia struct {
	id             uuid.UUID
	folio          Folio
	origen         OrigenFolio
	clienteID      *int
	ventaID        *int
	estadoCuenta   *EstadoCuenta
	estado         EstadoFolio
	description    string
	vigenciaHasta  *time.Time
	calle          string
	numeroExterior string
	colonia        string
	localidad      string
	ciudad         string
	codigoPostal   string
	gpsLat         *float64
	gpsLon         *float64
	abiertoPor     string
	cerradoEn      *time.Time
	audit          audit.Timestamped

	articulos          []*Articulo
	eventosPendientes  []*Evento
	imagenesPendientes []*Imagen
}

// ActorParams carries the event provenance every mutating method requires
// (brief decision 8): who performed the action, the offline idempotency key
// and the device timestamp. The aggregate never invents these values.
//
// RolDecisor is MANDATORY in the three decision events (RegistrarDiagnostico,
// AutorizarCambioFisico, RegistrarDesenlace) and optional elsewhere, where it
// is stored when present. GPSLat/GPSLon are always optional but come as a
// pair, each in range (spec §3.3): the column pair records where the agent
// was, and half a position is not a position.
//
// Huella is the fingerprint of the request content (computed in app, opaque
// here) and is copied to every event the command emits.
type ActorParams struct {
	Usuario           string
	ClaveIdempotencia string
	DeviceCreatedAt   time.Time
	RolDecisor        *RolDecisor
	GPSLat, GPSLon    *float64
	Huella            string
}

// AbrirGarantiaParams carries the inputs to AbrirGarantia.
type AbrirGarantiaParams struct {
	Folio          Folio
	Origen         OrigenFolio
	ClienteID      *int
	VentaID        *int
	EstadoCuenta   *EstadoCuenta
	Description    string
	VigenciaHasta  *time.Time
	Calle          string
	NumeroExterior string
	Colonia        string
	Localidad      string
	Ciudad         string
	CodigoPostal   string
	GPSLat         *float64
	GPSLon         *float64
	AbiertoPor     string
	Now            time.Time
	Actor          ActorParams
}

// validarOrigenPiso rejects client and sale data on a piso folio.
func validarOrigenPiso(p AbrirGarantiaParams) error {
	if !p.Origen.EsPiso() {
		return nil
	}
	if p.ClienteID != nil {
		return ErrClienteIDNoPermitido
	}
	if p.VentaID != nil {
		return ErrVentaIDNoPermitido
	}
	if p.EstadoCuenta != nil {
		return ErrEstadoCuentaNoPermitido
	}
	if strings.TrimSpace(p.Calle) != "" || strings.TrimSpace(p.NumeroExterior) != "" ||
		strings.TrimSpace(p.Colonia) != "" || strings.TrimSpace(p.Localidad) != "" ||
		strings.TrimSpace(p.Ciudad) != "" || strings.TrimSpace(p.CodigoPostal) != "" ||
		p.GPSLat != nil || p.GPSLon != nil {
		return ErrDomicilioNoPermitido
	}
	return nil
}

// validarOrigenCliente requires client, sale, account state and the full home
// address on a cliente folio. The domicilio is a single concept made of six
// fields, the same block validarOrigenPiso forbids entirely on piso folios;
// GPS stays optional (decision 5).
func validarOrigenCliente(p AbrirGarantiaParams) error {
	if !p.Origen.EsCliente() {
		return nil
	}
	if p.ClienteID == nil {
		return ErrClienteIDObligatorio
	}
	if p.VentaID == nil {
		return ErrVentaIDObligatorio
	}
	if p.EstadoCuenta == nil {
		return ErrEstadoCuentaObligatorio
	}
	if strings.TrimSpace(p.Calle) == "" ||
		strings.TrimSpace(p.NumeroExterior) == "" ||
		strings.TrimSpace(p.Colonia) == "" ||
		strings.TrimSpace(p.Localidad) == "" ||
		strings.TrimSpace(p.Ciudad) == "" ||
		strings.TrimSpace(p.CodigoPostal) == "" {
		return ErrDomicilioObligatorio
	}
	return nil
}

// validarDomicilioLargo enforces each address field against its column width
// (migration 000050). It runs after validarOrigenCliente, which already
// required every field on cliente folios, and after validarOrigenPiso, which
// forbids the whole block on piso ones. Counting is in characters because the
// columns are VARCHAR(n) UTF8: a byte count would reject accented names that
// the database happily stores.
func validarDomicilioLargo(p AbrirGarantiaParams) error {
	if utf8.RuneCountInString(p.Calle) > 300 {
		return ErrCalleMuyLarga
	}
	if utf8.RuneCountInString(p.NumeroExterior) > 20 {
		return ErrNumeroExteriorMuyLargo
	}
	if utf8.RuneCountInString(p.Colonia) > 100 {
		return ErrColoniaMuyLarga
	}
	if utf8.RuneCountInString(p.Localidad) > 100 {
		return ErrLocalidadMuyLarga
	}
	if utf8.RuneCountInString(p.Ciudad) > 100 {
		return ErrCiudadMuyLarga
	}
	if utf8.RuneCountInString(p.CodigoPostal) > 10 {
		return ErrCodigoPostalMuyLargo
	}
	return nil
}

// AbrirGarantia opens a new warranty folio. The origin decides which fields
// are mandatory and which are rejected: cliente folios take client, sale and
// account balance plus the home address; piso folios reject all of them.
//
// It emits the folio_abierto event as part of creation (not persisted yet —
// it stays in EventosPendientes until the repository saves the aggregate).
func AbrirGarantia(p AbrirGarantiaParams) (*Garantia, error) {
	if !p.Folio.IsValid() {
		return nil, ErrFolioInvalido
	}
	if !p.Origen.IsValid() {
		return nil, ErrOrigenFolioInvalido
	}
	abiertoPor := normalizarNFC(strings.TrimSpace(p.AbiertoPor))
	if abiertoPor == "" {
		return nil, ErrAbiertoPorObligatorio
	}
	if utf8.RuneCountInString(abiertoPor) > 64 {
		return nil, ErrAbiertoPorMuyLargo
	}
	description := normalizarNFC(strings.TrimSpace(p.Description))
	if description == "" {
		return nil, ErrDescriptionObligatoria
	}
	if err := validarOrigenPiso(p); err != nil {
		return nil, err
	}
	if err := validarOrigenCliente(p); err != nil {
		return nil, err
	}
	if err := validarDomicilioLargo(p); err != nil {
		return nil, err
	}
	if !gpsValido(p.GPSLat, p.GPSLon) {
		return nil, ErrDomicilioGPSInvalido
	}
	if p.EstadoCuenta != nil && !p.EstadoCuenta.IsValid() {
		return nil, ErrEstadoCuentaInvalido
	}
	g := &Garantia{
		id:             uuid.New(),
		folio:          p.Folio,
		origen:         p.Origen,
		clienteID:      p.ClienteID,
		ventaID:        p.VentaID,
		estadoCuenta:   p.EstadoCuenta,
		estado:         EstadoFolioAbierto,
		description:    description,
		vigenciaHasta:  p.VigenciaHasta,
		calle:          normalizarNFC(p.Calle),
		numeroExterior: normalizarNFC(p.NumeroExterior),
		colonia:        normalizarNFC(p.Colonia),
		localidad:      normalizarNFC(p.Localidad),
		ciudad:         normalizarNFC(p.Ciudad),
		codigoPostal:   normalizarNFC(p.CodigoPostal),
		gpsLat:         p.GPSLat,
		gpsLon:         p.GPSLon,
		abiertoPor:     abiertoPor,
		audit:          audit.NewTimestamped(p.Now),
	}
	e, err := g.buildEvent(p.Actor, p.Now, TipoEventoFolioAbierto, nil, "", nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	g.eventosPendientes = append(g.eventosPendientes, e)
	return g, nil
}

// buildEvent builds a validated timeline event WITHOUT appending it. Callers
// build the event first — so a NOT NULL violation can fail before any state
// is touched — then mutate state and buffer the event with append.
func (g *Garantia) buildEvent(actor ActorParams, now time.Time, tipo TipoEvento, articuloRef *uuid.UUID, description string, desde, hasta *Etapa, ubicDesde, ubicHasta *Ubicacion) (*Evento, error) {
	return newEvento(EventoParams{
		GarantiaID:        g.id,
		ArticuloRef:       articuloRef,
		Tipo:              tipo,
		Description:       description,
		EtapaDesde:        desde,
		EtapaHasta:        hasta,
		UbicacionDesde:    ubicDesde,
		UbicacionHasta:    ubicHasta,
		Usuario:           actor.Usuario,
		RolDecisor:        actor.RolDecisor,
		GPSLat:            actor.GPSLat,
		GPSLon:            actor.GPSLon,
		CreatedAt:         now,
		DeviceCreatedAt:   actor.DeviceCreatedAt,
		ClaveIdempotencia: actor.ClaveIdempotencia,
		Huella:            actor.Huella,
	})
}

// exigirOperacion applies the single R4 rule to one operation and maps a
// refusal to its sentinel. Every article mutator calls it before touching
// anything, so the estado × operación table is the only place the rule lives.
func (g *Garantia) exigirOperacion(op operacionArticulo, etapa Etapa) error {
	if !folioAdmiteOperacion(g.estado, op, etapa) {
		return ErrFolioNoAdmiteOperacion
	}
	return nil
}

func (g *Garantia) findArticulo(id uuid.UUID) (*Articulo, error) {
	for _, a := range g.articulos {
		if a.id == id {
			return a, nil
		}
	}
	return nil, ErrArticuloNoEncontrado
}

// AgregarArticuloParams carries the inputs to AgregarArticulo. The event
// provenance (user, idempotency key, device time) comes from ActorParams
// (decision 8), not from here.
type AgregarArticuloParams struct {
	ArticuloID  *int
	Clave       string
	Description string
}

// AgregarArticulo adds a child article to the folio. The article starts in
// registrado with the location dictated by the origin (domicilio_cliente for
// cliente, almacen_revision for piso). Only abierto and en_proceso folios
// admit new articles.
func (g *Garantia) AgregarArticulo(p AgregarArticuloParams, actor ActorParams, now time.Time) error {
	if !folioAdmiteOperacion(g.estado, opAgregarArticulo, "") {
		return ErrFolioNoAdmiteArticulos
	}
	ubicacion := UbicacionAlmacenRevision
	if g.origen.EsCliente() {
		ubicacion = UbicacionDomicilioCliente
	}
	articulo, err := newArticulo(NewArticuloParams{
		ID:          uuid.New(),
		GarantiaID:  g.id,
		Rol:         RolArticuloOriginal,
		ArticuloID:  p.ArticuloID,
		Clave:       p.Clave,
		Description: p.Description,
		Etapa:       EtapaRegistrado,
		Ubicacion:   ubicacion,
		CreatedAt:   now,
	})
	if err != nil {
		return err
	}
	e, err := g.buildEvent(actor, now, TipoEventoArticuloAgregado, &articulo.id, "", nil, nil, nil, nil)
	if err != nil {
		return err
	}
	g.articulos = append(g.articulos, articulo)
	g.eventosPendientes = append(g.eventosPendientes, e)
	g.audit.MarkUpdatedAt(now)
	return nil
}

// AvanzarArticulo moves one article forward to the requested stage. The move
// is validated against the stage machine filtered by the folio origin (R1) and
// bounded by AvanzarArticulo's reach (R2): decision-only destinations and the
// exits of dictamen_recibido and standby are rejected here and belong to their
// own commands. Anything rejected leaves both the stage and the pending-event
// buffer untouched.
func (g *Garantia) AvanzarArticulo(articuloID uuid.UUID, hasta Etapa, actor ActorParams, now time.Time) error {
	articulo, err := g.findArticulo(articuloID)
	if err != nil {
		return err
	}
	if err := g.exigirOperacion(opAvanzar, articulo.Etapa()); err != nil {
		return err
	}
	desde := articulo.Etapa()
	ubicDesde, ubicHasta := cambioUbicacionPorEtapa(articulo.Ubicacion(), hasta)
	e, err := g.buildEvent(actor, now, TipoEventoEtapaAvanzada, &articuloID, "", &desde, &hasta, ubicDesde, ubicHasta)
	if err != nil {
		return err
	}
	if err := articulo.avanzar(hasta, g.origen, now); err != nil {
		return err
	}
	g.eventosPendientes = append(g.eventosPendientes, e)
	g.audit.MarkUpdatedAt(now)
	return nil
}

// RegistrarDiagnostico fixes the article's repair route and advances it in
// the same call (see articulo.registrarDiagnostico).
func (g *Garantia) RegistrarDiagnostico(articuloID uuid.UUID, ruta RutaReparacion, actor ActorParams, now time.Time) error {
	articulo, err := g.findArticulo(articuloID)
	if err != nil {
		return err
	}
	if err := g.exigirOperacion(opRegistrarDiagnostico, articulo.Etapa()); err != nil {
		return err
	}
	if !ruta.IsValid() {
		return ErrRutaReparacionInvalida
	}
	hasta := diagnosticoTarget(ruta)
	desde := articulo.Etapa()
	ubicDesde, ubicHasta := cambioUbicacionPorEtapa(articulo.Ubicacion(), hasta)
	e, err := g.buildEvent(actor, now, TipoEventoDiagnosticoRegistrado, &articuloID, "", &desde, &hasta, ubicDesde, ubicHasta)
	if err != nil {
		return err
	}
	if actor.RolDecisor == nil {
		return ErrRolDecisorObligatorio
	}
	if err := articulo.registrarDiagnostico(ruta, now); err != nil {
		return err
	}
	g.eventosPendientes = append(g.eventosPendientes, e)
	g.audit.MarkUpdatedAt(now)
	return nil
}

// RegistrarDictamen applies the supplier's verdict and advances the article
// in the same call (see articulo.registrarDictamen). Only legal for supplier
// route articles and only from dictamen_recibido.
func (g *Garantia) RegistrarDictamen(articuloID uuid.UUID, dictamen Dictamen, actor ActorParams, now time.Time) error {
	articulo, err := g.findArticulo(articuloID)
	if err != nil {
		return err
	}
	if err := g.exigirOperacion(opRegistrarDictamen, articulo.Etapa()); err != nil {
		return err
	}
	if !dictamen.IsValid() {
		return ErrDictamenInvalido
	}
	hasta := dictamenTarget(dictamen)
	desde := articulo.Etapa()
	ubicDesde, ubicHasta := cambioUbicacionPorEtapa(articulo.Ubicacion(), hasta)
	e, err := g.buildEvent(actor, now, TipoEventoDictamenRegistrado, &articuloID, "", &desde, &hasta, ubicDesde, ubicHasta)
	if err != nil {
		return err
	}
	if err := articulo.registrarDictamen(dictamen, now); err != nil {
		return err
	}
	g.eventosPendientes = append(g.eventosPendientes, e)
	g.audit.MarkUpdatedAt(now)
	return nil
}

// AutorizarCambioFisico moves the original article into standby and creates
// the replacement row in listo_entrega/almacen_revision. The replacement is
// built before the original moves, so a NOT NULL violation (an empty
// description on a hydrated row, for instance) fails with the aggregate
// untouched. The swap is only reachable from en_taller or
// espera_respuesta_cliente (en_taller walking the composed machine path
// en_taller -> cambio_autorizado -> standby); a single cambio_autorizado
// event is emitted carrying the swap from/to.
func (g *Garantia) AutorizarCambioFisico(articuloID uuid.UUID, actor ActorParams, now time.Time) error {
	articulo, err := g.findArticulo(articuloID)
	if err != nil {
		return err
	}
	if err := g.exigirOperacion(opAutorizarCambioFisico, articulo.Etapa()); err != nil {
		return err
	}
	desde := articulo.Etapa()
	hasta := EtapaStandby
	e, err := g.buildEvent(actor, now, TipoEventoCambioAutorizado, &articuloID, "", &desde, &hasta, nil, nil)
	if err != nil {
		return err
	}
	if actor.RolDecisor == nil {
		return ErrRolDecisorObligatorio
	}
	reemplazo, err := newArticulo(NewArticuloParams{
		ID:          uuid.New(),
		GarantiaID:  g.id,
		Rol:         RolArticuloReemplazo,
		ReemplazaA:  &articuloID,
		Description: articulo.Description(),
		Etapa:       EtapaListoEntrega,
		Ubicacion:   UbicacionAlmacenRevision,
		CreatedAt:   now,
	})
	if err != nil {
		return err
	}
	if err := articulo.autorizarCambioFisico(now); err != nil {
		return err
	}
	g.articulos = append(g.articulos, reemplazo)
	g.eventosPendientes = append(g.eventosPendientes, e)
	g.audit.MarkUpdatedAt(now)
	return nil
}

// RegistrarDesenlace applies a parallel-flow outcome from standby, advancing
// the article to the matching terminal stage in the same call.
func (g *Garantia) RegistrarDesenlace(articuloID uuid.UUID, desenlace Desenlace, actor ActorParams, now time.Time) error {
	articulo, err := g.findArticulo(articuloID)
	if err != nil {
		return err
	}
	if err := g.exigirOperacion(opRegistrarDesenlace, articulo.Etapa()); err != nil {
		return err
	}
	hasta, err := desenlaceTarget(desenlace)
	if err != nil {
		return err
	}
	desde := articulo.Etapa()
	ubicDesde, ubicHasta := cambioUbicacionPorEtapa(articulo.Ubicacion(), hasta)
	e, err := g.buildEvent(actor, now, TipoEventoDesenlaceRegistrado, &articuloID, "", &desde, &hasta, ubicDesde, ubicHasta)
	if err != nil {
		return err
	}
	if actor.RolDecisor == nil {
		return ErrRolDecisorObligatorio
	}
	if err := articulo.registrarDesenlace(desenlace, hasta, now); err != nil {
		return err
	}
	g.eventosPendientes = append(g.eventosPendientes, e)
	g.audit.MarkUpdatedAt(now)
	return nil
}

// MarcarNoDevuelto moves an article from espera_respuesta_cliente to standby
// without creating a replacement (R8): the client never answered or refused a
// working item. It is a decision, so it requires a decider role, and it emits
// the devolucion_fallida event.
func (g *Garantia) MarcarNoDevuelto(articuloID uuid.UUID, actor ActorParams, now time.Time) error {
	articulo, err := g.findArticulo(articuloID)
	if err != nil {
		return err
	}
	if err := g.exigirOperacion(opMarcarNoDevuelto, articulo.Etapa()); err != nil {
		return err
	}
	desde := articulo.Etapa()
	hasta := EtapaStandby
	e, err := g.buildEvent(actor, now, TipoEventoDevolucionFallida, &articuloID, "", &desde, &hasta, nil, nil)
	if err != nil {
		return err
	}
	if actor.RolDecisor == nil {
		return ErrRolDecisorObligatorio
	}
	if err := articulo.marcarNoDevuelto(now); err != nil {
		return err
	}
	g.eventosPendientes = append(g.eventosPendientes, e)
	g.audit.MarkUpdatedAt(now)
	return nil
}

// MoverArticulo records a manual location change (R3). It may set only the
// working locations — never entregado or baja, which only a stage sets — the
// target must differ from where the article already is, and a terminal article
// cannot move. It emits its own articulo_movido event carrying the from/to
// location pair.
func (g *Garantia) MoverArticulo(articuloID uuid.UUID, hasta Ubicacion, actor ActorParams, now time.Time) error {
	articulo, err := g.findArticulo(articuloID)
	if err != nil {
		return err
	}
	if err := g.exigirOperacion(opMoverArticulo, articulo.Etapa()); err != nil {
		return err
	}
	desde := articulo.Ubicacion()
	e, err := g.buildEvent(actor, now, TipoEventoArticuloMovido, &articuloID, "", nil, nil, &desde, &hasta)
	if err != nil {
		return err
	}
	if err := articulo.moverA(hasta, now); err != nil {
		return err
	}
	g.eventosPendientes = append(g.eventosPendientes, e)
	g.audit.MarkUpdatedAt(now)
	return nil
}

// IniciarProceso moves the folio from abierto to en_proceso. It requires at
// least one article; a folio without articles cannot start the process. It
// emits the folio_en_proceso event (R7), not the stage event.
func (g *Garantia) IniciarProceso(actor ActorParams, now time.Time) error {
	if !g.estado.CanTransitionTo(EstadoFolioEnProceso) {
		return ErrTransicionEstadoNoPermitida
	}
	if len(g.articulos) == 0 {
		return ErrGarantiaSinArticulos
	}
	e, err := g.buildEvent(actor, now, TipoEventoFolioEnProceso, nil, "", nil, nil, nil, nil)
	if err != nil {
		return err
	}
	g.estado = EstadoFolioEnProceso
	g.eventosPendientes = append(g.eventosPendientes, e)
	g.audit.MarkUpdatedAt(now)
	return nil
}

// MarcarListoEntrega moves the folio to listo_entrega once every article is
// in listo_entrega, standby or a terminal stage (decision 4). It emits the
// folio_listo_entrega event (R7).
func (g *Garantia) MarcarListoEntrega(actor ActorParams, now time.Time) error {
	if !g.estado.CanTransitionTo(EstadoFolioListoEntrega) {
		return ErrTransicionEstadoNoPermitida
	}
	for _, a := range g.articulos {
		if !stageListoParaEntrega(a.Etapa()) {
			return ErrArticulosNoListosParaEntrega
		}
	}
	e, err := g.buildEvent(actor, now, TipoEventoFolioListoEntrega, nil, "", nil, nil, nil, nil)
	if err != nil {
		return err
	}
	g.estado = EstadoFolioListoEntrega
	g.eventosPendientes = append(g.eventosPendientes, e)
	g.audit.MarkUpdatedAt(now)
	return nil
}

// stageListoParaEntrega reports whether an article blocks the delivery
// readiness check.
func stageListoParaEntrega(e Etapa) bool {
	return e == EtapaListoEntrega || e == EtapaStandby || e.EsTerminal()
}

// Entregar hands the folio to the client and closes the client path. The agent
// GPS is mandatory — the delivery audit of §4.1 — so a delivery with no
// coordinates is refused. On a cliente folio every article in listo_entrega
// becomes entregado, with its location and its R6 outcome, and leaves one event
// of its own, plus the folio_entregado. Articles in standby are not touched:
// the folio may close with one still there (§3.2). A piso folio moves no
// articles — entregado exists only for cliente items (R1) — and simply records
// the folio fact.
//
// Every event is built before the first mutation, so an invalid actor leaves
// the aggregate untouched. The per-article events go first (they validate the
// actor when there is something to deliver); the folio event closes the batch.
func (g *Garantia) Entregar(actor ActorParams, now time.Time) error {
	if !g.estado.CanTransitionTo(EstadoFolioEntregado) {
		return ErrTransicionEstadoNoPermitida
	}
	if actor.GPSLat == nil || actor.GPSLon == nil {
		return ErrEntregaGPSRequerido
	}
	type entregaPendiente struct {
		articulo *Articulo
		evento   *Evento
	}
	var entregas []entregaPendiente
	if g.origen.EsCliente() {
		for _, a := range g.articulos {
			if a.etapa != EtapaListoEntrega {
				continue
			}
			desde := EtapaListoEntrega
			hasta := EtapaEntregado
			ubicDesde, ubicHasta := cambioUbicacionPorEtapa(a.ubicacion, hasta)
			// Each delivered article gets its own key derived from the
			// command's: CLAVE_IDEMPOTENCIA is UNIQUE, so reusing the
			// command's key for every article would violate the index on
			// the second one. The folio event keeps the original key; it
			// is the row the replay lookup finds by TIPO.
			actorArticulo := actor
			actorArticulo.ClaveIdempotencia = claveDerivada(actor.ClaveIdempotencia, a.id.String())
			ev, err := g.buildEvent(actorArticulo, now, TipoEventoEtapaAvanzada, &a.id, "", &desde, &hasta, ubicDesde, ubicHasta)
			if err != nil {
				return err
			}
			entregas = append(entregas, entregaPendiente{articulo: a, evento: ev})
		}
	}
	e, err := g.buildEvent(actor, now, TipoEventoFolioEntregado, nil, "", nil, nil, nil, nil)
	if err != nil {
		return err
	}
	for _, entrega := range entregas {
		entrega.articulo.entregar(now)
		g.eventosPendientes = append(g.eventosPendientes, entrega.evento)
	}
	g.estado = EstadoFolioEntregado
	g.eventosPendientes = append(g.eventosPendientes, e)
	g.audit.MarkUpdatedAt(now)
	return nil
}

// Cerrar seals a delivered folio as cerrado.
func (g *Garantia) Cerrar(actor ActorParams, now time.Time) error {
	if !g.estado.CanTransitionTo(EstadoFolioCerrado) {
		return ErrTransicionEstadoNoPermitida
	}
	e, err := g.buildEvent(actor, now, TipoEventoFolioCerrado, nil, "", nil, nil, nil, nil)
	if err != nil {
		return err
	}
	cerrado := now
	g.cerradoEn = &cerrado
	g.estado = EstadoFolioCerrado
	g.eventosPendientes = append(g.eventosPendientes, e)
	g.audit.MarkUpdatedAt(now)
	return nil
}

// Cancelar voids an open or in-process folio. The reason travels in the
// folio_cancelado event description.
func (g *Garantia) Cancelar(motivo string, actor ActorParams, now time.Time) error {
	if !g.estado.CanTransitionTo(EstadoFolioCancelado) {
		return ErrTransicionEstadoNoPermitida
	}
	motivo = strings.TrimSpace(motivo)
	if motivo == "" {
		return ErrMotivoCancelacionObligatorio
	}
	for _, a := range g.articulos {
		if ubicacionFueraDelTaller(a.Ubicacion()) {
			return ErrFolioConArticuloFuera
		}
	}
	e, err := g.buildEvent(actor, now, TipoEventoFolioCancelado, nil, motivo, nil, nil, nil, nil)
	if err != nil {
		return err
	}
	g.estado = EstadoFolioCancelado
	g.eventosPendientes = append(g.eventosPendientes, e)
	g.audit.MarkUpdatedAt(now)
	return nil
}

// HydrateGarantiaParams is the persisted shape used to rebuild a folio over
// Firebird.
type HydrateGarantiaParams struct {
	ID             uuid.UUID
	Folio          Folio
	Origen         OrigenFolio
	ClienteID      *int
	VentaID        *int
	EstadoCuenta   *EstadoCuenta
	Estado         EstadoFolio
	Description    string
	VigenciaHasta  *time.Time
	Calle          string
	NumeroExterior string
	Colonia        string
	Localidad      string
	Ciudad         string
	CodigoPostal   string
	GPSLat         *float64
	GPSLon         *float64
	AbiertoPor     string
	CerradoEn      *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Articulos      []*Articulo
}

// HydrateGarantia rebuilds a Garantia from a persisted row without any
// validation; the repository only calls it with rows it already wrote.
func HydrateGarantia(p HydrateGarantiaParams) *Garantia {
	return &Garantia{
		id:             p.ID,
		folio:          p.Folio,
		origen:         p.Origen,
		clienteID:      p.ClienteID,
		ventaID:        p.VentaID,
		estadoCuenta:   p.EstadoCuenta,
		estado:         p.Estado,
		description:    p.Description,
		vigenciaHasta:  p.VigenciaHasta,
		calle:          p.Calle,
		numeroExterior: p.NumeroExterior,
		colonia:        p.Colonia,
		localidad:      p.Localidad,
		ciudad:         p.Ciudad,
		codigoPostal:   p.CodigoPostal,
		gpsLat:         p.GPSLat,
		gpsLon:         p.GPSLon,
		abiertoPor:     p.AbiertoPor,
		cerradoEn:      p.CerradoEn,
		audit:          audit.HydrateTimestamped(p.CreatedAt, p.UpdatedAt),
		articulos:      p.Articulos,
	}
}

// ID returns the folio's primary key.
func (g *Garantia) ID() uuid.UUID { return g.id }

// Folio returns the friend folio number.
func (g *Garantia) Folio() Folio { return g.folio }

// Origen returns whether the folio belongs to a client or a sales floor.
func (g *Garantia) Origen() OrigenFolio { return g.origen }

// ClienteID returns the client identifier for cliente folios, if any.
func (g *Garantia) ClienteID() *int { return g.clienteID }

// VentaID returns the sale identifier for cliente folios, if any.
func (g *Garantia) VentaID() *int { return g.ventaID }

// EstadoCuenta returns the account balance snapshot for cliente folios.
func (g *Garantia) EstadoCuenta() *EstadoCuenta { return g.estadoCuenta }

// Estado returns the folio state machine position.
func (g *Garantia) Estado() EstadoFolio { return g.estado }

// Description returns the reported issue or item description.
func (g *Garantia) Description() string { return g.description }

// VigenciaHasta returns the warranty validity deadline, if set.
func (g *Garantia) VigenciaHasta() *time.Time { return g.vigenciaHasta }

// Calle returns the home address street line.
func (g *Garantia) Calle() string { return g.calle }

// NumeroExterior returns the home address street number.
func (g *Garantia) NumeroExterior() string { return g.numeroExterior }

// Colonia returns the home address neighborhood.
func (g *Garantia) Colonia() string { return g.colonia }

// Localidad returns the home address locality.
func (g *Garantia) Localidad() string { return g.localidad }

// Ciudad returns the home address city.
func (g *Garantia) Ciudad() string { return g.ciudad }

// CodigoPostal returns the home address postal code.
func (g *Garantia) CodigoPostal() string { return g.codigoPostal }

// GPSLat returns the folio latitude, if captured.
func (g *Garantia) GPSLat() *float64 { return g.gpsLat }

// GPSLon returns the folio longitude, if captured.
func (g *Garantia) GPSLon() *float64 { return g.gpsLon }

// AbiertoPor returns the operator who opened the folio.
func (g *Garantia) AbiertoPor() string { return g.abiertoPor }

// CerradoEn returns when the folio was closed, if it was.
func (g *Garantia) CerradoEn() *time.Time { return g.cerradoEn }

// CreatedAt returns the folio's creation timestamp (UTC).
func (g *Garantia) CreatedAt() time.Time { return g.audit.CreatedAt() }

// UpdatedAt returns the folio's last modification timestamp (UTC).
func (g *Garantia) UpdatedAt() time.Time { return g.audit.UpdatedAt() }

// Articulos returns an iterator over the articles of this folio.
func (g *Garantia) Articulos() iter.Seq[*Articulo] {
	return func(yield func(*Articulo) bool) {
		for _, a := range g.articulos {
			if !yield(a) {
				return
			}
		}
	}
}

// ArticulosCount returns the number of articles in the folio.
func (g *Garantia) ArticulosCount() int { return len(g.articulos) }

// ArticulosForRepo returns a copy of the articles slice for the repository.
func (g *Garantia) ArticulosForRepo() []*Articulo { return slices.Clone(g.articulos) }

// ArticulosNuevosForRepo returns the articles created in this session and not
// yet persisted, so Guardar knows which rows to INSERT (R10). The slice is a new
// one — the caller may reorder it freely — and the articles are the live ones,
// as the repository needs them to write.
func (g *Garantia) ArticulosNuevosForRepo() []*Articulo {
	return articulosQueCumplen(g.articulos, (*Articulo).EsNuevo)
}

// ArticulosModificadosForRepo returns the hydrated articles changed in this
// session, so Guardar knows which rows to UPDATE (R10). New articles are left
// out: they are an INSERT, never an UPDATE.
func (g *Garantia) ArticulosModificadosForRepo() []*Articulo {
	return articulosQueCumplen(g.articulos, (*Articulo).EstaModificado)
}

// articulosQueCumplen filters the aggregate's articles by a predicate into a new
// slice, the shape both R10 accessors share.
func articulosQueCumplen(articulos []*Articulo, cumple func(*Articulo) bool) []*Articulo {
	out := make([]*Articulo, 0, len(articulos))
	for _, a := range articulos {
		if cumple(a) {
			out = append(out, a)
		}
	}
	return out
}

// EventosPendientes returns an iterator over the timeline events the current
// session has produced but has not persisted yet. The historical timeline is
// not loaded by the aggregate (brief decision 7); the repository only writes
// what this iterator yields.
func (g *Garantia) EventosPendientes() iter.Seq[*Evento] {
	return func(yield func(*Evento) bool) {
		for _, e := range g.eventosPendientes {
			if !yield(e) {
				return
			}
		}
	}
}

// EventosPendientesCount returns the number of events pending persistence.
func (g *Garantia) EventosPendientesCount() int { return len(g.eventosPendientes) }

// EventosPendientesForRepo returns a copy of the pending events slice for the
// repository.
func (g *Garantia) EventosPendientesForRepo() []*Evento { return slices.Clone(g.eventosPendientes) }

// ImagenesPendientesForRepo returns a copy of the images the aggregate created
// in this session and that GarantiaRepo.Guardar must insert in the same
// transaction, after the events they hang from. Today the only one is the
// delivery signature Entregar attaches to its folio_entregado event (spec
// §4.1); evidence uploaded through AdjuntarImagen does not travel here, it is
// registered by ImagenRepo directly.
func (g *Garantia) ImagenesPendientesForRepo() []*Imagen { return slices.Clone(g.imagenesPendientes) }
