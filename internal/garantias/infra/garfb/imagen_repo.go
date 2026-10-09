package garfb

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

var _ outbound.ImagenRepo = (*ImagenRepo)(nil)

// ImagenRepo persists and reads warranty evidence metadata.
type ImagenRepo struct {
	pool *firebird.Pool
}

// NewImagenRepo creates an ImagenRepo backed by Firebird.
func NewImagenRepo(pool *firebird.Pool) *ImagenRepo {
	return &ImagenRepo{
		pool: pool,
	}
}

// Registrar inserts image metadata using the active transaction.
func (r *ImagenRepo) Registrar(
	ctx context.Context,
	img *domain.Imagen,
) error {
	tx, err := firebird.RequireTx(ctx)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(
		ctx,
		insertarImagenSQL,
		img.ID().String(),
		img.EventoID().String(),
		img.Ruta(),
		nullableString(img.Description()),
		img.SubidaPor(),
		firebird.ToWallClock(img.CreatedAt()),
	)
	if err != nil {
		return firebird.MapError(err)
	}

	return nil
}

// ListarPorEvento returns the images for one event ordered by CREATED_AT and ID.
func (r *ImagenRepo) ListarPorEvento(
	ctx context.Context,
	eventoID uuid.UUID,
) ([]*domain.Imagen, error) {
	var imagenes []*domain.Imagen

	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)

		rows, err := q.QueryContext(
			ctx,
			listarImagenesPorEventoSQL,
			eventoID.String(),
		)
		if err != nil {
			return firebird.MapError(err)
		}
		defer func() {
			_ = rows.Close()
		}()

		for rows.Next() {
			fila, err := scanImagen(rows)
			if err != nil {
				return err
			}

			img, err := hydrateImagen(fila)
			if err != nil {
				return err
			}

			imagenes = append(imagenes, img)
		}

		if err := rows.Err(); err != nil {
			return firebird.MapError(err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return imagenes, nil
}
