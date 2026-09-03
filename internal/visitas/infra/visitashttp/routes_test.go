//nolint:misspell // visitas vocabulary is Spanish per project convention.
package visitashttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/eventbus"
	"github.com/abdimuy/msp-api/internal/visitas/app"
	"github.com/abdimuy/msp-api/internal/visitas/infra/visitashttp"
)

// TestMountRouter_FinalPathIsV2Visitas replicates the composition root's
// mount shape (cmd/api/server.go: r.Route("/v2", ...) wrapping a bare
// Group that calls visitashttp.MountRouter) and asserts the operation is
// reachable at exactly POST /v2/visitas — NOT /v2/visitas/visitas, which
// would result from combining r.Route("/visitas") with an operation also
// registered at "/visitas".
func TestMountRouter_FinalPathIsV2Visitas(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	root := chi.NewRouter()
	root.Route("/v2", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(planter(visitaUser()))
			visitashttp.MountRouter(r, svc)
		})
	})

	visitaID := uuid.New()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)

	// The double-prefixed path must not serve the create operation.
	//
	// This used to assert 404. It asserts 405 since GET /v2/visitas/{id}
	// exists: chi now matches "/v2/visitas/visitas" against that pattern
	// (binding id="visitas") and answers "Method Not Allowed" to a POST
	// rather than "Not Found". The status changed; the guarantee did not, and
	// the guarantee is what this test is for — so it also asserts, on the
	// repo itself, that nothing was written. That assertion survives any
	// future status-code drift, which a bare 404/405 check would not.
	badReq := httptest.NewRequest(http.MethodPost, "/v2/visitas/visitas", bytes.NewBufferString(visitaBodyJSON(visitaID.String(), fecha)))
	badReq.Header.Set("Content-Type", "application/json")
	badReq.Header.Set("Idempotency-Key", visitaID.String())
	badRec := httptest.NewRecorder()
	root.ServeHTTP(badRec, badReq)
	assert.Equal(t, http.StatusMethodNotAllowed, badRec.Code,
		"the create operation must not be reachable at the doubled prefix; body: %s", badRec.Body.String())
	assert.Empty(t, repo.byID, "no visita may be persisted from the doubled-prefix path")

	// The exact expected path must serve the operation.
	goodReq := httptest.NewRequest(http.MethodPost, "/v2/visitas", bytes.NewBufferString(visitaBodyJSON(visitaID.String(), fecha)))
	goodReq.Header.Set("Content-Type", "application/json")
	goodReq.Header.Set("Idempotency-Key", visitaID.String())
	goodRec := httptest.NewRecorder()
	root.ServeHTTP(goodRec, goodReq)
	assert.Equal(t, http.StatusCreated, goodRec.Code, "body: %s", goodRec.Body.String())
}

// TestMountRouter_UnauthenticatedAtV2Visitas verifies the mounted path
// exists and returns 401 (not 404) when the CurrentUser is absent — the
// controller's docker-boot smoke test asserts exactly this shape against a
// live server.
func TestMountRouter_UnauthenticatedAtV2Visitas(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	root := chi.NewRouter()
	root.Route("/v2", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			// No planter middleware — mirrors an unauthenticated request.
			visitashttp.MountRouter(r, svc)
		})
	})

	visitaID := uuid.New()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodPost, "/v2/visitas", bytes.NewBufferString(visitaBodyJSON(visitaID.String(), fecha)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", visitaID.String())
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "body: %s", rec.Body.String())
}

// TestMountRouter_ByIDsDoesNotShadowObtenerVisita pins down the routing
// question raised in registerOperations's doc comment: "by-ids" is a
// literal path segment sibling to the "{id}" parameter at
// GET /visitas/{id}. chi must resolve GET /visitas/by-ids to
// VisitasExistentesPorIDs — never to ObtenerVisita with id="by-ids", which
// would fail uuid.Parse and return a generic 422 that could be mistaken for
// this endpoint working by accident.
func TestMountRouter_ByIDsDoesNotShadowObtenerVisita(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	known := uuid.New()
	seedVisitaDirect(t, repo, known)

	root := chi.NewRouter()
	root.Route("/v2", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(planter(visitaUser()))
			visitashttp.MountRouter(r, svc)
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/v2/visitas/by-ids?ids="+known.String(), nil)
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code,
		"GET /visitas/by-ids must resolve to VisitasExistentesPorIDs, not to ObtenerVisita "+
			"with id=\"by-ids\" (which would answer visita_id_invalido); body: %s", rec.Body.String())

	var got []string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, []string{known.String()}, got)
	assert.NotContains(t, rec.Body.String(), "visita_id_invalido")
}

// TestMountRouter_SSERouteEstaMontada is the composition test for the new
// route. It replicates the composition root's mount shape and asserts the
// stream is reachable at exactly
// GET /v2/visitas/sync/visitas/zona/{zona_id}/stream.
//
// The "/stream" suffix is not cosmetic and is worth a composition test rather
// than a unit one: middleware.isStreamingPath exempts exactly that suffix from
// the request Timeout middleware, so a route renamed to anything else would be
// cancelled on schedule mid-stream — a failure that only appears in production
// after the timeout elapses.
func TestMountRouter_SSERouteEstaMontada(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	root := chi.NewRouter()
	root.Route("/v2", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(planter(visitaUser()))
			visitashttp.MountRouter(r, svc, visitashttp.WithSSE(
				eventbus.New(),
				config.Visitas{SSEEnabled: false, SSEPingEvery: time.Hour},
				slog.New(slog.NewTextHandler(io.Discard, nil)),
			))
		})
	})

	// The flag is off, so the handler answers 503 immediately and the test
	// never has to hold a live stream open. 503 rather than 404 is itself the
	// proof the route resolved to the SSE handler.
	req := httptest.NewRequest(http.MethodGet, "/v2/visitas/sync/visitas/zona/7/stream", nil)
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code,
		"the SSE route must be mounted at the v2 path; body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "sse_deshabilitado")
}

// TestMountRouter_SinWithSSENoMontaElStream — the option is what mounts it.
// Without a bus there is no stream, rather than a route that would panic on
// the first connection.
func TestMountRouter_SinWithSSENoMontaElStream(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	root := chi.NewRouter()
	root.Route("/v2", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(planter(visitaUser()))
			visitashttp.MountRouter(r, svc) // no WithSSE
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/v2/visitas/sync/visitas/zona/7/stream", nil)
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestTopicoDePublicacionCoincideConElDelStream pins the constant the app
// layer publishes on against the one the stream subscribes to.
//
// They are separate literals because depguard's app-no-infra rule forbids the
// app package from importing visitashttp. That duplication is the only place
// this feature can silently break in a way no other test would catch: the
// service would publish, the stream would subscribe, and nothing would ever be
// delivered — a push channel that is simply, quietly dead.
func TestTopicoDePublicacionCoincideConElDelStream(t *testing.T) {
	t.Parallel()

	bus := eventbus.New()
	ch, unsubscribe := bus.Subscribe(visitashttp.TopicVisitasConfirmadas)
	t.Cleanup(unsubscribe)

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow}, app.WithEventBus(bus))
	_, err := svc.RegistrarVisita(context.Background(), app.RegistrarVisitaInput{
		ID:            uuid.New(),
		Cobrador:      "Ramírez García, Jorge",
		CobradorID:    42,
		ClienteID:     11486,
		FormaCobroID:  87327,
		TipoVisita:    "No se encontraba",
		ZonaClienteID: 7,
		Fecha:         fixedNow.Add(-time.Hour),
	}, uuid.New())
	require.NoError(t, err)

	select {
	case <-ch:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("the app layer publishes on a topic the stream does not subscribe to — " +
			"the push channel would be silently dead in production")
	}
}
