-- ============================================================================
-- Migración 000010: caché materializado MSP_SALDOS_VENTAS
-- ============================================================================
-- EXCEPCIÓN DOCUMENTADA a la regla "sin lógica en la BD" de CLAUDE.md:
--
-- La regla aplica únicamente a nuestras tablas operativas MSP_*. Aquí los
-- triggers y el procedimiento operan sobre tablas de Microsip (DOCTOS_CC,
-- IMPORTES_DOCTOS_CC, CLIENTES), siguiendo exactamente el mismo idioma que
-- usa Microsip internamente con AFECTA_SALDOS_CC y familia. Las razones:
--
--   1. Tablas de Microsip, no MSP_*: no rompemos la separación de capas del
--      dominio Go; el caché es un read-model derivado de datos que Microsip
--      ya controla y ya modifica con sus propios triggers.
--
--   2. Idioma AFECTA_SALDOS_CC: Microsip corre cientos de triggers PSQL en
--      estas mismas tablas (DOCTOS_CC, IMPORTES_DOCTOS_CC). Seguir ese
--      idioma es lo idiomático para este motor.
--
--   3. Atomicidad gratis dentro de la tx que disparó el cambio: el saldo
--      queda actualizado en el mismo COMMIT que insertó/modificó el pago.
--      Cero ventana de inconsistencia; no hay polling que sincronizar.
--
--   4. Elimina worker + polling: sin triggers necesitaríamos un worker Go
--      (~600 LOC) que sondeara cambios en IMPORTES_DOCTOS_CC y actualizara
--      la tabla. Con triggers el mismo código de Microsip actualiza el caché
--      de forma transparente.
--
-- Todos los triggers usan WHEN ANY DO { ... } para que un error en el caché
-- NUNCA aborte la transacción del usuario de Microsip. Los errores se
-- registran en MSP_SALDOS_ERRORS para diagnóstico posterior.
-- ============================================================================

-- ─── Tabla principal ─────────────────────────────────────────────────────────

CREATE TABLE MSP_SALDOS_VENTAS (
  DOCTO_CC_ID         INTEGER       NOT NULL,
  DOCTO_PV_ID         INTEGER,                        -- NULL si el cargo no vino de PV
  CLIENTE_ID          INTEGER       NOT NULL,
  ZONA_CLIENTE_ID     INTEGER,                        -- denormalizado para filtros por zona
  FOLIO               VARCHAR(20)   CHARACTER SET ASCII,
  FECHA_CARGO         DATE          NOT NULL,
  PRECIO_TOTAL        NUMERIC(14,2) NOT NULL,         -- importe del cargo (TIPO_IMPTE='C', CANCELADO='N')
  TOTAL_IMPORTE       NUMERIC(14,2) NOT NULL,         -- pagos activos: conceptos 87327, 155, 11
  IMPTE_REST          NUMERIC(14,2) NOT NULL,         -- enganches, condonaciones, mal cliente, fugas
  NUM_PAGOS           INTEGER       NOT NULL,
  FECHA_ULT_PAGO      DATE,                           -- NULL si sin pagos; KEY para filtro "en ruta"
  SALDO               NUMERIC(14,2) NOT NULL,         -- = PRECIO_TOTAL - TOTAL_IMPORTE - IMPTE_REST
  CARGO_CANCELADO     CHAR(1)       CHARACTER SET ASCII NOT NULL,  -- 'S' o 'N'
  UPDATED_AT          TIMESTAMP     NOT NULL,
  CONSTRAINT PK_MSP_SALDOS_VENTAS PRIMARY KEY (DOCTO_CC_ID)
);
COMMIT;

-- ─── Índices ─────────────────────────────────────────────────────────────────

CREATE INDEX IDX_MSP_SALDOS_PV          ON MSP_SALDOS_VENTAS (DOCTO_PV_ID);
CREATE INDEX IDX_MSP_SALDOS_ZONA_SALDO  ON MSP_SALDOS_VENTAS (ZONA_CLIENTE_ID, SALDO);
CREATE INDEX IDX_MSP_SALDOS_ZONA_FUP    ON MSP_SALDOS_VENTAS (ZONA_CLIENTE_ID, FECHA_ULT_PAGO);
CREATE INDEX IDX_MSP_SALDOS_CLIENTE     ON MSP_SALDOS_VENTAS (CLIENTE_ID, SALDO);
CREATE INDEX IDX_MSP_SALDOS_FECHA_CARGO ON MSP_SALDOS_VENTAS (FECHA_CARGO);
COMMIT;

-- ─── Tabla de errores del caché ───────────────────────────────────────────────

CREATE TABLE MSP_SALDOS_ERRORS (
  ERROR_ID    INTEGER       NOT NULL,
  CARGO_ID    INTEGER       NOT NULL,
  ERROR_MSG   VARCHAR(500)  CHARACTER SET UTF8,
  ERROR_AT    TIMESTAMP     NOT NULL,
  CONSTRAINT PK_MSP_SALDOS_ERRORS PRIMARY KEY (ERROR_ID)
);
COMMIT;

CREATE GENERATOR GEN_MSP_SALDOS_ERRORS_ID;
COMMIT;

-- ─── Procedimiento central ────────────────────────────────────────────────────
--
-- MSP_RECOMPUTE_SALDO_VENTA es la única autoridad para calcular el saldo de
-- un cargo (DOCTO_CC con NATURALEZA_CONCEPTO='C'). Se llama desde los triggers
-- y desde el bloque de backfill al final de esta migración.
--
-- Conceptos de cobranza activos incluidos en TOTAL_IMPORTE:
--   87327 — Cobranza en ruta    (cobrador visita al cliente)
--   155   — Cobro en mostrador  (cliente paga en tienda)
--   11    — Cobro               (auto-pago contado que Microsip genera en
--                                cascada; diverge del Node original que solo
--                                usaba 87327 y 155, pero concepto 11 es el
--                                que Microsip usa para saldar ventas de contado
--                                de forma automática — ignorarlo produciría
--                                saldos inflados en ventas al contado)
--
-- Cualquier otro concepto en TIPO_IMPTE='R' (enganches viejos, condonaciones,
-- mal cliente, fugas) va a IMPTE_REST; no se excluyen del saldo total pero sí
-- se segrega para el dashboard.
--
-- En caso de error: registra en MSP_SALDOS_ERRORS y retorna limpiamente
-- (WHEN ANY DO / EXIT). Nunca relanza — no debe abortar la tx del usuario.

CREATE PROCEDURE MSP_RECOMPUTE_SALDO_VENTA (
  CARGO_ID INTEGER
)
AS
  DECLARE VARIABLE v_cliente_id     INTEGER;
  DECLARE VARIABLE v_zona_id        INTEGER;
  DECLARE VARIABLE v_folio          VARCHAR(20);
  DECLARE VARIABLE v_fecha_cargo    DATE;
  DECLARE VARIABLE v_cancelado      CHAR(1);
  DECLARE VARIABLE v_docto_pv_id    INTEGER;
  DECLARE VARIABLE v_precio_total   NUMERIC(14,2);
  DECLARE VARIABLE v_total_importe  NUMERIC(14,2);
  DECLARE VARIABLE v_impte_rest     NUMERIC(14,2);
  DECLARE VARIABLE v_num_pagos      INTEGER;
  DECLARE VARIABLE v_fecha_ult_pago DATE;
  DECLARE VARIABLE v_saldo          NUMERIC(14,2);
BEGIN
  -- 1. Leer cabecera del cargo.
  SELECT DC.CLIENTE_ID, DC.FOLIO, DC.FECHA, DC.CANCELADO
    FROM DOCTOS_CC DC
    WHERE DC.DOCTO_CC_ID = :CARGO_ID
    INTO :v_cliente_id, :v_folio, :v_fecha_cargo, :v_cancelado;

  -- Si el cargo fue cancelado, eliminarlo del caché y salir.
  IF (v_cancelado = 'S') THEN
  BEGIN
    DELETE FROM MSP_SALDOS_VENTAS WHERE DOCTO_CC_ID = :CARGO_ID;
    EXIT;
  END

  -- 2. Zona actual del cliente (denormalizada; se actualiza por trigger en CLIENTES).
  SELECT C.ZONA_CLIENTE_ID
    FROM CLIENTES C
    WHERE C.CLIENTE_ID = :v_cliente_id
    INTO :v_zona_id;

  -- 3. DOCTO_PV_ID: si el cargo se originó en PV, obtener el ID del PV fuente.
  v_docto_pv_id = NULL;
  SELECT DES.DOCTO_FTE_ID
    FROM DOCTOS_ENTRE_SIS DES
    WHERE DES.CLAVE_SIS_FTE  = 'PV'
      AND DES.CLAVE_SIS_DEST = 'CC'
      AND DES.DOCTO_DEST_ID  = :CARGO_ID
    INTO :v_docto_pv_id;

  -- 4. PRECIO_TOTAL = suma de importes tipo cargo activos.
  SELECT COALESCE(SUM(IDC.IMPORTE), 0)
    FROM IMPORTES_DOCTOS_CC IDC
    WHERE IDC.DOCTO_CC_ID = :CARGO_ID
      AND IDC.TIPO_IMPTE  = 'C'
      AND IDC.CANCELADO   = 'N'
    INTO :v_precio_total;

  -- 5. Agrupación de pagos (TIPO_IMPTE='R', CANCELADO='N', acreditados al cargo).
  --    TOTAL_IMPORTE: conceptos de cobranza activos (87327, 155, 11).
  SELECT COALESCE(SUM(IDC.IMPORTE), 0)
    FROM IMPORTES_DOCTOS_CC IDC
    WHERE IDC.DOCTO_CC_ACR_ID   = :CARGO_ID
      AND IDC.TIPO_IMPTE        = 'R'
      AND IDC.CANCELADO         = 'N'
      AND IDC.CONCEPTO_CC_ID   IN (87327, 155, 11)
    INTO :v_total_importe;

  --    IMPTE_REST: cualquier otro concepto (enganches, condonaciones, fugas).
  SELECT COALESCE(SUM(IDC.IMPORTE), 0)
    FROM IMPORTES_DOCTOS_CC IDC
    WHERE IDC.DOCTO_CC_ACR_ID   = :CARGO_ID
      AND IDC.TIPO_IMPTE        = 'R'
      AND IDC.CANCELADO         = 'N'
      AND IDC.CONCEPTO_CC_ID NOT IN (87327, 155, 11)
    INTO :v_impte_rest;

  --    NUM_PAGOS y FECHA_ULT_PAGO (MAX(FECHA) — columna estándar de Microsip).
  SELECT COUNT(*), MAX(IDC.FECHA)
    FROM IMPORTES_DOCTOS_CC IDC
    WHERE IDC.DOCTO_CC_ACR_ID = :CARGO_ID
      AND IDC.TIPO_IMPTE      = 'R'
      AND IDC.CANCELADO       = 'N'
    INTO :v_num_pagos, :v_fecha_ult_pago;

  -- 6. SALDO.
  v_saldo = v_precio_total - v_total_importe - v_impte_rest;

  -- 7. Upsert atómico.
  UPDATE OR INSERT INTO MSP_SALDOS_VENTAS (
    DOCTO_CC_ID,
    DOCTO_PV_ID,
    CLIENTE_ID,
    ZONA_CLIENTE_ID,
    FOLIO,
    FECHA_CARGO,
    PRECIO_TOTAL,
    TOTAL_IMPORTE,
    IMPTE_REST,
    NUM_PAGOS,
    FECHA_ULT_PAGO,
    SALDO,
    CARGO_CANCELADO,
    UPDATED_AT
  ) VALUES (
    :CARGO_ID,
    :v_docto_pv_id,
    :v_cliente_id,
    :v_zona_id,
    :v_folio,
    :v_fecha_cargo,
    :v_precio_total,
    :v_total_importe,
    :v_impte_rest,
    :v_num_pagos,
    :v_fecha_ult_pago,
    :v_saldo,
    'N',
    CURRENT_TIMESTAMP
  ) MATCHING (DOCTO_CC_ID);

WHEN ANY DO
BEGIN
  INSERT INTO MSP_SALDOS_ERRORS (ERROR_ID, CARGO_ID, ERROR_MSG, ERROR_AT)
  VALUES (
    GEN_ID(GEN_MSP_SALDOS_ERRORS_ID, 1),
    :CARGO_ID,
    SUBSTRING('SQLCODE=' || SQLCODE || ' ' || GDSCODE FROM 1 FOR 500),
    CURRENT_TIMESTAMP
  );
  EXIT;
END
END;
COMMIT;

-- ─── Triggers ────────────────────────────────────────────────────────────────
--
-- Todos POSITION 100 (después de los triggers nativos de Microsip).
-- Todos usan WHEN ANY DO para no abortar nunca la tx del usuario.

-- Trigger 1: AFTER INSERT OR UPDATE en DOCTOS_CC.
--   Si NATURALEZA_CONCEPTO='C' → recomputar ese cargo.
--   Si NATURALEZA_CONCEPTO='R' (abono en DOCTOS_CC) → recomputar los cargos
--   acreditados (via IMPORTES_DOCTOS_CC.DOCTO_CC_ACR_ID).

CREATE TRIGGER MSP_SALDOS_DOCTOS_CC_AIU
  FOR DOCTOS_CC
  AFTER INSERT OR UPDATE
  POSITION 100
AS
  DECLARE VARIABLE acr_id INTEGER;
BEGIN
  IF (NEW.NATURALEZA_CONCEPTO = 'C') THEN
  BEGIN
    EXECUTE PROCEDURE MSP_RECOMPUTE_SALDO_VENTA(NEW.DOCTO_CC_ID);
  END
  ELSE IF (NEW.NATURALEZA_CONCEPTO = 'R') THEN
  BEGIN
    FOR SELECT DISTINCT IDC.DOCTO_CC_ACR_ID
          FROM IMPORTES_DOCTOS_CC IDC
          WHERE IDC.DOCTO_CC_ID = NEW.DOCTO_CC_ID
            AND IDC.DOCTO_CC_ACR_ID IS NOT NULL
          INTO :acr_id
    DO
      EXECUTE PROCEDURE MSP_RECOMPUTE_SALDO_VENTA(:acr_id);
  END

WHEN ANY DO
BEGIN
  INSERT INTO MSP_SALDOS_ERRORS (ERROR_ID, CARGO_ID, ERROR_MSG, ERROR_AT)
  VALUES (
    GEN_ID(GEN_MSP_SALDOS_ERRORS_ID, 1),
    NEW.DOCTO_CC_ID,
    SUBSTRING('TRG MSP_SALDOS_DOCTOS_CC_AIU SQLCODE=' || SQLCODE FROM 1 FOR 500),
    CURRENT_TIMESTAMP
  );
END
END;
COMMIT;

-- Trigger 2: AFTER DELETE en DOCTOS_CC.
--   Si el cargo eliminado era de naturaleza 'C', remover del caché.

CREATE TRIGGER MSP_SALDOS_DOCTOS_CC_AD
  FOR DOCTOS_CC
  AFTER DELETE
  POSITION 100
AS
BEGIN
  IF (OLD.NATURALEZA_CONCEPTO = 'C') THEN
  BEGIN
    DELETE FROM MSP_SALDOS_VENTAS WHERE DOCTO_CC_ID = OLD.DOCTO_CC_ID;
  END

WHEN ANY DO
BEGIN
  INSERT INTO MSP_SALDOS_ERRORS (ERROR_ID, CARGO_ID, ERROR_MSG, ERROR_AT)
  VALUES (
    GEN_ID(GEN_MSP_SALDOS_ERRORS_ID, 1),
    OLD.DOCTO_CC_ID,
    SUBSTRING('TRG MSP_SALDOS_DOCTOS_CC_AD SQLCODE=' || SQLCODE FROM 1 FOR 500),
    CURRENT_TIMESTAMP
  );
END
END;
COMMIT;

-- Trigger 3: AFTER INSERT OR UPDATE OR DELETE en IMPORTES_DOCTOS_CC.
--   Recomputa el cargo afectado por el importe insertado/modificado/eliminado.
--   Para INSERT/UPDATE: usa NEW.DOCTO_CC_ACR_ID (el cargo acreditado).
--     Si DOCTO_CC_ACR_ID es NULL (raro, importe sin acreditación directa),
--     también recomputa el documento contenedor NEW.DOCTO_CC_ID.
--   Para DELETE: igual pero usando OLD.*.

CREATE TRIGGER MSP_SALDOS_IMPORTES_CC_AIUD
  FOR IMPORTES_DOCTOS_CC
  AFTER INSERT OR UPDATE OR DELETE
  POSITION 100
AS
BEGIN
  IF (INSERTING OR UPDATING) THEN
  BEGIN
    IF (NEW.DOCTO_CC_ACR_ID IS NOT NULL) THEN
      EXECUTE PROCEDURE MSP_RECOMPUTE_SALDO_VENTA(NEW.DOCTO_CC_ACR_ID);
    ELSE
      EXECUTE PROCEDURE MSP_RECOMPUTE_SALDO_VENTA(NEW.DOCTO_CC_ID);
  END

  IF (DELETING) THEN
  BEGIN
    IF (OLD.DOCTO_CC_ACR_ID IS NOT NULL) THEN
      EXECUTE PROCEDURE MSP_RECOMPUTE_SALDO_VENTA(OLD.DOCTO_CC_ACR_ID);
    ELSE
      EXECUTE PROCEDURE MSP_RECOMPUTE_SALDO_VENTA(OLD.DOCTO_CC_ID);
  END

WHEN ANY DO
BEGIN
  INSERT INTO MSP_SALDOS_ERRORS (ERROR_ID, CARGO_ID, ERROR_MSG, ERROR_AT)
  VALUES (
    GEN_ID(GEN_MSP_SALDOS_ERRORS_ID, 1),
    CASE WHEN DELETING THEN OLD.DOCTO_CC_ID ELSE NEW.DOCTO_CC_ID END,
    SUBSTRING('TRG MSP_SALDOS_IMPORTES_CC_AIUD SQLCODE=' || SQLCODE FROM 1 FOR 500),
    CURRENT_TIMESTAMP
  );
END
END;
COMMIT;

-- Trigger 4: AFTER UPDATE en CLIENTES.
--   Si cambia ZONA_CLIENTE_ID, actualizar el campo denormalizado en el caché.
--   No se recomputa el saldo (no depende de la zona).

CREATE TRIGGER MSP_SALDOS_CLIENTES_AU
  FOR CLIENTES
  AFTER UPDATE
  POSITION 100
AS
BEGIN
  IF (NEW.ZONA_CLIENTE_ID IS DISTINCT FROM OLD.ZONA_CLIENTE_ID) THEN
  BEGIN
    UPDATE MSP_SALDOS_VENTAS
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
    SUBSTRING('TRG MSP_SALDOS_CLIENTES_AU SQLCODE=' || SQLCODE FROM 1 FOR 500),
    CURRENT_TIMESTAMP
  );
END
END;
COMMIT;

-- ─── Backfill inicial ─────────────────────────────────────────────────────────
-- Rellena MSP_SALDOS_VENTAS para todos los cargos activos existentes.
-- Tiempo estimado: ~20-25 minutos para ~351 K cargos (DOCTOS_CC con
-- NATURALEZA_CONCEPTO='C' y CANCELADO='N'). Solo se ejecuta una vez por
-- entorno (dev o producción) al aplicar esta migración.

EXECUTE BLOCK AS
  DECLARE VARIABLE cargo_id INTEGER;
BEGIN
  FOR SELECT DC.DOCTO_CC_ID
        FROM DOCTOS_CC DC
        WHERE DC.NATURALEZA_CONCEPTO = 'C'
          AND DC.CANCELADO           = 'N'
        INTO :cargo_id
  DO
    EXECUTE PROCEDURE MSP_RECOMPUTE_SALDO_VENTA(:cargo_id);
END;
COMMIT;

-- ─── Registro ────────────────────────────────────────────────────────────────
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (10, '000010_msp_saldos_ventas', CURRENT_TIMESTAMP);
COMMIT;
