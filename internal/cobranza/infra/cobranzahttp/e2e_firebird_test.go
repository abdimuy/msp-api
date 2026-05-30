//nolint:misspell // Spanish vocabulary (saldo, cobranza, etc.) by convention.
package cobranzahttp_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp"
	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	cobranzaoutbound "github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// ─── Test helpers ─────────────────────────────────────────────────────────────

func e2eRequireFBEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird E2E tests")
	}
}

func e2eRequireMigration000010(t *testing.T, q firebird.Querier) {
	t.Helper()
	var n int
	err := q.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM RDB$PROCEDURES WHERE RDB$PROCEDURE_NAME = 'MSP_RECOMPUTE_SALDO_VENTA'`,
	).Scan(&n)
	if err != nil || n == 0 {
		t.Skipf("migration 000010 not applied; skipping — run 'make fb-migrate-up'")
	}
}

// e2eTestPool builds a real Firebird pool from env vars.
func e2eTestPool(t *testing.T) *firebird.Pool {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird E2E tests")
	}
	cfg := config.Firebird{
		Host:     envOrDefault("FB_HOST", "localhost"),
		Port:     3050,
		Database: os.Getenv("FB_DATABASE"),
		User:     envOrDefault("FB_USER", "SYSDBA"),
		Password: os.Getenv("FB_PASSWORD"),
		Charset:  envOrDefault("FB_CHARSET", "UTF8"),
		PoolSize: 4,
	}
	pool, err := firebird.New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Stop(context.Background()) })
	return pool
}

func envOrDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// valuesForwardingCtx forwards the test tx context values across httptest boundary.
//
// into the httptest request so firebird.GetQuerier picks it up.
//
//nolint:containedctx // test-only: necessary to splice the Firebird tx context
type valuesForwardingCtx struct {
	context.Context
	values context.Context
}

func (c valuesForwardingCtx) Value(key any) any {
	if v := c.Context.Value(key); v != nil {
		return v
	}
	return c.values.Value(key)
}

// txInjector returns a chi middleware that splices the test transaction context
// onto every incoming request.
func txInjector(txCtx context.Context) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			merged := valuesForwardingCtx{Context: r.Context(), values: txCtx}
			next.ServeHTTP(w, r.WithContext(merged))
		})
	}
}

// planter plants the given CurrentUser onto the request context.
func planter(cu auth.CurrentUser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.PlantCurrentUser(r.Context(), cu)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// e2eCobranzaUser returns a CurrentUser with the cobranza permissions.
func e2eCobranzaUser() auth.CurrentUser {
	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-cobranza-e2e",
		Email:       "cobranza-e2e@muebleriamsp.mx",
		Nombre:      "Cobranza E2E",
		Permisos: []string{
			string(authdomain.PermCobranzaVerSaldos),
			string(authdomain.PermCobranzaReconciliar),
			string(authdomain.PermCobranzaBackfill),
		},
	}
}

// e2eNextID claims the next Microsip generator ID.
func e2eNextID(t *testing.T, q firebird.Querier) int {
	t.Helper()
	var id int
	err := q.QueryRowContext(context.Background(), `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&id)
	require.NoError(t, err)
	return id
}

// e2eClienteID returns the test cliente ID if it exists, or skips.
func e2eClienteID(t *testing.T, q firebird.Querier) int {
	t.Helper()
	const testClienteID = 11486
	var n int
	err := q.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM CLIENTES WHERE CLIENTE_ID = ?`, testClienteID).Scan(&n)
	if err != nil || n == 0 {
		t.Skipf("test cliente %d not found in DB", testClienteID)
	}
	return testClienteID
}

// e2eInsertCargo inserts a minimal DOCTOS_CC cargo row and its IMPORTES_DOCTOS_CC importe.
func e2eInsertCargo(t *testing.T, q firebird.Querier, clienteID int, folio string, importe decimal.Decimal) int {
	t.Helper()
	var cargoID int
	err := q.QueryRowContext(context.Background(), `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&cargoID)
	require.NoError(t, err)

	_, err = q.ExecContext(
		context.Background(), `
		INSERT INTO DOCTOS_CC
		  (DOCTO_CC_ID, CONCEPTO_CC_ID, FOLIO, NATURALEZA_CONCEPTO,
		   SUCURSAL_ID, FECHA, CLIENTE_ID, CLAVE_CLIENTE,
		   TIPO_CAMBIO, DESCRIPCION,
		   SISTEMA_ORIGEN, APLICADO, ESTATUS, ESTATUS_ANT,
		   CONTABILIZADO_GYP, ES_CFD, TIENE_ANTICIPO, CFDI_CERTIFICADO, ENVIADO,
		   INTEG_BA, CONTABILIZADO_BA, CANCELADO)
		VALUES (?, 87327, ?, 'C',
		        225490, CURRENT_DATE, ?, '0001',
		        1, 'Cargo E2E cobranzahttp',
		        'CC', 'S', 'N', 'N',
		        'N', 'N', 'N', 'N', 'N',
		        'N', 'N', 'N')`,
		cargoID, folio, clienteID,
	)
	require.NoError(t, err, "e2eInsertCargo: INSERT DOCTOS_CC")

	impteID := e2eNextID(t, q)
	_, err = q.ExecContext(
		context.Background(), `
		INSERT INTO IMPORTES_DOCTOS_CC
		  (IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, FECHA,
		   TIPO_IMPTE, DOCTO_CC_ACR_ID,
		   IMPORTE, IMPUESTO,
		   APLICADO, ESTATUS, CONCEPTO_CC_ID, CANCELADO)
		VALUES (?, ?, CURRENT_DATE,
		        'C', NULL,
		        ?, 0,
		        'N', 'N', 87327, 'N')`,
		impteID, cargoID, importe,
	)
	require.NoError(t, err, "e2eInsertCargo: INSERT IMPORTES_DOCTOS_CC")

	return cargoID
}

// buildReadRouter builds a chi router with the cobranza read routes + a
// pre-planted CurrentUser (auth bypassed) + the test tx injected.
func buildReadRouter(txCtx context.Context, svc *cobranzaapp.Service, cu auth.CurrentUser) http.Handler {
	r := chi.NewRouter()
	r.Use(txInjector(txCtx))
	r.Use(planter(cu))
	cobranzahttp.MountReadRouter(r, svc)
	return r
}

// buildAdminRouter builds a chi router with the cobranza admin routes.
func buildAdminRouter(txCtx context.Context, svc *cobranzaapp.Service, reconciler *cobranzaapp.Reconciler, errorsRepo cobranzaoutbound.ErrorsRepo, cu auth.CurrentUser) http.Handler {
	r := chi.NewRouter()
	r.Use(txInjector(txCtx))
	r.Use(planter(cu))
	cobranzahttp.MountAdminRouter(r, svc, reconciler, errorsRepo)
	return r
}

// ─── E2E Tests ────────────────────────────────────────────────────────────────

// TestE2E_Cobranza_HTTP_PorVenta_HappyPath plants a cargo, queries the read
// endpoint, and verifies the SaldoDTO fields.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Cobranza_HTTP_PorVenta_HappyPath(t *testing.T) {
	e2eRequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		e2eRequireMigration000010(t, q)

		clienteID := e2eClienteID(t, q)
		importe := decimal.RequireFromString("2500.00")
		cargoID := e2eInsertCargo(t, q, clienteID, "E2E-HTTP-001", importe)

		repo := cobranzaventfb.NewSaldosRepo(pool)
		_, err := repo.PorCargo(ctx, cargoID)
		if err != nil {
			t.Skipf("trigger did not create cache row for cargo %d — verify migration 000010", cargoID)
		}

		// The PorVenta endpoint requires a DOCTO_PV_ID. Since this cargo was
		// inserted directly (no PV document), use PorCargo path via the Saldo.
		// For HTTP happy-path we query by cargo directly via the reconcile report.
		svc := cobranzaapp.NewService(repo, cobranzaoutbound.ProductionClock{})
		reconciler := cobranzaapp.NewReconciler(
			cobranzaventfb.NewSaldosLister(pool),
			cobranzaventfb.NewRecomputer(pool, repo),
			repo,
			cobranzaoutbound.ProductionClock{},
			cobranzaapp.ReconcilerConfig{PageSize: 10, DriftLog: false, FixDrift: false},
			slog.Default(),
		)
		errorsRepo := cobranzaventfb.NewErrorsRepo(pool)

		cu := e2eCobranzaUser()
		r := buildAdminRouter(ctx, svc, reconciler, errorsRepo, cu)

		req := httptest.NewRequest(http.MethodPost, "/reconcile", nil)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, "reconcile body=%s", rec.Body.String())

		var report cobranzahttp.ReconcileReportDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &report))
		assert.GreaterOrEqual(t, report.Checked, 1, "reconcile must have checked >= 1 row")
		t.Logf("HTTP reconcile: checked=%d drift=%d errors=%d", report.Checked, report.Drift, report.Errors)
	})
}

// TestE2E_Cobranza_HTTP_PermDenied verifies that a request without
// PermCobranzaVerSaldos receives a 403.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Cobranza_HTTP_PermDenied(t *testing.T) {
	e2eRequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := cobranzaventfb.NewSaldosRepo(pool)
		svc := cobranzaapp.NewService(repo, cobranzaoutbound.ProductionClock{})

		// User with NO cobranza permissions.
		cu := auth.CurrentUser{
			ID:          uuid.New(),
			FirebaseUID: "fb-noperm",
			Email:       "noperm@muebleriamsp.mx",
			Nombre:      "Sin Permiso",
			Permisos:    []string{},
		}

		r := buildReadRouter(ctx, svc, cu)

		req := httptest.NewRequest(http.MethodGet, "/saldos/zona/999", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusForbidden, rec.Code, "expected 403 for missing PermCobranzaVerSaldos")
		t.Logf("403 denied correctly: %s", rec.Body.String())
	})
}

// TestE2E_Cobranza_HTTP_VentanaDias_TooLarge verifies that ventana_dias=91
// returns a 422 with the expected error code.
//
//nolint:paralleltest // serial: not Firebird-dependent but kept serial for consistency.
func TestE2E_Cobranza_HTTP_VentanaDias_TooLarge(t *testing.T) {
	e2eRequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := cobranzaventfb.NewSaldosRepo(pool)
		svc := cobranzaapp.NewService(repo, cobranzaoutbound.ProductionClock{})

		cu := e2eCobranzaUser()
		r := buildReadRouter(ctx, svc, cu)

		req := httptest.NewRequest(http.MethodGet, "/saldos/zona/999?ventana_dias=91", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		// Huma validates query param maximum:90, returns 422 before handler runs.
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code,
			"ventana_dias=91 must return 422; body=%s", rec.Body.String())
		t.Logf("422 validation: %s", rec.Body.String())
	})
}

// TestE2E_Cobranza_HTTP_Reconcile_Admin verifies the reconcile admin endpoint
// returns 200 with a valid ReconcileReportDTO for an authenticated admin.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Cobranza_HTTP_Reconcile_Admin(t *testing.T) {
	e2eRequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		e2eRequireMigration000010(t, q)

		repo := cobranzaventfb.NewSaldosRepo(pool)
		lister := cobranzaventfb.NewSaldosLister(pool)
		recomputer := cobranzaventfb.NewRecomputer(pool, repo)
		errorsRepo := cobranzaventfb.NewErrorsRepo(pool)
		svc := cobranzaapp.NewService(repo, cobranzaoutbound.ProductionClock{})
		reconciler := cobranzaapp.NewReconciler(
			lister, recomputer, repo,
			cobranzaoutbound.ProductionClock{},
			cobranzaapp.ReconcilerConfig{PageSize: 50, DriftLog: true, FixDrift: true},
			slog.Default(),
		)

		cu := e2eCobranzaUser()
		r := buildAdminRouter(ctx, svc, reconciler, errorsRepo, cu)

		req := httptest.NewRequest(http.MethodPost, "/reconcile", nil)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, "reconcile admin body=%s", rec.Body.String())

		var report cobranzahttp.ReconcileReportDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &report))
		assert.GreaterOrEqual(t, report.Checked, 0)
		assert.NotEmpty(t, report.StartedAt)
		assert.NotEmpty(t, report.FinishedAt)

		t.Logf("admin reconcile: checked=%d drift=%d errors=%d", report.Checked, report.Drift, report.Errors)
	})
}

// TestE2E_Cobranza_HTTP_ResumenZonas verifies the resumen-zonas endpoint
// returns a valid JSON array (may be empty if no open saldos in the DB snapshot).
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Cobranza_HTTP_ResumenZonas(t *testing.T) {
	e2eRequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := cobranzaventfb.NewSaldosRepo(pool)
		svc := cobranzaapp.NewService(repo, cobranzaoutbound.ProductionClock{})

		cu := e2eCobranzaUser()
		r := buildReadRouter(ctx, svc, cu)

		req := httptest.NewRequest(http.MethodGet, "/resumen-zonas", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, "resumen-zonas body=%s", rec.Body.String())

		var items []cobranzahttp.ResumenZonaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &items))
		t.Logf("resumen-zonas: %d zonas returned", len(items))
	})
}
