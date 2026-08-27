//nolint:misspell // Spanish vocabulary (estatus, cliente, ventas, etc.) by convention.
package ventfb_test

// Este archivo cubre la costura contra Firebird real de
// validarEstatusClienteMicrosipPreExistente (internal/ventas/app/aplicar_venta.go).
//
// Las pruebas unitarias en internal/ventas/app/aplicar_estatus_cliente_test.go
// ya prueban la regla en sí — con un doble de outbound.ClienteEstatusReader que
// nunca toca Firebird. Lo que esas pruebas NO pueden demostrar es que el
// ESTATUS que el guard evalúa es el mismo ESTATUS que vive en la fila real de
// CLIENTES, ni que el cableado de producción (ventfb.NewClienteRepo pasado a
// Service.WithEstatusReader) queda realmente conectado. newAplicarE2EHarness
// construye el Service SIN WithEstatusReader — el guard queda apagado por
// diseño para el resto de las pruebas E2E — así que aquí lo cableamos a mano y
// verificamos contra un UPDATE real sobre CLIENTES.ESTATUS.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
)

// setClienteEstatus sobreescribe CLIENTES.ESTATUS para testClienteID dentro de
// la transacción de prueba (rollback-only, nada persiste).
func setClienteEstatus(ctx context.Context, t *testing.T, q firebird.Querier, estatus string) {
	t.Helper()
	_, err := q.ExecContext(ctx,
		`UPDATE CLIENTES SET ESTATUS = ? WHERE CLIENTE_ID = ?`, estatus, testClienteID)
	require.NoError(t, err, "actualizar CLIENTES.ESTATUS del fixture")
}

// countDoctosPV cuenta las filas totales en DOCTOS_PV. Se usa antes/después de
// un intento de aplicar bloqueado para demostrar que el guard corta ANTES de
// que el writer de Microsip escriba nada — un guard que rechaza pero deja fila
// escrita sería peor que no tener guard.
func countDoctosPV(ctx context.Context, t *testing.T, q firebird.Querier) int {
	t.Helper()
	var n int
	require.NoError(t, q.QueryRowContext(ctx, `SELECT COUNT(*) FROM DOCTOS_PV`).Scan(&n),
		"contar DOCTOS_PV")
	return n
}

// TestE2E_AplicarVenta_EstatusV_Bloquea verifica que un cliente PRE-EXISTENTE
// con ESTATUS='V' (suspensión de ventas) real en CLIENTES bloquea AplicarVenta
// end-to-end, sin materializar nada en Microsip.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_AplicarVenta_EstatusV_Bloquea(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		userID := seedUsuarioRow(ctx, t, pool)
		h := newAplicarE2EHarness(ctx, t, pool)

		q := firebird.GetQuerier(ctx, pool.DB)
		requireCatalog(t, q) // siembra testClienteID en ESTATUS='A' si no existía

		setClienteEstatus(ctx, t, q, "V")

		// DETALLE CRÍTICO: el harness NO cablea el lector de estatus por
		// defecto (para no afectar al resto de las pruebas E2E). Sin esta
		// línea el guard queda apagado y la prueba pasaría sin ejercitar nada.
		h.svc = h.svc.WithEstatusReader(ventfb.NewClienteRepo(pool))

		v := buildAplicarContadoSinZona(t, userID)
		ventaID := h.persistAprobada(ctx, t, v)

		doctosPVAntes := countDoctosPV(ctx, t, q)

		_, err := h.svc.AplicarVenta(ctx, ventaID, userID)

		require.ErrorIs(t, err, domain.ErrClienteEstatusNoPermiteVenta,
			"un cliente con ESTATUS='V' real en CLIENTES debe bloquear la aplicación")

		doctosPVDespues := countDoctosPV(ctx, t, q)
		assert.Equal(t, doctosPVAntes, doctosPVDespues,
			"el guard debe cortar ANTES de escribir en Microsip: no debe aparecer ningún DOCTOS_PV nuevo")

		reread, err := h.svc.ObtenerVenta(ctx, ventaID)
		require.NoError(t, err, "la venta debe seguir existiendo tras el intento bloqueado")
		assert.Equal(t, domain.SincronizacionPendiente, reread.Sincronizacion(),
			"la venta NO debe quedar aplicada cuando el guard de estatus bloquea")
	})
}

// TestE2E_AplicarVenta_EstatusA_Aplica es el control positivo: la MISMA venta,
// mismo montaje, pero con ESTATUS='A' real en CLIENTES, debe aplicarse sin
// error. Sin esta prueba, la de arriba no distingue "el guard funciona" de
// "el montaje está roto y nada se aplica nunca" — es obligatoria.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_AplicarVenta_EstatusA_Aplica(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		userID := seedUsuarioRow(ctx, t, pool)
		h := newAplicarE2EHarness(ctx, t, pool)

		q := firebird.GetQuerier(ctx, pool.DB)
		requireCatalog(t, q)

		setClienteEstatus(ctx, t, q, "A")

		h.svc = h.svc.WithEstatusReader(ventfb.NewClienteRepo(pool))

		v := buildAplicarContadoSinZona(t, userID)
		ventaID := h.persistAprobada(ctx, t, v)

		result, err := h.svc.AplicarVenta(ctx, ventaID, userID)

		require.NoError(t, err, "un cliente con ESTATUS='A' real no debe bloquear la aplicación")
		assert.Equal(t, domain.SincronizacionAplicada, result.Sincronizacion(),
			"la venta debe quedar aplicada cuando el estatus del cliente lo permite")
		require.NotNil(t, result.MicrosipDoctoPVID(),
			"la venta aplicada debe traer un DOCTO_PV_ID real de Microsip")
	})
}

// TestE2E_AplicarVenta_EstatusC_Bloquea replica la prueba de 'V' pero con
// ESTATUS='C' (suspensión de créditos), que debe bloquear igual.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_AplicarVenta_EstatusC_Bloquea(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		userID := seedUsuarioRow(ctx, t, pool)
		h := newAplicarE2EHarness(ctx, t, pool)

		q := firebird.GetQuerier(ctx, pool.DB)
		requireCatalog(t, q)

		setClienteEstatus(ctx, t, q, "C")

		h.svc = h.svc.WithEstatusReader(ventfb.NewClienteRepo(pool))

		v := buildAplicarContadoSinZona(t, userID)
		ventaID := h.persistAprobada(ctx, t, v)

		_, err := h.svc.AplicarVenta(ctx, ventaID, userID)

		require.ErrorIs(t, err, domain.ErrClienteEstatusNoPermiteVenta,
			"un cliente con ESTATUS='C' real en CLIENTES debe bloquear la aplicación")
	})
}
