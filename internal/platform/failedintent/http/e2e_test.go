// E2E test for the failed-intent replay cycle wiring the *real* chi
// router, AuthnMiddleware, idempotency.Middleware and CaptureMiddleware
// together — the exact configuration provideRootHandler uses in
// production. Unit tests in this package stub the dispatcher with
// fakeDispatcher and therefore miss any bug that lives in the
// middleware chain (404 from inherited chi.RouteContext, 401 from
// authn rejecting the planted CurrentUser, 409 from idempotency-key
// reuse). This test exists to make those regressions impossible to
// land silently.
//
//nolint:revive // long test names are intentional for narrative.
package failedintenthttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/infra/authhttp"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	failedintenthttp "github.com/abdimuy/msp-api/internal/platform/failedintent/http"
	"github.com/abdimuy/msp-api/internal/platform/httptesting"
	"github.com/abdimuy/msp-api/internal/platform/idempotency"
)

// TestE2E_ReplayCycle_FullMiddlewareChain drives the canonical replay
// flow through the real chi+authn+idem+capture chain:
//
//  1. POST /v2/ventas with a body the stub handler rejects (422).
//     The capture middleware persists a failed_intent.
//  2. GET /v2/_admin/failed-intents → lists the captured row.
//  3. POST /v2/_admin/failed-intents/{id}/replay → plain replay; the
//     stub still 422s but the cycle dispatches cleanly (no 404 from
//     chi route-leak, no 401 from authn rejecting planted user, no
//     409 from idempotency-key reuse).
//  4. POST /v2/_admin/failed-intents/{id}/replay-with corrected body
//     → stub returns 201; the intent transitions to retried_ok.
//
// Each step's status is asserted explicitly. Any of the three bugs the
// 5-fix sweep addressed would cause a different status:
//   - chi RouteContext leak → step 3 returns 404
//   - authn unconditional bearer requirement → step 3 returns 401
//   - idempotency-key reuse → step 4 returns 422 idempotency_key_mismatch
func TestE2E_ReplayCycle_FullMiddlewareChain(t *testing.T) {
	t.Parallel()

	stub := newStubVentasHandler()
	usuarioID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	fbUID := "e2e-firebase-uid"
	fakeFB := httptesting.NewFakeFirebase(fbUID)
	fakeUsuarios := httptesting.NewFakeUsuarioRepo()
	fakeUsuarios.AddUsuario(httptesting.AddUsuarioParams{
		ID:          usuarioID,
		FirebaseUID: fbUID,
		Email:       "e2e@example.invalid",
		Nombre:      "E2E Tester",
		Activo:      true,
		Permissions: []authdomain.Permission{
			authdomain.PermFailedIntentsVer,
			authdomain.PermFailedIntentsResolver,
		},
	})
	intentStore := newE2EIntentStore()
	idemStore := httptesting.NewInMemoryIdempotencyStore()

	router := buildE2ERouter(t, e2eRouterDeps{
		firebase:    fakeFB,
		usuarios:    fakeUsuarios,
		intentStore: intentStore,
		idemStore:   idemStore,
		ventasStub:  stub,
		usuarioID:   usuarioID,
	})

	// ── 1. POST /v2/ventas with a body the stub rejects ──────────────────
	originalBody := `{"venta_id":"v1","cliente":"x"}`
	originalIdemKey := "user-original-key"

	req1 := httptesting.NewE2ERequest(http.MethodPost, "/v2/ventas", originalBody,
		httptesting.WithIdempotencyKey(originalIdemKey))
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusUnprocessableEntity, rec1.Code,
		"stub must reject the first call; got body=%s", rec1.Body.String())

	// ── 2. List captured intents and locate the one we just produced ─────
	req2 := httptesting.NewE2ERequest(http.MethodGet,
		"/v2/_admin/failed-intents?status=new&page_size=10", "")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code,
		"list admin endpoint must respond 200; got body=%s", rec2.Body.String())

	var listResp listResponseDTO
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &listResp))
	require.NotEmpty(t, listResp.Items, "the just-captured intent must be in the list")
	intentID := listResp.Items[0].ID

	// ── 3. Plain replay — must dispatch cleanly through chi+authn+idem ──
	// The stub still 422s (no body change), so outcome=retried_fail and
	// replay_http_status=422. The key assertion is that we got 200 from
	// the admin endpoint and the dispatched response was the stub's 422
	// — not 404 (chi leak) or 401 (authn rejected planted user) or 409
	// (idem-key reuse).
	req3 := httptesting.NewE2ERequest(http.MethodPost,
		"/v2/_admin/failed-intents/"+intentID+"/replay", "")
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, req3)
	require.Equal(t, http.StatusOK, rec3.Code, "replay admin endpoint must respond 200")

	var replayResp replayResponseDTO
	require.NoError(t, json.Unmarshal(rec3.Body.Bytes(), &replayResp))
	assert.Equal(t, "retried_fail", replayResp.Outcome)
	assert.Equal(t, http.StatusUnprocessableEntity, replayResp.ReplayHTTPStatus,
		"the stub's 422 must propagate; 404 here means chi RouteContext leak, "+
			"401 means authn ignored the planted user, "+
			"409 means idempotency-key was reused")

	// ── 4. Replay-with a corrected body → stub returns 201 ──────────────
	stub.respondCreated.Store(true)
	correctedBody, err := json.Marshal(map[string]any{
		"body": json.RawMessage(`{"venta_id":"v1","cliente":"FIXED"}`),
	})
	require.NoError(t, err)

	req4 := httptesting.NewE2ERequest(http.MethodPost,
		"/v2/_admin/failed-intents/"+intentID+"/replay-with",
		string(correctedBody))
	rec4 := httptest.NewRecorder()
	router.ServeHTTP(rec4, req4)
	require.Equal(t, http.StatusOK, rec4.Code, "replay-with admin endpoint must respond 200")

	require.NoError(t, json.Unmarshal(rec4.Body.Bytes(), &replayResp))
	assert.Equal(t, "retried_ok", replayResp.Outcome,
		"corrected body must succeed end-to-end")
	assert.Equal(t, http.StatusCreated, replayResp.ReplayHTTPStatus,
		"replay_http_status must be the stub's 201; "+
			"409 here means idempotency-key reuse")

	// ── Sanity: the stub saw three calls (original + plain replay + corrected).
	assert.Equal(t, int32(3), stub.calls.Load(),
		"stub must have observed 3 dispatches: original POST + plain replay + replay-with")
}

// TestE2E_AdminFailedIntents_PermissionGrid asserts that the per-route
// RequirePermission guards inside failedintenthttp.MountRouter actually
// fire in the production composition. The test runs the same request
// against three permission configurations and asserts the expected status
// for each route:
//
//   - PermFailedIntentsVer only          → GET endpoints 200; mutating 403.
//   - PermFailedIntentsResolver only     → GET endpoints 403; mutating … 200/403 depending on route.
//   - No permissions                     → everything 403.
//
// The guard wiring was previously only exercised by unit tests that
// stubbed out the authn middleware; without this composition test a
// regression that bypassed RequirePermission (e.g. forgetting the
// `.With(...)` on a new route) would land undetected.
//
// Note: the subtests cannot run in parallel because they share
// fakeUsuarios.SetPermissions(usuarioID, ...) state.
//
//nolint:tparallel // subtests share repository state, see note above.
func TestE2E_AdminFailedIntents_PermissionGrid(t *testing.T) {
	t.Parallel()

	const fbUID = "e2e-perm-grid-uid"
	usuarioID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")

	// Plant a known intent so /{id} endpoints have something to act on.
	intentStore := newE2EIntentStore()
	seedID := uuid.New()
	bodyText := `{"venta_id":"v-perm-grid","cliente":"y"}`
	seeded := failedintent.Intent{
		ID:             seedID,
		ReceivedAt:     time.Now().UTC(),
		Method:         http.MethodPost,
		Path:           "/v2/ventas",
		Body:           []byte(bodyText),
		HTTPStatus:     http.StatusUnprocessableEntity,
		ErrorCode:      "e2e_seed",
		Status:         failedintent.StatusNew,
		UsuarioID:      &usuarioID,
		IdempotencyKey: "seed-idem-key",
		RequestID:      uuid.New(),
	}
	require.NoError(t, intentStore.Save(t.Context(), seeded))

	fakeFB := httptesting.NewFakeFirebase(fbUID)
	fakeUsuarios := httptesting.NewFakeUsuarioRepo()
	fakeUsuarios.AddUsuario(httptesting.AddUsuarioParams{
		ID:          usuarioID,
		FirebaseUID: fbUID,
		Email:       "perm@example.invalid",
		Nombre:      "Perm Tester",
		Activo:      true,
		// No permissions to start.
	})
	router := buildE2ERouter(t, e2eRouterDeps{
		firebase:    fakeFB,
		usuarios:    fakeUsuarios,
		intentStore: intentStore,
		idemStore:   httptesting.NewInMemoryIdempotencyStore(),
		ventasStub:  newStubVentasHandler(),
		usuarioID:   usuarioID,
	})

	// Each row: permission set under test → expected status for each route.
	type expect struct {
		listStatus    int
		getStatus     int
		resolveStatus int
		replayStatus  int
	}
	cases := []struct {
		name  string
		perms []authdomain.Permission
		want  expect
	}{
		{
			name:  "no permissions ⇒ all 403",
			perms: nil,
			want: expect{
				listStatus:    http.StatusForbidden,
				getStatus:     http.StatusForbidden,
				resolveStatus: http.StatusForbidden,
				replayStatus:  http.StatusForbidden,
			},
		},
		{
			name:  "Ver only ⇒ reads 200, mutations 403",
			perms: []authdomain.Permission{authdomain.PermFailedIntentsVer},
			want: expect{
				listStatus:    http.StatusOK,
				getStatus:     http.StatusOK,
				resolveStatus: http.StatusForbidden,
				replayStatus:  http.StatusForbidden,
			},
		},
		{
			name:  "Resolver only ⇒ reads 403, mutations 200 / chain-status",
			perms: []authdomain.Permission{authdomain.PermFailedIntentsResolver},
			want: expect{
				listStatus:    http.StatusForbidden,
				getStatus:     http.StatusForbidden,
				resolveStatus: http.StatusOK,
				replayStatus:  http.StatusOK,
			},
		},
	}

	// Subtests share fakeUsuarios.SetPermissions state, so they must run
	// sequentially — do NOT call t.Parallel() inside the t.Run blocks.
	for _, tc := range cases { //nolint:paralleltest,tparallel // see comment above
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fakeUsuarios.SetPermissions(usuarioID, tc.perms)

			// GET /v2/_admin/failed-intents (list)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptesting.NewE2ERequest(http.MethodGet,
				"/v2/_admin/failed-intents", ""))
			assert.Equal(t, tc.want.listStatus, rec.Code, "list: %s", rec.Body.String())

			// GET /v2/_admin/failed-intents/{id}
			rec = httptest.NewRecorder()
			router.ServeHTTP(rec, httptesting.NewE2ERequest(http.MethodGet,
				"/v2/_admin/failed-intents/"+seedID.String(), ""))
			assert.Equal(t, tc.want.getStatus, rec.Code, "get: %s", rec.Body.String())

			// PATCH /{id}/resolve — Resolver-only path. The handler may
			// fail downstream (e.g. status conflict on the second test
			// iteration) but the RequirePermission gate is what we care
			// about here; we assert 200 OR a non-403 chain-side error.
			rec = httptest.NewRecorder()
			router.ServeHTTP(rec, httptesting.NewE2ERequest(http.MethodPatch,
				"/v2/_admin/failed-intents/"+seedID.String()+"/resolve",
				`{"notes":"resolved"}`))
			if tc.want.resolveStatus == http.StatusForbidden {
				assert.Equal(t, http.StatusForbidden, rec.Code, "resolve: %s", rec.Body.String())
			} else {
				assert.NotEqual(t, http.StatusForbidden, rec.Code,
					"resolve: RequirePermission must not block this case; body=%s",
					rec.Body.String())
			}

			// POST /{id}/replay — same shape.
			rec = httptest.NewRecorder()
			router.ServeHTTP(rec, httptesting.NewE2ERequest(http.MethodPost,
				"/v2/_admin/failed-intents/"+seedID.String()+"/replay", ""))
			if tc.want.replayStatus == http.StatusForbidden {
				assert.Equal(t, http.StatusForbidden, rec.Code, "replay: %s", rec.Body.String())
			} else {
				assert.NotEqual(t, http.StatusForbidden, rec.Code,
					"replay: RequirePermission must not block this case; body=%s",
					rec.Body.String())
			}
		})
	}
}

// TestE2E_MeFailedIntents_ScopedToCurrentUser asserts that
// /v2/me/failed-intents scopes results to the calling user (no
// failed_intents:* permission gate) and never leaks intents owned by other
// usuarios. This is the regression guard against a future change that
// forgets to apply UsuarioID = &cu.ID inside MeListar — e.g. accidentally
// using the admin-style ListParams.
func TestE2E_MeFailedIntents_ScopedToCurrentUser(t *testing.T) {
	t.Parallel()

	userA := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	userB := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	const (
		fbUIDa = "e2e-me-uid-a"
		fbUIDb = "e2e-me-uid-b"
	)

	// FakeFirebase routes the bearer token to the right UID.
	fakeFB := httptesting.NewFakeFirebase("")
	fakeFB.Verify = func(token string) (*outbound.FirebaseToken, error) {
		switch token {
		case "token-A":
			return &outbound.FirebaseToken{UID: fbUIDa}, nil
		case "token-B":
			return &outbound.FirebaseToken{UID: fbUIDb}, nil
		default:
			return nil, errors.New("unknown token")
		}
	}

	fakeUsuarios := httptesting.NewFakeUsuarioRepo()
	fakeUsuarios.AddUsuario(httptesting.AddUsuarioParams{
		ID: userA, FirebaseUID: fbUIDa,
		Email: "a@example.invalid", Nombre: "User A", Activo: true,
	})
	fakeUsuarios.AddUsuario(httptesting.AddUsuarioParams{
		ID: userB, FirebaseUID: fbUIDb,
		Email: "b@example.invalid", Nombre: "User B", Activo: true,
	})

	intentStore := newE2EIntentStore()
	router := buildE2ERouter(t, e2eRouterDeps{
		firebase:    fakeFB,
		usuarios:    fakeUsuarios,
		intentStore: intentStore,
		idemStore:   httptesting.NewInMemoryIdempotencyStore(),
		ventasStub:  newStubVentasHandler(),
		usuarioID:   userA, // unused outside dispatcher wiring
	})

	// User A submits a bad POST → capture middleware persists an intent
	// owned by User A.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptesting.NewE2ERequest(http.MethodPost, "/v2/ventas",
		`{"venta_id":"v-me","cliente":"A"}`,
		httptesting.WithBearer("token-A"),
		httptesting.WithIdempotencyKey("me-idem-a")))
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code,
		"the stub must reject; got %s", rec.Body.String())

	// ── User B: GET /v2/me/failed-intents → empty (no leak) ───────────────
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptesting.NewE2ERequest(http.MethodGet,
		"/v2/me/failed-intents", "", httptesting.WithBearer("token-B")))
	require.Equal(t, http.StatusOK, rec.Code, "me listing for B must 200; %s", rec.Body.String())
	var listB listResponseDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listB))
	assert.Empty(t, listB.Items,
		"user B must not see intents owned by user A — MeListar must scope by CurrentUser.ID")

	// ── User A: GET /v2/me/failed-intents → 1 item ────────────────────────
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptesting.NewE2ERequest(http.MethodGet,
		"/v2/me/failed-intents", "", httptesting.WithBearer("token-A")))
	require.Equal(t, http.StatusOK, rec.Code, "me listing for A must 200; %s", rec.Body.String())
	var listA listResponseDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listA))
	assert.Len(t, listA.Items, 1, "user A must see exactly their own captured intent")
}

// TestE2E_Admin_BlobIntent_DTORevealsHasBlob is the regression guard for the
// IntentDTO contract that the sistema-cobro-web UI depends on to distinguish
// JSON intents (replay-with allowed) from multipart intents (replay-with
// forbidden by the backend). The UI relies on `has_blob` and
// `body_content_type` to disable the structured-edit form for blob captures.
//
// The test seeds an intent with BodyBlobPath + BodyContentType populated,
// drives the real admin chain (authn + per-route permission), and asserts
// both fields surface in the JSON projection.
func TestE2E_Admin_BlobIntent_DTORevealsHasBlob(t *testing.T) {
	t.Parallel()

	const fbUID = "e2e-blob-dto-uid"
	usuarioID := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")

	intentStore := newE2EIntentStore()
	const expectedContentType = "multipart/form-data; boundary=----WebKitFormBoundaryE2E"
	const expectedBlobPath = "/var/blobs/intents/dd/e2e-blob.bin"
	seedID := uuid.New()
	seeded := failedintent.Intent{
		ID:              seedID,
		ReceivedAt:      time.Now().UTC(),
		Method:          http.MethodPost,
		Path:            "/v2/ventas",
		Body:            nil,
		BodyBlobPath:    expectedBlobPath,
		BodyContentType: expectedContentType,
		HTTPStatus:      http.StatusUnprocessableEntity,
		ErrorCode:       "idempotency_key_mismatch",
		Status:          failedintent.StatusNew,
		UsuarioID:       &usuarioID,
		IdempotencyKey:  "blob-dto-idem-key",
		RequestID:       uuid.New(),
	}
	require.NoError(t, intentStore.Save(t.Context(), seeded))

	fakeFB := httptesting.NewFakeFirebase(fbUID)
	fakeUsuarios := httptesting.NewFakeUsuarioRepo()
	fakeUsuarios.AddUsuario(httptesting.AddUsuarioParams{
		ID:          usuarioID,
		FirebaseUID: fbUID,
		Email:       "blob-dto@example.invalid",
		Nombre:      "Blob DTO Tester",
		Activo:      true,
		Permissions: []authdomain.Permission{authdomain.PermFailedIntentsVer},
	})

	router := buildE2ERouter(t, e2eRouterDeps{
		firebase:    fakeFB,
		usuarios:    fakeUsuarios,
		intentStore: intentStore,
		idemStore:   httptesting.NewInMemoryIdempotencyStore(),
		ventasStub:  newStubVentasHandler(),
		usuarioID:   usuarioID,
	})

	// List path: has_blob must surface in the items.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptesting.NewE2ERequest(http.MethodGet,
		"/v2/_admin/failed-intents?status=new&page_size=10", ""))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var list struct {
		Items []failedintenthttp.IntentDTO `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list.Items, 1)
	got := list.Items[0]
	assert.Equal(t, seedID.String(), got.ID)
	assert.True(t, got.HasBlob, "list DTO must expose has_blob=true for blob intents")
	assert.Equal(t, expectedContentType, got.BodyContentType,
		"list DTO must expose body_content_type so the UI can show the kind/boundary")

	// Detail path: same fields surface on /{id}.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptesting.NewE2ERequest(http.MethodGet,
		"/v2/_admin/failed-intents/"+seedID.String(), ""))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var detail failedintenthttp.IntentDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &detail))
	assert.True(t, detail.HasBlob, "detail DTO must expose has_blob=true")
	assert.Equal(t, expectedContentType, detail.BodyContentType)
}

// ─── helpers below ─────────────────────────────────────────────────────────────

type e2eRouterDeps struct {
	firebase    outbound.FirebaseClient
	usuarios    outbound.UsuarioRepo
	intentStore failedintent.Store
	idemStore   idempotency.Store
	ventasStub  *stubVentasHandler
	usuarioID   uuid.UUID
}

// buildE2ERouter assembles a chi router that mirrors the production
// composition for the routes this test exercises: /v2/ventas under
// (authn, capture, idem); /v2/_admin/failed-intents under (authn) and
// dispatching back through the same root router.
func buildE2ERouter(t *testing.T, d e2eRouterDeps) *chi.Mux {
	t.Helper()

	dispatcher := &settableDispatcher{}
	usuarioLookup := &e2eUsuarioLookup{repo: d.usuarios}
	fiSvc := failedintenthttp.NewService(d.intentStore, dispatcher, usuarioLookup, nil, nil, nil)

	// nil provisioner: this suite drives the dispatcher path (CurrentUser is
	// planted directly) and never exercises lazy enrollment.
	authn := authhttp.NewAuthnMiddleware(d.firebase, d.usuarios, nil)
	idemMW := idempotency.Middleware(idempotency.Config{
		Store:      d.idemStore,
		Methods:    []string{http.MethodPost, http.MethodPatch},
		RequireKey: false,
	})
	captureMW := failedintent.CaptureMiddleware(failedintent.Config{
		Store:        d.intentStore,
		PathPrefixes: []string{"/v2/ventas"},
	})

	r := chi.NewRouter()
	r.Route("/v2", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(authn.Handler, captureMW, idemMW)
			r.Post("/ventas", d.ventasStub.ServeHTTP)
		})

		r.Route("/_admin/failed-intents", func(r chi.Router) {
			r.Use(authn.Handler)
			failedintenthttp.MountRouter(r, fiSvc)
		})

		// /me/failed-intents is the self-service endpoint scoped to the
		// authenticated user (no failed_intents:* permission required). It
		// goes through the same authn middleware as the admin chain.
		r.Route("/me/failed-intents", func(r chi.Router) {
			r.Use(authn.Handler)
			failedintenthttp.MountMeRouter(r, fiSvc)
		})
	})

	dispatcher.Set(r)
	return r
}

// ─── stub /v2/ventas handler ──────────────────────────────────────────────────

// stubVentasHandler responds 422 with a Problem-Details body until
// respondCreated is flipped, after which it responds 201. Tracks call
// count to verify the full cycle (original POST + 2 replays) happened.
type stubVentasHandler struct {
	respondCreated atomic.Bool
	calls          atomic.Int32
}

func newStubVentasHandler() *stubVentasHandler { return &stubVentasHandler{} }

func (s *stubVentasHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.calls.Add(1)
	w.Header().Set("Content-Type", "application/problem+json")
	if s.respondCreated.Load() {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"status":"created"}`))
		return
	}
	w.WriteHeader(http.StatusUnprocessableEntity)
	_, _ = w.Write([]byte(`{"code":"e2e_stub_rejected","detail":"stubbed failure"}`))
}

// ─── settableDispatcher mirroring cmd/api ─────────────────────────────────────

type settableDispatcher struct{ h http.Handler }

func (d *settableDispatcher) Set(h http.Handler) { d.h = h }
func (d *settableDispatcher) Dispatch(w http.ResponseWriter, r *http.Request) {
	d.h.ServeHTTP(w, r)
}

// ─── e2eIntentStore — in-memory failedintent.Store ────────────────────────────

type e2eIntentStore struct {
	mu      sync.Mutex
	intents map[uuid.UUID]failedintent.Intent
	order   []uuid.UUID // insertion order for stable listing
}

func newE2EIntentStore() *e2eIntentStore {
	return &e2eIntentStore{intents: map[uuid.UUID]failedintent.Intent{}}
}

func (s *e2eIntentStore) Save(_ context.Context, i failedintent.Intent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.intents[i.ID]; !exists {
		s.order = append(s.order, i.ID)
	}
	s.intents[i.ID] = i
	return nil
}

func (s *e2eIntentStore) Get(_ context.Context, id uuid.UUID) (*failedintent.Intent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.intents[id]
	if !ok {
		return nil, nil //nolint:nilnil // contract
	}
	return &i, nil
}

func (s *e2eIntentStore) List(_ context.Context, p failedintent.ListParams) (failedintent.Page[failedintent.Intent], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]failedintent.Intent, 0, len(s.order))
	// Newest first.
	for i := len(s.order) - 1; i >= 0; i-- {
		intent := s.intents[s.order[i]]
		if p.Status != "" && intent.Status != p.Status {
			continue
		}
		if p.UsuarioID != nil {
			if intent.UsuarioID == nil || *intent.UsuarioID != *p.UsuarioID {
				continue
			}
		}
		items = append(items, intent)
	}
	return failedintent.Page[failedintent.Intent]{Items: items}, nil
}

func (s *e2eIntentStore) UpdateStatus(
	_ context.Context, id uuid.UUID, expected, next failedintent.Status,
	resolvedBy uuid.UUID, notes string, now time.Time,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	intent, ok := s.intents[id]
	if !ok || intent.Status != expected {
		return errors.New("status conflict")
	}
	intent.Status = next
	resolvedCopy := now
	intent.ResolvedAt = &resolvedCopy
	intent.ResolvedBy = &resolvedBy
	intent.Notes = notes
	s.intents[id] = intent
	return nil
}

// TransitionAfterReplay mirrors the firebird.Store contract: only STATUS is
// updated, the operator-resolution fields stay exactly as they were.
func (s *e2eIntentStore) TransitionAfterReplay(
	_ context.Context, id uuid.UUID, expected, next failedintent.Status,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	intent, ok := s.intents[id]
	if !ok || intent.Status != expected {
		return errors.New("status conflict")
	}
	intent.Status = next
	s.intents[id] = intent
	return nil
}

func (s *e2eIntentStore) IncrementRetry(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if intent, ok := s.intents[id]; ok {
		intent.RetryCount++
		s.intents[id] = intent
	}
	return nil
}

func (s *e2eIntentStore) PurgeOlderThan(context.Context, time.Time) (failedintent.PurgeResult, error) {
	return failedintent.PurgeResult{}, nil
}

func (s *e2eIntentStore) ReferencedPaths(context.Context) ([]string, error) {
	return nil, nil
}

// ─── e2eUsuarioLookup — UsuarioLookup adapter for the failedintent svc ────────

type e2eUsuarioLookup struct{ repo outbound.UsuarioRepo }

func (l *e2eUsuarioLookup) BuildCurrentUserByID(ctx context.Context, id uuid.UUID) (auth.CurrentUser, error) {
	u, err := l.repo.FindByID(ctx, id)
	if err != nil {
		return auth.CurrentUser{}, err
	}
	perms, err := l.repo.PermisosFor(ctx, id)
	if err != nil {
		return auth.CurrentUser{}, err
	}
	return auth.ToContract(u, perms), nil
}

// ─── DTO mirrors for decoding admin responses ─────────────────────────────────

type listResponseDTO struct {
	Items []struct {
		ID string `json:"id"`
	} `json:"items"`
}

type replayResponseDTO struct {
	Outcome           string `json:"outcome"`
	ReplayHTTPStatus  int    `json:"replay_http_status"`
	ReplayBodyPreview string `json:"replay_body_preview"`
}
