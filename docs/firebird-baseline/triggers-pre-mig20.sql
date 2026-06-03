-- ============================================================================
-- Baseline DDL: triggers captured via isql before migration 000020
-- ============================================================================
-- Captured from container mueblera-firebird on 2026-06-02 using:
--   docker exec -i mueblera-firebird /usr/local/firebird/bin/isql \
--     -user SYSDBA -password masterkey /firebird/data/MUEBLERA.FDB
-- with:
--   SELECT RDB$TRIGGER_NAME, RDB$TRIGGER_SOURCE FROM RDB$TRIGGERS
--   WHERE RDB$TRIGGER_NAME IN ('MSP_PAGOS_IMPORTES_AIUD', 'MSP_SALDOS_DOCTOS_CC_AD');
--
-- These are the exact trigger bodies present before migration 000020 is applied.
-- Use this file to restore the triggers in 000020_tombstone_on_delete.down.sql
-- if a rollback is needed.
-- ============================================================================

-- Trigger: MSP_PAGOS_IMPORTES_AIUD
-- Table:   IMPORTES_DOCTOS_CC
-- Event:   AFTER INSERT OR UPDATE OR DELETE
-- Source confirmed by isql dump (RDB$TRIGGER_SOURCE, id c:1824b)

CREATE OR ALTER TRIGGER MSP_PAGOS_IMPORTES_AIUD
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
END


-- Trigger: MSP_SALDOS_DOCTOS_CC_AD
-- Table:   DOCTOS_CC
-- Event:   AFTER DELETE
-- Source confirmed by isql dump (RDB$TRIGGER_SOURCE, id c:18242)

CREATE OR ALTER TRIGGER MSP_SALDOS_DOCTOS_CC_AD
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
END
