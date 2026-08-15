//nolint:misspell // Spanish vocabulary (ciudades, estados) per project convention.
package ventfb

import (
	"context"
	"database/sql"
	"sync"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// selectCiudadesCatalogo reads the whole CIUDADES catalog. It is small (70
// rows in production) so it is loaded once and matched in Go, where the same
// normalizer that the tests pin is applied to both sides. Matching in SQL
// would mean reimplementing accent folding in Firebird's collation rules.
const selectCiudadesCatalogo = `
SELECT CIUDAD_ID, NOMBRE, ESTADO_ID
FROM CIUDADES`

// CiudadCatalogoRepo resolves a captured ciudad name against Microsip's
// CIUDADES catalog.
//
// The catalog is cached for the process lifetime: it is a Microsip catalog
// maintained by the office, changing at most a few times a year, and a lookup
// sits on the apply path. A new ciudad becomes visible on the next restart.
type CiudadCatalogoRepo struct {
	pool *firebird.Pool

	mu    sync.RWMutex
	byKey map[string]outbound.CiudadResuelta
}

// NewCiudadCatalogoRepo wires a CiudadCatalogoRepo to the given pool.
func NewCiudadCatalogoRepo(pool *firebird.Pool) *CiudadCatalogoRepo {
	return &CiudadCatalogoRepo{pool: pool}
}

// Compile-time check.
var _ outbound.CiudadCatalogo = (*CiudadCatalogoRepo)(nil)

// Resolver returns the catalog IDs for nombre, or nil when nothing matches.
//
// A nil result is not an error: the caller decides whether a miss blocks the
// apply. Ambiguity is treated as a miss — the catalog carries near-duplicates
// ("TLACHICHUCA" / "TLACHICHUCA, PUE") and guessing which one a vendor meant
// is how a cliente lands in the wrong state.
func (r *CiudadCatalogoRepo) Resolver(ctx context.Context, nombre string) (*outbound.CiudadResuelta, error) {
	if err := r.load(ctx); err != nil {
		return nil, err
	}
	key := domain.NormalizeCiudad(nombre)
	if key == "" {
		return nil, nil //nolint:nilnil // no match, no error — documented contract
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	match, ok := r.byKey[key]
	if !ok {
		return nil, nil //nolint:nilnil // no match, no error — documented contract
	}
	return &match, nil
}

// load fills the cache once. A failed first load is retried on the next call
// rather than being cached as a permanent failure.
func (r *CiudadCatalogoRepo) load(ctx context.Context) error {
	r.mu.RLock()
	loaded := r.byKey != nil
	r.mu.RUnlock()
	if loaded {
		return nil
	}

	rows, err := r.pool.QueryContext(ctx, selectCiudadesCatalogo)
	if err != nil {
		return firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	byKey := make(map[string]outbound.CiudadResuelta)
	ambiguas := make(map[string]bool)
	for rows.Next() {
		var (
			id       int
			nombre   firebird.Win1252
			estadoID sql.NullInt64
		)
		if err := rows.Scan(&id, &nombre, &estadoID); err != nil {
			return firebird.MapError(err)
		}
		key := domain.NormalizeCiudad(string(nombre))
		if key == "" {
			continue
		}
		if _, dup := byKey[key]; dup {
			// Two rows fold onto the same key. Neither wins.
			ambiguas[key] = true
			continue
		}
		entry := outbound.CiudadResuelta{CiudadID: id}
		if estadoID.Valid {
			entry.EstadoID = int(estadoID.Int64)
		}
		byKey[key] = entry
	}
	if err := rows.Err(); err != nil {
		return firebird.MapError(err)
	}
	for key := range ambiguas {
		delete(byKey, key)
	}

	r.mu.Lock()
	r.byKey = byKey
	r.mu.Unlock()
	return nil
}
