//nolint:misspell // domain vocabulary is Spanish (traspaso, etc.) per project convention.
package outbound

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

// TraspasoRepo is the persistence port for the Traspaso aggregate. The
// Firebird adapter (infra/ventfb) is the production implementation; tests
// supply an in-memory fake.
type TraspasoRepo interface {
	// Save inserts a new Traspaso into Microsip (DOCTOS_IN + DOCTOS_IN_DET +
	// SUB_MOVTOS_IN + aplica_docto_in) and writes the MSP_VENTAS_TRASPASOS
	// lookup row. Returns the assigned DOCTO_IN_ID on success. The folio is
	// already minted on the aggregate; the repo does not mint anything.
	Save(ctx context.Context, t *domain.Traspaso) (doctoInID int, err error)

	// FindByID loads a Traspaso by its Microsip DOCTO_IN_ID. Returns
	// domain.ErrTraspasoNoEncontrado when no row matches.
	FindByID(ctx context.Context, doctoInID int) (*domain.Traspaso, error)

	// ListByVentaID returns all traspasos linked to the given venta, ordered
	// chronologically. Always returns a non-nil slice (empty when none).
	ListByVentaID(ctx context.Context, ventaID uuid.UUID) ([]*domain.Traspaso, error)

	// MarcarDirectoReversado sets REVERSADO='S' on the MSP_VENTAS_TRASPASOS
	// row for the given DOCTO_IN_ID (only rows with TIPO='directo' are
	// affected). This is called when a directo traspaso is superseded by a
	// new edit cycle. Executes within the caller's ambient transaction.
	MarcarDirectoReversado(ctx context.Context, doctoInID int) error
}
