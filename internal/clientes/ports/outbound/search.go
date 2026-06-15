//nolint:misspell // Spanish domain vocabulary (clientes, buscar, etc.) by project convention.
package outbound

import "context"

// SearchDoc is the unit of data indexed per client in the in-process search index.
// Texto holds any pre-tokenized text the index should match against (e.g. name,
// phone, address fragments). The index treats it as opaque.
type SearchDoc struct {
	ClienteID int
	Texto     string
}

// SearchIndex is the in-process full-text search port for the clientes directory.
// The implementation (e.g. a bleve or trie-backed index) is wired at the
// composition root. EstaListo guards reads during the initial warm-up phase —
// callers fall back to BuscarClienteIDsBasico on the repo when false.
type SearchIndex interface {
	// Buscar returns up to limit client IDs whose indexed text matches query.
	// Returns an empty slice (never nil) when there are no matches.
	Buscar(ctx context.Context, query string, limit int) ([]int, error)

	// Reconciliar replaces the index contents with docs. Called during
	// background refresh to keep the index consistent with the database.
	Reconciliar(ctx context.Context, docs []SearchDoc) error

	// EstaListo reports whether the index has been loaded and is safe to query.
	EstaListo() bool
}
