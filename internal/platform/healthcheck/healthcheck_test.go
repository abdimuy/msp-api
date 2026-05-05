package healthcheck_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/healthcheck"
)

func TestLiveness_Always200(t *testing.T) {
	t.Parallel()
	svc := healthcheck.New()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	svc.Liveness(rec, r)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"ok"`)
}

func TestReadiness_NoProbes_Returns200(t *testing.T) {
	t.Parallel()
	svc := healthcheck.New()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	svc.Readiness(rec, r)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestReadiness_AllProbesPass(t *testing.T) {
	t.Parallel()
	svc := healthcheck.New()
	svc.Register(healthcheck.ProbeFunc{N: "db", F: func(_ context.Context) error { return nil }})
	svc.Register(healthcheck.ProbeFunc{N: "fb", F: func(_ context.Context) error { return nil }})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	svc.Readiness(rec, r)

	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Status string            `json:"status"`
		Probes map[string]string `json:"probes"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "ok", body.Status)
	assert.Equal(t, "ok", body.Probes["db"])
	assert.Equal(t, "ok", body.Probes["fb"])
}

func TestReadiness_OneProbeFails(t *testing.T) {
	t.Parallel()
	svc := healthcheck.New()
	svc.Register(healthcheck.ProbeFunc{N: "db", F: func(_ context.Context) error { return nil }})
	svc.Register(healthcheck.ProbeFunc{N: "fb", F: func(_ context.Context) error { return errors.New("connection refused") }})

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	svc.Readiness(rec, r)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var body struct {
		Status string            `json:"status"`
		Probes map[string]string `json:"probes"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "unhealthy", body.Status)
	assert.Equal(t, "ok", body.Probes["db"])
	assert.Contains(t, body.Probes["fb"], "connection refused")
}

func TestProbeFunc_PassesThroughContext(t *testing.T) {
	t.Parallel()
	type ctxKey struct{}
	called := false
	probe := healthcheck.ProbeFunc{
		N: "x",
		F: func(ctx context.Context) error {
			called = true
			assert.Equal(t, "yes", ctx.Value(ctxKey{}))
			return nil
		},
	}
	ctx := context.WithValue(context.Background(), ctxKey{}, "yes")
	require.NoError(t, probe.Check(ctx))
	assert.True(t, called)
	assert.Equal(t, "x", probe.Name())
}
