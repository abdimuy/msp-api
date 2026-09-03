//nolint:misspell // visitas vocabulary is Spanish (imagen, descripcion) per project convention.
package visitasfb

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
	"github.com/abdimuy/msp-api/internal/visitas/ports/outbound"
)

// Compile-time assertion: Repo also satisfies outbound.VisitasImagenesRepo.
// One concrete type backs both ports so the two INSERTs share a pool and,
// more importantly, can share a transaction — the whole point of the atomic
// contract.
var _ outbound.VisitasImagenesRepo = (*Repo)(nil)

const insertVisitaImagenSQL = `
INSERT INTO MSP_VISITAS_IMAGENES (
	ID, VISITA_ID, STORAGE_KIND, STORAGE_KEY, MIME, SIZE_BYTES,
	DESCRIPCION, CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY
) VALUES (
	?, ?, ?, ?, ?, ?,
	?, ?, ?, ?, ?
)`

const findImagenIDsSQL = `
SELECT ID
FROM MSP_VISITAS_IMAGENES
WHERE VISITA_ID = ?`

// InsertImagen persists one imagen belonging to visitaID into
// MSP_VISITAS_IMAGENES (migration 000063).
//
// Every column is bound explicitly, timestamps included: the table has no
// DEFAULT and no trigger, so CREATED_AT/UPDATED_AT/CREATED_BY/UPDATED_BY come
// from the domain entity's audit subrecord (CLAUDE.md §1).
//
// firebird.RunInTx is nesting-aware: called from inside the app service's
// transaction it joins it rather than opening a second one, which is what
// makes visita + imagenes commit or roll back together. Called on its own it
// opens and commits its own transaction.
//
// A primary-key collision maps to [domain.ErrImagenYaExiste] instead of a raw
// constraint error, so a retry that re-sends the same imagen ids is a
// recognizable condition and not a 500.
func (r *Repo) InsertImagen(ctx context.Context, visitaID uuid.UUID, img *domain.Imagen) error {
	return firebird.RunInTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		aud := img.Audit()
		storage := img.Storage()
		_, err := q.ExecContext(
			ctx, insertVisitaImagenSQL,
			img.ID().String(),
			visitaID.String(),
			storage.Kind().String(),
			storage.Key(),
			img.Mime(),
			img.SizeBytes(),
			img.Descripcion(),
			firebird.ToWallClock(aud.CreatedAt()),
			firebird.ToWallClock(aud.UpdatedAt()),
			aud.CreatedBy().String(),
			aud.UpdatedBy().String(),
		)
		if err != nil {
			mapped := firebird.MapError(err)
			var ae *apperror.Error
			if errors.As(mapped, &ae) && ae.Code == "firebird_unique_violation" {
				return domain.ErrImagenYaExiste
			}
			return mapped
		}
		return nil
	})
}

// FindImagenIDs returns the ids already attached to visitaID, in no
// particular order. A visita with no images yields an empty slice, never an
// error — "no images" is the ordinary case, not a miss.
func (r *Repo) FindImagenIDs(ctx context.Context, visitaID uuid.UUID) ([]uuid.UUID, error) {
	var result []uuid.UUID
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, findImagenIDsSQL, visitaID.String())
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()

		out := make([]uuid.UUID, 0)
		for rows.Next() {
			var raw string
			if serr := rows.Scan(&raw); serr != nil {
				return firebird.MapError(serr)
			}
			id, perr := uuid.Parse(raw)
			if perr != nil {
				return apperror.NewInternal(
					"visita_imagen_id_invalido", "id de imagen inválido en la base de datos",
				).WithError(perr)
			}
			out = append(out, id)
		}
		if rerr := rows.Err(); rerr != nil {
			return firebird.MapError(rerr)
		}
		result = out
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
