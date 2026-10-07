package garfb

import (
	"context"

	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

var _ outbound.FolioGenerator = (*FolioGenerator)(nil)

const siguienteFolioSQL = `
	SELECT GEN_ID(GEN_MSP_GA_FOLIO, 1)
	FROM RDB$DATABASE`

// FolioGenerator provides the next available warranty folio number.
type FolioGenerator struct {
	pool *firebird.Pool
}

// NewFolioGenerator creates a folio generator backed by Firebird.
func NewFolioGenerator(pool *firebird.Pool) *FolioGenerator {
	return &FolioGenerator{
		pool: pool,
	}
}

// Siguiente returns the next number from the Firebird generator.
func (g *FolioGenerator) Siguiente(ctx context.Context) (int, error) {
	var numero int

	err := firebird.RunInReadTx(ctx, g.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, g.pool.DB)

		if err := q.QueryRowContext(ctx, siguienteFolioSQL).Scan(&numero); err != nil {
			return firebird.MapError(err)
		}

		return nil
	})
	if err != nil {
		return 0, err
	}

	return numero, nil
}
