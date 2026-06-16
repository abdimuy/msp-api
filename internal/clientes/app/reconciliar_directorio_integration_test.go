// Integration test for ReconciliarDirectorio against the real Firebird DB and
// Meilisearch instance. Skipped when MEILISEARCH_URL or FB_DATABASE is unset.
//
// This test populates the real Meilisearch index — it is intentionally NOT
// rolled back (the index itself is ephemeral / re-creatable). The Firebird
// reads are read-only, so no rollback is needed.
//
//nolint:paralleltest // serial: single integration test, not parallelisable.
//nolint:misspell    // Spanish domain vocabulary by project convention.
package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clientesapp "github.com/abdimuy/msp-api/internal/clientes/app"
	clientesfb "github.com/abdimuy/msp-api/internal/clientes/infra/clientesfb"
	clientessearchmeili "github.com/abdimuy/msp-api/internal/clientes/infra/clientessearch"
	clientesoutbound "github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
)

func requireMeiliAndFBEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("MEILISEARCH_URL") == "" {
		t.Skip("MEILISEARCH_URL not set — set it to run Meilisearch integration tests")
	}
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set — set it to run Firebird integration tests")
	}
}

// TestReconciliarDirectorio_LiveIntegration exercises the full pipeline:
// Firebird → app service → Meilisearch. It populates the real index and
// asserts doc count and document shape.
func TestReconciliarDirectorio_LiveIntegration(t *testing.T) {
	requireMeiliAndFBEnv(t)

	// Build config from env.
	cfg, err := config.Load()
	require.NoError(t, err, "config load must succeed")

	// Build Firebird pool.
	pool, err := firebird.New(cfg.Firebird)
	require.NoError(t, err, "firebird pool must initialise")
	defer pool.Close()

	// Build Meilisearch client.
	meiliClient, err := platformmeili.NewMeilisearchClient(cfg.Meilisearch)
	require.NoError(t, err, "meilisearch client must initialise")
	defer meiliClient.Close()

	ctx := context.Background()

	// Ensure the index exists with correct settings before indexing.
	idxCfg := clientessearchmeili.DefaultIndexConfig(cfg.Meilisearch.IndexName)
	require.NoError(t, meiliClient.EnsureIndex(ctx, idxCfg), "EnsureIndex must succeed")

	// Wire the service with real repo, noop analytics (pulse enrichment is
	// tested in unit tests; here we verify the pipeline shape), and the real
	// Meilisearch directory index.
	repo := clientesfb.NewClientesRepo(pool)
	dirIdx := clientessearchmeili.NewMeilisearchDirectoryIndex(meiliClient, cfg.Meilisearch.IndexName)

	// Reuse fakeAnalyticsClient and fakeClientesRepo from service_test.go
	// (same package: app_test). Both are zero-value fakes that return no errors
	// and empty maps — adequate for this structural integration test.
	svc := clientesapp.NewService(
		repo,
		&fakeAnalyticsClient{},
		dirIdx,
		clientesoutbound.ProductionClock{},
	)

	start := time.Now()
	n, err := svc.ReconciliarDirectorio(ctx)
	elapsed := time.Since(start)
	require.NoError(t, err, "ReconciliarDirectorio must succeed against live DB")

	t.Logf("ReconciliarDirectorio: docs=%d elapsed=%s", n, elapsed)
	assert.Positive(t, n, "should index at least one document")
	assert.Less(t, elapsed, 120*time.Second, "full reconcile should complete within 2 minutes")

	// Poll until Meilisearch has processed all documents (or 30s timeout).
	indexName := cfg.Meilisearch.IndexName
	statsURL := fmt.Sprintf("%s/indexes/%s/stats", cfg.Meilisearch.URL, indexName)
	var finalCount int64
	pollDeadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(pollDeadline) {
		resp, pollErr := http.Get(statsURL) //nolint:noctx // test-only convenience call
		require.NoError(t, pollErr)
		var stats struct {
			NumberOfDocuments int64 `json:"numberOfDocuments"` //nolint:tagliatelle // Meilisearch API uses camelCase
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&stats))
		_ = resp.Body.Close()
		finalCount = stats.NumberOfDocuments
		if finalCount >= int64(n) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Logf("Meilisearch index %q: numberOfDocuments=%d (reconciled %d)", indexName, finalCount, n)
	assert.GreaterOrEqual(t, finalCount, int64(1000),
		"index should contain at least 1000 documents after reconcile")

	// Fetch one document and verify it has the expected fields.
	docURL := fmt.Sprintf("%s/indexes/%s/documents?limit=1", cfg.Meilisearch.URL, indexName)
	resp2, err := http.Get(docURL) //nolint:noctx // test-only convenience call
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)

	var docResult struct {
		Results []map[string]any `json:"results"`
	}
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&docResult))
	require.NotEmpty(t, docResult.Results, "should have at least one document")

	sample := docResult.Results[0]
	t.Logf("Sample document: %v", sample)
	assert.Contains(t, sample, "id", "document must have 'id' field")
	assert.Contains(t, sample, "nombre", "document must have 'nombre' field")
	assert.Contains(t, sample, "zona_id", "document must have 'zona_id' field")
	assert.Contains(t, sample, "con_saldo", "document must have 'con_saldo' field")
	assert.Contains(t, sample, "segmento_orden", "document must have 'segmento_orden' field")
	assert.Contains(t, sample, "estado_pago_orden", "document must have 'estado_pago_orden' field")
}
