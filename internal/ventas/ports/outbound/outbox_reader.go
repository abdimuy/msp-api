package outbound

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// VentaEvento is one entry in a venta's event timeline. It is a ventas-owned
// projection of a platform outbox row — the module does not expose the
// dispatcher's delivery internals (attempts, last_error) to operators; only
// what the event WAS, WHEN it happened and WHO triggered it.
type VentaEvento struct {
	// ID is the outbox event's primary key (stable across reads).
	ID uuid.UUID
	// EventType is the canonical event name, e.g. "venta.aprobada".
	EventType string
	// Payload is the event's JSON body. The HTTP layer surfaces selected
	// fields (folio, size, ...) to the operator; the rest is available for
	// debugging.
	Payload json.RawMessage
	// OccurredAt is when the event was recorded (the business write's commit
	// time), in UTC.
	OccurredAt time.Time
	// ActorID is the usuario who triggered the event, extracted from the
	// payload's *_by field. Nil when the event carries no actor (e.g.
	// venta.imagen_adjuntada) or when no resolver is wired.
	ActorID *uuid.UUID
	// ActorNombre is the resolved display name for ActorID. Empty when the
	// actor is unknown, unresolved, or the resolver is not wired.
	ActorNombre string
}

// VentaEventReader returns the chronological event timeline for a venta.
// Implemented in infra over the platform outbox; consumed by the ventas
// query service to power the venta-detail "Historial" view.
type VentaEventReader interface {
	// EventosDeVenta returns every event recorded for the venta, oldest
	// first. Returns an empty slice (not an error) when the venta has no
	// events yet. The ActorID/ActorNombre fields are left empty by the
	// reader — the query service resolves actors via UsuarioNombreResolver.
	EventosDeVenta(ctx context.Context, ventaID uuid.UUID) ([]VentaEvento, error)
}

// UsuarioNombreResolver maps usuario ids to their display names. Used by the
// venta query service to label each event with WHO triggered it. Ids with no
// matching MSP_USUARIOS row are simply absent from the returned map.
type UsuarioNombreResolver interface {
	NombresPorID(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error)
}

// AlmacenNombreResolver maps Microsip almacén ids to their display names. Used
// by the venta query service to surface WHERE a traspaso moved stock (origen →
// destino) on the event timeline, turning opaque ALMACEN_IDs into the camioneta
// / tienda names operators recognize. ALMACENES is a Microsip table readable
// from any fb adapter, so this needs no cross-module dependency. Ids with no
// matching row are simply absent from the returned map.
type AlmacenNombreResolver interface {
	NombresPorID(ctx context.Context, ids []int) (map[int]string, error)
}

// ZonaNombreResolver maps Microsip zona-de-cliente ids to their display names.
// Used by the venta read paths (listing + detail) to surface the zona NAME
// next to the raw ZONA_CLIENTE_ID, so the desktop table does not have to
// resolve it against a local catalog. ZONAS_CLIENTES is a Microsip table
// readable from any fb adapter — same justification as AlmacenNombreResolver
// above — so this needs no cross-module dependency. Ids with no matching row
// are simply absent from the returned map.
type ZonaNombreResolver interface {
	NombresPorID(ctx context.Context, ids []int) (map[int]string, error)
}

// FaseDeVenta is what the outbox timeline can settle about ONE venta's
// fases. Both answers come from the same rows and the same single query, but
// they answer different questions — see FaseResolver.
type FaseDeVenta struct {
	// Desde is when the venta entered the fase it is in RIGHT NOW: the
	// timestamp of its newest phase-changing event, in UTC.
	Desde time.Time
	// Alcanzada is the HIGHEST fase (1..4, see domain.FaseDelEvento) the
	// venta ever reached, regardless of where it stands now. Zero means
	// unknown — the venta's only recorded phase event carries no fase (a
	// lone venta.cancelada) — and the caller must omit the field rather
	// than report a fase the venta cannot be proven to have reached.
	Alcanzada int
}

// FaseResolver answers two questions about each venta's fase, both derived
// from its outbox event timeline and both served by a SINGLE batched query so
// a listing page costs one round trip.
//
//  1. WHEN it entered its current fase (FaseDeVenta.Desde). The desktop
//     listing renders a "Fase" column with the elapsed time and flags the
//     ventas that stopped moving, which neither UPDATED_AT (any edit bumps
//     it, so a venta stuck six days looks fresh) nor a dedicated revisada_at
//     column (it does not exist) can provide.
//  2. HOW FAR it ever got (FaseDeVenta.Alcanzada). A cancelled venta's
//     situacion becomes "cancelada" and erases every trace of the fase it was
//     in; the desktop's progress ring still has to draw the arcs the venta
//     earned before leaving the rail.
//
// See domain.EsEventoDeCambioDeFase for which events count at all, and
// domain.FaseDelEvento for the fase each one places the venta in. Ventas
// without any phase event (captured before the timeline existed) are simply
// absent from the returned map — the caller must leave both fields out rather
// than invent a date or a fase.
type FaseResolver interface {
	FasesPorVenta(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]FaseDeVenta, error)
}
