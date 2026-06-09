package app

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// actorPayloadKeys are the payload fields, in priority order, that may carry
// the usuario who triggered an event. Different event types name the actor
// differently (created_by on creación, updated_by on transitions, applied_by
// on aplicación, etc.); the first key present and parseable as a UUID wins.
var actorPayloadKeys = []string{
	"applied_by",
	"approved_by",
	"created_by",
	"updated_by",
	"canceled_by",
	"resolved_by",
	"by",
}

// traspasoEventTypes are the inventario events whose payload carries
// almacen_origen / almacen_destino ids worth resolving to names so the
// timeline shows the stock route (camioneta → tienda) instead of opaque ids.
var traspasoEventTypes = map[string]struct{}{
	"traspaso.creado":    {},
	"traspaso.reversado": {},
}

// EventosDeVenta returns the venta's event timeline, oldest first, with each
// event labelled with the usuario who triggered it (when the payload carries
// one). The venta must exist — a miss surfaces ErrVentaNotFound so the HTTP
// layer maps it to 404 rather than returning an empty timeline for a
// non-existent id.
//
// When no event reader is wired (tests, or a deployment that has not opted
// into the timeline) the method returns an empty slice without error: the
// timeline is informational and its absence must not break the venta detail
// screen. Actor names are best-effort: a missing or unwired resolver leaves
// ActorNombre empty without failing the read.
func (s *Service) EventosDeVenta(
	ctx context.Context, ventaID uuid.UUID,
) ([]outbound.VentaEvento, error) {
	// Confirm the venta exists first so we return 404 for unknown ids instead
	// of a misleading empty timeline.
	if _, err := s.ventas.FindByID(ctx, ventaID); err != nil {
		return nil, err
	}
	if s.eventReader == nil {
		return []outbound.VentaEvento{}, nil
	}

	eventos, err := s.eventReader.EventosDeVenta(ctx, ventaID)
	if err != nil {
		return nil, err
	}

	// Extract the actor UUID from each payload, then resolve all of them to
	// names in a single batch.
	actorIDs := make([]uuid.UUID, 0, len(eventos))
	for i := range eventos {
		if id, ok := extractActorID(eventos[i].Payload); ok {
			eventos[i].ActorID = &id
			actorIDs = append(actorIDs, id)
		}
	}
	if len(actorIDs) > 0 && s.usuarioResolver != nil {
		nombres, resolveErr := s.usuarioResolver.NombresPorID(ctx, actorIDs)
		if resolveErr != nil {
			// Actor names are best-effort: log-free degradation keeps the
			// timeline working even if the lookup hiccups. The caller still
			// gets the events + actor ids; only the display names are absent.
			return eventos, nil //nolint:nilerr // intentional best-effort degradation
		}
		for i := range eventos {
			if eventos[i].ActorID == nil {
				continue
			}
			if nombre, ok := nombres[*eventos[i].ActorID]; ok {
				eventos[i].ActorNombre = nombre
			}
		}
	}

	s.enrichTraspasoAlmacenes(ctx, eventos)
	return eventos, nil
}

// enrichTraspasoAlmacenes resolves the almacén ids carried by traspaso events
// (almacen_origen / almacen_destino) to names and injects them back into each
// event's payload as almacen_origen_nombre / almacen_destino_nombre. The
// frontend already reads the traspaso payload (folio, detalles_count, ...), so
// the *_nombre keys are the natural extension — no DTO or struct change needed.
//
// Best-effort throughout: a nil/unwired resolver, a lookup error, or an
// unparseable payload leaves the events untouched. Resolution is batched: every
// almacén id across every traspaso event is resolved in a single query.
func (s *Service) enrichTraspasoAlmacenes(ctx context.Context, eventos []outbound.VentaEvento) {
	if s.almacenResolver == nil {
		return
	}

	// First pass: collect every almacén id referenced by a traspaso event.
	ids := make([]int, 0)
	for i := range eventos {
		if _, ok := traspasoEventTypes[eventos[i].EventType]; !ok {
			continue
		}
		if origen, ok := extractPayloadInt(eventos[i].Payload, "almacen_origen"); ok {
			ids = append(ids, origen)
		}
		if destino, ok := extractPayloadInt(eventos[i].Payload, "almacen_destino"); ok {
			ids = append(ids, destino)
		}
	}
	if len(ids) == 0 {
		return
	}

	nombres, err := s.almacenResolver.NombresPorID(ctx, ids)
	if err != nil {
		// Best-effort: leave traspaso payloads untouched on lookup failure.
		return
	}

	// Second pass: inject the resolved names into each traspaso payload.
	for i := range eventos {
		if _, ok := traspasoEventTypes[eventos[i].EventType]; !ok {
			continue
		}
		eventos[i].Payload = injectAlmacenNombres(eventos[i].Payload, nombres)
	}
}

// injectAlmacenNombres unmarshals the event payload, adds
// almacen_origen_nombre / almacen_destino_nombre for any id present in nombres,
// and re-marshals. Returns the payload unchanged when it is not a JSON object
// or when neither almacén id resolves to a name.
func injectAlmacenNombres(payload json.RawMessage, nombres map[int]string) json.RawMessage {
	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		return payload
	}
	changed := false
	for idKey, nombreKey := range map[string]string{
		"almacen_origen":  "almacen_origen_nombre",
		"almacen_destino": "almacen_destino_nombre",
	} {
		id, ok := payloadIntFromMap(fields, idKey)
		if !ok {
			continue
		}
		if nombre, found := nombres[id]; found {
			fields[nombreKey] = nombre
			changed = true
		}
	}
	if !changed {
		return payload
	}
	enriched, err := json.Marshal(fields)
	if err != nil {
		return payload
	}
	return enriched
}

// extractPayloadInt pulls an integer field from a raw JSON payload. JSON
// numbers decode to float64, so the value is read as float64 then narrowed.
// Returns ok=false when the payload is not an object, the key is absent, or
// the value is not numeric.
func extractPayloadInt(payload json.RawMessage, key string) (int, bool) {
	if len(payload) == 0 {
		return 0, false
	}
	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		return 0, false
	}
	return payloadIntFromMap(fields, key)
}

// payloadIntFromMap reads key from an already-unmarshalled payload map and
// narrows the JSON number (float64) to int. Returns ok=false when the key is
// absent or not a number.
func payloadIntFromMap(fields map[string]any, key string) (int, bool) {
	raw, ok := fields[key]
	if !ok {
		return 0, false
	}
	f, ok := raw.(float64)
	if !ok {
		return 0, false
	}
	return int(f), true
}

// extractActorID pulls the first actor UUID present in the event payload,
// trying the known *_by keys in priority order. Returns ok=false when the
// payload is not a JSON object, carries none of the keys, or the value is not
// a valid UUID.
func extractActorID(payload json.RawMessage) (uuid.UUID, bool) {
	if len(payload) == 0 {
		return uuid.Nil, false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return uuid.Nil, false
	}
	for _, key := range actorPayloadKeys {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			continue
		}
		id, err := uuid.Parse(s)
		if err != nil {
			continue
		}
		return id, true
	}
	return uuid.Nil, false
}
