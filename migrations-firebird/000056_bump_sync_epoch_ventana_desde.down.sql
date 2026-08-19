-- Down para 000056_bump_sync_epoch_ventana_desde.
--
-- Baja el epoch de las dos filas globales al valor previo. Es la única
-- operación que revierte el efecto, pero OJO: decrementar un epoch es
-- semánticamente peligroso —un teléfono que ya resincronizó guarda el valor
-- alto, y bajarlo hace que un bump futuro al mismo número pase inadvertido—.
-- Solo tiene sentido inmediatamente después de aplicar la up y antes de que
-- ningún cliente haya sincronizado.
--
-- La alternativa segura, si ya hubo clientes sincronizando, es NO correr este
-- down: dejar el epoch arriba (una resincronización de más no rompe nada) y
-- revertir únicamente el código.

UPDATE MSP_CFG_SYNC_EPOCH
   SET EPOCH      = EPOCH - 1,
       MOTIVO     = 'rollback de la migracion 000056',
       UPDATED_AT = CAST('2026-08-15 00:00:00' AS TIMESTAMP)
 WHERE RECURSO IN ('ventas', 'pagos')
   AND ZONA_CLIENTE_ID = 0
   AND EPOCH > 0;
COMMIT;

DELETE FROM MSP_MIGRATIONS WHERE ID = 56;
COMMIT;
