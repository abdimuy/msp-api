-- ============================================================================
-- Migración 000055: MSP_CFG_SYNC_EPOCH — generación (epoch) de sincronización
-- ============================================================================
--
-- Por qué:
--   El sync de cobranza (/v2/cobranza/sync/ventas/zona/{id} y
--   /v2/cobranza/sync/pagos/zona/{id}) es incremental por cursor sobre
--   UPDATED_AT. Cuando cambia lo que el SERVIDOR PROYECTA — no la fila
--   subyacente — las filas ya guardadas en el teléfono nunca se vuelven a
--   bajar, porque su UPDATED_AT no se movió. Ejemplo real: las coordenadas
--   (LAT/LON) de los pagos pasaron a leerse de DOCTOS_CC en vez de la caché
--   MSP_PAGOS_VENTAS; todos los pagos ya sincronizados quedaron con las
--   coordenadas viejas en el dispositivo.
--
--   Hasta ahora eso se resolvía con marcadores hardcodeados en la app
--   (MIGRATION_PAGO_RECIBIDO_ID, etc.): un APK nuevo por incidente. Esta
--   tabla mueve esa palanca al servidor. El API expone el epoch efectivo
--   como `sync_epoch` en cada página de sync; cuando el cliente ve un
--   sync_epoch mayor al que tiene guardado, borra su cursor y resincroniza
--   desde cero. Forzar una resincronización pasa a ser un UPDATE.
--
-- Semántica del epoch efectivo (calculado en Go, NO en la base):
--
--   epoch_efectivo(recurso, zona) = EPOCH de (recurso, zona)
--                                 + EPOCH de (recurso, 0)
--
--   tomando 0 cuando la fila no existe. La fila con ZONA_CLIENTE_ID = 0 es
--   la GLOBAL (0 nunca es una zona real de Microsip: las zonas viven en
--   ZONAS_CLIENTES con IDs como 12271). Así:
--     - UPDATE de la fila global  → mueve el epoch de TODAS las zonas.
--     - UPDATE de una fila de zona → mueve el epoch de ESA zona solamente.
--   La suma es monótona creciente mientras los valores solo se incrementen,
--   que es el único uso previsto (nunca se decrementa un epoch: eso haría
--   retroceder el marcador del cliente y perdería el forzado anterior).
--
-- Columnas:
--   RECURSO          VARCHAR(20) ASCII  'ventas' | 'pagos'. ASCII porque son
--                                        identificadores del contrato, no
--                                        texto de usuario.
--   ZONA_CLIENTE_ID  INTEGER            zona del cliente; 0 = fila global.
--   EPOCH            INTEGER            contador de generación.
--   MOTIVO           VARCHAR(200) UTF8  por qué se forzó (texto humano, para
--                                        auditoría/postmortem). Nullable.
--   UPDATED_AT       TIMESTAMP          cuándo se movió por última vez.
--
-- Restricciones (CLAUDE.md §1 — nada de lógica en la base):
--   Sin DEFAULT, sin trigger, sin generador, sin procedimiento, sin CHECK de
--   negocio. Los INSERT/UPDATE que haga Go pasan EPOCH y UPDATED_AT
--   explícitos; las filas semilla de abajo llevan un literal de timestamp
--   explícito, no CURRENT_TIMESTAMP.
--
--   No se otorgan GRANTs a PUBLIC (cf. 000051): ningún trigger de Microsip
--   toca esta tabla — la lee únicamente nuestro API, que se conecta como
--   SYSDBA.
-- ============================================================================

CREATE TABLE MSP_CFG_SYNC_EPOCH (
  RECURSO          VARCHAR(20)   CHARACTER SET ASCII  NOT NULL,
  ZONA_CLIENTE_ID  INTEGER                            NOT NULL,
  EPOCH            INTEGER                            NOT NULL,
  MOTIVO           VARCHAR(200)  CHARACTER SET UTF8,
  UPDATED_AT       TIMESTAMP                          NOT NULL,
  CONSTRAINT PK_MSP_CFG_SYNC_EPOCH PRIMARY KEY (RECURSO, ZONA_CLIENTE_ID)
);
COMMIT;

-- Filas globales semilla. EPOCH = 0 es el estado "nunca se forzó nada": el
-- epoch efectivo de cualquier zona arranca en 0, igual que si la tabla
-- estuviera vacía. UPDATED_AT es un literal explícito (nunca
-- CURRENT_TIMESTAMP) para que el valor sembrado sea idéntico en todos los
-- entornos donde se aplique esta migración.
INSERT INTO MSP_CFG_SYNC_EPOCH (RECURSO, ZONA_CLIENTE_ID, EPOCH, MOTIVO, UPDATED_AT)
VALUES ('ventas', 0, 0, 'semilla inicial de la migracion 000055', CAST('2026-08-13 00:00:00' AS TIMESTAMP));

INSERT INTO MSP_CFG_SYNC_EPOCH (RECURSO, ZONA_CLIENTE_ID, EPOCH, MOTIVO, UPDATED_AT)
VALUES ('pagos', 0, 0, 'semilla inicial de la migracion 000055', CAST('2026-08-13 00:00:00' AS TIMESTAMP));
COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (55, '000055_create_msp_cfg_sync_epoch', CURRENT_TIMESTAMP);
COMMIT;
