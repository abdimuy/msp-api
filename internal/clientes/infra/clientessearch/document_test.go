package clientessearch_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clientessearchmeili "github.com/abdimuy/msp-api/internal/clientes/infra/clientessearch"
	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
)

func TestDefaultIndexConfig_UIDAndPrimaryKey(t *testing.T) {
	t.Parallel()
	cfg := clientessearchmeili.DefaultIndexConfig("my-index")
	assert.Equal(t, "my-index", cfg.UID)
	assert.Equal(t, "id", cfg.PrimaryKey)
}

func TestDefaultIndexConfig_SearchableAttributes(t *testing.T) {
	t.Parallel()
	cfg := clientessearchmeili.DefaultIndexConfig("clientes")
	require.NotEmpty(t, cfg.SearchableAttributes)
	assert.Contains(t, cfg.SearchableAttributes, "nombre",
		"nombre must be searchable")
	assert.Contains(t, cfg.SearchableAttributes, "direccion",
		"direccion must be searchable")
}

func TestDefaultIndexConfig_FilterableAttributes(t *testing.T) {
	t.Parallel()
	cfg := clientessearchmeili.DefaultIndexConfig("clientes")
	require.NotEmpty(t, cfg.FilterableAttributes)

	for _, expected := range []string{
		"zona_id", "cobrador_id", "con_saldo", "segmento",
		"estado_pago", "score", "recencia_dias", "estatus",
	} {
		assert.True(t, slices.Contains(cfg.FilterableAttributes, expected),
			"filterable must include %q", expected)
	}
}

func TestDefaultIndexConfig_SortableAttributes(t *testing.T) {
	t.Parallel()
	cfg := clientessearchmeili.DefaultIndexConfig("clientes")
	require.NotEmpty(t, cfg.SortableAttributes)

	for _, expected := range []string{
		"nombre", "saldo", "score", "segmento_orden",
		"estado_pago_orden", "recencia_dias", "zona_id",
	} {
		assert.Contains(t, cfg.SortableAttributes, expected,
			"sortable must include %q", expected)
	}
}

func TestDefaultIndexConfig_RankingRules(t *testing.T) {
	t.Parallel()
	cfg := clientessearchmeili.DefaultIndexConfig("clientes")
	require.NotEmpty(t, cfg.RankingRules)
	// Must include the standard Meilisearch rules.
	for _, r := range []string{"words", "typo", "proximity", "attribute", "sort", "exactness"} {
		assert.Contains(t, cfg.RankingRules, r,
			"ranking rules must include %q", r)
	}
}

func TestDefaultIndexConfig_FacetingAndPagination(t *testing.T) {
	t.Parallel()
	cfg := clientessearchmeili.DefaultIndexConfig("clientes")
	assert.Positive(t, cfg.FacetingMaxValuesPerFacet,
		"FacetingMaxValuesPerFacet must be positive")
	assert.GreaterOrEqual(t, cfg.PaginationMaxTotalHits, int64(38000),
		"PaginationMaxTotalHits must exceed the padrón size (~38k)")
}

func TestFacetAttributes_NonEmpty(t *testing.T) {
	t.Parallel()
	facets := clientessearchmeili.FacetAttributes()
	assert.NotEmpty(t, facets)
	for _, expected := range []string{"zona_id", "cobrador_id", "segmento", "estado_pago"} {
		assert.Contains(t, facets, expected,
			"facet attributes must include %q", expected)
	}
}

func TestFacetAttributes_ReturnsACopy(t *testing.T) {
	t.Parallel()
	a := clientessearchmeili.FacetAttributes()
	b := clientessearchmeili.FacetAttributes()
	// Mutating a must not affect b.
	if len(a) > 0 {
		a[0] = "mutated"
		assert.NotEqual(t, "mutated", b[0],
			"FacetAttributes must return independent copies")
	}
}

// TestDefaultIndexConfig_IsPlatformType verifies the return type is the
// platform-level IndexConfig (not a clientes-specific type). The assertion is
// structural: passing the result to a function that accepts platformmeili.IndexConfig
// causes a compile error if the types diverge.
func TestDefaultIndexConfig_IsPlatformType(t *testing.T) {
	t.Parallel()
	acceptsPlatformType(clientessearchmeili.DefaultIndexConfig("x"))
}

// acceptsPlatformType is a compile-time adapter that verifies the argument is
// assignable to platformmeili.IndexConfig.
func acceptsPlatformType(cfg platformmeili.IndexConfig) {
	_ = cfg.UID // reference to avoid unused-variable linting on cfg
}
