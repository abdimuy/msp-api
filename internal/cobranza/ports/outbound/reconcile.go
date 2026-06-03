package outbound

import (
	"context"
	"time"
)

// DigestResult is the point-in-time fingerprint of a zone's active rows.
// All four fields are computed within one snapshot transaction so they are
// mutually consistent.
type DigestResult struct {
	// CountActivos is the number of active rows (CANCELADO='N' or
	// CARGO_CANCELADO='N') for the zone.
	CountActivos int
	// IDsXor is the XOR of all PK INTEGER values cast to int64. Combined with
	// CountActivos and IDsSum, the probability of false-match collision
	// approaches zero in practice.
	IDsXor int64
	// IDsSum is the running sum of all PK INTEGER values cast to int64.
	// Exact when count*max_pk < 2^63, which always holds for our PK ranges
	// below 100 M.
	IDsSum int64
	// MaxUpdatedAt is the highest UPDATED_AT in the active set. Zero value means
	// no rows matched (empty zone). The HTTP layer omits this field from the JSON
	// response when it is zero.
	MaxUpdatedAt time.Time
}

// PagosReconcileRepo is the read-only port that exposes digest + ID list for
// sync reconciliation. Its methods execute under a single snapshot transaction
// to guarantee point-in-time consistency. The same concrete struct (*PagosRepo)
// that implements PagosRepo also implements this interface; they are kept
// separate so the HTTP layer can depend only on the surface it needs.
type PagosReconcileRepo interface {
	// Digest returns the point-in-time fingerprint for active pagos in zonaID.
	Digest(ctx context.Context, zonaID int) (DigestResult, error)
	// ListIDs returns active pago IDs with IMPTE_DOCTO_CC_ID > after, ordered
	// ascending. Fetch limit+1 rows internally; if len > limit then has_more=true
	// and the returned slice is trimmed to limit.
	ListIDs(ctx context.Context, zonaID, after, limit int) (ids []int, hasMore bool, err error)
}

// SaldosReconcileRepo is the read-only port that exposes digest + ID list for
// sync reconciliation on the saldos cache. Same shape as PagosReconcileRepo.
type SaldosReconcileRepo interface {
	// Digest returns the point-in-time fingerprint for active saldos in zonaID.
	Digest(ctx context.Context, zonaID int) (DigestResult, error)
	// ListIDs returns active saldo IDs with DOCTO_CC_ID > after, ordered
	// ascending.
	ListIDs(ctx context.Context, zonaID, after, limit int) (ids []int, hasMore bool, err error)
}
