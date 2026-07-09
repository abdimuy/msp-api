// Integration test for the clientes Meilisearch directory adapter against a
// real Meilisearch instance. Skipped when MEILISEARCH_URL is unset — mirrors
// internal/platform/meilisearch/integration_test.go's gating pattern (also
// used by internal/ventas/infra/ventsearch/live_integration_test.go).
//
// Uses a throwaway, sanitized index name (unique per test run) and deletes it
// in t.Cleanup so the shared dev Meilisearch instance never accumulates
// leftover indexes across test runs.
//
//nolint:misspell // Spanish domain vocabulary (directorio, clientes, etc.) by project convention.
package clientessearch_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/clientes/infra/clientessearch"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"
	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
)

// skipIfNoMeilisearch skips the test when MEILISEARCH_URL is not set.
func skipIfNoMeilisearch(t *testing.T) string {
	t.Helper()
	url := os.Getenv("MEILISEARCH_URL")
	if url == "" {
		t.Skip("MEILISEARCH_URL not set — skipping Meilisearch integration test")
	}
	return url
}

// sanitizeIndexName converts a test name into a valid Meilisearch index UID.
func sanitizeIndexName(name string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	s := re.ReplaceAllString(name, "-")
	s = strings.ToLower(s)
	if len(s) > 512 {
		s = s[:512]
	}
	return s
}

// TestIntegration_DirectoryIndex_ReconciliarAutoCreatesIndex_WithFullSettings
// is the regression test for .superpowers/sdd/fix-settings-race-brief.md: a
// UpsertDocs call that auto-creates a brand-new Meilisearch index leaves it
// with DEFAULT settings (searchableAttributes=["*"], filterableAttributes=[],
// sortableAttributes=[]) unless something applies the real settings first.
// Before the fix, Reconciliar went straight to UpsertDocs — so if it was the
// FIRST call to touch a not-yet-existing index, filtered/sorted directory
// searches would fail.
//
// This test deliberately does NOT call EnsureIndex before Reconciliar — it
// must self-configure. RED (pre-fix): filterableAttributes/sortableAttributes
// come back empty and the filtered+sorted Buscar call fails. GREEN
// (post-fix): both are populated and Buscar succeeds.
func TestIntegration_DirectoryIndex_ReconciliarAutoCreatesIndex_WithFullSettings(t *testing.T) { //nolint:paralleltest // mutates shared Meilisearch state
	rawURL := skipIfNoMeilisearch(t)

	indexName := sanitizeIndexName("integration-clientes-autocreate-" + t.Name() + "-" + time.Now().Format("150405.000000"))
	client, err := platformmeili.NewMeilisearchClient(config.Meilisearch{URL: rawURL})
	require.NoError(t, err)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	t.Cleanup(func() {
		deleteIndexForTest(t, rawURL, indexName)
	})

	// No EnsureIndex call here — Reconciliar must configure the index itself.
	idx := clientessearch.NewMeilisearchDirectoryIndex(client, indexName)

	zonaID := 900123
	doc := outbound.DirectorioDoc{
		ClienteID: 999001,
		Nombre:    "LUCIA SIN INDICE CONFIGURADO",
		ZonaID:    zonaID,
		Estatus:   "A",
		Saldo:     decimal.NewFromInt(500),
	}
	require.NoError(t, idx.Reconciliar(ctx, []outbound.DirectorioDoc{doc}))

	// Wait for the document to actually be indexed before inspecting settings
	// or searching.
	statsURL := fmt.Sprintf("%s/indexes/%s/stats", rawURL, indexName)
	pollDeadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(pollDeadline) {
		if fetchNumberOfDocuments(t, statsURL) >= 1 {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}

	settings := fetchIndexSettings(t, rawURL, indexName)
	assert.NotEmpty(t, settings.FilterableAttributes,
		"filterableAttributes must not be empty — Reconciliar must configure a freshly auto-created index")
	assert.NotEmpty(t, settings.SortableAttributes,
		"sortableAttributes must not be empty — Reconciliar must configure a freshly auto-created index")

	// The actual symptom from the incident: a filtered + sorted search must
	// succeed, not fail with a Meilisearch "attribute not filterable/sortable"
	// error.
	res, err := idx.Buscar(ctx, outbound.DirectorioQuery{
		ZonaClienteID: &zonaID,
		SortBy:        "zona",
		SortOrder:     "asc",
		Limit:         10,
	})
	require.NoError(t, err, "filtered+sorted search must succeed once the index is fully configured")
	found := false
	for _, item := range res.Items {
		if item.ClienteID == doc.ClienteID {
			found = true
			break
		}
	}
	assert.True(t, found, "expected ClienteID %d in filtered+sorted results", doc.ClienteID)
}

// meilisearchIndexSettings is the minimal shape decoded from GET
// /indexes/{uid}/settings for this test's assertions.
//
//nolint:tagliatelle // Meilisearch API returns camelCase field names.
type meilisearchIndexSettings struct {
	FilterableAttributes []string `json:"filterableAttributes"`
	SortableAttributes   []string `json:"sortableAttributes"`
}

// fetchIndexSettings issues a raw HTTP GET against the Meilisearch REST API
// to read the live settings of the given index.
func fetchIndexSettings(t *testing.T, baseURL, indexName string) meilisearchIndexSettings {
	t.Helper()
	settingsURL := fmt.Sprintf("%s/indexes/%s/settings", baseURL, indexName)
	resp, err := http.Get(settingsURL) //nolint:noctx,gosec // test-only convenience call against a trusted local URL
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var settings meilisearchIndexSettings
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&settings))
	return settings
}

// fetchNumberOfDocuments polls the Meilisearch stats endpoint for the given
// index and returns numberOfDocuments (0 on any transport/decode error, to
// keep the polling loop resilient to transient 404s while the index is still
// being created).
func fetchNumberOfDocuments(t *testing.T, statsURL string) int64 {
	t.Helper()
	resp, err := http.Get(statsURL) //nolint:noctx,gosec // test-only convenience call against a trusted local URL
	if err != nil {
		return 0
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0
	}
	var stats struct {
		NumberOfDocuments int64 `json:"numberOfDocuments"` //nolint:tagliatelle // Meilisearch API uses camelCase
	}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return 0
	}
	return stats.NumberOfDocuments
}

// deleteIndexForTest issues a raw HTTP DELETE against the Meilisearch REST
// API to remove the throwaway index created by this test. Best-effort — a
// failure here just means an empty leftover index, not a data leak.
func deleteIndexForTest(t *testing.T, baseURL, indexName string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		fmt.Sprintf("%s/indexes/%s", baseURL, indexName), http.NoBody)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}
