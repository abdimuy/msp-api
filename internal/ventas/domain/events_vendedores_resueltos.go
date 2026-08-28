//nolint:misspell // ventas vocabulary is Spanish (vendedores, camioneta) per project convention.
package domain

import (
	"time"

	"github.com/google/uuid"
)

// EventTypeVentaVendedoresResueltos is emitted once per created venta, right
// after the server has decided WHICH vendedores the venta carries: the ones
// the fleet roster assigns to the venta's camioneta, or — when the roster
// could not be consulted — the ones the phone sent.
//
// It is pure evidence: nothing downstream branches on it. It exists because
// the decision is invisible otherwise. MSP_OUTBOX_EVENTS is the only history
// this system keeps and nobody purges it, so the row is the record of which
// of the two paths ran, what each side proposed, and whether they agreed.
const EventTypeVentaVendedoresResueltos = "venta.vendedores_resueltos"

// Values for VendedoresResueltosPayload.Origen — WHICH side won.
const (
	// OrigenVendedoresRoster means the roster answered and the venta carries
	// the vendedores it named.
	OrigenVendedoresRoster = "roster"
	// OrigenVendedoresCliente means the venta carries exactly what the phone
	// sent, because the roster could not be consulted or had nothing usable.
	OrigenVendedoresCliente = "cliente"
)

// VendedoresResueltosPayload carries the full evidence of one resolution.
// Every field is populated on every emission — an absent field would make the
// row ambiguous between "not measured" and "measured as zero".
type VendedoresResueltosPayload struct {
	// VentaID is the venta the resolution belongs to.
	VentaID uuid.UUID
	// By is the usuario that created the venta. Named so the timeline's
	// actor extraction (see the app layer's actorPayloadKeys) picks it up.
	By uuid.UUID
	// CamionetaID is the almacén de origen the venta's lines agree on, i.e.
	// the camioneta whose roster was consulted. Zero when it could not be
	// derived — in which case Motivo says so and no query ran.
	CamionetaID int
	// Origen is one of OrigenVendedores*.
	Origen string
	// Motivo is the machine-readable reason for Origen, chosen by the app
	// layer. It is what makes a fallback countable instead of anecdotal.
	Motivo string
	// RosterEmails are the canonical addresses the roster returned for
	// CamionetaID, sorted. Empty when no query ran or it returned nobody.
	RosterEmails []string
	// RosterExcluidos counts roster entries dropped for not carrying the
	// VENTAS module. Kept separate from RosterEmails so an over-eager filter
	// is visible as a number rather than an absence.
	RosterExcluidos int
	// EmailsSinUsuario are roster addresses with no MSP_USUARIOS row, sorted.
	// They cannot become vendedores (there is no id to reference) so they are
	// reported rather than silently dropped.
	EmailsSinUsuario []string
	// ClienteEmails are the canonical addresses the phone sent, sorted.
	ClienteEmails []string
	// SoloEnRoster / SoloEnCliente are the set differences, sorted. Together
	// with Coinciden they answer "did the two sides agree, and if not, how".
	SoloEnRoster  []string
	SoloEnCliente []string
	// Coinciden is true when RosterEmails and ClienteEmails are the same set.
	// False whenever a query ran and the sets differ — including the case the
	// whole feature exists for, where the phone dropped an address.
	Coinciden bool
	// DuracionMS is how long the roster query took, in milliseconds. It is
	// the only way to tell a healthy lookup from one that is creeping toward
	// the timeout.
	DuracionMS int64
}

// VendedoresResueltosEvent is the domain event carrying a
// VendedoresResueltosPayload.
type VendedoresResueltosEvent struct {
	payload    VendedoresResueltosPayload
	occurredAt time.Time
}

// NewVendedoresResueltosEvent constructs the event.
func NewVendedoresResueltosEvent(p VendedoresResueltosPayload, now time.Time) VendedoresResueltosEvent {
	return VendedoresResueltosEvent{payload: p, occurredAt: now}
}

// Compile-time check: the evidence event satisfies the module's Event contract.
var _ Event = VendedoresResueltosEvent{}

// EventType returns the canonical event identifier.
func (e VendedoresResueltosEvent) EventType() string { return EventTypeVentaVendedoresResueltos }

// AggregateID returns the venta ID.
func (e VendedoresResueltosEvent) AggregateID() uuid.UUID { return e.payload.VentaID }

// OccurredAt returns the moment the resolution finished.
func (e VendedoresResueltosEvent) OccurredAt() time.Time { return e.occurredAt }

// Payload returns the serializable event payload. Slice fields are emitted as
// empty arrays rather than null so a consumer never has to distinguish the
// two, and every key is always present for the same reason.
func (e VendedoresResueltosEvent) Payload() map[string]any {
	p := e.payload
	return map[string]any{
		"venta_id":           p.VentaID.String(),
		"by":                 p.By.String(),
		"camioneta_id":       p.CamionetaID,
		"origen":             p.Origen,
		"motivo":             p.Motivo,
		"roster_emails":      emptyIfNil(p.RosterEmails),
		"roster_total":       len(p.RosterEmails),
		"roster_excluidos":   p.RosterExcluidos,
		"emails_sin_usuario": emptyIfNil(p.EmailsSinUsuario),
		"cliente_emails":     emptyIfNil(p.ClienteEmails),
		"cliente_total":      len(p.ClienteEmails),
		"solo_en_roster":     emptyIfNil(p.SoloEnRoster),
		"solo_en_cliente":    emptyIfNil(p.SoloEnCliente),
		"coinciden":          p.Coinciden,
		"duracion_ms":        p.DuracionMS,
	}
}

// emptyIfNil returns an empty slice for a nil input so the marshalled payload
// carries [] instead of null.
func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
