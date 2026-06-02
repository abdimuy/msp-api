-- ============================================================================
-- Migración 000015: PRECIO_TOTAL / TOTAL_IMPORTE / IMPTE_REST incluyen IMPUESTO
-- ============================================================================
--
-- El procedure MSP_RECOMPUTE_SALDO_VENTA computaba PRECIO_TOTAL, TOTAL_IMPORTE
-- y IMPTE_REST sumando solo IDC.IMPORTE (importe neto sin IVA). El sistema
-- legacy Node calcula los mismos valores como IDC.IMPORTE + IDC.IMPUESTO
-- (importe con IVA incluido), que es lo que el cobrador realmente debe cobrar
-- y lo que las parcialidades en Microsip están alineadas a recibir.
--
-- Sin esta corrección, el saldo mostrado al cobrador termina en cifras no
-- redondas (ej. $3851.72 en vez de $3851 o $3850) porque está omitiendo la
-- porción de impuesto que el cliente sí está pagando.
--
-- Cambios:
--   * PRECIO_TOTAL: SUM(IMPORTE)         → SUM(IMPORTE + IMPUESTO)
--   * TOTAL_IMPORTE: SUM(IMPORTE)        → SUM(IMPORTE + IMPUESTO)
--   * IMPTE_REST:    SUM(IMPORTE)        → SUM(IMPORTE + IMPUESTO)
--
-- Backfill: re-ejecuta el procedure para todos los cargos no cancelados.
-- ============================================================================

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

-- ─── Re-backfill ──────────────────────────────────────────────────────────────
-- Re-corre el procedure para todos los cargos no cancelados para regenerar
-- el cache con IMPORTE + IMPUESTO. ~48s para 102K cargos (mismo tiempo que
-- el backfill de 000011 porque el work es idéntico).
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
VALUES (15, '000015_saldo_incluye_impuesto', CURRENT_TIMESTAMP);
COMMIT;
