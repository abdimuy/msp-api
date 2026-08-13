//nolint:misspell // Spanish domain vocabulary (recurso, zona) per project convention.
package ventfb

import (
	"context"
	"sync"
	"time"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Compile-time assertion: SyncEpochRepo satisfies the outbound port.
var _ outbound.SyncEpochRepo = (*SyncEpochRepo)(nil)

// DefaultSyncEpochTTL is how long a resolved epoch is served from memory
// before hitting Firebird again.
//
// Why cache at all — measured against the dev DB with an idle pool
// (TestSyncEpochRepo_Efectivo_Latencia, 50 iterations):
//
//	7.14 ms per call uncached   (BEGIN + SELECT + COMMIT + pool acquisition)
//	2.59 ms per query           (SELECT alone, inside an already-open tx)
//	0.00033 ms per call cached
//
// The two-row PK lookup is cheap, but 7 ms of BEGIN/COMMIT plus a POOL
// CONNECTION per sync page is not: Efectivo runs once per page, the mobile
// app paginates a full route in dozens of pages, and this endpoint already
// sees 8–21 s waits when the Firebird pool is saturated — where that 7 ms
// becomes seconds of queueing. Caching buys all of that back for a number
// that changes a handful of times per year.
//
// 30 s is the propagation delay an operator accepts after bumping a row: the
// epoch is an incident lever pulled by hand, not a real-time signal.
const DefaultSyncEpochTTL = 30 * time.Second

// epochKey identifies one cached entry.
type epochKey struct {
	recurso domain.RecursoSync
	zonaID  int
}

// epochEntry is a cached epoch plus the instant it was read.
type epochEntry struct {
	value  int
	readAt time.Time
}

// SyncEpochRepo implements outbound.SyncEpochRepo over MSP_CFG_SYNC_EPOCH,
// with a small TTL cache in front (see DefaultSyncEpochTTL).
//
// The cache is bounded by construction: at most one entry per (recurso,
// zona) pair — two recursos times the number of Microsip zones, a few dozen
// entries total — so it never needs eviction.
type SyncEpochRepo struct {
	pool *firebird.Pool
	ttl  time.Duration

	mu     sync.RWMutex
	cached map[epochKey]epochEntry
}

// NewSyncEpochRepo builds a SyncEpochRepo wired to the given pool, caching
// resolved epochs for DefaultSyncEpochTTL.
func NewSyncEpochRepo(pool *firebird.Pool) *SyncEpochRepo {
	return NewSyncEpochRepoWithTTL(pool, DefaultSyncEpochTTL)
}

// NewSyncEpochRepoWithTTL builds a SyncEpochRepo with an explicit cache TTL.
// Pass 0 to disable caching (every call hits Firebird) — used by tests that
// assert an UPDATE is visible immediately.
func NewSyncEpochRepoWithTTL(pool *firebird.Pool, ttl time.Duration) *SyncEpochRepo {
	return &SyncEpochRepo{
		pool:   pool,
		ttl:    ttl,
		cached: make(map[epochKey]epochEntry),
	}
}

// Efectivo returns the effective epoch for (recurso, zonaID) — the global row
// plus the zone row, missing rows counting as 0 (domain.EpochEfectivo).
//
// A fresh cache entry short-circuits the query. On a Firebird failure the
// last known value is served instead (stale but stable), and only when there
// is none does the error surface to the caller, which degrades to 0. The
// epoch must never be able to fail a sync.
func (r *SyncEpochRepo) Efectivo(ctx context.Context, recurso domain.RecursoSync, zonaID int) (int, error) {
	key := epochKey{recurso: recurso, zonaID: zonaID}
	if v, ok := r.fromCache(key); ok {
		return v, nil
	}

	rows, err := r.query(ctx, recurso, zonaID)
	if err != nil {
		if stale, ok := r.stale(key); ok {
			return stale, nil
		}
		return 0, err
	}

	epoch := domain.EpochEfectivo(rows, zonaID)
	r.store(key, epoch)
	return epoch, nil
}

// query reads the global row and the zone row for recurso in a single
// indexed PK lookup. An empty result set is not an error: it means nothing
// has ever been forced for this recurso, which domain.EpochEfectivo maps
// to 0.
func (r *SyncEpochRepo) query(ctx context.Context, recurso domain.RecursoSync, zonaID int) ([]domain.EpochRow, error) {
	out := make([]domain.EpochRow, 0, 2)
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, `
SELECT ZONA_CLIENTE_ID, EPOCH
FROM MSP_CFG_SYNC_EPOCH
WHERE RECURSO = ?
  AND ZONA_CLIENTE_ID IN (?, ?)`,
			recurso.String(), domain.ZonaEpochGlobal, zonaID)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var row domain.EpochRow
			if scanErr := rows.Scan(&row.ZonaClienteID, &row.Epoch); scanErr != nil {
				return firebird.MapError(scanErr)
			}
			out = append(out, row)
		}
		if serr := rows.Err(); serr != nil {
			return firebird.MapError(serr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// fromCache returns the cached epoch when the entry exists and is younger
// than the TTL. A zero TTL disables the cache entirely.
func (r *SyncEpochRepo) fromCache(key epochKey) (int, bool) {
	if r.ttl <= 0 {
		return 0, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.cached[key]
	if !ok || time.Since(e.readAt) >= r.ttl {
		return 0, false
	}
	return e.value, true
}

// stale returns the last known epoch regardless of age. Used only on the
// error path so a transient Firebird failure keeps the value stable instead
// of dropping it to 0.
func (r *SyncEpochRepo) stale(key epochKey) (int, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.cached[key]
	return e.value, ok
}

// store records a freshly read epoch. Always writes, even when the TTL is 0,
// so the stale-on-error path still has something to serve.
func (r *SyncEpochRepo) store(key epochKey, value int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cached[key] = epochEntry{value: value, readAt: time.Now()}
}
