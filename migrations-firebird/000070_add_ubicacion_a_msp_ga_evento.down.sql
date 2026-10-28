-- ============================================================================
-- Rollback de la migración 000070: MSP_GA_EVENTO.UBICACION_DESDE / _HASTA
-- ============================================================================
-- Orden inverso al up. Es destructivo por definición —se pierde el rastro de
-- ubicación de cada evento—, pero no hay nada que preservar: el dato sólo
-- existe para que la línea de tiempo cuente dónde estuvo el mueble.
--
-- Aquí no hace falta DROP INDEX ni DROP CONSTRAINT: nadie indexa por estas
-- columnas y no participan en ninguna FK (a diferencia del down de la 000057,
-- donde la columna sí estaba en un índice y en una FK).
--
-- OJO con el binario: si el código que escribe UBICACION_DESDE/_HASTA sigue
-- vivo, cualquier evento que mueva la ubicación fallará con "column unknown".
-- Primero se revierte el binario, después la columna.
-- ============================================================================

ALTER TABLE MSP_GA_EVENTO DROP UBICACION_HASTA;
COMMIT;

ALTER TABLE MSP_GA_EVENTO DROP UBICACION_DESDE;
COMMIT;

DELETE FROM MSP_MIGRATIONS WHERE ID = 70;
COMMIT;
