-- ============================================================================
-- Migración 000012: FECHA_ULT_PAGO como TIMESTAMP con COALESCE a MSP_PAGOS_RECIBIDOS
-- ============================================================================
--
-- Para el corte por fecha en /v2/cobranza/saldos/zona/{id}?desde=<timestamp>
-- queremos discriminar al segundo cuando el dato esté disponible. La tabla
-- MSP_PAGOS_RECIBIDOS (creada por la app móvil) registra cada cobro con
-- timestamp completo, pero solo cubre los pagos hechos vía la app (~17% del
-- histórico). Los pagos hechos directamente en oficina solo tienen
-- DOCTOS_CC.FECHA (DATE, precisión de día).
--
-- Solución: el caché materializa FECHA_ULT_PAGO con la mejor fuente
-- disponible por pago — TIMESTAMP de la app cuando existe, DATE de Microsip
-- cuando no — y la columna pasa de DATE a TIMESTAMP para preservar la hora.
--
-- Sin trigger adicional sobre MSP_PAGOS_RECIBIDOS: la app SIEMPRE escribe ahí
-- antes de sincronizar a Microsip; cuando el trigger nuestro sobre
-- IMPORTES_DOCTOS_CC dispare, la row de MSP_PAGOS_RECIBIDOS ya estará y el
-- LEFT JOIN del procedure la encontrará. El reconciler semanal cubre
-- cualquier edge case (UPDATE manual fuera de banda).
-- ============================================================================

-- ─── 1. Índice sobre MSP_PAGOS_RECIBIDOS para el LEFT JOIN del procedure ─────
-- Sin este índice, cada recompute haría full scan de 348 K rows.
-- ASCENDING por defecto. CREATE INDEX es atómico en Firebird; no requiere lock
-- exclusivo de la tabla.

CREATE INDEX IDX_PAGOS_REC_DOCTO_CC ON MSP_PAGOS_RECIBIDOS (DOCTO_CC_ID);
COMMIT;

-- ─── 2. Cambiar tipo de FECHA_ULT_PAGO de DATE a TIMESTAMP ───────────────────
-- Hay que dropear primero el índice IDX_MSP_SALDOS_ZONA_FUP que la cubre
-- (Firebird no permite ALTER COLUMN TYPE mientras hay un índice activo sobre
-- la columna).

DROP INDEX IDX_MSP_SALDOS_ZONA_FUP;
COMMIT;

ALTER TABLE MSP_SALDOS_VENTAS ALTER COLUMN FECHA_ULT_PAGO TYPE TIMESTAMP;
COMMIT;

CREATE INDEX IDX_MSP_SALDOS_ZONA_FUP ON MSP_SALDOS_VENTAS (ZONA_CLIENTE_ID, FECHA_ULT_PAGO);
COMMIT;

-- ─── 3. ALTER PROCEDURE: COALESCE MSP_PAGOS_RECIBIDOS.FECHA → DOCTOS_CC.FECHA
-- v_fecha_ult_pago pasa de DATE a TIMESTAMP.
-- MAX(COALESCE(P.FECHA, CAST(DC.FECHA AS TIMESTAMP))) — la decisión es por
-- pago, no por agregado: si un cargo tiene 5 pagos de app y 1 viejo de
-- oficina, igual queremos el MÁS reciente de los dos.

SET TERM ^ ;

ALTER PROCEDURE MSP_RECOMPUTE_SALDO_VENTA (
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
  DECLARE VARIABLE v_fecha_ult_pago TIMESTAMP;
  DECLARE VARIABLE v_saldo          NUMERIC(14,2);
BEGIN
  SELECT DC.CLIENTE_ID, DC.FOLIO, DC.FECHA, DC.CANCELADO
    FROM DOCTOS_CC DC
    WHERE DC.DOCTO_CC_ID = :CARGO_ID
    INTO :v_cliente_id, :v_folio, :v_fecha_cargo, :v_cancelado;

  IF (v_cancelado = 'S') THEN
  BEGIN
    DELETE FROM MSP_SALDOS_VENTAS WHERE DOCTO_CC_ID = :CARGO_ID;
    EXIT;
  END

  SELECT C.ZONA_CLIENTE_ID
    FROM CLIENTES C
    WHERE C.CLIENTE_ID = :v_cliente_id
    INTO :v_zona_id;

  v_docto_pv_id = NULL;
  SELECT DES.DOCTO_FTE_ID
    FROM DOCTOS_ENTRE_SIS DES
    WHERE DES.CLAVE_SIS_FTE  = 'PV'
      AND DES.CLAVE_SIS_DEST = 'CC'
      AND DES.DOCTO_DEST_ID  = :CARGO_ID
    INTO :v_docto_pv_id;

  SELECT COALESCE(SUM(IDC.IMPORTE), 0)
    FROM IMPORTES_DOCTOS_CC IDC
    WHERE IDC.DOCTO_CC_ID = :CARGO_ID
      AND IDC.TIPO_IMPTE  = 'C'
      AND IDC.CANCELADO   = 'N'
    INTO :v_precio_total;

  SELECT COALESCE(SUM(IDC.IMPORTE), 0)
    FROM IMPORTES_DOCTOS_CC IDC
    JOIN DOCTOS_CC DC2 ON DC2.DOCTO_CC_ID = IDC.DOCTO_CC_ID
    WHERE IDC.DOCTO_CC_ACR_ID = :CARGO_ID
      AND IDC.TIPO_IMPTE      = 'R'
      AND IDC.CANCELADO       = 'N'
      AND DC2.CONCEPTO_CC_ID IN (87327, 155)
    INTO :v_total_importe;

  SELECT COALESCE(SUM(IDC.IMPORTE), 0)
    FROM IMPORTES_DOCTOS_CC IDC
    JOIN DOCTOS_CC DC2 ON DC2.DOCTO_CC_ID = IDC.DOCTO_CC_ID
    WHERE IDC.DOCTO_CC_ACR_ID = :CARGO_ID
      AND IDC.TIPO_IMPTE      = 'R'
      AND IDC.CANCELADO       = 'N'
      AND DC2.CONCEPTO_CC_ID NOT IN (87327, 155)
    INTO :v_impte_rest;

  -- NUM_PAGOS y FECHA_ULT_PAGO restringidos a cobranza activa (87327, 155).
  -- COALESCE por fila: cada pago aporta su mejor timestamp disponible.
  -- LEFT JOIN a MSP_PAGOS_RECIBIDOS por DOCTO_CC_ID (índice creado arriba).
  SELECT COUNT(*), MAX(COALESCE(P.FECHA, CAST(DC2.FECHA AS TIMESTAMP)))
    FROM IMPORTES_DOCTOS_CC IDC
    JOIN DOCTOS_CC DC2 ON DC2.DOCTO_CC_ID = IDC.DOCTO_CC_ID
    LEFT JOIN MSP_PAGOS_RECIBIDOS P ON P.DOCTO_CC_ID = IDC.DOCTO_CC_ID
    WHERE IDC.DOCTO_CC_ACR_ID = :CARGO_ID
      AND IDC.TIPO_IMPTE      = 'R'
      AND IDC.CANCELADO       = 'N'
      AND DC2.CONCEPTO_CC_ID IN (87327, 155)
    INTO :v_num_pagos, :v_fecha_ult_pago;

  v_saldo = v_precio_total - v_total_importe - v_impte_rest;

  UPDATE OR INSERT INTO MSP_SALDOS_VENTAS (
    DOCTO_CC_ID, DOCTO_PV_ID, CLIENTE_ID, ZONA_CLIENTE_ID, FOLIO, FECHA_CARGO,
    PRECIO_TOTAL, TOTAL_IMPORTE, IMPTE_REST, NUM_PAGOS, FECHA_ULT_PAGO, SALDO,
    CARGO_CANCELADO, UPDATED_AT
  ) VALUES (
    :CARGO_ID, :v_docto_pv_id, :v_cliente_id, :v_zona_id, :v_folio, :v_fecha_cargo,
    :v_precio_total, :v_total_importe, :v_impte_rest, :v_num_pagos, :v_fecha_ult_pago, :v_saldo,
    'N', CURRENT_TIMESTAMP
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
END^

COMMIT^

-- ─── 4. Re-backfill ──────────────────────────────────────────────────────────
-- ~70 s para 102 K cargos (vs 48 s sin el LEFT JOIN).

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
END^

COMMIT^

SET TERM ; ^

-- ─── Registro ────────────────────────────────────────────────────────────────
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (12, '000012_fecha_ult_pago_timestamp_coalesce', CURRENT_TIMESTAMP);
COMMIT;
