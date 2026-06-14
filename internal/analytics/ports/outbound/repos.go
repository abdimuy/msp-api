package outbound

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// Page is the generic result wrapper returned by List methods. The winback
// list is bounded (capped by ListWinbackParams.Limit), so R1 does not paginate
// with a cursor; Page simply carries the materialized slice. Total reports how
// many rows matched the filter before Limit was applied, for UI display.
type Page[T any] struct {
	Items []T
	// Total is the count of rows matching the filter before Limit was applied.
	// Populated by the repo; used for UI display.
	Total int
}

// ListWinbackParams controls which rows WinbackRepo.ListCandidatos returns.
//
// Only columns stored in MSP_AN_WINBACK_CANDIDATOS are filterable here.
// Segmento and score are NOT stored — they are computed by the app layer at
// read time after the repo returns. Any filtering by computed fields must
// happen in the app layer after scoring.
type ListWinbackParams struct {
	// Zona restricts results to a specific sales zone. Empty string = no filter.
	Zona string

	// ExcluirControl omits rows where EN_CONTROL = 1 when true.
	ExcluirControl bool

	// Limit caps the number of rows returned. 0 = no explicit cap (repo may
	// apply its own reasonable default).
	Limit int
}

// RefreshState holds the execution state of a named background refresh job.
// It maps 1:1 to a row in MSP_AN_REFRESH_STATE.
type RefreshState struct {
	// Job is the unique job identifier (e.g. "winback_full", "winback_incr").
	Job string

	// LastWatermark is the timestamp up to which the last incremental run
	// processed Microsip anclas. Nil when no successful incremental run has
	// been recorded (i.e. only full refreshes have run, or the table is new).
	LastWatermark *time.Time

	// LastRunAt is the UTC timestamp when the job last started execution.
	LastRunAt time.Time
}

// WinbackRepo persists and retrieves WinbackCandidato projection rows.
//
//nolint:interfacebloat // five methods mandated by the R1 spec; each maps to a distinct operation.
type WinbackRepo interface {
	// UpsertCandidatos inserts or replaces the given candidatos in bulk.
	// The repo matches rows by CLIENTE_ID and updates all mutable fields.
	// Existing EN_CONTROL assignments are NOT overwritten by this call;
	// callers must merge control flags from ExistingControlFlags beforehand.
	UpsertCandidatos(ctx context.Context, candidatos []*domain.WinbackCandidato) error

	// ListCandidatos returns candidatos that match the given params, ordered
	// by MONETARY descending. Segmento and score filtering must be applied by
	// the app layer after calling this method.
	ListCandidatos(ctx context.Context, p ListWinbackParams) (Page[*domain.WinbackCandidato], error)

	// GetRefreshState returns the current execution state for the named job.
	// Returns domain.ErrRefreshStateNotFound when no row exists yet.
	GetRefreshState(ctx context.Context, job string) (RefreshState, error)

	// SaveRefreshState upserts the execution state for st.Job.
	SaveRefreshState(ctx context.Context, st RefreshState) error

	// ExistingControlFlags returns a map of clienteID → en_control for all
	// rows currently in MSP_AN_WINBACK_CANDIDATOS. The refresh command uses
	// this snapshot to carry forward the control assignment of existing
	// clients when building the new candidato set, so a refresh does not
	// accidentally flip the A/B flag.
	ExistingControlFlags(ctx context.Context) (map[int]bool, error)
}
