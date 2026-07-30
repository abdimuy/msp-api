// TestE2E_Visitas_* drive the full-stack visitas write lifecycle against a
// REAL Firebird database (mueblera-firebird): POST /v2/visitas (JSON) →
// visitashttp handler → app.RegistrarVisita → visitasfb repo → MSP_VISITAS,
// with the failed-intent capture/replay/resolve desk-correction cycle for
// rejected writes. Mirrors internal/cobranza/infra/cobranzahttp's A2/A5 e2e
// lifecycle tests, stripped of the multipart/blob machinery — visitas is
// JSON-only (no comprobante attachments).
//
// Every test runs inside fbtestutil.WithTestTransaction (rollback-only): no
// row survives the test process. Skips cleanly when FB_DATABASE is unset.
//
// Run: FB_DATABASE=/firebird/data/MUEBLERA.FDB go test ./internal/visitas/infra/visitashttp/... -race
//
//nolint:misspell // visitas vocabulary is Spanish per project convention.
package visitashttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	failedintentfb "github.com/abdimuy/msp-api/internal/platform/failedintent/firebird"
	failedintenthttp "github.com/abdimuy/msp-api/internal/platform/failedintent/http"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/visitas/app"
	"github.com/abdimuy/msp-api/internal/visitas/infra/visitasfb"
	"github.com/abdimuy/msp-api/internal/visitas/infra/visitashttp"
	visitasoutbound "github.com/abdimuy/msp-api/internal/visitas/ports/outbound"
)

// ─── Markers & fixtures ─────────────────────────────────────────────────────

// visitasE2EMarker tags every visita/intent this file creates so the task
// brief's post-run residual-row check
// (`SELECT COUNT(*) FROM MSP_VISITAS WHERE COBRADOR LIKE '%<marker>%'`) can
// select on a single, unambiguous value.
const visitasE2EMarker = "TASK4_VISITAS_HTTP_E2E"

// visitasE2ETestClienteID is the shared reference cliente used across the
// module's Firebird tests (see internal/cobranza/infra/cobranzahttp and
// internal/visitas/infra/visitasfb's own e2e suite).
const visitasE2ETestClienteID = 11486

func visitasE2ERequireFBEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird E2E tests")
	}
}

// visitasE2EClienteID returns the shared test cliente ID, or skips if the
// snapshot the test runs against does not have it.
func visitasE2EClienteID(t *testing.T, q firebird.Querier) int {
	t.Helper()
	var n int
	err := q.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM CLIENTES WHERE CLIENTE_ID = ?`, visitasE2ETestClienteID,
	).Scan(&n)
	if err != nil || n == 0 {
		t.Skipf("test cliente %d not found in DB", visitasE2ETestClienteID)
	}
	return visitasE2ETestClienteID
}

// visitasE2EFullUser holds every permission this lifecycle touches: writing a
// visita (cobranza:ver_pagos, the permission CrearVisita requires) plus
// inspecting/replaying/resolving a failed intent (failed_intents:ver /
// :resolver). A fresh UUID per call scopes captured intents away from
// anything else that might exist in the shared dev DB.
func visitasE2EFullUser() auth.CurrentUser {
	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-visitas-e2e-lifecycle",
		Email:       "visitas-e2e@muebleriamsp.mx",
		Nombre:      "Visitas Lifecycle E2E",
		Permisos: []string{
			string(authdomain.PermCobranzaVerPagos),
			string(authdomain.PermFailedIntentsVer),
			string(authdomain.PermFailedIntentsResolver),
		},
	}
}

// ─── tx-context splicing (mirrors cobranzahttp's txInjector) ───────────────

// visitasValuesForwardingCtx forwards the test tx context's values into the
// httptest request so firebird.GetQuerier picks it up.
//
//nolint:containedctx // test-only: necessary to splice the Firebird tx context
type visitasValuesForwardingCtx struct {
	context.Context
	values context.Context
}

func (c visitasValuesForwardingCtx) Value(key any) any {
	if v := c.Context.Value(key); v != nil {
		return v
	}
	return c.values.Value(key)
}

// visitasTxInjector returns a chi middleware that splices the test
// transaction context onto every incoming request.
func visitasTxInjector(txCtx context.Context) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			merged := visitasValuesForwardingCtx{Context: r.Context(), values: txCtx}
			next.ServeHTTP(w, r.WithContext(merged))
		})
	}
}

// ─── replay dispatcher + usuario lookup (mirror cobranzahttp's a2 helpers) ──

// visitasSettableDispatcher is the test's cycle-breaking replay dispatcher.
// It mirrors cmd/api.SettableReplayDispatcher: the failedintent Service is
// built with the dispatcher first, then Set publishes the assembled root
// router so replays route back through the exact same chi + capture chain
// the original request traversed.
type visitasSettableDispatcher struct{ h http.Handler }

func (d *visitasSettableDispatcher) Set(h http.Handler) { d.h = h }

func (d *visitasSettableDispatcher) Dispatch(w http.ResponseWriter, r *http.Request) {
	d.h.ServeHTTP(w, r)
}

// visitasUsuarioLookup rebuilds the original requester for replay. In
// production this reads the auth UsuarioRepo; here we return a fixed
// visitas-permitted user regardless of ID — the router's planter middleware
// re-plants the same user on the dispatched request anyway, so the lookup
// only needs to succeed.
type visitasUsuarioLookup struct{ cu auth.CurrentUser }

func (l *visitasUsuarioLookup) BuildCurrentUserByID(_ context.Context, _ uuid.UUID) (auth.CurrentUser, error) {
	return l.cu, nil
}

// visitasE2EOtherIntent returns the id of the single item whose ID differs
// from exclude.
func visitasE2EOtherIntent(items []failedintent.Intent, exclude uuid.UUID) uuid.UUID {
	for _, it := range items {
		if it.ID != exclude {
			return it.ID
		}
	}
	return uuid.Nil
}

// ─── Router assembly ────────────────────────────────────────────────────────

// visitasE2EAssembleRouter builds a router mirroring the production
// composition (cmd/api/server.go): POST /v2/visitas under [txInjector,
// planter, visitasCapture] and /v2/_admin/failed-intents under [txInjector,
// planter]. The failedintent Service is built against a REAL Firebird store
// (no blob storage — visitas capture is JSON-only, Blob stays nil); the
// replay dispatcher is published to the assembled root so replays route back
// through the same chain. Returns the root router and the failedintent store
// so tests can assert against MSP_FAILED_INTENTS.
func visitasE2EAssembleRouter(ctx context.Context, t *testing.T, pool *firebird.Pool, cu auth.CurrentUser) (http.Handler, *failedintentfb.Store) {
	t.Helper()

	fiStore := failedintentfb.New(pool)

	// Capture scoped exactly like production: POST /v2/visitas, >= 400.
	capture := failedintent.CaptureMiddleware(failedintent.Config{
		Store:        fiStore,
		PathPrefixes: []string{"/v2/visitas"},
		Methods:      []string{http.MethodPost},
	})

	repo := visitasfb.New(pool)
	svc := app.NewService(repo, visitasoutbound.ProductionClock{})

	dispatcher := &visitasSettableDispatcher{}
	fiSvc := failedintenthttp.NewService(
		fiStore, dispatcher, &visitasUsuarioLookup{cu: cu}, nil, nil, nil,
	)

	root := chi.NewRouter()
	root.Route("/v2", func(r chi.Router) {
		// visitashttp.MountRouter registers the operation at bare path
		// "/visitas" — mount on a bare Group (not r.Route("/visitas")), which
		// would double the prefix. Final path: POST /v2/visitas.
		r.Group(func(r chi.Router) {
			r.Use(visitasTxInjector(ctx), planter(cu), capture)
			visitashttp.MountRouter(r, svc)
		})
		r.Route("/_admin/failed-intents", func(r chi.Router) {
			r.Use(visitasTxInjector(ctx), planter(cu))
			failedintenthttp.MountRouter(r, fiSvc)
		})
	})
	dispatcher.Set(root)
	return root, fiStore
}

// ─── Request helpers ────────────────────────────────────────────────────────

// visitasE2EBody is the JSON document for POST /v2/visitas, mirroring
// visitashttp.CrearVisitaBody's wire shape.
type visitasE2EBody struct {
	ID            string  `json:"id"`
	ClienteID     int     `json:"cliente_id"`
	CobradorID    int     `json:"cobrador_id"`
	Cobrador      string  `json:"cobrador"`
	FormaCobroID  int     `json:"forma_cobro_id"`
	Lat           float64 `json:"lat"`
	Lng           float64 `json:"lng"`
	Nota          string  `json:"nota,omitempty"`
	TipoVisita    string  `json:"tipo_visita"`
	ZonaClienteID int     `json:"zona_cliente_id"`
	Fecha         string  `json:"fecha"`
}

// visitasE2EBuildBody marshals a POST /v2/visitas JSON body with realistic
// Mexican Spanish data plus the fields the caller wants to vary.
func visitasE2EBuildBody(t *testing.T, id uuid.UUID, clienteID int, cobrador, nota, tipoVisita string, fecha time.Time) []byte {
	t.Helper()
	b := visitasE2EBody{
		ID:            id.String(),
		ClienteID:     clienteID,
		CobradorID:    42,
		Cobrador:      cobrador,
		FormaCobroID:  87327,
		Lat:           19.432608,
		Lng:           -99.133209,
		Nota:          nota,
		TipoVisita:    tipoVisita,
		ZonaClienteID: 7,
		Fecha:         fecha.UTC().Format(time.RFC3339),
	}
	out, err := json.Marshal(b)
	require.NoError(t, err)
	return out
}

// visitasE2EDoRequest drives one request through the assembled root router
// and returns the recorder. idempotencyKey is skipped when empty (used for
// the admin/_failed-intents endpoints, which do not gate on it).
func visitasE2EDoRequest(t *testing.T, root http.Handler, method, target string, body []byte, idempotencyKey string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	return rec
}

// visitasE2ECountByID returns the number of MSP_VISITAS rows for the given ID.
func visitasE2ECountByID(ctx context.Context, t *testing.T, q firebird.Querier, id uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MSP_VISITAS WHERE ID = ?`, id.String(),
	).Scan(&n))
	return n
}

// visitasE2EMarkerCobrador builds a realistic Mexican Spanish cobrador name
// carrying the residual-row marker.
func visitasE2EMarkerCobrador(base string) string {
	return base + " (" + visitasE2EMarker + ")"
}

// ─── Tests ──────────────────────────────────────────────────────────────────

// TestE2E_Visitas_HappyWrite_PersistsEveryField is case 1 of the task brief:
// a valid POST /v2/visitas with a matching Idempotency-Key persists exactly
// one MSP_VISITAS row with every field matching what was sent.
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestE2E_Visitas_HappyWrite_PersistsEveryField(t *testing.T) {
	visitasE2ERequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		clienteID := visitasE2EClienteID(t, q)

		cu := visitasE2EFullUser()
		root, _ := visitasE2EAssembleRouter(ctx, t, pool, cu)
		repo := visitasfb.New(pool)

		visitaID := uuid.New()
		fecha := time.Now().UTC().Add(-2 * time.Hour)
		cobrador := visitasE2EMarkerCobrador("María de los Ángeles Hernández")
		nota := "cliente no estaba, vuelvo mañana"
		body := visitasE2EBuildBody(t, visitaID, clienteID, cobrador, nota, "no_encontrado", fecha)

		rec := visitasE2EDoRequest(t, root, http.MethodPost, "/v2/visitas", body, visitaID.String())
		require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())

		assert.Equal(t, 1, visitasE2ECountByID(ctx, t, q, visitaID), "exactly one row must be persisted")

		got, err := repo.FindByID(ctx, visitaID)
		require.NoError(t, err)
		assert.Equal(t, visitaID, got.ID())
		assert.Equal(t, cobrador, got.Cobrador())
		assert.Equal(t, 42, got.CobradorID())
		assert.WithinDuration(t, fecha, got.Fecha(), time.Second)
		assert.Equal(t, 87327, got.FormaCobroID())
		assert.InDelta(t, 19.432608, got.Lat(), 0.000001)
		assert.InDelta(t, -99.133209, got.Lng(), 0.000001)
		assert.Equal(t, nota, got.Nota())
		assert.Equal(t, "no_encontrado", got.TipoVisita())
		assert.Equal(t, 7, got.ZonaClienteID())
		assert.Equal(t, clienteID, got.ClienteID())
		assert.Nil(t, got.ImpteDoctoCCID())
	})
}

// TestE2E_Visitas_IdempotentReplay_RowUnchanged is case 2: a second identical
// POST (same ID, same Idempotency-Key) must return 2xx with the exact same
// stored visita, and MSP_VISITAS must still hold exactly one row.
//
// Every business field must match field-for-field. CreatedAt is the sole,
// DOCUMENTED exception: MSP_VISITAS has no CREATED_AT column (see the
// domain.Visita doc comment), so the audit subrecord is in-memory only. The
// first response echoes the freshly-constructed Visita's CreatedAt; the
// replay path falls back to repo.FindByID → domain.RehydrateVisita, which
// always returns a zero-valued audit subrecord (there is nothing to read it
// back from). Asserting these differ pins down that documented behavior
// instead of silently tolerating it.
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestE2E_Visitas_IdempotentReplay_RowUnchanged(t *testing.T) {
	visitasE2ERequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		clienteID := visitasE2EClienteID(t, q)

		cu := visitasE2EFullUser()
		root, _ := visitasE2EAssembleRouter(ctx, t, pool, cu)

		visitaID := uuid.New()
		fecha := time.Now().UTC().Add(-1 * time.Hour)
		cobrador := visitasE2EMarkerCobrador("Ramírez García, Jorge")
		body := visitasE2EBuildBody(t, visitaID, clienteID, cobrador, "salió, vuelve mañana", "cobro", fecha)

		rec1 := visitasE2EDoRequest(t, root, http.MethodPost, "/v2/visitas", body, visitaID.String())
		require.Equal(t, http.StatusCreated, rec1.Code, "first: body=%s", rec1.Body.String())

		rec2 := visitasE2EDoRequest(t, root, http.MethodPost, "/v2/visitas", body, visitaID.String())
		require.Equal(t, http.StatusCreated, rec2.Code, "second: body=%s", rec2.Body.String())

		var dto1, dto2 visitashttp.VisitaDTO
		require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &dto1))
		require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &dto2))

		assert.NotEmpty(t, dto1.CreatedAt, "the original insert must echo a real created_at")
		assert.Equal(t, "0001-01-01T00:00:00Z", dto2.CreatedAt,
			"replay's created_at is a documented zero-value — MSP_VISITAS has no CREATED_AT column to rehydrate it from")

		dto1.CreatedAt, dto2.CreatedAt = "", ""
		assert.Equal(t, dto1, dto2, "every business field must match the first response exactly")

		assert.Equal(t, 1, visitasE2ECountByID(ctx, t, q, visitaID), "exactly one row after two identical POSTs")
	})
}

// TestE2E_Visitas_Exitoso_NoSeCaptura is case 3 (negative): a POST
// /v2/visitas that SUCCEEDS must NOT leave a row in MSP_FAILED_INTENTS —
// capture is for failures only.
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestE2E_Visitas_Exitoso_NoSeCaptura(t *testing.T) {
	visitasE2ERequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		clienteID := visitasE2EClienteID(t, q)

		cu := visitasE2EFullUser()
		root, fiStore := visitasE2EAssembleRouter(ctx, t, pool, cu)

		visitaID := uuid.New()
		fecha := time.Now().UTC().Add(-1 * time.Hour)
		body := visitasE2EBuildBody(t, visitaID, clienteID, visitasE2EMarkerCobrador("Hernández Solís, Patricia"), "", "cobro", fecha)

		rec := visitasE2EDoRequest(t, root, http.MethodPost, "/v2/visitas", body, visitaID.String())
		require.Equal(t, http.StatusCreated, rec.Code, "valid visita must succeed; body=%s", rec.Body.String())

		assert.Equal(t, 1, visitasE2ECountByID(ctx, t, q, visitaID))

		list, err := fiStore.List(ctx, failedintent.ListParams{UsuarioID: &cu.ID})
		require.NoError(t, err)
		assert.Empty(t, list.Items, "a 2xx visita must never be captured as a failed intent")
	})
}

// TestE2E_Visitas_CapturaReplayResolve_CicloDeskCorrection is case 4: the
// full desk-correction lifecycle of a rejected visita against a REAL
// Firebird (everything inside the rollback-only WithTestTransaction).
//
//	Intent A — the correction path:
//	  1. POST /v2/visitas with cliente_id=0 → 422 visita_cliente_requerido;
//	     visitasCapture persists a failed_intent row in MSP_FAILED_INTENTS.
//	  2. POST /replay-with with a corrected JSON body (valid cliente_id) →
//	     ReplayDispatcher re-POSTs through the same chi+capture chain → 2xx →
//	     a row lands in MSP_VISITAS and the intent transitions to retried_ok.
//
//	Intent B — the manual-resolution path (resolve only works from `new`):
//	  3. A second cliente_id=0 POST → 422 → captured (new).
//	  4. PATCH /resolve {status:resolved_manual} → the intent transitions to
//	     resolved_manual, and the visita is never written.
//
// Mirrors internal/cobranza/infra/cobranzahttp's
// TestE2E_CapturaReplayResolve_CicloDeskCorrection, using the JSON-only
// /replay-with endpoint (visitas has no blob, so /replay-with-multipart does
// not apply).
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestE2E_Visitas_CapturaReplayResolve_CicloDeskCorrection(t *testing.T) {
	visitasE2ERequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		clienteID := visitasE2EClienteID(t, q)

		cu := visitasE2EFullUser()
		root, fiStore := visitasE2EAssembleRouter(ctx, t, pool, cu)
		repo := visitasfb.New(pool)

		fecha := time.Now().UTC().Add(-1 * time.Hour)
		cobrador := visitasE2EMarkerCobrador("Ramírez García, Jorge")

		// ── Intent A · step 1: rejected POST is captured ──────────────────
		visitaA := uuid.New()
		badBodyA := visitasE2EBuildBody(t, visitaA, 0, cobrador, "", "cobro", fecha)
		recA := visitasE2EDoRequest(t, root, http.MethodPost, "/v2/visitas", badBodyA, visitaA.String())
		require.Equal(t, http.StatusUnprocessableEntity, recA.Code,
			"cliente_id=0 must be rejected; body=%s", recA.Body.String())
		assert.Contains(t, recA.Body.String(), "visita_cliente_requerido")

		listA, err := fiStore.List(ctx, failedintent.ListParams{UsuarioID: &cu.ID})
		require.NoError(t, err)
		require.Len(t, listA.Items, 1, "exactly one intent captured for this test's user")
		intentA := listA.Items[0]
		assert.Equal(t, "/v2/visitas", intentA.Path)
		assert.Equal(t, failedintent.StatusNew, intentA.Status)

		assert.Equal(t, 0, visitasE2ECountByID(ctx, t, q, visitaA), "rejected visita must not have been persisted")

		// ── Intent A · step 2: replay-with corrected cliente_id → 2xx + row ─
		correctedBodyA := visitasE2EBuildBody(t, visitaA, clienteID, cobrador, "", "cobro", fecha)
		replayWithBody, err := json.Marshal(map[string]json.RawMessage{"body": json.RawMessage(correctedBodyA)})
		require.NoError(t, err)

		recReplay := visitasE2EDoRequest(t, root, http.MethodPost,
			"/v2/_admin/failed-intents/"+intentA.ID.String()+"/replay-with",
			replayWithBody, "")
		require.Equal(t, http.StatusOK, recReplay.Code,
			"replay-with admin endpoint must respond 200; body=%s", recReplay.Body.String())

		var replay struct {
			Outcome          string `json:"outcome"`
			ReplayHTTPStatus int    `json:"replay_http_status"`
		}
		require.NoError(t, json.Unmarshal(recReplay.Body.Bytes(), &replay))
		assert.Equal(t, string(failedintent.StatusRetriedOK), replay.Outcome,
			"corrected body must succeed end-to-end")
		assert.Equal(t, http.StatusCreated, replay.ReplayHTTPStatus,
			"the visitas handler's 201 must propagate through the dispatcher")

		assert.Equal(t, 1, visitasE2ECountByID(ctx, t, q, visitaA),
			"the corrected replay must land exactly one MSP_VISITAS row")

		gotA, err := repo.FindByID(ctx, visitaA)
		require.NoError(t, err)
		assert.Equal(t, clienteID, gotA.ClienteID())

		intentAAfter, err := fiStore.Get(ctx, intentA.ID)
		require.NoError(t, err)
		require.NotNil(t, intentAAfter)
		assert.Equal(t, failedintent.StatusRetriedOK, intentAAfter.Status,
			"a successful replay must transition the intent to retried_ok")

		// ── Intent B · steps 3-4: capture then manual resolve ─────────────
		visitaB := uuid.New()
		badBodyB := visitasE2EBuildBody(t, visitaB, 0, cobrador, "", "cobro", fecha)
		recB := visitasE2EDoRequest(t, root, http.MethodPost, "/v2/visitas", badBodyB, visitaB.String())
		require.Equal(t, http.StatusUnprocessableEntity, recB.Code, "second rejected visita; body=%s", recB.Body.String())

		listB, err := fiStore.List(ctx, failedintent.ListParams{UsuarioID: &cu.ID})
		require.NoError(t, err)
		require.Len(t, listB.Items, 2, "two intents captured for this test's user")
		intentB := visitasE2EOtherIntent(listB.Items, intentA.ID)

		recResolve := visitasE2EDoRequest(t, root, http.MethodPatch,
			"/v2/_admin/failed-intents/"+intentB.String()+"/resolve",
			[]byte(`{"status":"resolved_manual","notes":"corregido manualmente en microsip"}`), "")
		require.Equal(t, http.StatusOK, recResolve.Code,
			"resolve must respond 200; body=%s", recResolve.Body.String())

		gotIntentB, err := fiStore.Get(ctx, intentB)
		require.NoError(t, err)
		require.NotNil(t, gotIntentB)
		assert.Equal(t, failedintent.StatusResolvedManual, gotIntentB.Status,
			"resolve must transition the intent to resolved_manual")

		assert.Equal(t, 0, visitasE2ECountByID(ctx, t, q, visitaB),
			"a manually-resolved intent must not have written a visita")
	})
}

// TestE2E_Visitas_AccentRoundTrip_FullStack is case 5: a POST whose nota and
// cobrador carry ñ/ó/á/é survives HTTP → app → repo → MSP_VISITAS's
// CHARACTER SET NONE columns byte-identical (no mojibake).
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestE2E_Visitas_AccentRoundTrip_FullStack(t *testing.T) {
	visitasE2ERequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		clienteID := visitasE2EClienteID(t, q)

		cu := visitasE2EFullUser()
		root, _ := visitasE2EAssembleRouter(ctx, t, pool, cu)
		repo := visitasfb.New(pool)

		visitaID := uuid.New()
		fecha := time.Now().UTC().Add(-3 * time.Hour)
		cobrador := visitasE2EMarkerCobrador("Peña Núñez, José Ángel")
		nota := "Pagó la mensualidad, muy amable; regresa en año nuevo"
		body := visitasE2EBuildBody(t, visitaID, clienteID, cobrador, nota, "cobro", fecha)

		rec := visitasE2EDoRequest(t, root, http.MethodPost, "/v2/visitas", body, visitaID.String())
		require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())

		got, err := repo.FindByID(ctx, visitaID)
		require.NoError(t, err)
		assert.Equal(t, cobrador, got.Cobrador(), "cobrador must round-trip byte-identical (no mojibake)")
		assert.Equal(t, nota, got.Nota(), "nota must round-trip byte-identical (no mojibake)")
		assert.True(t, bytes.Equal([]byte(cobrador), []byte(got.Cobrador())), "cobrador bytes must match exactly")
		assert.True(t, bytes.Equal([]byte(nota), []byte(got.Nota())), "nota bytes must match exactly")

		// Also verify via a raw SQL SELECT — proves the HTTP DTO isn't hiding a
		// decode step that masks a storage-level mismatch.
		var rawCobrador, rawNota string
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT COBRADOR, NOTA FROM MSP_VISITAS WHERE ID = ?`, visitaID.String(),
		).Scan(&rawCobrador, &rawNota))
		assert.Equal(t, cobrador, rawCobrador)
		assert.Equal(t, nota, rawNota)
	})
}
