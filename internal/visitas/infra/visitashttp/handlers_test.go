//nolint:misspell // visitas vocabulary is Spanish per project convention.
package visitashttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/platform/httpdispatch"
	"github.com/abdimuy/msp-api/internal/visitas/app"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
	"github.com/abdimuy/msp-api/internal/visitas/infra/visitashttp"
)

// ─── Fixed test time ────────────────────────────────────────────────────────

var fixedNow = time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

// ─── fakeClock ──────────────────────────────────────────────────────────────

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time { return c.t }

// ─── fakeVisitasRepo ────────────────────────────────────────────────────────

// fakeVisitasRepo is an in-memory stand-in for outbound.VisitasRepo, local to
// the HTTP tests (the app package's own fake is unexported in package
// app_test and cannot be reused here).
type fakeVisitasRepo struct {
	byID map[uuid.UUID]*domain.Visita

	insertErr error
}

func newFakeVisitasRepo() *fakeVisitasRepo {
	return &fakeVisitasRepo{byID: make(map[uuid.UUID]*domain.Visita)}
}

func (r *fakeVisitasRepo) Insert(_ context.Context, v *domain.Visita) error {
	if r.insertErr != nil {
		return r.insertErr
	}
	if _, exists := r.byID[v.ID()]; exists {
		return domain.ErrVisitaYaExiste
	}
	r.byID[v.ID()] = v
	return nil
}

func (r *fakeVisitasRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.Visita, error) {
	v, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrVisitaNoEncontrada
	}
	return v, nil
}

// ─── CurrentUser fixtures ───────────────────────────────────────────────────

// visitaUser builds a CurrentUser holding auth.PermCobranzaVerPagos, the
// permission the visitas endpoint requires (cobradores already hold it).
func visitaUser() auth.CurrentUser {
	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-test-visita",
		Email:       "cobrador@muebleriamsp.mx",
		Nombre:      "Cobrador Test",
		Permisos:    []string{string(auth.PermCobranzaVerPagos)},
	}
}

// noPermUser builds a CurrentUser with no permissions at all.
func noPermUser() auth.CurrentUser {
	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-test-noperm",
		Email:       "vendedor@muebleriamsp.mx",
		Nombre:      "Sin Permiso",
		Permisos:    []string{},
	}
}

// ─── Router helpers ─────────────────────────────────────────────────────────

// planter plants the given CurrentUser onto the request context, mimicking
// the authn middleware.
func planter(cu auth.CurrentUser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.PlantCurrentUser(r.Context(), cu)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// mountWithUser wires a bare chi router at the module root ("/visitas") with
// a planted CurrentUser, mirroring how the composition root mounts the
// module via a bare Group (see cmd/api/server.go).
func mountWithUser(cu auth.CurrentUser, svc *app.Service) http.Handler {
	r := chi.NewRouter()
	r.Use(planter(cu))
	visitashttp.MountRouter(r, svc)
	return r
}

// visitaBodyJSON builds the JSON body for POST /visitas with realistic
// Mexican Spanish data.
func visitaBodyJSON(id, fecha string) string {
	return `{` +
		`"id":"` + id + `",` +
		`"cliente_id":11486,` +
		`"cobrador_id":42,` +
		`"cobrador":"Ramírez García, Jorge",` +
		`"forma_cobro_id":87327,` +
		`"lat":19.432608,` +
		`"lng":-99.133209,` +
		`"nota":"María salió, vuelve mañana",` +
		`"tipo_visita":"cobro",` +
		`"zona_cliente_id":7,` +
		`"fecha":"` + fecha + `"` +
		`}`
}

// ─── Tests ──────────────────────────────────────────────────────────────────

func TestHTTP_CrearVisita_HappyPath(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	visitaID := uuid.New()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)

	req := httptest.NewRequest(http.MethodPost, "/visitas", bytes.NewBufferString(visitaBodyJSON(visitaID.String(), fecha)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", visitaID.String())
	rec := httptest.NewRecorder()

	mountWithUser(visitaUser(), svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
	var dto visitashttp.VisitaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
	assert.Equal(t, visitaID.String(), dto.ID)
	assert.Equal(t, 11486, dto.ClienteID)
	assert.Equal(t, 42, dto.CobradorID)
	assert.Equal(t, "Ramírez García, Jorge", dto.Cobrador)
	assert.Equal(t, 87327, dto.FormaCobroID)
	assert.Equal(t, "cobro", dto.TipoVisita)
	assert.Equal(t, 7, dto.ZonaClienteID)
	assert.Nil(t, dto.ImpteDoctoCCID)
	assert.Len(t, repo.byID, 1)
}

func TestHTTP_CrearVisita_HappyPath_WithImpteDoctoCCID(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	visitaID := uuid.New()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)

	body := `{` +
		`"id":"` + visitaID.String() + `",` +
		`"cliente_id":11486,` +
		`"cobrador_id":42,` +
		`"cobrador":"Ramírez García, Jorge",` +
		`"forma_cobro_id":87327,` +
		`"lat":19.432608,` +
		`"lng":-99.133209,` +
		`"tipo_visita":"cobro",` +
		`"zona_cliente_id":7,` +
		`"impte_docto_cc_id":789012,` +
		`"fecha":"` + fecha + `"` +
		`}`

	req := httptest.NewRequest(http.MethodPost, "/visitas", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", visitaID.String())
	rec := httptest.NewRecorder()

	mountWithUser(visitaUser(), svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
	var dto visitashttp.VisitaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
	require.NotNil(t, dto.ImpteDoctoCCID)
	assert.Equal(t, 789012, *dto.ImpteDoctoCCID)
}

func TestHTTP_CrearVisita_NoAuth_Returns401(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	visitaID := uuid.New()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)

	// No planter middleware — CurrentUser never lands on the context.
	r := chi.NewRouter()
	visitashttp.MountRouter(r, svc)

	req := httptest.NewRequest(http.MethodPost, "/visitas", bytes.NewBufferString(visitaBodyJSON(visitaID.String(), fecha)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", visitaID.String())
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code, "body: %s", rec.Body.String())
	assert.Empty(t, repo.byID)
}

func TestHTTP_CrearVisita_MissingPermission_Returns403(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	visitaID := uuid.New()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)

	req := httptest.NewRequest(http.MethodPost, "/visitas", bytes.NewBufferString(visitaBodyJSON(visitaID.String(), fecha)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", visitaID.String())
	rec := httptest.NewRecorder()

	mountWithUser(noPermUser(), svc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
	assert.Empty(t, repo.byID)
}

func TestHTTP_CrearVisita_MalformedBodyID_Returns422(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)

	req := httptest.NewRequest(http.MethodPost, "/visitas", bytes.NewBufferString(visitaBodyJSON("not-a-uuid", fecha)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mountWithUser(visitaUser(), svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "visita_id_invalido")
	assert.Empty(t, repo.byID)
}

func TestHTTP_CrearVisita_MalformedFecha_Returns422(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	visitaID := uuid.New()

	req := httptest.NewRequest(http.MethodPost, "/visitas", bytes.NewBufferString(visitaBodyJSON(visitaID.String(), "not-a-date")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", visitaID.String())
	rec := httptest.NewRecorder()

	mountWithUser(visitaUser(), svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "visita_fecha_invalida")
	assert.Empty(t, repo.byID)
}

func TestHTTP_CrearVisita_IdempotencyKeyMismatch_NonInternal_Returns422(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	visitaID := uuid.New()
	other := uuid.New()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)

	req := httptest.NewRequest(http.MethodPost, "/visitas", bytes.NewBufferString(visitaBodyJSON(visitaID.String(), fecha)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", other.String())
	rec := httptest.NewRecorder()

	mountWithUser(visitaUser(), svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "idempotency_key_mismatch")
	assert.Empty(t, repo.byID)
}

// TestHTTP_CrearVisita_IdempotencyKeyMismatch_Internal_SkipsCheck verifies
// that an in-process replay dispatch (httpdispatch.IsInternal) bypasses the
// Idempotency-Key-vs-body.id cross-check entirely, mirroring cobranza's
// CrearPago (see internal/cobranza/infra/cobranzahttp/handlers_pago_recibido.go).
//
// httpdispatch.InternalContext must be applied to the REQUEST'S OWN context
// before it is fed to the router — never injected mid-chain by a chi
// middleware inside the same router that is already routing the request,
// which corrupts the in-flight chi.RouteContext and panics (see the
// package doc comment on httpdispatch.InternalContext, and how the real
// failed-intent replay handler uses it: it builds a brand-new *http.Request
// and sets replayCtx := httpdispatch.InternalContext(req.Context()) on it
// before calling dispatcher.Dispatch).
func TestHTTP_CrearVisita_IdempotencyKeyMismatch_Internal_SkipsCheck(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	visitaID := uuid.New()
	other := uuid.New()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)

	req := httptest.NewRequest(http.MethodPost, "/visitas", bytes.NewBufferString(visitaBodyJSON(visitaID.String(), fecha)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", other.String())
	req = req.WithContext(httpdispatch.InternalContext(auth.PlantCurrentUser(req.Context(), visitaUser())))
	rec := httptest.NewRecorder()

	r := chi.NewRouter()
	visitashttp.MountRouter(r, svc)
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
	assert.Len(t, repo.byID, 1)
}

// TestHTTP_CrearVisita_DomainValidationError_Returns422 verifies a domain
// sentinel (not a transport-layer error) also maps to 422.
func TestHTTP_CrearVisita_DomainValidationError_Returns422(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	visitaID := uuid.New()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)

	body := `{` +
		`"id":"` + visitaID.String() + `",` +
		`"cliente_id":0,` + // triggers domain.ErrVisitaClienteRequerido
		`"cobrador_id":42,` +
		`"cobrador":"Ramírez García, Jorge",` +
		`"forma_cobro_id":87327,` +
		`"lat":19.432608,` +
		`"lng":-99.133209,` +
		`"tipo_visita":"cobro",` +
		`"zona_cliente_id":7,` +
		`"fecha":"` + fecha + `"` +
		`}`

	req := httptest.NewRequest(http.MethodPost, "/visitas", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", visitaID.String())
	rec := httptest.NewRecorder()

	mountWithUser(visitaUser(), svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "visita_cliente_requerido")
	assert.Empty(t, repo.byID)
}

// TestHTTP_CrearVisita_IdempotentReplay_ReturnsStoredVisita verifies a
// retried request (same id, same Idempotency-Key) returns the already-stored
// visita instead of an error — app.Service.RegistrarVisita's idempotency
// contract surfaced end-to-end through the HTTP layer.
func TestHTTP_CrearVisita_IdempotentReplay_ReturnsStoredVisita(t *testing.T) {
	t.Parallel()

	repo := newFakeVisitasRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow})
	visitaID := uuid.New()
	fecha := fixedNow.Add(-time.Hour).Format(time.RFC3339)
	router := mountWithUser(visitaUser(), svc)

	body := visitaBodyJSON(visitaID.String(), fecha)

	req1 := httptest.NewRequest(http.MethodPost, "/visitas", bytes.NewBufferString(body))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Idempotency-Key", visitaID.String())
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusCreated, rec1.Code, "first: %s", rec1.Body.String())

	req2 := httptest.NewRequest(http.MethodPost, "/visitas", bytes.NewBufferString(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Idempotency-Key", visitaID.String())
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusCreated, rec2.Code, "second: %s", rec2.Body.String())

	var dto1, dto2 visitashttp.VisitaDTO
	require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &dto1))
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &dto2))
	assert.Equal(t, dto1.ID, dto2.ID)
	assert.Len(t, repo.byID, 1, "exactly one row stored")
}
