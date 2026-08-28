//nolint:misspell // ventas vocabulary is Spanish (vendedores, camioneta) per project convention.
package venthttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/imageprocessor"
	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
	ventasoutbound "github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// e2eRoster is a stand-in fleet roster for the E2E path. It answers with a
// fixed list regardless of the camioneta, recording the id it was asked
// about.
type e2eRoster struct {
	res           ventasoutbound.VendedoresDeCamioneta
	lastCamioneta int
	calls         int
}

func (r *e2eRoster) VendedoresDeCamioneta(
	_ context.Context, camionetaID int,
) (ventasoutbound.VendedoresDeCamioneta, error) {
	r.calls++
	r.lastCamioneta = camionetaID
	return r.res, nil
}

// vendedorEmailEnBD reads the address actually stored on the venta's single
// vendedor row. Asserting on the response DTO alone would not prove the
// column holds the canonical value — that is the whole point of this test.
func vendedorEmailEnBD(
	ctx context.Context, t *testing.T, pool *firebird.Pool, ventaID string,
) string {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	var email string
	err := q.QueryRowContext(ctx,
		`SELECT VENDEDOR_EMAIL FROM MSP_VENTAS_VENDEDORES WHERE VENTA_ID = ?`,
		ventaID,
	).Scan(&email)
	require.NoError(t, err)
	return strings.TrimSpace(email)
}

// TestE2E_Firebird_VendedoresResueltosPorElServidor drives the whole HTTP
// path against the real Firebird schema and pins the two things that only a
// real write can settle:
//
//  1. the venta persisted in MSP_VENTAS_VENDEDORES references the usuario the
//     ROSTER named, not the one the phone sent; and
//  2. VENDEDOR_EMAIL holds the canonical address even though every address
//     involved — the roster's, the phone's — arrived capitalized.
//
// Not parallel, for the same reason as the other E2E tests here: they share
// one rollback-only Firebird transaction.
func TestE2E_Firebird_VendedoresResueltosPorElServidor(t *testing.T) { //nolint:paralleltest // shared rollback-only tx
	pool := e2eTestPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		autorID := seedE2EUsuario(ctx, t, pool)
		// The person the roster names, provisioned with a canonical address.
		rosterEmail := "vendedor-" + uuid.NewString() + "@muebleriamsp.mx"
		rosterID := seedE2EUsuarioConEmail(ctx, t, pool, rosterEmail)

		roster := &e2eRoster{res: ventasoutbound.VendedoresDeCamioneta{
			Vendedores: []ventasoutbound.VendedorDeCamioneta{
				// Written the way a hand-typed Firestore document would be.
				{Email: strings.ToUpper(rosterEmail), Nombre: "ANA LAURA MENDEZ"},
			},
		}}

		repo := ventfb.NewVentaRepo(pool)
		svc := ventasapp.NewService(
			repo, nil, ventfb.NewUsuarioExistenceRepo(pool), newFakeStorage(),
			fixedClock{T: e2eFixedTime()}, noopOutbox{}, imageprocessor.NoOpProcessor{},
			nil, nil, nil, nil,
		).WithVendedoresDeCamioneta(roster, ventfb.NewUsuarioEmailRepo(pool))

		cu := auth.CurrentUser{
			ID:          autorID,
			FirebaseUID: "fb-e2e-" + autorID.String(),
			Email:       "e2e@example.invalid",
			Nombre:      "E2E Tester",
			Permisos: []string{
				string(authdomain.PermVentasVer),
				string(authdomain.PermVentasCrear),
			},
		}

		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(cu))
		venthttp.MountRouter(r, svc)

		body := validCreateBody()
		// The phone sends somebody else entirely, capitalized — the exact
		// shape that used to slip through unnoticed.
		body.Vendedores[0].UsuarioID = autorID.String()
		body.Vendedores[0].Email = "OTRO.VENDEDOR@MuebleriaMSP.MX"
		require.NotNil(t, body.Productos[0].AlmacenOrigenID,
			"the fixture must ship from an almacén de origen; that is the camioneta")
		camionetaEsperada := *body.Productos[0].AlmacenOrigenID

		req := crearVentaMultipartRequest(t, body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, "create body=%s", rec.Body.String())

		var created venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
		require.Len(t, created.Vendedores, 1)
		assert.Equal(t, rosterID.String(), created.Vendedores[0].UsuarioID,
			"the roster's usuario must be the one persisted, not the phone's")
		assert.Equal(t, rosterEmail, created.Vendedores[0].Email)

		assert.Equal(t, 1, roster.calls)
		assert.Equal(t, camionetaEsperada, roster.lastCamioneta,
			"the camioneta consulted is the venta's almacén de origen")

		assert.Equal(t, rosterEmail, vendedorEmailEnBD(ctx, t, pool, body.ID),
			"VENDEDOR_EMAIL must be stored canonical, whatever case arrived")
	})
}

// seedE2EUsuarioConEmail inserts a usuario row with the exact EMAIL given,
// inside the active test tx, and returns its id.
func seedE2EUsuarioConEmail(
	ctx context.Context, t *testing.T, pool *firebird.Pool, email string,
) uuid.UUID {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	id := uuid.New()
	now := e2eFixedTime()
	_, err := q.ExecContext(
		ctx,
		`INSERT INTO MSP_USUARIOS
		 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO, ESTATUS,
		  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
		 VALUES (?, ?, ?, 'e2e-vendedor', TRUE, 'FIREBASE_USER', ?, ?, ?, ?)`,
		id.String(), "fb-e2e-"+id.String(), email, now, now, id.String(), id.String(),
	)
	require.NoError(t, err, "seed e2e usuario with email %q", email)
	return id
}

// e2eRosterCaido is a roster that always fails, standing in for the Firebase
// outage the safety net exists for.
type e2eRosterCaido struct{ calls int }

func (r *e2eRosterCaido) VendedoresDeCamioneta(
	_ context.Context, _ int,
) (ventasoutbound.VendedoresDeCamioneta, error) {
	r.calls++
	return ventasoutbound.VendedoresDeCamioneta{}, errRosterCaido
}

// errRosterCaido is the failure the fake roster returns.
var errRosterCaido = errors.New("roster no disponible")

// TestE2E_Firebird_RosterCaido_GuardaLoDelClienteCanonico is the other half of
// the write-path proof: when the roster is down the venta is still created,
// carrying the phone's vendedor — and VENDEDOR_EMAIL is stored canonical even
// though the phone sent it capitalized. That second part is the fix that
// needed no new APK, measured where it matters (the column), not in a DTO.
func TestE2E_Firebird_RosterCaido_GuardaLoDelClienteCanonico(t *testing.T) { //nolint:paralleltest // shared rollback-only tx
	pool := e2eTestPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		autorID := seedE2EUsuario(ctx, t, pool)
		roster := &e2eRosterCaido{}

		svc := ventasapp.NewService(
			ventfb.NewVentaRepo(pool), nil, ventfb.NewUsuarioExistenceRepo(pool),
			newFakeStorage(), fixedClock{T: e2eFixedTime()}, noopOutbox{},
			imageprocessor.NoOpProcessor{}, nil, nil, nil, nil,
		).WithVendedoresDeCamioneta(roster, ventfb.NewUsuarioEmailRepo(pool))

		cu := auth.CurrentUser{
			ID:          autorID,
			FirebaseUID: "fb-e2e-" + autorID.String(),
			Email:       "e2e@example.invalid",
			Nombre:      "E2E Tester",
			Permisos: []string{
				string(authdomain.PermVentasVer),
				string(authdomain.PermVentasCrear),
			},
		}

		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(cu))
		venthttp.MountRouter(r, svc)

		body := validCreateBody()
		body.Vendedores[0].UsuarioID = autorID.String()
		body.Vendedores[0].Email = "Ana.Laura@MuebleriaMSP.MX"

		req := crearVentaMultipartRequest(t, body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code,
			"a roster outage must never fail the venta; body=%s", rec.Body.String())

		assert.Equal(t, 1, roster.calls, "the roster was consulted and it failed")
		assert.Equal(t, "ana.laura@muebleriamsp.mx",
			vendedorEmailEnBD(ctx, t, pool, body.ID),
			"the phone's capitalized address must land canonical in the column")
	})
}

// TestE2E_Firebird_ReemplazarVendedores_GuardaCanonico covers the OTHER write
// path the office uses: PUT /ventas/{id}/vendedores, where the roster never
// runs and the address the caller types is the address that gets stored.
//
// It exists because the server-side roster only covers creation. Without this,
// the desktop could still write a capitalized address into VENDEDOR_EMAIL and
// reintroduce the mismatch one endpoint over.
func TestE2E_Firebird_ReemplazarVendedores_GuardaCanonico(t *testing.T) { //nolint:paralleltest // shared rollback-only tx
	pool := e2eTestPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)

		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		id := e2eSeedVenta(t, r, usuarioID)

		body := validVendedoresBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		body.Vendedores[0].Email = "  Beto.Suarez@MuebleriaMSP.MX "

		req := jsonRequest(t, http.MethodPut, "/ventas/"+id+"/vendedores", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "replace body=%s", rec.Body.String())

		assert.Equal(t, "beto.suarez@muebleriamsp.mx", vendedorEmailEnBD(ctx, t, pool, id),
			"the replace path must canonicalize too, or the mismatch comes back")
	})
}
