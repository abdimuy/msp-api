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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics"
	analyticsapp "github.com/abdimuy/msp-api/internal/analytics/app"
	analyticsfb "github.com/abdimuy/msp-api/internal/analytics/infra/analyticsfb"
	analyticsoutbound "github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
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

// TestReconciliarDirectorio_B2_CobranzaSignalsInIndex verifies that B2 cobranza
// signal fields (tier_riesgo, pct_pagos_a_tiempo, fecha_prox_pago_ts) exist in
// the Meilisearch index and that filter/sort operations work against them. The
// test wires the real analytics service (not a fake) so candidatos with actual
// tier data populate the index. Skipped when MEILISEARCH_URL or FB_DATABASE is unset.
func TestReconciliarDirectorio_B2_CobranzaSignalsInIndex(t *testing.T) {
	requireMeiliAndFBEnv(t)

	cfg, err := config.Load()
	require.NoError(t, err)

	pool, err := firebird.New(cfg.Firebird)
	require.NoError(t, err)
	defer pool.Close()

	meiliClient, err := platformmeili.NewMeilisearchClient(cfg.Meilisearch)
	require.NoError(t, err)
	defer meiliClient.Close()

	ctx := context.Background()

	idxCfg := clientessearchmeili.DefaultIndexConfig(cfg.Meilisearch.IndexName)
	require.NoError(t, meiliClient.EnsureIndex(ctx, idxCfg))

	// Wire real analytics service — mirrors cmd/api/analytics_wiring.go.
	analyticsRepo := analyticsfb.NewRepo(pool)
	analyticsSvc := analyticsapp.NewService(
		analyticsoutbound.WinbackRepo(analyticsRepo),
		analyticsoutbound.MicrosipReader(analyticsRepo),
		analyticsoutbound.ProductionClock{},
		nil, // nil txMgr: fn runs directly (reads-only, no rollback needed)
	)

	// Adapter that bridges analyticsapp.Service → clientesoutbound.AnalyticsClient.
	adapter := &b2AnalyticsAdapter{svc: analyticsSvc}

	repo := clientesfb.NewClientesRepo(pool)
	dirIdx := clientessearchmeili.NewMeilisearchDirectoryIndex(meiliClient, cfg.Meilisearch.IndexName)
	svc := clientesapp.NewService(repo, adapter, dirIdx, clientesoutbound.ProductionClock{})

	start := time.Now()
	n, err := svc.ReconciliarDirectorio(ctx)
	t.Logf("B2 reconcile with real analytics: docs=%d elapsed=%s", n, time.Since(start))
	require.NoError(t, err)
	assert.Positive(t, n)

	// Wait for Meilisearch to finish processing the upserted batch.
	time.Sleep(3 * time.Second)

	indexName := cfg.Meilisearch.IndexName
	baseURL := cfg.Meilisearch.URL

	// (a) Field structure: every doc must carry the B2 fields.
	docURL := fmt.Sprintf("%s/indexes/%s/documents?limit=1", baseURL, indexName)
	respDocs, httpErr := http.Get(docURL) //nolint:noctx // test-only convenience call
	require.NoError(t, httpErr)
	defer respDocs.Body.Close()
	var docResult struct {
		Results []map[string]any `json:"results"`
	}
	require.NoError(t, json.NewDecoder(respDocs.Body).Decode(&docResult))
	require.NotEmpty(t, docResult.Results, "index must contain at least one document")
	sample := docResult.Results[0]
	t.Logf("sample B2 fields: tier_riesgo=%v pct_pagos_a_tiempo=%v fecha_prox_pago_ts=%v fecha_prox_pago=%v",
		sample["tier_riesgo"], sample["pct_pagos_a_tiempo"], sample["fecha_prox_pago_ts"], sample["fecha_prox_pago"])
	assert.Contains(t, sample, "tier_riesgo", "doc must have tier_riesgo")
	assert.Contains(t, sample, "pct_pagos_a_tiempo", "doc must have pct_pagos_a_tiempo")
	assert.Contains(t, sample, "pct_pagos_a_tiempo_str", "doc must have pct_pagos_a_tiempo_str")
	assert.Contains(t, sample, "fecha_prox_pago_ts", "doc must have fecha_prox_pago_ts")
	assert.Contains(t, sample, "fecha_prox_pago", "doc must have fecha_prox_pago")

	// (b) Filter by tier_riesgo — Meilisearch must accept the filter clause.
	// Count hits per tier; log totals (candidatos table may be empty on first run).
	tiers := []string{"AL_DIA", "VIGILANCIA", "EN_RIESGO", "CRITICO"}
	totalTierHits := 0
	searchURL := fmt.Sprintf("%s/indexes/%s/search", baseURL, indexName)
	for _, tier := range tiers {
		// Build valid JSON. The Meilisearch filter syntax uses double-quoted string
		// values; json.Marshal escapes them correctly in the JSON string field.
		filterExpr := fmt.Sprintf(`tier_riesgo = "%s"`, tier)
		type filterReq struct {
			Filter string   `json:"filter"`
			Limit  int      `json:"limit"`
			Facets []string `json:"facets"`
		}
		filterPayload, marshalErr := json.Marshal(filterReq{Filter: filterExpr, Limit: 1, Facets: []string{"tier_riesgo"}})
		require.NoError(t, marshalErr)
		reqBody := strings.NewReader(string(filterPayload))
		respFilter, filterErr := http.Post(searchURL, "application/json", reqBody) //nolint:noctx
		require.NoError(t, filterErr)
		require.Equal(t, http.StatusOK, respFilter.StatusCode, "filter by tier_riesgo=%q must return 200", tier)
		var sr struct {
			EstimatedTotalHits int64          `json:"estimatedTotalHits"` //nolint:tagliatelle
			FacetDistribution  map[string]any `json:"facetDistribution"`  //nolint:tagliatelle
		}
		require.NoError(t, json.NewDecoder(respFilter.Body).Decode(&sr))
		_ = respFilter.Body.Close()
		t.Logf("filter tier_riesgo=%q → hits=%d facets=%v", tier, sr.EstimatedTotalHits, sr.FacetDistribution)
		totalTierHits += int(sr.EstimatedTotalHits)
	}
	if totalTierHits == 0 {
		t.Log("INFO: zero tier_riesgo hits — MSP_CANDIDATOS may be empty (analytics refresh not yet run). " +
			"Field structure is correct; values populate after analytics refresh.")
	} else {
		t.Logf("total tier hits across all tiers: %d", totalTierHits)
	}

	// (c) Sort by fecha_prox_pago_ts — must return 200 (field is sortable).
	reqSort := strings.NewReader(`{"sort":["fecha_prox_pago_ts:asc"],"limit":3}`)
	respSort, sortErr := http.Post(searchURL, "application/json", reqSort) //nolint:noctx
	require.NoError(t, sortErr)
	require.Equal(t, http.StatusOK, respSort.StatusCode, "sort by fecha_prox_pago_ts must return 200")
	var sortResult struct {
		Hits []map[string]any `json:"hits"`
	}
	require.NoError(t, json.NewDecoder(respSort.Body).Decode(&sortResult))
	_ = respSort.Body.Close()
	t.Logf("sort by fecha_prox_pago_ts:asc → %d hits returned", len(sortResult.Hits))
	if len(sortResult.Hits) > 0 {
		h := sortResult.Hits[0]
		t.Logf("first hit: nombre=%v tier_riesgo=%v fecha_prox_pago_ts=%v fecha_prox_pago=%v",
			h["nombre"], h["tier_riesgo"], h["fecha_prox_pago_ts"], h["fecha_prox_pago"])
	}

	// (d) Sort by pct_pagos_a_tiempo — must return 200 (field is sortable).
	reqSortPct := strings.NewReader(`{"sort":["pct_pagos_a_tiempo:desc"],"limit":3}`)
	respSortPct, sortPctErr := http.Post(searchURL, "application/json", reqSortPct) //nolint:noctx
	require.NoError(t, sortPctErr)
	require.Equal(t, http.StatusOK, respSortPct.StatusCode, "sort by pct_pagos_a_tiempo must return 200")
	_ = respSortPct.Body.Close()
}

// b2AnalyticsAdapter bridges analyticsapp.Service to clientesoutbound.AnalyticsClient.
// It is a test-local adapter; the production equivalent lives in cmd/api/clientes_wiring.go.
type b2AnalyticsAdapter struct {
	svc *analyticsapp.Service
}

var _ clientesoutbound.AnalyticsClient = (*b2AnalyticsAdapter)(nil)

func (a *b2AnalyticsAdapter) ObtenerPulso(
	ctx context.Context,
	clienteID int,
) (analytics.ClientePulsoContract, bool, error) {
	pulso, err := a.svc.ObtenerPulsoCliente(ctx, clienteID)
	if err != nil {
		// NotFound errors: translate to (zero, false, nil) per port convention.
		if ae, ok := err.(interface{ IsNotFound() bool }); ok && ae.IsNotFound() {
			return analytics.ClientePulsoContract{}, false, nil
		}
		return analytics.ClientePulsoContract{}, false, err
	}
	return pulso, true, nil
}

func (a *b2AnalyticsAdapter) ObtenerPulsos(
	ctx context.Context,
	clienteIDs []int,
) (map[int]analytics.ClientePulsoContract, error) {
	return a.svc.ObtenerPulsosClientes(ctx, clienteIDs)
}
