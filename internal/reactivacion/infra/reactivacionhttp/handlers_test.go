//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package reactivacionhttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	reactivacionapp "github.com/abdimuy/msp-api/internal/reactivacion/app"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionhttp"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

var fixedNow = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return fixedNow }

// fakeReader returns a preset universe.
type fakeReader struct{ universo []outbound.ClienteUniverso }

func (r *fakeReader) LeerUniversoTehuacan(_ context.Context) ([]outbound.ClienteUniverso, error) {
	return r.universo, nil
}

// fakeRepo serves preset cohorte rows and captures upserts.
type fakeRepo struct {
	list     []*domain.CohorteCliente
	upserted int
}

func (r *fakeRepo) UpsertCohorte(_ context.Context, c []*domain.CohorteCliente) error {
	r.upserted += len(c)
	return nil
}

func (r *fakeRepo) ListarCohorte(_ context.Context, _ outbound.ListarCohorteParams) ([]*domain.CohorteCliente, error) {
	return r.list, nil
}

func (r *fakeRepo) ExistingControlFlags(_ context.Context) (map[int]bool, error) {
	return map[int]bool{}, nil
}

func (r *fakeRepo) ExistingContactadoFlags(_ context.Context) (map[int]bool, error) {
	return map[int]bool{}, nil
}

func buildService(reader outbound.UniversoReader, repo outbound.CohorteRepo) *reactivacionapp.Service {
	return reactivacionapp.NewService(reader, repo, fixedClock{}, nil, reactivacionapp.Config{})
}

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

func planter(cu auth.CurrentUser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.PlantCurrentUser(r.Context(), cu)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func buildRouter(svc *reactivacionapp.Service, cu auth.CurrentUser) http.Handler {
	r := chi.NewRouter()
	r.Use(planter(cu))
	reactivacionhttp.MountRouter(r, svc)
	return r
}

func buildRouterNoAuth(svc *reactivacionapp.Service) http.Handler {
	r := chi.NewRouter()
	reactivacionhttp.MountRouter(r, svc)
	return r
}

func do(h http.Handler, method, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func cohorteRow(clienteID int, seg domain.Segmento, enControl bool) *domain.CohorteCliente {
	return domain.HydrateCohorteCliente(domain.HydrateCohorteClienteParams{
		ID:                    uuid.New(),
		ClienteID:             clienteID,
		Nombre:                "Cliente Cohorte",
		Telefono:              "238 111 2222",
		Segmento:              seg,
		EnControl:             enControl,
		FueContactado:         false,
		CohorteFecha:          fixedNow,
		FechaUltimaCompraBase: fixedNow.AddDate(0, -2, 0),
		Saldo:                 decimal.Zero,
		PorLiquidarPct:        decimal.Zero,
		CreatedAt:             fixedNow,
		UpdatedAt:             fixedNow,
	})
}

// ─── GET /reactivacion/cohorte ──────────────────────────────────────────────────

func TestListCohorte_NoAuth_401(t *testing.T) {
	t.Parallel()
	svc := buildService(&fakeReader{}, &fakeRepo{})
	rec := do(buildRouterNoAuth(svc), http.MethodGet, "/reactivacion/cohorte")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestListCohorte_NoPerm_403(t *testing.T) {
	t.Parallel()
	svc := buildService(&fakeReader{}, &fakeRepo{})
	rec := do(buildRouter(svc, userWith()), http.MethodGet, "/reactivacion/cohorte")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestListCohorte_HappyPath_200(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{list: []*domain.CohorteCliente{
		cohorteRow(101, domain.SegmentoRecienLiquidado, false),
		cohorteRow(102, domain.SegmentoPorLiquidarHueco, true),
	}}
	svc := buildService(&fakeReader{}, repo)
	rec := do(buildRouter(svc, userWith(auth.PermReactivacionLeer)), http.MethodGet, "/reactivacion/cohorte")
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Items []reactivacionhttp.CohorteClienteDTO `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Items, 2)
	assert.Equal(t, 101, body.Items[0].ClienteID)
	assert.Equal(t, "recien_liquidado", body.Items[0].Segmento)
	assert.NotEmpty(t, body.Items[0].FechaUltimaCompraBase)
	assert.True(t, body.Items[1].EnControl)
}

// ─── GET /reactivacion/atribucion ───────────────────────────────────────────────

func TestAtribucion_NoPerm_403(t *testing.T) {
	t.Parallel()
	svc := buildService(&fakeReader{}, &fakeRepo{})
	rec := do(buildRouter(svc, userWith()), http.MethodGet, "/reactivacion/atribucion")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestAtribucion_HappyPath_200(t *testing.T) {
	t.Parallel()
	// One contacted converter, one control non-converter.
	contacted := domain.HydrateCohorteCliente(domain.HydrateCohorteClienteParams{
		ID: uuid.New(), ClienteID: 1, Segmento: domain.SegmentoRecienLiquidado,
		EnControl: false, FueContactado: true,
		CohorteFecha: fixedNow, FechaUltimaCompraBase: fixedNow.AddDate(0, 1, 0),
		Saldo: decimal.Zero, PorLiquidarPct: decimal.Zero, CreatedAt: fixedNow, UpdatedAt: fixedNow,
	})
	control := cohorteRow(2, domain.SegmentoRecienLiquidado, true)
	repo := &fakeRepo{list: []*domain.CohorteCliente{contacted, control}}
	svc := buildService(&fakeReader{}, repo)

	rec := do(buildRouter(svc, userWith(auth.PermReactivacionLeer)), http.MethodGet, "/reactivacion/atribucion")
	require.Equal(t, http.StatusOK, rec.Code)

	var dto reactivacionhttp.AtribucionDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
	assert.Equal(t, 1, dto.TreatmentTotal)
	assert.Equal(t, 1, dto.TreatmentConvertidos)
	assert.Equal(t, 1, dto.ControlTotal)
	assert.Equal(t, "1.0000", dto.TasaTreatment)
	assert.Equal(t, "0.0000", dto.TasaControl)
}

// ─── POST /reactivacion/cohorte/construir ────────────────────────────────────────

func TestConstruir_LeerPerm_403(t *testing.T) {
	t.Parallel()
	svc := buildService(&fakeReader{}, &fakeRepo{})
	// reactivacion:leer is NOT enough for the admin build.
	rec := do(buildRouter(svc, userWith(auth.PermReactivacionLeer)), http.MethodPost, "/reactivacion/cohorte/construir")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestConstruir_AdminPerm_202(t *testing.T) {
	t.Parallel()
	svc := buildService(&fakeReader{}, &fakeRepo{})
	rec := do(buildRouter(svc, userWith(auth.PermReactivacionAdministrar)), http.MethodPost, "/reactivacion/cohorte/construir")
	require.Equal(t, http.StatusAccepted, rec.Code)

	var body struct {
		Status  string `json:"status"`
		Mensaje string `json:"mensaje"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Contains(t, []string{"aceptado", "en_progreso"}, body.Status)
}
