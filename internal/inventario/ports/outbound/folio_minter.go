//nolint:misspell // domain vocabulary is Spanish (folio, etc.) per project convention.
package outbound

import (
	"context"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

// FolioMinter mints a fresh traspaso folio from Microsip's sequence generator.
// The Firebird adapter calls GEN_ID(GEN_MST_FOLIO, 1) and maps the result to
// the domain Folio VO. Tests supply a counter-backed fake.
type FolioMinter interface {
	// MintFolio allocates the next folio in the sequence and returns it as a
	// validated domain.Folio.
	MintFolio(ctx context.Context) (domain.Folio, error)
}
