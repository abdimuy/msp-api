//nolint:misspell // Spanish domain vocabulary (mensaje, segmento) by project convention.
package reactivacionfb

// mensajeCols is the canonical SELECT column list for MSP_RX_MENSAJES (the
// Fase 2 outbound channel queue). The order must match mensajeRowRaw.scanFrom
// exactly.
const mensajeCols = `
	ID,
	CLIENTE_ID,
	SEGMENTO,
	TELEFONO,
	CUERPO,
	ESTADO,
	SENDER_KIND,
	ENCOLADO_EN,
	ENVIADO_EN,
	ERROR,
	CREATED_AT,
	UPDATED_AT`

const selectMensajeBase = `SELECT` + mensajeCols + `
FROM MSP_RX_MENSAJES`

const selectMensajePendientes = selectMensajeBase + `
WHERE ESTADO = 'encolado'
ORDER BY ENCOLADO_EN ASC
ROWS ?`

const selectMensajePendientesSinLimite = selectMensajeBase + `
WHERE ESTADO = 'encolado'
ORDER BY ENCOLADO_EN ASC`

const updateMensaje = `
UPDATE MSP_RX_MENSAJES SET
  ESTADO = ?, SENDER_KIND = ?, ENVIADO_EN = ?, ERROR = ?, UPDATED_AT = ?
WHERE ID = ?`

const selectContarEnviadosHoy = `
SELECT COUNT(*) FROM MSP_RX_MENSAJES
WHERE ESTADO = 'enviado' AND ENVIADO_EN >= ?`

const selectClientesConMensaje = `SELECT DISTINCT CLIENTE_ID FROM MSP_RX_MENSAJES`

const updateCohorteMarcarContactado = `
UPDATE MSP_RX_COHORTE SET FUE_CONTACTADO = 1, UPDATED_AT = ?
WHERE CLIENTE_ID = ?`
