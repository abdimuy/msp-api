//nolint:misspell // Spanish vocabulary (pago, ráfaga, cargo) by project convention.
package microsip_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/infra/microsip"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// EL CONTROL QUE FALTABA: la ráfaga del MISMO cliente.
//
// TestE2E_PagoWriter_Rafaga_ClientesDistintos fijó «clientes distintos» como
// constante durante todo el experimento, porque las colisiones medidas en
// producción fueron entre clientes distintos. Nunca se probó lo contrario, y
// ahí hay una tensión que conviene resolver antes de creerle a nadie:
//
//   - SALDOS_CC es por cliente y MES (CLIENTE_ID, ANO, MES, CARGOS_CXC,
//     CREDITOS_CXC…). Clientes distintos escriben FILAS distintas, así que por
//     construcción no se estorban — y eso es lo que midió la ráfaga.
//   - Pero el patrón de producción es de TRES MILISEGUNDOS entre pagos, y eso
//     sugiere pagos estorbándose UNOS A OTROS, no un tercero estorbándolos a
//     todos. Una caja cobrando bloquea la fila que le toca; no tiene por qué
//     concentrar sus víctimas en los pagos más juntos de la ráfaga.
//
// Así que se varía la única variable que quedó fija. Dos formas, porque miden
// cosas distintas:
//
//	MismoCliente_CargosDistintos → aísla la contención de SALDOS_CC, que es
//	                               del cliente: los cargos no se comparten
//	MismoCliente_MismoCargo      → añade encima la del cargo
//
// SI FALLAN, tenemos el mecanismo sin esperar al despliegue. SI NO FALLAN, la
// contención entre pagos queda descartada ENTERA —ni entre clientes distintos
// ni entre pagos del mismo— y el ULTIMO_ERROR de producción pasa a ser la
// única vía que queda.
//
// EL TECHO VA HOLGADO A PROPÓSITO. Con dos segundos, una serialización
// legítima —Firebird espera, no falla: RunInTx es READ COMMITTED con WAIT— se
// leería como fallo y la conclusión saldría al revés. Con diez, sólo muere lo
// que de verdad no avanza, y la serialización se ve en el reloj, que también
// se reporta.
const (
	mismoClienteTecho       = 10 * time.Second
	mismoClientePresupuesto = 90 * time.Second
)

// rafagaMismoCliente siembra un cliente, le cuelga los cargos que pida y
// dispara numPagos concurrentes contra ellos. Devuelve los errores y el reloj.
func rafagaMismoCliente(t *testing.T, numPagos int, cargosDistintos bool) ([]error, time.Duration) {
	t.Helper()
	requireFBEnv(t)
	ctx := context.Background()

	pool := poolConTechoCorto(t, numPagos, mismoClienteTecho)
	h := &e2eHarness{
		pool:   pool,
		txMgr:  firebird.NewTxManager(pool.DB),
		writer: microsip.NewPagoWriter(pool),
	}

	clienteID := seedCliente(t, ctx, h.pool, h.txMgr)

	// El cargo lleva saldo de sobra: lo que se mide es la concurrencia, no el
	// tope del saldo. Un EX_SALDO_CARGO_EXCEDIDO aquí sería un fallo por la
	// razón equivocada y haría creer que hubo contención donde no la hubo.
	const importePago = 100
	saldoCargo := decimal.NewFromInt(int64(numPagos) * importePago * 10)

	entradas := make([]outbound.MicrosipPagoInput, numPagos)
	cargos := make([]int, numPagos)
	if cargosDistintos {
		for i := range numPagos {
			cargos[i] = seedCargo(t, ctx, h.pool, h.txMgr, clienteID, saldoCargo).doctoCCID
		}
	} else {
		unico := seedCargo(t, ctx, h.pool, h.txMgr, clienteID, saldoCargo).doctoCCID
		for i := range numPagos {
			cargos[i] = unico
		}
	}
	for i := range numPagos {
		entradas[i] = outbound.MicrosipPagoInput{
			CargoDoctoCCID: cargos[i], ClienteID: clienteID,
			CobradorID: testCobradorID, Cobrador: "Ramírez García, Jorge",
			Importe: decimal.NewFromInt(importePago), FormaCobroID: testFormaCobroID,
			ConceptoCCID: testConceptoCCID, FechaHoraPago: time.Now().UTC(),
		}
	}

	// Un canal de uno hace de mutex: registerAplicarCleanup se llama desde las
	// goroutines y t.Cleanup no promete ser concurrente aquí.
	mu := make(chan struct{}, 1)
	mu <- struct{}{}
	hecho := make(chan []error, 1)
	inicio := time.Now()
	go func() {
		hecho <- dispararRafaga(h, entradas, func(idx int, res outbound.MicrosipPagoResult) {
			<-mu
			h.registerAplicarCleanup(t, res, cargos[idx])
			mu <- struct{}{}
		})
	}()

	select {
	case errs := <-hecho:
		return errs, time.Since(inicio)
	case <-time.After(mismoClientePresupuesto):
		t.Fatalf("la ráfaga del mismo cliente no terminó en %s: el techo de %s no está en vigor",
			mismoClientePresupuesto, mismoClienteTecho)
		return nil, 0
	}
}

// reportar deja el censo escrito pase lo que pase: es el dato, no el veredicto.
func reportar(t *testing.T, etiqueta string, errs []error, transcurrido time.Duration) int {
	t.Helper()
	var fallidos int
	porCodigo := map[string]int{}
	for i, e := range errs {
		if e != nil {
			fallidos++
			porCodigo[codigoDe(e)]++
			t.Logf("  pago %d falló: %s — %v", i, codigoDe(e), e)
		}
	}
	t.Logf("%s: %d pagos en %s — %d fallaron", etiqueta, len(errs),
		transcurrido.Round(time.Millisecond), fallidos)
	for c, n := range porCodigo {
		t.Logf("  %d × %s", n, c)
	}
	return fallidos
}

// TestE2E_PagoWriter_Rafaga_MismoCliente_CargosDistintos: todos los pagos son
// del mismo cliente pero contra cargos distintos, así que la única fila que
// comparten es la de SALDOS_CC.
//
//nolint:paralleltest // compromete transacciones reales; la limpieza va por t.Cleanup.
func TestE2E_PagoWriter_Rafaga_MismoCliente_CargosDistintos(t *testing.T) {
	errs, transcurrido := rafagaMismoCliente(t, 10, true)
	fallidos := reportar(t, "MISMO cliente, cargos DISTINTOS", errs, transcurrido)

	require.Zero(t, fallidos,
		"diez pagos concurrentes del mismo cliente contra cargos distintos no deben perder escrituras")
}

// TestE2E_PagoWriter_Rafaga_MismoCliente_MismoCargo: además de SALDOS_CC
// comparten el cargo, así que también el caché por DOCTO_CC_ID.
//
//nolint:paralleltest // compromete transacciones reales; la limpieza va por t.Cleanup.
func TestE2E_PagoWriter_Rafaga_MismoCliente_MismoCargo(t *testing.T) {
	errs, transcurrido := rafagaMismoCliente(t, 10, false)
	fallidos := reportar(t, "MISMO cliente, MISMO cargo", errs, transcurrido)

	require.Zero(t, fallidos,
		"diez pagos concurrentes del mismo cliente contra el mismo cargo no deben perder escrituras")
}
