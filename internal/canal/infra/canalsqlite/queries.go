package canalsqlite

// mensajeColumns is the column list shared by every SELECT against
// mensajes_entrantes, in the exact order scanMensaje expects.
const mensajeColumns = `
	id, wamid, remitente, phone_number_id, tipo, contenido,
	timestamp_meta, recibido_en, estado, motivo_fallo, created_at, updated_at`

// insertMensajeSQL inserts one row, doing nothing when wamid already exists
// (ux_mensajes_entrantes_wamid). This is the whole of Guardar's idempotency:
// a redelivered webhook affects zero rows instead of erroring or
// duplicating the mailbox entry.
const insertMensajeSQL = `
INSERT INTO mensajes_entrantes (
	id, wamid, remitente, phone_number_id, tipo, contenido,
	timestamp_meta, recibido_en, estado, motivo_fallo, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (wamid) DO NOTHING`

// listarPendientesSQL returns pending rows oldest-first, the order the
// mailbox retries by (RecibidoEn, never TimestampMeta — see
// domain.MensajeEntrante's doc comment).
const listarPendientesSQL = `
SELECT` + mensajeColumns + `
FROM mensajes_entrantes
WHERE estado = ?
ORDER BY recibido_en ASC
LIMIT ?`

// marcarReenviadoSQL moves one row to EstadoReenvioReenviado.
const marcarReenviadoSQL = `
UPDATE mensajes_entrantes
SET estado = ?, updated_at = ?
WHERE id = ?`

// marcarFallidoSQL moves one row to EstadoReenvioFallido, recording motivo.
const marcarFallidoSQL = `
UPDATE mensajes_entrantes
SET estado = ?, motivo_fallo = ?, updated_at = ?
WHERE id = ?`

// contarPendientesSQL counts rows still in EstadoReenvioPendiente.
const contarPendientesSQL = `
SELECT COUNT(*) FROM mensajes_entrantes WHERE estado = ?`
