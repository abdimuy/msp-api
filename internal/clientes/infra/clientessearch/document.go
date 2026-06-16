// Package clientessearch defines the Meilisearch document shape and index
// settings for the clientes directory. It is the clientes-specific companion
// to the generic internal/platform/meilisearch package.
//
// This package is responsible for:
//   - ClienteDoc: the flat JSON document indexed into Meilisearch.
//   - DefaultIndexConfig: the IndexConfig (filterable/sortable/searchable/
//     ranking/faceting/pagination) to apply at boot via EnsureIndex.
//
// It must NOT import domain/, app/, or any other module — only the platform
// meilisearch package for the IndexConfig type.
package clientessearch

import (
	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
)

// ClienteDoc is the flat search document indexed into Meilisearch for each
// active cliente. Field names use snake_case JSON tags (tagliatelle enforced).
//
// Field roles:
//
//	searchable  → included in MEILISEARCH_SEARCHABLE_ATTRIBUTES
//	filterable  → can appear in filter= expressions and facets
//	sortable    → can appear in sort= clauses
//	display     → returned in hits but not searchable/filterable/sortable
type ClienteDoc struct {
	// ID is the Meilisearch primary key. Equals ClienteID cast to string.
	ID string `json:"id"`

	// ClienteID is the Microsip CLIENTE_ID (numeric). Stored for display.
	ClienteID int `json:"cliente_id"`

	// ── Searchable ───────────────────────────────────────────────────────

	// Nombre is the client's full name (NOMBRE in Microsip). Searchable.
	Nombre string `json:"nombre"`

	// Direccion is a combined address string (calle + colonia + poblacion)
	// used for full-text search. Searchable.
	Direccion string `json:"direccion"`

	// DireccionCalle is the raw street line for display.
	DireccionCalle string `json:"direccion_calle"`

	// DireccionColonia is the neighborhood.
	DireccionColonia string `json:"direccion_colonia"`

	// DireccionPoblacion is the city/town.
	DireccionPoblacion string `json:"direccion_poblacion"`

	// ── Filterable (also faceted where noted) ───────────────────────────

	// ZonaID is the delivery zone identifier. Filterable + facetable + sortable.
	ZonaID int `json:"zona_id"`

	// CobradorID is the assigned cobrador user ID. Filterable + facetable.
	CobradorID int `json:"cobrador_id"`

	// ConSaldo indicates whether the client has an outstanding balance > 0.
	// Filterable (boolean facet).
	ConSaldo bool `json:"con_saldo"`

	// Segmento is the RFM-derived customer segment label (e.g. "campeon",
	// "en_riesgo"). Filterable + facetable + sortable (via SegmentoOrden).
	Segmento string `json:"segmento"`

	// EstadoPago is the payment-state label (e.g. "al_dia", "vencido_30",
	// "vencido_60", "vencido_90plus"). Filterable + facetable + sortable
	// (via EstadoPagoOrden).
	EstadoPago string `json:"estado_pago"`

	// Score is the analytics winback score [0–100]. Filterable + sortable.
	Score float64 `json:"score"`

	// RecenciaDias is the number of days since last purchase. Filterable +
	// sortable.
	RecenciaDias int `json:"recencia_dias"`

	// Estatus is the Microsip client status flag (e.g. "activo", "inactivo").
	// Filterable.
	Estatus string `json:"estatus"`

	// ── Sortable numeric ordinals ────────────────────────────────────────

	// SegmentoOrden is the numeric ordinal for SegmentoOrden-based sorting
	// (higher = better segment). Sortable.
	SegmentoOrden int `json:"segmento_orden"`

	// EstadoPagoOrden is the numeric ordinal for payment-state sorting
	// (lower = worse state). Sortable.
	EstadoPagoOrden int `json:"estado_pago_orden"`

	// ── Display-only (not searchable/filterable/sortable) ───────────────

	// Telefono is the primary phone number for display.
	Telefono string `json:"telefono"`

	// DireccionCorta is a short one-line address for list display.
	DireccionCorta string `json:"direccion_corta"`

	// Saldo is the outstanding balance amount. Display + sortable.
	Saldo float64 `json:"saldo"`

	// Frecuencia is the purchase frequency (number of orders in period).
	// Display only.
	Frecuencia int `json:"frecuencia"`

	// Monetary is the total spend in the reference period. Display only.
	Monetary float64 `json:"monetary"`

	// NextBestProduct is the analytics-suggested next product category.
	// Display only.
	NextBestProduct string `json:"next_best_product"`

	// TienePulso indicates whether the analytics pulse record exists for
	// this client. Display only.
	TienePulso bool `json:"tiene_pulso"`
}

// defaultRankingRules is the ordered list of ranking rules applied to the
// clientes index. We include the standard Meilisearch defaults and add
// "sort" after "attribute" so explicit sort clauses take effect.
var defaultRankingRules = []string{
	"words",
	"typo",
	"proximity",
	"attribute",
	"sort",
	"exactness",
}

// searchableAttributes is the ordered list searched for query matches.
// Nombre is first so name hits rank highest.
var searchableAttributes = []string{
	"nombre",
	"direccion",
}

// filterableAttributes lists every attribute that can appear in filter
// expressions or be requested as a facet.
var filterableAttributes = []string{
	"zona_id",
	"cobrador_id",
	"con_saldo",
	"segmento",
	"estado_pago",
	"score",
	"recencia_dias",
	"estatus",
}

// sortableAttributes lists every attribute that can appear in sort clauses.
var sortableAttributes = []string{
	"nombre",
	"saldo",
	"score",
	"segmento_orden",
	"estado_pago_orden",
	"recencia_dias",
	"zona_id",
}

// facetAttributes are the attributes returned in FacetDistribution by default.
// Not a Meilisearch setting — callers pass these in SearchRequest.Facets.
// Exported so the HTTP handler can reference the canonical list.
var facetAttributes = []string{
	"zona_id",
	"cobrador_id",
	"segmento",
	"estado_pago",
}

// FacetAttributes returns the canonical list of facet attributes for the
// clientes index.
func FacetAttributes() []string {
	out := make([]string, len(facetAttributes))
	copy(out, facetAttributes)
	return out
}

// maxTotalHits is the pagination cap for the clientes index. The padron is
// ~38k; 50000 gives comfortable headroom.
const maxTotalHits = 50000

// maxValuesPerFacet is the maximum number of distinct values returned per
// facet in a search response.
const maxValuesPerFacet = 200

// DefaultIndexConfig returns the platformmeili.IndexConfig that bootstraps the
// Meilisearch clientes index. Safe to call multiple times; EnsureIndex is
// idempotent.
func DefaultIndexConfig(indexName string) platformmeili.IndexConfig {
	return platformmeili.IndexConfig{
		UID:                       indexName,
		PrimaryKey:                "id",
		SearchableAttributes:      searchableAttributes,
		FilterableAttributes:      filterableAttributes,
		SortableAttributes:        sortableAttributes,
		RankingRules:              defaultRankingRules,
		FacetingMaxValuesPerFacet: maxValuesPerFacet,
		PaginationMaxTotalHits:    maxTotalHits,
	}
}
