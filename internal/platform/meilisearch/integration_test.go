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

// TestIntegration_UpsertDocs_NewIndexAutoCreatesWithPrimaryKeyID reproduces
// the production race described in
// .superpowers/sdd/fix-meili-pk-brief.md: a reconcile worker's warm-up tick
// calls UpsertDocs on a brand-new index BEFORE the boot-time EnsureIndex
// goroutine has a chance to create it with an explicit primary key. When
// UpsertDocs is the call that ends up auto-creating the index, Meilisearch
// must be given the primary key explicitly — otherwise it falls back to
// inferring it from the document shape, which fails whenever more than one
// field name ends in "id" (id, cliente_id, zona_cliente_id — exactly the
// shape of ventsearch.VentaDoc and clientessearch.ClienteDoc) with
// index_primary_key_multiple_candidates_found, and the index is stuck at
// zero documents forever.
//
//nolint:paralleltest // mutates shared Meilisearch state
func TestIntegration_UpsertDocs_NewIndexAutoCreatesWithPrimaryKeyID(t *testing.T) {
	rawURL := skipIfNoMeilisearch(t)

	cfg := platformmeili.NewTestConfig(rawURL)
	cfg.IndexName = sanitizeIndexName("integration-pk-race-" + t.Name())

	c, err := platformmeili.NewRealClient(cfg)
	require.NoError(t, err)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanCancel()
		_ = c.DeleteIndexForTest(cleanCtx, cfg.IndexName)
	})

	// Documents mirror the real production shape that triggers Meilisearch's
	// primary-key inference ambiguity: three fields end in "id"
	// (id, cliente_id, zona_cliente_id), exactly like ventsearch.VentaDoc.
	docs := []map[string]any{
		{"id": "venta-1", "cliente_id": 101, "zona_cliente_id": 7, "nombre_cliente": "Fernández López"},
		{"id": "venta-2", "cliente_id": 102, "zona_cliente_id": 8, "nombre_cliente": "García Ramírez"},
	}

	// Deliberately NO EnsureIndex call — reproduces the reconcile worker
	// winning the race against the boot-time EnsureIndex goroutine, so this
	// UpsertDocs call is the one that ends up auto-creating the index.
	err = c.UpsertDocs(ctx, cfg.IndexName, docs)
	require.NoError(t, err, "UpsertDocs must not error when it auto-creates the index")

	// UpsertDocs is fire-and-forget (matches production usage) — poll for the
	// enqueued task to settle instead of sleeping a fixed duration. Bounded to
	// 10s: on a healthy fix the task resolves in well under a second; on the
	// old (buggy) code the add-documents task fails permanently and this loop
	// times out, which is the RED signal.
	require.Eventually(t, func() bool {
		info, ferr := c.FetchIndexInfoForTest(ctx, cfg.IndexName)
		return ferr == nil && info.NumberOfDocuments == int64(len(docs))
	}, 10*time.Second, 200*time.Millisecond,
		"documents must eventually be indexed — if this times out, the primary key "+
			"was left for Meilisearch to infer and the add-documents task failed permanently")

	info, err := c.FetchIndexInfoForTest(ctx, cfg.IndexName)
	require.NoError(t, err)
	assert.Equal(t, "id", info.PrimaryKey,
		"index auto-created by UpsertDocs must get primaryKey=id, not left for Meilisearch to infer")
	assert.Equal(t, int64(len(docs)), info.NumberOfDocuments,
		"documents must be indexed, not stuck behind a failed add-documents task")
}
