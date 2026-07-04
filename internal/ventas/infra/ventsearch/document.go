// Package ventsearch defines the Meilisearch document shape and index
// settings for the ventas search index. It is the ventas-specific companion
// to the generic internal/platform/meilisearch package.
//
// This package is responsible for:
//   - VentaDoc: the flat JSON document indexed into Meilisearch.
//   - DefaultIndexConfig: the IndexConfig (filterable/sortable/searchable/
//     ranking/pagination) to apply at boot via EnsureIndex.
//
// It must NOT import domain/, app/, or any other module — only the platform
// meilisearch package for the IndexConfig type, and the ports/outbound
// package for shared constants.
package ventsearch

import (
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"

	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
)

// VentaDoc is the flat search document indexed into Meilisearch for each
// venta. Field names use snake_case JSON tags (tagliatelle enforced).
//
// Field roles:
//
//	searchable  → included in searchableAttributes
//	filterable  → can appear in filter= expressions
//	sortable    → can appear in sort= clauses
//	display     → returned in hits but not searchable/filterable/sortable
type VentaDoc struct {
	// ID is the Meilisearch primary key. Equals VentaSearchDoc.ID.String().
	ID string `json:"id"`

	// ── Searchable ───────────────────────────────────────────────────────

	// NombreCliente is the cliente snapshot name. Searchable.
	NombreCliente string `json:"nombre_cliente"`

	// Telefono is the cliente snapshot phone number. Searchable.
	Telefono string `json:"telefono"`

	// Direccion is the combined address string (calle + colonia + poblacion +
	// ciudad). Searchable.
	Direccion string `json:"direccion"`

	// Folio is the Microsip folio once aplicada; empty when pendiente.
	// Searchable.
	Folio string `json:"folio"`

	// Vendedor is the concatenated names of the venta's vendedores.
	// Searchable.
	Vendedor string `json:"vendedor"`

	// ── Filterable ───────────────────────────────────────────────────────

	// TipoVenta is "CONTADO" | "CREDITO". Filterable.
	TipoVenta string `json:"tipo_venta"`

	// Situacion is "borrador" | "revisada" | "aprobada" | "cancelada".
	// Filterable.
	Situacion string `json:"situacion"`

	// Sincronizacion is "pendiente" | "aplicada". Filterable.
	Sincronizacion string `json:"sincronizacion"`

	// ZonaClienteID is the Microsip zona identifier. Filterable.
	ZonaClienteID int `json:"zona_cliente_id"`

	// VendedorEmail is the auth identity of the vendedor who registered the
	// venta. Filterable.
	VendedorEmail string `json:"vendedor_email"`

	// ClienteID is the Microsip CLIENTE_ID once linked; 0 when unlinked.
	// Filterable.
	ClienteID int `json:"cliente_id"`

	// Estado is "active" | "deleted". Filterable.
	Estado string `json:"estado"`

	// ── Filterable + sortable numeric ────────────────────────────────────

	// FechaVentaTs is the sale date as Unix epoch-seconds. Filterable (range)
	// + sortable.
	FechaVentaTs int64 `json:"fecha_venta_ts"`

	// PrecioTotal is the total sale amount. ONLY for Meilisearch numeric
	// filtering/sorting — render PrecioTotalStr instead, which is exact (a
	// decimal→float64→decimal round-trip loses precision on money).
	// Filterable (range) + sortable.
	PrecioTotal float64 `json:"precio_total"`

	// ── Sortable ──────────────────────────────────────────────────────────

	// CreatedAtTs is the audit creation timestamp as Unix epoch-seconds.
	// Sortable. omitempty drops the field when zero so the document has NO
	// sort value → Meilisearch ranks absent-attribute docs last in both asc
	// and desc.
	CreatedAtTs int64 `json:"created_at_ts,omitempty"`

	// ── Display-only (not searchable/filterable/sortable) ───────────────

	// PrecioTotalStr is the exact total sale amount (StringFixed 2). Display.
	PrecioTotalStr string `json:"precio_total_str"`

	// FechaVenta is the RFC3339 UTC display string for the sale date. Empty
	// when FechaVenta is the zero time.
	FechaVenta string `json:"fecha_venta"`

	// CreatedAt is the RFC3339 UTC display string for the audit creation
	// timestamp. Empty when CreatedAt is the zero time.
	CreatedAt string `json:"created_at"`
}

// defaultRankingRules is the ordered list of ranking rules applied to the
// ventas index. We include the standard Meilisearch defaults and add "sort"
// after "attribute" so explicit sort clauses take effect.
var defaultRankingRules = []string{
	"words",
	"typo",
	"proximity",
	"attribute",
	"sort",
	"exactness",
}

// searchableAttributes is the ordered list searched for query matches.
// NombreCliente is first so name hits rank highest.
var searchableAttributes = []string{
	"nombre_cliente",
	"telefono",
	"direccion",
	"folio",
	"vendedor",
}

// filterableAttributes lists every attribute that can appear in filter
// expressions.
var filterableAttributes = []string{
	"tipo_venta",
	"situacion",
	"sincronizacion",
	"zona_cliente_id",
	"vendedor_email",
	"cliente_id",
	"estado",
	"fecha_venta_ts",
	"precio_total",
}

// sortableAttributes lists every attribute that can appear in sort clauses.
var sortableAttributes = []string{
	"fecha_venta_ts",
	"precio_total",
	"nombre_cliente",
	"created_at_ts",
}

// DefaultIndexConfig returns the platformmeili.IndexConfig that bootstraps
// the Meilisearch ventas index. Safe to call multiple times; EnsureIndex is
// idempotent.
func DefaultIndexConfig(indexName string) platformmeili.IndexConfig {
	return platformmeili.IndexConfig{
		UID:                    indexName,
		PrimaryKey:             "id",
		SearchableAttributes:   searchableAttributes,
		FilterableAttributes:   filterableAttributes,
		SortableAttributes:     sortableAttributes,
		RankingRules:           defaultRankingRules,
		PaginationMaxTotalHits: outbound.MaxTotalHitsVentas,
	}
}
