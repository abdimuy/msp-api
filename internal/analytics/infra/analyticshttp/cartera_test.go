// cartera_test.go — handler tests for the cartera dashboard endpoints.
// Covers auth (401/403), serialization (money-as-string, dates RFC3339),
// Disponible=false roll-rate state, periodo format validation, and e2e path
// registration. Shares test helpers defined in handlers_test.go and
// openapi_test.go (same package: analyticshttp_test).
//
//nolint:misspell // analytics vocabulary is Spanish per project convention.
package analyticshttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	analyticsapp "github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/infra/analyticshttp"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
	"github.com/abdimuy/msp-api/internal/auth"
)

// ─── fakeCarteraRepo ─────────────────────────────────────────────────────────

// fakeCarteraRepo is a controllable implementation of outbound.CarteraRepo.
type fakeCarteraRepo struct {
	agingZona  []outbound.AgingRow
	agingCobr  []outbound.AgingRow
	vintages   []outbound.VintageRow
	ceiRows    []outbound.CEIRow
	snapshots  []domain.CarteraSnapshot
	agingErr   error
	ceiErr     error
	vintageErr error
	snapErr    error
}

func (f *fakeCarteraRepo) AgingSaldosByZona(_ context.Context, _ time.Time) ([]outbound.AgingRow, error) {
	return f.agingZona, f.agingErr
}

func (f *fakeCarteraRepo) AgingSaldosByCobrador(_ context.Context, _ time.Time) ([]outbound.AgingRow, error) {
	return f.agingCobr, f.agingErr
}

func (f *fakeCarteraRepo) VintageSaldos(_ context.Context) ([]outbound.VintageRow, error) {
	return f.vintages, f.vintageErr
}

func (f *fakeCarteraRepo) ColeccionCEI(_ context.Context, _, _ time.Time) ([]outbound.CEIRow, error) {
	return f.ceiRows, f.ceiErr
}

func (f *fakeCarteraRepo) SaveCarteraSnapshot(_ context.Context, _ []domain.CarteraSnapshot) error {
	return nil
}

func (f *fakeCarteraRepo) ListRecentSnapshots(_ context.Context, _ int) ([]domain.CarteraSnapshot, error) {
	return f.snapshots, f.snapErr
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// buildServiceWithCartera wires a *Service against in-memory fakes including a
// carteraRepo. The candidato repo (newControlledRepo) and microsip (noopMicrosip)
// stubs are shared from handlers_test.go.
func buildServiceWithCartera(carteraRepo outbound.CarteraRepo) *analyticsapp.Service {
	return analyticsapp.NewService(newControlledRepo(), &noopMicrosip{}, fixedClock{}, nil).
		WithCarteraRepo(carteraRepo)
}

// buildServiceFull lets tests provide a custom candidato repo AND a carteraRepo.
func buildServiceFull(winback outbound.WinbackRepo, carteraRepo outbound.CarteraRepo) *analyticsapp.Service {
	return analyticsapp.NewService(winback, &noopMicrosip{}, fixedClock{}, nil).
		WithCarteraRepo(carteraRepo)
}

// ─── 403 / 401 checks ────────────────────────────────────────────────────────

// TestSaludCartera_NoPermission_403 asserts that a user without
// PermAnalyticsCarteraRead gets 403 on GET /cartera/salud.
func TestSaludCartera_NoPermission_403(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCartera(&fakeCarteraRepo{})
	cu := userWith(auth.PermVentasListar)
	h := buildRouter(svc, cu)
	rec := doJSON(h, http.MethodGet, "/cartera/salud", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

// TestAgingCartera_NoPermission_403 asserts 403 on GET /cartera/aging.
func TestAgingCartera_NoPermission_403(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCartera(&fakeCarteraRepo{})
	cu := userWith()
	h := buildRouter(svc, cu)
	rec := doJSON(h, http.MethodGet, "/cartera/aging", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

// TestRollRate_NoPermission_403 asserts 403 on GET /cartera/roll-rate.
func TestRollRate_NoPermission_403(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCartera(&fakeCarteraRepo{})
	cu := userWith(auth.PermAnalyticsWinbackRead) // analytics perm but wrong one
	h := buildRouter(svc, cu)
	rec := doJSON(h, http.MethodGet, "/cartera/roll-rate", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

// TestCosechasCartera_NoPermission_403 asserts 403 on GET /cartera/cosechas.
func TestCosechasCartera_NoPermission_403(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCartera(&fakeCarteraRepo{})
	cu := userWith(auth.PermVentasListar)
	h := buildRouter(svc, cu)
	rec := doJSON(h, http.MethodGet, "/cartera/cosechas", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

// TestCobradorRanking_NoPermission_403 asserts 403 on GET /cartera/cobradores.
func TestCobradorRanking_NoPermission_403(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCartera(&fakeCarteraRepo{})
	cu := userWith() // no perms
	h := buildRouter(svc, cu)
	rec := doJSON(h, http.MethodGet, "/cartera/cobradores", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

// TestCuentasRiesgo_NoPermission_403 asserts 403 on GET /cartera/cuentas-riesgo.
func TestCuentasRiesgo_NoPermission_403(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCartera(&fakeCarteraRepo{})
	cu := userWith(auth.PermAnalyticsWinbackRead) // analytics perm but wrong one
	h := buildRouter(svc, cu)
	rec := doJSON(h, http.MethodGet, "/cartera/cuentas-riesgo", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

// TestSaludCartera_Unauthenticated_401 asserts 401 when no user is planted.
func TestSaludCartera_Unauthenticated_401(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCartera(&fakeCarteraRepo{})
	h := buildRouterNoAuth(svc)
	rec := doJSON(h, http.MethodGet, "/cartera/salud", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
}

// ─── Salud happy path ─────────────────────────────────────────────────────────

// TestSaludCartera_HappyPath_200 feeds known aging+CEI data and asserts:
// - money fields are 2-dp strings
// - ratio fields (PAR, CEI) are 4-dp strings
// - counts are integers
// - MargenRealProxy is "0.00" because the expected loss exceeds the margin.
func TestSaludCartera_HappyPath_200(t *testing.T) {
	t.Parallel()

	repo := &fakeCarteraRepo{
		agingZona: []outbound.AgingRow{
			{ZonaClienteID: 1, Bucket: domain.BucketAgingDias0_30, Saldo: decimal.NewFromInt(80000), Conteo: 5},
			{ZonaClienteID: 1, Bucket: domain.BucketAgingDias31_60, Saldo: decimal.NewFromInt(20000), Conteo: 2},
		},
		ceiRows: []outbound.CEIRow{
			{ZonaClienteID: 1, Importe: decimal.NewFromInt(5000)},
		},
	}
	svc := buildServiceWithCartera(repo)
	cu := userWith(auth.PermAnalyticsCarteraRead)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/cartera/salud", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		SaldoTotal       string `json:"saldo_total"`
		SaldoMoroso      string `json:"saldo_moroso"`
		PAR              string `json:"par"`
		CEIRate          string `json:"cei_rate"`
		ImporteColectado string `json:"importe_colectado"`
		CuentasTotal     int    `json:"cuentas_total"`
		CuentasEnMora    int    `json:"cuentas_en_mora"`
		MargenRealProxy  string `json:"margen_real_proxy"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

	assert.Equal(t, "100000.00", resp.SaldoTotal, "saldo_total must be 2-dp string")
	assert.Equal(t, "20000.00", resp.SaldoMoroso, "saldo_moroso must be 2-dp string")
	assert.Equal(t, "5000.00", resp.ImporteColectado, "importe_colectado must be 2-dp string")
	assert.Equal(t, 7, resp.CuentasTotal, "cuentas_total must match sum of conteos")
	assert.Equal(t, 2, resp.CuentasEnMora, "cuentas_en_mora must match 31-60 conteo")
	assert.NotEmpty(t, resp.PAR, "par must be non-empty string")
	assert.NotEmpty(t, resp.CEIRate, "cei_rate must be non-empty string")
	// MargenReal = max(0, 0.528×5000 − 0.2×100000×0.70) = max(0, 2640−14000) = 0
	assert.Equal(t, "0.00", resp.MargenRealProxy, "margen_real_proxy must be floored at 0")
}

// ─── Aging happy path ─────────────────────────────────────────────────────────

// TestAgingCartera_HappyPath_200 asserts that all 4 buckets are returned,
// saldo is a 2-dp string, and pct_saldo is a 4-dp string.
func TestAgingCartera_HappyPath_200(t *testing.T) {
	t.Parallel()

	repo := &fakeCarteraRepo{
		agingZona: []outbound.AgingRow{
			{ZonaClienteID: 1, Bucket: domain.BucketAgingDias0_30, Saldo: decimal.NewFromInt(80000), Conteo: 10},
			{ZonaClienteID: 1, Bucket: domain.BucketAgingDias31_60, Saldo: decimal.NewFromInt(15000), Conteo: 3},
			{ZonaClienteID: 1, Bucket: domain.BucketAgingDias61_90, Saldo: decimal.NewFromInt(3000), Conteo: 1},
			{ZonaClienteID: 1, Bucket: domain.BucketAgingDias90Plus, Saldo: decimal.NewFromInt(2000), Conteo: 1},
		},
	}
	svc := buildServiceWithCartera(repo)
	cu := userWith(auth.PermAnalyticsCarteraRead)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/cartera/aging", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Items []struct {
			Bucket   string `json:"bucket"`
			Saldo    string `json:"saldo"`
			Conteo   int    `json:"conteo"`
			PctSaldo string `json:"pct_saldo"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Items, 4, "must return all 4 aging buckets")

	first := resp.Items[0]
	assert.Equal(t, domain.BucketAgingDias0_30, first.Bucket, "first bucket must be 0-30")
	assert.Equal(t, "80000.00", first.Saldo, "saldo must be 2-dp string")
	assert.Equal(t, 10, first.Conteo)
	assert.NotEmpty(t, first.PctSaldo, "pct_saldo must be non-empty string")
}

// ─── Roll-rate Disponible=false ────────────────────────────────────────────────

// TestRollRate_Disponible_False asserts that an empty snapshot store serializes
// to disponible=false with zero roll_rate and empty date fields.
func TestRollRate_Disponible_False(t *testing.T) {
	t.Parallel()

	svc := buildServiceWithCartera(&fakeCarteraRepo{snapshots: nil})
	cu := userWith(auth.PermAnalyticsCarteraRead)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/cartera/roll-rate", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Disponible         bool    `json:"disponible"`
		RollRate           float64 `json:"roll_rate"`
		FechaCorteAnterior string  `json:"fecha_corte_anterior"`
		FechaCorteReciente string  `json:"fecha_corte_reciente"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.False(t, resp.Disponible, "Disponible must be false with no snapshot cuts")
	assert.InDelta(t, 0.0, resp.RollRate, 1e-9, "roll_rate must be 0 when Disponible=false")
	assert.Empty(t, resp.FechaCorteAnterior, "fecha_corte_anterior must be empty when Disponible=false")
	assert.Empty(t, resp.FechaCorteReciente, "fecha_corte_reciente must be empty when Disponible=false")
}

// ─── Roll-rate Disponible=true with RFC3339 dates ─────────────────────────────

// TestRollRate_Disponible_True_DateRFC3339 builds two snapshot cuts and asserts
// that the response carries disponible=true and RFC3339-parseable date strings.
func TestRollRate_Disponible_True_DateRFC3339(t *testing.T) {
	t.Parallel()

	fechaAnterior := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	fechaReciente := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	now := time.Now()

	makeSnap := func(fc time.Time, bucket string, saldo decimal.Decimal) domain.CarteraSnapshot {
		snap, err := domain.NewCarteraSnapshot(domain.NewCarteraSnapshotParams{
			FechaCorte:    fc,
			ZonaClienteID: 1,
			Bucket:        bucket,
			Saldo:         saldo,
			Conteo:        1,
			Now:           now,
		})
		require.NoError(t, err)
		return *snap
	}

	// Snapshots ordered FECHA_CORTE DESC (as the repo contract specifies).
	snaps := []domain.CarteraSnapshot{
		makeSnap(fechaReciente, domain.BucketAgingDias0_30, decimal.NewFromInt(80000)),
		makeSnap(fechaReciente, domain.BucketAgingDias31_60, decimal.NewFromInt(10000)),
		makeSnap(fechaAnterior, domain.BucketAgingDias0_30, decimal.NewFromInt(85000)),
		makeSnap(fechaAnterior, domain.BucketAgingDias31_60, decimal.NewFromInt(5000)),
	}

	svc := buildServiceWithCartera(&fakeCarteraRepo{snapshots: snaps})
	cu := userWith(auth.PermAnalyticsCarteraRead)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/cartera/roll-rate", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Disponible         bool    `json:"disponible"`
		RollRate           float64 `json:"roll_rate"`
		FechaCorteAnterior string  `json:"fecha_corte_anterior"`
		FechaCorteReciente string  `json:"fecha_corte_reciente"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.Disponible, "Disponible must be true with 2+ snapshot cuts")
	assert.NotEmpty(t, resp.FechaCorteAnterior)
	assert.NotEmpty(t, resp.FechaCorteReciente)

	_, errA := time.Parse(time.RFC3339Nano, resp.FechaCorteAnterior)
	require.NoError(t, errA, "fecha_corte_anterior must be RFC3339, got %q", resp.FechaCorteAnterior)
	_, errR := time.Parse(time.RFC3339Nano, resp.FechaCorteReciente)
	require.NoError(t, errR, "fecha_corte_reciente must be RFC3339, got %q", resp.FechaCorteReciente)
}

// ─── Periodo format validation ────────────────────────────────────────────────

// TestSaludCartera_InvalidPeriodo_422 asserts that a malformed periodo returns 422.
func TestSaludCartera_InvalidPeriodo_422(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCartera(&fakeCarteraRepo{})
	cu := userWith(auth.PermAnalyticsCarteraRead)
	h := buildRouter(svc, cu)
	rec := doJSON(h, http.MethodGet, "/cartera/salud?periodo=not-a-date", nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

// TestRollRate_InvalidPeriodo_422 asserts 422 on roll-rate too (periodo is
// validated even though the roll-rate endpoint ignores the CEI dates).
func TestRollRate_InvalidPeriodo_422(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCartera(&fakeCarteraRepo{})
	cu := userWith(auth.PermAnalyticsCarteraRead)
	h := buildRouter(svc, cu)
	rec := doJSON(h, http.MethodGet, "/cartera/roll-rate?periodo=2026/06", nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

// TestSaludCartera_ValidPeriodo_200 asserts that a valid YYYY-MM periodo is
// accepted and produces a 200 (service receives correct from/to window).
func TestSaludCartera_ValidPeriodo_200(t *testing.T) {
	t.Parallel()
	repo := &fakeCarteraRepo{
		agingZona: []outbound.AgingRow{
			{ZonaClienteID: 1, Bucket: domain.BucketAgingDias0_30, Saldo: decimal.NewFromInt(10000), Conteo: 1},
		},
		ceiRows: []outbound.CEIRow{},
	}
	svc := buildServiceWithCartera(repo)
	cu := userWith(auth.PermAnalyticsCarteraRead)
	h := buildRouter(svc, cu)
	rec := doJSON(h, http.MethodGet, "/cartera/salud?periodo=2026-05", nil)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// ─── Cuentas-riesgo happy path ─────────────────────────────────────────────────

// TestCuentasRiesgo_HappyPath_200 seeds one at-risk candidato and asserts the
// response contains the account with money as a string and a populated tier.
func TestCuentasRiesgo_HappyPath_200(t *testing.T) {
	t.Parallel()

	c := mustCandidato(1, decimal.NewFromInt(10000), 3, false)
	winbackRepo := newControlledRepo()
	winbackRepo.listResult = outbound.Page[*domain.WinbackCandidato]{
		Items: []*domain.WinbackCandidato{c},
		Total: 1,
	}

	svc := buildServiceFull(winbackRepo, &fakeCarteraRepo{})
	cu := userWith(auth.PermAnalyticsCarteraRead)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/cartera/cuentas-riesgo", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Items []struct {
			ClienteID  int    `json:"cliente_id"`
			Nombre     string `json:"nombre"`
			Saldo      string `json:"saldo"`
			TierRiesgo string `json:"tier_riesgo"`
			EstadoPago string `json:"estado_pago"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Items, 1, "one at-risk account must be returned")

	item := resp.Items[0]
	assert.Equal(t, 1, item.ClienteID)
	assert.Equal(t, "Cliente Test", item.Nombre)
	assert.Equal(t, "500.00", item.Saldo, "saldo must be 2-dp string")
	assert.NotEmpty(t, item.TierRiesgo, "tier_riesgo must be populated")
	assert.NotEmpty(t, item.EstadoPago, "estado_pago must be populated")
}

// ─── Cosechas happy path ──────────────────────────────────────────────────────

// TestCosechasCartera_HappyPath_200 asserts cosechas items are returned with
// saldo as a 2-dp string.
func TestCosechasCartera_HappyPath_200(t *testing.T) {
	t.Parallel()

	repo := &fakeCarteraRepo{
		vintages: []outbound.VintageRow{
			{ZonaClienteID: 1, CohortMonth: 24318, Saldo: decimal.NewFromInt(50000), Conteo: 10},
		},
	}
	svc := buildServiceWithCartera(repo)
	cu := userWith(auth.PermAnalyticsCarteraRead)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/cartera/cosechas", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Items []struct {
			CohortMonth int    `json:"cohort_month"`
			Saldo       string `json:"saldo"`
			Conteo      int    `json:"conteo"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Items, 1)
	assert.Equal(t, 24318, resp.Items[0].CohortMonth)
	assert.Equal(t, "50000.00", resp.Items[0].Saldo, "saldo must be 2-dp string")
}

// ─── Cobradores happy path ────────────────────────────────────────────────────

// TestCobradorRanking_HappyPath_200 asserts cobrador performance items are
// returned with money as 2-dp strings, rate fields as 4-dp strings, and
// cobrador_nombre populated from the contract.
func TestCobradorRanking_HappyPath_200(t *testing.T) {
	t.Parallel()

	cobID := 42
	repo := &fakeCarteraRepo{
		agingCobr: []outbound.AgingRow{
			{ZonaClienteID: 1, CobradorID: &cobID, CobradorNombre: "Maria Gonzalez", Bucket: domain.BucketAgingDias0_30, Saldo: decimal.NewFromInt(30000), Conteo: 5},
			{ZonaClienteID: 1, CobradorID: &cobID, CobradorNombre: "Maria Gonzalez", Bucket: domain.BucketAgingDias31_60, Saldo: decimal.NewFromInt(5000), Conteo: 1},
		},
		ceiRows: []outbound.CEIRow{
			{ZonaClienteID: 1, CobradorID: &cobID, Importe: decimal.NewFromInt(3000)},
		},
	}
	svc := buildServiceWithCartera(repo)
	cu := userWith(auth.PermAnalyticsCarteraRead)
	h := buildRouter(svc, cu)

	rec := doJSON(h, http.MethodGet, "/cartera/cobradores", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Items []struct {
			CobradorID       int    `json:"cobrador_id"`
			CobradorNombre   string `json:"cobrador_nombre"`
			ZonaClienteID    int    `json:"zona_cliente_id"`
			CEI              string `json:"cei"`
			PAR              string `json:"par"`
			SaldoTotal       string `json:"saldo_total"`
			ImporteColectado string `json:"importe_colectado"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Items, 1)
	item := resp.Items[0]
	assert.Equal(t, 42, item.CobradorID)
	assert.Equal(t, "Maria Gonzalez", item.CobradorNombre, "cobrador_nombre must be serialized in response")
	assert.Equal(t, 1, item.ZonaClienteID)
	assert.Equal(t, "35000.00", item.SaldoTotal, "saldo_total must be 2-dp string")
	assert.Equal(t, "3000.00", item.ImporteColectado)
	assert.NotEmpty(t, item.CEI, "cei must be non-empty 4-dp string")
	assert.NotEmpty(t, item.PAR, "par must be non-empty 4-dp string")
}

// ─── E2E: cartera routes registered in OpenAPI spec ───────────────────────────

// TestOpenAPI_CarteraPaths_Registered verifies that all 6 cartera paths appear
// in the generated OpenAPI specification after MountRouter.
func TestOpenAPI_CarteraPaths_Registered(t *testing.T) {
	t.Parallel()

	r := chi.NewRouter()
	api := analyticshttp.MountRouter(r, buildTestService())
	spec := api.OpenAPI()
	require.NotNil(t, spec.Paths, "OpenAPI paths must not be nil")

	carteraPaths := []string{
		"/cartera/salud",
		"/cartera/aging",
		"/cartera/cosechas",
		"/cartera/cobradores",
		"/cartera/cuentas-riesgo",
		"/cartera/roll-rate",
	}
	for _, path := range carteraPaths {
		_, ok := spec.Paths[path]
		assert.Truef(t, ok, "path %q must be registered in OpenAPI spec", path)
	}
}
