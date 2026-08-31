package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver used by rowState below.

	"github.com/abdimuy/msp-api/internal/canal"
	canalapp "github.com/abdimuy/msp-api/internal/canal/app"
	"github.com/abdimuy/msp-api/internal/canal/infra/canalsqlite"
	"github.com/abdimuy/msp-api/internal/platform/config"
)

// Task 11 — composition tests for cmd/winback.
//
// TESTING_REQUIREMENTS.md:119 requires every top-level route to carry a
// composition test wired through the binary's real middleware chain rather
// than a handler called in isolation. These tests build that chain by
// calling provideRouter (cmd/winback/app_wiring.go) and canal.Module()
// (internal/canal/module.go) exactly as appOptions() does — the only
// difference from production is which *config.Config and *slog.Logger are
// supplied, and that the graph is never Start()ed (so canal.Module()'s own
// 30s-default ReenvioWorker never begins ticking — see
// buildWinbackComposition's doc comment for why the forwarding/durability
// tests build their own short-interval *canalapp.ReenvioWorker off the same
// populated *canalapp.Service instead of waiting out that default).
//
// Test-only secrets; never real Meta or store credentials.
const (
	testAppSecret   = "e2e-app-secret"   //nolint:gosec // test fixture, not a real credential.
	testVerifyToken = "e2e-verify-token" //nolint:gosec // test fixture, not a real credential.
	testSharedToken = "e2e-shared-token" //nolint:gosec // test fixture, not a real credential.
	webhookPath     = "/canal/v1/webhook/whatsapp"
	saludPath       = "/canal/v1/salud"

	// signatureHeaderName is X-Hub-Signature-256, spelled out here rather
	// than importing canalhttp's unexported constant — these tests exercise
	// only the router's public HTTP surface.
	signatureHeaderName = "X-Hub-Signature-256"
)

// ── composition builder ─────────────────────────────────────────────────

// winbackComposition groups what buildWinbackComposition assembles, so a
// test can reach the router (for httptest requests), the service and repo
// (for a hand-driven ReenvioWorker and direct assertions), and assertDB
// (for rowState — see that helper's doc comment for why this is a single,
// shared, busy-timeout-armed connection rather than one opened per call).
type winbackComposition struct {
	router   chi.Router
	svc      *canalapp.Service
	repo     *canalsqlite.Repo
	dbPath   string
	assertDB *sql.DB
}

// buildWinbackComposition builds cmd/winback's real fx graph — provideRouter
// plus canal.Module(), the same two options appOptions() passes to fx.New —
// against a temp-file SQLite mailbox and forwarderURL as the store's
// endpoint. It never calls fx.App.Start: every fx.Invoke canal.Module()
// registers (registerBuzonRepoLifecycle, registerReenvioWorkerLifecycle,
// registerRoutes) already runs during fx.New itself, so the router carries
// its full real middleware-adjacent mount (RequestID, Recovery, AccessLog
// from provideRouter, then canalhttp.MountRouter from registerRoutes)
// without ever binding a TCP port or starting canal.Module()'s own worker —
// whose hardcoded ReenvioWorkerConfig{} default (30s) has no env override
// (see module.go's provideReenvioWorker) and would make every test that
// needs to observe a drain either 30s-slow or racy against that ticker.
// Tests that need to observe a drain build their own
// *canalapp.ReenvioWorker off the returned *canalapp.Service with a short
// interval — the exact production constructor
// (canalapp.NewReenvioWorker), only the cadence is test-tuned, mirroring
// internal/canal/app/reenvio_worker_test.go's own convention.
func buildWinbackComposition(t *testing.T, forwarderURL string) winbackComposition {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "buzon.sqlite")
	cfg := &config.Config{
		WhatsApp: config.WhatsApp{
			Enabled:   false, // whatsapp.NewClient returns a client that never dials out.
			AppSecret: testAppSecret,
		},
		Canal: config.Canal{
			SQLitePath:         dbPath,
			WebhookVerifyToken: testVerifyToken,
			SharedToken:        testSharedToken,
			ForwarderURL:       forwarderURL,
			ForwarderTimeout:   2 * time.Second,
		},
	}

	var router chi.Router
	var svc *canalapp.Service
	var repo *canalsqlite.Repo

	app := fx.New(
		fx.Supply(cfg),
		fx.Supply(slog.Default()),
		fx.Provide(provideRouter),
		canal.Module(),
		fx.Populate(&router, &svc, &repo),
		fx.NopLogger,
	)
	require.NoError(t, app.Err(), "the real cmd/winback fx graph must resolve cleanly")
	t.Cleanup(func() { assert.NoError(t, repo.Close()) })

	// One assertion connection for the whole test, armed with busy_timeout
	// — see rowState's doc comment for why this replaced a fresh
	// sql.Open per poll.
	assertDB, err := sql.Open("sqlite", dbPath+"?_busy_timeout=5000")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, assertDB.Close()) })

	return winbackComposition{router: router, svc: svc, repo: repo, dbPath: dbPath, assertDB: assertDB}
}

// startDrainWorker builds and starts a *canalapp.ReenvioWorker against
// comp.svc with a fast interval so a test can observe a drain without
// waiting out canal.Module()'s 30s production default. Stops on cleanup.
func startDrainWorker(t *testing.T, comp winbackComposition, interval time.Duration) {
	t.Helper()
	w := canalapp.NewReenvioWorker(comp.svc, canalapp.ReenvioWorkerConfig{Interval: interval, Batch: 10}, nil)
	require.NoError(t, w.Start(t.Context()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		assert.NoError(t, w.Stop(ctx))
	})
}

// ── the store's receiver, faked ─────────────────────────────────────────

// storeReceiver fakes the store's on-premise POST /mensaje-entrante
// endpoint. up toggles whether it answers 200 (healthy) or 503 (down) —
// exactly what ADR-0010 describes as the routine state of a server started
// by hand behind a tunnel that rotates hourly. wamids records every wamid
// the fake actually received a forwarded push for.
type storeReceiver struct {
	up atomic.Bool

	mu     sync.Mutex
	wamids []string
	tokens []string
}

func newStoreReceiver(up bool) *storeReceiver {
	s := &storeReceiver{}
	s.up.Store(up)
	return s
}

func (s *storeReceiver) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.tokens = append(s.tokens, r.Header.Get("X-Canal-Token"))
		s.mu.Unlock()

		if !s.up.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		var payload struct {
			Wamid string `json:"wamid"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		s.mu.Lock()
		s.wamids = append(s.wamids, payload.Wamid)
		s.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}
}

func (s *storeReceiver) received(wamid string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.wamids {
		if w == wamid {
			return true
		}
	}
	return false
}

// sawToken reports whether the fake ever received sharedToken on the
// sharedTokenHeader — proves ForwarderClient authenticates to the store.
func (s *storeReceiver) sawToken(sharedToken string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, tok := range s.tokens {
		if tok == sharedToken {
			return true
		}
	}
	return false
}

// ── webhook fixtures ─────────────────────────────────────────────────────

// signBody computes Meta's own "sha256=<hex>" HMAC-SHA256 signature of body
// under secret — mirrors canalhttp_test's private helper of the same name
// (internal/canal/infra/canalhttp/helpers_test.go), duplicated here because
// that helper is unexported in a `_test.go` file this package cannot import.
func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body) // hash.Hash.Write never returns a non-nil error.
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// metaTextPayload builds a minimal, realistic Meta WhatsApp webhook POST
// body carrying one inbound text message. Mirrors canalhttp_test's helper
// of the same name for the same reason as signBody.
func metaTextPayload(wamid, from, phoneNumberID, texto string, ts time.Time) []byte {
	return []byte(fmt.Sprintf(`{
		"object": "whatsapp_business_account",
		"entry": [{
			"id": "waba-1",
			"changes": [{
				"field": "messages",
				"value": {
					"messaging_product": "whatsapp",
					"metadata": {"display_phone_number": "5215500000000", "phone_number_id": %q},
					"messages": [{
						"from": %q,
						"id": %q,
						"timestamp": %q,
						"type": "text",
						"text": {"body": %q}
					}]
				}
			}]
		}]
	}`, phoneNumberID, from, wamid, strconv.FormatInt(ts.Unix(), 10), texto))
}

// rowState queries the mailbox's estado/motivo_fallo for wamid directly
// against db — comp.assertDB, a connection separate from the one
// buildWinbackComposition's repo holds, since outbound.BuzonRepo exposes no
// per-row getter (see internal/canal/app/service.go's doc comment on
// RecibirMensaje for why that is deliberate) and the composition/
// durability tests need to assert the exact estado a row is in, not just
// the pendiente count.
//
// db is opened once per test (buildWinbackComposition), not once per call:
// a fresh sql.Open on every poll of a require.Eventually loop — one test
// used to do exactly that — opens a fresh connection racing the drain
// worker's own write through the repo's own connection, and modernc.org/
// sqlite's default busy behaviour is to fail SQLITE_BUSY immediately
// rather than wait. That produced a genuine flake (a "database is locked"
// error surfacing through require.NoError below, from inside
// require.Eventually's polling goroutine, rendered as "condition never
// satisfied" at the top): the durability logic itself was correct and
// fast in every captured run, the harness was just racing its own
// assertion against the code under test for a lock. assertDB's DSN also
// carries "?_busy_timeout=5000" as defence in depth — a poll waits up to
// 5s for the writer instead of erroring — but the single long-lived
// connection is what actually removes the race (see
// buildWinbackComposition). Never add busy_timeout to
// internal/canal/infra/canalsqlite to chase this: production runs one
// process with MaxOpenConns(1), so this contention is the test harness's
// own creation, not something the production code needs to accommodate.
func rowState(t *testing.T, db *sql.DB, wamid string) (string, string, bool) {
	t.Helper()

	var estado, motivo string
	err := db.QueryRow(
		`SELECT estado, motivo_fallo FROM mensajes_entrantes WHERE wamid = ?`, wamid,
	).Scan(&estado, &motivo)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false
	}
	require.NoError(t, err)
	return estado, motivo, true
}

// ── composition tests ────────────────────────────────────────────────────

func TestE2E_WebhookChallenge_EchoesHubChallenge(t *testing.T) {
	t.Parallel()
	store := httptest.NewServer(newStoreReceiver(true).handler())
	t.Cleanup(store.Close)
	comp := buildWinbackComposition(t, store.URL)

	q := url.Values{
		"hub.mode":         {"subscribe"},
		"hub.verify_token": {testVerifyToken},
		"hub.challenge":    {"e2e-challenge-12345"},
	}
	req := httptest.NewRequest(http.MethodGet, webhookPath+"?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	comp.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "e2e-challenge-12345", rec.Body.String())
}

func TestE2E_WebhookPost_SignedRequest_LandsInSQLite(t *testing.T) {
	t.Parallel()
	store := httptest.NewServer(newStoreReceiver(true).handler())
	t.Cleanup(store.Close)
	comp := buildWinbackComposition(t, store.URL)

	body := metaTextPayload("wamid.e2e-lands", "5215511112222", "1234567890", "hola", time.Now())
	req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(body))
	req.Header.Set(signatureHeaderName, signBody(testAppSecret, body))
	rec := httptest.NewRecorder()
	comp.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	estado, _, found := rowState(t, comp.assertDB, "wamid.e2e-lands")
	require.True(t, found, "the signed POST must have persisted a row keyed by wamid")
	assert.Equal(t, "pendiente", estado)
}

func TestE2E_WebhookPost_SameWamidTwice_200TwiceOneRow(t *testing.T) {
	t.Parallel()
	store := httptest.NewServer(newStoreReceiver(true).handler())
	t.Cleanup(store.Close)
	comp := buildWinbackComposition(t, store.URL)

	body := metaTextPayload("wamid.e2e-redelivered", "5215511112222", "1234567890", "hola", time.Now())
	sig := signBody(testAppSecret, body)

	var codes []int
	for range 2 {
		req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(body))
		req.Header.Set(signatureHeaderName, sig)
		rec := httptest.NewRecorder()
		comp.router.ServeHTTP(rec, req)
		codes = append(codes, rec.Code)
	}
	assert.Equal(t, []int{http.StatusOK, http.StatusOK}, codes, "Meta's redelivery must see 200 both times")

	var count int
	require.NoError(t, comp.assertDB.QueryRow(
		`SELECT COUNT(*) FROM mensajes_entrantes WHERE wamid = ?`, "wamid.e2e-redelivered",
	).Scan(&count))
	assert.Equal(t, 1, count, "a redelivered wamid must not create a second row")
}

func TestE2E_ReenvioWorker_ForwardsPendienteToStore(t *testing.T) {
	t.Parallel()
	store := newStoreReceiver(true)
	srv := httptest.NewServer(store.handler())
	t.Cleanup(srv.Close)
	comp := buildWinbackComposition(t, srv.URL)

	body := metaTextPayload("wamid.e2e-forwarded", "5215511112222", "1234567890", "hola", time.Now())
	req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(body))
	req.Header.Set(signatureHeaderName, signBody(testAppSecret, body))
	rec := httptest.NewRecorder()
	comp.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	startDrainWorker(t, comp, 15*time.Millisecond)

	require.Eventually(t, func() bool {
		estado, _, found := rowState(t, comp.assertDB, "wamid.e2e-forwarded")
		return found && estado == "reenviado"
	}, 2*time.Second, 10*time.Millisecond, "the worker must drain the pendiente to the store and mark it reenviado")

	assert.True(t, store.received("wamid.e2e-forwarded"), "the store's fake endpoint must have actually received the push")
	assert.True(t, store.sawToken(testSharedToken), "the forwarder must authenticate to the store with the shared token")
}

// TestE2E_Durability_StoreDown_MessageSurvives_ThenForwardedOnceStoreReturns
// proves the mailbox's whole reason for existing (per task-11-brief.md,
// marked 🔴): while the store's on-premise receiver is down, an inbound
// message must survive in the mailbox as pendiente; once the receiver
// returns, it must be forwarded. Per ADR-0010 the store's server is started
// by hand behind a tunnel that rotates hourly, so a worker tick landing
// during an outage is the routine case, not an edge one — this test lets
// the real ReenvioWorker tick against a genuinely-down receiver (through the
// real fx-built *canalapp.Service, default ReenvioConfig included — see
// buildWinbackComposition) before bringing it back, rather than avoiding
// the question by never draining until after the receiver is healthy.
//
// 🔴 As written, this test currently FAILS — see task-11-report.md's defect
// section. reliability.DefaultRetry's MaxAttempts (3) is smaller than
// reliability.DefaultCircuit's FailureThreshold (5): a single pendiente's
// retry-exhaustion (internal/canal/app/service.go's intentarReenvio) always
// finishes and calls MarcarFallido before the circuit breaker ever
// accumulates enough failures to open and skip it instead. Once fallido,
// nothing in app/ or infra/ ever calls domain's own MarcarPendiente to
// requeue it — outbound.BuzonRepo has no such method — so the message is
// never forwarded even after the receiver returns. Left failing rather than
// weakened, per task-11-brief.md's "do not fix production code" instruction.
func TestE2E_Durability_StoreDown_MessageSurvives_ThenForwardedOnceStoreReturns(t *testing.T) {
	t.Parallel()
	store := newStoreReceiver(false) // the store's receiver starts down.
	srv := httptest.NewServer(store.handler())
	t.Cleanup(srv.Close)
	comp := buildWinbackComposition(t, srv.URL)

	body := metaTextPayload("wamid.e2e-durable", "5215511112222", "1234567890", "hola", time.Now())
	req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(body))
	req.Header.Set(signatureHeaderName, signBody(testAppSecret, body))
	rec := httptest.NewRecorder()
	comp.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	estado, _, found := rowState(t, comp.assertDB, "wamid.e2e-durable")
	require.True(t, found)
	require.Equal(t, "pendiente", estado, "the message must persist even though the store is unreachable at receipt time")

	startDrainWorker(t, comp, 20*time.Millisecond)

	// Give the worker time for a full retry-exhaustion cycle against the
	// down receiver (observed ~450ms for MaxAttempts=3 with backoff+jitter;
	// 1.5s leaves comfortable margin without the test itself waiting long).
	time.Sleep(1500 * time.Millisecond)
	estado, motivo, found := rowState(t, comp.assertDB, "wamid.e2e-durable")
	require.True(t, found)
	assert.Equal(t, "pendiente", estado,
		"the mailbox must survive a sustained outage in pendiente, not fallido — motivo=%q", motivo)

	store.up.Store(true)

	require.Eventually(t, func() bool {
		estado, _, found := rowState(t, comp.assertDB, "wamid.e2e-durable")
		return found && estado == "reenviado"
	}, 2*time.Second, 10*time.Millisecond, "once the store returns, the pendiente must be forwarded and marked reenviado")
}
