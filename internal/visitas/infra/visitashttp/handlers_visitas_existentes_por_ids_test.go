//nolint:misspell // visitas vocabulary is Spanish per project convention.
package visitashttp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/visitas/app"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
	"github.com/abdimuy/msp-api/internal/visitas/infra/visitashttp"
)

// byIDsRequest builds a GET /visitas/by-ids?ids=<raw> request. raw is used
// verbatim as the query value (callers pre-join with commas); it is escaped
// through url.Values so the request itself is always well-formed.
func byIDsRequest(raw string) *http.Request {
	v := url.Values{}
	v.Set("ids", raw)
	return httptest.NewRequest(http.MethodGet, "/visitas/by-ids?"+v.Encode(), nil)
}

// byIDsRequestNoParam builds a GET /visitas/by-ids request with no ids
// query parameter at all (distinct from ids= present-but-empty, though both
// must answer the same ids_required error).
func byIDsRequestNoParam() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/visitas/by-ids", nil)
}

// seedVisitaDirect registers a Visita straight into the fake repo (bypassing
// the write path — this suite only cares about existence, not row shape).
func seedVisitaDirect(t *testing.T, repo *fakeVisitasRepo, id uuid.UUID) {
	t.Helper()
	repo.byID[id] = domain.RehydrateVisita(domain.RehydrateVisitaParams{
		ID:         id,
		Cobrador:   "Ramírez García, Jorge",
		ClienteID:  11486,
		TipoVisita: "No se encontraba",
	})
}

func TestHTTP_VisitasExistentesPorIDs_HappyPath_MixOfKnownAndUnknown(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	known1, known2, unknown := uuid.New(), uuid.New(), uuid.New()
	seedVisitaDirect(t, repo, known1)
	seedVisitaDirect(t, repo, known2)

	raw := known1.String() + "," + unknown.String() + "," + known2.String()
	rec := httptest.NewRecorder()
	mountWithUser(visitaUser(), svc).ServeHTTP(rec, byIDsRequest(raw))

	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	var got []string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.ElementsMatch(t, []string{known1.String(), known2.String()}, got,
		"an unknown id must simply be absent from the response, not an error")
}

func TestHTTP_VisitasExistentesPorIDs_ResponseIsBareJSONArray(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	known := uuid.New()
	seedVisitaDirect(t, repo, known)

	rec := httptest.NewRecorder()
	mountWithUser(visitaUser(), svc).ServeHTTP(rec, byIDsRequest(known.String()))

	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	body := strings.TrimSpace(rec.Body.String())
	assert.True(t, strings.HasPrefix(body, "["), "response must be a bare JSON array, not an envelope: %s", body)
}

func TestHTTP_VisitasExistentesPorIDs_NoneKnown_ReturnsEmptyArray(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	rec := httptest.NewRecorder()
	mountWithUser(visitaUser(), svc).ServeHTTP(rec, byIDsRequest(uuid.New().String()+","+uuid.New().String()))

	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	var got []string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Empty(t, got)
}

func TestHTTP_VisitasExistentesPorIDs_DuplicateIDs_NoDuplicatesInResponse(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	known := uuid.New()
	seedVisitaDirect(t, repo, known)

	raw := known.String() + "," + known.String() + "," + known.String()
	rec := httptest.NewRecorder()
	mountWithUser(visitaUser(), svc).ServeHTTP(rec, byIDsRequest(raw))

	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	var got []string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, []string{known.String()}, got)
}

func TestHTTP_VisitasExistentesPorIDs_TrailingComma_SkipsEmptySegment(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	known := uuid.New()
	seedVisitaDirect(t, repo, known)

	rec := httptest.NewRecorder()
	mountWithUser(visitaUser(), svc).ServeHTTP(rec, byIDsRequest(known.String()+","))

	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	var got []string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, []string{known.String()}, got)
}

func TestHTTP_VisitasExistentesPorIDs_OnlyEmptySegments_ReturnsEmptyArray_NotError(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	rec := httptest.NewRecorder()
	mountWithUser(visitaUser(), svc).ServeHTTP(rec, byIDsRequest(",,"))

	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	var got []string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Empty(t, got)
}

func TestHTTP_VisitasExistentesPorIDs_MissingIDsParam_Returns422(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	rec := httptest.NewRecorder()
	mountWithUser(visitaUser(), svc).ServeHTTP(rec, byIDsRequestNoParam())

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body=%s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ids_required")
}

func TestHTTP_VisitasExistentesPorIDs_EmptyIDsParam_Returns422(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	rec := httptest.NewRecorder()
	mountWithUser(visitaUser(), svc).ServeHTTP(rec, byIDsRequest(""))

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body=%s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ids_required")
}

func TestHTTP_VisitasExistentesPorIDs_WhitespaceOnlyIDsParam_Returns422(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	rec := httptest.NewRecorder()
	mountWithUser(visitaUser(), svc).ServeHTTP(rec, byIDsRequest("   "))

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body=%s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ids_required")
}

func TestHTTP_VisitasExistentesPorIDs_MalformedUUIDAmongValid_Returns422_AndNothingLeaks(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	known := uuid.New()
	seedVisitaDirect(t, repo, known)

	raw := known.String() + ",no-soy-un-uuid"
	rec := httptest.NewRecorder()
	mountWithUser(visitaUser(), svc).ServeHTTP(rec, byIDsRequest(raw))

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body=%s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ids_invalid")
	assert.NotContains(t, rec.Body.String(), known.String(),
		"the whole request must be rejected, not silently narrowed to the valid subset")
}

// ─── The cap: boundary tested exactly at cap and cap+1 ─────────────────────

// nIDs returns n freshly minted UUID strings.
func nIDs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = uuid.New().String()
	}
	return out
}

func TestHTTP_VisitasExistentesPorIDs_AtCap_OK(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	raw := strings.Join(nIDs(100), ",") // maxIDsPorRequest == 100
	rec := httptest.NewRecorder()
	mountWithUser(visitaUser(), svc).ServeHTTP(rec, byIDsRequest(raw))

	assert.Equal(t, http.StatusOK, rec.Code, "exactly the cap must be accepted; body=%s", rec.Body.String())
}

func TestHTTP_VisitasExistentesPorIDs_OverCap_Returns422(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	raw := strings.Join(nIDs(101), ",") // cap + 1
	rec := httptest.NewRecorder()
	mountWithUser(visitaUser(), svc).ServeHTTP(rec, byIDsRequest(raw))

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "cap+1 must be rejected; body=%s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ids_too_many")
}

// TestHTTP_VisitasExistentesPorIDs_OverCap_RejectedWithoutParsingAnyUUID
// proves the cap is checked BEFORE any element is parsed: a list that is
// over the cap AND contains a malformed element must still answer
// ids_too_many, not ids_invalid — if the handler parsed elements before
// counting, the malformed one (deliberately placed first) would be hit
// first and the error code would betray the wrong order of operations.
func TestHTTP_VisitasExistentesPorIDs_OverCap_RejectedWithoutParsingAnyUUID(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	ids := append([]string{"esto-no-es-un-uuid"}, nIDs(100)...) // 101 elements, first one malformed
	raw := strings.Join(ids, ",")
	rec := httptest.NewRecorder()
	mountWithUser(visitaUser(), svc).ServeHTTP(rec, byIDsRequest(raw))

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body=%s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ids_too_many",
		"the cap must be enforced before any element is parsed, so this must be ids_too_many, not ids_invalid")
}

func TestHTTP_VisitasExistentesPorIDs_NoAuth_Returns401(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	r := chi.NewRouter() // no planter: unauthenticated
	visitashttp.MountRouter(r, svc)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, byIDsRequest(uuid.New().String()))

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "body=%s", rec.Body.String())
}

func TestHTTP_VisitasExistentesPorIDs_MissingPermission_Returns403(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})

	rec := httptest.NewRecorder()
	mountWithUser(noPermUser(), svc).ServeHTTP(rec, byIDsRequest(uuid.New().String()))

	assert.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
}
