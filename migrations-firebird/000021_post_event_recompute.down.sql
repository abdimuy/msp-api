-- ============================================================================
-- Rollback de la migración 000021: eliminar POST_EVENT de procedures/triggers
-- ============================================================================
-- Restaura MSP_RECOMPUTE_PAGO, MSP_RECOMPUTE_SALDO_VENTA,
-- MSP_PAGOS_IMPORTES_AIUD y MSP_SALDOS_DOCTOS_CC_AD a su estado de mig 20
-- (tombstones pero sin POST_EVENT).
-- ============================================================================

SET TERM ^ ;

-- ─── Procedure 1: MSP_RECOMPUTE_PAGO (estado post-mig 19, sin POST_EVENT) ───

ALTER PROCEDURE MSP_RECOMPUTE_PAGO (
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

  -- Si no existe el importe, no es pago (TIPO_IMPTE<>'R'), o no tiene cargo
  -- acreditado: este row nunca debió existir en el caché — borrar y salir.
  -- (No es una cancelación de negocio: no hay tombstone.)
  IF (v_docto_cc_id IS NULL OR v_tipo_impte <> 'R' OR v_acr_id IS NULL) THEN
  BEGIN
    DELETE FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = :IMPTE_ID;
    EXIT;
  END

  -- 2. Leer header del documento abono (cliente, folio, concepto, fecha).
  SELECT DC.CLIENTE_ID, DC.FOLIO, DC.CONCEPTO_CC_ID, DC.FECHA, DC.CANCELADO
    FROM DOCTOS_CC DC
    WHERE DC.DOCTO_CC_ID = :v_docto_cc_id
    INTO :v_cliente_id, :v_folio, :v_concepto_cc_id, :v_fecha_dc, :v_cancelado_dc;

  -- 3. Zona actual del cliente (siempre, para que el tombstone también la lleve).
  v_zona_id = NULL;
  SELECT C.ZONA_CLIENTE_ID
    FROM CLIENTES C
    WHERE C.CLIENTE_ID = :v_cliente_id
    INTO :v_zona_id;

  -- 4. Fecha del pago: TIMESTAMP de MSP_PAGOS_RECIBIDOS cuando exista; sino
  --    DOCTOS_CC.FECHA casteada a TIMESTAMP. Misma semántica que 000012/000013.
  v_fecha_pago = NULL;
  SELECT FIRST 1 P.FECHA
    FROM MSP_PAGOS_RECIBIDOS P
    WHERE P.DOCTO_CC_ID = :v_docto_cc_id
    INTO :v_fecha_pago;

  IF (v_fecha_pago IS NULL) THEN
    v_fecha_pago = CAST(v_fecha_dc AS TIMESTAMP);

  -- ── Tombstone ──────────────────────────────────────────────────────────────
  IF (v_cancelado_imp = 'S' OR v_cancelado_dc = 'S') THEN
  BEGIN
    UPDATE OR INSERT INTO MSP_PAGOS_VENTAS (
      IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, DOCTO_CC_ACR_ID, CLIENTE_ID, ZONA_CLIENTE_ID,
      FOLIO, CONCEPTO_CC_ID, FECHA, IMPORTE, IMPUESTO, LAT, LON,
      CANCELADO, APLICADO, UPDATED_AT
    ) VALUES (
      :IMPTE_ID, :v_docto_cc_id, :v_acr_id, :v_cliente_id, :v_zona_id,
      :v_folio, :v_concepto_cc_id, :v_fecha_pago, 0, 0, NULL, NULL,
      'S', :v_aplicado, CURRENT_TIMESTAMP
    ) MATCHING (IMPTE_DOCTO_CC_ID);
    EXIT;
  END

  -- 5. Upsert normal. LAT/LON quedan NULL (no hay fuente todavía).
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

-- ─── Procedure 2: MSP_RECOMPUTE_SALDO_VENTA (estado post-mig 15, sin POST_EVENT) ───

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

  v_zona_id = NULL;
  SELECT C.ZONA_CLIENTE_ID
    FROM CLIENTES C
    WHERE C.CLIENTE_ID = :v_cliente_id
    INTO :v_zona_id;

  -- ── Tombstone (cargo cancelado) ────────────────────────────────────────────
  IF (v_cancelado = 'S') THEN
  BEGIN
    UPDATE OR INSERT INTO MSP_SALDOS_VENTAS (
      DOCTO_CC_ID, DOCTO_PV_ID, CLIENTE_ID, ZONA_CLIENTE_ID, FOLIO, FECHA_CARGO,
      PRECIO_TOTAL, TOTAL_IMPORTE, IMPTE_REST, NUM_PAGOS, FECHA_ULT_PAGO, SALDO,
      CARGO_CANCELADO, UPDATED_AT
    ) VALUES (
      :CARGO_ID, NULL, :v_cliente_id, :v_zona_id, :v_folio, :v_fecha_cargo,
      0, 0, 0, 0, NULL, 0,
      'S', CURRENT_TIMESTAMP
    ) MATCHING (DOCTO_CC_ID);
    EXIT;
  END

  v_docto_pv_id = NULL;
  SELECT DES.DOCTO_FTE_ID
    FROM DOCTOS_ENTRE_SIS DES
    WHERE DES.CLAVE_SIS_FTE  = 'PV'
      AND DES.CLAVE_SIS_DEST = 'CC'
      AND DES.DOCTO_DEST_ID  = :CARGO_ID
    INTO :v_docto_pv_id;

  -- PRECIO_TOTAL con IVA incluido (alineado a las parcialidades del contrato).
  SELECT COALESCE(SUM(IDC.IMPORTE + IDC.IMPUESTO), 0)
    FROM IMPORTES_DOCTOS_CC IDC
    WHERE IDC.DOCTO_CC_ID = :CARGO_ID
      AND IDC.TIPO_IMPTE  = 'C'
      AND IDC.CANCELADO   = 'N'
    INTO :v_precio_total;

  -- TOTAL_IMPORTE: cobranza en ruta (87327) + cobro mostrador (155), con IVA.
  SELECT COALESCE(SUM(IDC.IMPORTE + IDC.IMPUESTO), 0)
    FROM IMPORTES_DOCTOS_CC IDC
    JOIN DOCTOS_CC DC2 ON DC2.DOCTO_CC_ID = IDC.DOCTO_CC_ID
    WHERE IDC.DOCTO_CC_ACR_ID = :CARGO_ID
      AND IDC.TIPO_IMPTE      = 'R'
      AND IDC.CANCELADO       = 'N'
      AND DC2.CONCEPTO_CC_ID IN (87327, 155)
    INTO :v_total_importe;

  -- IMPTE_REST: enganches, condonaciones, auto-pago, fugas, con IVA.
  SELECT COALESCE(SUM(IDC.IMPORTE + IDC.IMPUESTO), 0)
    FROM IMPORTES_DOCTOS_CC IDC
    JOIN DOCTOS_CC DC2 ON DC2.DOCTO_CC_ID = IDC.DOCTO_CC_ID
    WHERE IDC.DOCTO_CC_ACR_ID = :CARGO_ID
      AND IDC.TIPO_IMPTE      = 'R'
      AND IDC.CANCELADO       = 'N'
      AND DC2.CONCEPTO_CC_ID NOT IN (87327, 155)
    INTO :v_impte_rest;

  -- NUM_PAGOS y FECHA_ULT_PAGO (conteo, sin importes — se queda igual).
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

-- ─── Trigger 1: MSP_PAGOS_IMPORTES_AIUD (estado post-mig 20, sin POST_EVENT) ─

ALTER TRIGGER MSP_PAGOS_IMPORTES_AIUD
AS
BEGIN
  IF (INSERTING OR UPDATING) THEN
    EXECUTE PROCEDURE MSP_RECOMPUTE_PAGO(NEW.IMPTE_DOCTO_CC_ID);

  IF (DELETING) THEN
    UPDATE MSP_PAGOS_VENTAS
       SET CANCELADO = 'S', IMPORTE = 0, IMPUESTO = 0,
           UPDATED_AT = CURRENT_TIMESTAMP
     WHERE IMPTE_DOCTO_CC_ID = OLD.IMPTE_DOCTO_CC_ID;

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

-- ─── Trigger 2: MSP_SALDOS_DOCTOS_CC_AD (estado post-mig 20, sin POST_EVENT) ─

ALTER TRIGGER MSP_SALDOS_DOCTOS_CC_AD
AS
BEGIN
  IF (OLD.NATURALEZA_CONCEPTO = 'C') THEN
  BEGIN
    UPDATE MSP_SALDOS_VENTAS
       SET CARGO_CANCELADO = 'S', SALDO = 0, TOTAL_IMPORTE = 0, IMPTE_REST = 0,
           NUM_PAGOS = 0, PRECIO_TOTAL = 0, FECHA_ULT_PAGO = NULL,
           UPDATED_AT = CURRENT_TIMESTAMP
     WHERE DOCTO_CC_ID = OLD.DOCTO_CC_ID;
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
END^

COMMIT^

SET TERM ; ^

DELETE FROM MSP_MIGRATIONS WHERE ID = 21;
COMMIT;
