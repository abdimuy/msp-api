//nolint:misspell // Spanish vocabulary (pago, cobrador, motivo) by project convention.
package ventfb_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// EL MOTIVO DEL RECHAZO NO SE PUEDE GUARDAR SI TRAE UN ACENTO.
//
// Medido en producción el 2026-09-04. Cuando Microsip rechaza un pago, el
// mensaje que devuelve llega con bytes **Win1252**: la "é" de "crédito" viaja
// como 0xE9, que NO es UTF-8 válido. `MSP_PAGOS_RECIBIDOS.ULTIMO_ERROR` es
// CHARACTER SET UTF8, así que el UPDATE muere con:
//
//	Dynamic SQL Error / SQL error code = -303 / Malformed string
//
// y la transacción de registrarFalloAparte se deshace ENTERA. Consecuencia
// medida sobre las dos filas sobregiradas que quedaron pendientes:
//
//	ULTIMO_ERROR = NULL   →  nadie puede ver qué dijo Microsip
//	INTENTOS     = 0      →  sin backoff, el worker reintenta cada 60 s
//	                         y NUNCA llega a MaxIntentos: no se rinde jamás
//
// El log lo dice —`pago.fallo_no_persistido`, con el rechazo y el error de
// persistencia— pero `run.bat` trunca api.log en cada arranque, así que el
// motivo se pierde al reiniciar. Ésa es la razón por la que el plan de "leer
// los motivos reales en producción" no funcionaba.

// mensajeDeMicrosipEnWin1252 son los bytes EXACTOS del rechazo real, tomados
// del log de producción. La \xe9 es la "é" de "crédito" en Win1252.
//
// Se construye byte a byte a propósito: escribirlo como literal de Go lo haría
// UTF-8 válido y la prueba dejaría de reproducir nada.
func mensajeDeMicrosipEnWin1252() string {
	return "exception 7\nEX_SALDO_CARGO_EXCEDIDO\nEl importe del cr" +
		string([]byte{0xE9}) + "dito es mayor al saldo del cargo"
}

// TestE2E_UltimoError_ConAcentoWin1252_ReproduceElRechazo es el control
// positivo del diagnóstico: demuestra que el texto crudo NO se puede guardar.
//
//nolint:paralleltest // compromete transacciones reales; limpieza por t.Cleanup.
func TestE2E_UltimoError_ConAcentoWin1252_ReproduceElRechazo(t *testing.T) {
	h := newConcurrencyHarness(t)
	ctx := context.Background()

	pagoID := uuid.New()
	h.trackID(pagoID)
	require.NoError(t, h.txMgr.RunInTx(ctx, func(ctx context.Context) error {
		return h.repo.Insert(ctx, buildValidPagoRecibidoWithID(t, pagoID))
	}))

	crudo := mensajeDeMicrosipEnWin1252()
	err := h.txMgr.RunInTx(ctx, func(ctx context.Context) error {
		_, execErr := firebird.GetQuerier(ctx, h.pool.DB).ExecContext(ctx,
			"UPDATE MSP_PAGOS_RECIBIDOS SET ULTIMO_ERROR = ? WHERE ID = ?",
			crudo, pagoID.String())
		return execErr
	})

	require.Error(t, err,
		"si esto pasa, el defecto ya no existe y esta prueba sobra: los bytes "+
			"Win1252 entraron en una columna UTF8")
	assert.Contains(t, err.Error(), "Malformed string",
		"el rechazo es el -303 medido en producción")

	// Y la fila queda intacta: es la mitad que explica el INTENTOS=0.
	var motivo *string
	require.NoError(t, h.txMgr.RunInTx(ctx, func(ctx context.Context) error {
		return firebird.GetQuerier(ctx, h.pool.DB).QueryRowContext(ctx,
			"SELECT ULTIMO_ERROR FROM MSP_PAGOS_RECIBIDOS WHERE ID = ?", pagoID.String(),
		).Scan(&motivo)
	}))
	assert.Nil(t, motivo, "la transacción se deshizo entera; por eso INTENTOS tampoco sube")
}

// TestE2E_UltimoError_SaneadoSeGuarda es la otra mitad: el mismo mensaje,
// pasado por el saneador, SÍ entra — y entra legible, con su acento.
//
// Sin esta prueba, la de arriba sólo demostraría que el defecto existe. Ésta
// demuestra que el arreglo lo cierra, contra la base real y no contra un doble.
//
//nolint:paralleltest // compromete transacciones reales; limpieza por t.Cleanup.
func TestE2E_UltimoError_SaneadoSeGuarda(t *testing.T) {
	h := newConcurrencyHarness(t)
	ctx := context.Background()

	pagoID := uuid.New()
	h.trackID(pagoID)
	require.NoError(t, h.txMgr.RunInTx(ctx, func(ctx context.Context) error {
		return h.repo.Insert(ctx, buildValidPagoRecibidoWithID(t, pagoID))
	}))

	saneado := firebird.SanearParaUTF8(mensajeDeMicrosipEnWin1252())
	require.NoError(t, h.txMgr.RunInTx(ctx, func(ctx context.Context) error {
		_, execErr := firebird.GetQuerier(ctx, h.pool.DB).ExecContext(ctx,
			"UPDATE MSP_PAGOS_RECIBIDOS SET ULTIMO_ERROR = ? WHERE ID = ?",
			saneado, pagoID.String())
		return execErr
	}), "el mensaje saneado tiene que entrar sin -303")

	var motivo *string
	require.NoError(t, h.txMgr.RunInTx(ctx, func(ctx context.Context) error {
		return firebird.GetQuerier(ctx, h.pool.DB).QueryRowContext(ctx,
			"SELECT ULTIMO_ERROR FROM MSP_PAGOS_RECIBIDOS WHERE ID = ?", pagoID.String(),
		).Scan(&motivo)
	}))

	require.NotNil(t, motivo, "el motivo tiene que quedar guardado")
	assert.Contains(t, *motivo, "EX_SALDO_CARGO_EXCEDIDO",
		"la parte accionable del rechazo sobrevive")
	assert.Contains(t, *motivo, "crédito",
		"y con su acento: guardar el motivo mutilado sería medio arreglo")
}
