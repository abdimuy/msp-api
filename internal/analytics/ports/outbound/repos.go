package outbound

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

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
//nolint:interfacebloat // eight methods; each maps to a distinct operation.
type WinbackRepo interface {
	// UpsertCandidatos inserts or replaces the given candidatos in bulk.
	// The repo matches rows by CLIENTE_ID and updates all mutable fields.
	// Existing EN_CONTROL assignments are NOT overwritten by this call;
	// callers must merge control flags from ExistingControlFlags beforehand.
	//
	// R1 limitation — no eviction: rows are never deleted from
	// MSP_AN_WINBACK_CANDIDATOS. A client who reactivates between full refreshes
	// stays in the table until the next full refresh overwrites their row. This
	// means ListCandidatos and the Atribucion denominators may include stale
	// rows (clients who are no longer lapsed). Eviction logic is deferred to R2.
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

	// GetCandidato returns the candidato row for clienteID, or
	// domain.ErrWinbackCandidatoNotFound when the client is not materialized
	// (e.g. a client with zero purchase history).
	GetCandidato(ctx context.Context, clienteID int) (*domain.WinbackCandidato, error)

	// ListCandidatosByClienteIDs returns the materialized candidatos for the given
	// clienteIDs. Clients not materialized are simply absent from the result
	// (no error). An empty input returns an empty slice.
	ListCandidatosByClienteIDs(ctx context.Context, clienteIDs []int) ([]*domain.WinbackCandidato, error)

	// ListCandidatosByZona returns ALL materialized candidatos in the given zona
	// (unpaginated; used to build the peer-benchmark cohort). The target client
	// is included in the result; the app layer filters them out.
	ListCandidatosByZona(ctx context.Context, zona string) ([]*domain.WinbackCandidato, error)

	// ContarPagosRecientes returns, per clienteID, the count of real customer
	// payments (abono concepts 87327/155/11, CANCELADO='N' AND APLICADO='S')
	// with FECHA in the half-open interval [desde, hasta). Clients with zero
	// payments in the window are absent from the result map. An empty input
	// returns an empty map.
	//
	// This powers the live computation of the credit feature PAGOS_90D at read
	// time. The materialized PAGOS_90D column is a trailing-90-day count frozen
	// at refresh time, so it goes stale the moment the serving clock diverges
	// from the last refresh; computing it live keeps it consistent with the
	// rest of the recency-aware features (e.g. DIAS_SIN_PAGAR).
	ContarPagosRecientes(ctx context.Context, clienteIDs []int, desde, hasta time.Time) (map[int]int, error)
}

// ─── Cartera repo types ───────────────────────────────────────────────────────

// AgingRow is one row from the per-zone or per-cobrador aging aggregation.
type AgingRow struct {
	ZonaClienteID int
	CobradorID    *int            // nil for zone-level aggregates or clients with no cobrador
	Bucket        string          // one of the domain.BucketAgingDias* constants
	Saldo         decimal.Decimal // sum of SALDO in this bucket (NUMERIC 18,2)
	Conteo        int             // count of active cargo rows in this bucket
}

// VintageRow is one row from the vintage cohort aggregation over MSP_SALDOS_VENTAS.
type VintageRow struct {
	ZonaClienteID int
	CohortMonth   int             // year*12 + int(month), matches domain.VintageCohort
	Saldo         decimal.Decimal // sum of SALDO for this cohort (NUMERIC 18,2)
	Conteo        int             // count of active cargo rows in this cohort
}

// CEIRow is one row from the collection-effectiveness (CEI) aggregation.
type CEIRow struct {
	ZonaClienteID int
	CobradorID    *int            // nil when client has no cobrador assigned
	Importe       decimal.Decimal // sum of IMPORTE collected in the period (NUMERIC 18,2)
	Conteo        int             // count of distinct clientes who paid
}

// CarteraRepo provides aggregated cartera queries over the MSP_SALDOS_VENTAS
// and MSP_PAGOS_VENTAS read-model caches, plus read/write access to
// MSP_AN_CARTERA_SNAPSHOT for point-in-time roll-rate and trend analytics.
//
//nolint:interfacebloat // six methods map to distinct operations; splitting would obscure cohesion.
type CarteraRepo interface {
	// AgingSaldosByZona returns per-zone aging distribution: saldo + conteo per
	// (ZONA_CLIENTE_ID, aging bucket) from active MSP_SALDOS_VENTAS rows
	// (CARGO_CANCELADO='N', SALDO>0). Bucket boundaries match domain.BucketForDays.
	// CobradorID is always nil in the returned rows.
	AgingSaldosByZona(ctx context.Context, today time.Time) ([]AgingRow, error)

	// AgingSaldosByCobrador returns per-cobrador aging distribution: saldo +
	// conteo per (ZONA_CLIENTE_ID, COBRADOR_ID, aging bucket). JOINs CLIENTES
	// to resolve COBRADOR_ID; rows with no cobrador have CobradorID==nil.
	AgingSaldosByCobrador(ctx context.Context, today time.Time) ([]AgingRow, error)

	// VintageSaldos returns saldo aggregated by vintage cohort month
	// (FECHA_CARGO year×12+month ordinal, per domain.VintageCohort) and zone,
	// from active MSP_SALDOS_VENTAS rows only.
	VintageSaldos(ctx context.Context) ([]VintageRow, error)

	// ColeccionCEI returns collection effectiveness per (ZONA_CLIENTE_ID,
	// COBRADOR_ID) from MSP_PAGOS_VENTAS for abono concepts 87327/155/11,
	// CANCELADO='N', APLICADO='S', FECHA in [desde, hasta).
	ColeccionCEI(ctx context.Context, desde, hasta time.Time) ([]CEIRow, error)

	// SaveCarteraSnapshot upserts a batch of snapshot rows into
	// MSP_AN_CARTERA_SNAPSHOT using EXECUTE BLOCK. Zone-level rows
	// (CobradorID==nil) are matched with WHERE COBRADOR_ID IS NULL explicitly
	// because Firebird UNIQUE constraints do not enforce uniqueness for NULLs.
	SaveCarteraSnapshot(ctx context.Context, rows []domain.CarteraSnapshot) error

	// ListRecentSnapshots returns the most recent `limit` snapshot rows ordered
	// FECHA_CORTE DESC, ZONA_CLIENTE_ID ASC, BUCKET ASC. Limit <=0 returns all.
	ListRecentSnapshots(ctx context.Context, limit int) ([]domain.CarteraSnapshot, error)
}
