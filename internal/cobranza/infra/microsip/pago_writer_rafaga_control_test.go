//nolint:misspell // Spanish vocabulary (pago, ráfaga, bloqueo) by project convention.
package microsip_test

import (
	"context"
	"database/sql"
	"strconv"
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

// EL CONTROL POSITIVO DE LA RÁFAGA.
//
// TestE2E_PagoWriter_Rafaga_ClientesDistintos pasa: 50 pagos concurrentes de
// clientes distintos, cero fallos, dos veces. Ese verde NO significa nada por
// sí solo — un experimento que no se ha demostrado capaz de fallar no dice
// nada al pasar, y esa es la regla de esta casa: una ausencia no es hallazgo
// hasta demostrar que la medición HABRÍA encontrado lo que busca.
//
// Este archivo es esa demostración. Mete un fallo a propósito dentro de una
// ráfaga y afirma que la ráfaga lo ve, lo cuenta y lo atribuye a la goroutine
// correcta. Son dos controles porque prueban dos cosas distintas:
//
//	ControlPositivo_UnPagoInvalido  → el arnés ve un fallo CUALQUIERA
//	ControlPositivo_UnPagoBloqueado → el arnés ve un fallo POR CONTENCIÓN,
//	                                  que es la clase que nos importa
//
// El segundo además responde la pregunta que producción no ha podido
// contestar: CON QUÉ CÓDIGO se manifiesta la contención. Ese es el valor que
// se lleva uno aunque el defecto real esté en otro lado, porque es la firma
// que hay que buscar en el ULTIMO_ERROR de las filas nuevas.

const (
	// rafagaStatementTimeout es el techo de servidor para el pago bloqueado.
	// Corto para que la prueba no cuelgue diez minutos —el techo por omisión
	// es FB_STATEMENT_TIMEOUT, 10m— y largo para que una máquina lenta no
	// confunda una toma normal de bloqueo con el corte.
	rafagaStatementTimeout = 2 * time.Second
	// rafagaPresupuesto es cuánto se espera un desenlace antes de llamarlo
	// cuelgue. Cinco veces el techo.
	rafagaPresupuesto = 10 * time.Second
)

// setStatementTimeout arma la sentencia que Firebird acepta, en segundos
// enteros — la misma forma que usa internal/platform/firebird/driverwrap.go.
func setStatementTimeout(d time.Duration) string {
	segundos := int64((d + time.Second - 1) / time.Second)
	return "SET STATEMENT TIMEOUT " + strconv.FormatInt(segundos, 10) + " SECOND"
}

// poolConTechoCorto arma un pool privado de n conexiones y le pone el techo
// corto a TODAS.
//
// POR QUÉ NO SE USA cfg.StatementTimeout: firebird.registerOtelDriver envuelve
// su registro en un sync.Once y registerCancelProofDriver captura ahí el
// techo, así que el del PRIMER pool del proceso queda horneado en el único
// driver registrado y todos los pools posteriores lo heredan en silencio,
// diga lo que diga su config. El techo se aplica entonces por conexión.
//
// POR QUÉ LAS n SE SACAN A LA VEZ: sacándolas de una en una el pool devolvería
// la misma conexión n veces y las otras n-1 se quedarían sin techo — y como el
// pago bloqueado podría caer justo en una de ésas, la prueba colgaría diez
// minutos en vez de fallar en dos segundos.
func poolConTechoCorto(t *testing.T, n int) *firebird.Pool {
	t.Helper()
	cfg := fbtestutil.TestFirebirdConfig(t)
	cfg.PoolSize = n
	pool, err := firebird.New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, pool.Stop(context.Background())) })
	require.NoError(t, pool.Start(context.Background()))
	pool.SetMaxOpenConns(n)
	pool.SetMaxIdleConns(n)
	pool.SetConnMaxIdleTime(0)
	pool.SetConnMaxLifetime(0)

	conns := make([]*sql.Conn, 0, n)
	for range n {
		c, connErr := pool.Conn(context.Background())
		require.NoError(t, connErr, "sacando las n conexiones a la vez")
		_, execErr := c.ExecContext(context.Background(), setStatementTimeout(rafagaStatementTimeout))
		require.NoError(t, execErr, "aplicando el techo corto")
		conns = append(conns, c)
	}
	for _, c := range conns {
		require.NoError(t, c.Close())
	}
	return pool
}

// dispararRafaga corre las entradas en paralelo, todas soltadas a la vez, y
// devuelve el error de cada una en su posición.
func dispararRafaga(h *e2eHarness, entradas []outbound.MicrosipPagoInput, alAplicar func(int, outbound.MicrosipPagoResult)) []error {
	errs := make([]error, len(entradas))
	var salida sync.WaitGroup
	salida.Add(1)
	var listas, terminadas sync.WaitGroup
	for i := range entradas {
		listas.Add(1)
		terminadas.Add(1)
		go func(idx int) {
			defer terminadas.Done()
			listas.Done()
			salida.Wait()
			var res outbound.MicrosipPagoResult
			err := h.txMgr.RunInTx(context.Background(), func(txCtx context.Context) error {
				var e error
				res, e = h.writer.Aplicar(txCtx, entradas[idx])
				return e
			})
			errs[idx] = err
			if err == nil && alAplicar != nil {
				alAplicar(idx, res)
			}
		}(i)
	}
	listas.Wait()
	salida.Done()
	terminadas.Wait()
	return errs
}

// codigoDe devuelve el código de apperror, o una etiqueta si no lo es.
func codigoDe(err error) string {
	if err == nil {
		return ""
	}
	if ae, ok := apperror.As(err); ok {
		return ae.Code
	}
	return "(no es apperror)"
}

// TestE2E_PagoWriter_Rafaga_ControlPositivo_UnPagoInvalido mete UN pago que
// Microsip tiene que rechazar —importe muy por encima del saldo del cargo,
// que dispara EX_SALDO_CARGO_EXCEDIDO— y afirma que la ráfaga lo ve, cuenta
// exactamente uno, y lo atribuye a la goroutine que lo mandó.
//
// Es el control barato: prueba la maquinaria de observación, no la clase de
// fallo que buscamos. Sin él, un cero de fallos podría ser un arnés que
// simplemente no mira.
//
//nolint:paralleltest // compromete transacciones reales; la limpieza va por t.Cleanup.
func TestE2E_PagoWriter_Rafaga_ControlPositivo_UnPagoInvalido(t *testing.T) {
	h := newE2EHarness(t)
	ctx := context.Background()
	const numPagos = 6
	const elMalo = 2

	entradas := make([]outbound.MicrosipPagoInput, numPagos)
	cargos := make([]int, numPagos)
	for i := range numPagos {
		clienteID := seedCliente(t, ctx, h.pool, h.txMgr)
		cargo := seedCargo(t, ctx, h.pool, h.txMgr, clienteID, decimal.NewFromInt(1000))
		cargos[i] = cargo.doctoCCID
		importe := decimal.NewFromInt(100)
		if i == elMalo {
			// Muy por encima del saldo del cargo: el abono no cabe.
			importe = decimal.NewFromInt(999_999)
		}
		entradas[i] = outbound.MicrosipPagoInput{
			CargoDoctoCCID: cargo.doctoCCID, ClienteID: clienteID,
			CobradorID: testCobradorID, Cobrador: "Ramírez García, Jorge",
			Importe: importe, FormaCobroID: testFormaCobroID,
			ConceptoCCID: testConceptoCCID, FechaHoraPago: time.Now().UTC(),
		}
	}

	var mu sync.Mutex
	errs := dispararRafaga(h, entradas, func(idx int, res outbound.MicrosipPagoResult) {
		mu.Lock()
		defer mu.Unlock()
		h.registerAplicarCleanup(t, res, cargos[idx])
	})

	for i, err := range errs {
		if err != nil {
			t.Logf("pago %d falló: %s — %v", i, codigoDe(err), err)
		}
	}

	require.Error(t, errs[elMalo],
		"el pago inválido tiene que fallar: si pasa, el arnés no mide nada y el verde de la ráfaga no significa nada")
	for i, err := range errs {
		if i == elMalo {
			continue
		}
		require.NoErrorf(t, err,
			"el pago %d era válido: un fallo aquí sería contagio entre goroutines, no el pago inválido", i)
	}
}

// TestE2E_PagoWriter_Rafaga_ControlPositivo_UnPagoBloqueado es el control que
// importa: mete contención REAL —otra sesión sostiene sin cerrar la fila de
// SALDOS_CC del cliente de UN pago— y afirma que la ráfaga la ve.
//
// SALDOS_CC es el objetivo porque es lo que un pago de verdad escribe: la
// tabla es el agregado por cliente y mes (CLIENTE_ID, ANO, MES, CARGOS_CXC,
// CREDITOS_CXC…) y el abono incrementa el crédito. Es además una de las dos
// filas que persistPagoTx documenta como compartidas con Cxc.exe, que es
// exactamente lo que pasa en producción mientras el cobrador sube su bandeja
// y esta prueba, sola, no tiene.
//
// SI ESTA PRUEBA FALLA PORQUE NADA SE BLOQUEÓ, el hallazgo no es que la
// prueba esté mal: es que el camino del pago no toca esa fila, y entonces hay
// que buscar cuál toca antes de creerle nada al verde de la ráfaga.
//
//nolint:paralleltest // compromete transacciones reales; la limpieza va por t.Cleanup.
func TestE2E_PagoWriter_Rafaga_ControlPositivo_UnPagoBloqueado(t *testing.T) {
	requireFBEnv(t)
	ctx := context.Background()
	const numPagos = 6
	const elBloqueado = 3

	// El pool de la ráfaga: n conexiones, todas con el techo corto, para que
	// el bloqueado se corte en dos segundos y no en diez minutos.
	pool := poolConTechoCorto(t, numPagos)
	h := &e2eHarness{
		pool:   pool,
		txMgr:  firebird.NewTxManager(pool.DB),
		writer: microsip.NewPagoWriter(pool),
	}

	entradas := make([]outbound.MicrosipPagoInput, numPagos)
	cargos := make([]int, numPagos)
	clientes := make([]int, numPagos)
	for i := range numPagos {
		clienteID := seedCliente(t, ctx, h.pool, h.txMgr)
		cargo := seedCargo(t, ctx, h.pool, h.txMgr, clienteID, decimal.NewFromInt(1000))
		cargos[i], clientes[i] = cargo.doctoCCID, clienteID
		entradas[i] = outbound.MicrosipPagoInput{
			CargoDoctoCCID: cargo.doctoCCID, ClienteID: clienteID,
			CobradorID: testCobradorID, Cobrador: "Ramírez García, Jorge",
			Importe: decimal.NewFromInt(100), FormaCobroID: testFormaCobroID,
			ConceptoCCID: testConceptoCCID, FechaHoraPago: time.Now().UTC(),
		}
	}

	// La sesión que estorba: pool aparte de UNA conexión, para no gastarle
	// una a la ráfaga ni heredarle el techo corto (quien tiene que morir es
	// el bloqueado, no el que bloquea).
	cfgLock := fbtestutil.TestFirebirdConfig(t)
	cfgLock.PoolSize = 1
	poolLock, err := firebird.New(cfgLock)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, poolLock.Stop(context.Background())) })
	require.NoError(t, poolLock.Start(context.Background()))

	tx, err := poolLock.BeginTx(ctx, nil)
	require.NoError(t, err, "abriendo la transacción que estorba")
	liberado := false
	t.Cleanup(func() {
		if !liberado {
			assert.NoError(t, tx.Rollback())
		}
	})
	// Un UPDATE que no cambia nada toma el bloqueo de escritura igual.
	_, err = tx.ExecContext(ctx,
		`UPDATE SALDOS_CC SET CREDITOS_CXC = CREDITOS_CXC WHERE CLIENTE_ID = ?`,
		clientes[elBloqueado])
	require.NoError(t, err, "tomando el bloqueo sobre SALDOS_CC")

	var mu sync.Mutex
	hecho := make(chan []error, 1)
	go func() {
		hecho <- dispararRafaga(h, entradas, func(idx int, res outbound.MicrosipPagoResult) {
			mu.Lock()
			defer mu.Unlock()
			h.registerAplicarCleanup(t, res, cargos[idx])
		})
	}()

	var errs []error
	select {
	case errs = <-hecho:
	case <-time.After(rafagaPresupuesto):
		t.Fatalf("la ráfaga no terminó en %s: el techo de %s no está en vigor en todas las conexiones",
			rafagaPresupuesto, rafagaStatementTimeout)
	}

	require.NoError(t, tx.Rollback(), "soltando el bloqueo")
	liberado = true

	var numFallidos int
	for i, e := range errs {
		if e != nil {
			numFallidos++
			t.Logf("pago %d falló: %s — %v", i, codigoDe(e), e)
		}
	}
	t.Logf("con UNA fila bloqueada por otra sesión: %d de %d fallaron", numFallidos, numPagos)

	require.Error(t, errs[elBloqueado],
		"el pago cuyo cliente tenía la fila tomada NO falló: o el camino del pago no escribe SALDOS_CC "+
			"—y entonces hay que averiguar qué fila sí, porque el verde de la ráfaga no prueba nada— "+
			"o Firebird no serializa ahí")
	// El código concreto se DEJA ESCRITO en el log de arriba en vez de fijarlo
	// aquí: es el dato que se va a buscar en ULTIMO_ERROR, y clavarlo a una
	// constante haría que un cambio de mapeo rompiera la prueba en vez de
	// avisar. Lo que sí se afirma es que viaja como apperror tipado, porque de
	// eso depende que la fila lo pueda guardar.
	require.NotEqual(t, "(no es apperror)", codigoDe(errs[elBloqueado]),
		"el fallo por contención tiene que llegar como apperror tipado, o no hay motivo que escribir en la fila")
}
