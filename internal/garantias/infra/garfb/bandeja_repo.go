package garfb

import (
	"context"
	"strings"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/pagination"
)

var _ outbound.BandejaRepo = (*BandejaRepo)(nil)

const (
	defaultBandejaLimit = 20
	maxBandejaLimit     = 100
)

func normalizarLimite(limite int) int {
	if limite <= 0 {
		return defaultBandejaLimit
	}

	if limite > maxBandejaLimit {
		return maxBandejaLimit
	}

	return limite
}

func decodificarCursor(raw string) (pagination.Cursor, error) {
	cursor, err := pagination.DecodeCursor(raw)
	if err != nil {
		return pagination.Cursor{}, apperror.NewValidation(
			"warranty_cursor_invalid",
			"el cursor de garantías no es válido",
		).WithError(err)
	}

	if raw != "" &&
		(cursor.Schema != 1 || cursor.ID == nil || cursor.UpdatedAt.IsZero()) {
		return pagination.Cursor{}, apperror.NewValidation(
			"warranty_cursor_invalid",
			"el cursor de garantías no es válido",
		)
	}

	// UpdatedAt guarda CREATED_AT porque la bandeja pagina por CREATED_AT e ID.
	return cursor, nil
}

// BandejaRepo implements the read side of the warranty list.
type BandejaRepo struct {
	pool *firebird.Pool
}

// NewBandejaRepo creates a BandejaRepo backed by Firebird.
func NewBandejaRepo(pool *firebird.Pool) *BandejaRepo {
	return &BandejaRepo{
		pool: pool,
	}
}

func buildBandejaWhere(f outbound.ListarGarantiasFiltros) (string, []any) {
	var clauses []string
	var args []any

	if f.Estado != nil {
		clauses = append(clauses, "g.ESTADO = ?")
		args = append(args, string(*f.Estado))
	}

	if f.Origen != nil {
		clauses = append(clauses, "g.ORIGEN = ?")
		args = append(args, string(*f.Origen))
	}

	if f.ClienteID != nil {
		clauses = append(clauses, "g.CLIENTE_ID = ?")
		args = append(args, *f.ClienteID)
	}

	if f.Etapa != nil || f.Ubicacion != nil {
		articleClauses := []string{"a.GARANTIA_ID = g.ID"}

		if f.Etapa != nil {
			articleClauses = append(articleClauses, "a.ETAPA = ?")
			args = append(args, string(*f.Etapa))
		}

		if f.Ubicacion != nil {
			articleClauses = append(articleClauses, "a.UBICACION = ?")
			args = append(args, string(*f.Ubicacion))
		}

		clauses = append(
			clauses,
			`EXISTS (
	SELECT 1
	FROM MSP_GA_ARTICULO a
	WHERE `+strings.Join(articleClauses, `
	  AND `)+`
)`,
		)
	}

	if f.Desde != nil {
		clauses = append(clauses, "g.CREATED_AT >= ?")
		args = append(args, firebird.ToWallClock(*f.Desde))
	}

	if f.Hasta != nil {
		clauses = append(clauses, "g.CREATED_AT < ?")
		args = append(args, firebird.ToWallClock(*f.Hasta))
	}

	return strings.Join(clauses, " AND "), args
}

func buildBandejaQuery(
	f outbound.ListarGarantiasFiltros,
	p outbound.Paginacion,
) (string, []any, int, error) {
	limite := normalizarLimite(p.Limite)

	cursor, err := decodificarCursor(p.Cursor)
	if err != nil {
		return "", nil, 0, err
	}

	where, args := buildBandejaWhere(f)

	if p.Cursor != "" {
		cursorWhere := `(g.CREATED_AT < ? OR (g.CREATED_AT = ? AND g.ID < ?))`

		if where == "" {
			where = cursorWhere
		} else {
			where += " AND " + cursorWhere
		}

		createdAt := firebird.ToWallClock(cursor.UpdatedAt)

		args = append(
			args,
			createdAt,
			createdAt,
			cursor.ID.String(),
		)
	}

	query := listarGarantiasBaseSQL

	if where != "" {
		query += "\nWHERE " + where
	}

	query += "\nORDER BY g.CREATED_AT DESC, g.ID DESC"
	query += "\nROWS ?"

	// Se pide una fila extra para saber si existe otra página.
	args = append(args, limite+1)

	return query, args, limite, nil
}

func (r *BandejaRepo) Listar(
	ctx context.Context,
	f outbound.ListarGarantiasFiltros,
	p outbound.Paginacion,
) (outbound.Pagina[*domain.Garantia], error) {
	query, args, limite, err := buildBandejaQuery(f, p)
	if err != nil {
		return outbound.Pagina[*domain.Garantia]{}, err
	}

	var pagina outbound.Pagina[*domain.Garantia]

	err = firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)

		filas, err := consultarGarantias(ctx, q, query, args...)
		if err != nil {
			return err
		}

		hayMas := len(filas) > limite
		if hayMas {
			filas = filas[:limite]
		}

		ids := make([]string, 0, len(filas))
		for _, fila := range filas {
			ids = append(ids, fila.id)
		}

		articulos, err := listarArticulosPorGarantias(ctx, q, ids)
		if err != nil {
			return err
		}

		items := make([]*domain.Garantia, 0, len(filas))

		for _, fila := range filas {
			g, err := hydrateGarantia(
				fila,
				articulos[fila.id],
			)
			if err != nil {
				return err
			}

			items = append(items, g)
		}

		var siguienteCursor string

		if hayMas && len(items) > 0 {
			ultimo := items[len(items)-1]
			ultimoID := ultimo.ID()

			siguienteCursor, err = pagination.EncodeCursor(
				pagination.Cursor{
					// UpdatedAt contiene CREATED_AT para esta paginación.
					UpdatedAt: ultimo.CreatedAt(),
					ID:        &ultimoID,
				},
			)
			if err != nil {
				return err
			}
		}

		pagina = outbound.Pagina[*domain.Garantia]{
			Items:           items,
			SiguienteCursor: siguienteCursor,
		}

		return nil
	})
	if err != nil {
		return outbound.Pagina[*domain.Garantia]{}, err
	}

	return pagina, nil
}

func consultarGarantias(
	ctx context.Context,
	q firebird.Querier,
	query string,
	args ...any,
) ([]*garantiaRow, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var resultado []*garantiaRow

	for rows.Next() {
		fila, err := scanGarantia(rows)
		if err != nil {
			return nil, err
		}

		resultado = append(resultado, fila)
	}

	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}

	return resultado, nil
}

func listarArticulosPorGarantias(
	ctx context.Context,
	q firebird.Querier,
	garantiaIDs []string,
) (map[string][]*domain.Articulo, error) {
	query := listarArticulosPorGarantiasBaseSQL
	var args []any

	if len(garantiaIDs) == 0 {
		// La bandeja hace siempre dos consultas por página.
		query += "\nWHERE 1 = 0"
	} else {
		placeholders := strings.TrimSuffix(
			strings.Repeat("?,", len(garantiaIDs)),
			",",
		)

		query += "\nWHERE GARANTIA_ID IN (" + placeholders + ")"

		args = make([]any, 0, len(garantiaIDs))
		for _, id := range garantiaIDs {
			args = append(args, id)
		}
	}

	query += "\nORDER BY CREATED_AT, ID"

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() {
		_ = rows.Close()
	}()

	resultado := make(map[string][]*domain.Articulo)

	for rows.Next() {
		fila, err := scanArticulo(rows)
		if err != nil {
			return nil, err
		}

		articulo, err := hydrateArticulo(fila)
		if err != nil {
			return nil, err
		}

		resultado[fila.garantiaID] = append(
			resultado[fila.garantiaID],
			articulo,
		)
	}

	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}

	return resultado, nil
}
