//nolint:misspell // Spanish vocabulary (pago, cobrador, ráfaga) by project convention.
package microsip_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/infra/microsip"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// La ráfaga: qué le pasa al escritor de Microsip cuando la bandeja del
// teléfono se vacía de golpe.
//
// EL HECHO MEDIDO. Sin señal el teléfono guarda los pagos en una bandeja
// local; al recuperar red los vacía de golpe. El envío sale de
// PaymentsPendingSynchronizer y PaymentsViewModel con un `forEach { enqueue }`
// y cada pago se encola como un OneTimeWorkRequest con nombre único, así que
// ExistingWorkPolicy no serializa entre pagos distintos y el executor de
// WorkManager corre varios doWork() a la vez. En una ruta del 1 de septiembre
// fueron 56 pagos en tres segundos, y los TRES que llegaron a menos de tres
// milisegundos del anterior fallaron.
//
// POR QUÉ CLIENTES DISTINTOS. Las colisiones medidas ocurrieron entre clientes
// distintos —Isidro, Laura y Rebeca son tres— así que el recurso en disputa no
// puede ser una fila del cliente ni del cargo. Sembrar N pagos del mismo
// cliente mediría otra cosa y saldría verde por la razón equivocada.
//
// QUÉ NO ES, YA MEDIDO. GEN_FOLIO_TEMP no es el cuello: su fuente en la base
// es GEN_ID(ID_FOLIO_TEMP,1), un generador de Firebird, que es no
// transaccional y no toma bloqueos. Se descartó leyendo el procedimiento, no
// suponiéndolo.
//
// ESTA PRUEBA NO AFIRMA QUE FALLE. Afirma cuántas fallan y con qué error, y
// deja el conteo escrito.
//
// LO QUE MIDIÓ, 2026-09-02, CONTRA EL FIREBIRD DE DESARROLLO: nada falló.
//
//	pool de 10 (el de producción):  50 pagos en 613ms — 0 fallaron, 50 abonos
//	pool de 50 (concurrencia real): 50 pagos en 2.544s — 0 fallaron, 50 abonos
//
// Así que la ráfaga NO se reprodujo, y esta prueba pasa de ser una
// reproducción a ser una guarda: si alguien mete contención en este camino,
// se entera aquí.
//
// NO SE INVENTA UN ARREGLO CON ESTO. Que no reproduzca no prueba que el
// camino esté sano; prueba que el fallo necesita algo que esta prueba no
// tiene. Lo que le falta, y no puede tener, está medido y es concreto:
//
//   - EN PRODUCCIÓN CXC.EXE ESCRIBE AL MISMO TIEMPO. Las cajas tienen tomadas
//     SALDOS_CC y MSP_SALDOS_VENTAS mientras el cobrador sube su bandeja. Esa
//     forma sí está medida, pero en otro archivo y de a una:
//     TestE2E_ContencionDeFila_LaEsperaSeCortaComoTimeoutNoComoConflicto.
//     Aquí no hay una segunda sesión.
//   - LA BASE NO ES LA MISMA. Ésta es el clon de desarrollo sobre Docker en
//     una Mac; producción es Windows Server 2016 con la base en uso.
//   - EL CAMINO ES MÁS CORTO. Esto ejerce PagoWriter.Aplicar. La petición real
//     abre además el INSERT en MSP_PAGOS_RECIBIDOS, las imágenes y el UPDATE
//     final dentro de LA MISMA transacción, así que sostiene sus bloqueos
//     bastante más tiempo del que se sostienen aquí.
//
// El paso siguiente no es adivinar cuál de los tres es: es desplegar el
// arreglo que hace visible el rechazo y leer los motivos que las filas nuevas
// con ESTADO='P' van a dejar escritos. Con eso se vuelve, y se mide.
//
// QUÉ SE DESCARTÓ, Y CÓMO. GEN_FOLIO_TEMP era el sospechoso natural por ser
// lo único que todos los pagos tocan sin importar el cliente. No es: su
// fuente en la base es GEN_ID(ID_FOLIO_TEMP,1), un generador de Firebird, no
// transaccional y sin bloqueos. Se leyó el procedimiento; no se supuso.

// resultadoRafaga es lo que una goroutine de la ráfaga deja escrito.
type resultadoRafaga struct {
	indice int
	err    error
	codigo string
}

// TestE2E_PagoWriter_Rafaga_ClientesDistintos dispara N pagos concurrentes,
// cada uno de un cliente distinto y contra su propio cargo, cada uno en su
// propia transacción — la forma que produce la bandeja al vaciarse.
//
//nolint:paralleltest // compromete transacciones reales; la limpieza va por t.Cleanup.
func TestE2E_PagoWriter_Rafaga_ClientesDistintos(t *testing.T) {
	requireFBEnv(t)
	ctx := context.Background()

	// 50 se acerca a los 56 medidos sin volver la siembra eterna.
	const numPagos = 50

	// POOL PROPIO, Y DEL ANCHO DE LA RÁFAGA. El pool compartido lleva
	// FB_POOL_SIZE=10 —el mismo valor que trae producción por omisión
	// (config.go:371, envDefault "10")—, así que con 50 goroutines sólo diez
	// llegan a Firebird a la vez y las otras cuarenta esperan turno en
	// database/sql. Eso mediría una cola, no una ráfaga, y un cero de fallos
	// no querría decir nada. El pool va aparte para no imponerle 50
	// conexiones al resto del binario.
	//
	// Que producción también tenga 10 no vuelve inútil medir con 50: si algo
	// se rompe sólo con concurrencia real, hay que saberlo antes de subirle
	// el pool al servidor.
	cfg := fbtestutil.TestFirebirdConfig(t)
	cfg.PoolSize = numPagos
	pool, err := firebird.New(cfg)
	require.NoError(t, err)
	// Registrado ANTES de sembrar: t.Cleanup es LIFO, así que este cierre
	// corre DESPUÉS de todas las limpiezas que la siembra registre.
	t.Cleanup(func() { assert.NoError(t, pool.Stop(context.Background())) })
	require.NoError(t, pool.Start(context.Background()))
	pool.SetMaxOpenConns(numPagos)
	pool.SetMaxIdleConns(numPagos)

	h := &e2eHarness{
		pool:   pool,
		txMgr:  firebird.NewTxManager(pool.DB),
		writer: microsip.NewPagoWriter(pool),
	}

	// ─── siembra, en serie ───────────────────────────────────────────────
	// La siembra NO es la medición: va en serie a propósito, para que lo
	// único concurrente de la prueba sea Aplicar.
	destinos := make([]destinoRafaga, numPagos)
	for i := range numPagos {
		clienteID := seedCliente(t, ctx, h.pool, h.txMgr)
		cargo := seedCargo(t, ctx, h.pool, h.txMgr, clienteID, decimal.NewFromInt(1000))
		destinos[i] = destinoRafaga{clienteID: clienteID, cargoID: cargo.doctoCCID}
	}

	// ─── la ráfaga ───────────────────────────────────────────────────────
	resultados := make([]resultadoRafaga, numPagos)
	// La barrera hace que las goroutines salgan juntas. Sin ella, arrancar
	// 50 goroutines toma más de los tres milisegundos que separan a los
	// pagos que fallaron, y la prueba mediría un goteo, no una ráfaga.
	var salida sync.WaitGroup
	salida.Add(1)
	var listas, terminadas sync.WaitGroup

	for i := range numPagos {
		listas.Add(1)
		terminadas.Add(1)
		go func(idx int) {
			defer terminadas.Done()
			in := outbound.MicrosipPagoInput{
				CargoDoctoCCID: destinos[idx].cargoID,
				ClienteID:      destinos[idx].clienteID,
				CobradorID:     testCobradorID,
				Cobrador:       "Ramírez García, Jorge",
				Importe:        decimal.NewFromInt(100),
				FormaCobroID:   testFormaCobroID,
				ConceptoCCID:   testConceptoCCID,
				FechaHoraPago:  time.Now().UTC(),
			}
			listas.Done()
			salida.Wait()

			var res outbound.MicrosipPagoResult
			err := h.txMgr.RunInTx(context.Background(), func(txCtx context.Context) error {
				var e error
				res, e = h.writer.Aplicar(txCtx, in)
				return e
			})
			resultados[idx] = resultadoRafaga{indice: idx, err: err}
			if err != nil {
				if ae, ok := apperror.As(err); ok {
					resultados[idx].codigo = ae.Code
				} else {
					resultados[idx].codigo = "(no es apperror)"
				}
				return
			}
			// Sólo se registra limpieza de lo que de verdad se escribió.
			// Registrarla desde la goroutine es seguro: t.Cleanup lleva su
			// propio mutex.
			h.registerAplicarCleanup(t, res, destinos[idx].cargoID)
		}(i)
	}

	listas.Wait()
	inicio := time.Now()
	salida.Done()
	terminadas.Wait()
	transcurrido := time.Since(inicio)

	// ─── el censo ────────────────────────────────────────────────────────
	fallidos := make(map[string]int)
	var numFallidos int
	for _, r := range resultados {
		if r.err != nil {
			numFallidos++
			fallidos[r.codigo]++
		}
	}

	t.Logf("ráfaga de %d pagos concurrentes de clientes distintos en %s: %d fallaron",
		numPagos, transcurrido.Round(time.Millisecond), numFallidos)
	for codigo, n := range fallidos {
		t.Logf("  %d × %s", n, codigo)
	}
	for _, r := range resultados {
		if r.err != nil {
			t.Logf("  pago %d: %v", r.indice, r.err)
		}
	}

	// Control positivo del arnés: si NADA se escribió, la prueba no midió
	// contención — midió una siembra rota, y un cero de fallos no significaría
	// nada. Se afirma aparte para que ese modo no se confunda con éxito.
	require.Less(t, numFallidos, numPagos,
		"ningún pago de la ráfaga se escribió: el arnés está roto, no medido")

	q := firebird.GetQuerier(ctx, h.pool.DB)
	var aplicados int
	require.NoError(t, q.QueryRowContext(ctx,
		// TIPO_IMPTE='R' es obligatorio y no es decoración: el renglón del
		// propio cargo termina también con DOCTO_CC_ACR_ID apuntando al cargo,
		// así que sin el filtro el conteo sale exactamente al doble y parece
		// que cada pago se escribió dos veces. Medido: 100 de 50.
		fmt.Sprintf(`SELECT COUNT(*) FROM IMPORTES_DOCTOS_CC
		             WHERE TIPO_IMPTE = 'R' AND DOCTO_CC_ACR_ID IN (%s)`,
			listaDeIDs(destinos)),
	).Scan(&aplicados))
	t.Logf("abonos visibles en IMPORTES_DOCTOS_CC: %d de %d", aplicados, numPagos)

	require.Equal(t, numPagos-numFallidos, aplicados,
		"el conteo en la base tiene que coincidir con los que dijeron haber tenido éxito")

	// La afirmación que importa. Hoy pasa —la ráfaga no reprodujo el fallo—,
	// así que lo que guarda es que siga pasando. Si algún día falla, el
	// mensaje trae el censo de arriba y el arreglo se hace CON el error en la
	// mano, no adivinándolo.
	require.Zero(t, numFallidos,
		"una ráfaga de pagos de clientes distintos no debe perder escrituras")
}

// destinoRafaga es el par cliente/cargo al que apunta un pago de la ráfaga.
type destinoRafaga struct {
	clienteID int
	cargoID   int
}

// listaDeIDs arma la lista de cargos para el IN (...). Los IDs son enteros
// generados por la propia prueba desde ID_DOCTOS, no entrada externa.
func listaDeIDs(destinos []destinoRafaga) string {
	partes := make([]string, 0, len(destinos))
	for _, d := range destinos {
		partes = append(partes, strconv.Itoa(d.cargoID))
	}
	return strings.Join(partes, ",")
}
