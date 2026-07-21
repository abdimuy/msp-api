//nolint:misspell // Spanish domain vocabulary (cliente) by project convention.
package reactivacionfb

import (
	"context"
	"database/sql"
	"errors"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// Compile-time check: Repo must satisfy outbound.ClienteFactsReader. This
// file reads the per-cliente snapshot fields off MSP_RX_COHORTE.
var _ outbound.ClienteFactsReader = (*Repo)(nil)

// GetFacts returns the copiloto-relevant facts for clienteID, or (nil, nil)
// when the cliente is not in the MSP_RX_COHORTE snapshot.
func (r *Repo) GetFacts(ctx context.Context, clienteID int) (*outbound.ClienteFacts, error) {
	var result *outbound.ClienteFacts
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		row := q.QueryRowContext(ctx, selectClienteFacts, clienteID)

		var nombre, segmento string
		var telefono sql.NullString
		serr := row.Scan(&nombre, &segmento, &telefono)
		if errors.Is(serr, sql.ErrNoRows) {
			return nil
		}
		if serr != nil {
			return firebird.MapError(serr)
		}
		result = &outbound.ClienteFacts{
			Nombre:   nombre,
			Segmento: segmento,
			Telefono: nullStringVal(telefono),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
