-- ============================================================================
-- Migración 000011: corrección de FECHA_ULT_PAGO y lista de conceptos activos
-- en MSP_RECOMPUTE_SALDO_VENTA
-- ============================================================================
--
-- Dos correcciones al procedure de 000010 después de comparar con la
-- implementación canónica del sistema Node anterior
-- (sys_msp_backend/src/components/ventas/queries.ts:172):
--
--   1. FECHA_ULT_PAGO se debe sacar de DOCTOS_CC.FECHA (header del documento
--      de abono), NO de IMPORTES_DOCTOS_CC.FECHA. Microsip mueve la fecha
--      de la línea cuando oficina reaplica un pago, pero la del header
--      queda fija en el momento real del cobro. Para reportes de cobranza
--      lo que importa es el header.
--
--   2. Concepto 11 (auto-pago de contado) debe contar en IMPTE_REST, NO en
--      TOTAL_IMPORTE. El sistema Node lo excluye deliberadamente: TOTAL_IMPORTE
--      solo agrega cobranza en ruta (87327) y cobro mostrador (155) porque
--      esa es "actividad real de cobrador". El auto-pago de contado no es
--      actividad, es la liquidación inmediata del cargo — debe reducir el
--      SALDO igualmente (cae en IMPTE_REST) pero no inflar el conteo de
--      pagos de ruta.
--
--   3. Como consecuencia de (2), FECHA_ULT_PAGO también debe restringirse
--      a conceptos 87327 y 155 — sino una venta de contado aplicada hoy
--      aparecería en la "ruta" del cobrador como recién pagada, que es
--      ruido. Se hace via subquery filtrada por concepto.
--
-- El procedure se ALTERa (DROP + CREATE) y se re-ejecuta el backfill para
-- corregir los ~100K rows existentes cuyo TOTAL_IMPORTE/IMPTE_REST/
-- FECHA_ULT_PAGO pudieron quedar con la lógica anterior.
-- ============================================================================

-- ALTER (no DROP): los triggers MSP_SALDOS_DOCTOS_CC_AIU /
-- MSP_SALDOS_IMPORTES_CC_AIUD dependen del procedure y un DROP fallaría.
-- ALTER PROCEDURE reemplaza el cuerpo en sitio sin romper esas dependencias.

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

  -- 2. Zona actual del cliente.
  SELECT C.ZONA_CLIENTE_ID
    FROM CLIENTES C
    WHERE C.CLIENTE_ID = :v_cliente_id
    INTO :v_zona_id;

  -- 3. DOCTO_PV_ID si el cargo se originó en PV.
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
  --    CONCEPTO_CC_ID vive en DOCTOS_CC (header del abono), no en
  --    IMPORTES_DOCTOS_CC — JOIN al header para filtrar por concepto.
  --
  --    TOTAL_IMPORTE: cobranza en ruta (87327) + cobro mostrador (155).
  --    El concepto 11 (auto-pago contado) NO entra acá — no es actividad
  --    de cobrador. Cae en IMPTE_REST.
  SELECT COALESCE(SUM(IDC.IMPORTE), 0)
    FROM IMPORTES_DOCTOS_CC IDC
    JOIN DOCTOS_CC DC2 ON DC2.DOCTO_CC_ID = IDC.DOCTO_CC_ID
    WHERE IDC.DOCTO_CC_ACR_ID = :CARGO_ID
      AND IDC.TIPO_IMPTE      = 'R'
      AND IDC.CANCELADO       = 'N'
      AND DC2.CONCEPTO_CC_ID IN (87327, 155)
    INTO :v_total_importe;

  --    IMPTE_REST: todo lo demás (enganches, condonaciones, auto-pago
  --    contado, fugas). Necesario para que SALDO llegue a 0 en contado.
  SELECT COALESCE(SUM(IDC.IMPORTE), 0)
    FROM IMPORTES_DOCTOS_CC IDC
    JOIN DOCTOS_CC DC2 ON DC2.DOCTO_CC_ID = IDC.DOCTO_CC_ID
    WHERE IDC.DOCTO_CC_ACR_ID = :CARGO_ID
      AND IDC.TIPO_IMPTE      = 'R'
      AND IDC.CANCELADO       = 'N'
      AND DC2.CONCEPTO_CC_ID NOT IN (87327, 155)
    INTO :v_impte_rest;

  --    NUM_PAGOS y FECHA_ULT_PAGO restringidos a cobranza activa.
  --    DOCTOS_CC.FECHA (header) es la fecha real del cobro; Microsip NO la
  --    mueve cuando reaplica desde oficina (a diferencia de
  --    IMPORTES_DOCTOS_CC.FECHA que sí se mueve). El sistema Node anterior
  --    también usa este mismo criterio.
  SELECT COUNT(*), MAX(DC2.FECHA)
    FROM IMPORTES_DOCTOS_CC IDC
    JOIN DOCTOS_CC DC2 ON DC2.DOCTO_CC_ID = IDC.DOCTO_CC_ID
    WHERE IDC.DOCTO_CC_ACR_ID = :CARGO_ID
      AND IDC.TIPO_IMPTE      = 'R'
      AND IDC.CANCELADO       = 'N'
      AND DC2.CONCEPTO_CC_ID IN (87327, 155)
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
END^

COMMIT^

-- ─── Re-backfill ──────────────────────────────────────────────────────────────
-- Re-corre el procedure para todos los cargos existentes. Necesario porque
-- los rows actuales del cache tienen TOTAL_IMPORTE / IMPTE_REST / FECHA_ULT_PAGO
-- computados con la lógica vieja (incluían concepto 11 en TOTAL_IMPORTE y
-- usaban IMPORTES_DOCTOS_CC.FECHA para FECHA_ULT_PAGO).
--
-- ~48s para 102K cargos.

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
VALUES (11, '000011_fix_fecha_ult_pago_y_conceptos', CURRENT_TIMESTAMP);
COMMIT;
