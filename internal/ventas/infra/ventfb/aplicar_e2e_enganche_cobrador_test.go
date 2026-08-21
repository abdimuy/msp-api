//nolint:misspell // Spanish vocabulary by project convention.
package ventfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/infra/microsip"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// Reproducción del caso reportado el 2026-08-21: en producción, cada día
// aparecen enganches nuestros CANCELADOS y otro igual a su lado, creado por un
// usuario RUTAxx menos de un segundo después.
//
// Medido en producción el 20-ago: creamos 81 enganches, 20 quedaron cancelados,
// y los usuarios RUTAxx crearon exactamente 20. Uno a uno. Los dos documentos
// son idénticos en concepto, forma de cobro, importe, sucursal y estado
// aplicado — la ÚNICA diferencia es que el nuestro va con `COBRADOR_ID` nulo.
//
// La hipótesis que estas pruebas ponen a prueba es que ese par no lo produce
// nadie capturando a mano, sino la EDICIÓN del documento: en sistemas
// contables es común que modificar un documento ya aplicado se implemente como
// cancelar el viejo y emitir uno nuevo. Si eso viviera en la base —en un
// trigger de DOCTOS_CC— un UPDATE nuestro lo reproduciría aquí.
//
// El valor de la prueba está en las DOS respuestas posibles:
//
//   - Si duplica: el mecanismo es de la base, y ponerle el cobrador al crear
//     elimina el motivo de tocarlo.
//   - Si NO duplica: el cancelar+recrear lo hace el cliente de Microsip (o una
//     persona), y completar el campo no basta — la decisión pasa a ser de
//     proceso, no de código.

// engancheDeLaVenta localiza el enganche que la cascada dejó colgando del
// cargo de esa venta. Misma ruta que usa TestE2E_AplicarVenta_FechaVenta_*:
// DOCTOS_ENTRE_SIS lleva del DOCTO_PV al cargo, y el enganche cuelga del cargo
// por IMPORTES_DOCTOS_CC.DOCTO_CC_ACR_ID con TIPO_IMPTE='R'.
//
//nolint:nonamedreturns // dos ints del mismo tipo: sin nombres no se sabe cuál es cuál.
func engancheDeLaVenta(ctx context.Context, t *testing.T, q firebird.Querier, doctoPVID int) (cargo, enganche int) {
	t.Helper()
	require.NoError(t, q.QueryRowContext(ctx, `
		SELECT D.DOCTO_CC_ID
		FROM DOCTOS_ENTRE_SIS E
		JOIN DOCTOS_CC D ON D.DOCTO_CC_ID = E.DOCTO_DEST_ID
		WHERE E.CLAVE_SIS_FTE = 'PV' AND E.CLAVE_SIS_DEST = 'CC' AND E.DOCTO_FTE_ID = ?`,
		doctoPVID).Scan(&cargo), "debe existir el cargo de la venta")

	require.NoError(t, q.QueryRowContext(ctx, `
		SELECT DOCTO_CC_ID FROM IMPORTES_DOCTOS_CC
		WHERE DOCTO_CC_ACR_ID = ? AND TIPO_IMPTE = 'R'`,
		cargo).Scan(&enganche), "debe existir el enganche")
	return cargo, enganche
}

// contarEnganchesDelCliente cuenta los documentos de enganche del cliente en
// esa fecha, y cuántos están cancelados.
//
//nolint:nonamedreturns // idem: (total, cancelados) se leen mal como (int, int).
func contarEnganchesDelCliente(
	ctx context.Context, t *testing.T, q firebird.Querier, engancheID int,
) (total, cancelados int) {
	t.Helper()
	require.NoError(t, q.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN d.CANCELADO = 'S' THEN 1 ELSE 0 END), 0)
		FROM DOCTOS_CC d
		WHERE d.CONCEPTO_CC_ID = (SELECT CONCEPTO_CC_ID FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?)
		  AND d.CLIENTE_ID     = (SELECT CLIENTE_ID     FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?)
		  AND d.FECHA          = (SELECT FECHA          FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?)`,
		engancheID, engancheID, engancheID).Scan(&total, &cancelados))
	return total, cancelados
}

// TestE2E_Enganche_LlevaElCobradorDeLaZona fija que el enganche sale con dueño.
//
// Sin `COBRADOR_ID` el documento no pertenece a ninguna ruta, y en producción
// es el único campo que lo distingue del que acaba reemplazándolo. Que la
// duplicación se deba o no a eso está sin probar; que el documento esté
// incompleto, no.
//
//nolint:paralleltest // serial: comparte la tx con rollback.
func TestE2E_Enganche_LlevaElCobradorDeLaZona(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewVentaWriter(pool)
	fechaVenta := time.Date(2025, 3, 15, 12, 0, 0, 0, time.UTC)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		seedClienteFixture(t, q)
		userID := seedUsuarioRow(ctx, t, pool)

		var cobradorDeLaZona int
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT COBRADOR_ID FROM MSP_CFG_ZONA_CAJA WHERE ZONA_CLIENTE_ID = ?`,
			testZonaID).Scan(&cobradorDeLaZona))
		require.Positive(t, cobradorDeLaZona, "la zona de prueba debe tener cobrador")

		fpID := formaDePagoSemanalID
		cmID := creditoEnMeses12ID
		res, err := writer.Aplicar(ctx, outbound.MicrosipVentaInput{
			Venta:                buildAplicarCreditoConFechaVenta(t, userID, fechaVenta),
			CajaID:               testCajaID,
			CajeroID:             testCajeroID,
			VendedorID:           testVendedorID,
			CobradorID:           cobradorDeLaZona,
			SucursalID:           testSucursalID,
			FormaCobroID:         formaCobroCreditoID,
			FormaDePagoID:        &fpID,
			CreditoEnMesesID:     &cmID,
			NumeroDeVendedoresID: numVendedores1ID,
		})
		require.NoError(t, err)

		_, engancheID := engancheDeLaVenta(ctx, t, q, res.DoctoPVID)

		var aplicado, cancelado string
		var cobrador *int
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT APLICADO, CANCELADO, COBRADOR_ID FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`,
			engancheID).Scan(&aplicado, &cancelado, &cobrador))

		assert.Equal(t, "S", aplicado, "el enganche queda aplicado: cuenta como dinero recibido")
		assert.Equal(t, "N", cancelado)
		require.NotNil(t, cobrador, "el enganche debe salir con dueño, no con COBRADOR_ID nulo")
		assert.Equal(t, cobradorDeLaZona, *cobrador)
	})
}

// TestE2E_Enganche_SinCobradorEnLaZonaSigueSaliendo cubre las zonas que no
// tienen cobrador (MAYOREO): el centinela -1 debe escribirse como NULL, no
// reventar ni meter un id inventado. Es el mismo trato que ya le da
// cliente_writer al mismo centinela.
//
//nolint:paralleltest // serial: comparte la tx con rollback.
func TestE2E_Enganche_SinCobradorEnLaZonaSigueSaliendo(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewVentaWriter(pool)
	fechaVenta := time.Date(2025, 3, 15, 12, 0, 0, 0, time.UTC)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		seedClienteFixture(t, q)
		userID := seedUsuarioRow(ctx, t, pool)

		fpID := formaDePagoSemanalID
		cmID := creditoEnMeses12ID
		res, err := writer.Aplicar(ctx, outbound.MicrosipVentaInput{
			Venta:                buildAplicarCreditoConFechaVenta(t, userID, fechaVenta),
			CajaID:               testCajaID,
			CajeroID:             testCajeroID,
			VendedorID:           testVendedorID,
			CobradorID:           -1, // zona sin cobrador
			SucursalID:           testSucursalID,
			FormaCobroID:         formaCobroCreditoID,
			FormaDePagoID:        &fpID,
			CreditoEnMesesID:     &cmID,
			NumeroDeVendedoresID: numVendedores1ID,
		})
		require.NoError(t, err, "una zona sin cobrador no puede impedir aplicar la venta")

		_, engancheID := engancheDeLaVenta(ctx, t, q, res.DoctoPVID)

		var cobrador *int
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT COBRADOR_ID FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`, engancheID).Scan(&cobrador))
		assert.Nil(t, cobrador, "el centinela -1 se guarda como NULL, no como el id -1")
	})
}

// TestE2E_Enganche_PonerleElCobradorNoDuplica es la prueba de la hipótesis.
//
// Si el par cancelar+recrear que se ve en producción lo produjera un trigger de
// la base al modificar el documento, este UPDATE lo reproduciría: aparecería un
// segundo documento y el nuestro quedaría cancelado.
//
//nolint:paralleltest // serial: comparte la tx con rollback.
func TestE2E_Enganche_PonerleElCobradorNoDuplica(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewVentaWriter(pool)
	fechaVenta := time.Date(2025, 3, 15, 12, 0, 0, 0, time.UTC)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		seedClienteFixture(t, q)
		userID := seedUsuarioRow(ctx, t, pool)

		fpID := formaDePagoSemanalID
		cmID := creditoEnMeses12ID
		res, err := writer.Aplicar(ctx, outbound.MicrosipVentaInput{
			Venta:                buildAplicarCreditoConFechaVenta(t, userID, fechaVenta),
			CajaID:               testCajaID,
			CajeroID:             testCajeroID,
			VendedorID:           testVendedorID,
			SucursalID:           testSucursalID,
			FormaCobroID:         formaCobroCreditoID,
			FormaDePagoID:        &fpID,
			CreditoEnMesesID:     &cmID,
			NumeroDeVendedoresID: numVendedores1ID,
		})
		require.NoError(t, err)

		_, engancheID := engancheDeLaVenta(ctx, t, q, res.DoctoPVID)

		antesTotal, antesCancelados := contarEnganchesDelCliente(ctx, t, q, engancheID)
		require.Equal(t, 1, antesTotal, "prerrequisito: un solo enganche antes de editar")
		require.Zero(t, antesCancelados)

		// El cobrador sale de la MISMA fuente que usaría el arreglo: la config
		// de zona. Leerlo aquí comprueba de paso que el dato existe.
		var cobradorDeLaZona int
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT COBRADOR_ID FROM MSP_CFG_ZONA_CAJA WHERE ZONA_CLIENTE_ID = ?`,
			testZonaID).Scan(&cobradorDeLaZona),
			"la config de zona debe tener cobrador: es de donde saldría el dato")
		require.Positive(t, cobradorDeLaZona)

		// La edición: ponerle el cobrador que le falta.
		_, err = q.ExecContext(ctx,
			`UPDATE DOCTOS_CC SET COBRADOR_ID = ? WHERE DOCTO_CC_ID = ?`,
			cobradorDeLaZona, engancheID)
		require.NoError(t, err, "modificar el documento no debe fallar")

		despuesTotal, despuesCancelados := contarEnganchesDelCliente(ctx, t, q, engancheID)

		assert.Equal(t, 1, despuesTotal,
			"la base NO duplica al modificar: si aparecieran dos, el cancelar+recrear de "+
				"producción sería de un trigger y no del cliente de Microsip")
		assert.Zero(t, despuesCancelados, "y el nuestro no se cancela solo")

		var cobrador *int
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT COBRADOR_ID FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`, engancheID).Scan(&cobrador))
		require.NotNil(t, cobrador)
		assert.Equal(t, cobradorDeLaZona, *cobrador,
			"el cobrador se puede escribir en el documento sin efectos colaterales")
	})
}
