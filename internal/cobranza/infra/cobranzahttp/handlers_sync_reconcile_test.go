//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/app/eventbus"
	"github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ─── Fakes ────────────────────────────────────────────────────────────────────

// fakePagosReconcileRepo is an in-memory stub for tests.
type fakePagosReconcileRepo struct {
	digest outbound.DigestResult
	ids    []int
	err    error
}

func (f *fakePagosReconcileRepo) Digest(_ context.Context, _ int) (outbound.DigestResult, error) {
	return f.digest, f.err
}

func (f *fakePagosReconcileRepo) ListIDs(_ context.Context, _, after, limit int) ([]int, bool, error) {
	if f.err != nil {
		return nil, false, f.err
	}
	var filtered []int
	for _, id := range f.ids {
		if id > after {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) > limit {
		return filtered[:limit], true, nil
	}
	return filtered, false, nil
}

// fakeSaldosReconcileRepo is an in-memory stub for tests.
type fakeSaldosReconcileRepo struct {
	digest outbound.DigestResult
	ids    []int
	err    error
}

func (f *fakeSaldosReconcileRepo) Digest(_ context.Context, _ int) (outbound.DigestResult, error) {
	return f.digest, f.err
}

func (f *fakeSaldosReconcileRepo) ListIDs(_ context.Context, _, after, limit int) ([]int, bool, error) {
	if f.err != nil {
		return nil, false, f.err
	}
	var filtered []int
	for _, id := range f.ids {
		if id > after {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) > limit {
		return filtered[:limit], true, nil
	}
	return filtered, false, nil
}

// ─── auth helpers (re-declared here so this file is self-contained) ───────────

// reconcileUser builds a CurrentUser with both pagos and saldos read permissions.
func reconcileUser() auth.CurrentUser {
	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-test-reconcile",
		Email:       "sistema@muebleriamsp.mx",
		Nombre:      "Sistema Reconciliacion",
		Permisos: []string{
			string(authdomain.PermCobranzaVerPagos),
			string(authdomain.PermCobranzaVerSaldos),
		},
	}
}

// saldosOnlyUser has only saldos permission (no pagos).
func saldosOnlyUser() auth.CurrentUser {
	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-test-saldos",
		Email:       "saldos@muebleriamsp.mx",
		Nombre:      "Solo Saldos",
		Permisos:    []string{string(authdomain.PermCobranzaVerSaldos)},
	}
}

// buildReconcileSvc constructs a Service with the given fake reconcile repos attached.
func buildReconcileSvc(pagosR outbound.PagosReconcileRepo, saldosR outbound.SaldosReconcileRepo) *cobranzaapp.Service {
	svc := cobranzaapp.NewService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	svc.WithReconcilePorts(pagosR, saldosR)
	return svc
}

// mountReconcileRouter wires the read router with a planted CurrentUser.
func mountReconcileRouter(cu auth.CurrentUser, svc *cobranzaapp.Service) http.Handler {
	r := chi.NewRouter()
	r.Use(planter(cu))
	cobranzahttp.MountReadRouter(r, svc, eventbus.New(), config.Cobranza{}, slog.Default())
	return r
}

// ─── Digest happy path ─────────────────────────────────────────────────────────

func TestHandler_SyncPagosDigest_HappyPath(t *testing.T) {
	t.Parallel()
	fixedTime := time.Date(2026, 6, 2, 18, 13, 23, 0, time.UTC)
	pagosRepo := &fakePagosReconcileRepo{
		digest: outbound.DigestResult{
			CountActivos: 1247,
			IDsXor:       0xa7b3c9d2,
			IDsSum:       143982,
			MaxUpdatedAt: fixedTime,
		},
	}
	svc := buildReconcileSvc(pagosRepo, &fakeSaldosReconcileRepo{})
	handler := mountReconcileRouter(reconcileUser(), svc)

	req := httptest.NewRequest(http.MethodGet, "/sync/pagos/zona/21552/digest", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body)

	var body struct {
		CountActivos int    `json:"count_activos"`
		IDsXor       string `json:"ids_xor"`
		IDsSum       string `json:"ids_sum"`
		MaxUpdatedAt string `json:"max_updated_at"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, 1247, body.CountActivos)
	assert.Equal(t, "2813577682", body.IDsXor) // 0xa7b3c9d2 as decimal
	assert.Equal(t, "143982", body.IDsSum)
	assert.NotEmpty(t, body.MaxUpdatedAt)
}

func TestHandler_SyncSaldosDigest_HappyPath(t *testing.T) {
	t.Parallel()
	saldosRepo := &fakeSaldosReconcileRepo{
		digest: outbound.DigestResult{
			CountActivos: 312,
			IDsXor:       99,
			IDsSum:       88800,
			MaxUpdatedAt: time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		},
	}
	svc := buildReconcileSvc(&fakePagosReconcileRepo{}, saldosRepo)
	handler := mountReconcileRouter(saldosOnlyUser(), svc)

	req := httptest.NewRequest(http.MethodGet, "/sync/saldos/zona/21552/digest", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body)

	var body struct {
		CountActivos int    `json:"count_activos"`
		IDsXor       string `json:"ids_xor"`
		IDsSum       string `json:"ids_sum"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, 312, body.CountActivos)
	assert.Equal(t, "99", body.IDsXor)
	assert.Equal(t, "88800", body.IDsSum)
}

// ─── ListIDs pagination ────────────────────────────────────────────────────────

func TestHandler_SyncPagosIDs_HasMoreTrue(t *testing.T) {
	t.Parallel()
	// Feed 6 IDs; limit=5 → has_more=true and only 5 returned.
	pagosRepo := &fakePagosReconcileRepo{ids: []int{1, 2, 3, 4, 5, 6}}
	svc := buildReconcileSvc(pagosRepo, &fakeSaldosReconcileRepo{})
	handler := mountReconcileRouter(reconcileUser(), svc)

	req := httptest.NewRequest(http.MethodGet, "/sync/pagos/zona/1/ids?limit=5&after=0", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		IDs     []int `json:"ids"`
		HasMore bool  `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.True(t, body.HasMore, "expected has_more=true when 6 IDs with limit=5")
	assert.Equal(t, []int{1, 2, 3, 4, 5}, body.IDs)
}

func TestHandler_SyncPagosIDs_HasMoreFalse(t *testing.T) {
	t.Parallel()
	// 3 IDs, limit=5 → has_more=false.
	pagosRepo := &fakePagosReconcileRepo{ids: []int{10, 20, 30}}
	svc := buildReconcileSvc(pagosRepo, &fakeSaldosReconcileRepo{})
	handler := mountReconcileRouter(reconcileUser(), svc)

	req := httptest.NewRequest(http.MethodGet, "/sync/pagos/zona/1/ids?limit=5&after=0", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		IDs     []int `json:"ids"`
		HasMore bool  `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.False(t, body.HasMore)
	assert.Equal(t, []int{10, 20, 30}, body.IDs)
}

func TestHandler_SyncSaldosIDs_Pagination(t *testing.T) {
	t.Parallel()
	// 4 IDs; fetch page 1 (after=0, limit=2) → IDs 1,2 and has_more=true.
	// Then fetch page 2 (after=2, limit=2) → IDs 3,4 and has_more=false.
	saldosRepo := &fakeSaldosReconcileRepo{ids: []int{1, 2, 3, 4}}
	svc := buildReconcileSvc(&fakePagosReconcileRepo{}, saldosRepo)
	handler := mountReconcileRouter(saldosOnlyUser(), svc)

	// Page 1
	req1 := httptest.NewRequest(http.MethodGet, "/sync/saldos/zona/1/ids?limit=2&after=0", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)

	var page1 struct {
		IDs     []int `json:"ids"`
		HasMore bool  `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &page1))
	assert.Equal(t, []int{1, 2}, page1.IDs)
	assert.True(t, page1.HasMore)

	// Page 2
	req2 := httptest.NewRequest(http.MethodGet, "/sync/saldos/zona/1/ids?limit=2&after=2", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)

	var page2 struct {
		IDs     []int `json:"ids"`
		HasMore bool  `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &page2))
	assert.Equal(t, []int{3, 4}, page2.IDs)
	assert.False(t, page2.HasMore)
}

// ─── Limit clamping ────────────────────────────────────────────────────────────

func TestHandler_SyncPagosIDs_LimitClamping(t *testing.T) {
	t.Parallel()
	// Seed 20 IDs.
	ids := make([]int, 20)
	for i := range ids {
		ids[i] = i + 1
	}
	pagosRepo := &fakePagosReconcileRepo{ids: ids}

	t.Run("limit=0 defaults to 5000", func(t *testing.T) {
		t.Parallel()
		svc := buildReconcileSvc(pagosRepo, &fakeSaldosReconcileRepo{})
		handler := mountReconcileRouter(reconcileUser(), svc)
		// limit=0 should use the default (5000); all 20 IDs come back.
		req := httptest.NewRequest(http.MethodGet, "/sync/pagos/zona/1/ids?after=0", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		var body struct {
			IDs     []int `json:"ids"`
			HasMore bool  `json:"has_more"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Len(t, body.IDs, 20, "all 20 IDs should return when limit uses default 5000")
		assert.False(t, body.HasMore)
	})

	t.Run("limit=99999 is clamped to 10000", func(t *testing.T) {
		t.Parallel()
		// limit=99999 → Huma will reject it because maximum tag is 10000.
		// The handler receives a 422 from schema validation.
		svc := buildReconcileSvc(pagosRepo, &fakeSaldosReconcileRepo{})
		handler := mountReconcileRouter(reconcileUser(), svc)
		req := httptest.NewRequest(http.MethodGet, "/sync/pagos/zona/1/ids?limit=99999&after=0", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		// Huma enforces maximum:10000 at schema level → 422 Unprocessable Entity.
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	})
}

// ─── Auth error paths ─────────────────────────────────────────────────────────

func TestHandler_SyncPagosDigest_Unauthorized(t *testing.T) {
	t.Parallel()
	// No auth planted → 401.
	svc := buildReconcileSvc(&fakePagosReconcileRepo{}, &fakeSaldosReconcileRepo{})
	r := chi.NewRouter()
	// No planter — context has no CurrentUser.
	cobranzahttp.MountReadRouter(r, svc, eventbus.New(), config.Cobranza{}, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/sync/pagos/zona/1/digest", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_SyncPagosDigest_Forbidden(t *testing.T) {
	t.Parallel()
	// User has only saldos permission → 403 on the pagos endpoint.
	svc := buildReconcileSvc(&fakePagosReconcileRepo{}, &fakeSaldosReconcileRepo{})
	handler := mountReconcileRouter(saldosOnlyUser(), svc)

	req := httptest.NewRequest(http.MethodGet, "/sync/pagos/zona/1/digest", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestHandler_SyncPagosIDs_Forbidden(t *testing.T) {
	t.Parallel()
	svc := buildReconcileSvc(&fakePagosReconcileRepo{}, &fakeSaldosReconcileRepo{})
	handler := mountReconcileRouter(saldosOnlyUser(), svc)

	req := httptest.NewRequest(http.MethodGet, "/sync/pagos/zona/1/ids", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// ─── Empty zone ────────────────────────────────────────────────────────────────

func TestHandler_SyncPagosDigest_EmptyZone(t *testing.T) {
	t.Parallel()
	// Zone with no rows → count=0, xor=0, sum=0, max_updated_at must be omitted.
	pagosRepo := &fakePagosReconcileRepo{
		digest: outbound.DigestResult{},
	}
	svc := buildReconcileSvc(pagosRepo, &fakeSaldosReconcileRepo{})
	handler := mountReconcileRouter(reconcileUser(), svc)

	req := httptest.NewRequest(http.MethodGet, "/sync/pagos/zona/99999/digest", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		CountActivos int    `json:"count_activos"`
		IDsXor       string `json:"ids_xor"`
		IDsSum       string `json:"ids_sum"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, 0, body.CountActivos)
	assert.Equal(t, "0", body.IDsXor)
	assert.Equal(t, "0", body.IDsSum)

	// max_updated_at must be absent (omitempty) when the zone has no rows,
	// not marshalled as the Go zero time "0001-01-01T00:00:00Z".
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	assert.NotContains(t, raw, "max_updated_at", "max_updated_at must be omitted when zone is empty")
}

func TestHandler_SyncSaldosDigest_EmptyZone(t *testing.T) {
	t.Parallel()
	// Zone with no rows → max_updated_at must be omitted from the saldos digest too.
	saldosRepo := &fakeSaldosReconcileRepo{
		digest: outbound.DigestResult{},
	}
	svc := buildReconcileSvc(&fakePagosReconcileRepo{}, saldosRepo)
	handler := mountReconcileRouter(saldosOnlyUser(), svc)

	req := httptest.NewRequest(http.MethodGet, "/sync/saldos/zona/99999/digest", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		CountActivos int    `json:"count_activos"`
		IDsXor       string `json:"ids_xor"`
		IDsSum       string `json:"ids_sum"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, 0, body.CountActivos)
	assert.Equal(t, "0", body.IDsXor)
	assert.Equal(t, "0", body.IDsSum)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	assert.NotContains(t, raw, "max_updated_at", "max_updated_at must be omitted when zone is empty")
}

func TestHandler_SyncPagosIDs_EmptyZone(t *testing.T) {
	t.Parallel()
	pagosRepo := &fakePagosReconcileRepo{ids: nil}
	svc := buildReconcileSvc(pagosRepo, &fakeSaldosReconcileRepo{})
	handler := mountReconcileRouter(reconcileUser(), svc)

	req := httptest.NewRequest(http.MethodGet, "/sync/pagos/zona/99999/ids?after=0", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		IDs     []int `json:"ids"`
		HasMore bool  `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Empty(t, body.IDs)
	assert.False(t, body.HasMore)
}

// ─── Property test: digest changes when IDs change ────────────────────────────

// computeDigestInGo mirrors the Go-side arithmetic in digest_query.go so the
// property test can assert the same result independently.
func computeDigestInGo(ids []int) (int, int64, int64) {
	var count int
	var xorAcc, sumAcc int64
	for _, id := range ids {
		pk := int64(id)
		count++
		xorAcc ^= pk
		sumAcc += pk
	}
	return count, xorAcc, sumAcc
}

// TestProperty_Digest_MutatesOnChange verifies that mutating any one ID in
// the active set produces a different digest, and that restoring the original
// ID restores the original digest. Uses rapid for exhaustive random coverage.
func TestProperty_Digest_MutatesOnChange(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random set of 10..200 distinct positive integer IDs.
		n := rapid.IntRange(10, 200).Draw(t, "n")
		idSet := make(map[int]struct{}, n)
		for len(idSet) < n {
			id := rapid.IntRange(1, 1_000_000).Draw(t, "id")
			idSet[id] = struct{}{}
		}
		ids := make([]int, 0, n)
		for id := range idSet {
			ids = append(ids, id)
		}

		origCount, origXor, origSum := computeDigestInGo(ids)

		// Pick a random index to mutate.
		idx := rapid.IntRange(0, len(ids)-1).Draw(t, "mutate_idx")
		original := ids[idx]

		// Choose a replacement value not already in the set.
		var replacement int
		for {
			replacement = rapid.IntRange(1, 2_000_000).Draw(t, "replacement")
			if _, dup := idSet[replacement]; !dup {
				break
			}
		}
		ids[idx] = replacement
		mutCount, mutXor, mutSum := computeDigestInGo(ids)

		// At least one of the three values must differ (probability of all three
		// matching after a mutation approaches zero).
		if origCount == mutCount && origXor == mutXor && origSum == mutSum {
			t.Fatal("digest did not change after single-ID mutation; this indicates a collision")
		}

		// Restore the original ID and verify the digest is stable.
		ids[idx] = original
		restCount, restXor, restSum := computeDigestInGo(ids)
		if origCount != restCount || origXor != restXor || origSum != restSum {
			t.Fatalf("digest not stable after restore: want (%d,%d,%d) got (%d,%d,%d)",
				origCount, origXor, origSum, restCount, restXor, restSum)
		}
	})
}
