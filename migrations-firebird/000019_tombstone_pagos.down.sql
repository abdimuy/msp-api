-- ============================================================================
-- Rollback de la migración 000019: revertir tombstones a DELETE
-- ============================================================================
-- Restaura el body del procedure de 000013 (DELETE en cancelaciones) y borra
-- los tombstones existentes del caché para que la BD quede igual que antes
-- de aplicar 000019.
-- ============================================================================

-- Borrar tombstones existentes (rows con CANCELADO='S').
DELETE FROM MSP_PAGOS_VENTAS WHERE CANCELADO = 'S';
COMMIT;

SET TERM ^ ;

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

SET TERM ; ^

DELETE FROM MSP_MIGRATIONS WHERE ID = 19;
COMMIT;
