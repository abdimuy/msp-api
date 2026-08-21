-- Down para 000058_bump_sync_epoch_tombstones_huerfanos.
--
-- OJO: decrementar un epoch es semánticamente peligroso. Un teléfono que ya
-- resincronizó guarda el valor alto, y bajarlo hace que un bump futuro al
-- mismo número pase inadvertido. Sólo tiene sentido inmediatamente después de
-- aplicar la up y antes de que ningún cliente haya sincronizado.
--
-- La alternativa segura, si ya hubo clientes sincronizando, es NO correr este
-- down: dejar el epoch arriba —una resincronización de más no rompe nada— y
-- revertir únicamente el código.
--
-- Qué filas toca: las que la up marcó con su MOTIVO. Es lo único determinista
-- disponible — el conjunto de lápidas huérfanas cambia entre la up y la down,
-- así que recalcular la medición aquí bajaría un conjunto distinto del que se
-- subió. Si alguien edita el MOTIVO de la up, tiene que editar este WHERE.
--
-- Las filas que la up SEMBRÓ (paso A, MOTIVO 'semilla de zona…') se quedan.
-- Una fila de zona con EPOCH 0 es indistinguible de no tener fila: el epoch
-- efectivo es global + zona, y sumar cero no cambia nada.

UPDATE MSP_CFG_SYNC_EPOCH
   SET EPOCH      = EPOCH - 1,
       MOTIVO     = 'rollback de la migracion 000058',
       UPDATED_AT = CAST('2026-08-20 00:00:00' AS TIMESTAMP)
 WHERE RECURSO = 'ventas'
   AND ZONA_CLIENTE_ID <> 0
   AND MOTIVO = 'mig 000058: la venta borrada en Microsip vuelve a salir por el sync'
   AND EPOCH > 0;
COMMIT;

DELETE FROM MSP_MIGRATIONS WHERE ID = 58;
COMMIT;
