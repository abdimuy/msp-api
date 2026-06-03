-- ============================================================================
-- Migración 000024: tombstone triggers escriben changelog + TX_ID
-- ============================================================================
--
-- Extiende las ramas DELETING de los triggers MSP_PAGOS_IMPORTES_AIUD y
-- MSP_SALDOS_DOCTOS_CC_AD para que, además del UPDATE tombstone existente:
--
--   1. Escriban TX_ID = CAST(CURRENT_TRANSACTION AS BIGINT) en la fila
--      tombstoneada del caché, igual que MSP_RECOMPUTE_PAGO hace en mig 23.
--
--   2. Inserten una fila en el changelog respectivo (MSP_PAGOS_CHANGELOG o
--      MSP_SALDOS_CHANGELOG) antes del POST_EVENT, de modo que las
--      cancelaciones físicas (DELETE de IMPORTES_DOCTOS_CC o DOCTOS_CC)
--      queden visibles vía cursor SEQ_ID sin depender del poll por UPDATED_AT.
--
-- Reglas que se mantienen exactamente igual que mig 21:
--   - POST_EVENT permanece en su posición: después del INSERT al changelog,
--     antes del fin del bloque éxito.
--   - WHEN ANY DO no recibe changelog INSERTs (atomicidad rollback-safe).
--   - La rama INSERTING OR UPDATING no se toca: delega a MSP_RECOMPUTE_PAGO
--     (que post-mig-23 ya escribe su propio changelog row).
--
-- CAST(CURRENT_TRANSACTION AS BIGINT): PSQL pseudo-registro disponible en
-- Firebird 2.1+.  NO se usa RDB$GET_CONTEXT que requiere Firebird 3.0+.
-- ============================================================================

SET TERM ^ ;

-- ─── Trigger 1: MSP_PAGOS_IMPORTES_AIUD ──────────────────────────────────────
-- Rama DELETING: tombstone + TX_ID + INSERT changelog + POST_EVENT.
-- Rama INSERTING/UPDATING: sin cambios — delega a MSP_RECOMPUTE_PAGO.

ALTER TRIGGER MSP_PAGOS_IMPORTES_AIUD
AS
BEGIN
  IF (INSERTING OR UPDATING) THEN
    EXECUTE PROCEDURE MSP_RECOMPUTE_PAGO(NEW.IMPTE_DOCTO_CC_ID);

  IF (DELETING) THEN
  BEGIN
    UPDATE MSP_PAGOS_VENTAS
       SET CANCELADO = 'S', IMPORTE = 0, IMPUESTO = 0,
           UPDATED_AT = CURRENT_TIMESTAMP,
           TX_ID = CAST(CURRENT_TRANSACTION AS BIGINT)
     WHERE IMPTE_DOCTO_CC_ID = OLD.IMPTE_DOCTO_CC_ID;
    INSERT INTO MSP_PAGOS_CHANGELOG (SEQ_ID, IMPTE_DOCTO_CC_ID, TX_ID, COMMIT_AT)
    VALUES (
      GEN_ID(GEN_MSP_PAGOS_CHANGELOG_SEQ, 1),
      OLD.IMPTE_DOCTO_CC_ID,
      CAST(CURRENT_TRANSACTION AS BIGINT),
      CURRENT_TIMESTAMP
    );
    POST_EVENT 'pagos_changed';
  END

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

-- ─── Trigger 2: MSP_SALDOS_DOCTOS_CC_AD ──────────────────────────────────────
-- Rama DELETING (IF NATURALEZA_CONCEPTO='C'): tombstone + TX_ID + INSERT
-- changelog + POST_EVENT.

ALTER TRIGGER MSP_SALDOS_DOCTOS_CC_AD
AS
BEGIN
  IF (OLD.NATURALEZA_CONCEPTO = 'C') THEN
  BEGIN
    UPDATE MSP_SALDOS_VENTAS
       SET CARGO_CANCELADO = 'S', SALDO = 0, TOTAL_IMPORTE = 0, IMPTE_REST = 0,
           NUM_PAGOS = 0, PRECIO_TOTAL = 0, FECHA_ULT_PAGO = NULL,
           UPDATED_AT = CURRENT_TIMESTAMP,
           TX_ID = CAST(CURRENT_TRANSACTION AS BIGINT)
     WHERE DOCTO_CC_ID = OLD.DOCTO_CC_ID;
    INSERT INTO MSP_SALDOS_CHANGELOG (SEQ_ID, DOCTO_CC_ID, TX_ID, COMMIT_AT)
    VALUES (
      GEN_ID(GEN_MSP_SALDOS_CHANGELOG_SEQ, 1),
      OLD.DOCTO_CC_ID,
      CAST(CURRENT_TRANSACTION AS BIGINT),
      CURRENT_TIMESTAMP
    );
    POST_EVENT 'saldos_changed';
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

-- ─── Registro ────────────────────────────────────────────────────────────────
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (24, '000024_tombstone_changelog', CURRENT_TIMESTAMP);
COMMIT;
