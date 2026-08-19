//nolint:misspell // Spanish vocabulary (venta, fase) by convention.
package ventfb

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// outboxAggregateVenta is the AGGREGATE value the ventas module stamps on its
// own outbox rows. Duplicated as a literal here (the canonical copy lives in
// the app service) so the fb adapter does not import the app layer.
const outboxAggregateVenta = "venta"

// FaseRepo implements outbound.FaseResolver over the platform outbox
// (MSP_OUTBOX_EVENTS), which is the only place that records WHEN a venta
// changed fase and WHICH fases it went through — there is no revisada_at
// column, UPDATED_AT moves on every edit, and a cancelación overwrites the
// situacion the venta had reached.
type FaseRepo struct {
	pool *firebird.Pool
}

// NewFaseRepo builds a FaseRepo wired to the given pool.
func NewFaseRepo(pool *firebird.Pool) *FaseRepo {
	return &FaseRepo{pool: pool}
}

// Compile-time check: FaseRepo satisfies the outbound port.
var _ outbound.FaseResolver = (*FaseRepo)(nil)

// faseEventRow is one phase-changing outbox row as read from Firebird.
type faseEventRow struct {
	ventaID    uuid.UUID
	eventType  string
	occurredAt time.Time
}

// FasesPorVenta returns, for every requested venta that has at least one
// phase-changing event, WHEN it entered its current fase and HOW FAR it ever
// got. Ventas without such an event are absent from the map; the caller
// renders nothing rather than a wrong date or a wrong ring.
//
// The whole batch is served by ONE query — and both answers come out of THE
// SAME rows, never a second round trip: duplicate ids are collapsed first so
// the placeholder list stays bounded by the unique count, and an empty input
// short-circuits without touching the database.
func (r *FaseRepo) FasesPorVenta(
	ctx context.Context, ids []uuid.UUID,
) (map[uuid.UUID]outbound.FaseDeVenta, error) {
	if len(ids) == 0 {
		return map[uuid.UUID]outbound.FaseDeVenta{}, nil
	}

	unique := make([]uuid.UUID, 0, len(ids))
	seen := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}

	tipos := domain.EventTypesCambioDeFase()
	query := "SELECT AGGREGATE_ID, EVENT_TYPE, CREATED_AT" +
		" FROM MSP_OUTBOX_EVENTS" +
		" WHERE AGGREGATE = ?" +
		" AND EVENT_TYPE IN (" + placeholderList(len(tipos)) + ")" +
		" AND AGGREGATE_ID IN (" + placeholderList(len(unique)) + ")"

	args := make([]any, 0, 1+len(tipos)+len(unique))
	args = append(args, outboxAggregateVenta)
	for _, t := range tipos {
		args = append(args, t)
	}
	for _, id := range unique {
		args = append(args, id.String())
	}

	rows, err := r.readFaseEvents(ctx, query, args)
	if err != nil {
		return nil, err
	}
	return reduceFases(rows), nil
}

// readFaseEvents runs the batched query and decodes every row. CHAR(36)
// columns are right-padded by Firebird, so the id is trimmed before parsing;
// CREATED_AT is a wall-clock TIMESTAMP read back as UTC.
func (r *FaseRepo) readFaseEvents(
	ctx context.Context, query string, args []any,
) ([]faseEventRow, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]faseEventRow, 0, len(args))
	for rows.Next() {
		var (
			rawID        string
			rawEventType string
			rawCreatedAt any
		)
		if scanErr := rows.Scan(&rawID, &rawEventType, &rawCreatedAt); scanErr != nil {
			return nil, firebird.MapError(scanErr)
		}
		ventaID, parseErr := uuid.Parse(strings.TrimSpace(rawID))
		if parseErr != nil {
			// AGGREGATE_ID is CHAR(36) and only ever holds UUIDs we wrote.
			return nil, firebird.MapError(parseErr)
		}
		createdAt, timeErr := firebird.ScanUTCTime(rawCreatedAt)
		if timeErr != nil {
			return nil, firebird.MapError(timeErr)
		}
		out = append(out, faseEventRow{
			ventaID:    ventaID,
			eventType:  strings.TrimSpace(rawEventType),
			occurredAt: createdAt,
		})
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, firebird.MapError(rowsErr)
	}
	return out, nil
}

// reduceFaseDesde keeps, per venta, the newest phase-changing event. The
// event-type filter is applied AGAIN here on purpose: the SQL WHERE only
// narrows what travels over the wire, while domain.EsEventoDeCambioDeFase
// stays the single authority on what counts as a fase change (CLAUDE.md §1 —
// the rule lives in Go, never in the database).
func reduceFaseDesde(rows []faseEventRow) map[uuid.UUID]time.Time {
	out := make(map[uuid.UUID]time.Time, len(rows))
	for _, row := range rows {
		if !domain.EsEventoDeCambioDeFase(row.eventType) {
			continue
		}
		if current, ok := out[row.ventaID]; ok && !row.occurredAt.After(current) {
			continue
		}
		out[row.ventaID] = row.occurredAt
	}
	return out
}

// reduceFaseAlcanzada keeps, per venta, the HIGHEST fase it ever reached.
//
// This is NOT reduceFaseDesde with a different accessor, and the two must not
// be merged into one "obviously equivalent" pass — they disagree on two rows
// on purpose:
//
//   - venta.regresada_a_borrador MOVES fase_desde (the clock restarts where
//     the venta actually stands) but must NOT lower fase_alcanzada: the
//     maximum is a ceiling, and mapping the event to fase 1 makes that
//     automatic.
//   - venta.cancelada MOVES fase_desde but carries no fase at all, so it
//     contributes nothing here: cancelar no es avanzar, y tampoco borra el
//     avance. That is the whole reason this reduction exists — a venta
//     cancelled while in revisada must still report 2.
//
// Ordering is irrelevant here (a maximum is order-free) whereas fase_desde
// depends entirely on it — a second reason the two passes are not the same
// fold. Ventas whose rows carry no fase are absent from the map: zero is
// never reported as a fase.
func reduceFaseAlcanzada(rows []faseEventRow) map[uuid.UUID]int {
	out := make(map[uuid.UUID]int, len(rows))
	for _, row := range rows {
		fase, ok := domain.FaseDelEvento(row.eventType)
		if !ok {
			continue
		}
		if current, seen := out[row.ventaID]; seen && fase <= current {
			continue
		}
		out[row.ventaID] = fase
	}
	return out
}

// reduceFases runs both reductions over the SAME rows and joins them per
// venta. Membership follows fase_desde: any row that changed fase puts the
// venta in the map, even when none of its rows carries a fase number (a lone
// venta.cancelada), in which case Alcanzada stays zero — unknown, not one.
func reduceFases(rows []faseEventRow) map[uuid.UUID]outbound.FaseDeVenta {
	desde := reduceFaseDesde(rows)
	alcanzada := reduceFaseAlcanzada(rows)
	out := make(map[uuid.UUID]outbound.FaseDeVenta, len(desde))
	for ventaID, t := range desde {
		out[ventaID] = outbound.FaseDeVenta{Desde: t, Alcanzada: alcanzada[ventaID]}
	}
	return out
}

// placeholderList builds "?,?,?" for n > 0 parameters. Callers guarantee n is
// positive; the string carries no caller input, only positional markers.
func placeholderList(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
