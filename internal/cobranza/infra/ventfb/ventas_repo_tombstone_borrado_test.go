//nolint:misspell // Spanish vocabulary by project convention.
package ventfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// TestE2E_VentasRepo_SyncPorZona_TombstonePorBorradoFisico cubre el hueco que
// dejaron dos cambios que nadie cruzó:
//
//   - 83f7d68 (31/05) acotó la rama de cancelados a los cargos cuya
//     DOCTOS_CC.FECHA_HORA_CANCELACION cae dentro de la ventana, vía EXISTS.
//   - 4b8c233 (02/06, migración 000020) hizo que el BORRADO FÍSICO de un
//     DOCTOS_CC dejara lápida en MSP_SALDOS_VENTAS (CARGO_CANCELADO='S',
//     SALDO=0, FECHA_ULT_PAGO=NULL) en vez de borrar la fila del caché.
//
// Juntos se anulan: sin fila en DOCTOS_CC el EXISTS es falso, y la propia
// lápida dejó SALDO en 0 y FECHA_ULT_PAGO en NULL, así que las otras dos
// ramas del predicado tampoco la rescatan. La señal con la que el teléfono
// borra la venta nunca sale, y el cobrador arrastra una venta fantasma para
// siempre. Medido en producción: 29 lápidas huérfanas en 11 zonas.
//
// El paso 3 es un CONTROL POSITIVO deliberado: sin él, cualquier problema de
// fixture (cliente fuera de ruta, margen de reloj, zona equivocada) se lee
// como "el tombstone no viaja" y la prueba no prueba nada.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_VentasRepo_SyncPorZona_TombstonePorBorradoFisico(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)

		clienteID, zonaID := seedZonedCliente(t, q)

		importe := decimal.RequireFromString("3300.00")
		cargoID := insertCargoDoctosCC(t, q, clienteID, "TOMB-DEL", importe)

		// El margen de reloj del sync (syncClockSkewSeconds = 1 s) recorta el
		// borde superior de la página. Sin esta espera el control positivo de
		// abajo falla y la prueba deja de probar lo que dice probar.
		time.Sleep(2 * time.Second)

		repo := cobranzaventfb.NewVentasRepo(pool)
		desde := time.Now().Add(-7 * 24 * time.Hour)

		// ── Control positivo ──────────────────────────────────────────────
		pageViva, err := repo.SyncPorZona(ctx, zonaID, time.Time{}, 0, 5000, desde)
		require.NoError(t, err)
		if findVenta(pageViva.Items, cargoID) == nil {
			t.Fatalf("control positivo: la venta viva %d no viajó por el sync de la zona %d; "+
				"el fixture está mal y el resto de la prueba no probaría nada", cargoID, zonaID)
		}

		// ── Borrado físico en Microsip ────────────────────────────────────
		// Oficina elimina la venta: primero los importes (FK), después el
		// cargo. El trigger MSP_SALDOS_DOCTOS_CC_AD deja la lápida.
		_, err = q.ExecContext(ctx,
			`DELETE FROM IMPORTES_DOCTOS_CC WHERE DOCTO_CC_ID = ?`, cargoID)
		require.NoError(t, err, "DELETE IMPORTES_DOCTOS_CC")
		_, err = q.ExecContext(ctx,
			`DELETE FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`, cargoID)
		require.NoError(t, err, "DELETE DOCTOS_CC")

		// ── La lápida quedó escrita ───────────────────────────────────────
		var (
			cargoCancelado string
			saldo          decimal.Decimal
			fechaUltPago   *time.Time
		)
		err = q.QueryRowContext(ctx,
			`SELECT CARGO_CANCELADO, SALDO, FECHA_ULT_PAGO
			   FROM MSP_SALDOS_VENTAS WHERE DOCTO_CC_ID = ?`, cargoID).
			Scan(&cargoCancelado, &saldo, &fechaUltPago)
		require.NoError(t, err, "la fila del caché debe seguir ahí: el trigger deja lápida, no borra")
		require.Equal(t, "S", cargoCancelado, "el borrado físico debe dejar CARGO_CANCELADO='S'")
		require.True(t, saldo.IsZero(), "la lápida deja SALDO en 0; got=%s", saldo)
		require.Nil(t, fechaUltPago, "la lápida deja FECHA_ULT_PAGO en NULL")

		// Ya no queda fila en DOCTOS_CC: el EXISTS de la rama de cancelados
		// no puede rescatarla.
		var quedan int
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`, cargoID).Scan(&quedan))
		require.Zero(t, quedan, "prerequisito: el cargo ya no existe en DOCTOS_CC")

		// ── Lo que importa: la señal de borrado tiene que salir ───────────
		pageLapida, err := repo.SyncPorZona(ctx, zonaID, time.Time{}, 0, 5000, desde)
		require.NoError(t, err)

		v := findVenta(pageLapida.Items, cargoID)
		require.NotNil(t, v,
			"la lápida del cargo %d debe viajar: es la ÚNICA señal con la que el teléfono "+
				"borra una venta que oficina eliminó en Microsip", cargoID)
		assert.True(t, v.CargoCancelado(), "cargo_cancelado debe ser true")
		assert.True(t, v.Saldo().IsZero(), "la lápida viaja con saldo 0")
	})
}

// TestE2E_VentasRepo_ByIDs_TombstonePorBorradoFisico fija el mismo contrato en
// el canal de rescate: el teléfono pide por id el cargo que su inventario
// declaró faltante y tiene que recibir la lápida, no un conjunto vacío.
//
// Comparte la constante ventaStatusFilterConVentana con el sync, así que esta
// prueba es la guardia de que la paridad no se rompa por un atajo local
// (§4 de docs/COBRANZA-SYNC.md).
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_VentasRepo_ByIDs_TombstonePorBorradoFisico(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)

		clienteID, zonaID := seedZonedCliente(t, q)
		cargoID := insertCargoDoctosCC(t, q, clienteID, "TOMB-BID",
			decimal.RequireFromString("1800.00"))

		repo := cobranzaventfb.NewVentasRepo(pool)
		desde := time.Now().Add(-7 * 24 * time.Hour)

		// Control positivo: viva, by-ids la entrega.
		vivas, err := repo.ByIDs(ctx, zonaID, []int{cargoID}, desde)
		require.NoError(t, err)
		if len(vivas) != 1 {
			t.Fatalf("control positivo: by-ids no entregó la venta viva %d (got %d filas); "+
				"el fixture está mal y el resto de la prueba no probaría nada", cargoID, len(vivas))
		}

		_, err = q.ExecContext(ctx, `DELETE FROM IMPORTES_DOCTOS_CC WHERE DOCTO_CC_ID = ?`, cargoID)
		require.NoError(t, err)
		_, err = q.ExecContext(ctx, `DELETE FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`, cargoID)
		require.NoError(t, err)

		lapidas, err := repo.ByIDs(ctx, zonaID, []int{cargoID}, desde)
		require.NoError(t, err)
		require.Len(t, lapidas, 1,
			"by-ids debe entregar la lápida del cargo %d; si no, el reconciliador la ve "+
				"como fantasma, la pide, no la recibe y el ciclo se repite", cargoID)
		assert.True(t, lapidas[0].CargoCancelado())
	})
}
