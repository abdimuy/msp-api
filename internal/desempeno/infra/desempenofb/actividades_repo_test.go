//nolint:paralleltest // serial: cada prueba comparte una transacción que se revierte.
package desempenofb_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/desempeno/infra/desempenofb"
	"github.com/abdimuy/msp-api/internal/desempeno/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/microsipseed"
)

// Fecha de los abonos sembrados. DOCTOS_CC.FECHA es DATE: el abono de oficina
// queda a las 00:00 de CDMX (06:00Z).
var diaAbono = time.Date(2024, 3, 20, 0, 0, 0, 0, time.UTC)

func ventanaDelDia() (time.Time, time.Time) {
	return time.Date(2024, 3, 20, 0, 0, 0, 0, time.UTC), time.Date(2024, 3, 21, 12, 0, 0, 0, time.UTC)
}

func delCliente(acts []domain.Actividad, clienteID int) []domain.Actividad {
	var out []domain.Actividad
	for _, a := range acts {
		if a.ClienteID == clienteID {
			out = append(out, a)
		}
	}
	return out
}

func TestActividadesRepo_PagoDeOficina(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, domain.ConceptoCobranzaEnRuta)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "JUANA ANDRADE NÚÑEZ", zona)
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{})
		abono := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: domain.ConceptoCobranzaEnRuta, Importe: decimal.NewFromInt(150), Fecha: diaAbono,
			ConCobrador: true, ConFormaCobro: true, FormaCobroID: domain.FormaCobroEfectivo,
		})

		desde, hasta := ventanaDelDia()
		acts, err := desempenofb.NewActividadesRepo(pool).ActividadesZona(ctx, zona, desde, hasta)
		require.NoError(t, err)
		mias := delCliente(acts, cliente)
		require.Len(t, mias, 1)
		a := mias[0]
		assert.Equal(t, domain.IDPagoAplicado(impteDe(ctx, t, q, abono.DoctoCCID)), a.ID)
		assert.Equal(t, domain.TipoPago, a.Tipo)
		assert.Equal(t, "JUANA ANDRADE NÚÑEZ", a.ClienteNombre, "ISO8859_1 transliterado, sin doble decode")
		assert.Equal(t, venta.CargoCCID, a.VentaID)
		assert.Equal(t, venta.Folio, a.Folio)
		assert.Equal(t, venta.DoctoPVID, a.DoctoPVID, "contrato v2 docto_pv_id")
		assert.Equal(t, domain.OrigenOficina, a.RegistradoPor.Origen)
		assert.Equal(t, abono.CobradorID, a.RegistradoPor.CobradorID)
		assert.Nil(t, a.RecibidoAt)
		require.NotNil(t, a.Pago)
		assert.True(t, a.Pago.Importe.Equal(decimal.NewFromInt(150)))
		assert.Equal(t, domain.FormaCobroEfectivo, a.Pago.FormaCobroID)
		assert.True(t, a.Pago.Aplicado)
		assert.False(t, a.Pago.Rechazado)
		assert.Equal(t, abono.DoctoCCID, a.Pago.DoctoCCID)
		assert.Empty(t, a.Pago.PagoRecibidoID)
		assert.Equal(t, 0, a.NumFotos)
		assert.Equal(t, time.Date(2024, 3, 20, 6, 0, 0, 0, time.UTC), a.Hora, "medianoche de CDMX en UTC")
	})
}

func TestActividadesRepo_PagoDeLaAppConFotos(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, domain.ConceptoCobranzaEnRuta)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "ROSARIO VILLA LÓPEZ", zona)
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{})
		abono := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: domain.ConceptoCobranzaEnRuta, Importe: decimal.NewFromInt(200), Fecha: diaAbono,
			ConFormaCobro: true, FormaCobroID: domain.FormaCobroTransferencia,
		})
		usuarioID, uid := usuarioSembrado(ctx, t, q)
		pagoID := uuid.New()
		capturado := time.Date(2024, 3, 20, 16, 31, 0, 0, time.UTC)
		sembrarPagoRecibido(ctx, t, q, pagoRecibidoSembrado{
			id: pagoID, impteID: impteDe(ctx, t, q, abono.DoctoCCID), doctoCCID: abono.DoctoCCID,
			cargoID: venta.CargoCCID, clienteID: cliente, cobradorID: 11502,
			importe: decimal.NewFromInt(200), formaCobroID: domain.FormaCobroTransferencia,
			conceptoID: domain.ConceptoCobranzaEnRuta, fecha: capturado, recibido: capturado.Add(3 * time.Hour),
			estado: "A", createdBy: usuarioID,
		})
		sembrarImagenPago(ctx, t, q, pagoID)
		sembrarImagenPago(ctx, t, q, pagoID)

		desde, hasta := ventanaDelDia()
		acts, err := desempenofb.NewActividadesRepo(pool).ActividadesZona(ctx, zona, desde, hasta)
		require.NoError(t, err)
		mias := delCliente(acts, cliente)
		require.Len(t, mias, 1, "sin fan-out por el JOIN a MSP_PAGOS_RECIBIDOS ni por las fotos")
		a := mias[0]
		assert.Equal(t, domain.OrigenApp, a.RegistradoPor.Origen)
		assert.Equal(t, uid, a.RegistradoPor.UsuarioUID, "CREATED_BY → MSP_USUARIOS.FIREBASE_UID")
		assert.Equal(t, 11502, a.RegistradoPor.CobradorID)
		assert.Equal(t, "JUAN CARLOS MENDEZ", a.RegistradoPor.Nombre)
		require.NotNil(t, a.RecibidoAt)
		assert.Equal(t, capturado.Add(3*time.Hour), *a.RecibidoAt)
		assert.True(t, a.CapturaTardia(), "3 h entre captura y llegada")
		assert.Equal(t, capturado, a.Hora, "la hora real del cobro vive en MSP_PAGOS_RECIBIDOS (COBRANZA-SYNC.md §7.5)")
		assert.Equal(t, pagoID.String(), a.Pago.PagoRecibidoID)
		assert.Equal(t, 2, a.NumFotos)
		assert.Equal(t, domain.FormaCobroTransferencia, a.Pago.FormaCobroID)
	})
}

func TestActividadesRepo_CondonacionYConceptosExcluidos(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		const cobroEnMostrador = 155
		microsipseed.RequiereConceptos(t, q, domain.ConceptoCondonaciones, cobroEnMostrador)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "MARTHA ELENA RUIZ", zona)
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{})
		microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: domain.ConceptoCondonaciones, Importe: decimal.NewFromInt(50), Fecha: diaAbono,
			ConFormaCobro: true, FormaCobroID: domain.FormaCobroCondonacion,
		})
		microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: cobroEnMostrador, Importe: decimal.NewFromInt(300), Fecha: diaAbono,
		})

		desde, hasta := ventanaDelDia()
		acts, err := desempenofb.NewActividadesRepo(pool).ActividadesZona(ctx, zona, desde, hasta)
		require.NoError(t, err)
		mias := delCliente(acts, cliente)
		require.Len(t, mias, 1, "el cobro en mostrador (155) no es actividad de ruta")
		assert.Equal(t, domain.TipoCondonacion, mias[0].Tipo)
	})
}

func TestActividadesRepo_PagoPendienteRechazado(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "PEDRO SALINAS GÓMEZ", zona)
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{})
		usuarioID, uid := usuarioSembrado(ctx, t, q)
		pagoID := uuid.New()
		capturado := time.Date(2024, 3, 20, 18, 0, 0, 0, time.UTC)
		sembrarPagoRecibido(ctx, t, q, pagoRecibidoSembrado{
			id: pagoID, cargoID: venta.CargoCCID, clienteID: cliente, cobradorID: 11502,
			importe: decimal.NewFromInt(80), formaCobroID: domain.FormaCobroEfectivo,
			conceptoID: domain.ConceptoCobranzaEnRuta, fecha: capturado, recibido: capturado,
			estado: "P", intentos: 2, createdBy: usuarioID,
		})

		desde, hasta := ventanaDelDia()
		acts, err := desempenofb.NewActividadesRepo(pool).ActividadesZona(ctx, zona, desde, hasta)
		require.NoError(t, err)
		mias := delCliente(acts, cliente)
		require.Len(t, mias, 1)
		a := mias[0]
		assert.Equal(t, domain.IDPagoPendiente(pagoID.String()), a.ID)
		assert.Equal(t, venta.CargoCCID, a.VentaID)
		assert.Equal(t, venta.DoctoPVID, a.DoctoPVID)
		assert.Equal(t, uid, a.RegistradoPor.UsuarioUID)
		assert.False(t, a.Pago.Aplicado)
		assert.True(t, a.Pago.Rechazado, "INTENTOS > 0")
		assert.Equal(t, capturado, a.Hora)
		assert.InDelta(t, 18.4617, *a.Lat, 1e-9)
	})
}

func TestActividadesRepo_VisitaConFotoYAislamientoPorZona(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		zonaA := microsipseed.PrimeraZona(t, q)
		zonaB := segundaZona(ctx, t, q, zonaA)
		clienteA := microsipseed.ClienteEnZona(t, q, "LUCÍA FERNÁNDEZ ORTIZ", zonaA)
		clienteB := microsipseed.ClienteEnZona(t, q, "ANA MARÍA CRUZ", zonaB)
		venta := microsipseed.VentaCredito(t, q, clienteA, microsipseed.OpcionesVenta{})
		visita := uuid.New()
		cuando := time.Date(2024, 3, 20, 17, 0, 0, 0, time.UTC)
		sembrarVisita(ctx, t, q, visitaSembrada{
			id: visita, clienteID: clienteA, zonaID: zonaA, ventaID: venta.CargoCCID, cobradorID: 11502,
			tipo: "No se encontraba", fecha: cuando, lat: 18.4617, lng: -97.3928,
		})
		sembrarImagenVisita(ctx, t, q, visita)
		sembrarVisita(ctx, t, q, visitaSembrada{
			id: uuid.New(), clienteID: clienteB, zonaID: zonaB, cobradorID: 11502,
			tipo: "No se encontraba", fecha: cuando,
		})

		desde, hasta := ventanaDelDia()
		acts, err := desempenofb.NewActividadesRepo(pool).ActividadesZona(ctx, zonaA, desde, hasta)
		require.NoError(t, err)
		assert.Empty(t, delCliente(acts, clienteB), "la visita de la zona B no aparece en la zona A")
		mias := delCliente(acts, clienteA)
		require.Len(t, mias, 1)
		a := mias[0]
		assert.Equal(t, domain.IDVisita(visita.String()), a.ID)
		assert.Equal(t, domain.TipoVisita, a.Tipo)
		assert.Equal(t, venta.CargoCCID, a.VentaID)
		assert.Equal(t, venta.Folio, a.Folio)
		assert.Equal(t, venta.DoctoPVID, a.DoctoPVID)
		assert.Equal(t, 1, a.NumFotos)
		assert.Equal(t, "JUAN CARLOS MÉNDEZ", a.RegistradoPor.Nombre, "NONE con bytes UTF-8, sin Win1252")
		assert.Equal(t, 11502, a.RegistradoPor.CobradorID)
		assert.Empty(t, a.RegistradoPor.UsuarioUID)
		require.NotNil(t, a.Visita)
		assert.Equal(t, "Vecina dice que regresa el lunes", a.Visita.Nota)
		assert.Equal(t, cuando, a.Hora)
		assert.True(t, a.ConGPS())
	})
}

func TestActividadesRepo_RangoInclusivo(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "SILVIA CARRANZA MEÑACA", zona)
		desde := time.Date(2024, 3, 20, 14, 0, 0, 0, time.UTC)
		hasta := time.Date(2024, 3, 20, 15, 0, 0, 0, time.UTC)
		for _, h := range []time.Time{desde.Add(-time.Second), desde, hasta, hasta.Add(time.Second)} {
			sembrarVisita(ctx, t, q, visitaSembrada{
				id: uuid.New(), clienteID: cliente, zonaID: zona, cobradorID: 11502,
				tipo: "Casa cerrada con candado", fecha: h,
			})
		}
		acts, err := desempenofb.NewActividadesRepo(pool).ActividadesZona(ctx, zona, desde, hasta)
		require.NoError(t, err)
		mias := delCliente(acts, cliente)
		require.Len(t, mias, 2, "entran los dos bordes y nada más")
		for _, a := range mias {
			assert.False(t, a.ConGPS(), "0,0 es el centinela de sin GPS")
		}
	})
}

func TestActividadesRepo_PagosDeOtraZonaNoAparecen(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, domain.ConceptoCobranzaEnRuta)
		zonaA := microsipseed.PrimeraZona(t, q)
		zonaB := segundaZona(ctx, t, q, zonaA)
		clienteA := microsipseed.ClienteEnZona(t, q, "BEATRIZ OCHOA PEÑA", zonaA)
		clienteB := microsipseed.ClienteEnZona(t, q, "ANA MARÍA CRUZ", zonaB)
		usuarioID, _ := usuarioSembrado(ctx, t, q)
		capturado := time.Date(2024, 3, 20, 18, 0, 0, 0, time.UTC)
		for _, c := range []int{clienteA, clienteB} {
			venta := microsipseed.VentaCredito(t, q, c, microsipseed.OpcionesVenta{})
			microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
				ConceptoCCID: domain.ConceptoCobranzaEnRuta, Importe: decimal.NewFromInt(150), Fecha: diaAbono,
				ConFormaCobro: true, FormaCobroID: domain.FormaCobroEfectivo,
			})
			sembrarPagoRecibido(ctx, t, q, pagoRecibidoSembrado{
				id: uuid.New(), cargoID: venta.CargoCCID, clienteID: c, cobradorID: 11502,
				importe: decimal.NewFromInt(80), formaCobroID: domain.FormaCobroEfectivo,
				conceptoID: domain.ConceptoCobranzaEnRuta, fecha: capturado, recibido: capturado,
				estado: "P", createdBy: usuarioID,
			})
		}

		desde, hasta := ventanaDelDia()
		acts, err := desempenofb.NewActividadesRepo(pool).ActividadesZona(ctx, zonaA, desde, hasta)
		require.NoError(t, err)
		assert.Len(t, delCliente(acts, clienteA), 2, "control positivo: el aplicado y el pendiente de la zona A sí salen")
		assert.Empty(t, delCliente(acts, clienteB), "ni el pago aplicado ni el pendiente de la zona B aparecen en la zona A")
	})
}

// Un pago de la app que ya llegó a Microsip pero cuya fila de
// MSP_PAGOS_RECIBIDOS sigue en 'P' (el worker no alcanzó a marcarla) no puede
// contar dos veces: la fila aplicada manda. Se cubren las dos ligas: por
// IMPTE_DOCTO_CC_ID y, para capturas viejas sin él, por DOCTO_CC_ID.
func TestActividadesRepo_AplicadoYPendienteNoDuplica(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, domain.ConceptoCobranzaEnRuta)
		zona := microsipseed.PrimeraZona(t, q)
		usuarioID, _ := usuarioSembrado(ctx, t, q)
		capturado := time.Date(2024, 3, 20, 16, 0, 0, 0, time.UTC)
		// "impte": solo IMPTE_DOCTO_CC_ID (DOCTO_CC_ID nulo); "docto": solo
		// DOCTO_CC_ID (captura vieja); "ambas": la fila normal.
		for _, liga := range []string{"impte", "docto", "ambas"} {
			cliente := microsipseed.ClienteEnZona(t, q, "TERESA ÁVILA RÍOS "+liga, zona)
			venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{})
			abono := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
				ConceptoCCID: domain.ConceptoCobranzaEnRuta, Importe: decimal.NewFromInt(150), Fecha: diaAbono,
				ConFormaCobro: true, FormaCobroID: domain.FormaCobroEfectivo,
			})
			pagoID := uuid.New()
			sembrarPagoRecibido(ctx, t, q, pagoRecibidoSembrado{
				id: pagoID, impteID: impteDe(ctx, t, q, abono.DoctoCCID), doctoCCID: abono.DoctoCCID,
				cargoID: venta.CargoCCID, clienteID: cliente, cobradorID: 11502,
				importe: decimal.NewFromInt(150), formaCobroID: domain.FormaCobroEfectivo,
				conceptoID: domain.ConceptoCobranzaEnRuta, fecha: capturado, recibido: capturado,
				estado: "P", createdBy: usuarioID,
			})
			switch liga {
			case "docto":
				_, err := q.ExecContext(ctx,
					`UPDATE MSP_PAGOS_RECIBIDOS SET IMPTE_DOCTO_CC_ID = NULL WHERE ID = ?`, pagoID.String())
				require.NoError(t, err)
			case "impte":
				_, err := q.ExecContext(ctx,
					`UPDATE MSP_PAGOS_RECIBIDOS SET DOCTO_CC_ID = NULL WHERE ID = ?`, pagoID.String())
				require.NoError(t, err)
			}

			desde, hasta := ventanaDelDia()
			acts, err := desempenofb.NewActividadesRepo(pool).ActividadesZona(ctx, zona, desde, hasta)
			require.NoError(t, err)
			mias := delCliente(acts, cliente)
			require.Len(t, mias, 1, "%s: aplicado + pendiente del mismo pago = una sola actividad", liga)
			assert.True(t, mias[0].Pago.Aplicado, liga)
			assert.Equal(t, domain.OrigenApp, mias[0].RegistradoPor.Origen, "%s: la liga atribuye el pago a la app", liga)
			assert.Equal(t, pagoID.String(), mias[0].Pago.PagoRecibidoID, liga)
		}
	})
}

// Capturas viejas de la app quedaron ligadas solo por DOCTO_CC_ID (sin
// IMPTE_DOCTO_CC_ID): no deben salir como "oficina".
func TestActividadesRepo_PagoViejoDeLaAppLigadoPorDocto(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, domain.ConceptoCobranzaEnRuta)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "JOSÉ LUIS ARAGÓN", zona)
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{})
		abono := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: domain.ConceptoCobranzaEnRuta, Importe: decimal.NewFromInt(200), Fecha: diaAbono,
			ConFormaCobro: true, FormaCobroID: domain.FormaCobroEfectivo,
		})
		usuarioID, uid := usuarioSembrado(ctx, t, q)
		pagoID := uuid.New()
		capturado := time.Date(2024, 3, 20, 15, 0, 0, 0, time.UTC)
		sembrarPagoRecibido(ctx, t, q, pagoRecibidoSembrado{
			id: pagoID, impteID: impteDe(ctx, t, q, abono.DoctoCCID), doctoCCID: abono.DoctoCCID,
			cargoID: venta.CargoCCID, clienteID: cliente, cobradorID: 11502,
			importe: decimal.NewFromInt(200), formaCobroID: domain.FormaCobroEfectivo,
			conceptoID: domain.ConceptoCobranzaEnRuta, fecha: capturado, recibido: capturado,
			estado: "A", createdBy: usuarioID,
		})
		_, err := q.ExecContext(ctx, `UPDATE MSP_PAGOS_RECIBIDOS SET IMPTE_DOCTO_CC_ID = NULL WHERE ID = ?`, pagoID.String())
		require.NoError(t, err)

		desde, hasta := ventanaDelDia()
		acts, err := desempenofb.NewActividadesRepo(pool).ActividadesZona(ctx, zona, desde, hasta)
		require.NoError(t, err)
		mias := delCliente(acts, cliente)
		require.Len(t, mias, 1, "sin duplicar")
		assert.Equal(t, domain.OrigenApp, mias[0].RegistradoPor.Origen)
		assert.Equal(t, uid, mias[0].RegistradoPor.UsuarioUID)
		assert.Equal(t, pagoID.String(), mias[0].Pago.PagoRecibidoID)
		assert.Equal(t, capturado, mias[0].Hora)
	})
}

func TestActividadesRepo_AbonoCanceladoNoAparece(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, domain.ConceptoCobranzaEnRuta)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "MARCO ANTONIO GÜEMES", zona)
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{})
		vivo := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: domain.ConceptoCobranzaEnRuta, Importe: decimal.NewFromInt(100), Fecha: diaAbono,
		})
		cancelado := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: domain.ConceptoCobranzaEnRuta, Importe: decimal.NewFromInt(300), Fecha: diaAbono,
		})
		cancelarAbono(ctx, t, q, cancelado.DoctoCCID)

		desde, hasta := ventanaDelDia()
		acts, err := desempenofb.NewActividadesRepo(pool).ActividadesZona(ctx, zona, desde, hasta)
		require.NoError(t, err)
		mias := delCliente(acts, cliente)
		require.Len(t, mias, 1, "el abono cancelado no es actividad")
		assert.Equal(t, vivo.DoctoCCID, mias[0].Pago.DoctoCCID)
	})
}

func TestActividadesRepo_PagosEnElBordeHastaEntran(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, domain.ConceptoCobranzaEnRuta)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "ROCÍO PÉREZ IBARRA", zona)
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{})
		microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: domain.ConceptoCobranzaEnRuta, Importe: decimal.NewFromInt(100), Fecha: diaAbono,
		})
		// DOCTOS_CC.FECHA es DATE: el aplicado queda a las 00:00 CDMX = 06:00Z.
		hasta := time.Date(2024, 3, 20, 6, 0, 0, 0, time.UTC)
		usuarioID, _ := usuarioSembrado(ctx, t, q)
		sembrarPagoRecibido(ctx, t, q, pagoRecibidoSembrado{
			id: uuid.New(), cargoID: venta.CargoCCID, clienteID: cliente, cobradorID: 11502,
			importe: decimal.NewFromInt(80), formaCobroID: domain.FormaCobroEfectivo,
			conceptoID: domain.ConceptoCobranzaEnRuta, fecha: hasta, recibido: hasta,
			estado: "P", createdBy: usuarioID,
		})

		acts, err := desempenofb.NewActividadesRepo(pool).ActividadesZona(ctx, zona, hasta.Add(-time.Hour), hasta)
		require.NoError(t, err)
		mias := delCliente(acts, cliente)
		require.Len(t, mias, 2, "el aplicado y el pendiente exactamente en `hasta` entran")
		for _, a := range mias {
			assert.Equal(t, hasta, a.Hora)
		}
	})
}

// La caché MSP_PAGOS_VENTAS puede ir atrasada respecto a Microsip: importe,
// concepto, cancelado y aplicado se toman de IMPORTES_DOCTOS_CC/DOCTOS_CC
// cuando existen (misma regla que selectPagoColsP del sync).
func TestActividadesRepo_MicrosipMandaSobreLaCacheDesfasada(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, domain.ConceptoCobranzaEnRuta)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "IGNACIO ZÚÑIGA LEÓN", zona)
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{})
		abono := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: domain.ConceptoCobranzaEnRuta, Importe: decimal.NewFromInt(150), Fecha: diaAbono,
			ConFormaCobro: true, FormaCobroID: domain.FormaCobroEfectivo,
		})
		const cobroEnMostrador = 155
		_, err := q.ExecContext(ctx,
			`UPDATE MSP_PAGOS_VENTAS SET IMPORTE = 999, IMPUESTO = 0, CONCEPTO_CC_ID = ?, CANCELADO = 'S', APLICADO = 'N'
			 WHERE IMPTE_DOCTO_CC_ID = ?`, cobroEnMostrador, impteDe(ctx, t, q, abono.DoctoCCID))
		require.NoError(t, err, "desfasar la caché")

		desde, hasta := ventanaDelDia()
		acts, err := desempenofb.NewActividadesRepo(pool).ActividadesZona(ctx, zona, desde, hasta)
		require.NoError(t, err)
		mias := delCliente(acts, cliente)
		require.Len(t, mias, 1, "Microsip dice vivo y de cobranza en ruta: aparece")
		a := mias[0]
		assert.Equal(t, "150.00", a.Pago.Importe.StringFixed(2), "importe de IMPORTES_DOCTOS_CC, no de la caché")
		assert.Equal(t, domain.TipoPago, a.Tipo, "concepto de DOCTOS_CC")
		assert.True(t, a.Pago.Aplicado, "APLICADO de IMPORTES_DOCTOS_CC")
	})
}

// Una fila de MSP_PAGOS_RECIBIDOS sin cliente o sin importe (columnas
// nullables desde la migración 000016) no tumba la respuesta: se descarta y
// el resto sale.
func TestActividadesRepo_PendienteIncompletoSeDescarta(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "EUGENIA MUÑIZ ROBLES", zona)
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{})
		usuarioID, _ := usuarioSembrado(ctx, t, q)
		capturado := time.Date(2024, 3, 20, 17, 0, 0, 0, time.UTC)
		ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
		for _, id := range ids {
			sembrarPagoRecibido(ctx, t, q, pagoRecibidoSembrado{
				id: id, cargoID: venta.CargoCCID, clienteID: cliente, cobradorID: 11502,
				importe: decimal.NewFromInt(80), formaCobroID: domain.FormaCobroEfectivo,
				conceptoID: domain.ConceptoCobranzaEnRuta, fecha: capturado, recibido: capturado,
				estado: "P", createdBy: usuarioID,
			})
		}
		_, err := q.ExecContext(ctx, `UPDATE MSP_PAGOS_RECIBIDOS SET CLIENTE_ID = NULL WHERE ID = ?`, ids[1].String())
		require.NoError(t, err)
		_, err = q.ExecContext(ctx, `UPDATE MSP_PAGOS_RECIBIDOS SET IMPORTE = NULL WHERE ID = ?`, ids[2].String())
		require.NoError(t, err)

		desde, hasta := ventanaDelDia()
		acts, err := desempenofb.NewActividadesRepo(pool).ActividadesZona(ctx, zona, desde, hasta)
		require.NoError(t, err, "un NULL no tumba la consulta")
		var vistos []string
		for _, a := range acts {
			for _, id := range ids {
				if a.ID == domain.IDPagoPendiente(id.String()) {
					vistos = append(vistos, id.String())
				}
			}
		}
		assert.Equal(t, []string{ids[0].String()}, vistos, "solo sale la fila completa")
	})
}

// cancelarAbono cancela un abono como lo deja Microsip: el documento y su
// importe en 'S'. La caché MSP_PAGOS_VENTAS la mantienen los triggers.
func cancelarAbono(ctx context.Context, t *testing.T, q firebird.Querier, doctoCCID int) {
	t.Helper()
	_, err := q.ExecContext(ctx, `UPDATE DOCTOS_CC SET CANCELADO = 'S' WHERE DOCTO_CC_ID = ?`, doctoCCID)
	require.NoError(t, err, "cancelar DOCTOS_CC")
	var cache, importe string
	require.NoError(t, q.QueryRowContext(ctx,
		`SELECT p.CANCELADO, i.CANCELADO FROM MSP_PAGOS_VENTAS p
		 JOIN IMPORTES_DOCTOS_CC i ON i.IMPTE_DOCTO_CC_ID = p.IMPTE_DOCTO_CC_ID
		 WHERE p.DOCTO_CC_ID = ?`, doctoCCID).Scan(&cache, &importe),
		"el abono cancelado sigue en la caché (si no, la prueba no distinguiría el filtro)")
	require.Equal(t, "S", importe, "Microsip propaga la cancelación al importe")
}

// El reparto por lote usa la zona de la FILA (p.ZONA_CLIENTE_ID del pago,
// s.ZONA_CLIENTE_ID de la venta del pendiente, v.ZONA_CLIENTE_ID de la
// visita), no la zona actual del cliente: un cliente que cambió de ruta deja
// sus pagos viejos en la ruta donde se cobraron, igual que la lectura por
// zona. En dev no hay pagos con zona distinta a la del cliente, así que el
// caso se siembra.
func TestActividadesRepo_LoteRepartePorLaZonaDeLaFila(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, domain.ConceptoCobranzaEnRuta)
		zonaA := microsipseed.PrimeraZona(t, q)
		zonaB := segundaZona(ctx, t, q, zonaA)
		cliente := microsipseed.ClienteEnZona(t, q, "ROSALBA QUINTERO DÍAZ", zonaA)
		usuarioID, _ := usuarioSembrado(ctx, t, q)
		capturado := time.Date(2024, 3, 20, 18, 0, 0, 0, time.UTC)

		ventaAplicada := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{})
		microsipseed.AbonoAplicado(t, q, ventaAplicada, microsipseed.OpcionesAbono{
			ConceptoCCID: domain.ConceptoCobranzaEnRuta, Importe: decimal.NewFromInt(150), Fecha: diaAbono,
			ConFormaCobro: true, FormaCobroID: domain.FormaCobroEfectivo,
		})
		_, err := q.ExecContext(ctx, `UPDATE MSP_PAGOS_VENTAS SET ZONA_CLIENTE_ID = ? WHERE DOCTO_CC_ACR_ID = ?`,
			zonaB, ventaAplicada.CargoCCID)
		require.NoError(t, err)

		ventaPendiente := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{})
		sembrarPagoRecibido(ctx, t, q, pagoRecibidoSembrado{
			id: uuid.New(), cargoID: ventaPendiente.CargoCCID, clienteID: cliente, cobradorID: 11502,
			importe: decimal.NewFromInt(80), formaCobroID: domain.FormaCobroEfectivo,
			conceptoID: domain.ConceptoCobranzaEnRuta, fecha: capturado, recibido: capturado,
			estado: "P", createdBy: usuarioID,
		})
		_, err = q.ExecContext(ctx, `UPDATE MSP_SALDOS_VENTAS SET ZONA_CLIENTE_ID = ? WHERE DOCTO_CC_ID = ?`,
			zonaB, ventaPendiente.CargoCCID)
		require.NoError(t, err)

		sembrarVisita(ctx, t, q, visitaSembrada{
			id: uuid.New(), clienteID: cliente, zonaID: zonaB, cobradorID: 11502,
			tipo: "No se encontraba", fecha: capturado, lat: 18.4617, lng: -97.3928,
		})

		desde, hasta := ventanaDelDia()
		lote, err := desempenofb.NewActividadesRepo(pool).ActividadesZonas(ctx, []outbound.ConsultaActividades{
			{ZonaID: zonaA, Desde: desde, Hasta: hasta},
			{ZonaID: zonaB, Desde: desde, Hasta: hasta},
		})
		require.NoError(t, err)
		require.Len(t, lote, 2)
		assert.Empty(t, delCliente(lote[0], cliente), "nada sale en la zona actual del cliente")
		enB := delCliente(lote[1], cliente)
		tipos := map[string]int{}
		for _, a := range enB {
			switch {
			case a.Visita != nil:
				tipos["visita"]++
			case a.Pago != nil && a.Pago.Aplicado:
				tipos["aplicado"]++
			case a.Pago != nil:
				tipos["pendiente"]++
			}
		}
		assert.Equal(t, map[string]int{"aplicado": 1, "pendiente": 1, "visita": 1}, tipos,
			"los tres salen en la zona de su fila")
	})
}
