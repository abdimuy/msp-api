//nolint:misspell // Spanish domain vocabulary (recurso, zona) per project convention.
package outbound

import (
	"context"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// SyncEpochRepo reads the sync generation counters from MSP_CFG_SYNC_EPOCH.
//
// The epoch is a server-side lever to force mobile clients to resynchronize
// from scratch when what the server PROJECTS changes without the underlying
// row's UPDATED_AT moving (the incremental cursor would otherwise never
// re-deliver those rows). Bumping a row in MSP_CFG_SYNC_EPOCH raises the
// `sync_epoch` the sync endpoints return; the client wipes its cursor when it
// sees a value higher than the one it stored.
type SyncEpochRepo interface {
	// Efectivo returns the effective epoch for (recurso, zonaID): the global
	// row's epoch plus the zone row's epoch, with missing rows counting as 0
	// (see domain.EpochEfectivo).
	//
	// Implementations must treat "no rows" as 0 and not as an error. A
	// transport/SQL failure IS returned as an error; the caller is expected
	// to degrade to 0 rather than fail the sync.
	Efectivo(ctx context.Context, recurso domain.RecursoSync, zonaID int) (int, error)
}
