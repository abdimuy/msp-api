package meilisearch_test

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
)

// skipIfNoMeilisearch skips the test when MEILISEARCH_URL is not set.
// This mirrors the FB_DATABASE gate used by Firebird integration tests.
func skipIfNoMeilisearch(t *testing.T) string {
	t.Helper()
	url := os.Getenv("MEILISEARCH_URL")
	if url == "" {
		t.Skip("MEILISEARCH_URL not set — skipping Meilisearch integration test")
	}
	return url
}

// sanitizeIndexName converts a test name into a valid Meilisearch index UID
// (alphanumeric, hyphens and underscores only; max 512 characters).
func sanitizeIndexName(name string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	s := re.ReplaceAllString(name, "-")
	s = strings.ToLower(s)
	if len(s) > 512 {
		s = s[:512]
	}
	return s
}

// TestIntegration_EnsureIndex verifies that EnsureIndex creates the index and
// applies the settings against a live Meilisearch instance.
func TestIntegration_EnsureIndex(t *testing.T) { //nolint:paralleltest // mutates shared Meilisearch state
	rawURL := skipIfNoMeilisearch(t)

	cfg := platformmeili.NewTestConfig(rawURL)
	cfg.IndexName = sanitizeIndexName("integration-ensure-index-" + t.Name())

	c, err := platformmeili.NewRealClient(cfg)
	require.NoError(t, err)
	defer c.Close()

	// Clean up the index after the test so reruns start from a blank slate.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanCancel()
		_ = c.DeleteIndexForTest(cleanCtx, cfg.IndexName)
	})

	indexCfg := platformmeili.IndexConfig{
		UID:                  cfg.IndexName,
		PrimaryKey:           "id",
		SearchableAttributes: []string{"name"},
		FilterableAttributes: []string{"category"},
		SortableAttributes:   []string{"score"},
		RankingRules: []string{
			"words", "typo", "proximity", "attribute", "sort", "exactness",
		},
		FacetingMaxValuesPerFacet: 50,
		PaginationMaxTotalHits:    1000,
	}

	// First call — creates the index.
	err = c.EnsureIndex(ctx, indexCfg)
	require.NoError(t, err, "first EnsureIndex (create) must succeed")

	// Second call — idempotent re-apply.
	err = c.EnsureIndex(ctx, indexCfg)
	require.NoError(t, err, "second EnsureIndex (re-apply) must succeed")
}

// TestIntegration_UpsertAndSearch verifies the full document round-trip:
// upsert → search → delete.
func TestIntegration_UpsertAndSearch(t *testing.T) { //nolint:paralleltest // mutates shared Meilisearch state
	rawURL := skipIfNoMeilisearch(t)

	cfg := platformmeili.NewTestConfig(rawURL)
	cfg.IndexName = sanitizeIndexName("integration-search-" + t.Name())

	c, err := platformmeili.NewRealClient(cfg)
	require.NoError(t, err)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Clean up the index after the test.
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanCancel()
		_ = c.DeleteIndexForTest(cleanCtx, cfg.IndexName)
	})

	// Ensure the index exists before uploading docs.
	err = c.EnsureIndex(ctx, platformmeili.IndexConfig{
		UID:                  cfg.IndexName,
		PrimaryKey:           "id",
		SearchableAttributes: []string{"name"},
		FilterableAttributes: []string{"zone"},
		SortableAttributes:   []string{"name"},
	})
	require.NoError(t, err)

	// Upsert test documents and wait synchronously for indexing to complete.
	docs := []map[string]any{
		{"id": "doc-1", "name": "Fernández López", "zone": 1},
		{"id": "doc-2", "name": "García Ramírez", "zone": 2},
		{"id": "doc-3", "name": "Hernández Cruz", "zone": 1},
	}
	err = c.UpsertDocsAndWaitForTest(ctx, cfg.IndexName, docs, 100*time.Millisecond)
	require.NoError(t, err)

	// Search by name fragment.
	result, err := c.Search(ctx, cfg.IndexName, platformmeili.SearchParams{
		Query: "Fernández",
		Limit: 10,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, result.EstimatedTotalHits, int64(0),
		"search must return a total hits estimate")

	// Verify at least one hit contains the expected doc.
	found := false
	for _, raw := range result.Hits {
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err == nil {
			if id, ok := doc["id"].(string); ok && id == "doc-1" {
				found = true
			}
		}
	}
	assert.True(t, found, "search for 'Fernández' must return doc-1")

	// Clean up documents (index itself will be cleaned by t.Cleanup).
	err = c.DeleteDocs(ctx, cfg.IndexName, []string{"doc-1", "doc-2", "doc-3"})
	require.NoError(t, err)
}
