package firebird_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/healthcheck"
)

// TestReadiness_FirebirdProbe_ReportsOK verifies the integration the audit
// flagged as untested: that a *firebird.Pool plugged into healthcheck.Service
// answers the /readyz endpoint with status ok and a "firebird":"ok" entry.
//
// It does NOT spin up the full fx graph (that would also need Postgres +
// Firebase env vars) — instead it exercises the smallest surface the audit
// asked for: probe registration, HealthCheck round-trip, JSON body shape.
func TestReadiness_FirebirdProbe_ReportsOK(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t) // skips if FB_DATABASE unset

	svc := healthcheck.New()
	// Add a stub for the postgres probe so the response mirrors what /readyz
	// will look like in production (both DBs reported).
	svc.Register(healthcheck.ProbeFunc{
		N: "postgres",
		F: func(_ context.Context) error { return nil },
	})
	svc.Register(healthcheck.ProbeFunc{
		N: "firebird",
		F: pool.HealthCheck,
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody)
	svc.Readiness(w, r)

	require.Equal(t, http.StatusOK, w.Code, "all probes pass → 200")

	var body struct {
		Status string            `json:"status"`
		Probes map[string]string `json:"probes"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "ok", body.Status)
	assert.Equal(t, "ok", body.Probes["firebird"], "firebird probe must report ok")
	assert.Equal(t, "ok", body.Probes["postgres"], "postgres stub must report ok")
}

// TestReadiness_FirebirdProbe_DetectsDeadPool verifies the failure path: when
// the firebird pool is closed, HealthCheck returns an error and /readyz drops
// to 503. This is the negative case behind the audit's concern that we never
// proved the probe actually fails when the DB is gone.
func TestReadiness_FirebirdProbe_DetectsDeadPool(t *testing.T) {
	t.Parallel()

	// Dead-pool path doesn't need a live container — closing a never-started
	// pool is enough to make HealthCheck fail. Keeps this test runnable
	// without FIREBIRD=1 so the failure path is covered even on dev laptops.
	dead := newDeadPool(t)

	svc := healthcheck.New()
	svc.Register(healthcheck.ProbeFunc{N: "firebird", F: dead.HealthCheck})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody)
	svc.Readiness(w, r)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}
