//nolint:misspell // Spanish vocabulary (cobranza, pago, etc.) by convention.
package cobranzahttp_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
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
	cobranzafailedintents "github.com/abdimuy/msp-api/internal/cobranza/infra/failedintents"
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

// a2SavepointTxRunner is the transaction boundary the E2E harness would
// otherwise not have.
//
// The harness splices the test's rollback-only transaction onto every request,
// so firebird.TxManager finds one already on the context and joins it: a
// nested RunInTx hands commit AND rollback to the outer owner, which means a
// rejected pago would keep its row until the test ended. The very fact this
// task exists to prove — the row is gone the moment Microsip rejects — would
// be unprovable in the harness meant to prove it.
//
// A Firebird SAVEPOINT is exactly the missing boundary: it undoes everything
// the closure wrote and leaves the rows seeded by the outer transaction
// (cliente, cargo) untouched.
//
// Build it with newA2SavepointTxRunner, and only ever use it inside somebody
// else's transaction — RunInTx refuses to run without one.
type a2SavepointTxRunner struct {
	db     *sql.DB
	prefix string
	n      atomic.Int64
}

// newA2SavepointTxRunner builds a runner whose savepoint names cannot collide
// with another runner's.
//
// The counter alone is unique per INSTANCE, not per transaction: two harnesses
// assembled inside the same WithTestTransaction — nothing prevents it, and the
// helper has four callers — would both name their first savepoint SP_A2_1.
// Firebird releases the earlier savepoint when a name repeats, so a later
// ROLLBACK TO would silently undo less than this runner believes it undid.
// The random prefix removes the possibility instead of documenting it.
func newA2SavepointTxRunner(db *sql.DB) *a2SavepointTxRunner {
	// The first 8 characters of a UUID are hex digits, so the result is a
	// valid Firebird identifier well inside the length limit.
	return &a2SavepointTxRunner{db: db, prefix: "SP_A2_" + uuid.New().String()[:8] + "_"}
}

func (r *a2SavepointTxRunner) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	// Without an outer transaction GetQuerier falls back to *sql.DB
	// (transaction.go:145): the SAVEPOINT would run in autocommit on a
	// pooled connection, the closure's INSERTs would COMMIT to the shared
	// dev database, and the ROLLBACK TO could land on a different connection
	// and undo nothing at all. This double is only ever correct inside
	// somebody else's transaction, so it refuses rather than corrupt.
	if !firebird.HasTx(ctx) {
		return firebird.ErrNoTx
	}
	q := firebird.GetQuerier(ctx, r.db)
	name := r.prefix + strconv.FormatInt(r.n.Add(1), 10)
	if _, err := q.ExecContext(ctx, "SAVEPOINT "+name); err != nil {
		return err
	}

	// Panic net. The two returns below finish the savepoint themselves and
	// set finished; anything else leaving this function is fn panicking,
	// and a marker from a call that never finished must not outlive it.
	finished := false
	defer func() {
		if finished {
			return
		}
		_ = a2ReleaseAfterRollback(ctx, q, name)
	}()

	if err := fn(ctx); err != nil {
		rbErr := a2ReleaseAfterRollback(ctx, q, name)
		finished = true
		if rbErr != nil {
			return errors.Join(err, rbErr)
		}
		return err
	}

	_, relErr := q.ExecContext(ctx, "RELEASE SAVEPOINT "+name)
	finished = true
	if relErr == nil {
		return nil
	}
	// Answering "error" while the closure's writes are still in place is a
	// state production cannot produce: there, an error means nothing
	// persisted, and the caller reacts by cleaning up the blobs of a pago it
	// believes never existed. Undo first, then report.
	if rbErr := a2ReleaseAfterRollback(ctx, q, name); rbErr != nil {
		return errors.Join(relErr, rbErr)
	}
	return relErr
}

// a2ReleaseAfterRollback undoes everything written since the savepoint and
// drops the marker. Only the rollback can fail meaningfully: RELEASE merely
// discards a marker the outer transaction would discard anyway.
func a2ReleaseAfterRollback(ctx context.Context, q firebird.Querier, name string) error {
	if _, err := q.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+name); err != nil {
		return err
	}
	_, _ = q.ExecContext(ctx, "RELEASE SAVEPOINT "+name)
	return nil
}

// TestA2SavepointTxRunner_SinTransaccionDeAfuera_Rechaza is the control for
// the guard in RunInTx: with no outer transaction the runner must answer
// firebird.ErrNoTx and must NOT run the closure.
//
// That — the error, and the closure never running — is all this test
// measures. WHY the guard has to exist is a different claim and it is READ,
// not measured here: GetQuerier falls back to the pool's *sql.DB when the
// context carries no tx (transaction.go:145-150), so the closure's writes
// would land in autocommit on the shared dev database. Measuring that would
// mean committing to that database on purpose, which is exactly what the
// guard is for.
//
// The nil *sql.DB is not decoration: it is safe only because the guard
// answers before any statement is issued, so it pins that ordering too.
func TestA2SavepointTxRunner_SinTransaccionDeAfuera_Rechaza(t *testing.T) {
	t.Parallel()

	runner := newA2SavepointTxRunner(nil)
	called := false
	err := runner.RunInTx(context.Background(), func(context.Context) error {
		called = true
		return nil
	})

	require.ErrorIs(t, err, firebird.ErrNoTx)
	assert.False(t, called, "la clausura no puede correr sin transacción de afuera")
	assert.False(t, runner.HasTx(context.Background()))
}

// HasTx reports the truth: inside this harness there IS an outer transaction.
func (r *a2SavepointTxRunner) HasTx(ctx context.Context) bool { return firebird.HasTx(ctx) }

var _ cobranzaapp.TxRunner = (*a2SavepointTxRunner)(nil)

// a2Harness is everything a lifecycle test drives: the assembled root router,
// the Firebird failedintent store, the blob storage backing the captures, and
// the Microsip writer double so a test can make it reject and then accept.
type a2Harness struct {
	root    http.Handler
	intents *failedintentfb.Store
	blobs   *failedintentblobfs.Store
	writer  *recordingMicrosipWriter
}

// a2AssembleRouter builds a router mirroring the production composition for the
// desk-correction cycle: POST /v2/cobranza/pagos under [txInjector, planter,
// cobranzaCapture] (capture scoped to the pago-write path exactly as
// cmd/api/server.go wires it) and /v2/_admin/failed-intents under [txInjector,
// planter]. The failedintent Service is built against a REAL Firebird store +
// on-disk blob storage; the replay dispatcher is published to the assembled
// root so replays route back through the same chain.
//
// The Microsip writer starts out accepting; a test that needs a rejection
// calls h.writer.setErr.
func a2AssembleRouter(ctx context.Context, t *testing.T, pool *firebird.Pool, cu auth.CurrentUser) a2Harness {
	t.Helper()

	fiStore := failedintentfb.New(pool)
	fiBlobs, err := failedintentblobfs.New(t.TempDir())
	require.NoError(t, err)

	// Capture scoped exactly like production: POST /v2/cobranza/pagos, >= 400,
	// with cobranza's own resumen extractor registered under the same prefix
	// cmd/api registers it under — otherwise the captured rows would come out
	// with MODULO/RESUMEN null and this harness could not tell the difference
	// between "the extractor is not wired" and "the body had no pago in it".
	capture := failedintent.CaptureMiddleware(failedintent.Config{
		Store:        fiStore,
		Blob:         fiBlobs,
		PathPrefixes: []string{"/v2/cobranza/pagos"},
		Methods:      []string{http.MethodPost},
		Resumen: failedintent.NewRegistroExtractores().
			Registrar("/v2/cobranza/pagos", "pagos", cobranzafailedintents.NewResumenExtractor()),
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
		nil, newA2SavepointTxRunner(pool.DB),
	)

	dispatcher := &a2SettableDispatcher{}
	fiSvc := failedintenthttp.NewService(
		fiStore, dispatcher, &a2UsuarioLookup{cu: cu}, fiBlobs, nil, nil, nil,
	)

	root := chi.NewRouter()
	root.Route("/v2", func(r chi.Router) {
		r.Route("/cobranza", func(r chi.Router) {
			// authn is bypassed via planter; capture runs inside so the
			// intent carries UsuarioID; txInjector splices the test tx.
			r.Use(txInjector(ctx), planter(cu), capture)
			cobranzahttp.MountReadRouter(
				r, svc, eventbus.New(), config.Cobranza{}, slog.Default(), nil, nil, cobranzaoutbound.ProductionClock{},
			)
		})
		r.Route("/_admin/failed-intents", func(r chi.Router) {
			r.Use(txInjector(ctx), planter(cu))
			failedintenthttp.MountRouter(r, fiSvc)
		})
	})
	dispatcher.Set(root)
	return a2Harness{root: root, intents: fiStore, blobs: fiBlobs, writer: writer}
}

// a2Tables is the list every lifecycle test counts before and after, so a row
// that escaped the rollback-only transaction surfaces as a diff instead of as
// residue nobody notices.
//
//nolint:gochecknoglobals // package-private read-only constant list.
var a2Tables = []string{
	"MSP_PAGOS_RECIBIDOS",
	"MSP_PAGOS_IMAGENES",
	"MSP_FAILED_INTENTS",
}

// a2SnapshotCounts counts the rows of every table in a2Tables OUTSIDE the
// test transaction, so what it sees is what a committed write would have left.
func a2SnapshotCounts(t *testing.T, db *sql.DB) map[string]int {
	t.Helper()
	out := make(map[string]int, len(a2Tables))
	ctx := context.Background()
	for _, tbl := range a2Tables {
		var n int
		//nolint:gosec // table name comes from a package-private constant list.
		require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+tbl).Scan(&n),
			"censo de %s", tbl)
		out[tbl] = n
	}
	return out
}

// a2AssertCountsUnchanged compares two censuses table by table.
func a2AssertCountsUnchanged(t *testing.T, before, after map[string]int) {
	t.Helper()
	for _, tbl := range a2Tables {
		assert.Equal(t, before[tbl], after[tbl],
			"la tabla %s debe quedar igual (antes=%d, despues=%d)", tbl, before[tbl], after[tbl])
	}
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
		h := a2AssembleRouter(ctx, t, pool, cu)

		fecha := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
		pagoID := uuid.New()
		body, ct := buildCrearPagoMultipart(
			t, a2DatosJSON(pagoID.String(), validCargo, clienteID, importe, fecha), nil,
		)
		rec := doA2Request(t, h.root, http.MethodPost, "/v2/cobranza/pagos", body, ct)
		require.Equal(t, http.StatusOK, rec.Code, "valid pago must succeed; body=%s", rec.Body.String())

		assert.Equal(t, 1, a2CountPagos(ctx, t, q, pagoID), "the pago must have been persisted")

		list, err := h.intents.List(ctx, failedintent.ListParams{UsuarioID: &cu.ID})
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
		h := a2AssembleRouter(ctx, t, pool, cu)

		fecha := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
		const nonexistentCargo = 999999999

		// ── Intent A · step 1: rejected POST is captured ──────────────────
		pagoA := uuid.New()
		badBody, badCT := buildCrearPagoMultipart(
			t, a2DatosJSON(pagoA.String(), nonexistentCargo, clienteID, importe, fecha), nil,
		)
		recA := doA2Request(t, h.root, http.MethodPost, "/v2/cobranza/pagos", badBody, badCT)
		require.Equal(t, http.StatusUnprocessableEntity, recA.Code,
			"nonexistent cargo must be rejected; body=%s", recA.Body.String())
		assert.Contains(t, recA.Body.String(), "pago_cargo_no_encontrado")

		listA, err := h.intents.List(ctx, failedintent.ListParams{UsuarioID: &cu.ID})
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
		recReplay := doA2Request(t, h.root, http.MethodPost,
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

		gotA, err := h.intents.Get(ctx, intentA.ID)
		require.NoError(t, err)
		require.NotNil(t, gotA)
		assert.Equal(t, failedintent.StatusRetriedOK, gotA.Status,
			"a successful replay must transition the intent to retried_ok")

		// ── Intent B · steps 3-4: capture then manual resolve ─────────────
		pagoB := uuid.New()
		badBody2, badCT2 := buildCrearPagoMultipart(
			t, a2DatosJSON(pagoB.String(), nonexistentCargo, clienteID, importe, fecha), nil,
		)
		recB := doA2Request(t, h.root, http.MethodPost, "/v2/cobranza/pagos", badBody2, badCT2)
		require.Equal(t, http.StatusUnprocessableEntity, recB.Code, "second rejected pago; body=%s", recB.Body.String())

		listB, err := h.intents.List(ctx, failedintent.ListParams{UsuarioID: &cu.ID})
		require.NoError(t, err)
		require.Len(t, listB.Items, 2, "two intents captured for this test's user")
		intentB := a2OtherIntent(listB.Items, intentA.ID)

		recResolve := doA2Request(t, h.root, http.MethodPatch,
			"/v2/_admin/failed-intents/"+intentB.String()+"/resolve",
			bytes.NewBufferString(`{"status":"resolved_manual","notes":"corregido manualmente en microsip"}`),
			"application/json")
		require.Equal(t, http.StatusOK, recResolve.Code,
			"resolve must respond 200; body=%s", recResolve.Body.String())

		gotB, err := h.intents.Get(ctx, intentB)
		require.NoError(t, err)
		require.NotNil(t, gotB)
		assert.Equal(t, failedintent.StatusResolvedManual, gotB.Status,
			"resolve must transition the intent to resolved_manual")

		// No stray pago rows from the resolved-but-never-replayed intent B.
		assert.Equal(t, 0, a2CountPagos(ctx, t, q, pagoB),
			"a manually-resolved intent must not have written a pago")
	})
}

// errE2EMicrosipRejected is what the Microsip writer double answers when it
// refuses the pago: the shape of a real rejection (Cxc.exe down, DOCTO_CC
// refused, a lock that never cleared), not a validation error of ours.
var errE2EMicrosipRejected = errors.New("microsip: no se pudo registrar el DOCTO_CC")

// TestE2E_CobranzaPagoRechazadoPorMicrosip_SeCaptura proves that a pago
// Microsip rejects is answered with a failure AND left in custody, against a
// real Firebird failedintent store and a real on-disk blob.
//
// What it detects: the silent hole this branch closes. Before, the writer ran
// after the commit and its error was swallowed — the phone got 200, no intent
// was captured (capture only fires on >= 400), and the pago existed nowhere
// Microsip could see it. Nobody at the desk had a row to correct, so the debt
// stayed open and the client got charged twice.
//
// The census around the whole test proves the rollback-only transaction left
// the shared DB untouched.
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestE2E_CobranzaPagoRechazadoPorMicrosip_SeCaptura(t *testing.T) {
	e2eRequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	before := a2SnapshotCounts(t, pool.DB)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)

		clienteID := e2eClienteID(t, q)
		importe := decimal.RequireFromString("1500.00")
		validCargo := e2eInsertCargo(t, q, clienteID, "E2E-A6-01", importe)
		e2eRequireMigration000010(t, q)
		e2eRequireCargo(ctx, t, q, validCargo)

		cu := a2FullUser()
		h := a2AssembleRouter(ctx, t, pool, cu)
		// Everything about the request is valid; Microsip is what fails.
		h.writer.setErr(errE2EMicrosipRejected)

		fecha := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
		pagoID := uuid.New()
		datos := a2DatosJSON(pagoID.String(), validCargo, clienteID, importe, fecha)
		body, ct := buildCrearPagoMultipart(t, datos, nil)

		rec := doA2Request(t, h.root, http.MethodPost, "/v2/cobranza/pagos", body, ct)
		require.GreaterOrEqual(t, rec.Code, http.StatusBadRequest,
			"un rechazo de Microsip no puede contestar 2xx; body=%s", rec.Body.String())
		assert.Equal(t, 1, h.writer.calls(), "el escritor debe haberse invocado una vez")

		// The pago must not survive the rejection. Counted by cargo, not by
		// id: a row written under a different id would still be a pago this
		// request created, and counting by the unique id could not see it.
		assert.Equal(t, 0, a2CountPagosByCargo(ctx, t, q, validCargo),
			"el pago rechazado no puede quedar en MSP_PAGOS_RECIBIDOS")

		// ...and it must be in custody, with its blob and its resumen.
		list, err := h.intents.List(ctx, failedintent.ListParams{UsuarioID: &cu.ID})
		require.NoError(t, err)
		require.Len(t, list.Items, 1, "el rechazo debe dejar exactamente un intento capturado")
		intent := list.Items[0]

		assert.Equal(t, "/v2/cobranza/pagos", intent.Path)
		assert.Equal(t, http.MethodPost, intent.Method)
		assert.Equal(t, failedintent.StatusNew, intent.Status)
		assert.Equal(t, rec.Code, intent.HTTPStatus, "la fila guarda el código con que se contestó")
		require.NotNil(t, intent.UsuarioID)
		assert.Equal(t, cu.ID, *intent.UsuarioID, "el intento debe traer al cobrador que lo capturó")
		assert.Equal(t, intent.ID.String(), rec.Header().Get(failedintent.HeaderIntentCaptured),
			"el teléfono se entera por la cabecera de que el pago quedó en custodia")

		// The blob: the whole multipart body, recoverable from disk. Without
		// it there is nothing to re-dispatch when Microsip comes back.
		require.NotEmpty(t, intent.BodyBlobPath, "la captura multipart debe dejar blob")
		rc, err := h.blobs.Open(ctx, intent.BodyBlobPath)
		require.NoError(t, err, "el blob debe poder abrirse desde disco")
		defer func() { require.NoError(t, rc.Close()) }()
		guardado, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Contains(t, string(guardado), pagoID.String(),
			"el blob debe contener el cuerpo original, no un resumen de él")

		// The resumen: what makes the desk's row readable to a human.
		assert.Equal(t, "pagos", intent.Modulo)
		require.NotNil(t, intent.Resumen, "el renglón sin resumen no le dice nada a oficina")
		assert.Equal(t, "Ramírez García, Jorge", intent.Resumen.Titulo)
		assert.Equal(t, strconv.Itoa(clienteID), intent.Resumen.Referencia)
		require.NotNil(t, intent.Resumen.Monto)
		assert.True(t, importe.Equal(*intent.Resumen.Monto),
			"el monto del renglón debe ser el del pago (esperado=%s, obtenido=%s)",
			importe, intent.Resumen.Monto)
	})

	a2AssertCountsUnchanged(t, before, a2SnapshotCounts(t, pool.DB))
}

// TestE2E_CobranzaPagoRechazado_ReplayDejaExactamenteUnaFila closes the loop
// the previous test opens: Microsip comes back and the captured intent is
// re-dispatched.
//
// What it detects: a UUID burned by the failed attempt. If the rejected pago
// had left its row behind, the replay would hit the idempotent fast-path and
// answer 200 with a pago that Microsip never accepted — the same invisible
// debt, now blessed by the desk. It also detects the opposite defect: a
// replay that leaves a SECOND row behind. That one is only visible because
// the count goes by cargo — the pago id is unique, so counting by it could
// never return two. The contract is exactly one row for the cargo, and it
// must be the applied one (ESTADO='A').
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestE2E_CobranzaPagoRechazado_ReplayDejaExactamenteUnaFila(t *testing.T) {
	e2eRequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	before := a2SnapshotCounts(t, pool.DB)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)

		clienteID := e2eClienteID(t, q)
		importe := decimal.RequireFromString("1500.00")
		validCargo := e2eInsertCargo(t, q, clienteID, "E2E-A7-01", importe)
		e2eRequireMigration000010(t, q)
		e2eRequireCargo(ctx, t, q, validCargo)

		cu := a2FullUser()
		h := a2AssembleRouter(ctx, t, pool, cu)
		h.writer.setErr(errE2EMicrosipRejected)

		fecha := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
		pagoID := uuid.New()
		datos := a2DatosJSON(pagoID.String(), validCargo, clienteID, importe, fecha)

		body, ct := buildCrearPagoMultipart(t, datos, nil)
		rec := doA2Request(t, h.root, http.MethodPost, "/v2/cobranza/pagos", body, ct)
		require.GreaterOrEqual(t, rec.Code, http.StatusBadRequest,
			"el rechazo debe propagar; body=%s", rec.Body.String())
		require.Equal(t, 0, a2CountPagosByCargo(ctx, t, q, validCargo),
			"tras el rechazo MSP_PAGOS_RECIBIDOS debe quedar sin la fila")

		list, err := h.intents.List(ctx, failedintent.ListParams{UsuarioID: &cu.ID})
		require.NoError(t, err)
		require.Len(t, list.Items, 1)
		intent := list.Items[0]

		// Microsip comes back. The body does not change: what was wrong was
		// the other system, not the pago — hence a manifest that re-sends the
		// SAME datos.
		h.writer.setErr(nil)

		manifestBody, manifestCT := a2ReplayManifestForDatos(t, datos)
		recReplay := doA2Request(t, h.root, http.MethodPost,
			"/v2/_admin/failed-intents/"+intent.ID.String()+"/replay-with-multipart",
			manifestBody, manifestCT)
		require.Equal(t, http.StatusOK, recReplay.Code,
			"el endpoint de replay debe contestar 200; body=%s", recReplay.Body.String())

		var replay struct {
			Outcome          string `json:"outcome"`
			ReplayHTTPStatus int    `json:"replay_http_status"`
		}
		require.NoError(t, json.Unmarshal(recReplay.Body.Bytes(), &replay))
		assert.Equal(t, string(failedintent.StatusRetriedOK), replay.Outcome)
		assert.Equal(t, http.StatusOK, replay.ReplayHTTPStatus,
			"el éxito sigue siendo 200, no 201")

		assert.Equal(t, 1, a2CountPagosByCargo(ctx, t, q, validCargo),
			"el replay debe dejar exactamente una fila, ni cero ni dos")

		var estado string
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT ESTADO FROM MSP_PAGOS_RECIBIDOS WHERE ID = ?`, pagoID.String(),
		).Scan(&estado))
		assert.Equal(t, "A", estado,
			"la única fila debe ser la aplicada, no una pendiente invisible")

		assert.Equal(t, 2, h.writer.calls(),
			"el escritor corre una vez por intento: el rechazado y el aceptado")

		got, err := h.intents.Get(ctx, intent.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, failedintent.StatusRetriedOK, got.Status)
	})

	a2AssertCountsUnchanged(t, before, a2SnapshotCounts(t, pool.DB))
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

// a2CountPagosByCargo returns the number of MSP_PAGOS_RECIBIDOS rows charged
// to a cargo. Unlike a2CountPagos it can actually see a SECOND row: the pago
// id is a unique key, so counting by it can never return more than one and a
// spurious row under a different id would be invisible. Every test seeds its
// own cargo through GEN_ID(ID_DOCTOS), so this counts every row the test could
// possibly have created.
func a2CountPagosByCargo(ctx context.Context, t *testing.T, q firebird.Querier, cargoID int) int {
	t.Helper()
	var n int
	require.NoError(t, q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MSP_PAGOS_RECIBIDOS WHERE CARGO_DOCTO_CC_ID = ?`, cargoID,
	).Scan(&n))
	return n
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
