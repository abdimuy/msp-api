// Composition test for GET /v2/visitas/{id}.
//
// TESTING_REQUIREMENTS.md:121 demands a composition test per r.Route(...)
// block in provideRootHandler. Visitas mounts on an r.Group(...), so by the
// letter of that rule this route is exempt. The exemption is ignored on
// purpose: the rule's intent is per-route, and the bugs it exists to catch —
// chi RouteContext leaking, authn rejecting a planted CurrentUser,
// RequirePermission silently bypassed — are all reachable here.
//
// This file differs from visitas_lifecycle_e2e_test.go in one important way:
// that file plants CurrentUser with a test `planter` middleware, whereas this
// one wires the REAL authhttp.AuthnMiddleware and authenticates with a bearer
// token. Only the outermost adapters are stubbed (Firebase, the usuario
// repo); everything from chi inwards — authn, capture, the visitas handler,
// the app service and the visitasfb repo against real Firebird — is
// production code.
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
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authapp "github.com/abdimuy/msp-api/internal/auth/app"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/infra/authhttp"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	failedintentfb "github.com/abdimuy/msp-api/internal/platform/failedintent/firebird"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/httptesting"
	visitasapp "github.com/abdimuy/msp-api/internal/visitas/app"
	"github.com/abdimuy/msp-api/internal/visitas/infra/visitasfb"
	"github.com/abdimuy/msp-api/internal/visitas/infra/visitashttp"
	visitasoutbound "github.com/abdimuy/msp-api/internal/visitas/ports/outbound"
)

// ─── Router assembly (real authn) ───────────────────────────────────────────

const (
	obtenerE2EFirebaseUID = "fb-visitas-obtener-e2e"
	obtenerE2EBearer      = "raw-token-visitas-obtener"
	obtenerE2EMarker      = "TASK7_VISITAS_GET_E2E"
)

// obtenerE2ERouter assembles the production composition for the visitas
// group: r.Route("/v2") wrapping a bare Group carrying [txInjector,
// authn.Handler, capture], with visitashttp.MountRouter inside.
//
// perms are granted to the authenticated principal, so a caller can drive the
// 403 axis by passing none.
func obtenerE2ERouter(
	ctx context.Context,
	t *testing.T,
	pool *firebird.Pool,
	perms []authdomain.Permission,
) (http.Handler, *failedintentfb.Store, uuid.UUID) {
	t.Helper()

	usuarioID := uuid.New()
	fb := httptesting.NewFakeFirebase(obtenerE2EFirebaseUID)
	fb.Token.Email = "visitas-get-e2e@muebleriamsp.mx"
	fb.Token.Name = "Visitas GET E2E"

	usuarios := httptesting.NewFakeUsuarioRepo()
	usuarios.AddUsuario(httptesting.AddUsuarioParams{
		ID:          usuarioID,
		FirebaseUID: obtenerE2EFirebaseUID,
		Email:       "visitas-get-e2e@muebleriamsp.mx",
		Nombre:      "Visitas GET E2E",
		Activo:      true, // an inactive principal is rejected by authn itself
		Permissions: perms,
	})

	// The real authn middleware, over stubbed Firebase + usuario repo.
	authSvc := authapp.NewService(usuarios, nil, nil, obtenerE2EClock{}, nil, fb, nil)
	authn := authhttp.NewAuthnMiddleware(fb, usuarios, authSvc)

	fiStore := failedintentfb.New(pool)
	// Scoped exactly as production does it (failedintent_wiring.go): the
	// visitas capture is POST-only, so a GET must never be captured.
	capture := failedintent.CaptureMiddleware(failedintent.Config{
		Store:        fiStore,
		PathPrefixes: []string{"/v2/visitas"},
		Methods:      []string{http.MethodPost},
	})

	repo := visitasfb.New(pool)
	svc := visitasapp.NewService(repo, visitasoutbound.ProductionClock{})

	root := chi.NewRouter()
	root.Route("/v2", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(visitasTxInjector(ctx), authn.Handler, capture)
			visitashttp.MountRouter(r, svc)
		})
	})
	return root, fiStore, usuarioID
}

type obtenerE2EClock struct{}

func (obtenerE2EClock) Now() time.Time { return time.Now().UTC() }

// obtenerE2EGet issues GET /v2/visitas/{id} with a bearer token unless
// noBearer is set.
func obtenerE2EGet(root http.Handler, id string, noBearer bool) *httptest.ResponseRecorder {
	opts := []httptesting.E2ERequestOpt{httptesting.WithBearer(obtenerE2EBearer)}
	if noBearer {
		opts = []httptesting.E2ERequestOpt{httptesting.NoBearer()}
	}
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, httptesting.NewE2ERequest(http.MethodGet, "/v2/visitas/"+id, "", opts...))
	return rec
}

// ─── Tests ──────────────────────────────────────────────────────────────────

// TestE2E_ObtenerVisita_FullChain_HappyPath drives POST then GET through the
// full production chain against real Firebird, and asserts every field
// survives the round trip.
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestE2E_ObtenerVisita_FullChain_HappyPath(t *testing.T) {
	visitasE2ERequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		clienteID := visitasE2EClienteID(t, q)

		root, _, _ := obtenerE2ERouter(ctx, t, pool, []authdomain.Permission{authdomain.PermCobranzaVerPagos})

		visitaID := uuid.New()
		fecha := time.Now().UTC().Add(-2 * time.Hour)
		cobrador := visitasE2EMarkerCobrador("María de los Ángeles Hernández")
		nota := "cliente no estaba, vuelvo mañana"
		body := visitasE2EBuildBody(t, visitaID, clienteID, cobrador, nota, "No se encontraba", fecha)

		// Write through the same chain, authenticated with a real bearer.
		recPost := httptest.NewRecorder()
		root.ServeHTTP(recPost, httptesting.NewE2ERequest(
			http.MethodPost, "/v2/visitas", string(body),
			httptesting.WithBearer(obtenerE2EBearer),
			httptesting.WithIdempotencyKey(visitaID.String()),
		))
		require.Equal(t, http.StatusCreated, recPost.Code, "seed write; body=%s", recPost.Body.String())

		// Read it back.
		rec := obtenerE2EGet(root, visitaID.String(), false)
		require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())

		var dto visitashttp.VisitaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
		assert.Equal(t, visitaID.String(), dto.ID)
		assert.Equal(t, clienteID, dto.ClienteID)
		assert.Equal(t, 42, dto.CobradorID)
		assert.Equal(t, cobrador, dto.Cobrador, "accents must survive the full stack")
		assert.Equal(t, 87327, dto.FormaCobroID)
		assert.InDelta(t, 19.432608, dto.Lat, 0.000001)
		assert.InDelta(t, -99.133209, dto.Lng, 0.000001)
		assert.Equal(t, nota, dto.Nota)
		assert.Equal(t, "No se encontraba", dto.TipoVisita)
		assert.Equal(t, 7, dto.ZonaClienteID)
		assert.WithinDuration(t, fecha, mustParseRFC3339(t, dto.Fecha), time.Second)
	})
}

// TestE2E_ObtenerVisita_FullChain_NoAuth_401 drives the real authn middleware
// with no bearer token at all.
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestE2E_ObtenerVisita_FullChain_NoAuth_401(t *testing.T) {
	visitasE2ERequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root, _, _ := obtenerE2ERouter(ctx, t, pool, []authdomain.Permission{authdomain.PermCobranzaVerPagos})

		rec := obtenerE2EGet(root, uuid.New().String(), true)

		assert.Equal(t, http.StatusUnauthorized, rec.Code, "body=%s", rec.Body.String())
	})
}

// TestE2E_ObtenerVisita_FullChain_SinPermiso_403 authenticates successfully
// through real Firebase+usuario lookup but holds a permission that is not the
// required one — proving the 403 comes from authorization, not authentication.
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestE2E_ObtenerVisita_FullChain_SinPermiso_403(t *testing.T) {
	visitasE2ERequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root, _, _ := obtenerE2ERouter(ctx, t, pool, []authdomain.Permission{authdomain.PermCobranzaVerSaldos})

		rec := obtenerE2EGet(root, uuid.New().String(), false)

		assert.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
	})
}

// TestE2E_ObtenerVisita_FullChain_NotFound_404_YNoSeCaptura asserts a miss is
// a 404 and — because the visitas capture is scoped to POST — that the failing
// GET leaves nothing in MSP_FAILED_INTENTS. A capture config that widened to
// all methods would fill the desk-correction queue with unactionable reads.
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestE2E_ObtenerVisita_FullChain_NotFound_404_YNoSeCaptura(t *testing.T) {
	visitasE2ERequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root, fiStore, usuarioID := obtenerE2ERouter(ctx, t, pool, []authdomain.Permission{authdomain.PermCobranzaVerPagos})

		rec := obtenerE2EGet(root, uuid.New().String(), false)
		require.Equal(t, http.StatusNotFound, rec.Code, "body=%s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), "visita_not_found")

		list, err := fiStore.List(ctx, failedintent.ListParams{UsuarioID: &usuarioID})
		require.NoError(t, err)
		assert.Empty(t, list.Items, "a failing GET must not be captured — capture is POST-only")
	})
}

// TestE2E_ObtenerVisita_FullChain_LeeFilasLegacy is the decisive test for the
// TipoVisita decision, and the reason the catalogue could be closed on the
// write path at all.
//
// Each case writes a row into MSP_VISITAS with RAW SQL — bypassing the domain
// entirely, exactly as the old Node backend did — carrying a TIPO_VISITA that
// NewVisita would now refuse. The row is then read through the full HTTP
// stack. All of them must come back 200 with the value echoed verbatim.
//
// The values are not invented: they are measured. Together the three retired
// literals and the empty string account for 59,662 of the 226,746 rows in
// MSP_VISITAS — 26% of the table. If the read path validated, GET
// /v2/visitas/{id} would fail on more than a quarter of production.
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestE2E_ObtenerVisita_FullChain_LeeFilasLegacy(t *testing.T) {
	visitasE2ERequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	legacy := []struct {
		name string
		tipo string
	}{
		{"empty string (200 real rows)", ""},
		{"retired literal (38,855 real rows)", "No va a dar pago"},
		{"retired literal (16,925 real rows)", "Dijo que no va a pagar"},
		{"retired literal (3,682 real rows)", "Se esconde y no sale"},
		{"a value no app build ever sent", "cobro"},
	}

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		clienteID := visitasE2EClienteID(t, q)

		root, _, _ := obtenerE2ERouter(ctx, t, pool, []authdomain.Permission{authdomain.PermCobranzaVerPagos})

		for _, tc := range legacy {
			visitaID := uuid.New()
			fecha := time.Date(2024, 9, 15, 9, 59, 46, 0, time.UTC)

			// Raw INSERT: the domain never sees this value, just as it never
			// saw the ones the Node backend wrote.
			_, err := q.ExecContext(ctx, `
				INSERT INTO MSP_VISITAS (
					ID, COBRADOR, COBRADOR_ID, FECHA, FORMA_COBRO_ID, LAT, LNG,
					NOTA, TIPO_VISITA, ZONA_CLIENTE_ID, CLIENTE_ID, IMPTE_DOCTO_CC_ID
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				visitaID.String(),
				visitasE2EMarkerCobrador("Legacy "+obtenerE2EMarker),
				42,
				firebird.ToWallClock(fecha),
				87327,
				19.432608,
				-99.133209,
				nil,
				tc.tipo,
				7,
				clienteID,
				nil,
			)
			require.NoError(t, err, "raw legacy insert for %q", tc.name)

			rec := obtenerE2EGet(root, visitaID.String(), false)
			require.Equal(t, http.StatusOK, rec.Code,
				"%s: a legacy row must read back cleanly, not error; body=%s", tc.name, rec.Body.String())

			var dto visitashttp.VisitaDTO
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
			assert.Equal(t, tc.tipo, dto.TipoVisita,
				"%s: the stored value must be echoed verbatim, unvalidated", tc.name)
			assert.Equal(t, visitaID.String(), dto.ID)
		}
	})
}

// TestE2E_CrearVisita_FullChain_TipoFueraDelCatalogo_422 is the write-path
// counterpart driven through the same chain: the very value the test above
// reads back happily is refused on POST, with a distinguishable code — and
// because the write path IS captured, the rejection lands in
// MSP_FAILED_INTENTS for desk correction rather than being lost.
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestE2E_CrearVisita_FullChain_TipoFueraDelCatalogo_422(t *testing.T) {
	visitasE2ERequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		clienteID := visitasE2EClienteID(t, q)

		root, fiStore, usuarioID := obtenerE2ERouter(ctx, t, pool, []authdomain.Permission{authdomain.PermCobranzaVerPagos})

		visitaID := uuid.New()
		fecha := time.Now().UTC().Add(-time.Hour)
		body := visitasE2EBuildBody(t, visitaID, clienteID,
			visitasE2EMarkerCobrador("Ramírez García, Jorge"), "", "cobro", fecha)

		rec := httptest.NewRecorder()
		root.ServeHTTP(rec, httptesting.NewE2ERequest(
			http.MethodPost, "/v2/visitas", string(body),
			httptesting.WithBearer(obtenerE2EBearer),
			httptesting.WithIdempotencyKey(visitaID.String()),
		))

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body=%s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), "visita_tipo_invalido")
		assert.Equal(t, 0, visitasE2ECountByID(ctx, t, q, visitaID), "nothing may be persisted")

		list, err := fiStore.List(ctx, failedintent.ListParams{UsuarioID: &usuarioID})
		require.NoError(t, err)
		assert.Len(t, list.Items, 1, "a rejected write must be captured for desk correction")
	})
}

// TestE2E_CrearVisita_FullChain_AceptaLiteralRetirado closes the loop on the
// field-devices risk: a phone still running the pre-2025-09-02 build sends
// "Dijo que no va a pagar", and it must be accepted and persisted, not
// captured as a failure.
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestE2E_CrearVisita_FullChain_AceptaLiteralRetirado(t *testing.T) {
	visitasE2ERequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		clienteID := visitasE2EClienteID(t, q)

		root, fiStore, usuarioID := obtenerE2ERouter(ctx, t, pool, []authdomain.Permission{authdomain.PermCobranzaVerPagos})

		visitaID := uuid.New()
		fecha := time.Now().UTC().Add(-time.Hour)
		body := visitasE2EBuildBody(t, visitaID, clienteID,
			visitasE2EMarkerCobrador("Ramírez García, Jorge"), "", "Dijo que no va a pagar", fecha)

		rec := httptest.NewRecorder()
		root.ServeHTTP(rec, httptesting.NewE2ERequest(
			http.MethodPost, "/v2/visitas", string(body),
			httptesting.WithBearer(obtenerE2EBearer),
			httptesting.WithIdempotencyKey(visitaID.String()),
		))
		require.Equal(t, http.StatusCreated, rec.Code,
			"an unupdated phone must still be served; body=%s", rec.Body.String())

		// And it reads back with the retired literal intact.
		recGet := obtenerE2EGet(root, visitaID.String(), false)
		require.Equal(t, http.StatusOK, recGet.Code, "body=%s", recGet.Body.String())
		var dto visitashttp.VisitaDTO
		require.NoError(t, json.Unmarshal(recGet.Body.Bytes(), &dto))
		assert.Equal(t, "Dijo que no va a pagar", dto.TipoVisita)

		list, err := fiStore.List(ctx, failedintent.ListParams{UsuarioID: &usuarioID})
		require.NoError(t, err)
		assert.Empty(t, list.Items, "an accepted write must not be captured")
	})
}

// mustParseRFC3339 parses an RFC3339 timestamp or fails the test.
func mustParseRFC3339(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err, "response carried a non-RFC3339 timestamp: %q", s)
	return ts
}
