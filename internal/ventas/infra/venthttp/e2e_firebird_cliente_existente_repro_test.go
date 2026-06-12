//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
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
func pickActiveClienteID(ctx context.Context, t *testing.T, pool *firebird.Pool) int {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	var id int
	err := q.QueryRowContext(ctx,
		`SELECT FIRST 1 CLIENTE_ID FROM CLIENTES WHERE ESTATUS = 'A' ORDER BY CLIENTE_ID`,
	).Scan(&id)
	require.NoError(t, err, "debe existir al menos un cliente activo en CLIENTES")
	return id
}
