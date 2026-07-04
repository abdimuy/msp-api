//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
)

// TestRefrescarBusqueda_RequiresPermission asserts POST
// /ventas/_search/refresh is gated by PermVentasReindexar — a caller
// without it (even one with every other ventas permission) is rejected.
func TestRefrescarBusqueda_RequiresPermission(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()

	cu := fullPerms(uuid.New())
	// Strip the reindexar permission specifically.
	filtered := cu.Permisos[:0]
	for _, p := range cu.Permisos {
		if p != string(authdomain.PermVentasReindexar) {
			filtered = append(filtered, p)
		}
	}
	cu.Permisos = filtered

	r := buildRouter(t, svc, cu)
	req := httptest.NewRequest(http.MethodPost, "/ventas/_search/refresh", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
}

// TestRefrescarBusqueda_NoIndexWired_ReturnsZeroDocumentos asserts the
// endpoint completes successfully even when no Meilisearch index is wired
// to the Service (dev/test environments) — ReconciliarVentas no-ops and the
// endpoint reports 0 documents reindexed rather than failing.
func TestRefrescarBusqueda_NoIndexWired_ReturnsZeroDocumentos(t *testing.T) {
	t.Parallel()
	svc, _, _ := testService()

	r := buildRouter(t, svc, fullPerms(uuid.New()))
	req := httptest.NewRequest(http.MethodPost, "/ventas/_search/refresh", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())

	var body struct {
		Reindexado bool `json:"reindexado"`
		Documentos int  `json:"documentos"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.True(t, body.Reindexado)
	assert.Equal(t, 0, body.Documentos)
}
