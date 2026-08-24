//nolint:misspell // Spanish vocabulary by project convention.
package ventfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/infra/microsip"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// Estas pruebas fijan el hallazgo del 2026-08-21 sobre los enganches
// duplicados: NO los produce nadie capturando a mano, ni un trigger que
// cancele y recree. Los produce un sistema externo llamado MGGENCOBRO, que
// lleva años instalado en la misma base y del que sólo se ven tres piezas:
//
//  1. Un trigger AFTER INSERT sobre DOCTOS_PV (`MGGENCOBRO_DOCTOS_PV_AI0`) que
//     no cobra nada: ENCOLA la venta en `MGGENCOBRO_VEMOST_PROCESADAS` con
//     PROCESADA='N'.
//  2. Un programa externo —no está en la base, no hay procedimiento que lo
//     haga— que vacía esa cola, genera su propio enganche y lo apunta en
//     `MGGENCOBRO_REL_VEMOST_CR`.
//  3. Su configuración: `MGGENCOBRO_CONFIG` tiene una sola fila,
//     ConceptoCredito=24533. El nombre miente: 24533 es "Enganche".
//
// La condición del trigger es `TIPO_DOCTO='V' AND ESTATUS='N'`, que es
// exactamente lo que `venta_writer` fija como literales. Por eso TODA venta
// nuestra entra en su cola de trabajo.
//
// Que coincidan los literales no basta: hay que ver el trigger disparar por
// nuestra ruta. Eso es lo que prueba `TestE2E_VentaNuestra_EntraEnLaColaDeMGGENCOBRO`.

// TestE2E_VentaNuestra_NoEntraEnLaColaDeMGGENCOBRO vigila que el apagado siga
// puesto: aplicar una venta real NO debe dejarla encolada para el sistema
// externo.
//
// Antes de la migración 000060 esta prueba medía lo contrario, y pasaba: el
// trigger encolaba una fila con PROCESADA='N' por cada venta nuestra. Ese fue
// el control positivo que demostró el mecanismo.
//
// Si se pone en rojo, alguien reactivó `MGGENCOBRO_DOCTOS_PV_AI0` —el
// instalador del tercero, o un `down` de la 000060— y los pares de enganches
// volverán con él.
//
//nolint:paralleltest // serial: comparte la tx con rollback.
func TestE2E_VentaNuestra_NoEntraEnLaColaDeMGGENCOBRO(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	writer := microsip.NewVentaWriter(pool)
	fechaVenta := time.Date(2025, 3, 15, 12, 0, 0, 0, time.UTC)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		seedClienteFixture(t, q)
		userID := seedUsuarioRow(ctx, t, pool)

		// El DOCTO_PV_ID lo genera `Aplicar`, así que es nuevo por
		// construcción: cualquier fila que la cola traiga con ese id sólo
		// puede haberla puesto este INSERT. No hay un "antes" que consultar.
		res, err := aplicarVentaDePrueba(ctx, t, q, writer, userID, fechaVenta)
		require.NoError(t, err)

		var encoladas int
		// Nullable a propósito: sin filas, MIN(PROCESADA) es NULL. Escanear
		// eso en un string falla, y el fallo taparía el resultado real.
		var procesada *string
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT COUNT(*), MIN(PROCESADA) FROM MGGENCOBRO_VEMOST_PROCESADAS WHERE DOCTO_PV_ID = ?`,
			res.DoctoPVID).Scan(&encoladas, &procesada))

		assert.Zero(t, encoladas,
			"con el trigger apagado la venta no entra en la cola del sistema externo")
		assert.Nil(t, procesada, "no hay fila, así que no hay estado que leer")
	})
}

// TestMGGENCOBRO_ElTriggerSigueApagado mira el catálogo directamente, que es
// la comprobación más barata y la que no depende de aplicar una venta.
//
// Tolera que el trigger no exista: si algún día desinstalan MGGENCOBRO del
// todo, eso es MÁS apagado, no menos, y la prueba no tiene por qué estorbar.
// Lo único que no se acepta es que exista y esté activo.
//
//nolint:paralleltest // serial: sólo lee catálogo, pero comparte pool.
func TestMGGENCOBRO_ElTriggerSigueApagado(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)

		var existe, activo int
		require.NoError(t, q.QueryRowContext(ctx, `
			SELECT COUNT(*),
			       COALESCE(SUM(CASE WHEN COALESCE(RDB$TRIGGER_INACTIVE, 0) = 0 THEN 1 ELSE 0 END), 0)
			FROM RDB$TRIGGERS WHERE RDB$TRIGGER_NAME = 'MGGENCOBRO_DOCTOS_PV_AI0'`).
			Scan(&existe, &activo))

		if existe == 0 {
			t.Log("MGGENCOBRO ya no está en la base: desinstalado por completo")
			return
		}
		assert.Zero(t, activo,
			"el trigger de MGGENCOBRO debe seguir INACTIVE (migración 000060): "+
				"activo vuelve a encolar nuestras ventas y a duplicar el enganche 24533")
	})
}

// aplicarVentaDePrueba aplica una venta a crédito con los fixtures del paquete.
func aplicarVentaDePrueba(
	ctx context.Context, t *testing.T, q firebird.Querier,
	writer *microsip.VentaWriter, userID uuid.UUID, fechaVenta time.Time,
) (outbound.MicrosipVentaResult, error) {
	t.Helper()

	var cobradorDeLaZona int
	require.NoError(t, q.QueryRowContext(ctx,
		`SELECT COBRADOR_ID FROM MSP_CFG_ZONA_CAJA WHERE ZONA_CLIENTE_ID = ?`,
		testZonaID).Scan(&cobradorDeLaZona))

	fpID := formaDePagoSemanalID
	cmID := creditoEnMeses12ID
	return writer.Aplicar(ctx, outbound.MicrosipVentaInput{
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
}
