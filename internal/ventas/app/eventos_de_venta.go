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
	"cancelled_by",
	"resolved_by",
	"by",
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
	return eventos, nil
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
