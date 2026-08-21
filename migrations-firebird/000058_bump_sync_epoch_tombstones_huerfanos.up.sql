-- ============================================================================
-- Migración 000058: subir el epoch de las zonas con lápidas huérfanas
-- ============================================================================
--
-- Por qué:
--   El WHERE de /v2/cobranza/sync/ventas/zona/{id} y de /sync/saldos/by-ids
--   cambió: la rama de cancelados de ventaStatusFilterConVentana ganó el caso
--   del BORRADO FÍSICO (NOT EXISTS sobre DOCTOS_CC). Filas que antes no
--   calificaban ahora sí: las lápidas que dejó el trigger
--   MSP_SALDOS_DOCTOS_CC_AD (migración 000020) cuando oficina eliminó una
--   venta en Microsip.
--
--   Ese cambio no mueve ningún UPDATED_AT — las filas de MSP_SALDOS_VENTAS
--   son exactamente las mismas de ayer— así que quedan POR DEBAJO del cursor
--   que cada teléfono ya guardó y no llegarían nunca. Subir el epoch es la
--   palanca que 000055 creó para exactamente este caso (COBRANZA-SYNC.md §3:
--   "siempre que cambie el WHERE de lo que se entrega, aunque no se mueva
--   ningún UPDATED_AT").
--
-- Por qué POR ZONA y no la fila global (a diferencia de 000056):
--   Subir el epoch mete a cada teléfono de la zona en un replay completo, y
--   un teléfono que no persiste AFTER_ID no puede terminarlo: se queda
--   re-descargando por ciclo (COBRANZA-SYNC.md §8, restricción D1). El
--   arreglo del predicado ya viaja en el binario y limpia solo los borrados
--   NUEVOS sin costar una sola re-descarga; el epoch es únicamente para el
--   rezago, y el rezago vive en unas pocas zonas. No hay razón para pagar un
--   replay de la flota entera.
--
-- Por qué las zonas se calculan aquí y no van escritas a mano:
--   El conjunto cambia solo. El análisis del 2026-08-20 midió 29 lápidas en
--   11 zonas, todas de la semana del cutover; para cuando esta migración se
--   aplique en producción el conjunto será otro. Una lista transcrita el día
--   del análisis se aplica el día del despliegue: llega tarde por definición
--   y además admite errores de transcripción. La medición va en la propia
--   sentencia, con la MISMA condición que el predicado del sync usa para
--   decidir qué entrega.
--
--   Consulta equivalente, para correr antes y ver qué va a tocar
--   (docs/ops/lapidas-huerfanas-despliegue.md):
--
--     SELECT s.ZONA_CLIENTE_ID, COUNT(*) FROM MSP_SALDOS_VENTAS s
--     WHERE s.CARGO_CANCELADO = 'S'
--       AND NOT EXISTS (SELECT 1 FROM DOCTOS_CC d
--                       WHERE d.DOCTO_CC_ID = s.DOCTO_CC_ID)
--     GROUP BY 1 ORDER BY 2 DESC;
--
-- La palanca manual — zonas excluidas por adopción de AFTER_ID:
--   Las DOS sentencias llevan `AND s.ZONA_CLIENTE_ID NOT IN (0)`. El 0 es la
--   fila global de MSP_CFG_SYNC_EPOCH y nunca es una zona real de Microsip,
--   así que tal como está la lista de exclusión está VACÍA. Si el paso 2 del
--   despliegue encuentra una zona con teléfonos por debajo de la versión que
--   introdujo AFTER_ID, se agrega su id a las dos listas ANTES de aplicar.
--   Esa zona se bumpea después, cuando termine de adoptar; mientras tanto sus
--   borrados nuevos ya se limpian solos por el binario.
--
-- Por qué se exige además el filtro de cliente:
--   Una lápida cuyo cliente no está en 'A' o no tiene domicilio principal no
--   viaja igual (ventaClienteFilter la descarta en los cinco canales), así
--   que bumpear su zona sería un replay de toda la zona a cambio de nada. Las
--   dos condiciones de abajo son las mismas de ventaClienteFilter, palabra
--   por palabra.
--
-- Zonas nulas: excluidas. El sync es por zona (WHERE s.ZONA_CLIENTE_ID = ?),
--   así que una lápida sin zona no viaja por ningún canal, con o sin este
--   arreglo, y no hay epoch que subir.
--
-- CLAUDE.md §1: sin trigger, sin generador, sin DEFAULT, sin procedimiento.
--   Dos sentencias de datos, de un solo tiro, con UPDATED_AT literal.
-- ============================================================================

-- ─── Paso A: sembrar la fila de epoch de cada zona afectada ──────────────────
-- MSP_CFG_SYNC_EPOCH sólo trae las dos filas globales de 000055. El epoch
-- efectivo es global + zona (000055), así que una zona sin fila cuenta como 0
-- y el UPDATE del paso B no tendría nada que subir. Se siembra en 0 para que
-- el paso B haga el único incremento: así el bump es de exactamente uno,
-- corra esta migración sobre una base que ya tenía la fila o sobre una que no.
INSERT INTO MSP_CFG_SYNC_EPOCH (RECURSO, ZONA_CLIENTE_ID, EPOCH, MOTIVO, UPDATED_AT)
SELECT DISTINCT
       'ventas', s.ZONA_CLIENTE_ID, 0,
       'mig 000058: semilla de zona con lapidas huerfanas',
       CAST('2026-08-20 00:00:00' AS TIMESTAMP)
FROM MSP_SALDOS_VENTAS s
WHERE s.CARGO_CANCELADO = 'S'
  AND s.ZONA_CLIENTE_ID IS NOT NULL
  AND s.ZONA_CLIENTE_ID NOT IN (0)
  AND NOT EXISTS (SELECT 1 FROM DOCTOS_CC d
                  WHERE d.DOCTO_CC_ID = s.DOCTO_CC_ID)
  AND EXISTS (SELECT 1 FROM CLIENTES cf
              WHERE cf.CLIENTE_ID = s.CLIENTE_ID AND cf.ESTATUS = 'A')
  AND EXISTS (SELECT 1 FROM DIRS_CLIENTES df
              WHERE df.CLIENTE_ID = s.CLIENTE_ID AND df.ES_DIR_PPAL = 'S')
  AND NOT EXISTS (SELECT 1 FROM MSP_CFG_SYNC_EPOCH e
                  WHERE e.RECURSO = 'ventas'
                    AND e.ZONA_CLIENTE_ID = s.ZONA_CLIENTE_ID);
COMMIT;

-- ─── Paso B: subir el epoch de esas mismas zonas ─────────────────────────────
-- EPOCH + 1 y no un literal: el epoch nunca debe retroceder — un valor menor
-- al que el teléfono guardó pierde el forzado anterior. Sumar uno es monótono
-- aunque alguien ya haya subido estas filas a mano por un incidente previo.
-- MSP_MIGRATIONS garantiza que corra una sola vez.
--
-- El MOTIVO es además la marca por la que el down sabe qué filas tocó: no se
-- edita sin editar también el down.
UPDATE MSP_CFG_SYNC_EPOCH e
   SET EPOCH      = e.EPOCH + 1,
       MOTIVO     = 'mig 000058: la venta borrada en Microsip vuelve a salir por el sync',
       UPDATED_AT = CAST('2026-08-20 00:00:00' AS TIMESTAMP)
 WHERE e.RECURSO = 'ventas'
   AND e.ZONA_CLIENTE_ID NOT IN (0)
   AND EXISTS (
         SELECT 1 FROM MSP_SALDOS_VENTAS s
         WHERE s.ZONA_CLIENTE_ID = e.ZONA_CLIENTE_ID
           AND s.CARGO_CANCELADO = 'S'
           AND NOT EXISTS (SELECT 1 FROM DOCTOS_CC d
                           WHERE d.DOCTO_CC_ID = s.DOCTO_CC_ID)
           AND EXISTS (SELECT 1 FROM CLIENTES cf
                       WHERE cf.CLIENTE_ID = s.CLIENTE_ID AND cf.ESTATUS = 'A')
           AND EXISTS (SELECT 1 FROM DIRS_CLIENTES df
                       WHERE df.CLIENTE_ID = s.CLIENTE_ID AND df.ES_DIR_PPAL = 'S')
       );
COMMIT;

-- ─── Registro ────────────────────────────────────────────────────────────────
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (58, '000058_bump_sync_epoch_tombstones_huerfanos', CURRENT_TIMESTAMP);
COMMIT;
