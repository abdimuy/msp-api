-- ============================================================================
-- Migración 000019: tombstones en MSP_PAGOS_VENTAS para propagar cancelaciones
-- ============================================================================
--
-- Antes de esta migración (000013), el procedure MSP_RECOMPUTE_PAGO hacía
-- DELETE cuando el importe o su header DOCTOS_CC estaba cancelado. Esto rompe
-- el sync incremental por cursor (UPDATED_AT): un cliente que sincroniza cada
-- 30 s nunca recibe la noticia del DELETE y queda con un row fantasma en su
-- BD local.
--
-- Solución: tombstone lógico. Igual que la migración 000014 hizo para
-- MSP_SALDOS_VENTAS, aquí el row se mantiene pero con IMPORTE=0, IMPUESTO=0
-- y CANCELADO='S', con UPDATED_AT refrescado al momento de la cancelación.
-- El cliente recibe la row en su próxima sincronización y la descarta de su
-- BD local porque cancelado=true.
--
-- Distinción importante:
--   - Filas "inválidas" (no existe DOCTO_CC_ID, TIPO_IMPTE<>'R', no tiene
--     DOCTO_CC_ACR_ID): siguen con DELETE+EXIT, porque son rows que nunca
--     debieron existir en el caché, no cancelaciones de negocio.
--   - Filas canceladas de negocio (IMPORTES_DOCTOS_CC.CANCELADO='S' o
--     DOCTOS_CC.CANCELADO='S'): pasan a tombstone.
--
-- Lecturas humanas (/v2/cobranza/pagos/...) filtrarán CANCELADO='N' — eso
-- se implementa en el commit siguiente de este plan de 4 commits, no aquí.
-- El reconciler semanal limpiará tombstones >TombstoneRetentionDays para
-- que el caché no crezca indefinidamente.
--
-- Backfill: dos bloques cubren ambos casos de cancelación existentes:
--   Bloque A: importes con IMPORTES_DOCTOS_CC.CANCELADO='S' (importe cancelado).
--   Bloque B: importes activos cuyo header DOCTOS_CC tiene CANCELADO='S'
--             (cancelación del documento de abono completo).
-- ============================================================================

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
  -- Importe o header cancelado: escribir tombstone en lugar de DELETE para que
  -- el sync incremental por UPDATED_AT propague la cancelación al cliente móvil.
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

-- ─── Re-backfill de pagos cancelados ─────────────────────────────────────────
-- Los pagos cancelados existentes no están en el caché (la lógica anterior los
-- borraba). Re-ejecutar el procedure sobre los dos casos de cancelación crea
-- sus tombstones para que el sync incremental los propague desde el primer ciclo.
--
-- Bloque A: importes donde IMPORTES_DOCTOS_CC.CANCELADO='S' (importe cancelado
--           directamente).
-- Bloque B: importes activos cuyo DOCTOS_CC.CANCELADO='S' (header cancelado —
--           naturaleza R para cubrir solo documentos de abono/pago).

EXECUTE BLOCK AS
  DECLARE VARIABLE impte_id INTEGER;
BEGIN
  FOR SELECT IDC.IMPTE_DOCTO_CC_ID
        FROM IMPORTES_DOCTOS_CC IDC
        WHERE IDC.TIPO_IMPTE = 'R'
          AND IDC.CANCELADO  = 'S'
        INTO :impte_id
  DO
    EXECUTE PROCEDURE MSP_RECOMPUTE_PAGO(:impte_id);
END^

COMMIT^

EXECUTE BLOCK AS
  DECLARE VARIABLE impte_id INTEGER;
BEGIN
  FOR SELECT IDC.IMPTE_DOCTO_CC_ID
        FROM IMPORTES_DOCTOS_CC IDC
        JOIN DOCTOS_CC DC ON DC.DOCTO_CC_ID = IDC.DOCTO_CC_ID
        WHERE IDC.TIPO_IMPTE = 'R'
          AND IDC.CANCELADO  = 'N'
          AND DC.CANCELADO   = 'S'
        INTO :impte_id
  DO
    EXECUTE PROCEDURE MSP_RECOMPUTE_PAGO(:impte_id);
END^

COMMIT^

SET TERM ; ^

-- ─── Registro ────────────────────────────────────────────────────────────────
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (19, '000019_tombstone_pagos', CURRENT_TIMESTAMP);
COMMIT;
