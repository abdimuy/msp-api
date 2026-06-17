//nolint:misspell // Spanish domain vocabulary (directorio, clientes, etc.) by project convention.
package outbound

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// MaxTotalHitsDirectorio is the pagination cap shared between the Meilisearch
// index settings (PaginationMaxTotalHits) and the cursor-emission guard in the
// app layer. The padron is ~38k active clients; 50000 gives comfortable headroom.
// Both sites must use this constant so they stay in sync automatically.
const MaxTotalHitsDirectorio = 50_000

// DirectorioDoc is the ports-level contract document for the Meilisearch
// directory index. It is a plain struct with no JSON tags and no meilisearch
// import — infra maps it to the wire shape (clientessearch.ClienteDoc).
//
// Fields mirror clientessearch.ClienteDoc one-to-one so the mapping in the
// infra adapter is trivial. Pulse-derived fields are zero-valued when
// TienePulso is false.
type DirectorioDoc struct {
	// Identity fields from domain.Cliente.
	ClienteID  int
	Nombre     string
	ZonaID     int
	ZonaNombre string // display name for the zone; not searchable/filterable
	CobradorID int
	Estatus    string
	Telefono   string

	// Address fields from domain.Direccion.
	Direccion          string // searchable full-text (calle + colonia + poblacion)
	DireccionCalle     string
	DireccionColonia   string
	DireccionPoblacion string
	DireccionCorta     string // Direccion.Corta() one-liner

	// Balance.
	Saldo    decimal.Decimal
	ConSaldo bool

	// Pulse-derived fields. Zero when TienePulso is false.
	Score           int
	Segmento        string
	EstadoPago      string
	RecenciaDias    int
	Frecuencia      int
	Monetary        decimal.Decimal
	NextBestProduct string
	TienePulso      bool

	// Cobranza intelligence signals (B2). Zero/empty when TienePulso is false.
	TierRiesgo      string // AL_DIA | VIGILANCIA | EN_RIESGO | CRITICO
	PctPagosATiempo decimal.Decimal
	FechaProxPago   time.Time // zero when no cadence

	// Credit-risk signals (R3). Zero/empty when TienePulso is false or client
	// has no credit relationship (contado clients).
	BandaCredito string // BAJO | MEDIO | ALTO | CRITICO; "" when no aplica
	ScoreCredito int    // 0–100, higher = lower risk; 0 when no aplica
}

// DirectorioQuery carries all parameters for a single Meilisearch directory
// search. The app layer builds this from BuscarClientesInput and passes it to
// DirectoryIndex.Buscar; the infra adapter translates it to SearchParams.
type DirectorioQuery struct {
	// Q is the full-text query string. Empty means browse (no text search).
	Q string
	// ZonaClienteID restricts to a specific zone. Nil = no filter.
	ZonaClienteID *int
	// CobradorID restricts to a specific cobrador. Nil = no filter.
	CobradorID *int
	// ConSaldo when true restricts to clients with outstanding balance > 0.
	ConSaldo bool
	// Segmento restricts to an exact analytics segment label. Empty = no filter.
	Segmento string
	// EstadoPago restricts to an exact payment-state label. Empty = no filter.
	EstadoPago string
	// ScoreMin keeps only items whose pulse Score >= this value. Nil = no filter.
	ScoreMin *int
	// TierRiesgo restricts to a specific cobranza risk tier (exact match). Empty = no filter.
	TierRiesgo string
	// BandaCredito restricts to a specific credit-risk band (exact match). Empty = no filter.
	// Accepted values: BAJO, MEDIO, ALTO, CRITICO. Clients with no credit relationship
	// index banda_credito="" and will never match a non-empty BandaCredito filter.
	BandaCredito string
	// SortBy is the sort column (e.g. "nombre", "saldo", "score"). Empty means
	// default order (nombre:asc when Q is empty, Meilisearch relevance when Q is set).
	SortBy string
	// SortOrder is "asc" (default) or "desc".
	SortOrder string
	// Offset is the 0-based result index to start from (for offset pagination).
	Offset int
	// Limit is the maximum number of results to return. Default 50 if zero.
	Limit int
}

// DirectorioResultado is the result of a DirectoryIndex.Buscar call.
type DirectorioResultado struct {
	// Items is the page of matched documents.
	Items []DirectorioDoc
	// Facets is the per-attribute value counts returned by Meilisearch.
	// Keys are attribute names (e.g. "zona_id", "segmento"); inner keys are
	// the facet values, inner ints are the hit counts.
	Facets map[string]map[string]int
	// Total is the estimated total number of hits for this query (may be
	// capped by the index's PaginationMaxTotalHits).
	Total int
}

// DirectoryIndex is the outbound port for the Meilisearch customer directory
// index. The implementation lives in infra/clientessearch.
type DirectoryIndex interface {
	// Buscar executes a directory search against the Meilisearch index and
	// returns a page of matched documents together with facet counts.
	// Returns an apperror with KindServiceUnavailable when Meilisearch is
	// not configured or transiently unavailable — callers must surface this
	// as HTTP 503 with no SQL fallback.
	Buscar(ctx context.Context, q DirectorioQuery) (DirectorioResultado, error)

	// Reconciliar bulk-upserts the full active client set into the index.
	// It is an additive reconcile only: clients that transition out of ESTATUS
	// A/B are near-zero in practice and out-of-scope for deletion here.
	Reconciliar(ctx context.Context, docs []DirectorioDoc) error
}
