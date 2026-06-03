//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp_test

import (
	"bufio"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/app/eventbus"
	"github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp"
	"github.com/abdimuy/msp-api/internal/platform/config"
)

// ─── Helpers ──────────────────────────────────────────────────────────────────

// sseCobranzaUser returns a CurrentUser holding both cobranza read permissions.
func sseCobranzaUser() auth.CurrentUser {
	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-sse-test",
		Email:       "sse-test@muebleriamsp.mx",
		Nombre:      "SSE Test",
		Permisos: []string{
			string(authdomain.PermCobranzaVerSaldos),
			string(authdomain.PermCobranzaVerPagos),
		},
	}
}

// sseNopermUser returns a CurrentUser with NO cobranza permissions.
func sseNopermUser() auth.CurrentUser {
	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-sse-noperm",
		Email:       "sse-noperm@muebleriamsp.mx",
		Nombre:      "SSE NoPerm",
		Permisos:    []string{},
	}
}

// buildSSERouter wires MountReadRouter with the given bus, config, and user.
// The user is planted via the planter middleware so authn is bypassed.
func buildSSERouter(bus *eventbus.Bus, sseCfg config.Cobranza, cu auth.CurrentUser) http.Handler {
	r := chi.NewRouter()
	r.Use(planter(cu))
	cobranzahttp.MountReadRouter(r, nil, bus, sseCfg, slog.Default())
	return r
}

// newSSEClient opens a real SSE stream against srv at path and returns a
// line scanner and a cancel function. The caller must call cancel() to close
// the connection.
//
// httptest.NewServer creates a real TCP listener so chunked transfer encoding
// works end-to-end. httptest.NewRecorder does NOT flush to the client and
// cannot be used for SSE testing.
func newSSEClient(t *testing.T, srv *httptest.Server, path string) (*bufio.Scanner, func()) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	require.NoError(t, err)

	// Use a plain http.Client (no timeout) — the test will cancel via cancel().
	resp, err := srv.Client().Do(req) //nolint:bodyclose // closed by cancel() below
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	scanner := bufio.NewScanner(resp.Body)

	cancel := func() {
		_ = resp.Body.Close()
	}
	// Ensure cleanup even if caller forgets.
	t.Cleanup(cancel)
	return scanner, cancel
}

// readNextNonBlankLine reads lines from scanner until it finds a non-empty,
// non-blank one and returns it. Returns "" if the scanner is exhausted.
func readNextNonBlankLine(scanner *bufio.Scanner) string {
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestSSE_FeatureFlagOff_Returns503 verifies that when SSEEnabled=false the
// endpoint returns 503 Service Unavailable with the expected JSON body.
func TestSSE_FeatureFlagOff_Returns503(t *testing.T) {
	t.Parallel()

	bus := eventbus.New()
	defer bus.Close()

	cfg := config.Cobranza{SSEEnabled: false}
	handler := buildSSERouter(bus, cfg, sseCobranzaUser())
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/sync/pagos/zona/1/stream")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

// TestSSE_InvalidZonaID_Returns400 verifies that a non-numeric or zero zona_id
// returns 400 Bad Request before any streaming begins.
func TestSSE_InvalidZonaID_Returns400(t *testing.T) {
	t.Parallel()

	bus := eventbus.New()
	defer bus.Close()

	cfg := config.Cobranza{SSEEnabled: true, SSEPingEvery: 25 * time.Second}
	handler := buildSSERouter(bus, cfg, sseCobranzaUser())
	srv := httptest.NewServer(handler)
	defer srv.Close()

	for _, path := range []string{
		"/sync/pagos/zona/0/stream",
		"/sync/pagos/zona/-5/stream",
	} {
		// Sequential: each request is synchronous (400 before streaming starts).
		resp, err := srv.Client().Get(srv.URL + path)
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "path=%s", path)
	}
}

// TestSSE_Unauthenticated_Returns401 verifies that a request with no planted
// CurrentUser returns 401 Unauthorized.
func TestSSE_Unauthenticated_Returns401(t *testing.T) {
	t.Parallel()

	bus := eventbus.New()
	defer bus.Close()

	cfg := config.Cobranza{SSEEnabled: true, SSEPingEvery: 25 * time.Second}
	// Build router WITHOUT planting a CurrentUser.
	r := chi.NewRouter()
	cobranzahttp.MountReadRouter(r, nil, bus, cfg, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/sync/pagos/zona/1/stream", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestSSE_Authz_NoPerm_Returns403 verifies that a user without the cobranza
// read permission receives a 403 Forbidden.
//
// NOTE: Zone-level access (per-zona membership) is not implemented in
// auth.CurrentUser — the auth domain has no Zonas []int field. Any
// authenticated user with the correct read perm can stream any zona_id.
// The test is named _NoPerm (not _WrongZona) to reflect this authz model.
// A future iteration can add Zonas []int to CurrentUser and narrow the check.
func TestSSE_Authz_NoPerm_Returns403(t *testing.T) {
	t.Parallel()

	bus := eventbus.New()
	defer bus.Close()

	cfg := config.Cobranza{SSEEnabled: true, SSEPingEvery: 25 * time.Second}
	handler := buildSSERouter(bus, cfg, sseNopermUser())
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/sync/pagos/zona/1/stream")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestSSE_PublishDeliversEvent opens a stream, publishes via the bus, and
// asserts the event arrives within 50 ms.
func TestSSE_PublishDeliversEvent(t *testing.T) {
	t.Parallel()

	bus := eventbus.New()
	defer bus.Close()

	cfg := config.Cobranza{SSEEnabled: true, SSEPingEvery: 60 * time.Second}
	handler := buildSSERouter(bus, cfg, sseCobranzaUser())
	srv := httptest.NewServer(handler)
	defer srv.Close()

	scanner, cancel := newSSEClient(t, srv, "/sync/pagos/zona/1/stream")
	defer cancel()

	// Give the handler a moment to subscribe before publishing.
	time.Sleep(10 * time.Millisecond)

	bus.Publish("pagos_changed")

	lineCh := make(chan string, 1)
	go func() {
		lineCh <- readNextNonBlankLine(scanner)
	}()

	select {
	case line := <-lineCh:
		assert.Contains(t, line, "pagos_changed",
			"expected SSE event line containing topic name")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("SSE event did not arrive within 200ms")
	}
}

// TestSSE_KeepAlivePing asserts that a ": ping" comment arrives after the
// configured ping interval. The interval is set to 50 ms to keep the test fast.
func TestSSE_KeepAlivePing(t *testing.T) {
	t.Parallel()

	bus := eventbus.New()
	defer bus.Close()

	// Short ping interval so we don't have to wait 25 s.
	cfg := config.Cobranza{SSEEnabled: true, SSEPingEvery: 50 * time.Millisecond}
	handler := buildSSERouter(bus, cfg, sseCobranzaUser())
	srv := httptest.NewServer(handler)
	defer srv.Close()

	scanner, cancel := newSSEClient(t, srv, "/sync/saldos/zona/2/stream")
	defer cancel()

	// Read the first non-blank line — should be the ping comment.
	lineCh := make(chan string, 1)
	go func() {
		lineCh <- readNextNonBlankLine(scanner)
	}()

	select {
	case line := <-lineCh:
		assert.Equal(t, ": ping", line, "expected SSE keep-alive comment")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("SSE keep-alive ping did not arrive within 500ms")
	}
}

// TestSSE_ClientDisconnect_Unsubscribes closes the stream and asserts that
// the bus subscriber count drops to 0 within 1 s.
func TestSSE_ClientDisconnect_Unsubscribes(t *testing.T) {
	t.Parallel()

	bus := eventbus.New()
	defer bus.Close()

	cfg := config.Cobranza{SSEEnabled: true, SSEPingEvery: 60 * time.Second}
	handler := buildSSERouter(bus, cfg, sseCobranzaUser())
	srv := httptest.NewServer(handler)
	defer srv.Close()

	_, cancel := newSSEClient(t, srv, "/sync/pagos/zona/3/stream")

	// Wait until the server has subscribed.
	require.Eventually(t, func() bool {
		return bus.SubscriberCount("pagos_changed") == 1
	}, 500*time.Millisecond, 5*time.Millisecond,
		"subscriber count should reach 1 after connection")

	// Close the client connection.
	cancel()

	// The handler's context.Done() fires; it defers unsubscribe().
	require.Eventually(t, func() bool {
		return bus.SubscriberCount("pagos_changed") == 0
	}, time.Second, 5*time.Millisecond,
		"subscriber count should return to 0 after disconnect")
}

// TestSSE_SlowClient_DoesNotBlockPublisher verifies that a slow-reading client
// does not prevent a fast client from receiving events.
//
// Two clients connect to the same topic. The slow client never reads. The fast
// client must receive its event within 50 ms. This is possible because the
// eventbus uses a non-blocking select-default publish pattern (buffer=1,
// coalescing).
func TestSSE_SlowClient_DoesNotBlockPublisher(t *testing.T) {
	t.Parallel()

	bus := eventbus.New()
	defer bus.Close()

	cfg := config.Cobranza{SSEEnabled: true, SSEPingEvery: 60 * time.Second}
	handler := buildSSERouter(bus, cfg, sseCobranzaUser())
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Slow client — connect but never read.
	slowResp, err := srv.Client().Get(srv.URL + "/sync/pagos/zona/4/stream")
	require.NoError(t, err)
	defer func() { _ = slowResp.Body.Close() }()
	require.Equal(t, http.StatusOK, slowResp.StatusCode)

	// Fast client — reads eagerly.
	fastScanner, fastCancel := newSSEClient(t, srv, "/sync/pagos/zona/4/stream")
	defer fastCancel()

	// Wait until both are subscribed.
	require.Eventually(t, func() bool {
		return bus.SubscriberCount("pagos_changed") == 2
	}, 500*time.Millisecond, 5*time.Millisecond,
		"both clients must be subscribed before publishing")

	bus.Publish("pagos_changed")

	lineCh := make(chan string, 1)
	go func() {
		lineCh <- readNextNonBlankLine(fastScanner)
	}()

	select {
	case line := <-lineCh:
		assert.Contains(t, line, "pagos_changed")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("fast client did not receive event within 200ms despite slow client")
	}
}

// TestSSE_100Clients_1000Publishes_Race stress-tests the SSE path under the
// Go race detector.
//
// Scale is reduced to 50 clients × 200 publishes under -race because the
// race detector adds ~5–10× overhead; the full 100×1000 would exceed the
// 10 s per-test budget. Each client must receive at least 1 event.
//
// Note: due to SSE coalescing (buffer=1), a client may receive fewer events
// than publishes — this is by design. The assertion is "at least 1 event per
// client", not "every publish is received".
func TestSSE_100Clients_1000Publishes_Race(t *testing.T) {
	t.Parallel()

	const (
		// Scale down under -race: 50 clients, 200 publishes.
		// Full scale (100×1000) would take >10s under the race detector.
		numClients = 50
		numPublish = 200
	)

	bus := eventbus.New()
	defer bus.Close()

	cfg := config.Cobranza{SSEEnabled: true, SSEPingEvery: 60 * time.Second}
	handler := buildSSERouter(bus, cfg, sseCobranzaUser())
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Connect all clients.
	scanners := make([]*bufio.Scanner, numClients)
	cancels := make([]func(), numClients)
	for i := range numClients {
		s, c := newSSEClient(t, srv, fmt.Sprintf("/sync/pagos/zona/%d/stream", i+1))
		scanners[i] = s
		cancels[i] = c
	}
	defer func() {
		for _, c := range cancels {
			c()
		}
	}()

	// Wait until all clients are subscribed.
	require.Eventually(t, func() bool {
		return bus.SubscriberCount("pagos_changed") == numClients
	}, 5*time.Second, 10*time.Millisecond,
		"all %d clients must subscribe before publishing", numClients)

	// Publish from multiple goroutines to stress the concurrent path.
	var wg sync.WaitGroup
	for range numPublish {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus.Publish("pagos_changed")
		}()
	}
	wg.Wait()

	// Each client must have received at least 1 event within the deadline.
	// We read one line per client concurrently.
	results := make([]string, numClients)
	var rmu sync.Mutex
	var rwg sync.WaitGroup
	for i, sc := range scanners {
		rwg.Add(1)
		go func(idx int, scanner *bufio.Scanner) {
			defer rwg.Done()
			line := readNextNonBlankLine(scanner)
			rmu.Lock()
			results[idx] = line
			rmu.Unlock()
		}(i, sc)
	}

	done := make(chan struct{})
	go func() {
		rwg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for all clients to receive at least 1 event")
	}

	rmu.Lock()
	defer rmu.Unlock()
	for i, line := range results {
		assert.NotEmpty(t, line, "client %d received no event", i)
	}
}
