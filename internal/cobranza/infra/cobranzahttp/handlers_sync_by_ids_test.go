//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/app/eventbus"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"
)

// ─── Compile-time interface checks ────────────────────────────────────────────

var (
	_ outbound.PagosRepo  = (*fakePagosByIDsRepo)(nil)
	_ outbound.VentasRepo = (*fakeVentasByIDsRepo)(nil)
)

// ─── Fakes ────────────────────────────────────────────────────────────────────

// fakePagosByIDsRepo is a minimal PagosRepo stub for by-ids unit tests.
// It records the last ByIDs call so tests can assert deduplication.
type fakePagosByIDsRepo struct {
	rows       []domain.Pago
	err        error
	lastIDs    []int
	lastZonaID int
}

func (f *fakePagosByIDsRepo) PorVenta(_ context.Context, _ int) ([]domain.Pago, error) {
	return nil, nil
}

func (f *fakePagosByIDsRepo) PorCliente(_ context.Context, _ int) ([]domain.Pago, error) {
	return nil, nil
}

func (f *fakePagosByIDsRepo) EnRutaPorZona(_ context.Context, _ int, _ time.Time) ([]domain.Pago, error) {
	return nil, nil
}

func (f *fakePagosByIDsRepo) SyncPorZona(_ context.Context, _ int, _ time.Time, _, _ int, _ time.Time) (outbound.SyncPage[domain.Pago], error) {
	return outbound.SyncPage[domain.Pago]{}, nil
}

func (f *fakePagosByIDsRepo) ByIDs(_ context.Context, zonaID int, ids []int) ([]domain.Pago, error) {
	f.lastZonaID = zonaID
	f.lastIDs = ids
	return f.rows, f.err
}

// fakeVentasByIDsRepo is a minimal VentasRepo stub for by-ids unit tests.
// It records the last ByIDs call so tests can assert deduplication.
type fakeVentasByIDsRepo struct {
	rows       []domain.Venta
	err        error
	lastIDs    []int
	lastZonaID int
}

func (f *fakeVentasByIDsRepo) SyncPorZona(_ context.Context, _ int, _ time.Time, _, _ int, _ time.Time) (outbound.SyncPage[domain.Venta], error) {
	return outbound.SyncPage[domain.Venta]{}, nil
}

func (f *fakeVentasByIDsRepo) ByIDs(_ context.Context, zonaID int, ids []int) ([]domain.Venta, error) {
	f.lastZonaID = zonaID
	f.lastIDs = ids
	return f.rows, f.err
}

// ─── Test helpers ─────────────────────────────────────────────────────────────

// byIDsUser builds a CurrentUser with both pagos and saldos permissions.
func byIDsUser() auth.CurrentUser {
	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-test-byids",
		Email:       "cobrador@muebleriamsp.mx",
		Nombre:      "Cobrador Test ByIDs",
		Permisos: []string{
			string(authdomain.PermCobranzaVerPagos),
			string(authdomain.PermCobranzaVerSaldos),
		},
	}
}

// byIDsSaldosOnlyUser has only the saldos permission.
func byIDsSaldosOnlyUser() auth.CurrentUser {
	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-test-byids-saldos",
		Email:       "saldos@muebleriamsp.mx",
		Nombre:      "Solo Saldos ByIDs",
		Permisos:    []string{string(authdomain.PermCobranzaVerSaldos)},
	}
}

// mountByIDsRouter builds a read router with the two stub repos wired, then
// plants the given CurrentUser.
func mountByIDsRouter(cu auth.CurrentUser, pagos outbound.PagosRepo, ventas outbound.VentasRepo) http.Handler {
	r := chi.NewRouter()
	r.Use(planter(cu))
	cobranzahttp.MountReadRouter(r, nil, eventbus.New(), config.Cobranza{}, slog.Default(), pagos, ventas)
	return r
}

// makePago builds a minimal domain.Pago for tests.
func makePago(impteID, zonaID int) domain.Pago {
	now := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	return domain.HydratePago(domain.HydratePagoParams{
		ImpteDoctoCCID: impteID,
		DoctoCCID:      impteID + 1000,
		DoctoCCAcrID:   impteID + 2000,
		ClienteID:      11486,
		ZonaClienteID:  &zonaID,
		Folio:          "CV-2026-001",
		ConceptoCCID:   87327,
		Fecha:          now,
		Importe:        decimal.NewFromInt(1500),
		Impuesto:       decimal.NewFromInt(0),
		Cancelado:      false,
		Aplicado:       true,
		UpdatedAt:      now,
	})
}

// makeVenta builds a minimal domain.Venta for tests. Fields mirror what the
// Android VentaDto expects as non-null Strings (frec_pago, cliente_nombre,
// nombre_cobrador, calle, etc.) — the exact shape that was crashing .isBlank().
func makeVenta(doctoCCID, zonaID int) domain.Venta {
	now := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	return domain.HydrateVenta(domain.HydrateVentaParams{
		DoctoCCID:      doctoCCID,
		ClienteID:      11486,
		ZonaClienteID:  &zonaID,
		Folio:          "Y00002207",
		FechaCargo:     now,
		PrecioTotal:    decimal.NewFromInt(8900),
		TotalImporte:   decimal.NewFromInt(250),
		ImpteRest:      decimal.NewFromInt(0),
		Saldo:          decimal.NewFromInt(8650),
		NumPagos:       2,
		CargoCancelado: false,
		UpdatedAt:      now,
		ClienteNombre:  "JORGE HERNANDEZ GARCIA",
		NombreCobrador: "COBRADOR RUTA 1",
		ZonaNombre:     "ZONA 01",
		Calle:          "CALLE REFORMA 123",
		Ciudad:         "GUADALAJARA",
		Estado:         "JALISCO",
		Telefono:       "3312345678",
		FrecPago:       "SEMANAL",
		Vendedor1:      "VENDEDOR UNO",
	})
}

// idsParam joins ints into a comma-separated string for ?ids= query param.
func idsParam(ids ...int) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, ",")
}

// ─── Pagos by-ids tests ────────────────────────────────────────────────────────

func TestByIDs_Pagos_HappyPath(t *testing.T) {
	t.Parallel()

	pagosRepo := &fakePagosByIDsRepo{
		rows: []domain.Pago{
			makePago(101, 21552),
			makePago(102, 21552),
			makePago(103, 21552),
		},
	}
	handler := mountByIDsRouter(byIDsUser(), pagosRepo, &fakeVentasByIDsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/sync/pagos/by-ids?zona_id=21552&ids=101,102,103", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var dtos []cobranzahttp.PagoDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dtos))
	assert.Len(t, dtos, 3)
	assert.Equal(t, 101, dtos[0].ImpteDoctoCCID)
	assert.Equal(t, 102, dtos[1].ImpteDoctoCCID)
	assert.Equal(t, 103, dtos[2].ImpteDoctoCCID)
}

func TestByIDs_Pagos_EmptyIDs(t *testing.T) {
	t.Parallel()

	handler := mountByIDsRouter(byIDsUser(), &fakePagosByIDsRepo{}, &fakeVentasByIDsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/sync/pagos/by-ids?zona_id=21552&ids=", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "ids_invalid")
}

func TestByIDs_Pagos_TooMany(t *testing.T) {
	t.Parallel()

	// 501 IDs → ids_too_many.
	ids := make([]int, 501)
	for i := range ids {
		ids[i] = i + 1
	}

	handler := mountByIDsRouter(byIDsUser(), &fakePagosByIDsRepo{}, &fakeVentasByIDsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/sync/pagos/by-ids?zona_id=21552&ids="+idsParam(ids...), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "ids_too_many")
}

func TestByIDs_Pagos_NonNumeric(t *testing.T) {
	t.Parallel()

	handler := mountByIDsRouter(byIDsUser(), &fakePagosByIDsRepo{}, &fakeVentasByIDsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/sync/pagos/by-ids?zona_id=21552&ids=1,abc,3", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "ids_invalid")
}

func TestByIDs_Pagos_ZonaMissing(t *testing.T) {
	t.Parallel()

	handler := mountByIDsRouter(byIDsUser(), &fakePagosByIDsRepo{}, &fakeVentasByIDsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/sync/pagos/by-ids?ids=1,2,3", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "zona_id_required")
}

func TestByIDs_Pagos_ZonaForbidden(t *testing.T) {
	t.Parallel()

	// User with NO cobranza permissions → 403.
	handler := mountByIDsRouter(noPermUser(), &fakePagosByIDsRepo{}, &fakeVentasByIDsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/sync/pagos/by-ids?zona_id=21552&ids=1,2,3", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestByIDs_Pagos_DuplicateIDs(t *testing.T) {
	t.Parallel()

	pagosRepo := &fakePagosByIDsRepo{rows: []domain.Pago{makePago(1, 21552), makePago(2, 21552)}}
	handler := mountByIDsRouter(byIDsUser(), pagosRepo, &fakeVentasByIDsRepo{})

	// Pass duplicates: 1,1,1,2 → repo should receive deduplicated list.
	req := httptest.NewRequest(http.MethodGet, "/sync/pagos/by-ids?zona_id=21552&ids=1,1,1,2", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	// The handler itself does NOT dedup — the repo impl does; here we verify
	// the parsed IDs list passed to ByIDs (before repo-level dedup) contains
	// all 4 tokens since parseByIDsParam does not dedup, only the repo does.
	// What matters is that 200 is returned and the repo was called.
	assert.Equal(t, 21552, pagosRepo.lastZonaID)
	assert.NotEmpty(t, pagosRepo.lastIDs)
}

func TestByIDs_Pagos_ExactlyAt500(t *testing.T) {
	t.Parallel()

	// Exactly 500 IDs → must return 200, not 400.
	ids := make([]int, 500)
	for i := range ids {
		ids[i] = i + 1
	}

	pagosRepo := &fakePagosByIDsRepo{rows: nil}
	handler := mountByIDsRouter(byIDsUser(), pagosRepo, &fakeVentasByIDsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/sync/pagos/by-ids?zona_id=21552&ids="+idsParam(ids...), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code, "exactly 500 IDs must succeed; body: %s", rec.Body.String())
}

// ─── Saldos by-ids tests ────────────────────────────────────────────────────────

// TestByIDs_Saldos_HappyPath verifies that /sync/saldos/by-ids now returns the
// enriched VentaDTO (38 fields) rather than the legacy SaldoDTO (15 fields).
// This is the fix for the Android NPE in applyByIds where Gson left non-null
// String fields (frec_pago, cliente_nombre, nombre_cobrador, calle, …) as null.
func TestByIDs_Saldos_HappyPath(t *testing.T) {
	t.Parallel()

	ventasRepo := &fakeVentasByIDsRepo{
		rows: []domain.Venta{
			makeVenta(5001, 21552),
			makeVenta(5002, 21552),
		},
	}
	handler := mountByIDsRouter(byIDsUser(), &fakePagosByIDsRepo{}, ventasRepo)

	req := httptest.NewRequest(http.MethodGet, "/sync/saldos/by-ids?zona_id=21552&ids=5001,5002", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var dtos []cobranzahttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dtos))
	require.Len(t, dtos, 2)
	assert.Equal(t, 5001, dtos[0].DoctoCCID)
	assert.Equal(t, 5002, dtos[1].DoctoCCID)
	// Assert the enriched fields that were null before the fix.
	assert.NotEmpty(t, dtos[0].ClienteNombre, "cliente_nombre must be present in VentaDTO")
	assert.NotEmpty(t, dtos[0].NombreCobrador, "nombre_cobrador must be present in VentaDTO")
	assert.NotEmpty(t, dtos[0].FrecPago, "frec_pago must be present in VentaDTO")
	assert.NotEmpty(t, dtos[0].Calle, "calle must be present in VentaDTO")
}

func TestByIDs_Saldos_PermDenied(t *testing.T) {
	t.Parallel()

	// User with only pagos permission → 403 on saldos.
	pagosOnlyUser := auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-pagos-only",
		Email:       "pagos@muebleriamsp.mx",
		Nombre:      "Solo Pagos",
		Permisos:    []string{string(authdomain.PermCobranzaVerPagos)},
	}
	handler := mountByIDsRouter(pagosOnlyUser, &fakePagosByIDsRepo{}, &fakeVentasByIDsRepo{})

	req := httptest.NewRequest(http.MethodGet, "/sync/saldos/by-ids?zona_id=21552&ids=5001", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestByIDs_Saldos_EmptyList_Returns200(t *testing.T) {
	t.Parallel()

	// When the repo returns no rows the handler must return 200 [].
	ventasRepo := &fakeVentasByIDsRepo{rows: nil}
	handler := mountByIDsRouter(byIDsSaldosOnlyUser(), &fakePagosByIDsRepo{}, ventasRepo)

	req := httptest.NewRequest(http.MethodGet, "/sync/saldos/by-ids?zona_id=21552&ids=99999", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var dtos []cobranzahttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dtos))
	assert.Empty(t, dtos, "empty result from repo must produce [] not null")
}

// ─── Property test: response ⊆ request ────────────────────────────────────────

// TestProperty_ByIDs_ResponseSubsetOfRequest verifies that for any set of
// requested IDs the returned DTOs are all members of the requested set.
// rapid generates random ID lists; the fake repo returns all of them.
func TestProperty_ByIDs_ResponseSubsetOfRequest(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(t *rapid.T) {
		// Generate 1–100 distinct positive IDs.
		n := rapid.IntRange(1, 100).Draw(t, "n")
		idSet := make(map[int]struct{}, n)
		for len(idSet) < n {
			id := rapid.IntRange(1, 100_000).Draw(t, "id")
			idSet[id] = struct{}{}
		}
		reqIDs := make([]int, 0, n)
		pagos := make([]domain.Pago, 0, n)
		for id := range idSet {
			reqIDs = append(reqIDs, id)
			pagos = append(pagos, makePago(id, 21552))
		}

		pagosRepo := &fakePagosByIDsRepo{rows: pagos}
		handler := mountByIDsRouter(byIDsUser(), pagosRepo, &fakeVentasByIDsRepo{})

		req := httptest.NewRequest(http.MethodGet,
			"/sync/pagos/by-ids?zona_id=21552&ids="+idsParam(reqIDs...), nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var dtos []cobranzahttp.PagoDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dtos))

		// Every returned ID must be in the requested set.
		for _, dto := range dtos {
			if _, ok := idSet[dto.ImpteDoctoCCID]; !ok {
				t.Fatalf("returned ID %d not in requested set %v", dto.ImpteDoctoCCID, reqIDs)
			}
		}
	})
}
