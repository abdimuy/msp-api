package garfb

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

var _ outbound.EventoRepo = (*EventoRepo)(nil)

type EventoRepo struct {
	pool *firebird.Pool
}

func NewEventoRepo(pool *firebird.Pool) *EventoRepo {
	return &EventoRepo{
		pool: pool,
	}
}

func (r *EventoRepo) ListarPorGarantia(
	ctx context.Context,
	garantiaID uuid.UUID,
) ([]*domain.Evento, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)

	rows, err := q.QueryContext(
		ctx,
		listarEventosPorGarantiaSQL,
		garantiaID.String(),
	)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	var eventos []*domain.Evento

	for rows.Next() {
		er, err := scanEvento(rows)
		if err != nil {
			return nil, err
		}

		e, err := hydrateEvento(er)
		if err != nil {
			return nil, err
		}

		eventos = append(eventos, e)
	}

	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}

	return eventos, nil
}

func (r *EventoRepo) ObtenerPorClaveIdempotencia(
	ctx context.Context,
	clave string,
) (*domain.Evento, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)

	row := q.QueryRowContext(
		ctx,
		obtenerEventoPorClaveIdempotenciaSQL,
		clave,
	)

	er, err := scanEvento(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}

		return nil, err
	}

	return hydrateEvento(er)
}
