//nolint:misspell // Spanish domain vocabulary (categorias, cliente) by project convention.
package reactivacionfb

import (
	"context"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// Compile-time check: Repo must satisfy outbound.CategoriasClienteReader. This
// file reads the cliente's purchased product lines off the Microsip read-model
// (DOCTOS_PV → DOCTOS_PV_DET → ARTICULOS). Read-only.
var _ outbound.CategoriasClienteReader = (*Repo)(nil)

// CategoriasCompradas returns the DISTINCT LINEA_ARTICULO_IDs clienteID has
// ever bought. A cliente with no purchase history yields an empty (non-nil)
// slice — never an error.
func (r *Repo) CategoriasCompradas(ctx context.Context, clienteID int) ([]int, error) {
	result := make([]int, 0)
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, selectCategoriasCompradas, clienteID)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var lineaID int
			if serr := rows.Scan(&lineaID); serr != nil {
				return firebird.MapError(serr)
			}
			result = append(result, lineaID)
		}
		return firebird.MapError(rows.Err())
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
