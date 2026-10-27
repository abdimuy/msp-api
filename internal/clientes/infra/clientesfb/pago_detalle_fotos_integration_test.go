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

// capturaApp inserta una fila de MSP_PAGOS_RECIBIDOS ligada al abono con el id
// dado (impte nil = captura vieja, sin IMPTE_DOCTO_CC_ID) y numFotos
// comprobantes.
func capturaApp(ctx context.Context, t *testing.T, q firebird.Querier, id uuid.UUID, doctoCCID int, impte *int, numFotos int) {
	t.Helper()
	now := firebird.ToWallClock(time.Now().UTC())
	_, err := q.ExecContext(ctx,
		`INSERT INTO MSP_PAGOS_RECIBIDOS (ID, FECHA, DOCTO_CC_ID, IMPTE_DOCTO_CC_ID, ESTADO, INTENTOS, RECEIVED_AT, UPDATED_AT)
		 VALUES (?, ?, ?, ?, 'A', 0, ?, ?)`,
		id.String(), now, doctoCCID, impte, now, now)
	require.NoError(t, err)
	for range numFotos {
		img := uuid.New()
		_, err = q.ExecContext(ctx,
			`INSERT INTO MSP_PAGOS_IMAGENES (ID, PAGO_ID, STORAGE_KIND, STORAGE_KEY, MIME, SIZE_BYTES,
			  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
			 VALUES (?, ?, 'FILESYSTEM', ?, 'image/jpeg', 1024, ?, ?, ?, ?)`,
			img.String(), id.String(), "pagos/"+id.String()+"/"+img.String()+".jpg",
			now, now, id.String(), id.String())
		require.NoError(t, err)
	}
}

// TestClientesRepo_ObtenerPagoDetalle_PagoRecibidoMismaReglaQueActividad fija
// que el detalle liga la captura con la MISMA regla que la actividad de
// desempeño (desempenofb/actividades_repo.go, queryPagosAplicados): primero
// la captura ligada por IMPTE_DOCTO_CC_ID y, sólo si no hay, la vieja ligada
// por DOCTO_CC_ID con IMPTE_DOCTO_CC_ID nulo. Así actividad y detalle
// muestran las mismas fotos. La captura vieja lleva el UUID MENOR a propósito:
// con la regla anterior (MIN por DOCTO_CC_ID) ganaba ella.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestClientesRepo_ObtenerPagoDetalle_PagoRecibidoMismaReglaQueActividad(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, conceptoCobranza)
		cliente := microsipseed.Cliente(t, q, "DOS CAPTURAS PRUEBA ÁNGEL")
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{})
		abono := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: conceptoCobranza, Importe: decimal.NewFromInt(200),
		})
		soloVieja := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: conceptoCobranza, Importe: decimal.NewFromInt(50),
		})

		var impte int
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT MIN(IMPTE_DOCTO_CC_ID) FROM IMPORTES_DOCTOS_CC WHERE DOCTO_CC_ID = ? AND TIPO_IMPTE = 'R'`,
			abono.DoctoCCID).Scan(&impte))

		ligada := uuid.MustParse("ffffffff-0000-4000-8000-000000000001")
		vieja := uuid.MustParse("00000000-0000-4000-8000-000000000002")
		capturaApp(ctx, t, q, ligada, abono.DoctoCCID, &impte, 1)
		capturaApp(ctx, t, q, vieja, abono.DoctoCCID, nil, 2)
		soloViejaID := uuid.MustParse("00000000-0000-4000-8000-000000000003")
		capturaApp(ctx, t, q, soloViejaID, soloVieja.DoctoCCID, nil, 3)

		detalle, err := repo.ObtenerPagoDetalle(ctx, abono.DoctoCCID)
		require.NoError(t, err)
		assert.Equal(t, ligada.String(), detalle.PagoRecibidoID, "gana la captura ligada por IMPTE_DOCTO_CC_ID")
		assert.Equal(t, 1, detalle.NumFotos, "las fotos son las de esa misma captura")

		respaldo, err := repo.ObtenerPagoDetalle(ctx, soloVieja.DoctoCCID)
		require.NoError(t, err)
		assert.Equal(t, soloViejaID.String(), respaldo.PagoRecibidoID, "sin captura por IMPTE, la vieja por DOCTO_CC_ID")
		assert.Equal(t, 3, respaldo.NumFotos)
	})
}
