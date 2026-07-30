//nolint:misspell // visitas vocabulary is Spanish per project convention.
package visitashttp_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

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

	// The double-prefixed path must 404 (route does not exist there).
	badReq := httptest.NewRequest(http.MethodPost, "/v2/visitas/visitas", bytes.NewBufferString(visitaBodyJSON(visitaID.String(), fecha)))
	badReq.Header.Set("Content-Type", "application/json")
	badReq.Header.Set("Idempotency-Key", visitaID.String())
	badRec := httptest.NewRecorder()
	root.ServeHTTP(badRec, badReq)
	assert.Equal(t, http.StatusNotFound, badRec.Code, "double-prefixed path must not exist")

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
