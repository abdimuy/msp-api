-- ============================================================================
-- Rollback de la migración 000020: restaurar DELETE directo en triggers
-- ============================================================================
-- Restaura los bodies de los triggers a su estado pre-mig20 exactamente como
-- fueron capturados en docs/firebird-baseline/triggers-pre-mig20.sql (isql -x,
-- 2026-06-02, RDB$TRIGGER_SOURCE ids c:1824b y c:18242).
-- ============================================================================

SET TERM ^ ;

-- ─── Restaurar MSP_PAGOS_IMPORTES_AIUD ───────────────────────────────────────

ALTER TRIGGER MSP_PAGOS_IMPORTES_AIUD
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

-- ─── Restaurar MSP_SALDOS_DOCTOS_CC_AD ───────────────────────────────────────

ALTER TRIGGER MSP_SALDOS_DOCTOS_CC_AD
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
END^

COMMIT^

SET TERM ; ^

DELETE FROM MSP_MIGRATIONS WHERE ID = 20;
COMMIT;
