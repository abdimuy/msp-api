//nolint:misspell // vocabulario español por convención del proyecto.
package visitasfb

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

// Lectura de MSP_VISITAS_IMAGENES (GET /v2/visitas/{id}/imagenes[/{img_id}]).
// Todas las columnas son nuestras: ID/VISITA_ID/STORAGE_*/MIME ASCII y
// DESCRIPCION UTF8 (migración 000063), así que se escanean como string.
const selectVisitaImagenCols = `
	ID, STORAGE_KIND, STORAGE_KEY, MIME, SIZE_BYTES, DESCRIPCION,
	CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY`

const listVisitaImagenesSQL = `
SELECT ` + selectVisitaImagenCols + `
FROM MSP_VISITAS_IMAGENES
WHERE VISITA_ID = ?
ORDER BY CREATED_AT ASC, ID ASC`

// Pertenencia: (ID, VISITA_ID). Una imagen de otra visita es "no encontrada".
const findVisitaImagenSQL = `
SELECT ` + selectVisitaImagenCols + `
FROM MSP_VISITAS_IMAGENES
WHERE ID = ? AND VISITA_ID = ?`

type scanner interface{ Scan(dest ...any) error }

func scanVisitaImagen(row scanner) (*domain.Imagen, error) {
	var (
		id, kind, key, mime, createdBy, updatedBy string
		size                                      int64
		desc                                      sql.NullString
		createdRaw, updatedRaw                    any
	)
	if err := row.Scan(&id, &kind, &key, &mime, &size, &desc, &createdRaw, &updatedRaw, &createdBy, &updatedBy); err != nil {
		return nil, err
	}
	created, err := firebird.ScanUTCTime(createdRaw)
	if err != nil {
		return nil, err
	}
	updated, err := firebird.ScanUTCTime(updatedRaw)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, 3)
	for _, raw := range []string{id, createdBy, updatedBy} {
		u, err := uuid.Parse(raw)
		if err != nil {
			return nil, apperror.NewInternal("visita_imagen_id_invalido", "id inválido en MSP_VISITAS_IMAGENES").WithError(err)
		}
		ids = append(ids, u)
	}
	var d *string
	if desc.Valid {
		v := desc.String
		d = &v
	}
	return domain.HydrateImagen(domain.HydrateImagenParams{
		ID: ids[0], Storage: domain.HydrateImagenStorage(domain.StorageKind(kind), key),
		Mime: mime, SizeBytes: size, Descripcion: d,
		CreatedAt: created, UpdatedAt: updated,
		CreatedBy: ids[1], UpdatedBy: ids[2],
	}), nil
}

// ListImagenes implementa outbound.VisitasImagenesRepo.
func (r *Repo) ListImagenes(ctx context.Context, visitaID uuid.UUID) ([]*domain.Imagen, error) {
	out := []*domain.Imagen{}
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, err := q.QueryContext(ctx, listVisitaImagenesSQL, visitaID.String())
		if err != nil {
			return firebird.MapError(err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			img, err := scanVisitaImagen(rows)
			if err != nil {
				return firebird.MapError(err)
			}
			out = append(out, img)
		}
		return firebird.MapError(rows.Err())
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// FindImagenDeVisita implementa outbound.VisitasImagenesRepo.
func (r *Repo) FindImagenDeVisita(ctx context.Context, visitaID, imagenID uuid.UUID) (*domain.Imagen, error) {
	var img *domain.Imagen
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		var err error
		img, err = scanVisitaImagen(q.QueryRowContext(ctx, findVisitaImagenSQL, imagenID.String(), visitaID.String()))
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrImagenNoEncontrada
		}
		return firebird.MapError(err)
	})
	if err != nil {
		return nil, err
	}
	return img, nil
}
