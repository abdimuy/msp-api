package microsipfb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/abdimuy/msp-api/internal/microsip/domain"
	"github.com/abdimuy/msp-api/internal/microsip/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// AlmacenRepo is the Firebird-backed implementation of
// outbound.AlmacenRepo. The repo materializes the ARTICULOS query template
// at construction time so each ListarArticulos call avoids re-formatting
// the price-list IN clause.
type AlmacenRepo struct {
	pool             *firebird.Pool
	articulosByAlmcQ string
}

// NewAlmacenRepo wires an AlmacenRepo to the supplied pool. priceListIDs
// is the set of PRECIO_EMPRESA_IDs used to filter PRECIOS_ARTICULOS rows;
// the slice must be non-empty (the wiring layer guarantees this via the
// config default), and ids are interpolated as untrusted-but-validated
// ints — never as strings — so SQL injection is impossible.
func NewAlmacenRepo(pool *firebird.Pool, priceListIDs []int) *AlmacenRepo {
	return &AlmacenRepo{
		pool:             pool,
		articulosByAlmcQ: fmt.Sprintf(selectArticulosByAlmacenTpl, intListToInClause(priceListIDs)),
	}
}

// Compile-time check.
var _ outbound.AlmacenRepo = (*AlmacenRepo)(nil)

// intListToInClause renders a slice of ints as a comma-separated SQL
// fragment. An empty slice degrades to "NULL" so the LEFT JOIN matches
// nothing instead of producing a syntax error.
func intListToInClause(ids []int) string {
	if len(ids) == 0 {
		return "NULL"
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, ",")
}

// Listar returns every visible almacen.
func (r *AlmacenRepo) Listar(ctx context.Context) ([]domain.Almacen, error) {
	rows, err := r.pool.QueryContext(ctx, selectAlmacenes)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Almacen
	for rows.Next() {
		a, scanErr := scanAlmacen(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return out, nil
}

// Obtener returns a single almacen by ID, or nil when not found.
func (r *AlmacenRepo) Obtener(ctx context.Context, almacenID int) (*domain.Almacen, error) {
	row := r.pool.QueryRowContext(ctx, selectAlmacenByID, almacenID)
	a, err := scanAlmacen(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // optional pointer pattern: missing row maps to nil, not error.
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ListarArticulos returns articulos with positive existencias for the
// almacen, optionally filtered by name substring.
func (r *AlmacenRepo) ListarArticulos(ctx context.Context, almacenID int, buscar string) ([]domain.ArticuloAlmacen, error) {
	// CONTAINING is case-insensitive in Firebird and treats an empty
	// argument as "match anything", matching the legacy default of "".
	containing, err := firebird.EncodeWin1252(buscar)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	rows, err := r.pool.QueryContext(ctx, r.articulosByAlmcQ, almacenID, containing)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.ArticuloAlmacen
	for rows.Next() {
		var (
			articuloID, lineaID int
			articulo, linea     firebird.Win1252
			existencias         int64
			precios             sql.NullString
		)
		if err := rows.Scan(&articuloID, &articulo, &existencias, &lineaID, &linea, &precios); err != nil {
			return nil, firebird.MapError(err)
		}
		out = append(out, domain.ArticuloAlmacen{
			ArticuloID:      articuloID,
			Articulo:        string(articulo),
			Existencias:     existencias,
			LineaArticuloID: lineaID,
			LineaArticulo:   string(linea),
			Precios:         precios.String,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return out, nil
}

// scanner abstracts QueryRow / Rows so scanAlmacen serves both Obtener and
// Listar (which iterates over *sql.Rows).
type scanner interface {
	Scan(dest ...any) error
}

func scanAlmacen(s scanner) (domain.Almacen, error) {
	var (
		id          int
		nombre      firebird.Win1252
		existencias int64
	)
	if err := s.Scan(&id, &nombre, &existencias); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Almacen{}, sql.ErrNoRows
		}
		return domain.Almacen{}, firebird.MapError(err)
	}
	return domain.Almacen{ID: id, Nombre: string(nombre), Existencias: existencias}, nil
}
