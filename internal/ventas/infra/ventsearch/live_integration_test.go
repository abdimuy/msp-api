// Integration test for the ventas Meilisearch adapter against a real
// Meilisearch instance. Skipped when MEILISEARCH_URL is unset — mirrors
// internal/platform/meilisearch/integration_test.go's gating pattern.
//
// Uses a throwaway, sanitized index name (unique per test run) and deletes
// it in t.Cleanup so the shared dev Meilisearch instance never accumulates
// leftover indexes across test runs.
package ventsearch_test

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

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/config"
	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventsearch"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
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

// TestIntegration_VentaSearchIndex_LiveRoundTrip exercises the full ventas
// search pipeline against a real Meilisearch instance: EnsureIndex →
// Reconciliar (bulk upsert) → Buscar by nombre/telefono/folio → assert the
// matched IDs. Uses a throwaway index name, deleted in t.Cleanup.
func TestIntegration_VentaSearchIndex_LiveRoundTrip(t *testing.T) { //nolint:paralleltest // mutates shared Meilisearch state
	rawURL := skipIfNoMeilisearch(t)

	indexName := sanitizeIndexName("integration-ventas-" + t.Name() + "-" + uuid.NewString())
	client, err := platformmeili.NewMeilisearchClient(config.Meilisearch{URL: rawURL})
	require.NoError(t, err)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Cleanup(func() {
		deleteIndexForTest(t, rawURL, indexName)
	})

	require.NoError(t, client.EnsureIndex(ctx, ventsearch.DefaultIndexConfig(indexName)),
		"EnsureIndex must succeed")

	idx := ventsearch.NewMeilisearchVentaSearchIndex(client, indexName)

	idJuan := uuid.New()
	idMaria := uuid.New()
	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	docs := []outbound.VentaSearchDoc{
		{
			ID:             idJuan,
			NombreCliente:  "JUAN PEREZ GOMEZ",
			Telefono:       "2381234567",
			Direccion:      "AV. REFORMA CENTRO TEHUACAN",
			Folio:          "Y00001234",
			Vendedor:       "ANA VENDEDORA",
			TipoVenta:      "CONTADO",
			Situacion:      "aprobada",
			Sincronizacion: "aplicada",
			ZonaClienteID:  21563,
			VendedorEmail:  "ana@example.com",
			ClienteID:      555,
			Estado:         "active",
			FechaVenta:     now,
			PrecioTotal:    decimal.NewFromInt(1000),
			CreatedAt:      now,
		},
		{
			ID:             idMaria,
			NombreCliente:  "MARIA LOPEZ SANCHEZ",
			Telefono:       "2387654321",
			Direccion:      "CALLE 5 SUR CENTRO PUEBLA",
			Folio:          "",
			Vendedor:       "LUIS VENDEDOR",
			TipoVenta:      "CREDITO",
			Situacion:      "borrador",
			Sincronizacion: "pendiente",
			ZonaClienteID:  99,
			VendedorEmail:  "luis@example.com",
			ClienteID:      0,
			Estado:         "active",
			FechaVenta:     now,
			PrecioTotal:    decimal.NewFromInt(5000),
			CreatedAt:      now,
		},
	}

	require.NoError(t, idx.Reconciliar(ctx, docs))

	// Poll /indexes/{name}/stats until Meilisearch finishes indexing.
	statsURL := fmt.Sprintf("%s/indexes/%s/stats", rawURL, indexName)
	pollDeadline := time.Now().Add(30 * time.Second)
	var numDocs int64
	for time.Now().Before(pollDeadline) {
		numDocs = fetchNumberOfDocuments(t, statsURL)
		if numDocs >= int64(len(docs)) {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	require.GreaterOrEqual(t, numDocs, int64(len(docs)), "index should contain both reconciled docs")

	// Search by nombre.
	resNombre, err := idx.Buscar(ctx, outbound.VentasSearchQuery{Q: "Juan Perez", Limit: 10})
	require.NoError(t, err)
	assert.Contains(t, resNombre.IDs, idJuan, "search by nombre_cliente must find Juan")

	// Search by telefono.
	resTelefono, err := idx.Buscar(ctx, outbound.VentasSearchQuery{Q: "2387654321", Limit: 10})
	require.NoError(t, err)
	assert.Contains(t, resTelefono.IDs, idMaria, "search by telefono must find Maria")

	// Search by folio.
	resFolio, err := idx.Buscar(ctx, outbound.VentasSearchQuery{Q: "Y00001234", Limit: 10})
	require.NoError(t, err)
	assert.Contains(t, resFolio.IDs, idJuan, "search by folio must find Juan")
}

// fetchNumberOfDocuments polls the Meilisearch stats endpoint for the given
// index and returns numberOfDocuments (0 on any transport/decode error, to
// keep the polling loop resilient to transient 404s while the index is
// still being created).
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

// deleteIndexForTest issues a raw HTTP DELETE against the Meilisearch
// REST API to remove the throwaway index created by this test. Best-effort
// — a failure here just means an empty leftover index, not a data leak.
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
