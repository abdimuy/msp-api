//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/microsipseed"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// TestRepro_CrearVentaConClienteExistente reproduces "no se puede crear una
// venta con un cliente ya existente". It wires the REAL ClienteExistenceChecker
// (like production) and POSTs a venta whose cliente_id points to a real, active
// row in Microsip CLIENTES. Everything runs inside a rollback-only tx.
//
//nolint:paralleltest // shares one rollback tx with the dev DB
func TestRepro_CrearVentaConClienteExistente(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)

		// Pick a real, active cliente_id from CLIENTES (the FK target).
		clienteID := pickActiveClienteID(ctx, t, pool)
		t.Logf("usando CLIENTE_ID existente = %d", clienteID)

		svc := buildE2EServiceWithCliente(pool) // real cliente checker, like prod
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		// Case A: existing cliente_id → expected 201 Created.
		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		body.Cliente.ClienteID = &clienteID

		req := crearVentaMultipartRequest(t, body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		t.Logf("CASE A (cliente existente) → status=%d body=%s", rec.Code, rec.Body.String())
		require.Equal(t, http.StatusCreated, rec.Code,
			"crear venta con cliente existente debe ser 201; got %d: %s", rec.Code, rec.Body.String())

		var created venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
		require.NotNil(t, created.Cliente.ClienteID, "respuesta debe conservar cliente_id")
		assert.Equal(t, clienteID, *created.Cliente.ClienteID)

		// Case B: bogus cliente_id → expected 422 cliente_id_invalido.
		bogus := 999999999
		bodyB := validCreateBody()
		bodyB.Vendedores[0].UsuarioID = usuarioID.String()
		bodyB.Cliente.ClienteID = &bogus
		reqB := crearVentaMultipartRequest(t, bodyB)
		recB := httptest.NewRecorder()
		r.ServeHTTP(recB, reqB)
		t.Logf("CASE B (cliente inexistente) → status=%d body=%s", recB.Code, recB.Body.String())
	})
}

// pickActiveClienteID returns the CLIENTE_ID of an active Microsip cliente,
// suitable as the FK target for MSP_VENTAS.CLIENTE_ID.
//
// It SEEDS the cliente instead of picking one from the padrón. It used to read
// `FIRST 1 ... WHERE ESTATUS='A'`, which made the test depend on the shared DB
// carrying real clients — against the test artifact, which no longer ships the
// padrón (docs/base-de-datos-de-pruebas.md), that query returns no rows and the
// test fails for a reason that has nothing to do with the code under test.
//
// Everything runs inside the caller's rollback-only transaction, so the seeded
// cliente disappears with it.
func pickActiveClienteID(ctx context.Context, t *testing.T, pool *firebird.Pool) int {
	t.Helper()
	return seedClienteConClave(ctx, t, pool, "VENTAS E2E CLIENTE EXISTENTE")
}

// seedClienteConClave inserts a synthetic cliente together with its
// CLAVES_CLIENTES row and returns the CLIENTE_ID.
//
// The clave is not optional for anything that reaches AplicarVenta: the
// Microsip writer reads CLAVE_CLIENTE before inserting the venta and fails with
// clave_cliente_not_found when it is missing.
func seedClienteConClave(ctx context.Context, t *testing.T, pool *firebird.Pool, nombre string) int {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	clienteID := microsipseed.Cliente(t, q, nombre)

	var rolClaveID int
	require.NoError(t,
		q.QueryRowContext(ctx,
			`SELECT FIRST 1 ROL_CLAVE_CLI_ID FROM ROLES_CLAVES_CLIENTES ORDER BY ROL_CLAVE_CLI_ID`,
		).Scan(&rolClaveID),
		"ROLES_CLAVES_CLIENTES debe tener al menos una fila")

	_, err := q.ExecContext(ctx,
		`INSERT INTO CLAVES_CLIENTES
		   (CLAVE_CLIENTE_ID, CLAVE_CLIENTE, CLIENTE_ID, ROL_CLAVE_CLI_ID)
		 VALUES (-1, ?, ?, ?)`,
		strconv.Itoa(clienteID), clienteID, rolClaveID)
	require.NoError(t, err, "insertar CLAVES_CLIENTES del cliente sembrado")

	return clienteID
}
