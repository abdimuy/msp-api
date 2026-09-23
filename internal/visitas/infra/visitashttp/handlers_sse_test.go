// Tests for the visitas SSE stream — GET
// /v2/visitas/sync/visitas/zona/{zona_id}/stream.
//
// The stream is a separate route rather than a second event type on
// cobranza's pagos/saldos streams, and that decision is the thing most worth
// protecting here. It was made on a measurement, not a preference: the
// shipped Android client (CobranzaSseSubscriber.kt:324-333) reads the SSE
// event NAME into a log line and discards it, deciding what an event means
// from which socket delivered it. Multiplexing visitas onto the pagos stream
// would not have broken old phones — it would have made every one of them run
// a full cobranza cursor sync on every visit confirmed anywhere.
//
//nolint:misspell // visitas vocabulary is Spanish per project convention.
package visitashttp_test

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/eventbus"
	visitasapp "github.com/abdimuy/msp-api/internal/visitas/app"
	"github.com/abdimuy/msp-api/internal/visitas/infra/visitashttp"
)

// sseTestCfg returns a Visitas config with the stream on and a ping interval
// long enough that no keep-alive can race an assertion about event bytes.
func sseTestCfg() config.Visitas {
	return config.Visitas{SSEEnabled: true, SSEPingEvery: time.Hour}
}

// discardLogger keeps the handler's structured logs out of the test output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// sseServer starts an httptest.Server mounting the visitas router with SSE
// wired to bus, authenticating every request as cu.
//
// httptest.Server rather than httptest.NewRecorder: a recorder is not an
// http.Flusher-backed streaming writer, and the handler's whole behaviour is
// what it writes incrementally before returning.
func sseServer(t *testing.T, bus *eventbus.Bus, cfg config.Visitas, cu *auth.CurrentUser) *httptest.Server {
	t.Helper()
	r := chi.NewRouter()
	if cu != nil {
		r.Use(planter(*cu))
	}
	repo := newFakeVisitasRepo()
	svc := visitasapp.NewService(repo, &fakeClock{t: time.Now().UTC()})
	visitashttp.MountRouter(r, svc, visitashttp.WithSSE(bus, cfg, discardLogger()))
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// openStream issues the streaming GET and returns the live response plus the
// cancel that tears the connection down. Callers close the body themselves —
// deliberately, so bodyclose can see it and so a leaked stream in a future
// test is a lint failure rather than a slow flake.
func openStream(t *testing.T, srv *httptest.Server, zona string) (*http.Response, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/visitas/sync/visitas/zona/"+zona+"/stream", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := srv.Client().Do(req)
	if err != nil {
		cancel()
		t.Fatalf("stream request failed: %v", err)
	}
	return resp, cancel
}

// readEvent reads SSE lines until a blank line terminates one event, or the
// deadline expires. Returns the raw block.
func readEvent(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	var b strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("reading SSE event: %v (got so far: %q)", err, b.String())
		}
		if line == "\n" || line == "\r\n" {
			if b.Len() > 0 {
				return b.String()
			}
			continue // leading blank line, keep going
		}
		_, _ = b.WriteString(line)
	}
}

// TestSSEVisitas_EmiteElEventoAlConfirmarseUnaVisita is the core server
// obligation: a confirmed visita reaches a connected phone as an SSE event.
//
// It drives the REAL app service — not a bare bus.Publish — so the test fails
// if anyone removes the announcement from the registration path, which is the
// only thing that makes the stream worth serving.
func TestSSEVisitas_EmiteElEventoAlConfirmarseUnaVisita(t *testing.T) {
	t.Parallel()

	bus := eventbus.New()
	cu := visitaUser()
	srv := sseServer(t, bus, sseTestCfg(), &cu)

	resp, cancel := openStream(t, srv, "7")
	defer cancel()
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	reader := bufio.NewReader(resp.Body)

	// A real registration, through the real service, with the real bus.
	repo := newFakeVisitasRepo()
	svc := visitasapp.NewService(repo, &fakeClock{t: time.Now().UTC()},
		visitasapp.WithEventBus(bus))
	registrarVisitaDePrueba(t, svc)

	block := readEvent(t, reader)

	assert.Contains(t, block, "event: visitas_confirmadas",
		"the stream must name the event so a client can switch on it")
	assert.Contains(t, block, `"ts":`,
		"ts carries transit timing")
	assert.NotContains(t, block, `"ids"`,
		"the event is a wakeup and must carry NO visit ids: it has no authority, "+
			"the reconciler confirms through by-ids")
}

// TestSSEVisitas_EventoDesconocidoNoRompeElStream is the forward-compatibility
// guarantee in the direction the SERVER controls: a future event type written
// onto this stream must leave the connection usable.
//
// The client-side half of this — that an unknown event causes no ACTION, not
// merely no disconnect — lives in VisitasSseSubscriberTest on the app side.
// Both halves are needed: surviving an event you then act on wrongly is the
// exact defect measured in the cobranza client.
func TestSSEVisitas_EventoDesconocidoNoRompeElStream(t *testing.T) {
	t.Parallel()

	bus := eventbus.New()
	cu := visitaUser()
	srv := sseServer(t, bus, sseTestCfg(), &cu)

	resp, cancel := openStream(t, srv, "7")
	defer cancel()
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	reader := bufio.NewReader(resp.Body)

	// An unrelated topic must not reach this stream at all...
	bus.Publish("un_topico_del_futuro", []int{1, 2, 3})
	// ...and the stream must still deliver its own events afterwards.
	bus.Publish(visitashttp.TopicVisitasConfirmadas, []int{})

	block := readEvent(t, reader)
	assert.Contains(t, block, "event: visitas_confirmadas",
		"a publish on a foreign topic must neither be delivered here nor break the stream")
}

// TestSSEVisitas_FlagApagadoResponde503 pins the killswitch. 503 (not 404) is
// load-bearing: the client latches "feature off" on a 503 and stops
// reconnecting, whereas a 404 would look like a deploy skew and keep it
// retrying with backoff forever.
func TestSSEVisitas_FlagApagadoResponde503(t *testing.T) {
	t.Parallel()

	cu := visitaUser()
	cfg := sseTestCfg()
	cfg.SSEEnabled = false
	srv := sseServer(t, eventbus.New(), cfg, &cu)

	resp, cancel := openStream(t, srv, "7")
	defer cancel()
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "sse_deshabilitado")
}

// TestSSEVisitas_KillswitchEsIndependienteDelDeCobranza proves the two push
// channels can be switched separately.
//
// Not a hypothetical: an ops emergency on the cobranza stream (proxy
// buffering, a connection leak) must not silently disable the visits
// reconciler's fastest trigger, and a visitas incident must not take down the
// channel that moves money. One shared flag would fuse the two blast radii.
func TestSSEVisitas_KillswitchEsIndependienteDelDeCobranza(t *testing.T) {
	t.Parallel()

	var visitas config.Visitas
	var cobranza config.Cobranza
	assert.NotEqual(t,
		reflectEnvTag(t, &visitas, "SSEEnabled"),
		reflectEnvTag(t, &cobranza, "SSEEnabled"),
		"visitas and cobranza SSE must read DIFFERENT env vars")
	assert.Equal(t, "VISITAS_SSE_ENABLED", reflectEnvTag(t, &visitas, "SSEEnabled"))
	assert.Equal(t, "COBRANZA_SSE_ENABLED", reflectEnvTag(t, &cobranza, "SSEEnabled"))
}

// TestSSEVisitas_Authz cubre la preamble de autenticación y permisos, y el
// zona_id inválido — todos rechazados antes de abrir el stream.
func TestSSEVisitas_Authz(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		cu   *auth.CurrentUser
		zona string
		want int
		code string
	}{
		{name: "sin autenticar", cu: nil, zona: "7", want: http.StatusUnauthorized, code: "no_autenticado"},
		{name: "sin permiso", cu: userPtr(secVisitasNoPermUser()), zona: "7", want: http.StatusForbidden, code: "permiso_denegado"},
		{name: "zona no numerica", cu: userPtr(visitaUser()), zona: "abc", want: http.StatusBadRequest, code: "zona_id_invalida"},
		{name: "zona cero", cu: userPtr(visitaUser()), zona: "0", want: http.StatusBadRequest, code: "zona_id_invalida"},
		{name: "zona negativa", cu: userPtr(visitaUser()), zona: "-1", want: http.StatusBadRequest, code: "zona_id_invalida"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := sseServer(t, eventbus.New(), sseTestCfg(), tc.cu)
			resp, cancel := openStream(t, srv, tc.zona)
			defer cancel()
			defer func() { _ = resp.Body.Close() }()

			assert.Equal(t, tc.want, resp.StatusCode)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Contains(t, string(body), tc.code)
		})
	}
}

// TestSSEVisitas_DesconectarNoFiltraSuscriptores asserts the handler
// unsubscribes when the client goes away. A stream endpoint that leaks a
// subscriber per connection degrades silently until Publish is fanning out to
// thousands of dead channels.
func TestSSEVisitas_DesconectarNoFiltraSuscriptores(t *testing.T) {
	t.Parallel()

	bus := eventbus.New()
	cu := visitaUser()
	srv := sseServer(t, bus, sseTestCfg(), &cu)

	resp, cancel := openStream(t, srv, "7")
	defer cancel()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	require.Eventually(t, func() bool {
		return bus.SubscriberCount(visitashttp.TopicVisitasConfirmadas) == 1
	}, 2*time.Second, 10*time.Millisecond, "the stream must subscribe")

	// The client goes away: close the body and cancel the request context.
	_ = resp.Body.Close()
	cancel()

	assert.Eventually(t, func() bool {
		return bus.SubscriberCount(visitashttp.TopicVisitasConfirmadas) == 0
	}, 2*time.Second, 10*time.Millisecond, "the stream must unsubscribe on disconnect")
}

// userPtr is a tiny helper so the table above can hold optional users inline.
func userPtr(cu auth.CurrentUser) *auth.CurrentUser { return &cu }

// reflectEnvTag reads the `env` struct tag of one field, so the killswitch
// test asserts on the actual wire-up rather than on a string a future edit
// could leave behind.
func reflectEnvTag(t *testing.T, structPtr any, field string) string {
	t.Helper()
	typ := reflect.TypeOf(structPtr).Elem()
	f, ok := typ.FieldByName(field)
	require.True(t, ok, "field %s not found on %s", field, typ.Name())
	return f.Tag.Get("env")
}

// registrarVisitaDePrueba registers one well-formed visita through the real
// app service — the production path that must announce.
func registrarVisitaDePrueba(t *testing.T, svc *visitasapp.Service) {
	t.Helper()
	_, err := svc.RegistrarVisita(context.Background(), visitasapp.RegistrarVisitaInput{
		ID:            uuid.New(),
		Cobrador:      "Ramírez García, Jorge",
		CobradorID:    42,
		ClienteID:     11486,
		FormaCobroID:  87327,
		TipoVisita:    "No se encontraba",
		ZonaClienteID: 7,
		Fecha:         time.Now().UTC().Add(-time.Hour),
	}, uuid.New())
	require.NoError(t, err)
}
