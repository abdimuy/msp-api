//nolint:misspell // analytics vocabulary is Spanish per project convention.
package analyticshttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	analyticsapp "github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/infra/analyticshttp"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
	"github.com/abdimuy/msp-api/internal/auth"
)

// ─── fixed timestamps ─────────────────────────────────────────────────────────

var (
	fixedNow          = time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	fixedUltimaCompra = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC) // 529 days before fixedNow → lapsed
	fixedCohort       = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
)

// ─── fakes ────────────────────────────────────────────────────────────────────

// fixedClock always returns fixedNow.
type fixedClock struct{}

func (fixedClock) Now() time.Time { return fixedNow }

// controlledRepo lets each test configure the behavior of the repo methods.
type controlledRepo struct {
	mu sync.Mutex

	// listResult is returned by ListCandidatos.
	listResult outbound.Page[*domain.WinbackCandidato]
	listErr    error

	// capturedListParams records the last ListCandidatos call.
	capturedListParams outbound.ListWinbackParams

	// refreshStateErr controls GetRefreshState; if nil, returns ErrRefreshStateNotFound.
	refreshStateErr error

	// saveStateErr controls SaveRefreshState.
	saveStateErr error

	// controlFlags is returned by ExistingControlFlags.
	controlFlags    map[int]bool
	controlFlagsErr error

	// upsertErr controls UpsertCandidatos.
	upsertErr error
}

func newControlledRepo() *controlledRepo {
	return &controlledRepo{
		controlFlags:    map[int]bool{},
		refreshStateErr: domain.ErrRefreshStateNotFound,
	}
}

func (r *controlledRepo) UpsertCandidatos(_ context.Context, _ []*domain.WinbackCandidato) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.upsertErr
}

func (r *controlledRepo) ListCandidatos(_ context.Context, p outbound.ListWinbackParams) (outbound.Page[*domain.WinbackCandidato], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.capturedListParams = p
	return r.listResult, r.listErr
}

func (r *controlledRepo) GetRefreshState(_ context.Context, _ string) (outbound.RefreshState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.refreshStateErr != nil {
		return outbound.RefreshState{}, r.refreshStateErr
	}
	wm := fixedNow.Add(-24 * time.Hour)
	return outbound.RefreshState{Job: "winback_incr", LastWatermark: &wm}, nil
}

func (r *controlledRepo) SaveRefreshState(_ context.Context, _ outbound.RefreshState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saveStateErr
}

func (r *controlledRepo) ExistingControlFlags(_ context.Context) (map[int]bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.controlFlags, r.controlFlagsErr
}

// noopMicrosip returns an empty ancla list by default.
type noopMicrosip struct {
	anclas []outbound.AnclaCliente
	err    error
}

func (m *noopMicrosip) LeerAnclasDesde(_ context.Context, _ *time.Time) ([]outbound.AnclaCliente, error) {
	return m.anclas, m.err
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// buildService constructs a real *analyticsapp.Service with the given fake ports.
func buildService(repo outbound.WinbackRepo, micro outbound.MicrosipReader) *analyticsapp.Service {
	return analyticsapp.NewService(repo, micro, fixedClock{}, nil)
}

// userWith returns an auth.CurrentUser holding the given permissions.
func userWith(perms ...auth.Permission) auth.CurrentUser {
	codes := make([]string, len(perms))
	for i, p := range perms {
		codes[i] = string(p)
	}
	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-test-1",
		Email:       "tester@muebleriamsp.mx",
		Nombre:      "Analista Test",
		Permisos:    codes,
	}
}

// planter is a chi middleware that plants cu on the request context.
func planter(cu auth.CurrentUser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.PlantCurrentUser(r.Context(), cu)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// buildRouter mounts MountRouter behind a planter that authenticates as cu.
func buildRouter(svc *analyticsapp.Service, cu auth.CurrentUser) http.Handler {
	r := chi.NewRouter()
	r.Use(planter(cu))
	analyticshttp.MountRouter(r, svc)
	return r
}

// buildRouterNoAuth mounts MountRouter without planting any CurrentUser.
func buildRouterNoAuth(svc *analyticsapp.Service) http.Handler {
	r := chi.NewRouter()
	analyticshttp.MountRouter(r, svc)
	return r
}

// doJSON issues a request through h and returns the recorder.
func doJSON(h http.Handler, method, target string, body any) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			panic("doJSON: marshal: " + err.Error())
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// mustCandidato builds a WinbackCandidato with the given monetary so we can
// control which segmento/score it receives without a real DB.
func mustCandidato(clienteID int, monetary decimal.Decimal, frecuencia int, enControl bool) *domain.WinbackCandidato {
	c, err := domain.CrearWinbackCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:         clienteID,
		Nombre:            "Cliente Test",
		Zona:              "NORTE",
		Telefono:          "5551234567",
		FechaUltimaCompra: fixedUltimaCompra,
		Frecuencia:        frecuencia,
		Monetary:          monetary,
		Saldo:             decimal.NewFromInt(500),
		PorLiquidarPct:    decimal.NewFromInt(50),
		NextBestProduct:   "Sala Monaco",
		EnControl:         enControl,
		CohorteFecha:      fixedCohort,
		Now:               fixedNow,
	})
	if err != nil {
		panic("mustCandidato: " + err.Error())
	}
	return c
}

// ─── Scenario 1: GET /winback happy path ─────────────────────────────────────

// TestListarWinback_HappyPath_200 feeds a known candidato through the real service
// and asserts the response shape: status 200, dates as RFC3339 UTC strings,
// money as StringFixed(2), segmento/score/en_control populated.
func TestListarWinback_HappyPath_200(t *testing.T) {
	t.Parallel()

	monetary := decimal.NewFromInt(25000) // > umbralValioso → DORMIDO_VALIOSO if lapsed
	c := mustCandidato(42, monetary, 5, false)

	repo := newControlledRepo()
	repo.listResult = outbound.Page[*domain.WinbackCandidato]{Items: []*domain.WinbackCandidato{c}, Total: 1}

	svc := buildService(repo, &noopMicrosip{})
	cu := userWith(auth.PermAnalyticsWinbackRead)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/winback", nil)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp struct {
		Items []struct {
			ClienteID         int    `json:"cliente_id"`
			Nombre            string `json:"nombre"`
			Zona              string `json:"zona"`
			Telefono          string `json:"telefono"`
			FechaUltimaCompra string `json:"fecha_ultima_compra"`
			RecenciaDias      int    `json:"recencia_dias"`
			Frecuencia        int    `json:"frecuencia"`
			Monetary          string `json:"monetary"`
			Saldo             string `json:"saldo"`
			PorLiquidarPct    string `json:"por_liquidar_pct"`
			Segmento          string `json:"segmento"`
			Score             int    `json:"score"`
			EnControl         bool   `json:"en_control"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Items, 1)

	item := resp.Items[0]
	assert.Equal(t, 42, item.ClienteID)
	assert.Equal(t, "Cliente Test", item.Nombre)
	assert.Equal(t, "NORTE", item.Zona)
	assert.Equal(t, "5551234567", item.Telefono)
	assert.Equal(t, "25000.00", item.Monetary)
	assert.Equal(t, "500.00", item.Saldo)
	assert.Equal(t, "50.00", item.PorLiquidarPct)
	assert.Equal(t, 5, item.Frecuencia)
	assert.False(t, item.EnControl)

	// FechaUltimaCompra must be RFC3339 UTC.
	require.NotEmpty(t, item.FechaUltimaCompra)
	parsed, parseErr := time.Parse(time.RFC3339Nano, item.FechaUltimaCompra)
	require.NoError(t, parseErr, "FechaUltimaCompra must be RFC3339")
	assert.Equal(t, fixedUltimaCompra.UTC(), parsed.UTC())

	// Segmento must be one of the known values.
	assert.NotEmpty(t, item.Segmento)
	// Score must be in [0, 100].
	assert.GreaterOrEqual(t, item.Score, 0)
	assert.LessOrEqual(t, item.Score, 100)
	// RecenciaDias: fixedNow - fixedUltimaCompra = 529 days.
	assert.Positive(t, item.RecenciaDias)
}

// ─── Scenario 2: query param pass-through ────────────────────────────────────

// TestListarWinback_QueryParams_PassedToRepo verifies that zona and
// incluir_control are forwarded to the repo and that ExcluirControl is the
// inverse of incluir_control.
func TestListarWinback_QueryParams_PassedToRepo(t *testing.T) {
	t.Parallel()

	repo := newControlledRepo()
	// Return empty page — we just want to check the params forwarded.
	repo.listResult = outbound.Page[*domain.WinbackCandidato]{}

	svc := buildService(repo, &noopMicrosip{})
	cu := userWith(auth.PermAnalyticsWinbackRead)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/winback?zona=SUR&incluir_control=true", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	repo.mu.Lock()
	captured := repo.capturedListParams
	repo.mu.Unlock()

	assert.Equal(t, "SUR", captured.Zona)
	assert.False(t, captured.ExcluirControl, "incluir_control=true must set ExcluirControl=false")
}

// TestListarWinback_ExcluirControl_Default verifies that the default
// (incluir_control not set) causes ExcluirControl=true.
func TestListarWinback_ExcluirControl_Default(t *testing.T) {
	t.Parallel()

	repo := newControlledRepo()
	repo.listResult = outbound.Page[*domain.WinbackCandidato]{}

	svc := buildService(repo, &noopMicrosip{})
	cu := userWith(auth.PermAnalyticsWinbackRead)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/winback", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	repo.mu.Lock()
	captured := repo.capturedListParams
	repo.mu.Unlock()

	assert.True(t, captured.ExcluirControl, "default incluir_control=false must set ExcluirControl=true")
}

// ─── Scenario 3: invalid segmento → 422 ──────────────────────────────────────

// TestListarWinback_InvalidSegmento_422 sends a non-existent segmento value
// and asserts the response is 422 (KindValidation), not 500.
func TestListarWinback_InvalidSegmento_422(t *testing.T) {
	t.Parallel()

	repo := newControlledRepo()
	repo.listResult = outbound.Page[*domain.WinbackCandidato]{}

	svc := buildService(repo, &noopMicrosip{})
	cu := userWith(auth.PermAnalyticsWinbackRead)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/winback?segmento=NO_EXISTE", nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "invalid segmento must map to 422, got: %s", rec.Body.String())
	assert.Less(t, rec.Code, 500, "must not 5xx on invalid segmento")
}

// ─── Scenario 4: permission denied → 403 ─────────────────────────────────────

// TestListarWinback_NoPermission_403 verifies that a user without
// PermAnalyticsWinbackRead gets 403.
func TestListarWinback_NoPermission_403(t *testing.T) {
	t.Parallel()

	repo := newControlledRepo()
	svc := buildService(repo, &noopMicrosip{})
	// User holds an unrelated perm only.
	cu := userWith(auth.PermVentasListar)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/winback", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

// TestAtribucion_NoPermission_403 verifies that GET /winback/attribution
// also gates on PermAnalyticsWinbackRead.
func TestAtribucion_NoPermission_403(t *testing.T) {
	t.Parallel()

	repo := newControlledRepo()
	svc := buildService(repo, &noopMicrosip{})
	cu := userWith() // no perms
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/winback/attribution", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

// TestRefrescarCandidatos_NoPermission_403 verifies that POST /winback/refresh
// gates on PermAnalyticsRefresh.
func TestRefrescarCandidatos_NoPermission_403(t *testing.T) {
	t.Parallel()

	repo := newControlledRepo()
	svc := buildService(repo, &noopMicrosip{})
	cu := userWith(auth.PermAnalyticsWinbackRead) // read perm only, not refresh
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodPost, "/winback/refresh", map[string]bool{"full": false})
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

// ─── Scenario 5: GET /winback/attribution happy path ─────────────────────────

// TestAtribucion_HappyPath_200 seeds treatment+control candidates and asserts
// that rate fields use StringFixed(4) and counts are correct.
func TestAtribucion_HappyPath_200(t *testing.T) {
	t.Parallel()

	// 2 treatment candidates; 1 converted (has last purchase > cohort).
	// 1 control candidate; 0 converted.
	cohort := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
	afterCohort := cohort.Add(24 * time.Hour)
	beforeCohort := cohort.Add(-24 * time.Hour)

	makeC := func(clienteID int, enControl bool, lastBuy time.Time) *domain.WinbackCandidato {
		c, err := domain.CrearWinbackCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID:         clienteID,
			Nombre:            "Cliente",
			Zona:              "NORTE",
			FechaUltimaCompra: lastBuy,
			Frecuencia:        3,
			Monetary:          decimal.NewFromInt(10000),
			Saldo:             decimal.Zero,
			PorLiquidarPct:    decimal.Zero,
			EnControl:         enControl,
			CohorteFecha:      cohort,
			Now:               fixedNow,
		})
		require.NoError(t, err)
		return c
	}

	treatConv := makeC(1, false, afterCohort)        // treatment, converted
	treatNoConv := makeC(2, false, beforeCohort)     // treatment, not converted
	controlCandidate := makeC(3, true, beforeCohort) // control, not converted

	repo := newControlledRepo()
	repo.listResult = outbound.Page[*domain.WinbackCandidato]{
		Items: []*domain.WinbackCandidato{treatConv, treatNoConv, controlCandidate},
		Total: 3,
	}

	svc := buildService(repo, &noopMicrosip{})
	cu := userWith(auth.PermAnalyticsWinbackRead)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/winback/attribution", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		TreatmentTotal       int    `json:"treatment_total"`
		TreatmentConvertidos int    `json:"treatment_convertidos"`
		ControlTotal         int    `json:"control_total"`
		ControlConvertidos   int    `json:"control_convertidos"`
		TasaTreatment        string `json:"tasa_treatment"`
		TasaControl          string `json:"tasa_control"`
		Uplift               string `json:"uplift"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

	assert.Equal(t, 2, resp.TreatmentTotal)
	assert.Equal(t, 1, resp.TreatmentConvertidos)
	assert.Equal(t, 1, resp.ControlTotal)
	assert.Equal(t, 0, resp.ControlConvertidos)

	// tasa_treatment = 1/2 = 0.5000; tasa_control = 0/1 = 0.0000; uplift = 0.5000
	assert.Equal(t, "0.5000", resp.TasaTreatment, "rateScale must be 4 dp")
	assert.Equal(t, "0.0000", resp.TasaControl, "rateScale must be 4 dp")
	assert.Equal(t, "0.5000", resp.Uplift, "uplift must use 4 dp")
}

// ─── Scenario 6: POST /winback/refresh ───────────────────────────────────────

// refreshBody is the shape of RefreshOutput.Body used in handler tests.
type refreshBody struct {
	Estado  string `json:"estado"`
	Mensaje string `json:"mensaje"`
}

// TestRefrescarCandidatos_FullTrue_202_Iniciado verifies that full=true triggers
// a background refresh and the handler returns 202 with estado="iniciado".
func TestRefrescarCandidatos_FullTrue_202_Iniciado(t *testing.T) {
	t.Parallel()

	repo := newControlledRepo()
	micro := &noopMicrosip{anclas: []outbound.AnclaCliente{
		{
			ClienteID:         99,
			Nombre:            "Cliente Refreshed",
			Zona:              "NORTE",
			Telefono:          "5559876543",
			FechaUltimaCompra: fixedUltimaCompra,
			Frecuencia:        2,
			Monetary:          decimal.NewFromInt(5000),
			Saldo:             decimal.Zero,
			PorLiquidarPct:    decimal.Zero,
		},
	}}

	svc := buildService(repo, micro)
	cu := userWith(auth.PermAnalyticsRefresh)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodPost, "/winback/refresh", map[string]bool{"full": true})
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	var resp refreshBody
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

	assert.Equal(t, "iniciado", resp.Estado)
	assert.NotEmpty(t, resp.Mensaje)
}

// TestRefrescarCandidatos_FullFalse_202_Iniciado verifies that full=false
// (incremental) also returns 202 with estado="iniciado".
func TestRefrescarCandidatos_FullFalse_202_Iniciado(t *testing.T) {
	t.Parallel()

	repo := newControlledRepo()
	micro := &noopMicrosip{}

	svc := buildService(repo, micro)
	cu := userWith(auth.PermAnalyticsRefresh)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodPost, "/winback/refresh", map[string]bool{"full": false})
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	var resp refreshBody
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

	assert.Equal(t, "iniciado", resp.Estado)
	assert.NotEmpty(t, resp.Mensaje)
}

// TestRefrescarCandidatos_YaEnProgreso_202 simulates the single-flight guard
// returning false (a refresh is already running) and asserts the handler still
// returns 202 but with estado="ya_en_progreso".
//
// A blocking MicrosipReader holds the guard open between the two HTTP requests.
func TestRefrescarCandidatos_YaEnProgreso_202(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	bmImpl := &blockingMicrosipImpl{release: release}

	repo := newControlledRepo()
	svc := buildService(repo, bmImpl)
	cu := userWith(auth.PermAnalyticsRefresh)
	h := buildRouter(svc, cu)

	// First request: starts the background goroutine which blocks on microsip.
	rec1 := doJSON(h, http.MethodPost, "/winback/refresh", map[string]bool{"full": false})
	require.Equal(t, http.StatusAccepted, rec1.Code, rec1.Body.String())

	var resp1 refreshBody
	require.NoError(t, json.NewDecoder(rec1.Body).Decode(&resp1))
	assert.Equal(t, "iniciado", resp1.Estado, "first trigger must be iniciado")

	// The goroutine is now blocked inside LeerAnclasDesde; the guard is held.
	// Second request: guard is taken, must return ya_en_progreso.
	rec2 := doJSON(h, http.MethodPost, "/winback/refresh", map[string]bool{"full": false})
	require.Equal(t, http.StatusAccepted, rec2.Code, rec2.Body.String())

	var resp2 refreshBody
	require.NoError(t, json.NewDecoder(rec2.Body).Decode(&resp2))
	assert.Equal(t, "ya_en_progreso", resp2.Estado, "second trigger while first runs must be ya_en_progreso")

	// Unblock the goroutine so the test goroutine exits cleanly.
	close(release)
}

// blockingMicrosipImpl is a MicrosipReader that blocks LeerAnclasDesde until
// its release channel is closed. Used to hold the single-flight guard open.
type blockingMicrosipImpl struct {
	release chan struct{}
}

func (b *blockingMicrosipImpl) LeerAnclasDesde(_ context.Context, _ *time.Time) ([]outbound.AnclaCliente, error) {
	<-b.release
	return nil, nil
}

// ─── Scenario 7: internal error → 500 ────────────────────────────────────────

// TestListarWinback_RepoError_500 injects a non-apperror (generic) error from
// the repo and verifies mapAppError yields a 500.
func TestListarWinback_RepoError_500(t *testing.T) {
	t.Parallel()

	repo := newControlledRepo()
	repo.listErr = errors.New("connection refused")

	svc := buildService(repo, &noopMicrosip{})
	cu := userWith(auth.PermAnalyticsWinbackRead)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/winback", nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
}

// TestAtribucion_RepoError_500 similarly asserts 500 for attribution.
func TestAtribucion_RepoError_500(t *testing.T) {
	t.Parallel()

	repo := newControlledRepo()
	repo.listErr = errors.New("db timeout")

	svc := buildService(repo, &noopMicrosip{})
	cu := userWith(auth.PermAnalyticsWinbackRead)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/winback/attribution", nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
}

// TestRefrescarCandidatos_MicrosipError_StillReturns202 verifies that even when
// Microsip would return an error, the handler still returns 202 immediately
// because the refresh runs asynchronously. The error is logged by the background
// goroutine rather than surfaced as an HTTP 500.
func TestRefrescarCandidatos_MicrosipError_StillReturns202(t *testing.T) {
	t.Parallel()

	repo := newControlledRepo()
	micro := &noopMicrosip{err: errors.New("microsip unavailable")}

	svc := buildService(repo, micro)
	cu := userWith(auth.PermAnalyticsRefresh)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodPost, "/winback/refresh", map[string]bool{"full": true})
	assert.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	var resp refreshBody
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "iniciado", resp.Estado)
}

// ─── Unauthenticated (no CurrentUser) → 401 ──────────────────────────────────

// TestListarWinback_Unauthenticated_401 verifies that a request without a
// planted CurrentUser gets a 401.
func TestListarWinback_Unauthenticated_401(t *testing.T) {
	t.Parallel()

	repo := newControlledRepo()
	svc := buildService(repo, &noopMicrosip{})
	h := buildRouterNoAuth(svc)

	rec := doJSON(h, http.MethodGet, "/winback", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
}

// TestRefrescarCandidatos_Unauthenticated_401 verifies POST /winback/refresh
// also returns 401 when no user is planted.
func TestRefrescarCandidatos_Unauthenticated_401(t *testing.T) {
	t.Parallel()

	repo := newControlledRepo()
	svc := buildService(repo, &noopMicrosip{})
	h := buildRouterNoAuth(svc)

	rec := doJSON(h, http.MethodPost, "/winback/refresh", map[string]bool{"full": false})
	assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
}

// ─── Multi-item list: limit param ────────────────────────────────────────────

// TestListarWinback_LimitParam_Honored verifies that the limit query param
// is forwarded (checked via ExcluirControl / Zona logic in the real service,
// which handles limit AFTER scoring). With limit=1 and 2 candidates in the
// repo result, the response contains at most 1 item.
func TestListarWinback_LimitParam_Honored(t *testing.T) {
	t.Parallel()

	c1 := mustCandidato(1, decimal.NewFromInt(30000), 5, false)
	c2 := mustCandidato(2, decimal.NewFromInt(10000), 2, false)

	repo := newControlledRepo()
	repo.listResult = outbound.Page[*domain.WinbackCandidato]{Items: []*domain.WinbackCandidato{c1, c2}, Total: 2}

	svc := buildService(repo, &noopMicrosip{})
	cu := userWith(auth.PermAnalyticsWinbackRead)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/winback?limit=1", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Items []struct {
			ClienteID int `json:"cliente_id"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Len(t, resp.Items, 1, "limit=1 must truncate to 1 item")
}

// ─── Ensure context key is auth.PlantCurrentUser (not test-only) ─────────────

// TestCurrentUserContext_KeyMatchesAuth verifies that the context planting
// used in tests produces a user readable by the same key the handler uses
// (auth.CurrentUserFromContext). This acts as a canary for any future key change.
func TestCurrentUserContext_KeyMatchesAuth(t *testing.T) {
	t.Parallel()

	cu := userWith(auth.PermAnalyticsWinbackRead)
	ctx := auth.PlantCurrentUser(context.Background(), cu)
	got, ok := auth.CurrentUserFromContext(ctx)
	require.True(t, ok, "CurrentUserFromContext must find the planted user")
	assert.Equal(t, cu.ID, got.ID)
	assert.Equal(t, cu.Email, got.Email)
}
