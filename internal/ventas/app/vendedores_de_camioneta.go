//nolint:misspell // ventas vocabulary is Spanish (vendedores, camioneta) per project convention.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// vendedoresRosterTimeout bounds the roster lookup plus the identity lookup
// that follows it.
//
// It is deliberately short and deliberately NOT configurable. Creating a
// venta is the one operation in this system that must not become slower or
// less available because a remote roster is having a bad day: the phone is
// standing in a customer's living room, the request already carries several
// megabytes of photos, and the client-side timeout that gave us the
// "ventas atoradas" incident is measured in minutes, not seconds. Three
// seconds is far above the observed lookup cost (a single indexed query over
// a collection of a few dozen documents) and far below anything a person
// would notice, so exceeding it means the roster is unhealthy, and an
// unhealthy roster must yield to what the phone already sent.
const vendedoresRosterTimeout = 3 * time.Second

// Motivo values recorded on the evidence event. They are the vocabulary the
// owner reads to tell a healthy resolution from a degraded one, so each names
// a DISTINCT failure — collapsing them into one "fallback" would hide which
// half of the pipeline broke.
const (
	// motivoRosterOK: the roster answered and its people won.
	motivoRosterOK = "ok"
	// motivoCamionetaAusente: the venta's lines carry no almacén de origen,
	// so there is no camioneta to ask about. No query ran.
	motivoCamionetaAusente = "camioneta_ausente"
	// motivoCamionetaAmbigua: the venta's lines disagree about the almacén de
	// origen. Asking about either one would be a guess. No query ran.
	motivoCamionetaAmbigua = "camioneta_ambigua"
	// motivoRosterFallo: the roster query errored or timed out.
	motivoRosterFallo = "roster_fallo"
	// motivoRosterVacio: the roster answered, with nobody.
	motivoRosterVacio = "roster_vacio"
	// motivoUsuariosFallo: the roster answered but MSP_USUARIOS could not be
	// read to turn its emails into ids.
	motivoUsuariosFallo = "usuarios_fallo"
	// motivoSinUsuarios: the roster answered and not one of its emails has an
	// MSP_USUARIOS row, so there is no id to reference.
	motivoSinUsuarios = "sin_usuarios"
	// motivoResolucionPanico: an adapter panicked mid-resolution. A panic is
	// not a failure mode anybody planned for, which is exactly why it is
	// contained: an unrecovered one unwinds the HTTP handler and answers 500,
	// and a venta lost to a defect in a REMOTE roster client is the outcome
	// this feature is forbidden to produce.
	motivoResolucionPanico = "resolucion_panico"
	// motivoRosterInvalido: the roster named people this API can reference,
	// but the vendedor rows they produce are not values the venta may hold
	// (see validarVendedoresDeRoster). The roster's content must never be
	// able to reject a venta, so the client's list wins instead.
	motivoRosterInvalido = "roster_invalido"
	// motivoRosterExcedeSlots: the roster named more people than a venta can
	// carry all the way into Microsip — see maxVendedoresDeRoster.
	motivoRosterExcedeSlots = "roster_excede_slots"
)

// maxVendedoresDeRoster is the largest roster this API will impose on a venta.
//
// Three is not a preference, it is what the downstream write supports:
// AplicarVenta maps the seller count through MSP_CFG_NUM_VENDEDORES — which
// holds rows for 1, 2 and 3 and nothing else — and resolveVendedorListaIDs
// fills exactly three Microsip columns (LIBRES_CARGOS_CC.VENDEDOR_1/2/3).
// A venta created with four vendedores is therefore accepted and then can
// NEVER be applied: it fails at aplicar with num_vendedores_sin_mapeo, long
// after the seller has left the customer's house, and needs manual repair.
//
// Measured against the live roster on 2026-08-27, the largest camioneta has
// exactly three people, so this bound has zero headroom: adding one person to
// any camioneta in Firestore would, without this guard, silently make every
// venta from that truck unappliable. Falling back to the phone's list keeps
// the venta both creatable and appliable, and the evidence event records the
// full roster that was refused so the condition is countable.
const maxVendedoresDeRoster = 3

// WithVendedoresDeCamioneta attaches the roster resolver and the email →
// usuario identity resolver that let the SERVER decide a venta's vendedores
// instead of trusting the list the phone computed.
//
// Both are required for the feature to run: without the roster there is
// nobody to ask, and without the identity resolver a roster email cannot
// become a vendedor row. Passing nil for either leaves CrearVenta behaving
// EXACTLY as it did before this existed — the phone's list is used verbatim
// and no evidence event is emitted. Returns s for fluent wiring at the
// composition root.
func (s *Service) WithVendedoresDeCamioneta(
	roster outbound.VendedoresDeCamionetaResolver,
	usuarios outbound.VendedorUsuarioEmailResolver,
) *Service {
	s.vendedoresRoster = roster
	s.vendedoresUsuarios = usuarios
	return s
}

// resolucionVendedores is the outcome of one attempt to let the roster decide
// a venta's vendedores. It is what the evidence event is built from.
type resolucionVendedores struct {
	camionetaID      int
	origen           string
	motivo           string
	rosterEmails     []string
	rosterExcluidos  int
	emailsSinUsuario []string
	clienteEmails    []string
	duracion         time.Duration
}

// resolverVendedores decides which vendedores a venta being created carries.
//
// It returns the list to use and, when the roster was actually consultable,
// the evidence of how the decision was made. A nil evidence pointer means the
// feature is not wired and nothing happened — no query, no event, no change
// in behavior.
//
// The contract, in one sentence: the roster wins whenever it produces at
// least one vendedor this API can reference, and the client's list wins in
// every other case. See the package docs on the event type for why.
//
// It NEVER returns an error. Every failure downgrades to the client's list,
// because a venta that cannot be created is strictly worse than a venta whose
// vendedor list came from a phone.
func (s *Service) resolverVendedores(
	ctx context.Context, in CrearVentaInput,
) ([]CrearVentaVendedorInput, *resolucionVendedores) {
	cliente := normalizarVendedores(in.Vendedores)
	if s.vendedoresRoster == nil || s.vendedoresUsuarios == nil {
		return cliente, nil
	}

	res := &resolucionVendedores{
		origen:        domain.OrigenVendedoresCliente,
		clienteEmails: emailsDeVendedores(cliente),
	}

	camioneta, motivo := camionetaDeVenta(in)
	if motivo != "" {
		res.motivo = motivo
		return cliente, res
	}
	res.camionetaID = camioneta

	// ONE deadline for the WHOLE resolution, not one per remote call. Bounding
	// only the roster query leaves the identity lookup that follows it
	// unbounded, and MSP_USUARIOS is read through the same Firebird pool that
	// has wedged before (a cancelled context poisons a connection, and a
	// zombie transaction has held a DELETE for ~30 h). An unbounded call there
	// hangs CrearVenta forever, which is the failure this timeout exists to
	// make impossible — it must not be reachable by a different door.
	ctx, cancel := context.WithTimeout(ctx, vendedoresRosterTimeout)
	defer cancel()

	resueltos := s.resolverConRoster(ctx, camioneta, cliente, res)
	if len(resueltos) == 0 {
		return cliente, res
	}

	res.origen = domain.OrigenVendedoresRoster
	res.motivo = motivoRosterOK
	return resueltos, res
}

// resolverConRoster is the roster half of resolverVendedores: it asks the
// roster, turns its emails into identities, and builds the vendedor rows.
//
// It returns nil for every outcome that must fall back, having recorded the
// reason on res. It NEVER returns an error and NEVER panics: a panic in
// either adapter is recovered here, because an unrecovered one unwinds
// CrearVenta and answers 500 — a venta destroyed by a remote roster, which is
// the single outcome the safety net exists to prevent.
//
//nolint:nonamedreturns // the named result is what the deferred recover writes.
func (s *Service) resolverConRoster(
	ctx context.Context,
	camioneta int,
	cliente []CrearVentaVendedorInput,
	res *resolucionVendedores,
) (resueltos []CrearVentaVendedorInput) {
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "ventas.vendedores_resolucion_panico",
				"camioneta_id", camioneta,
				"panic", fmt.Sprint(r),
				"stack", string(debug.Stack()))
			res.motivo = motivoResolucionPanico
			resueltos = nil
		}
	}()

	roster, err := s.consultarRoster(ctx, camioneta, res)
	if err != nil {
		return nil
	}
	res.rosterExcluidos = roster.ExcluidosSinModuloVentas
	res.rosterEmails = emailsDeRoster(roster.Vendedores)
	if len(res.rosterEmails) == 0 {
		res.motivo = motivoRosterVacio
		return nil
	}
	if len(res.rosterEmails) > maxVendedoresDeRoster {
		slog.WarnContext(ctx, "ventas.vendedores_roster_excede_slots",
			"camioneta_id", camioneta,
			"roster_total", len(res.rosterEmails),
			"maximo", maxVendedoresDeRoster)
		res.motivo = motivoRosterExcedeSlots
		return nil
	}

	usuarios, err := s.vendedoresUsuarios.UsuariosPorEmail(ctx, res.rosterEmails)
	if err != nil {
		slog.WarnContext(ctx, "ventas.vendedores_usuarios_fallo",
			"camioneta_id", camioneta, "error", err)
		res.motivo = motivoUsuariosFallo
		return nil
	}

	construidos, sinUsuario := construirVendedores(roster.Vendedores, usuarios, cliente)
	res.emailsSinUsuario = sinUsuario
	if len(construidos) == 0 {
		res.motivo = motivoSinUsuarios
		return nil
	}
	if err := validarVendedoresDeRoster(construidos); err != nil {
		slog.WarnContext(ctx, "ventas.vendedores_roster_invalido",
			"camioneta_id", camioneta, "error", err)
		res.motivo = motivoRosterInvalido
		return nil
	}
	return construidos
}

// validarVendedoresDeRoster proves, BEFORE the aggregate is built, that every
// roster-derived row is a value the venta may hold.
//
// The rows the phone sends were typed into a bounded form; the ones the roster
// produces carry whatever an operator wrote into a Firestore document the
// desktop owns. Without this check a 300-character NOMBRE, or one holding a
// control character, reaches domain.CrearVenta and REJECTS the venta —
// Firestore content deciding whether a sale can be recorded. The check uses
// the domain constructor itself rather than restating its rules, so it cannot
// drift from what CrearVenta will do a few lines later.
func validarVendedoresDeRoster(in []CrearVentaVendedorInput) error {
	for _, v := range in {
		if _, err := domain.NewVendedorSnapshot(domain.NewVendedorSnapshotParams{
			UsuarioID: v.UsuarioID,
			Email:     v.Email,
			Nombre:    v.Nombre,
		}); err != nil {
			return fmt.Errorf("vendedor %s: %w", v.Email, err)
		}
	}
	return nil
}

// consultarRoster runs the roster query and records how long it took. The
// deadline is the caller's — see resolverVendedores, which bounds the whole
// resolution rather than this one call. A failure is logged and reported
// through res, so the caller only has to decide what to return.
func (s *Service) consultarRoster(
	ctx context.Context, camioneta int, res *resolucionVendedores,
) (outbound.VendedoresDeCamioneta, error) {
	started := s.clock.Now()
	roster, err := s.vendedoresRoster.VendedoresDeCamioneta(ctx, camioneta)
	res.duracion = s.clock.Now().Sub(started)
	if err != nil {
		slog.WarnContext(ctx, "ventas.vendedores_roster_fallo",
			"camioneta_id", camioneta, "error", err)
		res.motivo = motivoRosterFallo
		return outbound.VendedoresDeCamioneta{}, err
	}
	return roster, nil
}

// emitirEvidenciaVendedores drops the resolution evidence on the outbox. It
// is a no-op when res is nil (feature unwired), which is what keeps the
// pre-feature behavior byte-identical.
//
// Emitted AFTER the venta is committed, next to the aggregate's own events,
// so an event never describes a venta that does not exist.
func (s *Service) emitirEvidenciaVendedores(
	ctx context.Context, ventaID, by uuid.UUID, res *resolucionVendedores,
) {
	if res == nil {
		return
	}
	soloRoster, soloCliente := diferenciaEmails(res.rosterEmails, res.clienteEmails)
	ev := domain.NewVendedoresResueltosEvent(domain.VendedoresResueltosPayload{
		VentaID:          ventaID,
		By:               by,
		CamionetaID:      res.camionetaID,
		Origen:           res.origen,
		Motivo:           res.motivo,
		RosterEmails:     res.rosterEmails,
		RosterExcluidos:  res.rosterExcluidos,
		EmailsSinUsuario: res.emailsSinUsuario,
		ClienteEmails:    res.clienteEmails,
		SoloEnRoster:     soloRoster,
		SoloEnCliente:    soloCliente,
		Coinciden:        len(soloRoster) == 0 && len(soloCliente) == 0,
		DuracionMS:       res.duracion.Milliseconds(),
	}, s.clock.Now())
	s.enqueueEvent(ctx, outboxAggregateVenta, ventaID, ev.EventType(), ev.Payload())
}

// camionetaDeVenta derives WHICH camioneta a venta being created belongs to.
//
// The camioneta is the almacén de ORIGEN of the venta's lines. That is not a
// guess: a stand-alone producto is REQUIRED by the domain to carry a positive
// AlmacenOrigen (see validateProductoAlmacenes), the almacén de DESTINO the
// client sends is vestigial (the real destination is the configured almacén
// de exhibición), and the app already treats the origen as a single value per
// venta — buildTraspasoDetallesFromVenta refuses to build the automatic
// traspaso when the lines disagree ("productos_multiples_almacenes_origen").
// Measured against the live catalog on 2026-08-27: every distinct
// CAMIONETA_ASIGNADA in the roster is a real ALMACENES row, and the almacén
// de origen recorded on existing ventas is one of them.
//
// Returns (id, "") when exactly one positive origen is present, and
// (0, motivo) otherwise — no origen at all, or several that disagree. In both
// of those cases asking the roster would mean picking one arbitrarily, so the
// caller keeps the client's list instead.
func camionetaDeVenta(in CrearVentaInput) (int, string) {
	origenes := make(map[int]struct{}, 2)
	for _, c := range in.Combos {
		if c.AlmacenOrigen > 0 {
			origenes[c.AlmacenOrigen] = struct{}{}
		}
	}
	for _, p := range in.Productos {
		// Combo children inherit the parent combo's origen, already counted.
		if p.ComboID != nil || p.AlmacenOrigen == nil || *p.AlmacenOrigen <= 0 {
			continue
		}
		origenes[*p.AlmacenOrigen] = struct{}{}
	}
	switch len(origenes) {
	case 0:
		return 0, motivoCamionetaAusente
	case 1:
		for id := range origenes {
			return id, ""
		}
		return 0, motivoCamionetaAusente // unreachable: len == 1
	default:
		return 0, motivoCamionetaAmbigua
	}
}

// construirVendedores turns the roster's people into vendedor inputs, keeping
// only those with an MSP_USUARIOS row, and returns the roster emails that had
// none (sorted) alongside.
//
// Row ids are REUSED from the client's list whenever the same usuario is
// already there. That keeps a create/retry pair byte-identical for the
// vendedores the phone got right, so nothing about this feature changes what
// a replayed request writes.
//
// The nombre prefers the roster's, falling back to MSP_USUARIOS.NOMBRE when
// the roster carries none — the venta's vendedor snapshot requires a name.
//
// ORDER is not cosmetic here, which is why the result is reordered rather
// than left in the roster's: the slice index becomes MSP_VENTAS_VENDEDORES
// .POSICION, and AplicarVenta maps position k to Microsip's
// LIBRES_CARGOS_CC.VENDEDOR_1/2/3. The roster's own order is whatever
// Firestore returns — document-id order, i.e. Firebase uids — so taking it
// verbatim would decide who Microsip records as the FIRST seller by an
// accident of uid sorting. Anyone the phone also named keeps the phone's
// position; the people only the roster knows follow in roster order.
func construirVendedores(
	roster []outbound.VendedorDeCamioneta,
	usuarios map[string]outbound.UsuarioDeVendedor,
	cliente []CrearVentaVendedorInput,
) ([]CrearVentaVendedorInput, []string) {
	idsCliente := make(map[uuid.UUID]uuid.UUID, len(cliente))
	posCliente := make(map[uuid.UUID]int, len(cliente))
	for i, v := range cliente {
		idsCliente[v.UsuarioID] = v.ID
		if _, dup := posCliente[v.UsuarioID]; !dup {
			posCliente[v.UsuarioID] = i
		}
	}

	out := make([]CrearVentaVendedorInput, 0, len(roster))
	sinUsuario := make([]string, 0)
	vistos := make(map[uuid.UUID]struct{}, len(roster))
	for _, r := range roster {
		// Normalized again rather than trusted: the port asks implementations
		// to return canonical addresses, but a lookup keyed by a canonical
		// address and probed with a raw one is EXACTLY the defect this
		// feature exists to remove, and it must not be reintroduced one layer
		// down by taking an adapter at its word.
		email := domain.NormalizarEmail(r.Email)
		u, ok := usuarios[email]
		if !ok {
			sinUsuario = append(sinUsuario, email)
			continue
		}
		if _, dup := vistos[u.ID]; dup {
			continue
		}
		vistos[u.ID] = struct{}{}

		rowID, reused := idsCliente[u.ID]
		if !reused {
			rowID = uuid.New()
		}
		nombre := nombreDeVendedor(r.Nombre, u.Nombre, email)
		out = append(out, CrearVentaVendedorInput{
			ID:        rowID,
			UsuarioID: u.ID,
			Email:     email,
			Nombre:    nombre,
		})
	}
	sort.Strings(sinUsuario)
	ordenarPorPosicionDelCliente(out, posCliente)
	return out, sinUsuario
}

// ordenarPorPosicionDelCliente stable-sorts the resolved vendedores so the
// people the phone also named keep the phone's order and come first.
//
// See construirVendedores: position becomes POSICION becomes Microsip's
// VENDEDOR_1/2/3, so this is the difference between "the seller the phone put
// first is Microsip's first seller" and "whoever Firestore happened to return
// first is".
func ordenarPorPosicionDelCliente(in []CrearVentaVendedorInput, pos map[uuid.UUID]int) {
	rango := func(v CrearVentaVendedorInput) int {
		if p, ok := pos[v.UsuarioID]; ok {
			return p
		}
		// Everybody the phone did not name sorts after everybody it did,
		// keeping their relative roster order (the sort is stable).
		return len(pos) + 1
	}
	sort.SliceStable(in, func(i, j int) bool { return rango(in[i]) < rango(in[j]) })
}

// nombreDeVendedor picks the display name for a roster-derived vendedor,
// preferring the roster's own, then the MSP_USUARIOS one, and finally the
// address itself.
//
// Every candidate goes through domain.SanitizarNombreVendedor, so a name that
// the venta could not hold — blank, over-long, carrying a control character —
// degrades to the next candidate instead of rejecting the venta. The address
// is the last resort rather than a nicety: a vendedor whose name is unusable
// everywhere must still appear on the venta, because dropping him silently is
// the exact defect this feature was built to end.
func nombreDeVendedor(candidatos ...string) string {
	for _, c := range candidatos {
		if n := domain.SanitizarNombreVendedor(c); n != "" {
			return n
		}
	}
	return ""
}

// normalizarVendedores returns a copy of the client's vendedores with every
// email in canonical form. This is the client half of the two-sided
// normalization: the roster half happens inside the adapter.
func normalizarVendedores(in []CrearVentaVendedorInput) []CrearVentaVendedorInput {
	out := make([]CrearVentaVendedorInput, len(in))
	copy(out, in)
	for i := range out {
		out[i].Email = domain.NormalizarEmail(out[i].Email)
	}
	return out
}

// emailsDeVendedores projects the canonical, deduplicated, sorted email set of
// a vendedor list.
func emailsDeVendedores(in []CrearVentaVendedorInput) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, v := range in {
		if v.Email == "" {
			continue
		}
		if _, dup := seen[v.Email]; dup {
			continue
		}
		seen[v.Email] = struct{}{}
		out = append(out, v.Email)
	}
	sort.Strings(out)
	return out
}

// emailsDeRoster projects the canonical, deduplicated, sorted email set the
// roster returned. The adapter already normalizes, but the set is rebuilt
// here so a sloppy implementation cannot smuggle a duplicate or a raw address
// into the evidence.
func emailsDeRoster(in []outbound.VendedorDeCamioneta) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, v := range in {
		email := domain.NormalizarEmail(v.Email)
		if email == "" {
			continue
		}
		if _, dup := seen[email]; dup {
			continue
		}
		seen[email] = struct{}{}
		out = append(out, email)
	}
	sort.Strings(out)
	return out
}

// diferenciaEmails returns (only in a, only in b), both sorted. Inputs are
// already sorted sets; the result is what makes a disagreement legible
// instead of a pair of totals that happen not to match.
func diferenciaEmails(a, b []string) ([]string, []string) {
	inB := make(map[string]struct{}, len(b))
	for _, e := range b {
		inB[e] = struct{}{}
	}
	inA := make(map[string]struct{}, len(a))
	for _, e := range a {
		inA[e] = struct{}{}
	}
	soloA := make([]string, 0)
	for _, e := range a {
		if _, ok := inB[e]; !ok {
			soloA = append(soloA, e)
		}
	}
	soloB := make([]string, 0)
	for _, e := range b {
		if _, ok := inA[e]; !ok {
			soloB = append(soloB, e)
		}
	}
	return soloA, soloB
}
