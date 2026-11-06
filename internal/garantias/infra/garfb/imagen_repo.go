package garfb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/nakagami/firebirdsql"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

var _ outbound.ImagenRepo = (*ImagenRepo)(nil)

// ImagenRepo persists and reads warranty evidence metadata.
type ImagenRepo struct {
	pool *firebird.Pool

	decorateQuerier func(firebird.Querier) firebird.Querier
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

	return insertarImagen(ctx, tx, img)
}

func insertarImagen(
	ctx context.Context,
	q firebird.Querier,
	img *domain.Imagen,
) error {
	_, err := q.ExecContext(
		ctx,
		insertarImagenSQL,
		img.ID().String(),
		img.EventoID().String(),
		string(img.Tipo()),
		img.Ruta(),
		nullableString(img.Description()),
		img.SubidaPor(),
		nullableString(img.Huella()),
		firebird.ToWallClock(img.CreatedAt()),
	)
	if err != nil {
		if esImagenDuplicada(err) {
			return domain.ErrImagenDuplicada
		}

		return firebird.MapError(err)
	}

	return nil
}

// ListarPorEvento returns the images for one event ordered by CREATED_AT and ID.
//
//nolint:dupl // El flujo de lectura es similar al de EventoRepo, pero usa otro dominio y mapper.
func (r *ImagenRepo) ListarPorEvento(
	ctx context.Context,
	eventoID uuid.UUID,
) ([]*domain.Imagen, error) {
	imagenes := make([]*domain.Imagen, 0)

	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		if r.decorateQuerier != nil {
			q = r.decorateQuerier(q)
		}

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

// Obtener returns one image by ID.
func (r *ImagenRepo) Obtener(
	ctx context.Context,
	id uuid.UUID,
) (*domain.Imagen, error) {
	var imagen *domain.Imagen

	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)

		fila, err := scanImagen(q.QueryRowContext(
			ctx,
			obtenerImagenPorIDSQL,
			id.String(),
		))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domain.ErrImagenNoEncontrada
			}

			return err
		}

		imagen, err = hydrateImagen(fila)
		return err
	})
	if err != nil {
		return nil, err
	}

	return imagen, nil
}

func esImagenDuplicada(err error) bool {
	if err == nil {
		return false
	}

	var fbErr *firebirdsql.FbError
	if !errors.As(err, &fbErr) {
		return false
	}

	const duplicateKeyCode = 335544665

	tieneCodigoDuplicado := false
	for _, code := range fbErr.GDSCodes {
		if code == duplicateKeyCode {
			tieneCodigoDuplicado = true
			break
		}
	}

	return tieneCodigoDuplicado &&
		strings.Contains(fbErr.Message, "PK_MSP_GA_IMAGEN")
}

const maxEventosPorConsultaImagenes = 1000

// ListarPorEventos returns the images of the requested events ordered by
// EVENTO_ID, CREATED_AT and ID. An empty list does not touch the database.
func (r *ImagenRepo) ListarPorEventos(
	ctx context.Context,
	eventoIDs []uuid.UUID,
) ([]*domain.Imagen, error) {
	if len(eventoIDs) == 0 {
		return []*domain.Imagen{}, nil
	}

	// Evita repetir filas si el caller manda el mismo evento más de una vez,
	// incluso si los duplicados terminarían en lotes distintos.
	ids := make([]uuid.UUID, 0, len(eventoIDs))
	vistos := make(map[uuid.UUID]struct{}, len(eventoIDs))
	for _, id := range eventoIDs {
		if _, existe := vistos[id]; existe {
			continue
		}
		vistos[id] = struct{}{}
		ids = append(ids, id)
	}

	imagenes := make([]*domain.Imagen, 0)

	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		if r.decorateQuerier != nil {
			q = r.decorateQuerier(q)
		}

		for inicio := 0; inicio < len(ids); inicio += maxEventosPorConsultaImagenes {
			fin := inicio + maxEventosPorConsultaImagenes
			if fin > len(ids) {
				fin = len(ids)
			}

			lote, err := listarImagenesPorEventosLote(ctx, q, ids[inicio:fin])
			if err != nil {
				return err
			}

			imagenes = append(imagenes, lote...)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Cada lote viene ordenado desde Firebird. Si hubo más de un lote,
	// restauramos aquí el orden global prometido por el puerto.
	sort.Slice(imagenes, func(i, j int) bool {
		if imagenes[i].EventoID() != imagenes[j].EventoID() {
			return imagenes[i].EventoID().String() < imagenes[j].EventoID().String()
		}
		if !imagenes[i].CreatedAt().Equal(imagenes[j].CreatedAt()) {
			return imagenes[i].CreatedAt().Before(imagenes[j].CreatedAt())
		}
		return imagenes[i].ID().String() < imagenes[j].ID().String()
	})

	return imagenes, nil
}

func listarImagenesPorEventosLote(
	ctx context.Context,
	q firebird.Querier,
	eventoIDs []uuid.UUID,
) ([]*domain.Imagen, error) {
	marcadores := make([]string, len(eventoIDs))
	args := make([]any, len(eventoIDs))

	for i, id := range eventoIDs {
		marcadores[i] = "?"
		args[i] = id.String()
	}

	query := fmt.Sprintf(
		listarImagenesPorEventosBaseSQL,
		strings.Join(marcadores, ", "),
	)

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	imagenes := make([]*domain.Imagen, 0)

	for rows.Next() {
		fila, err := scanImagen(rows)
		if err != nil {
			return nil, err
		}

		imagen, err := hydrateImagen(fila)
		if err != nil {
			return nil, err
		}

		imagenes = append(imagenes, imagen)
	}

	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}

	return imagenes, nil
}
