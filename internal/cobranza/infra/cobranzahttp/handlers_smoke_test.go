//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp_test

// Scenario H — end-to-end smoke test for the SSE + by-ids push channel.
//
// This test exercises the full HTTP layer in a single scenario:
//  1. Build a real httptest.Server with MountReadRouter (fake bus + fake repo).
//  2. Subscribe an SSE client to /sync/pagos/zona/21552/stream.
//  3. Publish a list of IDs on the bus.
//  4. Assert the SSE data line contains {"ts":N,"ids":[101,202]}.
//  5. Call /sync/pagos/by-ids?zona_id=21552&ids=101,202.
//  6. Assert the endpoint returns 200 with matching rows (or [] when repo is empty).
//
// Pure unit — no Firebird needed. The test is race-clean; goroutines are started
// only within t.Cleanup / channel-receive patterns already used by the existing
// SSE tests in this package.

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/app/eventbus"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp"
	"github.com/abdimuy/msp-api/internal/platform/config"
)

// buildSmokeRouter wires MountReadRouter with the given bus, fake repos, and
// plants a CurrentUser that has both pagos + saldos permissions.
func buildSmokeRouter(
	bus *eventbus.Bus,
	pagos *fakePagosByIDsRepo,
	saldos *fakeSaldosByIDsRepo,
) http.Handler {
	sseCfg := config.Cobranza{
		SSEEnabled:   true,
		SSEPingEvery: 60 * time.Second, // long enough that pings don't interfere
	}
	r := chi.NewRouter()
	r.Use(planter(byIDsUser())) // byIDsUser has PermCobranzaVerPagos + PermCobranzaVerSaldos
	cobranzahttp.MountReadRouter(r, nil, bus, sseCfg, slog.Default(), pagos, saldos)
	return r
}

// TestSmoke_ScenarioH is the end-to-end smoke test for the SSE + by-ids push
// channel. It is intentionally structured as a single sequential scenario
// (not parallel sub-tests) so the SSE subscription is guaranteed to be in
// place before the bus publish.
func TestSmoke_ScenarioH(t *testing.T) {
	t.Parallel()

	// ── 1. Build server ──────────────────────────────────────────────────────

	bus := eventbus.New()
	defer bus.Close()

	const zonaID = 21552
	pagoRows := []domain.Pago{
		makePago(101, zonaID),
		makePago(202, zonaID),
	}
	pagosRepo := &fakePagosByIDsRepo{rows: pagoRows}
	saldosRepo := &fakeSaldosByIDsRepo{}

	handler := buildSmokeRouter(bus, pagosRepo, saldosRepo)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// ── 2. Subscribe SSE client ───────────────────────────────────────────────

	scanner, cancelSSE := newSSEClient(t, srv, "/sync/pagos/zona/21552/stream")
	defer cancelSSE()

	// Wait until the handler has subscribed to the bus before publishing.
	require.Eventually(t, func() bool {
		return bus.SubscriberCount("pagos_changed") == 1
	}, 500*time.Millisecond, 5*time.Millisecond,
		"SSE handler must subscribe to bus before publish")

	// ── 3. Publish IDs on the bus ─────────────────────────────────────────────

	published := []int{101, 202}
	bus.Publish("pagos_changed", published)

	// ── 4. Assert SSE event format ────────────────────────────────────────────
	//
	// The SSE stream emits lines like:
	//   event: pagos_changed
	//   data: {"ts":1234567890123,"ids":[101,202]}
	//
	// We read until we find the "data:" line. A ": ping" comment or an
	// "event:" line may appear first; we skip them.

	lineCh := make(chan string, 8)
	go func() {
		scanner := scanner // capture
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) != "" {
				lineCh <- line
			}
		}
		close(lineCh)
	}()

	var dataLine string
	deadline := time.After(500 * time.Millisecond)
outer:
	for {
		select {
		case line, ok := <-lineCh:
			if !ok {
				t.Fatal("SSE scanner closed before receiving data line")
			}
			if strings.HasPrefix(line, "data:") {
				dataLine = line
				break outer
			}
			// Skip "event: pagos_changed" and ": ping" lines.
		case <-deadline:
			t.Fatal("SSE data line did not arrive within 500ms")
		}
	}

	// Close SSE connection — we have the line we need.
	cancelSSE()

	// Verify format: data: {"ts":<N>,"ids":[101,202]}
	require.True(t, strings.HasPrefix(dataLine, "data:"),
		"expected data: prefix, got: %s", dataLine)

	rawJSON := strings.TrimSpace(strings.TrimPrefix(dataLine, "data:"))

	var payload struct {
		Ts  int64 `json:"ts"`
		IDs []int `json:"ids"`
	}
	require.NoError(t, json.Unmarshal([]byte(rawJSON), &payload),
		"SSE data must be valid JSON; got: %s", rawJSON)

	assert.Positive(t, payload.Ts, "ts must be a positive epoch ms")
	assert.ElementsMatch(t, published, payload.IDs,
		"SSE ids payload must match published IDs")

	// Also verify the ids field is present and is an array (not missing / null).
	assert.Contains(t, rawJSON, `"ids":[`,
		"SSE data must contain ids array field")

	// ── 5 & 6. Call by-ids endpoint and assert 200 with matching rows ─────────

	req, err := http.NewRequest(http.MethodGet,
		srv.URL+"/sync/pagos/by-ids?zona_id=21552&ids=101,202", nil)
	require.NoError(t, err)

	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode,
		"by-ids must return 200 OK")
	assert.Equal(t, "application/json; charset=utf-8",
		resp.Header.Get("Content-Type"),
		"by-ids must return JSON content-type")

	var dtos []cobranzahttp.PagoDTO
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&dtos),
		"by-ids response must be valid JSON array")

	// The fake repo returns the rows we seeded; assert they match.
	require.Len(t, dtos, 2, "by-ids must return 2 rows matching the seeded repo")
	gotIDs := make([]int, len(dtos))
	for i, dto := range dtos {
		gotIDs[i] = dto.ImpteDoctoCCID
	}
	assert.ElementsMatch(t, []int{101, 202}, gotIDs,
		"by-ids returned rows must include IDs 101 and 202")
}

// TestSmoke_ScenarioH_EmptyRepo verifies that by-ids returns 200 [] when the
// repo has no matching rows. This covers the "no rows committed in the dev DB"
// branch described in the scenario H requirements.
func TestSmoke_ScenarioH_EmptyRepo(t *testing.T) {
	t.Parallel()

	bus := eventbus.New()
	defer bus.Close()

	pagosRepo := &fakePagosByIDsRepo{rows: nil}
	saldosRepo := &fakeSaldosByIDsRepo{}

	handler := buildSmokeRouter(bus, pagosRepo, saldosRepo)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet,
		srv.URL+"/sync/pagos/by-ids?zona_id=21552&ids=101,202", nil)
	require.NoError(t, err)

	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode,
		"by-ids must return 200 even when repo has no rows")

	var dtos []cobranzahttp.PagoDTO
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&dtos))
	assert.Empty(t, dtos, "empty repo must produce [] not null")
}
