//nolint:paralleltest // serial: cada prueba comparte una transacción que se revierte.
package desempenofb_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/desempeno/infra/desempenofb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/microsipseed"
)

func cuentaDe(cs []domain.CuentaUniverso, ventaID int) (domain.CuentaUniverso, bool) {
	for _, c := range cs {
		if c.VentaID == ventaID {
			return c, true
		}
	}
	return domain.CuentaUniverso{}, false
}

func TestCuentasRepo_VentaConSaldo(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "JUANA ANDRADE NÚÑEZ", zona)
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{
			Total: decimal.NewFromInt(8800), ArticuloID: microsipseed.ArticuloConAcento(t, q),
		})

		cs, err := desempenofb.NewCuentasRepo(pool).UniversoSemana(ctx, zona, time.Date(2026, 10, 9, 14, 46, 0, 0, time.UTC))
		require.NoError(t, err)
		c, ok := cuentaDe(cs, venta.CargoCCID)
		require.True(t, ok, "la venta con saldo está en el universo")
		assert.Equal(t, cliente, c.ClienteID)
		assert.Equal(t, "JUANA ANDRADE NÚÑEZ", c.ClienteNombre)
		assert.Equal(t, venta.DoctoPVID, c.DoctoPVID)
		assert.Equal(t, venta.Folio, c.Folio)
		assert.Equal(t, venta.ArticuloNombre, c.Producto, "ISO8859_1 con Ñ, sin doble decode")
		assert.Equal(t, "733.00", c.Parcialidad.StringFixed(2), "8800 / 12 meses, entero (LIBRES_CARGOS_CC)")
		assert.True(t, c.Saldo.Equal(decimal.NewFromInt(8800)))
	})
}

func TestCuentasRepo_SaldadaEntraSoloSiPagoEnLaSemana(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, domain.ConceptoCobranzaEnRuta)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "ROSARIO VILLA LÓPEZ", zona)
		total := decimal.NewFromInt(1200)
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{Total: total})
		pagoDia := time.Date(2024, 3, 20, 0, 0, 0, 0, time.UTC)
		microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: domain.ConceptoCobranzaEnRuta, Importe: total, Fecha: pagoDia,
		})
		repo := desempenofb.NewCuentasRepo(pool)

		cs, err := repo.UniversoSemana(ctx, zona, time.Date(2024, 3, 18, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		c, ok := cuentaDe(cs, venta.CargoCCID)
		require.True(t, ok, "saldada con pago dentro de la semana: sigue en el universo")
		assert.True(t, c.Saldo.IsZero())

		cs, err = repo.UniversoSemana(ctx, zona, time.Date(2024, 3, 25, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		_, ok = cuentaDe(cs, venta.CargoCCID)
		assert.False(t, ok, "saldada antes de la semana: fuera")
	})
}

func TestCuentasRepo_AislamientoPorZona(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		zonaA := microsipseed.PrimeraZona(t, q)
		zonaB := segundaZona(ctx, t, q, zonaA)
		ventaA := microsipseed.VentaCredito(t, q, microsipseed.ClienteEnZona(t, q, "BEATRIZ OCHOA PEÑA", zonaA), microsipseed.OpcionesVenta{})
		ventaB := microsipseed.VentaCredito(t, q, microsipseed.ClienteEnZona(t, q, "ANA MARÍA CRUZ", zonaB), microsipseed.OpcionesVenta{})

		cs, err := desempenofb.NewCuentasRepo(pool).UniversoSemana(ctx, zonaA, time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		_, ok := cuentaDe(cs, ventaA.CargoCCID)
		assert.True(t, ok, "control positivo: la venta de la zona A sí sale")
		_, ok = cuentaDe(cs, ventaB.CargoCCID)
		assert.False(t, ok, "la venta de la zona B no aparece en la zona A")
	})
}

// conceptoEnganche es el concepto de abono con que Microsip registra el
// enganche (venta_writer.go lo escribe así; categoria.go lo clasifica).
const conceptoEnganche = 24533

// La regla de "nueva" del teléfono (OrdenDeCobranza.kt:103) compara
// saldo == totalVenta − enganche. Si el repositorio dejara PrecioTotal o
// Enganche en cero, toda venta con enganche saldría como "no nueva" (o al
// revés) sin que nada se pusiera rojo; esta prueba lo fija.
func TestCuentasRepo_PrecioTotalYEngancheDeLaVenta(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, conceptoEnganche, domain.ConceptoCobranzaEnRuta)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "GUADALUPE TORRES MUÑOZ", zona)
		total := decimal.NewFromInt(8800)
		enganche := decimal.NewFromInt(1000)
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{Total: total, Enganche: enganche})
		microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: conceptoEnganche, Importe: enganche, Fecha: venta.Fecha,
		})
		repo := desempenofb.NewCuentasRepo(pool)
		inicio := time.Date(2026, 10, 9, 14, 46, 0, 0, time.UTC)

		cs, err := repo.UniversoSemana(ctx, zona, inicio)
		require.NoError(t, err)
		c, ok := cuentaDe(cs, venta.CargoCCID)
		require.True(t, ok)
		assert.Equal(t, "8800.00", c.PrecioTotal.StringFixed(2), "MSP_SALDOS_VENTAS.PRECIO_TOTAL")
		assert.Equal(t, "1000.00", c.Enganche.StringFixed(2), "LIBRES_CARGOS_CC.ENGANCHE")
		assert.Equal(t, "7800.00", c.Saldo.StringFixed(2), "el enganche ya restó del saldo")
		assert.True(t, c.EsNueva(), "saldo == total − enganche: nadie ha abonado")

		microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: domain.ConceptoCobranzaEnRuta, Importe: decimal.NewFromInt(150),
			Fecha: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC),
		})
		cs, err = repo.UniversoSemana(ctx, zona, inicio)
		require.NoError(t, err)
		c, ok = cuentaDe(cs, venta.CargoCCID)
		require.True(t, ok)
		assert.Equal(t, "7650.00", c.Saldo.StringFixed(2))
		assert.False(t, c.EsNueva(), "con un abono de cobranza ya no es nueva")
	})
}

func TestCuentasRepo_SinContratoEngancheEnCero(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "ROSA ELENA PÉREZ", zona)
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{})
		// Sin LIBRES_CARGOS_CC el teléfono toma enganche = 0 (VentaDto.kt
		// parseAmount): el LEFT JOIN no debe tumbar la fila ni la consulta.
		_, err := q.ExecContext(ctx, `DELETE FROM LIBRES_CARGOS_CC WHERE DOCTO_CC_ID = ?`, venta.CargoCCID)
		require.NoError(t, err)

		cs, err := desempenofb.NewCuentasRepo(pool).UniversoSemana(ctx, zona, time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		c, ok := cuentaDe(cs, venta.CargoCCID)
		require.True(t, ok)
		assert.True(t, c.Enganche.IsZero())
		assert.True(t, c.Parcialidad.IsZero())
		assert.True(t, c.EsNueva(), "8800 == 8800 − 0")
	})
}

// fijarFrecuencia apunta LIBRES_CARGOS_CC.FORMA_DE_PAGO del cargo al atributo
// del catálogo cuyo valor, sin distinguir mayúsculas, es frec. Toma el de ID
// más alto: en dev conviven "QUINCENAL" y "Quincenal", y el de minúsculas
// obliga a que el repositorio normalice a mayúsculas como el sync.
func fijarFrecuencia(ctx context.Context, t *testing.T, q firebird.Querier, cargoID int, frec string) {
	t.Helper()
	var atributo int
	require.NoError(t, q.QueryRowContext(ctx,
		`SELECT FIRST 1 LISTA_ATRIB_ID FROM LISTAS_ATRIBUTOS
		 WHERE UPPER(VALOR_DESPLEGADO) = ? ORDER BY LISTA_ATRIB_ID DESC`, frec).Scan(&atributo),
		"el catálogo LISTAS_ATRIBUTOS necesita la frecuencia %s", frec)
	_, err := q.ExecContext(ctx, `UPDATE LIBRES_CARGOS_CC SET FORMA_DE_PAGO = ? WHERE DOCTO_CC_ID = ?`, atributo, cargoID)
	require.NoError(t, err, "fijar FORMA_DE_PAGO")
}

// Los tres campos alimentan NumAtrasos (fórmula del teléfono). Si el
// repositorio los dejara vacíos, los atrasos saldrían mal en silencio.
func TestCuentasRepo_FrecuenciaFechaVentaYUltimoPago(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, domain.ConceptoCobranzaEnRuta)
		zona := microsipseed.PrimeraZona(t, q)
		casos := []struct {
			frec    string
			fecha   time.Time
			pagoDia time.Time
		}{
			{"SEMANAL", time.Date(2024, 3, 4, 0, 0, 0, 0, time.UTC), time.Date(2024, 3, 11, 0, 0, 0, 0, time.UTC)},
			{"QUINCENAL", time.Date(2024, 3, 6, 0, 0, 0, 0, time.UTC), time.Date(2024, 3, 21, 0, 0, 0, 0, time.UTC)},
			{"MENSUAL", time.Date(2024, 3, 8, 0, 0, 0, 0, time.UTC), time.Date(2024, 4, 8, 0, 0, 0, 0, time.UTC)},
		}
		ventas := make([]microsipseed.Venta, len(casos))
		for i, cs := range casos {
			cliente := microsipseed.ClienteEnZona(t, q, "MARÍA JOSÉ IBÁÑEZ "+cs.frec, zona)
			ventas[i] = microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{Fecha: cs.fecha})
			fijarFrecuencia(ctx, t, q, ventas[i].CargoCCID, cs.frec)
			microsipseed.AbonoAplicado(t, q, ventas[i], microsipseed.OpcionesAbono{
				ConceptoCCID: domain.ConceptoCobranzaEnRuta, Importe: decimal.NewFromInt(100), Fecha: cs.pagoDia,
			})
		}

		cs, err := desempenofb.NewCuentasRepo(pool).UniversoSemana(ctx, zona, time.Date(2026, 10, 9, 14, 46, 0, 0, time.UTC))
		require.NoError(t, err)
		for i, caso := range casos {
			c, ok := cuentaDe(cs, ventas[i].CargoCCID)
			require.True(t, ok, caso.frec)
			assert.Equal(t, caso.frec, c.FrecPago, "UPPER(LISTAS_ATRIBUTOS.VALOR_DESPLEGADO), como `frec_pago` del sync")
			require.NotNil(t, c.FechaVenta, "%s: DOCTOS_PV.FECHA existe y no puede quedar nil", caso.frec)
			// DOCTOS_PV.FECHA y DOCTOS_CC.FECHA son DATE: medianoche de CDMX (06:00Z).
			assert.Equal(t, caso.fecha.Add(6*time.Hour), *c.FechaVenta, caso.frec)
			require.NotNil(t, c.FechaUltPago, "%s: hubo un pago y FECHA_ULT_PAGO no puede quedar nil", caso.frec)
			assert.Equal(t, caso.pagoDia.Add(6*time.Hour), *c.FechaUltPago, caso.frec)
		}
	})
}

func TestCuentasRepo_SinPagosNiFrecuencia(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "ÁNGEL NÚÑEZ SOLÍS", zona)
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{})

		cs, err := desempenofb.NewCuentasRepo(pool).UniversoSemana(ctx, zona, time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		c, ok := cuentaDe(cs, venta.CargoCCID)
		require.True(t, ok)
		assert.Nil(t, c.FechaUltPago, "sin pagos: nil, no la fecha cero")
		// El teléfono toma SEMANAL cuando frec_pago llega vacío
		// (VentaDto.kt: frec_pago.ifBlank { SEMANAL }); el universo ya lo
		// trata así en su WHERE.
		assert.Equal(t, "SEMANAL", c.FrecPago)
		require.NotNil(t, c.FechaVenta)
	})
}
