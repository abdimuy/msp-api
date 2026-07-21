//nolint:misspell // Spanish domain vocabulary by project convention.
package outbound

import (
	"context"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

// ListarCohorteParams controls which MSP_RX_COHORTE rows CohorteRepo.ListarCohorte
// returns.
type ListarCohorteParams struct {
	// Segmento restricts results to one segmento. Empty string = no filter.
	Segmento domain.Segmento

	// SoloTratamiento omits rows where EN_CONTROL = 1 when true (returns only the
	// treatment group). When false, both groups are returned.
	SoloTratamiento bool
}

// CohorteRepo persists and retrieves the MSP_RX_COHORTE snapshot.
type CohorteRepo interface {
	// UpsertCohorte inserts or updates one row per cliente matched by CLIENTE_ID.
	// EN_CONTROL, FUE_CONTACTADO and COHORTE_FECHA are set only on the first
	// INSERT and are NEVER overwritten on subsequent rebuilds — callers must
	// carry those flags forward via ExistingControlFlags / ExistingContactadoFlags.
	UpsertCohorte(ctx context.Context, cohorte []*domain.CohorteCliente) error

	// ListarCohorte returns cohorte rows matching p, ordered by CLIENTE_ID.
	ListarCohorte(ctx context.Context, p ListarCohorteParams) ([]*domain.CohorteCliente, error)

	// ExistingControlFlags returns clienteID → EN_CONTROL for every row currently
	// in MSP_RX_COHORTE, so a rebuild carries forward the A/B assignment.
	ExistingControlFlags(ctx context.Context) (map[int]bool, error)

	// ExistingContactadoFlags returns clienteID → FUE_CONTACTADO for every row
	// currently in MSP_RX_COHORTE, so a rebuild carries forward the channel's
	// contact flag (set by Fase 3).
	ExistingContactadoFlags(ctx context.Context) (map[int]bool, error)
}
