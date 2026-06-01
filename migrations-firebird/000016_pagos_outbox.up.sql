-- ============================================================================
-- Migración 000016: outbox para MSP_PAGOS_RECIBIDOS + MSP_PAGOS_IMAGENES
-- ============================================================================
-- Hoy MSP_PAGOS_RECIBIDOS es bridge mínimo (ID, DOCTO_CC_ID, FECHA): la app
-- legacy escribe ahí AL FINAL del flujo, después de los 5 INSERTs a Microsip
-- (DOCTOS_CC + IMPORTES_DOCTOS_CC + FORMAS_COBRO_DOCTOS), únicamente para que
-- el trigger MSP_PAGOS_RECIBIDOS_AIUD (migración 000013) actualice
-- MSP_PAGOS_VENTAS con la FECHA real del pago.
--
-- Esta migración evoluciona la tabla en un outbox completo: el server v2
-- inserta primero la fila con ESTADO='P' (pendiente) y todos los datos
-- necesarios para reintentar la aplicación a Microsip si el writer falla, y
-- la marca como ESTADO='A' tras aplicar exitosamente (con DOCTO_CC_ID,
-- IMPTE_DOCTO_CC_ID y FOLIO populados). El worker PagoRetryWorker drena los
-- pendientes con backoff exponencial.
--
-- Mientras tanto el legacy Node sigue funcionando: solo inserta los 3 campos
-- originales — el resto quedan NULL en sus filas, y ESTADO se backfillea a
-- 'A' (post-aplicada) porque las únicas filas que hoy existen llegan después
-- de un INSERT exitoso a Microsip.
--
-- También crea MSP_PAGOS_IMAGENES para guardar comprobantes (transferencia
-- SPEI en PDF, fotos de cheques, etc.). Paralela a MSP_VENTAS_IMAGENES de
-- la migración 000002.
-- ============================================================================

-- ─── MSP_PAGOS_RECIBIDOS: nuevas columnas ─────────────────────────────────────
-- Una ALTER por columna; Firebird tolera multi-ADD pero el formato uno-por-uno
-- es el adoptado en migraciones anteriores (000004/000005) y deja diagnósticos
-- más claros si una columna falla individualmente.

ALTER TABLE MSP_PAGOS_RECIBIDOS ADD CARGO_DOCTO_CC_ID INTEGER;
ALTER TABLE MSP_PAGOS_RECIBIDOS ADD CLIENTE_ID        INTEGER;
ALTER TABLE MSP_PAGOS_RECIBIDOS ADD COBRADOR_ID       INTEGER;
ALTER TABLE MSP_PAGOS_RECIBIDOS ADD COBRADOR          VARCHAR(100) CHARACTER SET UTF8;
ALTER TABLE MSP_PAGOS_RECIBIDOS ADD IMPORTE           NUMERIC(14,2);
ALTER TABLE MSP_PAGOS_RECIBIDOS ADD FORMA_COBRO_ID    INTEGER;
ALTER TABLE MSP_PAGOS_RECIBIDOS ADD CONCEPTO_CC_ID    INTEGER;
ALTER TABLE MSP_PAGOS_RECIBIDOS ADD LAT               VARCHAR(20);
ALTER TABLE MSP_PAGOS_RECIBIDOS ADD LON               VARCHAR(20);
ALTER TABLE MSP_PAGOS_RECIBIDOS ADD ESTADO            CHAR(1) CHARACTER SET ASCII;
ALTER TABLE MSP_PAGOS_RECIBIDOS ADD INTENTOS          INTEGER;
ALTER TABLE MSP_PAGOS_RECIBIDOS ADD ULTIMO_ERROR      VARCHAR(500) CHARACTER SET UTF8;
ALTER TABLE MSP_PAGOS_RECIBIDOS ADD IMPTE_DOCTO_CC_ID INTEGER;
ALTER TABLE MSP_PAGOS_RECIBIDOS ADD FOLIO             VARCHAR(20) CHARACTER SET ASCII;
ALTER TABLE MSP_PAGOS_RECIBIDOS ADD RECEIVED_AT       TIMESTAMP;
ALTER TABLE MSP_PAGOS_RECIBIDOS ADD APLICADO_AT       TIMESTAMP;
ALTER TABLE MSP_PAGOS_RECIBIDOS ADD CREATED_BY        VARCHAR(36) CHARACTER SET ASCII;
ALTER TABLE MSP_PAGOS_RECIBIDOS ADD UPDATED_BY        VARCHAR(36) CHARACTER SET ASCII;
ALTER TABLE MSP_PAGOS_RECIBIDOS ADD UPDATED_AT        TIMESTAMP;

COMMIT;

-- ─── Backfill: filas pre-existentes ya estaban aplicadas ─────────────────────
-- El legacy Node solo INSERTaba en MSP_PAGOS_RECIBIDOS al final de la tx
-- exitosa contra Microsip, así que toda fila existente en la tabla refleja un
-- pago ya aplicado.
--
-- CARGO_DOCTO_CC_ID etc. quedan NULL: para filas históricas la info viaja por
-- DOCTOS_CC/IMPORTES_DOCTOS_CC y MSP_PAGOS_VENTAS — no se reusan en el outbox.
-- ESTADO='A' garantiza que el retry worker no las procese.

UPDATE MSP_PAGOS_RECIBIDOS
   SET ESTADO       = 'A',
       INTENTOS     = 0,
       RECEIVED_AT  = FECHA,
       APLICADO_AT  = FECHA,
       UPDATED_AT   = FECHA
 WHERE ESTADO IS NULL;

COMMIT;

-- ─── Constraints post-backfill ────────────────────────────────────────────────
-- ESTADO, INTENTOS, RECEIVED_AT, UPDATED_AT son obligatorios. El resto
-- (incluyendo CARGO_DOCTO_CC_ID/CLIENTE_ID/IMPORTE) queda nullable para
-- compatibilizar con las filas backfilleadas. El domain Go fuerza no-NULL
-- en la creación de nuevas filas.
--
-- DOCTO_CC_ID se vuelve NULLABLE: las filas pendientes (ESTADO='P') aún no
-- tienen DOCTO_CC_ID — se asigna al aplicar.

ALTER TABLE MSP_PAGOS_RECIBIDOS ALTER COLUMN ESTADO        SET NOT NULL;
ALTER TABLE MSP_PAGOS_RECIBIDOS ALTER COLUMN INTENTOS      SET NOT NULL;
ALTER TABLE MSP_PAGOS_RECIBIDOS ALTER COLUMN RECEIVED_AT   SET NOT NULL;
ALTER TABLE MSP_PAGOS_RECIBIDOS ALTER COLUMN UPDATED_AT    SET NOT NULL;
ALTER TABLE MSP_PAGOS_RECIBIDOS ALTER COLUMN DOCTO_CC_ID   DROP NOT NULL;

COMMIT;

-- ─── Índices para queries del outbox ─────────────────────────────────────────
-- ESTADO: filtro principal del worker (WHERE ESTADO='P' AND INTENTOS<N).
-- CARGO_DOCTO_CC_ID: query "ya pagué este cargo?" para idempotencia multi-device.
-- (ESTADO, INTENTOS): cover-index del worker para evitar full-scan.

CREATE INDEX IDX_MSP_PAGOS_REC_ESTADO ON MSP_PAGOS_RECIBIDOS (ESTADO);
CREATE INDEX IDX_MSP_PAGOS_REC_CARGO  ON MSP_PAGOS_RECIBIDOS (CARGO_DOCTO_CC_ID);
CREATE INDEX IDX_MSP_PAGOS_REC_PEND   ON MSP_PAGOS_RECIBIDOS (ESTADO, INTENTOS);

COMMIT;

-- ─── MSP_PAGOS_IMAGENES: paralela a MSP_VENTAS_IMAGENES ──────────────────────
-- Almacena referencias a comprobantes (recibos de transferencia bancaria,
-- fotos del cheque, fotos del pago en efectivo). El blob vive en disco bajo
-- STORAGE_DIR; aquí solo guardamos la metadata.
--
-- DESCRIPCION es opcional (texto libre, "Recibo SPEI BBVA", etc.).
-- STORAGE_KIND es enum textual; hoy solo 'FILESYSTEM'.

CREATE TABLE MSP_PAGOS_IMAGENES (
  ID            VARCHAR(36)   CHARACTER SET ASCII NOT NULL,
  PAGO_ID       VARCHAR(36)   CHARACTER SET ASCII NOT NULL,
  STORAGE_KIND  VARCHAR(20)   CHARACTER SET ASCII NOT NULL,
  STORAGE_KEY   VARCHAR(500)  CHARACTER SET ASCII NOT NULL,
  MIME          VARCHAR(50)   CHARACTER SET ASCII NOT NULL,
  SIZE_BYTES    BIGINT        NOT NULL,
  DESCRIPCION   VARCHAR(200)  CHARACTER SET UTF8,
  CREATED_AT    TIMESTAMP     NOT NULL,
  UPDATED_AT    TIMESTAMP     NOT NULL,
  CREATED_BY    VARCHAR(36)   CHARACTER SET ASCII NOT NULL,
  UPDATED_BY    VARCHAR(36)   CHARACTER SET ASCII NOT NULL,
  CONSTRAINT PK_MSP_PAGOS_IMAGENES PRIMARY KEY (ID),
  CONSTRAINT FK_MSP_PAGOS_IMG_PAGO FOREIGN KEY (PAGO_ID)
    REFERENCES MSP_PAGOS_RECIBIDOS(ID) ON DELETE CASCADE
);

COMMIT;

CREATE INDEX IDX_MSP_PAGOS_IMG_PAGO ON MSP_PAGOS_IMAGENES (PAGO_ID);

COMMIT;

-- ─── Registro ────────────────────────────────────────────────────────────────
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (16, '000016_pagos_outbox', CURRENT_TIMESTAMP);

COMMIT;
