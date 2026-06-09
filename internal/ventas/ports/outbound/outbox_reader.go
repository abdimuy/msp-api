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
