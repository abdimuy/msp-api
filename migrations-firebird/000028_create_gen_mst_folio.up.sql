-- ============================================================================
-- Migración 000028: GEN_MST_FOLIO (generador de folios para traspasos)
-- ============================================================================
--
-- Por qué existe:
--   El módulo inventario emite traspasos (DOCTOS_IN con CONCEPTO_IN_ID=36)
--   etiquetados con folios MST000001, MSU000001, ... — minted via
--   GEN_ID(GEN_MST_FOLIO, 1) y mapeados a string en Go.
--
--   Producción Microsip ya tiene este generador (el legacy Node lo creaba en
--   la migración 007 de sys_msp_backend). Esta migración lo crea idempotente
--   en cualquier entorno (dev, CI, prod nueva) que aún no lo tenga.
--
-- ADR 0006 exempts the Firebird adapter from the "no DB logic" rule —
-- generators son aceptables aquí porque la base es trigger-driven y este
-- generador modela un secuencial atómico que de otra forma requeriría una
-- transacción + retry loop en Go.
--
-- Idempotencia:
--   Firebird no soporta CREATE GENERATOR IF NOT EXISTS pre-3.0. En 3.0+ la
--   sintaxis es CREATE SEQUENCE; usamos EXECUTE BLOCK para no fallar si el
--   generador ya existe (caso producción Microsip).
-- ============================================================================

SET TERM ^ ;

EXECUTE BLOCK
AS
BEGIN
  IF (NOT EXISTS (
    SELECT 1 FROM RDB$GENERATORS
    WHERE RDB$GENERATOR_NAME = 'GEN_MST_FOLIO'
  )) THEN
    EXECUTE STATEMENT 'CREATE GENERATOR GEN_MST_FOLIO';
END^

SET TERM ; ^

COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (28, '000028_create_gen_mst_folio', CURRENT_TIMESTAMP);
COMMIT;
