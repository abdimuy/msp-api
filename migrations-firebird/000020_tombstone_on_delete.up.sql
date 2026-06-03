-- ============================================================================
-- Migración 000020: tombstone también en DELETE directo (pagos + saldos)
-- ============================================================================
--
-- Problema que resuelve:
-- La migración 000019 convirtió las CANCELACIONES de IMPORTES_DOCTOS_CC en
-- tombstones (CANCELADO='S', IMPORTE=0) dentro de MSP_RECOMPUTE_PAGO. Pero
-- cuando una fila de IMPORTES_DOCTOS_CC es eliminada físicamente (DELETE), el
-- trigger MSP_PAGOS_IMPORTES_AIUD hacía un DELETE FROM MSP_PAGOS_VENTAS, lo
-- que deja al cobrador con un row fantasma en su caché local porque el sync
-- incremental por UPDATED_AT nunca recibe la noticia.
--
-- Mismo problema para MSP_SALDOS_DOCTOS_CC_AD: cuando se elimina físicamente
-- un DOCTOS_CC cargo, el trigger borraba el row de MSP_SALDOS_VENTAS.
--
-- Solución: reemplazar el DELETE en la rama DELETING de ambos triggers por un
-- UPDATE que escribe un tombstone lógico. Si el row no existe en el caché, el
-- UPDATE es un no-op (no crea row fantasma, no genera error).
--
-- Nota: el tombstone del DELETE en MSP_PAGOS_VENTAS NO pasa por
-- MSP_RECOMPUTE_PAGO porque el importe ya no existe — no hay datos que leer.
-- Se escribe el tombstone mínimo directamente desde el trigger.
-- ============================================================================

SET TERM ^ ;

-- ─── Trigger 1: MSP_PAGOS_IMPORTES_AIUD ──────────────────────────────────────
-- Rama DELETING: reemplaza DELETE por UPDATE tombstone.
-- Si el row no está en MSP_PAGOS_VENTAS, el UPDATE es no-op (0 rows affected,
-- sin error, sin phantom row en el caché).

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

-- ─── Trigger 2: MSP_SALDOS_DOCTOS_CC_AD ──────────────────────────────────────
-- Rama DELETING: reemplaza DELETE por UPDATE tombstone en MSP_SALDOS_VENTAS.
-- El guard IF (OLD.NATURALEZA_CONCEPTO = 'C') se mantiene exactamente igual.
-- Si el row no está en MSP_SALDOS_VENTAS, el UPDATE es no-op.

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

-- ─── Registro ────────────────────────────────────────────────────────────────
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (20, '000020_tombstone_on_delete', CURRENT_TIMESTAMP);
COMMIT;
