package clientesfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/clientes/infra/clientesfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/microsipseed"
)

// TestClientesRepo_ObtenerPagoDetalle_PagoRecibidoYFotos: un pago que pasó por
// la app expone su UUID de MSP_PAGOS_RECIBIDOS y cuántas fotos tiene (spec
// desempeño §3.2.3); uno de oficina, null y 0.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestClientesRepo_ObtenerPagoDetalle_PagoRecibidoYFotos(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, conceptoCobranza)
		cliente := microsipseed.Cliente(t, q, "PAGO CON FOTOS PRUEBA Ñ")
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{})
		deLaApp := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: conceptoCobranza, Importe: decimal.NewFromInt(150),
		})
		deOficina := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: conceptoCobranza, Importe: decimal.NewFromInt(100),
		})

		pagoID := uuid.New()
		now := firebird.ToWallClock(time.Now().UTC())
		_, err := q.ExecContext(ctx,
			`INSERT INTO MSP_PAGOS_RECIBIDOS (ID, FECHA, DOCTO_CC_ID, ESTADO, INTENTOS, RECEIVED_AT, UPDATED_AT)
			 VALUES (?, ?, ?, 'A', 0, ?, ?)`,
			pagoID.String(), now, deLaApp.DoctoCCID, now, now)
		require.NoError(t, err)
		for range 2 {
			img := uuid.New()
			_, err = q.ExecContext(ctx,
				`INSERT INTO MSP_PAGOS_IMAGENES (ID, PAGO_ID, STORAGE_KIND, STORAGE_KEY, MIME, SIZE_BYTES,
				  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
				 VALUES (?, ?, 'FILESYSTEM', ?, 'image/jpeg', 1024, ?, ?, ?, ?)`,
				img.String(), pagoID.String(), "pagos/"+pagoID.String()+"/"+img.String()+".jpg",
				now, now, pagoID.String(), pagoID.String())
			require.NoError(t, err)
		}

		detalle, err := repo.ObtenerPagoDetalle(ctx, deLaApp.DoctoCCID)
		require.NoError(t, err)
		assert.Equal(t, pagoID.String(), detalle.PagoRecibidoID)
		assert.Equal(t, 2, detalle.NumFotos)
		assert.Equal(t, "app", detalle.Origen)

		oficina, err := repo.ObtenerPagoDetalle(ctx, deOficina.DoctoCCID)
		require.NoError(t, err)
		assert.Empty(t, oficina.PagoRecibidoID, "sin fila en MSP_PAGOS_RECIBIDOS")
		assert.Equal(t, 0, oficina.NumFotos)
	})
}
