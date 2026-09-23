-- Revert 000064: eliminar VERSION de MSP_VENTAS.
--
-- Destructivo por definición: se pierde el contador de concurrencia
-- optimista de cada venta. No hay nada más que preservar — la columna sólo
-- existe para el guardia CAS de los UPDATE, y sin ella ese guardia
-- simplemente deja de aplicarse (vuelve al last-write-wins de antes).
--
-- OJO al orden de reversión: el binario que ya trae el guardia CAS escribe
-- `WHERE VERSION = ?` en cada UPDATE de MSP_VENTAS. Correr este down con ese
-- binario vivo hace que TODA edición de una venta falle con "column
-- unknown" o, peor, con ErrVentaVersionConflicto espurio. Primero se
-- revierte el binario, después la columna.

ALTER TABLE MSP_VENTAS DROP VERSION;
COMMIT;

DELETE FROM MSP_MIGRATIONS WHERE ID = 64;
COMMIT;
