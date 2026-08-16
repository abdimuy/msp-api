-- ============================================================================
-- Migración 000056: subir el epoch de sync (ventas y pagos) tras cambiar la
--                   ventana que el servidor proyecta
-- ============================================================================
--
-- Por qué:
--   El WHERE de /v2/cobranza/sync/ventas/zona/{id} y de
--   /v2/cobranza/sync/pagos/zona/{id} cambió: los pagos dejaron de filtrarse
--   por su propia FECHA y ahora derivan de la venta
--   (MSP_SALDOS_VENTAS.FECHA_ULT_PAGO >= desde), y las ventas saldadas dentro
--   de la ventana volvieron a viajar. Filas que antes NO calificaban ahora sí
--   califican.
--
--   Ese cambio no mueve ningún UPDATED_AT: las filas de MSP_SALDOS_VENTAS y
--   MSP_PAGOS_VENTAS son exactamente las mismas de ayer. Como el sync es
--   incremental por cursor sobre UPDATED_AT, todas quedan POR DEBAJO del
--   cursor que cada teléfono ya guardó y no llegarían nunca — el escenario
--   que la migración 000055 describe palabra por palabra: "cuando cambia lo
--   que el SERVIDOR PROYECTA — no la fila subyacente — las filas ya guardadas
--   en el teléfono nunca se vuelven a bajar".
--
--   Subir el epoch hace que el cliente vea un sync_epoch mayor al que tiene
--   guardado, borre su cursor y resincronice desde cero. Es la palanca que
--   000055 creó para exactamente este caso.
--
-- Por qué migración y no un UPDATE a mano:
--   El repo aplica los .sql por MSP_MIGRATIONS (make fb-migrate-up), así que
--   una migración es lo único que garantiza que el bump ocurra UNA vez en
--   cada entorno —dev, test y producción— y quede registrado junto al cambio
--   de código que lo motiva. Un UPDATE manual se aplica donde alguien se
--   acuerde: el día que producción recibe el binario nuevo sin el bump, los
--   teléfonos se quedan sin las filas y el defecto se ve "arreglado en el
--   servidor y roto en campo".
--
--   Los bumps posteriores por incidente sí pueden ser un UPDATE manual: esos
--   son operación, no despliegue.
--
-- Recursos afectados: los DOS canales cursor-based que existen ('ventas' y
--   'pagos', el conjunto canónico que fija domain.RecursoSync). No hay un
--   recurso 'saldos': el sync de saldos crudos (/sync/saldos/zona) no lleva
--   filtro de ventana y no cambió; lo que la app consume como saldos es
--   /sync/ventas/zona, que es el recurso 'ventas'.
--
-- Filas: solo las globales (ZONA_CLIENTE_ID = 0). El cambio es del proyector,
--   no de una zona en particular, así que tiene que alcanzar a todas: el
--   epoch efectivo de cualquier zona es global + zona (000055).
--
-- EPOCH + 1 en vez de un literal: el epoch nunca debe retroceder — un valor
--   menor al que el teléfono guardó pierde el forzado anterior. Sumar uno es
--   monótono aunque alguien ya haya subido estas filas a mano por un
--   incidente previo. La migración corre una sola vez (MSP_MIGRATIONS la
--   registra), así que no hay riesgo de doble bump.
--
-- CLAUDE.md §1: sin trigger, sin generador, sin DEFAULT. UPDATED_AT va como
--   literal explícito, igual que las filas semilla de 000055.
-- ============================================================================

UPDATE MSP_CFG_SYNC_EPOCH
   SET EPOCH      = EPOCH + 1,
       MOTIVO     = 'mig 000056: ventana de sync derivada de la venta (FECHA_ULT_PAGO) en sync/digest/by-ids',
       UPDATED_AT = CAST('2026-08-15 00:00:00' AS TIMESTAMP)
 WHERE RECURSO = 'ventas'
   AND ZONA_CLIENTE_ID = 0;

UPDATE MSP_CFG_SYNC_EPOCH
   SET EPOCH      = EPOCH + 1,
       MOTIVO     = 'mig 000056: ventana de sync derivada de la venta (FECHA_ULT_PAGO) en sync/digest/by-ids',
       UPDATED_AT = CAST('2026-08-15 00:00:00' AS TIMESTAMP)
 WHERE RECURSO = 'pagos'
   AND ZONA_CLIENTE_ID = 0;
COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (56, '000056_bump_sync_epoch_ventana_desde', CURRENT_TIMESTAMP);
COMMIT;
