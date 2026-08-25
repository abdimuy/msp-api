-- ============================================================================
-- Rollback de la migración 000057: MSP_GA_ARTICULO.REEMPLAZA_A
-- ============================================================================
-- Orden inverso al up: índice, constraint y al final la columna.
--
-- Aquí sí hay DROP INDEX y DROP CONSTRAINT, a diferencia del down de la
-- 000050: allá se dropeaban tablas completas y DROP TABLE se lleva sus
-- objetos dependientes. Una columna no. Firebird rechaza eliminar una
-- columna que participa en un índice o en una FK, así que hay que quitarlos
-- explícitamente y en ese orden.
-- ============================================================================

DROP INDEX IDX_MSP_GA_ARTICULO_REEMP;

COMMIT;

ALTER TABLE MSP_GA_ARTICULO
  DROP CONSTRAINT FK_MSP_GA_ARTICULO_REEMP;

COMMIT;

ALTER TABLE MSP_GA_ARTICULO
  DROP REEMPLAZA_A;

COMMIT;

-- ─── Quitar la migración del registro ───────────────────────────────────────
DELETE FROM MSP_MIGRATIONS WHERE ID = 57;

COMMIT;
