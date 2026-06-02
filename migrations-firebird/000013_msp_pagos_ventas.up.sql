-- ============================================================================
-- Migración 000013: caché materializado MSP_PAGOS_VENTAS
-- ============================================================================
-- Espejo de MSP_SALDOS_VENTAS pero a nivel de cada IMPORTES_DOCTOS_CC con
-- TIPO_IMPTE='R' (pago). Habilita:
--
--   1. Lecturas O(1) de pagos por venta / cliente / zona+fecha sin pegarle
--      a 3 tablas de Microsip (10-30 ms vs 600-12000 ms en dev).
--   2. Sync incremental por UPDATED_AT desde la app móvil (worker cada 30 s).
--
-- Misma EXCEPCIÓN documentada en 000010: triggers sobre tablas Microsip,
-- POSITION 100, WHEN ANY DO defensivo, errores a MSP_SALDOS_ERRORS (la misma
-- tabla ya existente). El caché es un read-model derivado.
--
-- Histórico: esta migración decidió que los pagos cancelados se borraran
-- de la tabla (a diferencia de saldos). Esa decisión fue revertida por la
-- migración 000019: hoy las cancelaciones de pagos también escriben un
-- tombstone (CANCELADO='S', IMPORTE=0, IMPUESTO=0, UPDATED_AT refrescado)
-- para que el sync incremental por UPDATED_AT pueda propagar la
-- cancelación al cliente móvil. Ver 000019_tombstone_pagos.up.sql.
-- ============================================================================

-- ─── Tabla principal ─────────────────────────────────────────────────────────

CREATE TABLE MSP_PAGOS_VENTAS (
  IMPTE_DOCTO_CC_ID  INTEGER       NOT NULL,                -- PK = id del importe individual
  DOCTO_CC_ID        INTEGER       NOT NULL,                -- header del documento abono
  DOCTO_CC_ACR_ID    INTEGER       NOT NULL,                -- cargo acreditado (= MSP_SALDOS_VENTAS.DOCTO_CC_ID)
  CLIENTE_ID         INTEGER       NOT NULL,
  ZONA_CLIENTE_ID    INTEGER,                               -- denormalizado para filtro O(1)
  FOLIO              VARCHAR(20)   CHARACTER SET ASCII,
  CONCEPTO_CC_ID     INTEGER       NOT NULL,
  FECHA              TIMESTAMP     NOT NULL,                -- COALESCE(MSP_PAGOS_RECIBIDOS.FECHA, DOCTOS_CC.FECHA)
  IMPORTE            NUMERIC(14,2) NOT NULL,
  IMPUESTO           NUMERIC(14,2) NOT NULL,
  LAT                NUMERIC(15,8),                         -- reservado: hoy no hay fuente; placeholder para futuro geofencing
  LON                NUMERIC(15,8),                         -- idem
  CANCELADO          CHAR(1)       CHARACTER SET ASCII NOT NULL,
  APLICADO           CHAR(1)       CHARACTER SET ASCII NOT NULL,
  UPDATED_AT         TIMESTAMP     NOT NULL,                -- KEY para sync incremental
  CONSTRAINT PK_MSP_PAGOS_VENTAS PRIMARY KEY (IMPTE_DOCTO_CC_ID)
);
COMMIT;

-- ─── Índices ─────────────────────────────────────────────────────────────────

CREATE INDEX IDX_MSP_PAGOS_ZONA_FECHA    ON MSP_PAGOS_VENTAS (ZONA_CLIENTE_ID, FECHA);
CREATE INDEX IDX_MSP_PAGOS_ZONA_UPDATED  ON MSP_PAGOS_VENTAS (ZONA_CLIENTE_ID, UPDATED_AT);
CREATE INDEX IDX_MSP_PAGOS_CARGO         ON MSP_PAGOS_VENTAS (DOCTO_CC_ACR_ID);
CREATE INDEX IDX_MSP_PAGOS_CLIENTE_FECHA ON MSP_PAGOS_VENTAS (CLIENTE_ID, FECHA);
COMMIT;

-- ─── Procedimiento central ────────────────────────────────────────────────────
--
-- MSP_RECOMPUTE_PAGO reconstruye 1 fila de MSP_PAGOS_VENTAS desde scratch para
-- el IMPTE_DOCTO_CC_ID dado. Llamado desde:
--   - los triggers de IMPORTES_DOCTOS_CC, DOCTOS_CC, CLIENTES, MSP_PAGOS_RECIBIDOS
--   - el bloque de backfill al final de esta migración
--   - el reconciler semanal del módulo Go (vía outbound.PagosRecomputer)
--
-- Salidas:
--   - DELETE + EXIT si el importe ya no califica (TIPO_IMPTE!='R', CANCELADO='S',
--     no existe el importe, o el header DOCTOS_CC fue cancelado).
--   - UPSERT atómico de la fila si califica.
--
-- En caso de error: registra en MSP_SALDOS_ERRORS (la misma tabla del cache de
-- saldos, ya existe desde 000010) y retorna limpiamente.

SET TERM ^ ;

CREATE PROCEDURE MSP_RECOMPUTE_PAGO (
  IMPTE_ID INTEGER
)
AS
  DECLARE VARIABLE v_docto_cc_id    INTEGER;
  DECLARE VARIABLE v_acr_id         INTEGER;
  DECLARE VARIABLE v_tipo_impte     CHAR(1);
  DECLARE VARIABLE v_cancelado_imp  CHAR(1);
  DECLARE VARIABLE v_aplicado       CHAR(1);
  DECLARE VARIABLE v_importe        NUMERIC(14,2);
  DECLARE VARIABLE v_impuesto       NUMERIC(14,2);
  DECLARE VARIABLE v_cliente_id     INTEGER;
  DECLARE VARIABLE v_folio          VARCHAR(20);
  DECLARE VARIABLE v_concepto_cc_id INTEGER;
  DECLARE VARIABLE v_fecha_dc       DATE;
  DECLARE VARIABLE v_cancelado_dc   CHAR(1);
  DECLARE VARIABLE v_zona_id        INTEGER;
  DECLARE VARIABLE v_fecha_pago     TIMESTAMP;
BEGIN
  -- 1. Leer el importe.
  v_docto_cc_id   = NULL;
  v_acr_id        = NULL;
  v_tipo_impte    = NULL;
  v_cancelado_imp = NULL;
  v_aplicado      = NULL;
  v_importe       = NULL;
  v_impuesto      = NULL;

  SELECT IDC.DOCTO_CC_ID, IDC.DOCTO_CC_ACR_ID, IDC.TIPO_IMPTE, IDC.CANCELADO,
         IDC.APLICADO, IDC.IMPORTE, IDC.IMPUESTO
    FROM IMPORTES_DOCTOS_CC IDC
    WHERE IDC.IMPTE_DOCTO_CC_ID = :IMPTE_ID
    INTO :v_docto_cc_id, :v_acr_id, :v_tipo_impte, :v_cancelado_imp,
         :v_aplicado, :v_importe, :v_impuesto;

  -- Si no existe el importe, o no es pago, o está cancelado, o no tiene cargo
  -- acreditado: borrar del caché y salir.
  IF (v_docto_cc_id IS NULL OR v_tipo_impte <> 'R' OR v_cancelado_imp = 'S'
      OR v_acr_id IS NULL) THEN
  BEGIN
    DELETE FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = :IMPTE_ID;
    EXIT;
  END

  -- 2. Leer header del documento abono (cliente, folio, concepto, fecha).
  SELECT DC.CLIENTE_ID, DC.FOLIO, DC.CONCEPTO_CC_ID, DC.FECHA, DC.CANCELADO
    FROM DOCTOS_CC DC
    WHERE DC.DOCTO_CC_ID = :v_docto_cc_id
    INTO :v_cliente_id, :v_folio, :v_concepto_cc_id, :v_fecha_dc, :v_cancelado_dc;

  -- Si el header del abono fue cancelado, tampoco mostramos el pago.
  IF (v_cancelado_dc = 'S') THEN
  BEGIN
    DELETE FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = :IMPTE_ID;
    EXIT;
  END

  -- 3. Zona actual del cliente (denormalizada para filtro por zona).
  v_zona_id = NULL;
  SELECT C.ZONA_CLIENTE_ID
    FROM CLIENTES C
    WHERE C.CLIENTE_ID = :v_cliente_id
    INTO :v_zona_id;

  -- 4. Fecha del pago: TIMESTAMP de MSP_PAGOS_RECIBIDOS (pago hecho vía app
  --    móvil — captura hora exacta) cuando exista; sino DOCTOS_CC.FECHA (DATE,
  --    precisión de día) casteada a TIMESTAMP. Igual semántica que migración
  --    000012 para FECHA_ULT_PAGO.
  v_fecha_pago = NULL;
  SELECT FIRST 1 P.FECHA
    FROM MSP_PAGOS_RECIBIDOS P
    WHERE P.DOCTO_CC_ID = :v_docto_cc_id
    INTO :v_fecha_pago;

  IF (v_fecha_pago IS NULL) THEN
    v_fecha_pago = CAST(v_fecha_dc AS TIMESTAMP);

  -- 5. Upsert atómico. LAT/LON quedan NULL (no hay fuente todavía).
  UPDATE OR INSERT INTO MSP_PAGOS_VENTAS (
    IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, DOCTO_CC_ACR_ID, CLIENTE_ID, ZONA_CLIENTE_ID,
    FOLIO, CONCEPTO_CC_ID, FECHA, IMPORTE, IMPUESTO, LAT, LON,
    CANCELADO, APLICADO, UPDATED_AT
  ) VALUES (
    :IMPTE_ID, :v_docto_cc_id, :v_acr_id, :v_cliente_id, :v_zona_id,
    :v_folio, :v_concepto_cc_id, :v_fecha_pago, :v_importe, :v_impuesto, NULL, NULL,
    'N', :v_aplicado, CURRENT_TIMESTAMP
  ) MATCHING (IMPTE_DOCTO_CC_ID);

WHEN ANY DO
BEGIN
  INSERT INTO MSP_SALDOS_ERRORS (ERROR_ID, CARGO_ID, ERROR_MSG, ERROR_AT)
  VALUES (
    GEN_ID(GEN_MSP_SALDOS_ERRORS_ID, 1),
    :IMPTE_ID,
    SUBSTRING('MSP_RECOMPUTE_PAGO SQLCODE=' || SQLCODE || ' ' || GDSCODE FROM 1 FOR 500),
    CURRENT_TIMESTAMP
  );
  EXIT;
END
END^

COMMIT^

-- ─── Triggers ────────────────────────────────────────────────────────────────
-- Todos POSITION 100 (después de los triggers nativos de Microsip).
-- Todos usan WHEN ANY DO defensivo. Errores van a MSP_SALDOS_ERRORS.

-- Trigger 1: AFTER INSERT OR UPDATE OR DELETE en IMPORTES_DOCTOS_CC.
--   En INSERT/UPDATE: recomputa NEW.IMPTE_DOCTO_CC_ID (el procedure decide si
--   queda o se borra del caché).
--   En DELETE: borra del caché por OLD.IMPTE_DOCTO_CC_ID.

CREATE TRIGGER MSP_PAGOS_IMPORTES_AIUD
  FOR IMPORTES_DOCTOS_CC
  AFTER INSERT OR UPDATE OR DELETE
  POSITION 100
AS
BEGIN
  IF (INSERTING OR UPDATING) THEN
    EXECUTE PROCEDURE MSP_RECOMPUTE_PAGO(NEW.IMPTE_DOCTO_CC_ID);

  IF (DELETING) THEN
    DELETE FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = OLD.IMPTE_DOCTO_CC_ID;

WHEN ANY DO
BEGIN
  INSERT INTO MSP_SALDOS_ERRORS (ERROR_ID, CARGO_ID, ERROR_MSG, ERROR_AT)
  VALUES (
    GEN_ID(GEN_MSP_SALDOS_ERRORS_ID, 1),
    CASE WHEN DELETING THEN OLD.IMPTE_DOCTO_CC_ID ELSE NEW.IMPTE_DOCTO_CC_ID END,
    SUBSTRING('TRG MSP_PAGOS_IMPORTES_AIUD SQLCODE=' || SQLCODE FROM 1 FOR 500),
    CURRENT_TIMESTAMP
  );
END
END^
COMMIT^

-- Trigger 2: AFTER UPDATE en DOCTOS_CC.
--   Si cambia CANCELADO, CONCEPTO_CC_ID, FOLIO o FECHA del header, hay que
--   recomputar todos los importes 'R' asociados (porque esos campos viven
--   denormalizados en el caché de pagos).

CREATE TRIGGER MSP_PAGOS_DOCTOS_CC_AU
  FOR DOCTOS_CC
  AFTER UPDATE
  POSITION 100
AS
  DECLARE VARIABLE impte_id INTEGER;
BEGIN
  IF (NEW.CANCELADO       IS DISTINCT FROM OLD.CANCELADO
   OR NEW.CONCEPTO_CC_ID  IS DISTINCT FROM OLD.CONCEPTO_CC_ID
   OR NEW.FOLIO           IS DISTINCT FROM OLD.FOLIO
   OR NEW.FECHA           IS DISTINCT FROM OLD.FECHA) THEN
  BEGIN
    FOR SELECT IDC.IMPTE_DOCTO_CC_ID
          FROM IMPORTES_DOCTOS_CC IDC
          WHERE IDC.DOCTO_CC_ID = NEW.DOCTO_CC_ID
            AND IDC.TIPO_IMPTE  = 'R'
          INTO :impte_id
    DO
      EXECUTE PROCEDURE MSP_RECOMPUTE_PAGO(:impte_id);
  END

WHEN ANY DO
BEGIN
  INSERT INTO MSP_SALDOS_ERRORS (ERROR_ID, CARGO_ID, ERROR_MSG, ERROR_AT)
  VALUES (
    GEN_ID(GEN_MSP_SALDOS_ERRORS_ID, 1),
    NEW.DOCTO_CC_ID,
    SUBSTRING('TRG MSP_PAGOS_DOCTOS_CC_AU SQLCODE=' || SQLCODE FROM 1 FOR 500),
    CURRENT_TIMESTAMP
  );
END
END^
COMMIT^

-- Trigger 3: AFTER UPDATE en CLIENTES.
--   Si cambia ZONA_CLIENTE_ID, actualizar el campo denormalizado en el caché
--   por bulk UPDATE (sin recompute — la zona no afecta importe/fecha/cliente).

CREATE TRIGGER MSP_PAGOS_CLIENTES_AU
  FOR CLIENTES
  AFTER UPDATE
  POSITION 100
AS
BEGIN
  IF (NEW.ZONA_CLIENTE_ID IS DISTINCT FROM OLD.ZONA_CLIENTE_ID) THEN
  BEGIN
    UPDATE MSP_PAGOS_VENTAS
       SET ZONA_CLIENTE_ID = NEW.ZONA_CLIENTE_ID,
           UPDATED_AT      = CURRENT_TIMESTAMP
     WHERE CLIENTE_ID = NEW.CLIENTE_ID;
  END

WHEN ANY DO
BEGIN
  INSERT INTO MSP_SALDOS_ERRORS (ERROR_ID, CARGO_ID, ERROR_MSG, ERROR_AT)
  VALUES (
    GEN_ID(GEN_MSP_SALDOS_ERRORS_ID, 1),
    NEW.CLIENTE_ID,
    SUBSTRING('TRG MSP_PAGOS_CLIENTES_AU SQLCODE=' || SQLCODE FROM 1 FOR 500),
    CURRENT_TIMESTAMP
  );
END
END^
COMMIT^

-- Trigger 4: AFTER INSERT OR UPDATE OR DELETE en MSP_PAGOS_RECIBIDOS.
--   Defensa: la app móvil escribe en MSP_PAGOS_RECIBIDOS *antes* o
--   *después* del documento DOCTOS_CC. Si llega después, el FECHA del caché
--   ya está computado con DOCTOS_CC.FECHA y este trigger lo corrige al
--   timestamp más preciso de la app. El reconciler semanal cubre cualquier
--   otro edge case.

CREATE TRIGGER MSP_PAGOS_RECIBIDOS_AIUD
  FOR MSP_PAGOS_RECIBIDOS
  AFTER INSERT OR UPDATE OR DELETE
  POSITION 100
AS
  DECLARE VARIABLE impte_id INTEGER;
  DECLARE VARIABLE target_docto_cc_id INTEGER;
BEGIN
  IF (INSERTING OR UPDATING) THEN
    target_docto_cc_id = NEW.DOCTO_CC_ID;
  ELSE
    target_docto_cc_id = OLD.DOCTO_CC_ID;

  FOR SELECT IDC.IMPTE_DOCTO_CC_ID
        FROM IMPORTES_DOCTOS_CC IDC
        WHERE IDC.DOCTO_CC_ID = :target_docto_cc_id
          AND IDC.TIPO_IMPTE  = 'R'
        INTO :impte_id
  DO
    EXECUTE PROCEDURE MSP_RECOMPUTE_PAGO(:impte_id);

WHEN ANY DO
BEGIN
  INSERT INTO MSP_SALDOS_ERRORS (ERROR_ID, CARGO_ID, ERROR_MSG, ERROR_AT)
  VALUES (
    GEN_ID(GEN_MSP_SALDOS_ERRORS_ID, 1),
    CASE WHEN DELETING THEN OLD.DOCTO_CC_ID ELSE NEW.DOCTO_CC_ID END,
    SUBSTRING('TRG MSP_PAGOS_RECIBIDOS_AIUD SQLCODE=' || SQLCODE FROM 1 FOR 500),
    CURRENT_TIMESTAMP
  );
END
END^
COMMIT^

SET TERM ; ^

-- ─── Backfill inicial ─────────────────────────────────────────────────────────
-- Rellena MSP_PAGOS_VENTAS para todos los importes 'R' activos cuyo
-- DOCTOS_CC tampoco esté cancelado. ~2.17 M rows en dev → ~90 s estimado.
-- Solo se ejecuta una vez por entorno al aplicar esta migración.

SET TERM ^ ;

EXECUTE BLOCK AS
  DECLARE VARIABLE impte_id INTEGER;
BEGIN
  FOR SELECT IDC.IMPTE_DOCTO_CC_ID
        FROM IMPORTES_DOCTOS_CC IDC
        JOIN DOCTOS_CC DC ON DC.DOCTO_CC_ID = IDC.DOCTO_CC_ID
        WHERE IDC.TIPO_IMPTE = 'R'
          AND IDC.CANCELADO  = 'N'
          AND DC.CANCELADO   = 'N'
        INTO :impte_id
  DO
    EXECUTE PROCEDURE MSP_RECOMPUTE_PAGO(:impte_id);
END^

COMMIT^

SET TERM ; ^

-- ─── Registro ────────────────────────────────────────────────────────────────
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (13, '000013_msp_pagos_ventas', CURRENT_TIMESTAMP);
COMMIT;
