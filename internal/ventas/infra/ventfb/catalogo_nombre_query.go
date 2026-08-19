//nolint:misspell // Spanish vocabulary (nombres, catalogo) by convention.
package ventfb

import (
	"context"
	"strings"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// catalogoNombreQuery describes a Microsip catalog table that maps an integer
// id to a NOMBRE. Both fields are compile-time constants owned by this
// package — they are interpolated into the SQL text (Firebird cannot
// parameterize identifiers) and must NEVER carry caller-supplied input.
type catalogoNombreQuery struct {
	// table is the Microsip table name, e.g. "ALMACENES".
	table string
	// idColumn is the table's integer primary key, e.g. "ALMACEN_ID".
	idColumn string
}

// nombresPorID returns a map from id to NOMBRE for every id that has a row in
// the catalog table. Ids without a row are absent from the map (callers treat
// them as "unknown"). Duplicate ids are collapsed before the query so the
// placeholder list stays bounded by the unique count. The pool's
// FB_CHARSET=UTF8 makes the server transcode the legacy WIN1252 NOMBRE
// column, so a plain string scan + TrimSpace is enough.
func (c catalogoNombreQuery) nombresPorID(
	ctx context.Context, pool *firebird.Pool, ids []int,
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
	// #nosec G202 -- table/idColumn are package-owned constants, never input.
	query := "SELECT " + c.idColumn + ", NOMBRE FROM " + c.table +
		" WHERE " + c.idColumn + " IN (" + placeholders + ")"

	args := make([]any, len(unique))
	for i, id := range unique {
		args[i] = id
	}

	q := firebird.GetQuerier(ctx, pool.DB)
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
