//nolint:misspell // Spanish vocabulary (almacenes, nombres) by convention.
package ventfb

import (
	"context"
	"strings"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// AlmacenNombreRepo implements outbound.AlmacenNombreResolver by reading
// display names from Microsip's ALMACENES table. Used to label venta-event
// timeline entries for traspasos with WHERE the stock moved (camioneta →
// tienda) instead of opaque ALMACEN_IDs.
type AlmacenNombreRepo struct {
	pool *firebird.Pool
}

// NewAlmacenNombreRepo builds an AlmacenNombreRepo wired to the given pool.
func NewAlmacenNombreRepo(pool *firebird.Pool) *AlmacenNombreRepo {
	return &AlmacenNombreRepo{pool: pool}
}

// Compile-time check: AlmacenNombreRepo satisfies the outbound port.
var _ outbound.AlmacenNombreResolver = (*AlmacenNombreRepo)(nil)

// NombresPorID returns a map from almacén id to NOMBRE for every id that has a
// row in ALMACENES. Ids without a row are absent from the map (the caller
// treats them as "unknown almacén"). Duplicate ids are collapsed before the
// query so the placeholder list stays bounded by the unique count. The pool's
// FB_CHARSET=UTF8 makes the server transcode the legacy WIN1252 NOMBRE column,
// so a plain string scan + TrimSpace is enough.
func (r *AlmacenNombreRepo) NombresPorID(
	ctx context.Context, ids []int,
) (map[int]string, error) {
	out := make(map[int]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	unique := make([]int, 0, len(ids))
	seen := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}

	placeholders := strings.Repeat("?,", len(unique))
	placeholders = placeholders[:len(placeholders)-1] // drop trailing comma
	query := "SELECT ALMACEN_ID, NOMBRE FROM ALMACENES WHERE ALMACEN_ID IN (" + placeholders + ")"

	args := make([]any, len(unique))
	for i, id := range unique {
		args[i] = id
	}

	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			id     int
			nombre string
		)
		if scanErr := rows.Scan(&id, &nombre); scanErr != nil {
			return nil, firebird.MapError(scanErr)
		}
		out[id] = strings.TrimSpace(nombre)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, firebird.MapError(rowsErr)
	}
	return out, nil
}
