// Composition test for GET /v2/visitas/by-ids.
//
// Follows the same shape as obtener_visita_e2e_test.go: the router is
// assembled with the REAL authhttp.AuthnMiddleware over a bearer token, not
// the test `planter` shortcut — only the outermost adapters (Firebase, the
// usuario repo) are stubbed. Everything from chi inwards — authn, capture,
// the visitas handler, the app service, and the visitasfb repo against real
// Firebird — is production code. Reuses obtenerE2ERouter, defined in
// obtener_visita_e2e_test.go, since both routes mount on the exact same
// production composition.
//
// Run: FB_DATABASE=/firebird/data/MUEBLERA.FDB go test ./internal/visitas/infra/visitashttp/... -race
//
//nolint:misspell // visitas vocabulary is Spanish per project convention.
package visitashttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/httptesting"
)

// e2eByIDsGet issues GET /v2/visitas/by-ids?ids=<raw> with a bearer token.
func e2eByIDsGet(root http.Handler, raw string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v2/visitas/by-ids?ids="+raw, nil)
	req.Header.Set("Authorization", "Bearer "+obtenerE2EBearer)
	root.ServeHTTP(rec, req)
	return rec
}

// TestE2E_VisitasExistentesPorIDs_FullChain_HappyPath writes two visitas
// through the real production POST chain, then confirms them (plus an
// unknown id) through GET /v2/visitas/by-ids over the same chain and a real
// Firebird database.
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestE2E_VisitasExistentesPorIDs_FullChain_HappyPath(t *testing.T) {
	visitasE2ERequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		clienteID := visitasE2EClienteID(t, q)

		root, _, _ := obtenerE2ERouter(ctx, t, pool, []authdomain.Permission{authdomain.PermCobranzaVerPagos})

		id1, id2 := uuid.New(), uuid.New()
		unknown := uuid.New()
		fecha := time.Now().UTC().Add(-time.Hour)
		cobrador := visitasE2EMarkerCobrador("Por IDs E2E")

		for _, id := range []uuid.UUID{id1, id2} {
			body := visitasE2EBuildBody(t, id, clienteID, cobrador, "", "No se encontraba", fecha)
			rec := httptest.NewRecorder()
			root.ServeHTTP(rec, httptesting.NewE2ERequest(
				http.MethodPost, "/v2/visitas", string(body),
				httptesting.WithBearer(obtenerE2EBearer),
				httptesting.WithIdempotencyKey(id.String()),
			))
			require.Equal(t, http.StatusCreated, rec.Code, "seed write; body=%s", rec.Body.String())
		}

		raw := id1.String() + "," + unknown.String() + "," + id2.String()
		rec := e2eByIDsGet(root, raw)
		require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())

		var got []string
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.ElementsMatch(t, []string{id1.String(), id2.String()}, got,
			"the unknown id must be absent from the response, not an error")
	})
}

// TestE2E_VisitasExistentesPorIDs_FullChain_NoAuth_401 drives the real authn
// middleware with no bearer token at all.
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestE2E_VisitasExistentesPorIDs_FullChain_NoAuth_401(t *testing.T) {
	visitasE2ERequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root, _, _ := obtenerE2ERouter(ctx, t, pool, []authdomain.Permission{authdomain.PermCobranzaVerPagos})

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v2/visitas/by-ids?ids="+uuid.New().String(), nil)
		root.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code, "body=%s", rec.Body.String())
	})
}

// TestE2E_VisitasExistentesPorIDs_FullChain_SinPermiso_403 authenticates
// successfully through real Firebase+usuario lookup but holds a permission
// that is not the required one.
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestE2E_VisitasExistentesPorIDs_FullChain_SinPermiso_403(t *testing.T) {
	visitasE2ERequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root, _, _ := obtenerE2ERouter(ctx, t, pool, []authdomain.Permission{authdomain.PermCobranzaVerSaldos})

		rec := e2eByIDsGet(root, uuid.New().String())

		assert.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
	})
}

// TestE2E_VisitasExistentesPorIDs_FullChain_AtCap_OK_OverCap_422 is the
// boundary test — cap = maxIDsPorRequest (100), cap+1 — driven through the
// full production chain, including the real Firebird repo's
// "WHERE ID IN (...)" query built with exactly 100 bound parameters.
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestE2E_VisitasExistentesPorIDs_FullChain_AtCap_OK_OverCap_422(t *testing.T) {
	visitasE2ERequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root, _, _ := obtenerE2ERouter(ctx, t, pool, []authdomain.Permission{authdomain.PermCobranzaVerPagos})

		atCap := make([]string, 100)
		for i := range atCap {
			atCap[i] = uuid.New().String()
		}
		recAtCap := e2eByIDsGet(root, strings.Join(atCap, ","))
		require.Equal(t, http.StatusOK, recAtCap.Code,
			"exactly the cap must be accepted through the full chain; body=%s", recAtCap.Body.String())

		overCap := make([]string, 101)
		for i := range overCap {
			overCap[i] = uuid.New().String()
		}
		recOverCap := e2eByIDsGet(root, strings.Join(overCap, ","))
		require.Equal(t, http.StatusUnprocessableEntity, recOverCap.Code,
			"cap+1 must be rejected through the full chain; body=%s", recOverCap.Body.String())
		assert.Contains(t, recOverCap.Body.String(), "ids_too_many")
	})
}

// TestE2E_VisitasExistentesPorIDs_FullChain_DevuelveIDsDeFilasLegacy proves
// the confirmation channel works for a row written with RAW SQL — bypassing
// the domain, exactly as the old Node backend did — carrying a TIPO_VISITA
// outside the closed catalogue. FindExistingIDs only touches the ID column,
// so this is really a defence-in-depth check that the by-ids query never
// trips on a legacy row's other columns, but it is worth pinning down given
// the surrounding module's TipoVisita concerns.
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestE2E_VisitasExistentesPorIDs_FullChain_DevuelveIDsDeFilasLegacy(t *testing.T) {
	visitasE2ERequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		clienteID := visitasE2EClienteID(t, q)

		root, _, _ := obtenerE2ERouter(ctx, t, pool, []authdomain.Permission{authdomain.PermCobranzaVerPagos})

		legacyID := uuid.New()
		fecha := time.Date(2024, 9, 15, 9, 59, 46, 0, time.UTC)
		_, err := q.ExecContext(ctx, `
			INSERT INTO MSP_VISITAS (
				ID, COBRADOR, COBRADOR_ID, FECHA, FORMA_COBRO_ID, LAT, LNG,
				NOTA, TIPO_VISITA, ZONA_CLIENTE_ID, CLIENTE_ID, IMPTE_DOCTO_CC_ID
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			legacyID.String(),
			visitasE2EMarkerCobrador("Legacy ByIDs "+obtenerE2EMarker),
			42,
			firebird.ToWallClock(fecha),
			87327,
			19.432608,
			-99.133209,
			nil,
			"No va a dar pago", // retired literal — outside the closed catalogue
			7,
			clienteID,
			nil,
		)
		require.NoError(t, err, "raw legacy insert")

		rec := e2eByIDsGet(root, legacyID.String())
		require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())

		var got []string
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, []string{legacyID.String()}, got,
			"a legacy row's id must confirm cleanly regardless of its TIPO_VISITA")
	})
}
