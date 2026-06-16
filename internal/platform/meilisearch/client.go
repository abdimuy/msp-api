package meilisearch

import (
	"context"
	"encoding/json"
	"errors"
)

// ErrMeilisearchNotConfigured is returned by NotConfiguredClient when
// Meilisearch is explicitly opted out via MEILISEARCH_ALLOW_UNCONFIGURED=true.
// Callers may check for it with errors.Is to degrade gracefully.
var ErrMeilisearchNotConfigured = errors.New("meilisearch: not configured")

// ErrMeilisearchTransient is the sentinel that wraps transient failures
// (network errors, 5xx responses, timeouts). Callers can check with
// errors.Is(err, ErrMeilisearchTransient) to decide whether to retry.
var ErrMeilisearchTransient = errors.New("meilisearch: transient failure")

// IndexConfig describes how to create and configure a Meilisearch index.
// All fields except UID and PrimaryKey are optional; zero values mean
// "use Meilisearch defaults".
type IndexConfig struct {
	// UID is the unique index identifier (e.g. "clientes").
	UID string
	// PrimaryKey is the field name that Meilisearch uses as the document
	// identifier. Mandatory when creating a new index.
	PrimaryKey string

	// SearchableAttributes is the ordered list of attributes searched for
	// a query match. Nil means Meilisearch's default (all attributes).
	SearchableAttributes []string

	// FilterableAttributes is the list of attributes that can be used
	// in filter expressions and faceted search.
	FilterableAttributes []string

	// SortableAttributes is the list of attributes that can appear in sort
	// clauses (asc/desc).
	SortableAttributes []string

	// RankingRules is the ordered list of ranking rules. Nil means the
	// Meilisearch default: words, typo, proximity, attribute, sort, exactness.
	RankingRules []string

	// FacetingMaxValuesPerFacet caps how many facet values are returned per
	// facet in a search response. 0 means use the server default (100).
	FacetingMaxValuesPerFacet int64

	// PaginationMaxTotalHits caps the total number of hits reachable via
	// offset/limit pagination. 0 means use the server default (1000).
	PaginationMaxTotalHits int64
}

// SearchParams carries the parameters for a single search request.
type SearchParams struct {
	// Query is the full-text search string.
	Query string
	// Filter is the Meilisearch filter expression (e.g. "zona_id = 5").
	Filter string
	// Sort is a list of sort clauses (e.g. ["nombre:asc", "saldo:desc"]).
	Sort []string
	// Offset is the number of results to skip (for keyset/offset pagination).
	Offset int64
	// Limit is the maximum number of results to return.
	Limit int64
	// Facets is the list of attributes for which to compute facet distributions.
	Facets []string
}

// SearchResult carries the response from a search request in a
// representation-independent form.
type SearchResult struct {
	// Hits is a JSON-encoded slice of matched documents. Each element is a
	// raw JSON object; callers unmarshal into their own types.
	Hits []json.RawMessage
	// FacetDistribution is the per-facet value counts keyed by attribute name.
	// Inner int64 values are the hit counts per facet value.
	FacetDistribution map[string]map[string]int64
	// EstimatedTotalHits is the approximate total number of results for this
	// query (may be capped by the index's PaginationMaxTotalHits setting).
	EstimatedTotalHits int64
}

// Client is the interface that both RealClient and NotConfiguredClient satisfy.
// All operations are context-aware and return an error that is either an
// apperror, ErrMeilisearchTransient, or ErrMeilisearchNotConfigured.
type Client interface {
	// EnsureIndex creates the index if it does not exist and applies the
	// supplied settings. Safe to call on every boot (idempotent).
	EnsureIndex(ctx context.Context, cfg IndexConfig) error

	// UpsertDocs bulk-adds or replaces documents in the index identified by
	// indexUID. docs must be JSON-serializable (slice of structs or maps).
	UpsertDocs(ctx context.Context, indexUID string, docs any) error

	// DeleteDocs removes documents by their primary-key values.
	DeleteDocs(ctx context.Context, indexUID string, ids []string) error

	// Search executes a search request against the index identified by indexUID.
	Search(ctx context.Context, indexUID string, params SearchParams) (SearchResult, error)

	// Close releases any resources held by the client (idle HTTP connections,
	// background goroutines started by the SDK).
	Close()
}
