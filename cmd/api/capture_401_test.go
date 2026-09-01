package main

import (
	"bytes"
	"context"
	"database/sql"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/auth/infra/authhttp"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	failedintentblobfs "github.com/abdimuy/msp-api/internal/platform/failedintent/blobfs"
	failedintentfb "github.com/abdimuy/msp-api/internal/platform/failedintent/firebird"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// These two tests are about the chain, not about cobranza's handler: what is
// under test is where the capture instances sit relative to authn, which is
// decided here in cmd/api and nowhere else. They therefore assemble the same
// chain provideRootHandler mounts for /v2/cobranza — real authn.Handler, real
// captureAroundAuth pair, real Firebird Store — and put a stub where the pago
// handler would be.
//
// The stub is the point: a request rejected at the auth boundary never reaches
// the handler, so no handler is needed to prove that the evidence survives.

// pagoCapturePath is the captured path for cobranza pagos. Declared once so
// the census and the assertions cannot drift apart.
const pagoCapturePath = "/v2/cobranza/pagos"

// captureChainDeps is everything the assembled chain writes into.
type captureChainDeps struct {
	store *failedintentfb.Store
	blobs *failedintentblobfs.Store
}

// newCaptureChainDeps wires the real Firebird store and a filesystem blob
// store rooted in the test's temp dir.
func newCaptureChainDeps(t *testing.T, pool *firebird.Pool) captureChainDeps {
	t.Helper()
	blobs, err := failedintentblobfs.New(t.TempDir())
	require.NoError(t, err)
	return captureChainDeps{store: failedintentfb.New(pool), blobs: blobs}
}

// assembleCobranzaChain mirrors the cobranza mount in provideRootHandler:
//
//	r.Use(cobranzaCapture.OutsideAuth, authn.Handler, cobranzaCapture.InsideAuth)
//
// txInjector is the one addition — it splices the test's rollback-only
// transaction onto every request so the captured row disappears with it.
// authn is the real middleware; its dependencies are nil because a missing
// Authorization header is rejected before any of them is touched.
func assembleCobranzaChain(
	txCtx context.Context, deps captureChainDeps, planted *auth.CurrentUser, downstream http.HandlerFunc,
) http.Handler {
	configs := provideFailedIntentCapturas(deps.store, deps.blobs, nil, &config.Config{})
	capture := captureAroundAuth(configs.Cobranza)
	authn := authhttp.NewAuthnMiddleware(nil, nil, nil)

	r := chi.NewRouter()
	r.Route("/v2", func(r chi.Router) {
		r.Route("/cobranza", func(r chi.Router) {
			r.Use(txInjectorFor(txCtx, planted), capture.OutsideAuth, authn.Handler, capture.InsideAuth)
			r.Post("/pagos", downstream)
		})
	})
	return r
}

// valuesForwardingCtx keeps the request's own context — chi's RouteContext
// lives there — while falling back to the test transaction context for value
// lookups, which is how firebird.GetQuerier finds the tx.
//
//nolint:containedctx // test-only: splicing the Firebird tx context is the point.
type valuesForwardingCtx struct {
	context.Context
	values context.Context
}

func (c valuesForwardingCtx) Value(key any) any {
	if v := c.Context.Value(key); v != nil {
		return v
	}
	return c.values.Value(key)
}

// txInjectorFor splices the test transaction onto the request context, and
// optionally plants a CurrentUser. A planted user is how the authenticated
// case gets past the real authn middleware without a Firebase client:
// authn.Handler honours an already-planted user (the in-process replay path).
func txInjectorFor(txCtx context.Context, planted *auth.CurrentUser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var ctx context.Context = valuesForwardingCtx{Context: r.Context(), values: txCtx}
			if planted != nil {
				ctx = auth.PlantCurrentUser(ctx, *planted)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// buildPagoMultipart builds a body shaped like the pago upload: the `datos`
// JSON field plus one image part.
func buildPagoMultipart(t *testing.T) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("datos",
		`{"id":"6f1d6f1e-0b5e-4a3b-9c2f-6f1d6f1e0b5e","cobrador_id":7,"importe":1500.00}`))
	part, err := mw.CreateFormFile("imagen", "comprobante.jpg")
	require.NoError(t, err)
	_, err = part.Write([]byte("bytes-del-comprobante"))
	require.NoError(t, err)
	require.NoError(t, mw.Close())
	return buf.Bytes(), mw.FormDataContentType()
}

// capturedRow is the subset of MSP_FAILED_INTENTS these tests assert on.
type capturedRow struct {
	usuarioID   sql.NullString
	firebaseUID sql.NullString
	httpStatus  int
	errorCode   string
	blobPath    sql.NullString
	contentType sql.NullString
	method      string
	path        string
}

// readCapturedRow reads one row by id THROUGH the test transaction.
func readCapturedRow(ctx context.Context, t *testing.T, pool *firebird.Pool, id string) capturedRow {
	t.Helper()
	var row capturedRow
	q := firebird.GetQuerier(ctx, pool.DB)
	err := q.QueryRowContext(ctx, `
		SELECT USUARIO_ID, FIREBASE_UID, HTTP_STATUS, ERROR_CODE,
		       BODY_BLOB_PATH, BODY_CONTENT_TYPE, METHOD, PATH
		  FROM MSP_FAILED_INTENTS
		 WHERE ID = ?`, id,
	).Scan(&row.usuarioID, &row.firebaseUID, &row.httpStatus, &row.errorCode,
		&row.blobPath, &row.contentType, &row.method, &row.path)
	require.NoError(t, err, "the captured row must be readable by its id")
	return row
}

// capturedIDs lists the ids of the captured path INSIDE the test transaction.
// The dev database carries residue from earlier runs, so the tests work on the
// difference between two of these — never on an absolute count, and never on
// "the newest row", which residue could also satisfy.
func capturedIDs(ctx context.Context, t *testing.T, pool *firebird.Pool) map[string]bool {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	rows, err := q.QueryContext(ctx,
		"SELECT ID FROM MSP_FAILED_INTENTS WHERE PATH = ?", pagoCapturePath)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	ids := map[string]bool{}
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		ids[strings.TrimSpace(id)] = true
	}
	require.NoError(t, rows.Err())
	return ids
}

// newlyCaptured returns the ids present in after but not in before.
func newlyCaptured(before, after map[string]bool) []string {
	var out []string
	for id := range after {
		if !before[id] {
			out = append(out, id)
		}
	}
	return out
}

// countCommittedRows counts the same rows OUTSIDE the test transaction, so
// anything that escaped the rollback shows up as a diff instead of as residue
// nobody notices.
func countCommittedRows(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM MSP_FAILED_INTENTS WHERE PATH = ?", pagoCapturePath).Scan(&n))
	return n
}

// TestCapture_PagoWithExpiredSession_LeavesEvidence is the case that produced
// the incident: the phone posts a pago with a session the server no longer
// accepts, reads a 401, and — before this chain — left nothing behind
// anywhere. The row must exist, must carry the upload, and must leave
// USUARIO_ID null instead of guessing who sent it.
func TestCapture_PagoWithExpiredSession_LeavesEvidence(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	committedBefore := countCommittedRows(t, pool.DB)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		deps := newCaptureChainDeps(t, pool)
		reached := false
		chain := assembleCobranzaChain(ctx, deps, nil, func(w http.ResponseWriter, _ *http.Request) {
			reached = true
			w.WriteHeader(http.StatusOK)
		})

		body, contentType := buildPagoMultipart(t)
		before := capturedIDs(ctx, t, pool)

		req := httptest.NewRequest(http.MethodPost, pagoCapturePath, bytes.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		// No Authorization header: the expired-session case reaches the
		// server exactly like this one — rejected by authn, handler never run.
		rw := httptest.NewRecorder()
		chain.ServeHTTP(rw, req)

		require.Equal(t, http.StatusUnauthorized, rw.Code)
		require.False(t, reached, "authn must have cut the chain before the handler")
		assert.Empty(t, rw.Header().Get(failedintent.HeaderIntentCaptured),
			"custody is NOT promised: the header means stored AND re-dispatchable, "+
				"and a row without a usuario is not")

		fresh := newlyCaptured(before, capturedIDs(ctx, t, pool))
		require.Len(t, fresh, 1,
			"the rejected pago must leave exactly one row in MSP_FAILED_INTENTS")

		row := readCapturedRow(ctx, t, pool, fresh[0])
		assert.False(t, row.usuarioID.Valid, "no one was authenticated, so no one may be named")
		assert.Empty(t, strings.TrimSpace(row.firebaseUID.String))
		assert.Equal(t, http.StatusUnauthorized, row.httpStatus)
		assert.Equal(t, "missing_authorization", row.errorCode)
		assert.Equal(t, http.MethodPost, row.method)
		assert.Equal(t, pagoCapturePath, row.path)
		assert.Equal(t, contentType, strings.TrimSpace(row.contentType.String),
			"the boundary must survive so the body can be replayed byte-exact")

		require.True(t, row.blobPath.Valid, "the body is the evidence")
		stored, err := os.ReadFile(strings.TrimSpace(row.blobPath.String))
		require.NoError(t, err)
		assert.Equal(t, body, stored, "the stored body must be the one the phone sent")
	})

	assert.Equal(t, committedBefore, countCommittedRows(t, pool.DB),
		"nothing may escape the rollback-only transaction")
}

// TestCapture_AuthenticatedFailure_LeavesExactlyOneRow is the negative
// control. Two capture instances now see every authenticated request; a
// rejection that the in-chain instance already owns must still produce ONE
// row, with the requester on it.
//
// The Firebird Store cannot be what makes this true: its dedup only runs when
// the intent carries an Idempotency-Key (firebird/store.go:76-78) and the pago
// path sends none, so two saves here would be two rows.
func TestCapture_AuthenticatedFailure_LeavesExactlyOneRow(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	committedBefore := countCommittedRows(t, pool.DB)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		deps := newCaptureChainDeps(t, pool)
		cobrador := auth.CurrentUser{
			ID:          uuid.New(),
			FirebaseUID: "fb-uid-cobrador",
			Email:       "gabriel.roque@muebleriamsp.mx",
			Nombre:      "Gabriel Roque",
		}
		chain := assembleCobranzaChain(ctx, deps, &cobrador, func(w http.ResponseWriter, r *http.Request) {
			// A real handler parses the multipart before it can reject on a
			// business rule, so this one drains the body too.
			_ = r.ParseMultipartForm(1 << 20)
			w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"code":"pago_cargo_no_encontrado","detail":"el cargo no existe"}`))
		})

		body, contentType := buildPagoMultipart(t)
		before := capturedIDs(ctx, t, pool)

		req := httptest.NewRequest(http.MethodPost, pagoCapturePath, bytes.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		rw := httptest.NewRecorder()
		chain.ServeHTTP(rw, req)

		require.Equal(t, http.StatusUnprocessableEntity, rw.Code)
		id := rw.Header().Get(failedintent.HeaderIntentCaptured)
		require.NotEmpty(t, id, "the in-chain instance still confirms custody, as it always did")

		fresh := newlyCaptured(before, capturedIDs(ctx, t, pool))
		require.Len(t, fresh, 1,
			"an authenticated failure leaves ONE row, not one per capture instance")
		assert.Equal(t, id, fresh[0], "and it is the row the client was told about")

		row := readCapturedRow(ctx, t, pool, id)
		require.True(t, row.usuarioID.Valid, "the in-chain instance owns this row, so it carries the requester")
		assert.Equal(t, cobrador.ID.String(), strings.TrimSpace(row.usuarioID.String))
		assert.Equal(t, http.StatusUnprocessableEntity, row.httpStatus)
		assert.Equal(t, "pago_cargo_no_encontrado", row.errorCode)
	})

	assert.Equal(t, committedBefore, countCommittedRows(t, pool.DB),
		"nothing may escape the rollback-only transaction")
}
