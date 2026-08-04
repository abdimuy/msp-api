//nolint:misspell // Spanish vocabulary (cobranza, pago, etc.) by convention.
package cobranzahttp_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/app/eventbus"
	"github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp"
	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	cobranzaoutbound "github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	failedintentblobfs "github.com/abdimuy/msp-api/internal/platform/failedintent/blobfs"
	failedintentfb "github.com/abdimuy/msp-api/internal/platform/failedintent/firebird"
	failedintenthttp "github.com/abdimuy/msp-api/internal/platform/failedintent/http"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/infra/storage"
)

// ─── A2 helpers ───────────────────────────────────────────────────────────────

// a2SettableDispatcher is the test's cycle-breaking replay dispatcher. It
// mirrors cmd/api.SettableReplayDispatcher: the failedintent Service is built
// with the dispatcher first, then Set publishes the assembled root router so
// replays route back through the exact same chi + capture chain the original
// request traversed.
type a2SettableDispatcher struct{ h http.Handler }

func (d *a2SettableDispatcher) Set(h http.Handler) { d.h = h }

func (d *a2SettableDispatcher) Dispatch(w http.ResponseWriter, r *http.Request) {
	d.h.ServeHTTP(w, r)
}

// a2UsuarioLookup rebuilds the original requester for replay. In production
// this reads the auth UsuarioRepo; here we return a fixed cobranza-permitted
// user regardless of ID — the router's planter middleware re-plants the same
// user on the dispatched request anyway, so the lookup only needs to succeed.
type a2UsuarioLookup struct{ cu auth.CurrentUser }

func (l *a2UsuarioLookup) BuildCurrentUserByID(_ context.Context, _ uuid.UUID) (auth.CurrentUser, error) {
	return l.cu, nil
}

// a2FullUser is a principal holding every permission this lifecycle touches:
// creating a pago (cobranza:ver_pagos) plus inspecting/replaying/resolving a
// failed intent (failed_intents:ver / :resolver). A fresh UUID per call scopes
// the intents this test captures away from any committed rows in the shared DB.
func a2FullUser() auth.CurrentUser {
	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-a2-lifecycle",
		Email:       "a2-lifecycle@muebleriamsp.mx",
		Nombre:      "Desk Correction E2E",
		Permisos: []string{
			string(authdomain.PermCobranzaVerPagos),
			string(authdomain.PermFailedIntentsVer),
			string(authdomain.PermFailedIntentsResolver),
		},
	}
}

// a2DatosJSON builds the `datos` field JSON for a POST /pagos body.
func a2DatosJSON(pagoID string, cargoID, clienteID int, importe decimal.Decimal, fechaRFC3339 string) string {
	return `{"id":"` + pagoID + `",` +
		`"cargo_docto_cc_id":` + itoa(cargoID) + `,` +
		`"cliente_id":` + itoa(clienteID) + `,` +
		`"cobrador_id":42,"cobrador":"Ramírez García, Jorge",` +
		`"importe":"` + importe.StringFixed(2) + `",` +
		`"forma_cobro_id":87327,"fecha_hora_pago":"` + fechaRFC3339 + `"}`
}

// a2ReplayManifestForDatos builds the admin replay-with-multipart body: a
// single `__manifest` field describing a body with one part named "datos"
// whose bytes are the corrected JSON (kind=field, base64). No file uploads —
// the original capture was datos-only, so there are no parts to keep.
func a2ReplayManifestForDatos(t *testing.T, correctedDatos string) (*bytes.Buffer, string) {
	t.Helper()
	manifest := map[string]any{
		"parts": []map[string]any{
			{
				"name": "datos",
				"source": map[string]any{
					"kind":  "field",
					"value": base64.StdEncoding.EncodeToString([]byte(correctedDatos)),
				},
			},
		},
	}
	manifestJSON, err := json.Marshal(manifest)
	require.NoError(t, err)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("__manifest", string(manifestJSON)))
	require.NoError(t, mw.Close())
	return &buf, mw.FormDataContentType()
}

// a2AssembleRouter builds a router mirroring the production composition for the
// desk-correction cycle: POST /v2/cobranza/pagos under [txInjector, planter,
// cobranzaCapture] (capture scoped to the pago-write path exactly as
// cmd/api/server.go wires it) and /v2/_admin/failed-intents under [txInjector,
// planter]. The failedintent Service is built against a REAL Firebird store +
// on-disk blob storage; the replay dispatcher is published to the assembled
// root so replays route back through the same chain. Returns the root router
// and the failedintent store so tests can assert against MSP_FAILED_INTENTS.
func a2AssembleRouter(ctx context.Context, t *testing.T, pool *firebird.Pool, cu auth.CurrentUser) (http.Handler, *failedintentfb.Store) {
	t.Helper()

	fiStore := failedintentfb.New(pool)
	fiBlobs, err := failedintentblobfs.New(t.TempDir())
	require.NoError(t, err)

	// Capture scoped exactly like production: POST /v2/cobranza/pagos, >= 400.
	capture := failedintent.CaptureMiddleware(failedintent.Config{
		Store:        fiStore,
		Blob:         fiBlobs,
		PathPrefixes: []string{"/v2/cobranza/pagos"},
		Methods:      []string{http.MethodPost},
	})

	// cobranza side: real repos, fake Microsip writer (no real commit).
	fsProv, err := storage.NewFilesystemProvider(t.TempDir())
	require.NoError(t, err)
	pagosRepo := cobranzaventfb.NewPagosRecibidosRepo(pool)
	writer := &recordingMicrosipWriter{
		result: cobranzaoutbound.MicrosipPagoResult{DoctoCCID: 1, ImpteDoctoCCID: 2, Folio: "A2-001"},
	}
	svc := cobranzaapp.NewService(
		cobranzaventfb.NewSaldosRepo(pool),
		cobranzaventfb.NewPagosRepo(pool),
		cobranzaventfb.NewVentasRepo(pool),
		cobranzaoutbound.ProductionClock{},
		pagosRepo, pagosRepo, writer,
		&cobranzaStorageE2EAdapter{inner: fsProv},
		nil, firebird.NewTxManager(pool.DB),
	)

	dispatcher := &a2SettableDispatcher{}
	fiSvc := failedintenthttp.NewService(
		fiStore, dispatcher, &a2UsuarioLookup{cu: cu}, fiBlobs, nil, nil,
	)

	root := chi.NewRouter()
	root.Route("/v2", func(r chi.Router) {
		r.Route("/cobranza", func(r chi.Router) {
			// authn is bypassed via planter; capture runs inside so the
			// intent carries UsuarioID; txInjector splices the test tx.
			r.Use(txInjector(ctx), planter(cu), capture)
			cobranzahttp.MountReadRouter(
				r, svc, eventbus.New(), config.Cobranza{}, slog.Default(), nil, nil,
			)
		})
		r.Route("/_admin/failed-intents", func(r chi.Router) {
			r.Use(txInjector(ctx), planter(cu))
			failedintenthttp.MountRouter(r, fiSvc)
		})
	})
	dispatcher.Set(root)
	return root, fiStore
}

// TestE2E_CobranzaPagoExitoso_NoSeCaptura is the negative of the happy path:
// a POST /v2/cobranza/pagos that SUCCEEDS (valid cargo → 2xx) must NOT leave a
// row in MSP_FAILED_INTENTS. Capture is for failures only; a false capture on
// success would spam the desk queue and, worse, invite an operator to "re-apply"
// a pago that already landed — a double-collection risk. Runs against real
// Firebird inside the rollback-only tx.
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestE2E_CobranzaPagoExitoso_NoSeCaptura(t *testing.T) {
	e2eRequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)

		clienteID := e2eClienteID(t, q)
		importe := decimal.RequireFromString("1500.00")
		validCargo := e2eInsertCargo(t, q, clienteID, "E2E-A5-01", importe)
		e2eRequireMigration000010(t, q)
		e2eRequireCargo(ctx, t, q, validCargo)

		cu := a2FullUser()
		root, fiStore := a2AssembleRouter(ctx, t, pool, cu)

		fecha := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
		pagoID := uuid.New()
		body, ct := buildCrearPagoMultipart(
			t, a2DatosJSON(pagoID.String(), validCargo, clienteID, importe, fecha), nil,
		)
		rec := doA2Request(t, root, http.MethodPost, "/v2/cobranza/pagos", body, ct)
		require.Equal(t, http.StatusOK, rec.Code, "valid pago must succeed; body=%s", rec.Body.String())

		assert.Equal(t, 1, a2CountPagos(ctx, t, q, pagoID), "the pago must have been persisted")

		list, err := fiStore.List(ctx, failedintent.ListParams{UsuarioID: &cu.ID})
		require.NoError(t, err)
		assert.Empty(t, list.Items, "a 2xx pago must never be captured as a failed intent")
	})
}

// TestE2E_CapturaReplayResolve_CicloDeskCorrection is the only e2e that drives
// the full desk-correction lifecycle of a rejected pago against a REAL Firebird
// (everything inside the rollback-only WithTestTransaction):
//
//	Intent A — the money path:
//	  1. POST /v2/cobranza/pagos with a nonexistent cargo → 422
//	     pago_cargo_no_encontrado; cobranzaCapture persists a failed_intent
//	     row in MSP_FAILED_INTENTS (multipart body → blob on disk).
//	  2. POST /replay-with-multipart with a manifest correcting cargo_docto_cc_id
//	     to the valid seeded cargo → ReplayDispatcher re-POSTs through the same
//	     chi+capture chain → 2xx → a row lands in MSP_PAGOS_RECIBIDOS and the
//	     intent transitions to retried_ok.
//
//	Intent B — the manual-resolution path (resolve only works from `new`):
//	  3. A second nonexistent-cargo POST → 422 → captured (new).
//	  4. PATCH /resolve {status:resolved_manual} → the intent transitions to
//	     resolved_manual.
//
// This is the ONLY test that exercises editing (replay-with) and resolving a
// failed pago against Firebird for real; failedintent/http/e2e_test.go proves
// the same wiring but only with in-memory stores.
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestE2E_CapturaReplayResolve_CicloDeskCorrection(t *testing.T) {
	e2eRequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)

		clienteID := e2eClienteID(t, q)
		importe := decimal.RequireFromString("1500.00")
		validCargo := e2eInsertCargo(t, q, clienteID, "E2E-A2-01", importe)
		e2eRequireMigration000010(t, q)
		e2eRequireCargo(ctx, t, q, validCargo)

		// ── assemble a router mirroring production: cobranza pago writes under
		// authn+capture, failedintent admin under authn, all against real repos
		// + a real failedintent Firebird store, sharing the rollback tx. ──
		cu := a2FullUser()
		root, fiStore := a2AssembleRouter(ctx, t, pool, cu)

		fecha := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
		const nonexistentCargo = 999999999

		// ── Intent A · step 1: rejected POST is captured ──────────────────
		pagoA := uuid.New()
		badBody, badCT := buildCrearPagoMultipart(
			t, a2DatosJSON(pagoA.String(), nonexistentCargo, clienteID, importe, fecha), nil,
		)
		recA := doA2Request(t, root, http.MethodPost, "/v2/cobranza/pagos", badBody, badCT)
		require.Equal(t, http.StatusUnprocessableEntity, recA.Code,
			"nonexistent cargo must be rejected; body=%s", recA.Body.String())
		assert.Contains(t, recA.Body.String(), "pago_cargo_no_encontrado")

		listA, err := fiStore.List(ctx, failedintent.ListParams{UsuarioID: &cu.ID})
		require.NoError(t, err)
		require.Len(t, listA.Items, 1, "exactly one intent captured for this test's user")
		intentA := listA.Items[0]
		assert.Equal(t, "/v2/cobranza/pagos", intentA.Path)
		assert.Equal(t, failedintent.StatusNew, intentA.Status)
		require.NotEmpty(t, intentA.BodyBlobPath, "multipart capture must persist a blob")

		// No pago row exists yet — validateCargo rejects before any INSERT.
		assert.Equal(t, 0, a2CountPagos(ctx, t, q, pagoA), "rejected pago must not have been persisted")

		// ── Intent A · step 2: replay-with corrected cargo → 2xx + pago row ─
		correctedDatos := a2DatosJSON(pagoA.String(), validCargo, clienteID, importe, fecha)
		manifestBody, manifestCT := a2ReplayManifestForDatos(t, correctedDatos)
		recReplay := doA2Request(t, root, http.MethodPost,
			"/v2/_admin/failed-intents/"+intentA.ID.String()+"/replay-with-multipart",
			manifestBody, manifestCT)
		require.Equal(t, http.StatusOK, recReplay.Code,
			"replay-with admin endpoint must respond 200; body=%s", recReplay.Body.String())

		var replay struct {
			Outcome          string `json:"outcome"`
			ReplayHTTPStatus int    `json:"replay_http_status"`
		}
		require.NoError(t, json.Unmarshal(recReplay.Body.Bytes(), &replay))
		assert.Equal(t, string(failedintent.StatusRetriedOK), replay.Outcome,
			"corrected body must succeed end-to-end")
		assert.Equal(t, http.StatusOK, replay.ReplayHTTPStatus,
			"the cobranza handler's 200 must propagate through the dispatcher")

		assert.Equal(t, 1, a2CountPagos(ctx, t, q, pagoA),
			"the corrected replay must land exactly one MSP_PAGOS_RECIBIDOS row")

		gotA, err := fiStore.Get(ctx, intentA.ID)
		require.NoError(t, err)
		require.NotNil(t, gotA)
		assert.Equal(t, failedintent.StatusRetriedOK, gotA.Status,
			"a successful replay must transition the intent to retried_ok")

		// ── Intent B · steps 3-4: capture then manual resolve ─────────────
		pagoB := uuid.New()
		badBody2, badCT2 := buildCrearPagoMultipart(
			t, a2DatosJSON(pagoB.String(), nonexistentCargo, clienteID, importe, fecha), nil,
		)
		recB := doA2Request(t, root, http.MethodPost, "/v2/cobranza/pagos", badBody2, badCT2)
		require.Equal(t, http.StatusUnprocessableEntity, recB.Code, "second rejected pago; body=%s", recB.Body.String())

		listB, err := fiStore.List(ctx, failedintent.ListParams{UsuarioID: &cu.ID})
		require.NoError(t, err)
		require.Len(t, listB.Items, 2, "two intents captured for this test's user")
		intentB := a2OtherIntent(listB.Items, intentA.ID)

		recResolve := doA2Request(t, root, http.MethodPatch,
			"/v2/_admin/failed-intents/"+intentB.String()+"/resolve",
			bytes.NewBufferString(`{"status":"resolved_manual","notes":"corregido manualmente en microsip"}`),
			"application/json")
		require.Equal(t, http.StatusOK, recResolve.Code,
			"resolve must respond 200; body=%s", recResolve.Body.String())

		gotB, err := fiStore.Get(ctx, intentB)
		require.NoError(t, err)
		require.NotNil(t, gotB)
		assert.Equal(t, failedintent.StatusResolvedManual, gotB.Status,
			"resolve must transition the intent to resolved_manual")

		// No stray pago rows from the resolved-but-never-replayed intent B.
		assert.Equal(t, 0, a2CountPagos(ctx, t, q, pagoB),
			"a manually-resolved intent must not have written a pago")
	})
}

// doA2Request drives one request through the assembled root router and returns
// the recorder.
func doA2Request(t *testing.T, root http.Handler, method, target string, body *bytes.Buffer, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	return rec
}

// a2CountPagos returns the number of MSP_PAGOS_RECIBIDOS rows for the pago UUID.
func a2CountPagos(ctx context.Context, t *testing.T, q firebird.Querier, pagoID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MSP_PAGOS_RECIBIDOS WHERE ID = ?`, pagoID.String(),
	).Scan(&n))
	return n
}

// a2OtherIntent returns the id of the single item whose ID differs from exclude.
func a2OtherIntent(items []failedintent.Intent, exclude uuid.UUID) uuid.UUID {
	for _, it := range items {
		if it.ID != exclude {
			return it.ID
		}
	}
	return uuid.Nil
}
